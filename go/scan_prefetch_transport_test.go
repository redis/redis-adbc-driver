// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type prefetchCloseObserver struct {
	goredis.UniversalClient
	exited, premature *atomic.Bool
}

func (c prefetchCloseObserver) Close() error {
	if !c.exited.Load() {
		c.premature.Store(true)
	}
	return nil
}

func TestHashPrefetchTransportFinishWaitsForWorker(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	var exited, premature atomic.Bool
	transport := &executionTransport{ctx: ctx, cancel: cancel, client: prefetchCloseObserver{exited: &exited, premature: &premature}}
	ctx = context.WithValue(ctx, transportContextKey{}, transport)
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := newHashPagePrefetch(ctx, func(ctx context.Context) hashPage {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release // the pool must remain open while worker cleanup is unfinished
		exited.Store(true)
		return hashPage{err: io.EOF}
	})
	awaitPrefetch(t, started)
	finished := make(chan struct{})
	go func() { transport.finish(); close(finished) }()
	awaitPrefetch(t, canceled)
	close(release)
	awaitPrefetch(t, finished)
	p.close()
	if premature.Load() || !exited.Load() {
		t.Fatal("pool closed before its background worker exited")
	}
	var called atomic.Bool
	late := newHashPagePrefetch(ctx, func(context.Context) hashPage { called.Store(true); return hashPage{err: io.EOF} })
	late.close()
	if called.Load() {
		t.Fatal("worker started after pool finish")
	}
}

func TestHashPrefetchConnectionCloseDuringInitialFillJoinsWorker(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	var exited, maintenancePremature, executionPremature atomic.Bool
	connection := &connectionImpl{store: &store{client: prefetchCloseObserver{exited: &exited, premature: &maintenancePremature}}}
	transport := &executionTransport{owner: connection.store, ctx: ctx, cancel: cancel,
		client: prefetchCloseObserver{exited: &exited, premature: &executionPremature}, owners: []*streams{&connection.streams}}
	connection.streams.add(transport) // initial fill: the public reader isn't attached yet
	ctx = context.WithValue(ctx, transportContextKey{}, transport)
	started := make(chan struct{})
	p := newHashPagePrefetchWithCleanup(ctx, func(ctx context.Context) hashPage {
		close(started)
		<-ctx.Done()
		return hashPage{err: io.EOF}
	}, func(context.Context) {
		// Model in-flight cursor cleanup after interruption. The maintenance
		// pool must remain open until this cleanup has joined.
		time.Sleep(200 * time.Millisecond)
		exited.Store(true)
	})
	awaitPrefetch(t, started)
	if err := connection.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.close()
	transport.finish()
	if maintenancePremature.Load() || executionPremature.Load() {
		t.Fatal("connection closed a pool before initial-fill prefetch cleanup joined")
	}
}

func TestHashPrefetchJoinsBlockedNetworkWork(t *testing.T) {
	for _, command := range []string{"HMGET", "FT.CURSOR"} {
		t.Run(command, func(t *testing.T) {
			server := newFakeRedis(t, command)
			cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
			ordinary := newClient(cfg, false, timeouts{})
			defer ordinary.Close()
			connection := &connectionImpl{store: &store{client: ordinary}, clientCfg: cfg}
			ctx, transport := connection.executionContext(context.Background())
			defer transport.finish()
			p := newHashPagePrefetch(ctx, func(ctx context.Context) hashPage {
				if command == "FT.CURSOR" {
					return hashPage{err: once(ctx, connection.store.redis(ctx), "FT.CURSOR", "READ", "idx", 1, "COUNT", 10).Err()}
				}
				values, err := connection.store.fetchRowSlices(ctx, []string{"row"}, []string{"value"}, wideFetchChunk)
				return hashPage{values: values, err: err}
			})
			defer p.close()
			deadline := time.Now().Add(2 * time.Second)
			for server.count(command) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if server.count(command) == 0 {
				t.Fatal("network work did not start")
			}
			joined := make(chan struct{})
			go func() { p.close(); close(joined) }()
			awaitPrefetch(t, joined)
			// Releasing the execution transport happens only after this join.
			if got := server.count(command); got != 1 {
				t.Fatalf("canceled network work replayed %d times", got)
			}
		})
	}
}
