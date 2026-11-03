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

type snapshotIndexBuildCounter struct {
	snapshotHook
	creates atomic.Int64
}

func (h *snapshotIndexBuildCounter) record(cmd goredis.Cmder) {
	args := cmd.Args()
	if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "FT.CREATE") {
		h.creates.Add(1)
	}
}
func (h *snapshotIndexBuildCounter) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { h.record(cmd); return next(ctx, cmd) }
}
func (h *snapshotIndexBuildCounter) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			h.record(cmd)
		}
		return next(ctx, cmds)
	}
}
func TestSnapshotBuildsOnlySealedDMLIndex(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,100) AS g(i)")
	s := snapshotTestStore(t, raw)
	counter := &snapshotIndexBuildCounter{}
	s.client.AddHook(counter)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t SET v=999 WHERE id=1")
	if n := counter.creates.Load(); n != 1 {
		t.Fatalf("one-row UPDATE built %d indexes; require only the sealed output index", n)
	}
	h.expectRows("SELECT id,v FROM t WHERE id=1", "1|999")
	h.expectRows("SELECT COUNT(*),SUM(v) FROM t", "100|6048")
}

func TestSnapshotBuildsOnlySealedDDLAndIngestIndexes(t *testing.T) {
	for _, sql := range []string{
		"INSERT INTO t VALUES(101,101)",
		"DELETE FROM t WHERE id=1",
		"ALTER TABLE t ADD COLUMN label VARCHAR DEFAULT 'x'",
		"TRUNCATE t",
		"CREATE TABLE u AS SELECT * FROM t",
		"CREATE TABLE empty(v INTEGER)",
	} {
		t.Run(sql, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
			h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,100) AS g(i)")
			s := snapshotTestStore(t, raw)
			counter := &snapshotIndexBuildCounter{}
			s.client.AddHook(counter)
			snapshotExecuteTest(t, s, h.ctx, sql)
			if n := counter.creates.Load(); n != 1 {
				t.Fatalf("%s built %d indexes; require one final index", sql, n)
			}
		})
	}
}

func TestSnapshotNoopDMLDoesNotPublishOrBuildIndexes(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t VALUES(1,10)")
	s := snapshotTestStore(t, raw)
	counter := &snapshotIndexBuildCounter{}
	s.client.AddHook(counter)
	for _, sql := range []string{"UPDATE t SET v=999 WHERE false RETURNING v", "DELETE FROM t WHERE id=999 RETURNING id"} {
		before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
		res := snapshotExecuteTest(t, s, h.ctx, sql)
		if res.affected != 0 || len(res.rows) != 0 {
			t.Fatalf("no-op DML returned rows or changes: %v", res)
		}
		if n := counter.creates.Load(); n != 0 {
			t.Fatalf("no-op DML built %d indexes", n)
		}
		if after := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); after != before {
			t.Fatal("no-op DML published an empty generation")
		}
	}
	h.expectRows("SELECT id,v FROM t", "1|10")
}

func TestSnapshotDeferredBaselineKeepsPrivateTombstones(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.prepareSnapshotTable(wctx, "public", "t"); err != nil {
		t.Fatal(err)
	}
	m := snapshotTableTest(t, s, wctx, "t")
	if _, err := s.insertRows(wctx, m, [][]Value{{intValue(typeInt32, 7)}}); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, wctx, "DELETE FROM t WHERE v=7")
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*) FROM t", "0")
	h.expectRows("SELECT v FROM t")
}
