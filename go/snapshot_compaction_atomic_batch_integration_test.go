// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func compactionPhysicalCopyRows(cmd goredis.Cmder) int64 {
	if wrapped, ok := cmd.(snapshotFencedCommand); ok {
		cmd = wrapped.Cmder
	}
	args := cmd.Args()
	if len(args) > 2 && cmd.Name() == "eval" && strings.Contains(fmt.Sprint(args[1]), "snapshot atomic compaction copy batch") {
		var keys int
		fmt.Sscan(fmt.Sprint(args[2]), &keys)
		return int64(keys - 3)
	}
	if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "HSET") && snapshotRowPrefix(fmt.Sprint(args[1])) != "" {
		return 1
	}
	return -1
}

type compactionAtomicBatchObserver struct {
	denyResolution bool
	denied         atomic.Bool
	commands, rows atomic.Int64
	lose           bool
	lost           atomic.Bool
}

func (*compactionAtomicBatchObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *compactionAtomicBatchObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if h.denyResolution && h.lost.Load() && cmd.Name() == "hget" && len(args) == 3 && args[1] == snapshotCatalogKey && args[2] == "compaction" && h.denied.CompareAndSwap(false, true) {
			err := fmt.Errorf("compaction outcome probe unavailable")
			cmd.SetErr(err)
			return err
		}

		rows := compactionPhysicalCopyRows(cmd)
		if rows >= 0 {
			h.commands.Add(1)
			h.rows.Add(rows)
		}
		err := next(ctx, cmd)
		if err == nil && rows >= 0 && h.lose && h.lost.CompareAndSwap(false, true) {
			err = fmt.Errorf("lost successful compaction copy reply")
			cmd.SetErr(err)
		}
		return err
	}
}
func (h *compactionAtomicBatchObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		var copies int
		for _, cmd := range cmds {
			if rows := compactionPhysicalCopyRows(cmd); rows >= 0 {
				h.commands.Add(1)
				h.rows.Add(rows)
				copies++
			}
		}
		err := next(ctx, cmds)
		if err == nil && copies > 0 && h.lose && h.lost.CompareAndSwap(false, true) {
			err = fmt.Errorf("lost successful compaction copy reply")
			for _, cmd := range cmds {
				cmd.SetErr(err)
			}
		}
		return err
	}
}
func TestSnapshotCompactionAtomicCopyBatch(t *testing.T) {
	for _, lose := range []bool{false, true} {
		t.Run(fmt.Sprint(lose), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
			for i := 0; i < 3; i++ {
				h.exec("UPDATE t SET v=v+1 WHERE __rowid<=128")
			}
			s := snapshotTestStore(t, raw)
			token := snapshotID()
			if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
				t.Fatal("start", started, err)
			}
			job, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || job == nil {
				t.Fatal(err)
			}
			observer := &compactionAtomicBatchObserver{lose: lose}
			s.client.AddHook(observer)
			err = s.copySnapshotCompactionBatch(h.ctx, job)
			durable, readErr := s.readSnapshotCompaction(h.ctx)
			if err != nil {
				t.Fatalf("copy must resolve its committed outcome without replay: %v (durable checkpoint present=%v)", err, durable != nil && durable.After != "")
			}
			if readErr != nil || durable == nil || durable.After != fmt.Sprintf("%020d", 64) || job.After != durable.After {
				t.Fatal("rows and checkpoint did not commit together", readErr)
			}
			if observer.commands.Load() != 1 || observer.rows.Load() != 64 {
				t.Fatal("copy batch used per-row mutations or replayed rows", observer.commands.Load(), observer.rows.Load())
			}
			if n := raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val(); n != 64 {
				t.Fatal("copy output count", n)
			}
			if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, s)
			h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|9007199254740993|9007199254740996")
		})
	}
}

func compactionAtomicFixture(t *testing.T) (*sqlHarness, *goredis.Client, *store, *snapshotCompactionJob, string) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,257) AS g(i)")
	for i := 0; i < 3; i++ {
		h.exec("UPDATE t SET v=v+1 WHERE __rowid<=128")
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("compaction admission", started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.releaseSnapshotCompaction(h.ctx, token) })
	return h, raw, s, job, token
}
func TestSnapshotCompactionAtomicErrorDoesNotRewind(t *testing.T) {
	h, raw, s, job, token := compactionAtomicFixture(t)
	observer := &compactionAtomicBatchObserver{lose: true, denyResolution: true}
	s.client.AddHook(observer)
	if _, err := s.snapshotCompactionStep(h.ctx, token); err == nil {
		t.Fatal("unreadable outcome was guessed")
	}
	inspect := snapshotTestStore(t, raw)
	durable, err := inspect.readSnapshotCompaction(h.ctx)
	if err != nil || durable == nil || durable.After != fmt.Sprintf("%020d", 64) || durable.LastError == "" || durable.CopyPending != nil {
		t.Fatal("error logging overwrote committed checkpoint", err)
	}
	if observer.commands.Load() != 1 || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 64 {
		t.Fatal("copy was replayed or lost")
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, inspect)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|1|4")
}
func TestSnapshotCompactionAtomicFencesAdvancedSuccessor(t *testing.T) {
	h, raw, s, job, token := compactionAtomicFixture(t)
	if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	stale := *job
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	next := snapshotID()
	if _, err := s.snapshotCompactionStep(h.ctx, next); err != nil {
		t.Fatal(err)
	}
	if n := raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val(); n != 128 {
		t.Fatal("successor did not advance", n)
	}
	if err := s.copySnapshotCompactionBatch(h.ctx, &stale); err == nil {
		t.Fatal("stale owner reported success from successor checkpoint")
	}
	if n := raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val(); n != 128 {
		t.Fatal("stale owner changed output", n)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, next); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|1|4")
}
func TestSnapshotCompactionAtomicPreflight(t *testing.T) {
	for _, mode := range []string{"wrong_type", "unregistered_hash", "row_id", "duplicate_field", "tombstone", "prefix_alias", "checkpoint", "duplicate_member"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, s, job, _ := compactionAtomicFixture(t)
			scan := newSnapshotDeltaScan(s, job.Source, nil)
			scan.pageRows = snapshotCompactionBatchRows
			keys, rows, err := scan.next(s.compactionWriteContext(h.ctx, job))
			if err != nil {
				t.Fatal(err)
			}
			images := make([]snapshotInsertRow, 0, len(rows))
			for i, row := range rows {
				id := strings.TrimPrefix(keys[i], job.Source.prefix())
				fields := []any{rowIDField, id}
				for field, value := range row {
					if field != rowIDField {
						fields = append(fields, field, value)
					}
				}
				key := job.Prefix + id
				images = append(images, snapshotInsertRow{key: key, member: snapshotDirectoryMember(key), fields: fields})
			}
			second := images[1].key
			after := scan.last
			switch mode {
			case "wrong_type":
				if err := raw.Set(h.ctx, second, "sentinel", 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "unregistered_hash":
				if err := raw.HSet(h.ctx, second, "sentinel", "original").Err(); err != nil {
					t.Fatal(err)
				}
			case "row_id":
				images[1].fields[1] = "999"
			case "duplicate_field":
				images[1].fields = append(images[1].fields, rowIDField, "2")
			case "tombstone":
				images[1].fields = append(images[1].fields, snapshotDeletedField, "1")
			case "prefix_alias":
				images[1].key = snapshotRows(job.Data, "other", snapshotAccessID(job.Prefix)) + "2"
			case "checkpoint":
				after = "00000000000000000000"
			case "duplicate_member":
				images[1].member = images[0].member
			}
			if err := s.commitSnapshotCompactionCopy(h.ctx, job, after, images); err == nil {
				t.Fatal("invalid batch accepted")
			}
			durable, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || durable.After != "" || durable.CopyPending != nil {
				t.Fatal("preflight mutated checkpoint", err)
			}
			if raw.Exists(h.ctx, images[0].key).Val() != 0 || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 0 {
				t.Fatal("preflight partially wrote earlier rows")
			}
			if mode == "wrong_type" && raw.Get(h.ctx, second).Val() != "sentinel" {
				t.Fatal("unregistered object changed")
			}
			if mode == "unregistered_hash" && raw.HGet(h.ctx, second, "sentinel").Val() != "original" {
				t.Fatal("unregistered HASH changed")
			}
		})
	}
}
func TestSnapshotCompactionAtomicAllDeletedPage(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,130) AS g(i)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	h.exec("DELETE FROM t WHERE id<=64")
	h.exec("UPDATE t SET v=2")
	h.exec("UPDATE t SET v=CASE WHEN id=66 THEN 3 WHEN id=65 THEN 2 ELSE 1 END")
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal(started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	observer := &compactionAtomicBatchObserver{lose: true}
	s.client.AddHook(observer)
	if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	if job.After != fmt.Sprintf("%020d", 64) || job.CopyBatch == nil || job.CopyBatch.Rows != 0 || observer.commands.Load() != 1 || observer.rows.Load() != 0 {
		t.Fatal("deleted page did not atomically advance without images")
	}
	if n := raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val(); n != 0 {
		t.Fatal("tombstones copied as live rows", n)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "66|1|3")
	result := snapshotExecuteTest(t, s, old, "SELECT COUNT(*),MIN(v),MAX(v) FROM t")
	if len(result.rows) != 1 || result.rows[0][0].I != 130 || result.rows[0][1].I != 1 || result.rows[0][2].I != 1 {
		t.Fatal("old reader changed", result.rows)
	}
	// The physical source IDs, including the deleted prefix, remain exact.
	if n, err := strconv.ParseInt(job.After, 10, 64); err != nil || n != 64 {
		t.Fatal("checkpoint identity changed")
	}
}

type compactionPartialCopyHook struct{ fired atomic.Bool }

func (*compactionPartialCopyHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*compactionPartialCopyHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *compactionPartialCopyHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if len(args) > 1 && cmd.Name() == "eval" && args[1] == snapshotCompactionCopyLua && h.fired.CompareAndSwap(false, true) {
			args[1] = strings.Replace(snapshotCompactionCopyLua, "for i,fields in ipairs(rows) do redis.call('HSET',KEYS[i+3],unpack(fields)) end", "for i,fields in ipairs(rows) do redis.call('HSET',KEYS[i+3],unpack(fields));if i==1 then return redis.error_reply('injected incomplete compaction copy') end end", 1)
			if args[1] == snapshotCompactionCopyLua {
				return fmt.Errorf("partial-copy injection did not match")
			}
		}
		return next(ctx, cmd)
	}
}
func TestSnapshotCompactionAtomicIncompleteCopyResumes(t *testing.T) {
	h, raw, s, job, token := compactionAtomicFixture(t)
	hook := &compactionPartialCopyHook{}
	s.client.AddHook(hook)
	if _, err := s.snapshotCompactionStep(h.ctx, token); err == nil {
		t.Fatal("incomplete batch reported completion")
	}
	durable, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || durable.After != "" || durable.CopyPending == nil || durable.CopyPending.After != fmt.Sprintf("%020d", 64) {
		t.Fatal("partial mutation lost incomplete marker", err)
	}
	if raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 64 || raw.Exists(h.ctx, job.Prefix+"1").Val() != 1 || raw.Exists(h.ctx, job.Prefix+"2").Val() != 0 {
		t.Fatal("injection did not leave registered incomplete output")
	}
	// Omitting the pending marker in a caller-supplied checkpoint cannot bypass
	// the durable incomplete-batch fence and begin indexing partial output.
	for _, phase := range []string{"copy", "index"} {
		premature := *durable
		premature.Phase, premature.CopyPending = phase, nil
		if err := s.checkpointSnapshotCompaction(h.ctx, &premature); err == nil {
			t.Fatalf("checkpoint cleared incomplete copy in phase %s", phase)
		}
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotReapTest(s, h.ctx)
	if raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 64 {
		t.Fatal("incomplete durable output reclaimed")
	}
	resumed := snapshotTestStore(t, raw)
	snapshotDrainCompactionTest(t, resumed)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|1|4")
}
func TestSnapshotCompactionAtomicCheckpointCannotRewind(t *testing.T) {
	h, raw, s, job, _ := compactionAtomicFixture(t)
	stale := *job
	if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := s.checkpointSnapshotCompaction(h.ctx, &stale); err == nil {
		t.Fatal("stale metadata checkpoint rewound atomic copy progress")
	}
	durable, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || durable.After != fmt.Sprintf("%020d", 64) || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 64 {
		t.Fatal("checkpoint or images changed", err)
	}
}
