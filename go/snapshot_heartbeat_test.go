// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type heartbeatControlClient struct {
	goredis.UniversalClient
	mu       sync.Mutex
	ops      []string
	renews   int
	firstErr error
	valid    int64
	validErr error
	called   chan string
}

func (c *heartbeatControlClient) Process(ctx context.Context, cmd goredis.Cmder) error {
	a := snapshotObservedControlArgs(cmd)
	var keys int
	fmt.Sscan(fmt.Sprint(a[2]), &keys)
	op := fmt.Sprint(a[3+keys])
	c.mu.Lock()
	c.ops = append(c.ops, op)
	var err error
	n := int64(1)
	if op == "renew" {
		c.renews++
		if c.renews == 1 {
			err = c.firstErr
		}
	}
	if op == "valid" {
		n = c.valid
		err = c.validErr
	}
	c.mu.Unlock()
	if marker, ok := cmd.(interface{ NoRetry() bool }); !ok || !marker.NoRetry() {
		panic("heartbeat command permits transport replay")
	}
	cmd.(onceCmd).SetVal(n)
	cmd.SetErr(err)
	c.called <- op
	return err
}

func heartbeatTestPin(t *testing.T, firstErr error, contexts ...context.Context) (*snapshotPin, *heartbeatControlClient, chan time.Time) {
	t.Helper()
	c := &heartbeatControlClient{firstErr: firstErr, valid: 1, called: make(chan string, 32)}
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	p := &snapshotPin{ctx: ctx, store: &store{client: c}, token: "token", root: "root", stop: make(chan struct{}), done: make(chan struct{})}
	ticks := make(chan time.Time)
	go p.heartbeatTicks(ticks)
	t.Cleanup(p.release)
	return p, c, ticks
}

func heartbeatTestTick(t *testing.T, p *snapshotPin, ticks chan time.Time) {
	t.Helper()
	select {
	case ticks <- time.Now():
	case <-p.done:
		t.Fatal("heartbeat abandoned a lease after an uncertain renewal")
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not accept its next tick")
	}
}

func heartbeatTestCall(t *testing.T, c *heartbeatControlClient, want string) {
	t.Helper()
	select {
	case got := <-c.called:
		if got != want {
			t.Fatalf("control operation = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("missing %s control operation", want)
	}
}

func TestSnapshotHeartbeatUncertainRenewalRequiresLiveProof(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, io.ErrUnexpectedEOF, errors.Join(io.EOF, controlScriptNativeError("NOPERM uncertain reply"))} {
		t.Run(err.Error(), func(t *testing.T) {
			p, c, ticks := heartbeatTestPin(t, err)
			heartbeatTestTick(t, p, ticks)
			heartbeatTestCall(t, c, "renew")
			// No immediate replay: only the next scheduled heartbeat may act,
			// and it must first prove this exact root/token remains live.
			heartbeatTestTick(t, p, ticks)
			heartbeatTestCall(t, c, "valid")
			heartbeatTestCall(t, c, "renew")
			if err := p.valid(context.Background()); err != nil {
				t.Fatal(err)
			}
			heartbeatTestCall(t, c, "valid")
		})
	}
}

func TestSnapshotHeartbeatUncertainRenewalCannotReviveExpiredPin(t *testing.T) {
	p, c, ticks := heartbeatTestPin(t, context.DeadlineExceeded)
	c.valid = 0
	heartbeatTestTick(t, p, ticks)
	heartbeatTestCall(t, c, "renew")
	heartbeatTestTick(t, p, ticks)
	heartbeatTestCall(t, c, "valid")
	heartbeatTestCall(t, c, "release")
	<-p.done
	if p.valid(context.Background()) == nil {
		t.Fatal("expired pin remained usable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.renews != 1 {
		t.Fatal("expired pin was renewed")
	}
}

func TestSnapshotHeartbeatUncertainProofDoesNotRenew(t *testing.T) {
	p, c, ticks := heartbeatTestPin(t, io.EOF)
	c.validErr = context.DeadlineExceeded
	heartbeatTestTick(t, p, ticks)
	heartbeatTestCall(t, c, "renew")
	heartbeatTestTick(t, p, ticks)
	heartbeatTestCall(t, c, "valid")
	if p.valid(context.Background()) == nil {
		t.Fatal("unavailable server proof was accepted")
	}
	heartbeatTestCall(t, c, "valid")
	p.release()
	heartbeatTestCall(t, c, "release")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.renews != 1 {
		t.Fatal("uncertain proof allowed renewal")
	}
}

func TestSnapshotHeartbeatNativeRenewalRefusalIsTerminal(t *testing.T) {
	p, c, ticks := heartbeatTestPin(t, controlScriptNativeError("NOPERM lease renewal denied"))
	heartbeatTestTick(t, p, ticks)
	heartbeatTestCall(t, c, "renew")
	heartbeatTestCall(t, c, "release")
	<-p.done
	if p.valid(context.Background()) == nil {
		t.Fatal("native refusal left pin usable")
	}
}

func TestSnapshotHeartbeatCancellationAfterUncertainRenewal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, c, ticks := heartbeatTestPin(t, io.EOF, ctx)
	heartbeatTestTick(t, p, ticks)
	heartbeatTestCall(t, c, "renew")
	cancel()
	heartbeatTestCall(t, c, "release")
	<-p.done
	if p.valid(context.Background()) == nil {
		t.Fatal("canceled pin remained usable")
	}
}
