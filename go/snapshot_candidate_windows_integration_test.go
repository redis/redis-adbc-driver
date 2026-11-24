// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// candidateQueryHook records the query string of every FT.SEARCH.
type candidateQueryHook struct {
	mu      sync.Mutex
	queries []string
}

func (h *candidateQueryHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *candidateQueryHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *candidateQueryHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if args := cmd.Args(); len(args) > 2 && strings.EqualFold(fmt.Sprint(args[0]), "FT.SEARCH") {
			h.mu.Lock()
			h.queries = append(h.queries, fmt.Sprint(args[2]))
			h.mu.Unlock()
		}
		return next(ctx, cmd)
	}
}

var (
	windowTerm = regexp.MustCompile(`@__rowid:\[\((\d+) (\d+)\]`)
	openTerm   = regexp.MustCompile(`@__rowid:\[\((\d+) \+inf\]`)
)

// #599: open-ended `@__rowid:[(after +inf]` keyset pages make Search evaluate
// every remaining match per page, so a whole scan costs O(rows²/page). Pages
// must read bounded row-ID windows, with at most one open-ended tail page per
// source, while keeping complete, ordered results across sparse/dense/empty
// regions and newer-segment visibility (UPDATE and DELETE).
func TestSnapshotCandidateBoundedWindows(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_candidate_windows"
	h.dropTables(table)
	t.Cleanup(func() { h.dropTables(table) })
	h.exec("CREATE TABLE " + table + " (k BIGINT, g INTEGER)")
	// Dense [1,100], a sparse middle with two matches, an empty gap and a
	// dense tail [451,600]: windows of 64 row IDs see full, short and empty
	// pages and must widen across the gap.
	h.exec("INSERT INTO " + table + ` SELECT i, CASE WHEN i <= 100 OR i >= 451 OR i IN (200, 333) THEN 1 ELSE 0 END
		FROM generate_series(1, 600) AS g(i)`)
	h.exec("UPDATE " + table + " SET g = 0 WHERE k = 50")  // newer version stops matching
	h.exec("UPDATE " + table + " SET g = 1 WHERE k = 250") // newer version starts matching
	h.exec("DELETE FROM " + table + " WHERE k = 60")       // tombstone
	var want []int
	for i := 1; i <= 600; i++ {
		if (i <= 100 || i >= 451 || i == 200 || i == 333 || i == 250) && i != 50 && i != 60 {
			want = append(want, i)
		}
	}

	scan := func(t *testing.T, stale int64) ([]string, int) {
		t.Helper()
		hook := &candidateQueryHook{}
		raw := h.rawClient(hook)
		defer raw.Close()
		st, ctx, pin := h.inspectionSnapshot(raw)
		defer pin.release()
		meta, err := st.getTable(ctx, defaultSchema, table)
		if err != nil {
			t.Fatal(err)
		}
		if len(meta.snapshotSegments()) < 2 {
			t.Fatalf("fixture has %d segments, want newer-segment visibility", len(meta.snapshotSegments()))
		}
		d := newSnapshotCandidateScan(ctx, st, meta, "@g:[1 1]", []string{"k"}, 4)
		if d.bound < 600 {
			t.Fatalf("row-ID window bound %d does not cover the table", d.bound)
		}
		if stale > 0 {
			// A stale bound only moves work to the open-ended tail page.
			d.bound = stale
		}
		var got []int
		for {
			_, rows, err := d.next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				k, err := strconv.Atoi(row["k"])
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, k)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("candidates %v\nwant %v", got, want)
		}
		hook.mu.Lock()
		defer hook.mu.Unlock()
		return hook.queries, len(d.sources)
	}

	t.Run("windows", func(t *testing.T) {
		queries, sources := scan(t, 0)
		open, windows := 0, 0
		for _, q := range queries {
			switch {
			case windowTerm.MatchString(q):
				m := windowTerm.FindStringSubmatch(q)
				lo, _ := strconv.ParseInt(m[1], 10, 64)
				hi, _ := strconv.ParseInt(m[2], 10, 64)
				if hi <= lo {
					t.Fatalf("empty window %q", q)
				}
				windows++
			case openTerm.MatchString(q):
				open++
			}
		}
		// One open-ended tail page per source proves completeness past the bound.
		if windows == 0 || open > sources {
			t.Fatalf("got %d bounded windows, %d open-ended pages for %d sources: %q", windows, open, sources, queries)
		}
	})
	t.Run("selective", func(t *testing.T) {
		// Sparse windows widen geometrically: a selective predicate needs only
		// a few bounded windows and one tail page per source.
		hook := &candidateQueryHook{}
		raw := h.rawClient(hook)
		defer raw.Close()
		st, ctx, pin := h.inspectionSnapshot(raw)
		defer pin.release()
		meta, err := st.getTable(ctx, defaultSchema, table)
		if err != nil {
			t.Fatal(err)
		}
		d := newSnapshotCandidateScan(ctx, st, meta, "@k:[333 333]", []string{"k"}, 4)
		var got []string
		for {
			_, rows, err := d.next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				got = append(got, row["k"])
			}
		}
		if !reflect.DeepEqual(got, []string{"333"}) {
			t.Fatalf("selective candidates %v, want [333]", got)
		}
		// 600 row IDs from a 64-row initial window: (0,64], (64,192], (192,448],
		// (448,600], then the tail page.
		open := 0
		for _, q := range hook.queries {
			switch {
			case openTerm.MatchString(q):
				open++
			case !windowTerm.MatchString(q):
				t.Fatalf("unbounded non-tail page %q", q)
			}
		}
		if open > len(d.sources) || len(hook.queries) > 6*len(d.sources) {
			t.Fatalf("%d Search queries (%d open-ended) for %d sources: %q", len(hook.queries), open, len(d.sources), hook.queries)
		}
	})
	t.Run("stale bound", func(t *testing.T) {
		queries, _ := scan(t, 120)
		open := 0
		for _, q := range queries {
			if openTerm.MatchString(q) {
				open++
			}
		}
		if open == 0 {
			t.Fatal("rows past a stale bound need open-ended tail pages")
		}
	})
}
