// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type compactionBurstBoundaryHook struct {
	afterFirst func()
	copies     int
}

func (*compactionBurstBoundaryHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*compactionBurstBoundaryHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *compactionBurstBoundaryHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && compactionPhysicalCopyRows(cmd) >= 0 {
			h.copies++
			if h.copies == 1 && h.afterFirst != nil {
				h.afterFirst()
			}
		}
		return err
	}
}

func TestSnapshotCompactionBurst(t *testing.T) {
	for _, mode := range []string{"complete", "lost_reply", "takeover", "ddl", "cancel", "deleted_prefix"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,1100) AS g(i)")
			// Sparse overlays exercise the large-input threshold independently of width.
			if mode == "deleted_prefix" {
				h.exec("DELETE FROM t WHERE id<=256")
			} else {
				h.exec("UPDATE t SET v=v+1 WHERE id=1")
			}
			h.exec("UPDATE t SET v=v+1 WHERE id>900")
			h.exec("UPDATE t SET v=v+1 WHERE id>900")
			s := snapshotTestStore(t, raw)
			token := snapshotID()
			if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
				t.Fatal(started, err)
			}
			defer s.releaseSnapshotCompaction(h.ctx, token)
			job, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || job == nil {
				t.Fatal(err)
			}
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			boundary := &compactionBurstBoundaryHook{}
			successor := ""
			if mode == "takeover" {
				peer := snapshotTestStore(t, raw)
				successor = snapshotID()
				boundary.afterFirst = func() {
					if err := peer.releaseSnapshotCompaction(h.ctx, token); err != nil {
						t.Fatal(err)
					}
					adopted, _, pin, err := peer.acquireCompactionStepCatalog(h.ctx, successor)
					if err != nil || adopted == nil {
						t.Fatal("takeover", err)
					}
					pin.release()
				}
			}
			if mode == "ddl" {
				boundary.afterFirst = func() { h.exec("DROP TABLE t") }
			}
			if mode == "cancel" {
				boundary.afterFirst = cancel
			}
			observer := &compactionAtomicBatchObserver{lose: mode == "lost_reply" || mode == "takeover"}
			s.client.AddHook(observer)
			s.client.AddHook(boundary)
			err = s.copySnapshotCompactionBurst(ctx, job, root)
			if mode == "cancel" {
				if err != context.Canceled {
					t.Fatal("cancellation", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantCopies := snapshotCompactionCopyBatchesPerStep
			if mode == "ddl" || mode == "cancel" || mode == "takeover" {
				wantCopies = 1
			}
			if int(observer.commands.Load()) != wantCopies {
				t.Fatal("unbounded copy or replay", observer.commands.Load(), wantCopies)
			}
			durable, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || durable == nil {
				t.Fatal(err)
			}
			if durable.After != fmt.Sprintf("%020d", wantCopies*snapshotCompactionBatchRows) || durable.CopyBatch.Rows > snapshotCompactionBatchRows {
				t.Fatal("checkpoint or persisted batch bound")
			}
			if mode == "deleted_prefix" && raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 0 {
				t.Fatal("deleted winners copied")
			}
			if successor != "" {
				if job.Token != successor {
					t.Fatal("successor progress not reconciled")
				}
				if err := s.releaseSnapshotCompaction(h.ctx, successor); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, s)
			if mode == "ddl" {
				return
			}
			want := 1100
			if mode == "deleted_prefix" {
				want -= 256
			}
			h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", fmt.Sprintf("%d|9007199254740993|9007199254740995", want))
		})
	}
}

func TestSnapshotCompactionBurstStopsAtEOF(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,1100) AS g(i)")
	for range 3 {
		h.exec("UPDATE t SET v=v+1 WHERE id<=200")
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal(started, err)
	}
	defer s.releaseSnapshotCompaction(h.ctx, token)
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	for n := 0; n < 17; n++ {
		if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	observer := &compactionAtomicBatchObserver{}
	s.client.AddHook(observer)
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := s.copySnapshotCompactionBurst(h.ctx, job, root); err != nil {
		t.Fatal(err)
	}
	if job.Phase != "index" || job.After != fmt.Sprintf("%020d", 1100) || observer.commands.Load() != 1 || observer.rows.Load() != 12 {
		t.Fatal("burst crossed EOF/phase", job.Phase, observer.commands.Load(), observer.rows.Load())
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "1100|1|4")
}
