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

func executionPoolFixture(t *testing.T, cluster bool, hang ...string) (*connectionImpl, *fakeRedis) {
	t.Helper()
	server := newFakeRedis(t, hang...)
	cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
	cfg.cluster.ClusterSlots = func(context.Context) ([]goredis.ClusterSlot, error) {
		return []goredis.ClusterSlot{{Start: 0, End: 16383, Nodes: []goredis.ClusterNode{{Addr: server.addr}}}}, nil
	}
	c := &connectionImpl{store: &store{client: newClient(cfg, cluster, timeouts{})}, clientCfg: cfg}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, server
}

func TestExecutionPoolReusesOnlyCompletedLease(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		t.Run(fmt.Sprint(cluster), func(t *testing.T) {
			c, server := executionPoolFixture(t, cluster)
			var prior *executionTransport
			var pool *executionPool
			handshakes := 0
			for range 4 {
				ctx, transport := c.executionContext(context.Background(), &c.streams)
				defer transport.finish()
				if transport == prior {
					t.Fatal("execution lifetime reused")
				}
				if pool != nil && transport.pool != pool {
					t.Fatal("completed pool not reused")
				}
				if prior != nil {
					// A caller retaining an old cancellation handle cannot close the new lease.
					prior.closeWith(context.Canceled)
				}
				if err := c.store.redis(ctx).Ping(ctx).Err(); err != nil {
					t.Fatal(err)
				}
				if handshakes == 0 {
					handshakes = server.count("HELLO")
				}
				transport.finish()
				if len(c.store.activeExecutions()) != 0 {
					t.Fatal("completed execution remains active")
				}
				prior, pool = transport, transport.pool
			}
			if got := server.count("HELLO"); got != handshakes {
				t.Fatalf("RESP3 handshakes=%d, want unchanged initial %d after four executions", got, handshakes)
			}
			c.store.mu.Lock()
			idle := c.store.idleExecution
			c.store.mu.Unlock()
			if idle != pool {
				t.Fatal("idle pool not retained")
			}
			if err := c.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := pool.client.Ping(context.Background()).Err(); err == nil {
				t.Fatal("Close left idle pool usable")
			}
		})
	}
}

func TestExecutionPoolsIsolateConcurrentOwners(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		t.Run(fmt.Sprint(cluster), func(t *testing.T) {
			c, server := executionPoolFixture(t, cluster)
			a, ta := c.executionContext(context.Background(), &c.streams)
			b, tb := c.executionContext(context.Background(), &c.streams)
			if ta.pool == tb.pool {
				t.Fatal("active executions share a pool")
			}
			if err := c.store.redis(a).Ping(a).Err(); err != nil {
				t.Fatal(err)
			}
			if err := c.store.redis(b).Ping(b).Err(); err != nil {
				t.Fatal(err)
			}
			ta.closeWith(context.Canceled)
			ta.finish()
			if err := c.store.redis(b).Ping(b).Err(); err != nil {
				t.Fatal("cancel affected another execution", err)
			}
			tb.finish()
			handshakes := server.count("HELLO")
			if handshakes < 2 {
				t.Fatal("concurrent pools did not negotiate independently")
			}
			next, tn := c.executionContext(context.Background())
			defer tn.finish()
			if tn.pool != tb.pool {
				t.Fatal("healthy concurrent pool not retained")
			}
			if err := c.store.redis(next).Ping(next).Err(); err != nil {
				t.Fatal(err)
			}
			if server.count("HELLO") != handshakes {
				t.Fatal("healthy pool redialed")
			}
		})
	}
}

func TestExecutionPoolDiscardsUnknownReplyWithoutReplay(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		t.Run(fmt.Sprint(cluster), func(t *testing.T) {
			c, server := executionPoolFixture(t, cluster, "SET")
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			active, transport := c.executionContext(ctx)
			if err := c.store.redis(active).Set(active, "uncertain", "value", 0).Err(); err == nil {
				t.Fatal("missing reply accepted")
			}
			handshakes := server.count("HELLO")
			transport.finish()
			next, tn := c.executionContext(context.Background())
			defer tn.finish()
			if tn.pool == transport.pool {
				t.Fatal("uncertain/canceled pool reused")
			}
			if err := c.store.redis(next).Ping(next).Err(); err != nil {
				t.Fatal(err)
			}
			if server.count("SET") != 1 {
				t.Fatal("unknown write replayed")
			}
			if server.count("HELLO") <= handshakes {
				t.Fatal("next execution did not negotiate a fresh socket")
			}
		})
	}
}

func TestExecutionPoolRetainsAfterCompleteNativeError(t *testing.T) {
	c, _ := executionPoolFixture(t, false)
	ctx, t1 := c.executionContext(context.Background())
	if err := c.store.redis(ctx).Do(ctx, "UNKNOWN").Err(); err == nil {
		t.Fatal("fake server did not reject command")
	}
	t1.finish()
	_, t2 := c.executionContext(context.Background())
	defer t2.finish()
	if t1.pool != t2.pool {
		t.Fatal("complete native error unnecessarily discarded pool")
	}
}

func TestExecutionPoolTimeoutIdentity(t *testing.T) {
	c, server := executionPoolFixture(t, false)
	ctx, t1 := c.executionContext(context.Background())
	if err := c.store.redis(ctx).Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	t1.finish()
	ctx, t2 := c.executionContextWithTimeouts(context.Background(), timeouts{read: time.Second, write: time.Second})
	defer t2.finish()
	if t1.pool == t2.pool {
		t.Fatal("pool with different timeouts reused")
	}
	if err := c.store.redis(ctx).Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if server.count("HELLO") != 2 {
		t.Fatal("replacement timeout pool not negotiated")
	}
}

func TestExecutionPoolRetainsAtMostOneIdlePool(t *testing.T) {
	c, _ := executionPoolFixture(t, false)
	ca, a := c.executionContext(context.Background())
	cb, b := c.executionContext(context.Background())
	if err := c.store.redis(ca).Ping(ca).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.store.redis(cb).Ping(cb).Err(); err != nil {
		t.Fatal(err)
	}
	a.finish()
	b.finish()
	c.store.mu.Lock()
	idle := c.store.idleExecution
	c.store.mu.Unlock()
	if idle != a.pool {
		t.Fatal("first idle pool not preserved")
	}
	if err := b.pool.client.Ping(context.Background()).Err(); err == nil {
		t.Fatal("excess idle pool stayed open")
	}
}

func TestSnapshotReaperRunningRequiresLiveWorker(t *testing.T) {
	s := &store{}
	if s.snapshotReaperRunning() {
		t.Fatal("missing worker treated as collector")
	}
	done := make(chan struct{})
	s.compactor = &snapshotCompactor{done: done}
	if !s.snapshotReaperRunning() {
		t.Fatal("live worker missed")
	}
	close(done)
	if s.snapshotReaperRunning() {
		t.Fatal("completed worker treated as collector")
	}
	s.compactor = nil
	if s.snapshotReaperRunning() {
		t.Fatal("stopped worker treated as collector")
	}
}

func TestExecutionPoolMissingValueIsCompleteReply(t *testing.T) {
	c, server := executionPoolFixture(t, false)
	ctx, t1 := c.executionContext(context.Background())
	if err := c.store.redis(ctx).HGet(ctx, "missing", "field").Err(); err != goredis.Nil {
		t.Fatal(err)
	}
	t1.finish()
	next, t2 := c.executionContext(context.Background())
	defer t2.finish()
	if t1.pool != t2.pool {
		t.Fatal("missing value discarded drained pool")
	}
	if err := c.store.redis(next).Ping(next).Err(); err != nil {
		t.Fatal(err)
	}
	if server.count("HELLO") != 1 {
		t.Fatal("missing value caused another handshake")
	}
}

func TestExecutionPoolUncertaintyDoesNotTrustWrappedRefusals(t *testing.T) {
	native := redisErr("ERR complete server reply")
	for _, tc := range []struct {
		err       error
		uncertain bool
	}{
		{nil, false}, {goredis.Nil, false}, {native, false},
		{io.ErrUnexpectedEOF, true}, {context.Canceled, true},
		{fmt.Errorf("transport: %w", native), true},
		{errors.Join(goredis.Nil, io.EOF), true},
		{errors.Join(native, io.EOF), true},
	} {
		if got := executionPoolUncertain(tc.err); got != tc.uncertain {
			t.Fatalf("uncertainty for %T = %t, want %t", tc.err, got, tc.uncertain)
		}
	}
}

func TestExecutionPoolStaleCancellationDuringNextLease(t *testing.T) {
	c, server := executionPoolFixture(t, false)
	ctx, a := c.executionContext(context.Background())
	if err := c.store.redis(ctx).Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	a.finish()
	next, b := c.executionContext(context.Background())
	defer b.finish()
	if a.pool != b.pool {
		t.Fatal("pool did not transfer")
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 32 {
			a.closeWith(context.Canceled)
		}
	})
	for range 32 {
		if err := c.store.redis(next).Ping(next).Err(); err != nil {
			t.Fatal("stale owner interrupted next lease", err)
		}
	}
	wg.Wait()
	if server.count("HELLO") != 1 {
		t.Fatal("stale cancellation forced a redial")
	}
}

func TestExecutionPoolConcurrentFinishAndConnectionClose(t *testing.T) {
	for range 16 {
		c, _ := executionPoolFixture(t, false)
		ctx, transport := c.executionContext(context.Background(), &c.streams)
		if err := c.store.redis(ctx).Ping(ctx).Err(); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; transport.finish() })
		wg.Go(func() {
			<-start
			if err := c.Close(context.Background()); err != nil {
				t.Error(err)
			}
		})
		close(start)
		wg.Wait()
		c.store.mu.Lock()
		idle := c.store.idleExecution
		c.store.mu.Unlock()
		if idle != nil || len(c.store.activeExecutions()) != 0 {
			t.Fatal("Close leaked an idle/active pool")
		}
		if err := transport.pool.client.Ping(context.Background()).Err(); err == nil {
			t.Fatal("Close left execution pool usable")
		}
	}
}

func TestExecutionPoolPipelineNativeErrorCannotHideLostLaterReply(t *testing.T) {
	for _, later := range []error{io.ErrUnexpectedEOF, context.DeadlineExceeded, errors.Join(goredis.Nil, io.EOF)} {
		p := &executionPool{}
		ctx, cancel := context.WithCancelCause(context.Background())
		transport := &executionTransport{ctx: ctx, cancel: cancel, pool: p}
		ctx = context.WithValue(ctx, transportContextKey{}, transport)
		native := redisErr("ERR first command refused")
		a, b := goredis.NewCmd(ctx, "first"), goredis.NewCmd(ctx, "second")
		hook := p.ProcessPipelineHook(func(context.Context, []goredis.Cmder) error { a.SetErr(native); b.SetErr(later); return native })
		if err := hook(ctx, []goredis.Cmder{a, b}); err != native {
			t.Fatal("original pipeline error changed", err)
		}
		transport.mu.Lock()
		failed := transport.failed
		transport.mu.Unlock()
		cancel(nil)
		if !failed {
			t.Fatalf("native first error hid uncertain %T later reply", later)
		}
	}
}
