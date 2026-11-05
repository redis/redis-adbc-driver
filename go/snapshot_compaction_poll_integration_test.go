// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type compactionPendingIndexHook struct {
	snapshotHook
	index  string
	busy   int64 // negative means busy until cancellation
	reads  atomic.Int64
	events chan time.Time
}

func (h *compactionPendingIndexHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if cmd.Name() != "ft.info" || len(a) < 2 || fmt.Sprint(a[1]) != h.index {
			return next(ctx, cmd)
		}
		n := h.reads.Add(1)
		var err error
		if h.busy < 0 || n <= h.busy {
			cmd.(interface{ SetVal(any) }).SetVal([]any{"indexing", int64(1), "percent_indexed", "0"})
		} else {
			err = next(ctx, cmd)
		}
		if n <= 8 {
			h.events <- time.Now()
		}
		return err
	}
}

// No caller wakes the worker between readiness probes. A known owned job must
// not fall back to the one-second discovery interval after a busy index reply.
func TestSnapshotCompactionPendingIndexPolling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		busy   int64
		cancel bool
	}{
		{"ready", 1, false},
		{"backoff", 7, false},
		{"cancel", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
			for range 3 {
				h.exec("UPDATE t SET v=v+1 WHERE id=1")
			}
			s := snapshotTestStore(t, raw)
			token := snapshotID()
			if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
				t.Fatal("job did not start", started, err)
			}
			job, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || job == nil {
				t.Fatal("missing job", err)
			}
			for step := 0; job.Phase == "copy" && step < 10; step++ {
				if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			if job.Phase != "index" {
				t.Fatal("bounded copy setup did not reach indexing")
			}
			// Create the real output index before injecting one busy observation.
			if progress, err := s.indexSnapshotCompaction(h.ctx, job); err != nil || !progress || job.Phase != "index" {
				t.Fatal("output index was not created", progress, err)
			}
			hook := &compactionPendingIndexHook{index: snapshotIndexPrefix(job.Data) + "compacted", busy: tc.busy, events: make(chan time.Time, 8)}
			s.client.AddHook(hook)
			ctx, cancel := context.WithCancel(h.ctx)
			worker := &snapshotCompactor{store: s, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), token: token}
			defer func() {
				cancel()
				select {
				case <-worker.done:
				case <-time.After(3 * time.Second):
					t.Error("worker did not stop and release ownership")
				}
			}()
			worker.wake <- struct{}{}
			go worker.run(ctx)
			var first time.Time
			select {
			case first = <-hook.events:
			case <-time.After(5 * time.Second):
				t.Fatal("worker never observed the busy index")
			}
			if tc.cancel {
				cancel()
				select {
				case <-worker.done:
				case <-time.After(500 * time.Millisecond):
					t.Fatal("pending readiness wait ignored cancellation")
				}
			} else {
				select {
				case <-hook.events:
				case <-time.After(500 * time.Millisecond):
					t.Fatal("owned index readiness fell back to idle discovery polling")
				}
				if tc.busy > 1 {
					var last time.Time
					for n := int64(2); n <= tc.busy; n++ {
						select {
						case last = <-hook.events:
						case <-time.After(5 * time.Second):
							t.Fatal("bounded readiness polling did not resume")
						}
					}
					if last.Sub(first) < 200*time.Millisecond {
						t.Fatal("busy readiness polling did not back off")
					}
				}
			}
			check := snapshotTestStore(t, raw)
			if tc.cancel {
				pending, err := check.readSnapshotCompaction(h.ctx)
				if err != nil || pending == nil || pending.Phase != "index" || pending.Data != job.Data {
					t.Fatal("cancellation lost the recoverable output", err)
				}
				snapshotDrainCompactionTest(t, check)
			} else {
				deadline := time.Now().Add(5 * time.Second)
				for {
					pending, err := check.readSnapshotCompaction(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					if pending == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("ready worker failed to publish")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			pinned, pin, err := check.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			installed := snapshotTableTest(t, check, pinned, "t").prefix()
			pin.release()
			if installed != job.Prefix {
				t.Fatal("worker replaced its durable output instead of resuming it")
			}
			rows, _ := h.query("SELECT id,v FROM t ORDER BY id")
			if len(rows) != 257 {
				t.Fatal("incomplete final fetch", len(rows))
			}
			for i, row := range rows {
				want := int64(9007199254740993)
				if i == 0 {
					want += 3
				}
				if row != fmt.Sprintf("%d|%d", i+1, want) {
					t.Fatalf("wrong final row %d", i+1)
				}
			}
		})
	}
}
