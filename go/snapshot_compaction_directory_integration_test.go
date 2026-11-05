// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type compactionDirectoryObservation struct{}
type compactionDirectoryReads struct {
	snapshotHook
	sources map[string]bool
	max     atomic.Int64
}

func (h *compactionDirectoryReads) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if ctx.Value(compactionDirectoryObservation{}) == true && cmd.Name() == "zrange" && len(args) == 4 && h.sources[fmt.Sprint(args[1])] {
			first, e1 := strconv.ParseInt(fmt.Sprint(args[2]), 10, 64)
			last, e2 := strconv.ParseInt(fmt.Sprint(args[3]), 10, 64)
			if e1 == nil && e2 == nil {
				h.max.Store(max(h.max.Load(), last-first+1))
			}
		}
		return next(ctx, cmd)
	}
}

func TestSnapshotCompactionDirectoryReadsMatchCheckpointBatch(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,500) AS g(i)")
	h.exec("UPDATE t SET v=v+1 WHERE id=1")
	h.exec("UPDATE t SET v=v+1 WHERE id=1")
	h.exec("DELETE FROM t WHERE id=2")
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("compaction admission", started, err)
	}
	defer s.releaseSnapshotCompaction(h.ctx, token)
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil {
		t.Fatal("missing job", err)
	}
	observer := &compactionDirectoryReads{sources: map[string]bool{}}
	for _, source := range job.Source.snapshotSegments() {
		observer.sources[snapshotMembers(snapshotDataID(source.Prefix))] = true
	}
	s.client.AddHook(observer)
	ctx := context.WithValue(h.ctx, compactionDirectoryObservation{}, true)
	for job.Phase == "copy" {
		before := job.After
		if err := s.copySnapshotCompactionBatch(ctx, job); err != nil {
			t.Fatal(err)
		}
		if job.Phase == "copy" && job.After <= before {
			t.Fatal("copy checkpoint did not advance")
		}
	}
	if got := observer.max.Load(); got == 0 || got > snapshotCompactionBatchRows {
		t.Errorf("checkpoint batch requested %d directory entries per source, limit %d", got, snapshotCompactionBatchRows)
	}
	if got, err := raw.ZCard(h.ctx, snapshotMembers(job.Data)).Result(); err != nil || got != 499 {
		t.Fatal("copied visible directory", got, err)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "499|9007199254740993|9007199254740995")
	h.expectRows("SELECT id,v FROM t WHERE id<=3 ORDER BY id", "1|9007199254740995", "3|9007199254740993")
}
