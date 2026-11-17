// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type compactionDirectoryIndexObserver struct{ creates, scans atomic.Int64 }

func (*compactionDirectoryIndexObserver) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}
func (h *compactionDirectoryIndexObserver) observe(cmd goredis.Cmder) {
	if wrapped, ok := cmd.(snapshotFencedCommand); ok {
		cmd = wrapped.Cmder
	}
	a := cmd.Args()
	if len(a) < 2 || !strings.EqualFold(fmt.Sprint(a[0]), "FT.CREATE") {
		return
	}
	h.creates.Add(1)
	for _, arg := range a[2:] {
		if strings.EqualFold(fmt.Sprint(arg), "SKIPINITIALSCAN") {
			return
		}
	}
	h.scans.Add(1)
}
func (h *compactionDirectoryIndexObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		h.observe(cmd)
		return next(ctx, cmd)
	}
}
func (h *compactionDirectoryIndexObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			h.observe(cmd)
		}
		return next(ctx, cmds)
	}
}

// Ordinary sealed outputs already avoid an initial database scan. Compaction
// must establish the same complete index from its registered output directory,
// including multi-batch output and tombstones retained by a selective merge.
func TestSnapshotCompactionBuildsIndexFromDirectory(t *testing.T) {
	for _, dense := range []bool{false, true} {
		t.Run(fmt.Sprintf("dense_%t", dense), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v BIGINT,payload TEXT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993,CASE WHEN i%5=0 THEN NULL ELSE 'row-'||i END FROM generate_series(1,257) AS g(i)")
			predicate := " WHERE id=1"
			if dense {
				predicate = ""
			}
			for range 3 {
				h.exec("UPDATE t SET v=v+1" + predicate)
			}
			if !dense {
				h.exec("DELETE FROM t WHERE id=2")
			}
			s := snapshotTestStore(t, raw)
			observer := &compactionDirectoryIndexObserver{}
			s.client.AddHook(observer)
			snapshotDrainCompactionTest(t, s)
			rows, _ := h.query("SELECT id,v,payload FROM t ORDER BY id")
			wantCount := 257
			if !dense {
				wantCount--
			}
			if len(rows) != wantCount {
				t.Fatal("incomplete final compaction fetch", len(rows))
			}
			position := 0
			for id := 1; id <= 257; id++ {
				if !dense && id == 2 {
					continue
				}
				v := int64(9007199254740993)
				if dense || id == 1 {
					v += 3
				}
				payload := fmt.Sprintf("row-%d", id)
				if id%5 == 0 {
					payload = "NULL"
				}
				if rows[position] != fmt.Sprintf("%d|%d|%s", id, v, payload) {
					t.Fatalf("wrong final compaction row %d", id)
				}
				position++
			}
			if n := observer.creates.Load(); n != 1 {
				t.Fatalf("compaction created %d indexes instead of one final index", n)
			}
			if n := observer.scans.Load(); n != 0 {
				t.Fatalf("compaction started %d full-keyspace index scans instead of populating its exact directory", n)
			}
		})
	}
}

func assertCompactionAtomicRows(t *testing.T, h *sqlHarness) {
	t.Helper()
	rows, _ := h.query("SELECT id,v FROM t ORDER BY id")
	if len(rows) != 257 {
		t.Fatal("incomplete resumed compaction fetch", len(rows))
	}
	for i, row := range rows {
		want := 1
		if i < 128 {
			want = 4
		}
		if row != fmt.Sprintf("%d|%d", i+1, want) {
			t.Fatalf("wrong resumed compaction row %d", i+1)
		}
	}
}

func TestSnapshotCompactionDiscardsUnindexedOutput(t *testing.T) {
	for _, checkpointed := range []bool{false, true} {
		t.Run(fmt.Sprint(checkpointed), func(t *testing.T) {
			h, _, s, job, _ := compactionAtomicFixture(t)
			// Simulate a populated output whose required index is absent.
			ctx := s.compactionWriteContext(h.ctx, job)
			if err := s.client.HSet(ctx, job.Prefix+"1", rowIDField, "1", "id", "1", "v", "4").Err(); err != nil {
				t.Fatal(err)
			}
			if checkpointed {
				job.After = formatSnapshotIndexMember(1)
				if err := s.checkpointSnapshotCompaction(h.ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			observer := &compactionDirectoryIndexObserver{}
			s.client.AddHook(observer)
			if err := s.releaseSnapshotCompaction(h.ctx, job.Token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, s)
			assertCompactionAtomicRows(t, h)
			if observer.creates.Load() != 1 || observer.scans.Load() != 0 {
				t.Fatalf("unindexed output was not rebuilt: creates=%d initial_scans=%d", observer.creates.Load(), observer.scans.Load())
			}
		})
	}
}

type compactionIndexCreateInterruption struct {
	compactionDirectoryIndexObserver
	cancel context.CancelFunc
	fired  atomic.Bool
}

func (h *compactionIndexCreateInterruption) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		h.observe(cmd)
		err := next(ctx, cmd)
		if a := cmd.Args(); err == nil && len(a) > 1 && strings.EqualFold(fmt.Sprint(a[0]), "FT.CREATE") && h.fired.CompareAndSwap(false, true) {
			if h.cancel != nil {
				h.cancel()
				return nil
			}
			err = fmt.Errorf("lost successful empty compaction index reply")
			cmd.SetErr(err)
		}
		return err
	}
}

func TestSnapshotCompactionPreindexInterrupted(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			h, raw, s, job, token := compactionAtomicFixture(t)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			observer := &compactionIndexCreateInterruption{}
			if canceled {
				observer.cancel = cancel
			}
			s.client.AddHook(observer)
			if err := s.copySnapshotCompactionBatch(ctx, job); err == nil || !observer.fired.Load() {
				t.Fatal("index interruption did not stop before copying", err)
			}
			durable, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || durable == nil || durable.After != "" || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 0 {
				t.Fatal("index interruption advanced rows or checkpoint", err)
			}
			if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, snapshotTestStore(t, raw))
			assertCompactionAtomicRows(t, h)
			if observer.creates.Load() != 1 || observer.scans.Load() != 0 {
				t.Fatal("resume replayed index creation or started an initial scan")
			}
		})
	}
}

func TestSnapshotCompactionPreindexAdmission(t *testing.T) {
	for _, mode := range []string{"stale_owner", "populated", "advanced", "initial_scan"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, s, job, _ := compactionAtomicFixture(t)
			if mode == "populated" || mode == "advanced" {
				if err := s.client.HSet(s.compactionWriteContext(h.ctx, job), job.Prefix+"1", rowIDField, "1", "id", "1", "v", "4").Err(); err != nil {
					t.Fatal(err)
				}
				if mode == "advanced" {
					job.After = formatSnapshotIndexMember(1)
					if err := s.checkpointSnapshotCompaction(h.ctx, job); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "stale_owner" {
				job.Token = snapshotID()
			}
			output := *job.Source
			output.KeyPrefix = job.Prefix
			output.IndexName = snapshotIndexPrefix(job.Data) + "guard"
			args := indexCreateArgs(&output)
			if mode == "initial_scan" {
				args = snapshotScannedIndexArgs(&output)
			}
			if err := s.searchDo(s.compactionWriteContext(h.ctx, job), output.index(), args...).Err(); err == nil {
				t.Fatal("invalid early index admission succeeded")
			}
			if err := raw.Do(h.ctx, "FT.INFO", output.index()).Err(); !isUnknownIndex(err) {
				t.Fatal("rejected early creation left an index", err)
			}
		})
	}
}
