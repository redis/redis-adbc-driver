// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotValidationBatchTracker struct {
	snapshotHook
	direct, batches, infos, directories atomic.Int64
	invalid                             atomic.Bool
}

func (h *snapshotValidationBatchTracker) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "ft.info" {
			h.direct.Add(1)
		}
		return next(ctx, cmd)
	}
}

func (h *snapshotValidationBatchTracker) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		infos, dirs := 0, 0
		for _, cmd := range cmds {
			switch cmd.Name() {
			case "ft.info":
				infos++
				if once, ok := cmd.(interface{ NoRetry() bool }); !ok || !once.NoRetry() {
					h.invalid.Store(true)
				}
			case "zcard":
				dirs++
			}
		}
		if infos != 0 {
			h.batches.Add(1)
			h.infos.Add(int64(infos))
			h.directories.Add(int64(dirs))
			if infos != dirs || infos > snapshotMaxSegments {
				h.invalid.Store(true)
			}
		}
		return next(ctx, cmds)
	}
}

func TestSnapshotValidationBatchesPublishedGenerations(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE batch_validation(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO batch_validation VALUES(1,9007199254740993),(2,7),(3,NULL)")
	h.exec("UPDATE batch_validation SET v=9007199254740995 WHERE id=1")
	h.exec("DELETE FROM batch_validation WHERE id=2")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	tracker := &snapshotValidationBatchTracker{}
	s.client.AddHook(tracker)
	m := snapshotTableTest(t, s, ctx, "batch_validation")
	if len(m.snapshotSegments()) != 3 {
		t.Fatalf("wanted three published sources, got %d", len(m.snapshotSegments()))
	}
	for range 4 {
		if _, err := s.getTable(ctx, defaultSchema, "batch_validation"); err != nil {
			t.Fatal(err)
		}
	}
	if tracker.direct.Load() != 0 || tracker.batches.Load() != 1 || tracker.infos.Load() != 3 || tracker.directories.Load() != 3 || tracker.invalid.Load() {
		t.Fatalf("invalid published validation batch: direct=%d batches=%d info=%d directories=%d invalid=%t",
			tracker.direct.Load(), tracker.batches.Load(), tracker.infos.Load(), tracker.directories.Load(), tracker.invalid.Load())
	}
	// Changed descriptors cannot reuse a successful proof for the same prefix.
	broken := *m
	wrong := *m.SnapshotRows + 1
	broken.SnapshotRows = &wrong
	if err := s.validateSnapshotTable(ctx, &broken); err == nil {
		t.Fatal("changed row cardinality reused a successful proof")
	}
	rows := valueRows(snapshotExecuteTest(t, s, ctx, "SELECT id,v FROM batch_validation ORDER BY id").rows)
	if fmt.Sprint(rows) != "[1|9007199254740995 3|NULL]" {
		t.Fatal("batching changed exact visible values", rows)
	}
}
