// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// #654: a streamed SELECT … ORDER BY whose input exceeds the sort budget
// spills sorted runs to query-local files and streams their merge. It returns
// the in-memory sort's rows in the same order (ties in scan order, NULLs,
// NaN, DESC, OFFSET / LIMIT), keeps its buffer within the budget, merges in
// several passes when there are more runs than one merge opens, and removes
// its files whether the stream is read to the end or released early.
func TestSQLSortSpill(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_sort_spill"
	h.dropTables(table)
	t.Cleanup(func() { h.dropTables(table) })
	h.exec("CREATE TABLE " + table + " (id INTEGER, k INTEGER, s VARCHAR, d NUMERIC(12,2), f DOUBLE PRECISION, ts TIMESTAMP)")
	floats := []string{"'NaN'", "'Infinity'", "'-Infinity'", "-0.0", "0.0", "1.5", "-2.25", "NULL"}
	var vals []string
	for id := 1; id <= 3000; id++ {
		k := "NULL"
		if id%37 != 0 {
			k = fmt.Sprint((id * 7919) % 97) // many ties
		}
		s := "NULL"
		if id%11 != 0 {
			s = fmt.Sprintf("'%s-%c-ü%d'", strings.Repeat("x", id%13), 'a'+rune(id%26), id%50)
		}
		d := "NULL"
		if id%17 != 0 {
			d = fmt.Sprintf("%d.%02d", (id*31)%1000-500, id%100)
		}
		ts := fmt.Sprintf("TIMESTAMP '2026-%02d-%02d %02d:%02d:00'", 1+id%12, 1+id%28, id%24, id%60)
		vals = append(vals, fmt.Sprintf("(%d, %s, %s, %s, %s, %s)", id, k, s, d, floats[id%len(floats)], ts))
	}
	for start := 0; start < len(vals); start += 500 {
		h.exec("INSERT INTO " + table + " VALUES " + strings.Join(vals[start:min(start+500, len(vals))], ", "))
	}

	var mu sync.Mutex
	var dirs []string
	testHookSortSpillDir = func(dir string) {
		mu.Lock()
		dirs = append(dirs, dir)
		mu.Unlock()
	}
	t.Cleanup(func() { testHookSortSpillDir, testSortSpillBytes = nil, 0 })
	removed := func(what string) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		for _, dir := range dirs {
			if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s: spill directory %s remains (%v)", what, dir, err)
			}
		}
		dirs = nil
	}

	const budget = 16 << 10
	multiPass := false
	for _, q := range []string{
		"SELECT id, k, s, d, f, ts FROM " + table + " ORDER BY k NULLS FIRST",
		// Expressions keep these sorts out of a Search index sort.
		"SELECT id, s FROM " + table + " ORDER BY s || '' DESC NULLS LAST, d",
		"SELECT id, f, d * 2 FROM " + table + " ORDER BY f * 1, id DESC",
		// An expression in the select list keeps a LIMIT out of selectTopK.
		"SELECT id, ts, k + 1 FROM " + table + " ORDER BY ts + INTERVAL '1 day' DESC, k * 1 LIMIT 2000 OFFSET 17",
		"SELECT id, upper(s) FROM " + table + " WHERE k > 3 ORDER BY lower(s), id",
		"SELECT d, id FROM " + table + " ORDER BY d + 0 DESC NULLS FIRST, k, id",
	} {
		testSortSpillBytes = 0
		before := sortSpillStats.runs.Load()
		want, _ := h.query(q)
		if sortSpillStats.runs.Load() != before {
			t.Fatalf("%s: spilled below the default budget", q)
		}
		testSortSpillBytes = budget
		runs, merged := sortSpillStats.runs.Load(), sortSpillStats.mergedRuns.Load()
		sortSpillStats.maxBuffered.Store(0)
		got, _ := h.query(q)
		runs, merged = sortSpillStats.runs.Load()-runs, sortSpillStats.mergedRuns.Load()-merged
		if runs < 2 {
			t.Fatalf("%s: %d runs, want a spilled sort", q, runs)
		}
		multiPass = multiPass || merged > 0
		if !slices.Equal(got, want) {
			for i := range min(len(got), len(want)) {
				if got[i] != want[i] {
					t.Fatalf("%s: row %d is %q, in memory %q (%d / %d rows)", q, i, got[i], want[i], len(got), len(want))
				}
			}
			t.Fatalf("%s: %d rows, in memory %d", q, len(got), len(want))
		}
		// A run is written once the buffer reaches the budget, so it holds at
		// most the budget plus one row.
		if max := sortSpillStats.maxBuffered.Load(); max == 0 || max > budget+4096 {
			t.Fatalf("%s: largest spilled buffer %d bytes for a %d-byte budget", q, max, budget)
		}
		removed(q)
	}
	if !multiPass {
		t.Fatal("no sort had more runs than one merge opens")
	}

	// Released after its first batch: the merge's files go with the stream.
	testSortSpillBytes = budget
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionIntStreamBatchRows, "64"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT id, s FROM "+table+" ORDER BY lower(s), id"); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rdr.Next() || rdr.RecordBatch().NumRows() != 64 {
		t.Fatal("expected a first batch of 64 rows", rdr.Err())
	}
	mu.Lock()
	open := len(dirs)
	mu.Unlock()
	if open != 1 {
		t.Fatalf("%d spill directories for one sort", open)
	}
	rdr.Release()
	removed("released early")

	// Only the returned rows evaluate the select list, as in memory:
	// 1/(k-96) divides by zero for k = 96, whose rows the OFFSET skips (and
	// a LIMIT before them, in ascending order).
	for _, q := range []string{
		"SELECT 1 / (k - 96), id FROM " + table + " WHERE k IS NOT NULL ORDER BY k DESC, id OFFSET 60 LIMIT 2000",
		"SELECT 1 / (k - 96), id FROM " + table + " WHERE k IS NOT NULL ORDER BY k * 1, id LIMIT 2000",
	} {
		runs := sortSpillStats.runs.Load()
		if rows, _ := h.query(q); len(rows) != 2000 || sortSpillStats.runs.Load() == runs {
			t.Fatalf("%s: %d rows, spilled %v", q, len(rows), sortSpillStats.runs.Load() != runs)
		}
	}
	removed("offset and limit")
}
