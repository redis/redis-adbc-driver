// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type selectiveCompactionObserver struct {
	snapshotCompactionBatchTracker
	base      string
	baseReads int
}

func (o *selectiveCompactionObserver) observe(cmd goredis.Cmder) {
	if wrapped, ok := cmd.(snapshotFencedCommand); ok {
		cmd = wrapped.Cmder
	}
	a := cmd.Args()
	if len(a) > 1 && strings.HasPrefix(fmt.Sprint(a[1]), o.base) {
		switch cmd.Name() {
		case "hget", "hmget", "hgetall":
			o.baseReads++
		}
	}
}
func (o *selectiveCompactionObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	observed := o.snapshotCompactionBatchTracker.ProcessHook(next)
	return func(ctx context.Context, cmd goredis.Cmder) error { o.observe(cmd); return observed(ctx, cmd) }
}
func (o *selectiveCompactionObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	observed := o.snapshotCompactionBatchTracker.ProcessPipelineHook(next)
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			o.observe(cmd)
		}
		return observed(ctx, cmds)
	}
}
func TestSnapshotCompactionPreservesLargeBase(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprint(deleted), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v BIGINT,payload TEXT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993,CASE WHEN i%5=0 THEN NULL ELSE 'row-'||i END FROM generate_series(1,20000) AS g(i)")
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			base := snapshotTableTest(t, s, ctx, "t").prefix()
			pin.release()
			h.exec("UPDATE t SET v=v+1 WHERE id=1")
			h.exec("UPDATE t SET v=v+1 WHERE id=1")
			if deleted {
				h.exec("DELETE FROM t WHERE id=1")
			} else {
				h.exec("UPDATE t SET v=v+1 WHERE id=1")
			}
			observer := &selectiveCompactionObserver{base: base}
			s.client.AddHook(observer)
			snapshotDrainCompactionTest(t, s)
			if observer.totalRows > 1 || observer.baseReads > snapshotCompactionBatchRows {
				t.Fatalf("small overlays rewrote/read the unchanged base: images=%d base_reads=%d", observer.totalRows, observer.baseReads)
			}
			ctx, pin, err = s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			current := snapshotTableTest(t, s, ctx, "t")
			segments := current.snapshotSegments()
			pin.release()
			if len(segments) != 2 || segments[0].Prefix != base || segments[1].Rows != 1 {
				t.Fatal("small merge did not retain the base and one exact overlay")
			}
			if deleted && raw.HGet(h.ctx, current.prefix()+"1", snapshotDeletedField).Val() != "1" {
				t.Fatal("partial merge discarded a tombstone and can resurrect the base")
			}
			rows, _ := h.query("SELECT id,v,payload FROM t ORDER BY id")
			count := 20000
			if deleted {
				count--
			}
			if len(rows) != count {
				t.Fatal("incomplete final fetch", len(rows))
			}
			for i, row := range rows {
				id := i + 1
				if deleted {
					id++
				}
				v := int64(9007199254740993)
				if id == 1 {
					v += 3
				}
				payload := fmt.Sprintf("row-%d", id)
				if id%5 == 0 {
					payload = "NULL"
				}
				if row != fmt.Sprintf("%d|%d|%s", id, v, payload) {
					t.Fatalf("wrong final row %d", id)
				}
			}
		})
	}
}

func TestSnapshotSelectiveCompactionGraftsNewerTail(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
	for range 3 {
		h.exec("UPDATE t SET v=v+1 WHERE id=1")
	}
	s := snapshotTestStore(t, raw)
	old, oldPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer oldPin.release()
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal(started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.SourceStart != 1 {
		t.Fatal("not a selective job", err)
	}
	oldSource := job.Source.prefix()
	// New tail publications must survive the selected middle range's install.
	h.exec("DELETE FROM t WHERE id=1")
	h.exec("INSERT INTO t VALUES(258,9007199254741093)")
	snapshotCompleteJobTest(t, s, token)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := snapshotTableTest(t, s, ctx, "t")
	segments := current.snapshotSegments()
	pin.release()
	if len(segments) != 4 || segments[1].Prefix != job.Prefix || segments[0].Rows != 257 {
		t.Fatal("selective install discarded the preserved base or newer tail")
	}
	rows, _ := h.query("SELECT id,v FROM t ORDER BY id")
	if len(rows) != 257 {
		t.Fatal("incomplete newer-tail fetch", len(rows))
	}
	for i, row := range rows {
		id := i + 2
		v := int64(9007199254740993)
		if id == 258 {
			v += 100
		}
		if row != fmt.Sprintf("%d|%d", id, v) {
			t.Fatalf("wrong newer-tail row %d", id)
		}
	}
	prior := snapshotExecuteTest(t, s, old, "SELECT COUNT(*),MAX(v) FROM t")
	if len(prior.rows) != 1 || prior.rows[0][0].I != 257 || prior.rows[0][1].I != 9007199254740996 {
		t.Fatal("selective merge changed a pinned source")
	}
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, oldSource+"1").Val() != 1 {
		t.Fatal("pinned selected source reclaimed")
	}
	oldPin.release()
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, oldSource+"1").Val() != 0 {
		t.Fatal("unreferenced selected source retained")
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(id),MAX(id),MAX(v) FROM t", "257|2|258|9007199254741093")
}

func TestSnapshotSelectiveCompactionRepairsIncompleteTombstoneBatch(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
	h.exec("UPDATE t SET v=v+1 WHERE id=1")
	h.exec("UPDATE t SET v=v+1 WHERE id=1")
	h.exec("DELETE FROM t WHERE id=2")
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal(started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.SourceStart != 1 {
		t.Fatal("not a selective job", err)
	}
	changed := *job
	changed.SourceStart = 0
	if err := s.checkpointSnapshotCompaction(h.ctx, &changed); err == nil {
		t.Fatal("checkpoint changed the immutable source range/tombstone policy")
	}
	s.client.AddHook(&compactionPartialCopyHook{})
	if _, err := s.snapshotCompactionStep(h.ctx, token); err == nil {
		t.Fatal("partial copy reported success")
	}
	durable, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || durable.CopyPending == nil || durable.After != "" || durable.SourceStart != 1 || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 2 {
		t.Fatal("partial selective output lost its marker or registered tombstone", err)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	resumed := snapshotTestStore(t, raw)
	snapshotDrainCompactionTest(t, resumed)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "256|9007199254740993|9007199254740995")
	h.expectRows("SELECT id FROM t WHERE id=2")
	ctx, pin, err := resumed.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := snapshotTableTest(t, resumed, ctx, "t")
	pin.release()
	if current.prefix() != job.Prefix || raw.HGet(h.ctx, job.Prefix+"2", snapshotDeletedField).Val() != "1" {
		t.Fatal("takeover replaced durable output or lost its tombstone")
	}
}

func TestSnapshotCompactionCheckpointKeepsSourceIdentity(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,257) AS g(i)")
	for range 3 {
		h.exec("UPDATE t SET v=v+1 WHERE id=1")
	}
	s := snapshotTestStore(t, raw)
	if started, err := s.startSnapshotCompaction(h.ctx, snapshotID()); err != nil || !started {
		t.Fatal(started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil {
		t.Fatal("missing compaction job", err)
	}
	for _, field := range []string{"references", "source", "queue_key", "queue_version", "queue_oid"} {
		changed := *job
		switch field {
		case "references":
			changed.References = append([]string(nil), job.References...)
			changed.References[0] = snapshotID()
		case "source":
			changed.Source = cloneMeta(job.Source)
			changed.Source.SnapshotLineage = snapshotID()
		case "queue_key":
			changed.QueueKey += "_other"
		case "queue_version":
			changed.QueueVersion = snapshotID()
		case "queue_oid":
			changed.QueueOID = "0"
		}
		if err := s.checkpointSnapshotCompaction(h.ctx, &changed); err == nil {
			t.Fatal("checkpoint changed immutable job identity", field)
		}
	}
	// A normal decoded/encoded checkpoint must still renew successfully.
	if err := s.checkpointSnapshotCompaction(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, job.Token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|1|4")
}
