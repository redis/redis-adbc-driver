// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotBatchWire struct {
	sync.Mutex
	digests, bodies map[string]int
}

func (*snapshotBatchWire) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotBatchWire) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *snapshotBatchWire) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if len(a) > 1 {
			h.Lock()
			for _, s := range []struct{ body, digest string }{{snapshotWriteBatchLua, snapshotWriteBatchDigest}, {snapshotIndexNotifyLua, snapshotIndexNotifyDigest}, {snapshotCompactionCopyLua, snapshotCompactionCopyDigest}} {
				if cmd.Name() == "fcall" && a[1] == s.digest {
					h.digests[s.digest]++
				} else if cmd.Name() == "eval" && a[1] == s.body {
					h.bodies[s.digest]++
				}
			}
			h.Unlock()
		}
		return next(ctx, cmd)
	}
}

func TestSnapshotCompactionFunctionsSurviveScriptFlush(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
	for range 3 {
		h.exec("UPDATE t SET v=v+1")
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("compaction admission", started, err)
	}
	defer s.releaseSnapshotCompaction(h.ctx, token)
	observer := &snapshotBatchWire{digests: map[string]int{}, bodies: map[string]int{}}
	s.client.AddHook(observer)
	for trial := range 3 {
		if trial != 1 {
			if err := raw.ScriptFlush(h.ctx).Err(); err != nil {
				t.Fatal(err)
			}
		}
		observer.Lock()
		clear(observer.digests)
		clear(observer.bodies)
		observer.Unlock()
		// Observe one atomic batch per cache trial; maintenance steps may
		// intentionally group several independently fenced batches.
		job, err := s.readSnapshotCompaction(h.ctx)
		if err != nil || job == nil {
			t.Fatal("read job", err)
		}
		if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
			t.Fatal(err)
		}
		wantBody := 0
		observer.Lock()
		if observer.digests[snapshotCompactionCopyDigest] != 1 || observer.bodies[snapshotCompactionCopyDigest] != wantBody {
			t.Errorf("trial %d: digest attempts %d full bodies %d, want 1/%d", trial, observer.digests[snapshotCompactionCopyDigest], observer.bodies[snapshotCompactionCopyDigest], wantBody)
		}
		observer.Unlock()
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	rows, _ := h.query("SELECT id,v FROM t ORDER BY id")
	if len(rows) != 257 {
		t.Fatal("incomplete compaction result", len(rows))
	}
	for i, row := range rows {
		if row != fmt.Sprintf("%d|9007199254740996", i+1) {
			t.Fatal("incorrect compacted value", i+1)
		}
	}
}

func TestSnapshotBatchFunctionsSurviveScriptFlush(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	rec := followupRecord(3000)
	defer rec.Release()
	empty := rec.NewSlice(0, 0)
	err := followupIngest(h.ctx, h.conn, "batch_cache", adbc.OptionValueIngestModeCreate, empty)
	empty.Release()
	if err != nil {
		t.Fatal(err)
	}
	observer := &snapshotBatchWire{digests: map[string]int{}, bodies: map[string]int{}}
	ctx, _ := statisticsHookedTransport(t, h, observer)
	for trial := range 3 {
		if trial != 1 {
			if err := raw.ScriptFlush(ctx).Err(); err != nil {
				t.Fatal(err)
			}
		}
		observer.Lock()
		clear(observer.digests)
		clear(observer.bodies)
		observer.Unlock()
		part := rec.NewSlice(int64(trial*1000), int64((trial+1)*1000))
		err := followupIngest(ctx, h.conn, "batch_cache", adbc.OptionValueIngestModeAppend, part)
		part.Release()
		if err != nil {
			t.Fatal(err)
		}
		wantBody := 0
		observer.Lock()
		for _, digest := range []string{snapshotWriteBatchDigest, snapshotIndexNotifyDigest} {
			if observer.digests[digest] != 4 || observer.bodies[digest] != wantBody {
				t.Errorf("trial %d: digest attempts %d full bodies %d, want 4/%d", trial, observer.digests[digest], observer.bodies[digest], wantBody)
			}
		}
		observer.Unlock()
	}
	actual, err := followupFetch(ctx, h.conn, "SELECT * FROM batch_cache")
	if err != nil {
		t.Fatal(err)
	}
	followupValidate(t, actual, 1, 3000)
}
