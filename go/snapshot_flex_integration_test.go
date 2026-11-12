// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// Run on a disposable Software RAM or Flex endpoint via REDIS_SNAPSHOT_URI.
// The same plain-index contract must work on both products.
func TestSnapshotFlexLifecycle(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT,s TEXT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,CASE WHEN i%5=0 THEN NULL ELSE 'row-'||i END FROM generate_series(1,257) AS g(i)")
	h.expectRows("SELECT id FROM t WHERE id<=3 ORDER BY id DESC", "3", "2", "1")
	h.expectRows("SELECT COUNT(*),SUM(id) FROM t", "257|33153")
	s := snapshotTestStore(t, raw)
	s.flex = true
	oldCtx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(oldCtx, defaultSchema, "t")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		h.exec("UPDATE t SET v=v+1 WHERE id=1")
	}
	h.exec("DELETE FROM t WHERE id=2")
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT id,v FROM t WHERE id<=3 ORDER BY id", "1|9007199254740996", "3|9007199254740993")
	h.expectRows("SELECT COUNT(*),SUM(id) FROM t", "256|33151")
	_, rows, err := newSnapshotDeltaScan(s, old, nil).next(oldCtx)
	if err != nil || len(rows) != 256 || rows[0]["v"] != "9007199254740993" || rows[1]["id"] != "2" {
		t.Fatalf("old snapshot changed: rows=%d err=%v", len(rows), err)
	}
	h.exec("TRUNCATE t")
	h.expectRows("SELECT COUNT(*) FROM t", "0")
	h.exec("INSERT INTO t VALUES (9,42,'new')")
	h.expectRows("SELECT id,v,s FROM t", "9|42|new")
}

// Reuse the existing correctness/fault oracles in explicit Flex mode. These
// tests deliberately exclude ALTER, adoption and other stopgap exclusions.
func TestSnapshotFlexContracts(t *testing.T) {
	t.Setenv("REDIS_SNAPSHOT_FLEX", "true")
	for _, tc := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"stream_generation", TestSnapshotStreamRetainsGeneration},
		{"arrow_atomicity", TestSnapshotArrowIngestAtomicity},
		{"arrow_modes", TestSnapshotArrowIngestModes},
		{"index_images", TestSnapshotIndexNotificationBatchesPreserveValues},
		{"index_unknown_and_cancel", TestSnapshotIndexNotificationUnknownReplyAndCancellation},
		{"index_damaged", TestSnapshotIndexNotificationRejectsDamagedBatch},
		{"compaction", TestSnapshotCompactionBuildsIndexFromDirectory},
		{"compaction_lost_reply", TestSnapshotCompactionAtomicErrorDoesNotRewind},
		{"publication_lost_reply", TestSnapshotLostPublicationReply},
		{"independent_writers", TestSnapshotIndependentPublicationMergesCollidingBuckets},
		{"source_conflict", TestSnapshotIndependentPublicationReadSourceConflicts},
		{"expiry_and_cancel", TestSnapshotExpiredReaderAndCancellation},
	} {
		t.Run(tc.name, tc.run)
	}
}

func TestSnapshotFlexPublicationProofFailures(t *testing.T) {
	for _, mode := range []string{"permission", "transport", "warning"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
			h.exec("CREATE TABLE t(v INTEGER)")
			h.exec("INSERT INTO t VALUES(1)")
			s := snapshotTestStore(t, raw)
			s.flex = true
			work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=2")
			observer := &snapshotIndexNotifyObserver{proofDenied: mode == "permission", proofTransport: mode == "transport", proofWarning: mode == "warning"}
			scans := &snapshotFullIndexScanCounter{}
			s.client.AddHook(observer)
			s.client.AddHook(scans)
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			err := s.publishSnapshot(work, pin, id)
			if err == nil || scans.full.Load() != 0 || observer.proofCalls.Load() != 1 {
				t.Fatal("failed proof published or invoked initial scan/replay", err, scans.full.Load(), observer.proofCalls.Load())
			}
			if mode == "permission" && !isStatus(err, adbc.StatusUnauthorized) {
				t.Fatal("wrong permission status", err)
			}
			if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
				t.Fatal("failed proof changed current root")
			}
			h.expectRows("SELECT v FROM t", "1")
		})
	}
}

func TestSnapshotFlexRowCounterIsolationAndLimit(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	s := snapshotTestStore(t, raw)
	s.flex = true
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	h.exec("INSERT INTO t VALUES(2)")
	if n, err := s.redis(old).Get(old, seqKey(defaultSchema, "t")).Int64(); err != nil || n != 1 {
		t.Fatal("insert mutated pinned row counter", n, err)
	}
	work, p, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=v WHERE v=1")
	defer s.abortSnapshot(p, id)
	meta, err := s.getTable(work, defaultSchema, "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.redis(work).Set(work, seqKey(defaultSchema, "t"), int64(exactDoubleLimit-1), 0).Err(); err != nil {
		t.Fatal(err)
	}
	row := [][]Value{{{T: typeInt32, I: 3}}}
	a, err := s.allocRows(work, meta, row)
	if err != nil || a.first != exactDoubleLimit {
		t.Fatal("exact boundary allocation", a, err)
	}
	if _, err := s.allocRows(work, meta, row); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal("overflow allocation was admitted", err)
	}
	if n, err := s.redis(work).Get(work, seqKey(defaultSchema, "t")).Int64(); err != nil || n != exactDoubleLimit {
		t.Fatal("refused allocation advanced sequence", n, err)
	}
	h.expectRows("SELECT v FROM t ORDER BY v", "1", "2")
}

func TestSnapshotFlexCompactionMissingIndexRecovery(t *testing.T) {
	t.Setenv("REDIS_SNAPSHOT_FLEX", "true")
	for _, phase := range []string{"uncheckpointed", "index"} {
		t.Run(phase, func(t *testing.T) {
			h, raw, s, job, _ := compactionAtomicFixture(t)
			observer := &compactionDirectoryIndexObserver{}
			s.client.AddHook(observer)
			if phase == "uncheckpointed" {
				wctx := s.compactionWriteContext(h.ctx, job)
				if err := s.redis(wctx).HSet(wctx, job.Prefix+"1", rowIDField, "1", "id", "1", "v", "4").Err(); err != nil {
					t.Fatal(err)
				}
				if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
					t.Fatal(err)
				}
			} else {
				for job.Phase == "copy" {
					if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
						t.Fatal(err)
					}
				}
				index := snapshotIndexPrefix(job.Data) + "compacted"
				if err := raw.Do(h.ctx, "FT.DROPINDEX", index).Err(); err != nil {
					t.Fatal(err)
				}
				if _, err := s.indexSnapshotCompaction(h.ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			if current, err := s.readSnapshotCompaction(h.ctx); err != nil || current != nil {
				t.Fatal("unindexed private job was retained", err)
			}
			snapshotDrainCompactionTest(t, s)
			assertCompactionAtomicRows(t, h)
			if observer.scans.Load() != 0 {
				t.Fatal("Flex compaction invoked initial scan")
			}
			assertSnapshotDataReferences(t, h, s)
		})
	}
}

// Reject accidentally selected RAM-only execution paths on both RAM and Flex.
type snapshotFlexCommandGuard struct{}

func (*snapshotFlexCommandGuard) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotFlexCommandGuard) check(cmd goredis.Cmder) error {
	if fenced, ok := cmd.(snapshotFencedCommand); ok {
		cmd = fenced.Cmder
	}
	a := cmd.Args()
	if len(a) == 0 {
		return nil
	}
	name := strings.ToLower(fmt.Sprint(a[0]))
	if !strings.HasPrefix(name, "ft.") {
		return nil
	}
	if name == "ft.aggregate" || name == "ft.cursor" {
		return fmt.Errorf("Flex selected unsupported command %s", name)
	}
	skip := false
	for _, arg := range a[2:] {
		switch strings.ToUpper(fmt.Sprint(arg)) {
		case "SKIPINITIALSCAN":
			skip = true
		case "SORTABLE", "UNF", "SORTBY", "DD":
			return fmt.Errorf("Flex selected unsupported %s argument %s", name, arg)
		}
	}
	if name == "ft.create" && !skip {
		return fmt.Errorf("Flex attempted initial index scan")
	}
	return nil
}
func (h *snapshotFlexCommandGuard) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if err := h.check(cmd); err != nil {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}
func (h *snapshotFlexCommandGuard) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.check(cmd); err != nil {
				cmd.SetErr(err)
				return err
			}
		}
		return next(ctx, cmds)
	}
}

func TestSnapshotFlexPartitionsAndRekey(t *testing.T) {
	t.Setenv("REDIS_SNAPSHOT_FLEX", "true")
	h, _, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,65) AS g(i)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionIntPartitionCount, "4"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
		t.Fatal(err)
	}
	_, parts, _, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	release, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringPartitionSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE t SET v=42 WHERE id<=32")
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE t RENAME TO renamed")
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM renamed", "65|42|9007199254740993")
	h.exec("TRUNCATE renamed")
	h.exec("DROP TABLE renamed")
	worker, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(h.ctx)
	seen := map[int64]bool{}
	for _, part := range parts.PartitionIDs {
		r, err := worker.ReadPartition(h.ctx, part)
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			record := r.RecordBatch()
			ids := record.Column(0).(*array.Int64)
			vs := record.Column(1).(*array.Int64)
			for i := 0; i < ids.Len(); i++ {
				id := ids.Value(i)
				if id < 1 || id > 65 || seen[id] || vs.Value(i) != 9007199254740993 {
					t.Fatal("partition mixed generations or duplicated ID", id)
				}
				seen[id] = true
			}
		}
		err = r.Err()
		r.Release()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 65 {
		t.Fatal("incomplete partition fetch", len(seen))
	}
	h.exec("DROP PARTITION SNAPSHOT '" + release + "'")
}
