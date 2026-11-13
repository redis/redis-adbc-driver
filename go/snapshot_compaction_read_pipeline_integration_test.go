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
	serial              bool
	singles             int
}

func (*compactionReadPipelineObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *compactionReadPipelineObserver) directory(ctx context.Context, cmd goredis.Cmder) bool {
	a := cmd.Args()
	return ctx.Value(compactionReadObservation{}) == true && len(a) > 1 && h.sources[fmt.Sprint(a[1])] && (cmd.Name() == "zrange" || cmd.Name() == "zrangebylex" || cmd.Name() == "zlexcount")
}
func (h *compactionReadPipelineObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if h.directory(ctx, cmd) {
			if h.serial {
				h.singles++
				return next(ctx, cmd)
			}
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
			if h.serial {
				return errors.New("small compaction must retain serial directory reads")
			}
			h.commands += len(reads)
			if len(reads) > snapshotMaxSegments {
				return errors.New("unbounded directory read pipeline")
			}
		}
		err := next(ctx, cmds)
		var target goredis.Cmder
		for _, cmd := range reads {
			if strings.HasPrefix(h.failure, "rank_") == (cmd.Name() == "zlexcount") {
				target = cmd
				break
			}
		}
		if err == nil && target != nil && h.failure != "" && !h.failed {
			h.failed = true
			injected := errors.New("injected compaction directory read failure")
			if strings.HasSuffix(h.failure, "command") {
				target.SetErr(injected)
				return nil
			}
			return injected
		}
		return err
	}
}

func TestSnapshotCompactionSmallReadsStaySerial(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
	for i := range 3 {
		h.expectRows("UPDATE t SET v=v+1 WHERE id=1 RETURNING v", fmt.Sprint(int64(9007199254740994)+int64(i)))
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("start", started, err)
	}
	defer s.releaseSnapshotCompaction(h.ctx, token)
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.SourceStart != 1 {
		t.Fatal("small selective job", err)
	}
	observer := &compactionReadPipelineObserver{sources: map[string]bool{}, serial: true}
	for _, seg := range job.Source.snapshotSegments() {
		observer.sources[snapshotMembers(snapshotDataID(seg.Prefix))] = true
	}
	s.client.AddHook(observer)
	ctx := context.WithValue(h.ctx, compactionReadObservation{}, true)
	for range 2 {
		if err := s.copySnapshotCompactionBatch(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if observer.pipelines != 0 || observer.singles != 3*len(observer.sources) || job.Phase != "index" {
		t.Fatal("small job changed its serial reads", observer.pipelines, observer.singles, job.Phase)
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
		want := int64(9007199254740993)
		if i == 0 {
			want += 3
		}
		if row != fmt.Sprintf("%d|%d", i+1, want) {
			t.Fatal("incorrect small-job row", i+1)
		}
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
				wantPipelines := (2*len(observer.sources) + snapshotMaxSegments - 1) / snapshotMaxSegments
				if observer.pipelines != wantPipelines || observer.commands != 2*len(observer.sources) {
					t.Fatal("expected combined bounded directory reads", observer.pipelines, observer.commands)
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

func TestSnapshotCompactionPrefetchBoundsAndSparseTails(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	for _, sources := range []int{1, 4, 5, snapshotMaxSegments} {
		t.Run(fmt.Sprint(sources), func(t *testing.T) {
			var original []snapshotDeltaSource
			observer := &compactionReadPipelineObserver{sources: map[string]bool{}}
			var members []string
			for row := range 129 {
				members = append(members, formatSnapshotIndexMember(9007199254740993+int64(row)*3))
			}
			for range sources {
				data := snapshotID()
				directory := snapshotMembers(data)
				observer.sources[directory] = true
				values := make([]goredis.Z, len(members))
				for i, member := range members {
					values[i] = goredis.Z{Score: 0, Member: member}
				}
				if err := raw.ZAdd(h.ctx, directory, values...).Err(); err != nil {
					t.Fatal(err)
				}
				original = append(original, snapshotDeltaSource{segment: snapshotSegment{Prefix: snapshotRows(data), Rows: int64(len(members))}})
			}
			s.client.AddHook(observer)
			ctx := context.WithValue(h.ctx, compactionReadObservation{}, true)
			for _, position := range []int{-1, 63, 127} {
				after := ""
				commandsPerSource := 1
				if position >= 0 {
					after, commandsPerSource = members[position], 2
				}
				scan := &snapshotDeltaScan{sources: append([]snapshotDeltaSource(nil), original...)}
				observer.pipelines, observer.commands = 0, 0
				if err := s.prefetchSnapshotCompactionScan(ctx, ctx, scan, after); err != nil {
					t.Fatal(err)
				}
				end := min(position+1+snapshotCompactionBatchRows, len(members))
				want := members[position+1 : end]
				for _, src := range scan.sources {
					if fmt.Sprint(src.ids) != fmt.Sprint(want) || src.offset != int64(end) || src.done != (len(want) < snapshotCompactionBatchRows) {
						t.Fatal("sparse exact-ID page or tail proof changed", position, src.offset, len(src.ids), src.done)
					}
				}
				commands := commandsPerSource * sources
				wantPipelines := (commands + snapshotMaxSegments - 1) / snapshotMaxSegments
				if observer.commands != commands || observer.pipelines != wantPipelines {
					t.Fatal("directory pipeline bound/round trips changed", observer.commands, observer.pipelines, wantPipelines)
				}
			}
		})
	}
}

func TestSnapshotCompactionPrefetchRejectsChangedDirectory(t *testing.T) {
	for _, corruption := range []string{"missing", "truncated_tail", "malformed"} {
		t.Run(corruption, func(t *testing.T) {
			h, raw, s, job, token := compactionAtomicFixture(t)
			if err := s.copySnapshotCompactionBatch(h.ctx, job); err != nil {
				t.Fatal(err)
			}
			before := job.After
			directory := snapshotMembers(snapshotDataID(job.Source.snapshotSegments()[0].Prefix))
			original, err := raw.ZRange(h.ctx, directory, 0, -1).Result()
			if err != nil {
				t.Fatal(err)
			}
			switch corruption {
			case "missing":
				err = raw.Del(h.ctx, directory).Err()
			case "truncated_tail":
				err = raw.ZRemRangeByLex(h.ctx, directory, "["+formatSnapshotIndexMember(80), "+").Err()
			case "malformed":
				bad := formatSnapshotIndexMember(65)[:19] + "x"
				if err = raw.ZRem(h.ctx, directory, formatSnapshotIndexMember(65)).Err(); err == nil {
					err = raw.ZAdd(h.ctx, directory, goredis.Z{Member: bad}).Err()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.copySnapshotCompactionBatch(h.ctx, job); err == nil {
				t.Fatal("changed source directory accepted")
			}
			durable, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || durable == nil || durable.After != before || durable.CopyPending != nil || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 64 {
				t.Fatal("invalid read advanced a checkpoint or emitted rows", err)
			}
			// Restore only the deliberately corrupted test directory, then prove
			// that a new owner resumes from the same acknowledged checkpoint.
			if err := raw.Del(h.ctx, directory).Err(); err != nil {
				t.Fatal(err)
			}
			values := make([]goredis.Z, len(original))
			for i, member := range original {
				values[i] = goredis.Z{Member: member}
			}
			if err := raw.ZAdd(h.ctx, directory, values...).Err(); err != nil {
				t.Fatal(err)
			}
			if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, s)
			rows, _ := h.query("SELECT id,v FROM t ORDER BY id")
			if len(rows) != 257 {
				t.Fatal("incomplete recovered result", len(rows))
			}
			for i, row := range rows {
				v := 1
				if i < 128 {
					v = 4
				}
				if row != fmt.Sprintf("%d|%d", i+1, v) {
					t.Fatal("incorrect recovered row", i+1)
				}
			}
		})
	}
}
