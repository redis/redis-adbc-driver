// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestManagedCandidatePagesBoundedAndComplete(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE candidate_page(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO candidate_page SELECT i,CASE WHEN i%17=0 THEN NULL ELSE 9007199254740993+i END FROM generate_series(1,8194) AS g(i)")
	guard := &snapshotCandidateReads{}
	hooked, impl := statisticsHookedTransport(t, h, guard)
	s := impl.store
	ctx, pin, err := s.acquireSnapshot(hooked)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	e := &executor{store: s, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
	query := "SELECT id,v FROM candidate_page WHERE id BETWEEN 2 AND 8193"
	parsed, err := ParseScript(query)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := e.prepareScan(ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	it, err := e.openScan(ctx, scan.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	if it.candidates == nil || !it.hashFetch || len(it.candidates.sources) != 1 {
		t.Fatal("fixture did not use single published candidate HASH fetch")
	}
	want := int(min(int64(wideFetchChunk), scan.caps[0]))
	if it.candidates.page != want {
		t.Fatalf("candidate page still repeats small reads: got %d, want bounded %d", it.candidates.page, want)
	}
	for _, end := range []int64{8193, 8194} {
		guard.search.Store(0)
		guard.rows.Store(0)
		records, err := followupFetch(hooked, h.conn, fmt.Sprintf("SELECT id,v FROM candidate_page WHERE id BETWEEN 2 AND %d", end))
		if err != nil {
			t.Fatal(err)
		}
		id := int64(1)
		for _, rec := range records {
			ids, values := rec.Column(0).(*array.Int64), rec.Column(1).(*array.Int64)
			for row := 0; row < int(rec.NumRows()); row++ {
				id++
				if ids.IsNull(row) || ids.Value(row) != id || values.IsNull(row) != (id%17 == 0) || !values.IsNull(row) && values.Value(row) != 9007199254740993+id {
					t.Fatal("incorrect candidate tail/value/NULL", id)
				}
			}
			rec.Release()
		}
		if id != end || guard.rows.Load() != end-1 {
			t.Fatal("candidate omission/repeated HASH fetch", id, guard.rows.Load())
		}
		// Check exact multiples (final empty seek) and a one-row partial tail.
		if got, max := guard.search.Load(), (end-1)/int64(want)+1; got > max {
			t.Fatalf("redundant candidate queries: %d, maximum %d", got, max)
		}
	}
}
