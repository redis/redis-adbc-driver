// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type compactionReadObservation struct{}

type compactionReadPipelineObserver struct {
	sources             map[string]bool
	pipelines, commands int
	failure             string
	failed              bool
}

func (*compactionReadPipelineObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *compactionReadPipelineObserver) directory(ctx context.Context, cmd goredis.Cmder) bool {
	a := cmd.Args()
	return ctx.Value(compactionReadObservation{}) == true && len(a) > 1 && h.sources[fmt.Sprint(a[1])] && (cmd.Name() == "zrange" || cmd.Name() == "zlexcount")
}
func (h *compactionReadPipelineObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if h.directory(ctx, cmd) {
			err := errors.New("compaction directory reads require a bounded pipeline")
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}
func (h *compactionReadPipelineObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		var reads []goredis.Cmder
		for _, cmd := range cmds {
			if h.directory(ctx, cmd) {
				reads = append(reads, cmd)
			}
		}
		if len(reads) > 0 {
			h.pipelines++
			h.commands += len(reads)
			if len(reads) > snapshotMaxSegments {
				return errors.New("unbounded directory read pipeline")
			}
		}
		err := next(ctx, cmds)
		target := 1
		if strings.HasPrefix(h.failure, "page_") {
			target = 2
		}
		if err == nil && len(reads) > 0 && h.failure != "" && !h.failed && h.pipelines == target {
			h.failed = true
			injected := errors.New("injected compaction directory read failure")
			if strings.HasSuffix(h.failure, "command") {
				reads[0].SetErr(injected)
				return nil
			}
			return injected
		}
		return err
	}
}

func TestSnapshotCompactionReadPipelines(t *testing.T) {
	for _, failure := range []string{"", "rank_pipeline", "rank_command", "page_pipeline", "page_command"} {
		t.Run("failure_"+failure, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
			for range 3 {
				h.exec("UPDATE t SET v=v+1")
			}
			s := snapshotTestStore(t, raw)
			token := snapshotID()
			if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
				t.Fatal("start", started, err)
			}
			defer s.releaseSnapshotCompaction(h.ctx, token)
			job, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || job == nil {
				t.Fatal("job", err)
			}
			// Advance once to exercise resumed nonzero rank positioning as well
			// as bounded initial page reads in the observed next batch.
			if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
				t.Fatal(err)
			}
			before := job.After
			observer := &compactionReadPipelineObserver{sources: map[string]bool{}, failure: failure}
			for _, seg := range job.Source.snapshotSegments() {
				observer.sources[snapshotMembers(snapshotDataID(seg.Prefix))] = true
			}
			s.client.AddHook(observer)
			ctx := context.WithValue(h.ctx, compactionReadObservation{}, true)
			err = s.copySnapshotCompactionBatch(ctx, job)
			if failure != "" {
				if err == nil || !observer.failed {
					t.Fatal("read failure must stop copying", err)
				}
				durable, readErr := s.readSnapshotCompaction(h.ctx)
				if readErr != nil || durable == nil || durable.After != before || durable.CopyPending != nil {
					t.Fatal("failed read advanced durable copy", readErr)
				}
				if count, err := raw.ZCard(h.ctx, snapshotMembers(job.Data)).Result(); err != nil || count != 64 {
					t.Fatal("failed read emitted rows", count, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if observer.pipelines != 2 || observer.commands != 2*len(observer.sources) {
					t.Fatal("expected two bounded directory pipelines", observer.pipelines, observer.commands)
				}
			}
			if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, s)
			rows, _ := h.query("SELECT id,v FROM t ORDER BY id")
			if len(rows) != 257 {
				t.Fatal("incomplete final fetch", len(rows))
			}
			for i, row := range rows {
				if row != fmt.Sprintf("%d|9007199254740996", i+1) {
					t.Fatal("incorrect compacted row", i+1)
				}
			}
		})
	}
}
