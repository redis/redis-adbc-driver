// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

func TestRequiredKeyStreaming(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_required_key_stream"
	h.setupStreamTable(table, 71)
	defer h.dropTables(table)
	h.exec("UPDATE " + table + " SET label=NULL WHERE id%3=0")
	for _, q := range []string{
		"SELECT id,label FROM " + table + " WHERE __rowid IN (1,3,5,9,2,7)",
		"(SELECT CAST(id AS BIGINT) AS n,label FROM " + table + " WHERE __rowid IN (1,3,5,9,2,7) LIMIT 3 OFFSET 1) UNION ALL (SELECT CAST(id AS DOUBLE PRECISION),label FROM " + table + " WHERE __rowid IN (1,2,3,4) LIMIT 2 OFFSET 1)",
	} {
		ordinary, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: "false"})
		want, schema := unionValues(t, ordinary)
		ordinary.Release()
		st, r, _ := h.openStmtStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"})
		got, actual := unionValues(t, r)
		r.Release()
		st.Close(h.ctx)
		if !slices.Equal(want, got) || !schema.Equal(actual) {
			t.Fatal(want, got, schema, actual)
		}
	}
	for _, q := range []string{
		"SELECT COUNT(*) FROM " + table,
		"SELECT DISTINCT label FROM " + table,
		"SELECT id FROM " + table + " ORDER BY label",
		"SELECT id FROM (SELECT * FROM " + table + ") t",
		"WITH t AS (SELECT * FROM " + table + ") SELECT * FROM t",
		"SELECT id,(SELECT 1) FROM " + table,
		"SELECT id FROM " + table + " UNION SELECT id FROM " + table,
	} {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetOption(h.ctx, OptionStringStreamRequired, "true"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSqlQuery(h.ctx, q); err != nil {
			t.Fatal(err)
		}
		r, _, err := st.ExecuteQuery(h.ctx)
		if r != nil {
			r.Release()
			t.Fatal("blocking plan returned a reader", q)
		}
		st.Close(h.ctx)
		if !isStatus(err, adbc.StatusNotImplemented) {
			t.Fatal(q, err)
		}
	}
	q := "SELECT id FROM " + table + " WHERE __rowid IN (1,2,3,4,5,6,7,8,9,10)"
	for _, mode := range []string{"release", "context", "statement", "connection"} {
		other := newSQLHarness(t)
		ctx, cancel := context.WithCancel(other.ctx)
		st, r, _ := other.openStmtStream(ctx, other.conn, q, nil, map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"})
		if !r.Next() {
			t.Fatal(r.Err())
		}
		batch := r.RecordBatch()
		batch.Retain()
		switch mode {
		case "release":
			r.Release()
		case "context":
			cancel()
		case "statement":
			if err := canceler(t, st).Cancel(ctx); err != nil {
				t.Fatal(err)
			}
		case "connection":
			if err := other.conn.Close(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if mode != "release" {
			if r.Next() || r.Err() == nil {
				t.Fatal("key stream survived cancellation", mode, r.Err())
			}
			r.Release()
		}
		if batch.NumRows() != 1 {
			t.Fatal("retained batch invalidated")
		}
		batch.Release()
		cancel()
		st.Close(h.ctx)
	}
	late := "SELECT id FROM " + table + " WHERE __rowid IN (1,2) UNION ALL SELECT id/(id-9) FROM " + table + " WHERE __rowid IN (9,10,11)"
	st, r, _ := h.openStmtStream(h.ctx, h.conn, late, nil, map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"})
	count := 0
	prefix := []int64{0, 1, -8}
	for r.Next() {
		for i := 0; i < int(r.RecordBatch().NumRows()); i++ {
			v, err := valueAt(r.RecordBatch().Column(0), i)
			if err != nil || count >= len(prefix) || v.I != prefix[count] {
				t.Fatal("wrong late-error prefix", count, v, err)
			}
			count++
		}
	}
	if count == 0 || r.Err() == nil {
		t.Fatal("late key-child error lost", count, r.Err())
	}
	r.Release()
	st.Close(h.ctx)
}

// Instrumented comparison: retained consumer batches, sampled live Go heap,
// and exact allocation counters. No peak-RSS or constant-memory claim.
func TestKeyStreamingAcceptanceProfile(t *testing.T) {
	if os.Getenv("ARROW_ACCEPTANCE_PROFILE") != "1" {
		t.Skip("set ARROW_ACCEPTANCE_PROFILE=1")
	}
	h := newSQLHarness(t)
	const table = "it_key_acceptance_profile"
	h.setupStreamTable(table, 8192)
	defer h.dropTables(table)
	for _, count := range []int{128, 2048, 8192} {
		for _, cardinality := range []int{1, 17, count} {
			ids := make([]string, count)
			for i := range ids {
				ids[i] = fmt.Sprint(i + 1)
			}
			q := fmt.Sprintf("SELECT id %% %d AS grp,id FROM %s WHERE __rowid IN (%s)", cardinality, table, strings.Join(ids, ","))
			schemaStmt, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := schemaStmt.SetSqlQuery(h.ctx, q); err != nil {
				t.Fatal(err)
			}
			expectedSchema, err := schemaStmt.(adbc.StatementExecuteSchemaWithContext).ExecuteSchema(h.ctx)
			schemaStmt.Close(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			for trial := 0; trial < 3; trial++ {
				for step := 0; step < 2; step++ {
					stream := (trial+step)%2 == 0
					mode := fmt.Sprint(stream)
					runtime.GC()
					var before, first, after, sample runtime.MemStats
					runtime.ReadMemStats(&before)
					peak := before.HeapAlloc
					start := time.Now()
					// Own statements here instead of the integration helper's
					// test-lifetime cleanup closures, which retain parsed plans.
					stmt, err := h.conn.NewStatement(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					if err := stmt.SetSqlQuery(h.ctx, q); err != nil {
						t.Fatal(err)
					}
					for k, v := range map[string]string{OptionStringStreamResults: mode, OptionIntStreamBatchRows: "64"} {
						if err := stmt.SetOption(h.ctx, k, v); err != nil {
							t.Fatal(err)
						}
					}
					r, _, err := stmt.ExecuteQuery(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					actualSchema := r.Schema()
					if !r.Next() {
						t.Fatal(r.Err())
					}
					firstTime := time.Since(start)
					runtime.ReadMemStats(&first)
					peak = max(peak, first.HeapAlloc)
					var batches []arrow.RecordBatch
					for {
						batch := r.RecordBatch()
						batch.Retain()
						batches = append(batches, batch)
						runtime.ReadMemStats(&sample)
						peak = max(peak, sample.HeapAlloc)
						if !r.Next() {
							break
						}
					}
					err = r.Err()
					r.Release()
					stmt.Close(h.ctx)
					elapsed := time.Since(start)
					runtime.ReadMemStats(&after)
					peak = max(peak, after.HeapAlloc)
					// Independent complete validation after timing, including duplicate
					// grouping values at low cardinality and exact physical schema.
					seen := make([]bool, count)
					rows := 0
					for _, batch := range batches {
						if batch.NumCols() != 2 {
							t.Fatal(batch.Schema())
						}
						for i := 0; i < int(batch.NumRows()); i++ {
							g, e1 := valueAt(batch.Column(0), i)
							v, e2 := valueAt(batch.Column(1), i)
							if e1 != nil || e2 != nil || v.I < 0 || v.I >= int64(count) || seen[v.I] || g.I != v.I%int64(cardinality) {
								t.Fatal("profile value mismatch", g, v, e1, e2)
							}
							seen[v.I] = true
							rows++
						}
						batch.Release()
					}
					if err != nil || rows != count || !actualSchema.Equal(expectedSchema) {
						t.Fatal(rows, count, err)
					}
					t.Logf("rows=%d cardinality=%d trial=%d stream=%s first_ns=%d full_ns=%d first_alloc=%d full_alloc=%d sampled_heap=%d", count, cardinality, trial, mode, firstTime.Nanoseconds(), elapsed.Nanoseconds(), first.TotalAlloc-before.TotalAlloc, after.TotalAlloc-before.TotalAlloc, peak)
				}
			}
		}
	}
}
