// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// searchFieldTerm matches the field name of each `@field:` Search term.
var searchFieldTerm = regexp.MustCompile(`@([^:\s()|]+):`)

// partitionRowStrings renders every row as its column values joined by "|".
func partitionRowStrings(r array.RecordReader) ([]string, error) {
	var rows []string
	for r.Next() {
		rec := r.RecordBatch()
		for i := range int(rec.NumRows()) {
			cells := make([]string, rec.NumCols())
			for c := range int(rec.NumCols()) {
				if rec.Column(c).IsNull(i) {
					cells[c] = "NULL"
				} else {
					cells[c] = rec.Column(c).ValueStr(i)
				}
			}
			rows = append(rows, strings.Join(cells, "|"))
		}
	}
	return rows, r.Err()
}

// #599: a managed snapshot partition read scanned its whole row-ID span from
// exact directories and applied the WHERE clause client-side, so a selective
// DuckDB `adbc_scanner` partition (`WHERE l_shipinstruct = ?`) fetched four
// rows for every one it returned. A partition whose predicate adds Search terms
// must read candidates restricted to its exact row-ID span, keeping newer-
// segment visibility and complete results. Unfiltered and non-indexable
// predicates keep the exact directory path: the span bound alone is also a
// Search range, and a pure row-ID range is slower through Search.
func TestSnapshotPartitionFilteredCandidates(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_candidates"
	h.dropTables(table)
	t.Cleanup(func() { h.dropTables(table) })
	h.exec("CREATE TABLE " + table + " (k BIGINT, g TEXT, v INTEGER)")
	h.exec("INSERT INTO " + table + ` SELECT i, CASE WHEN i % 4 = 0 THEN 'hit' ELSE 'miss' END,
		CASE WHEN i % 10 = 0 THEN NULL ELSE i END FROM generate_series(1, 2000) AS g(i)`)
	h.exec("UPDATE " + table + " SET g = 'miss' WHERE k = 400") // newer version stops matching
	h.exec("UPDATE " + table + " SET g = 'hit' WHERE k = 401")  // newer version starts matching
	h.exec("UPDATE " + table + " SET v = -1 WHERE k = 404")     // newer version keeps matching
	h.exec("DELETE FROM " + table + " WHERE k = 800")           // tombstone

	// Partitions read on another connection, hooked on both its maintenance
	// and execution clients.
	hook := &candidateQueryHook{}
	workerCtx, worker := statisticsHookedTransport(t, newSQLHarness(t), hook)

	for _, tc := range []struct {
		name, query, literal, param string
		candidates                  bool
	}{
		{name: "tag", query: "SELECT k, v FROM " + table + " WHERE g = 'hit'", candidates: true},
		{name: "bound parameter", query: "SELECT k, v FROM " + table + " WHERE g = ?",
			literal: "SELECT k, v FROM " + table + " WHERE g = 'hit'", param: "hit", candidates: true},
		{name: "tag and residual", query: "SELECT k, v FROM " + table + " WHERE g = 'hit' AND k % 3 = 0", candidates: true},
		{name: "null result values", query: "SELECT k, v FROM " + table + " WHERE g = 'hit' AND v IS NULL", candidates: true},
		{name: "unfiltered", query: "SELECT k, v FROM " + table},
		{name: "no index term", query: "SELECT k, v FROM " + table + " WHERE k % 4 = 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stmt, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close(h.ctx)
			if err := stmt.SetSqlQuery(h.ctx, tc.query); err != nil {
				t.Fatal(err)
			}
			if err := stmt.SetOption(h.ctx, OptionIntPartitionCount, "4"); err != nil {
				t.Fatal(err)
			}
			if tc.param != "" {
				b := array.NewStringBuilder(memory.DefaultAllocator)
				b.Append(tc.param)
				a := b.NewArray()
				b.Release()
				rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "", Type: arrow.BinaryTypes.String}}, nil), []arrow.Array{a}, 1)
				a.Release()
				err := stmt.Bind(h.ctx, rec)
				rec.Release()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, parts, _, err := stmt.ExecutePartitions(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			registerPartitionTestCleanup(t, h, parts)
			if len(parts.PartitionIDs) < 2 {
				t.Fatalf("expected several partitions, got %d", len(parts.PartitionIDs))
			}

			var got []string
			bounded, rowIDQueries := 0, 0
			for _, id := range parts.PartitionIDs {
				d, err := decodePartition(id)
				if err != nil {
					t.Fatal(err)
				}
				hook.mu.Lock()
				hook.queries = nil
				hook.mu.Unlock()
				r, err := worker.ReadPartition(workerCtx, id)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := partitionRowStrings(r)
				r.Release()
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, rows...)
				hook.mu.Lock()
				queries := slices.Clone(hook.queries)
				hook.mu.Unlock()
				lo, hi := "["+strconv.FormatInt(d.Lo, 10)+" +inf]", "[-inf "+strconv.FormatInt(d.Hi, 10)+"]"
				spanned := false
				for _, q := range queries {
					if !strings.Contains(q, "@"+rowIDField+":") {
						continue
					}
					rowIDQueries++
					// The caller's filter must accompany the exact span bounds.
					filtered := false
					for _, m := range searchFieldTerm.FindAllStringSubmatch(q, -1) {
						filtered = filtered || m[1] != rowIDField
					}
					if filtered && strings.Contains(q, lo) && strings.Contains(q, hi) {
						spanned = true
					}
				}
				if spanned {
					bounded++
				}
			}

			literal := tc.query
			if tc.literal != "" {
				literal = tc.literal
			}
			r, _ := h.openStream(h.ctx, h.conn, literal, nil, nil)
			want, err := partitionRowStrings(r)
			r.Release()
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			slices.Sort(want)
			if len(want) == 0 || !slices.Equal(got, want) {
				t.Fatalf("partition rows differ: got %d rows, want %d\n got: %.300q\nwant: %.300q", len(got), len(want), got, want)
			}
			if tc.name == "tag" {
				// Superseded and deleted versions stay invisible; newer matches appear.
				for _, row := range []string{"400|NULL", "800|NULL", "404|404"} {
					if slices.Contains(got, row) {
						t.Fatalf("stale or non-matching row %s returned", row)
					}
				}
				for _, row := range []string{"401|401", "404|-1", "396|396"} {
					if !slices.Contains(got, row) {
						t.Fatalf("matching row %s missing", row)
					}
				}
			}
			switch {
			case tc.candidates && bounded != len(parts.PartitionIDs):
				t.Fatalf("%d of %d filtered partitions read span-bounded Search candidates (%d row-ID queries)",
					bounded, len(parts.PartitionIDs), rowIDQueries)
			case !tc.candidates && rowIDQueries != 0:
				t.Fatalf("%s partitions issued %d row-ID Search queries; want exact directories", tc.name, rowIDQueries)
			}
			t.Logf("partitions=%d rows=%d rowIDQueries=%d", len(parts.PartitionIDs), len(got), rowIDQueries)
		})
	}
}
