// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotReleaseOutage struct {
	snapshotHook
	unavailable atomic.Bool
	attempts    atomic.Int64
	block       atomic.Bool
	started     chan struct{}
	unblock     chan struct{}
	signaled    atomic.Bool
}

func (h *snapshotReleaseOutage) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		if len(a) > 3 && snapshotObservedControlName(cmd) == "eval" && (a[1] == snapshotControl || a[1] == snapshotReaderControl) {
			var keys int
			fmt.Sscan(fmt.Sprint(a[2]), &keys)
			if 3+keys < len(a) && a[3+keys] == "release" {
				h.attempts.Add(1)
				if h.unavailable.Load() {
					if h.block.Load() {
						if h.signaled.CompareAndSwap(false, true) {
							close(h.started)
						}
						select {
						case <-ctx.Done():
						case <-h.unblock:
						}
					}
					err := errors.New("injected pin cleanup connection outage")
					cmd.SetErr(err)
					return err
				}
			}
		}
		return next(ctx, cmd)
	}
}

func TestSnapshotPinConcurrentFailedReleaseSharesOneBoundedAttempt(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	outage := &snapshotReleaseOutage{started: make(chan struct{}), unblock: make(chan struct{})}
	outage.unavailable.Store(true)
	s.client.AddHook(outage)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	_, pin, err := s.acquireSnapshot(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-pin.done:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat cleanup did not stop")
	}
	outage.block.Store(true)
	var callers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		callers.Go(func() { <-start; pin.release() })
	}
	defer func() { close(outage.unblock); callers.Wait() }()
	close(start)
	select {
	case <-outage.started:
	case <-time.After(time.Second):
		t.Fatal("explicit cleanup retry did not start")
	}
	done := make(chan struct{})
	go func() { callers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("concurrent cleanup callers did not share one bounded attempt")
	}
	if outage.attempts.Load() != 2 {
		t.Fatalf("concurrent failure sent %d cleanup commands", outage.attempts.Load())
	}
	if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.token).Val() {
		t.Fatal("failed cleanup unexpectedly deleted its lease")
	}
	outage.unavailable.Store(false)
	pin.release()
	if outage.attempts.Load() != 3 || raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.token).Val() {
		t.Fatal("a later release did not clean the lease after transport recovery")
	}
}

func TestSnapshotPinReleaseAfterHeartbeatCleanupOutage(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		t.Run(fmt.Sprintf("restricted_reader_%t", restricted), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			s.restrictedReader = restricted
			if restricted {
				s.readerNamespace = snapshotReaderNamespace("cleanup-outage-reader")
			}
			outage := &snapshotReleaseOutage{}
			outage.unavailable.Store(true)
			s.client.AddHook(outage)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			_, pin, err := s.acquireSnapshot(ctx, !restricted)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			field := "p:" + pin.token
			if restricted {
				field = pin.token
			}
			cancel()
			select {
			case <-pin.done:
			case <-time.After(5 * time.Second):
				t.Fatal("heartbeat cleanup did not stop")
			}
			if outage.attempts.Load() != 1 || !raw.HExists(h.ctx, pin.leaseKey(), field).Val() {
				t.Fatal("failed heartbeat cleanup did not retain its server lease")
			}
			outage.unavailable.Store(false)
			pin.release()
			if raw.HExists(h.ctx, pin.leaseKey(), field).Val() {
				t.Fatal("explicit release left the failed heartbeat's writer lease blocking compaction")
			}
			if outage.attempts.Load() != 2 {
				t.Fatalf("cleanup attempts: %d", outage.attempts.Load())
			}
			pin.release()
			if outage.attempts.Load() != 2 {
				t.Fatal("successful cleanup was repeated")
			}
		})
	}
}

func TestSnapshotPinNormalConcurrentReleaseIsOneCommand(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	observer := &snapshotReleaseOutage{}
	s.client.AddHook(observer)
	_, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var callers sync.WaitGroup
	for i := 0; i < 8; i++ {
		callers.Go(pin.release)
	}
	callers.Wait()
	if observer.attempts.Load() != 1 || raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.token).Val() {
		t.Fatal("normal concurrent release must delete its lease with one command")
	}
}
