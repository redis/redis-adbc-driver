// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"github.com/apache/arrow-go/v18/arrow/array"
	"testing"
	"time"
)

func TestManagedSingleGenerationDirectoryScan(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE managed_fast (id BIGINT, v BIGINT)")
	h.exec("INSERT INTO managed_fast VALUES(1,9007199254740993),(2,NULL)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	e := &executor{store: s, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
	parsed, err := ParseScript("SELECT id,v FROM managed_fast")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.meta.SnapshotOverlay {
		t.Fatal("fixture is not a single generation")
	}
	sc, err := e.prepareScan(ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	it, err := e.openScan(ctx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	if it.delta == nil || it.cur != nil || !it.hashFetch {
		t.Fatal("single-generation scan still opens a Search data cursor")
	}
	if plan.meta.SnapshotOverlay {
		t.Fatal("optimization changed persisted/planning metadata")
	}
	keys, rows, err := it.nextPage(ctx)
	if err != nil || len(keys) != 2 || len(rows) != 2 {
		t.Fatalf("keys=%v rows=%v err=%v", keys, rows, err)
	}
	if rows[0]["v"].I != 9007199254740993 || !rows[1]["v"].Null {
		t.Fatal("lost exact integer/NULL")
	}
	// Hold an old generation through another statement's publication.
	h.exec("UPDATE managed_fast SET v=7 WHERE id=1")
	old := snapshotExecuteTest(t, s, ctx, "SELECT v FROM managed_fast WHERE id=1")
	if len(old.rows) != 1 || old.rows[0][0].I != 9007199254740993 {
		t.Fatal("pinned input changed")
	}
	h.expectRows("SELECT id,v FROM managed_fast", "1|7", "2|NULL")
}

func TestManagedDirectoryPagesRetainTail(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE page_tail (id BIGINT,v BIGINT)")
	h.exec("INSERT INTO page_tail SELECT i,CASE WHEN i%17=0 THEN NULL ELSE 9007199254740993+i END FROM generate_series(1,4097) AS g(i)")
	reader, _ := h.openStream(h.ctx, h.conn, "SELECT id,v FROM page_tail", nil, map[string]string{OptionIntStreamBatchRows: "17"})
	defer reader.Release()
	seen := int64(0)
	for reader.Next() {
		rec := reader.RecordBatch()
		ids, values := rec.Column(0).(*array.Int64), rec.Column(1).(*array.Int64)
		if rec.NumRows() > 17 {
			t.Fatal("Arrow batch ceiling changed")
		}
		for i := 0; i < int(rec.NumRows()); i++ {
			seen++
			if ids.Value(i) != seen || values.IsNull(i) != (seen%17 == 0) || !values.IsNull(i) && values.Value(i) != 9007199254740993+seen {
				t.Fatalf("tail/order/exact value differs at row %d", seen)
			}
		}
	}
	if err := reader.Err(); err != nil || seen != 4097 {
		t.Fatalf("rows=%d err=%v", seen, err)
	}
}
