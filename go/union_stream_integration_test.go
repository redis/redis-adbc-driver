// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"slices"
	"testing"
	"time"
)

func TestUnionAllStream(t *testing.T) {
	h := newSQLHarness(t)
	const left = "it_union_stream_left"
	const right = "it_union_stream_right"
	h.setupStreamTable(left, 25001)
	h.setupStreamTable(right, 13)
	defer h.dropTables(left, right)
	r, n := h.openStream(h.ctx, h.conn, "SELECT id FROM "+left+" UNION ALL SELECT id FROM "+right, nil, map[string]string{OptionIntStreamBatchRows: "113"})
	defer r.Release()
	if n != -1 {
		t.Fatalf("UNION ALL materialized %d rows before ExecuteQuery returned", n)
	}
	vals, batches, err := readInts(r)
	if err != nil || len(vals) != 25014 {
		t.Fatal(len(vals), err)
	}
	for _, n := range batches {
		if n < 1 || n > 113 {
			t.Fatal("batch bound", n)
		}
	}
	for i, v := range vals {
		want := int64(i)
		if i >= 25001 {
			want = int64(i - 25001)
		}
		if v != want {
			t.Fatal(i, v, want)
		}
	}
}

// Collect exact values and schema, without retaining consumer batches.
func unionValues(t *testing.T, r array.RecordReader) ([]string, *arrow.Schema) {
	t.Helper()
	var out []string
	for r.Next() {
		rec := r.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			row := make([]Value, rec.NumCols())
			for j := range row {
				v, err := valueAt(rec.Column(j), i)
				if err != nil {
					t.Fatal(err)
				}
				row[j] = v
			}
			out = append(out, valueRows([][]Value{row})[0])
		}
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	return out, r.Schema()
}

func TestUnionAllStreamSemantics(t *testing.T) {
	h := newSQLHarness(t)
	const a = "it_union_semantics_a"
	const b = "it_union_semantics_b"
	h.setupStreamTable(a, 53)
	h.setupStreamTable(b, 17)
	defer h.dropTables(a, b)
	h.exec("UPDATE " + a + " SET label=NULL WHERE id % 3=0")
	queries := []string{
		"SELECT id,label FROM " + a + " UNION ALL SELECT id,label FROM " + b,
		"SELECT CAST(id+9007199254740993 AS BIGINT) AS n FROM " + a + " UNION ALL SELECT CAST(qty AS DOUBLE PRECISION) FROM " + b,
		"(SELECT CAST(id+16777217 AS BIGINT) AS n FROM " + a + " UNION ALL SELECT CAST(qty AS REAL) FROM " + b + ") UNION ALL SELECT CAST(id AS DOUBLE PRECISION) FROM " + a,
		"SELECT CAST(id AS DECIMAL(16,2)) AS n FROM " + a + " UNION ALL SELECT CAST(qty AS DECIMAL(20,4)) FROM " + b,
		"(SELECT id,label FROM " + a + " LIMIT 7 OFFSET 2) UNION ALL (SELECT id,label FROM " + b + " LIMIT 9 OFFSET 1) LIMIT 10 OFFSET 4",
		"SELECT id,label FROM " + a + " WHERE id<0 UNION ALL SELECT id,label FROM " + b + " LIMIT 7 OFFSET 1",
		"SELECT id,label FROM " + a + " UNION ALL SELECT id,label FROM " + b + " LIMIT 0",
		"SELECT CASE WHEN id%2=0 THEN DATE '2024-01-01' ELSE TIMESTAMP '2024-02-01 01:02:03' END AS d FROM " + a + " UNION ALL SELECT TIMESTAMPTZ '2024-03-01 01:02:03+02' FROM " + b,
		"SELECT ARRAY[CAST(id AS INT),NULL] AS v FROM " + a + " UNION ALL SELECT ARRAY[CAST(id AS INT),NULL] FROM " + b,
		"SELECT CAST(label AS CHAR(8)) AS s FROM " + a + " UNION ALL SELECT CAST(label AS VARCHAR(12)) FROM " + b,
	}
	h.exec("SET TIME ZONE 'America/Los_Angeles'")
	for i, q := range queries {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			materialized, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: "false"})
			expected, schema := unionValues(t, materialized)
			materialized.Release()
			r, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "3", OptionIntStreamBatchBytes: "900"})
			defer r.Release()
			actual, gotSchema := unionValues(t, r)
			if !schema.Equal(gotSchema) || !slices.Equal(actual, expected) {
				t.Fatalf("schema/values differ\n%s\n%s\n%v\n%v", schema, gotSchema, expected, actual)
			}
		})
	}
	// Single-row parameters preserve a stable schema and stream; multiple rows
	// retain the established materialized concatenation path.
	rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int64}}, nil))
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).Append(10)
	params := rb.NewRecordBatch()
	defer params.Release()
	q := "SELECT id FROM " + a + " WHERE id >= $1 UNION ALL SELECT id FROM " + b + " WHERE id >= $1 LIMIT 30 OFFSET 2"
	m, _ := h.openStream(h.ctx, h.conn, q, params, map[string]string{OptionStringStreamResults: "false"})
	expected, schema := unionValues(t, m)
	m.Release()
	r, n := h.openStream(h.ctx, h.conn, q, params, map[string]string{OptionIntStreamBatchRows: "3"})
	defer r.Release()
	actual, gotSchema := unionValues(t, r)
	if n != -1 || !schema.Equal(gotSchema) || !slices.Equal(actual, expected) {
		t.Fatal(n, expected, actual)
	}
}

func unionActiveLeaf(n *unionRows) *streamReader {
	if n.leaf != nil {
		return n.leaf
	}
	for _, child := range n.children {
		if child != nil {
			if leaf := unionActiveLeaf(child); leaf != nil {
				return leaf
			}
		}
	}
	return nil
}

func TestUnionAllStreamLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	const a = "it_union_lifecycle_a"
	const b = "it_union_lifecycle_b"
	h.setupStreamTable(a, 25001)
	h.setupStreamTable(b, 25001)
	defer h.dropTables(a, b)
	q := "SELECT id FROM " + a + " UNION ALL SELECT id FROM " + b
	raw := h.rawClient()
	opts := map[string]string{OptionIntStreamBatchRows: "37"}
	for _, mode := range []string{"release", "context", "statement", "connection"} {
		t.Run(mode, func(t *testing.T) {
			other := h
			if mode == "connection" {
				other = newSQLHarness(t)
			}
			ctx, cancel := context.WithCancel(other.ctx)
			defer cancel()
			st, r, n := other.openStmtStream(ctx, other.conn, q, nil, opts)
			if n != -1 || !r.Next() {
				t.Fatal(n, r.Err())
			}
			reader := r.(*streamReader)
			leaf := unionActiveLeaf(reader.union)
			id := cursorOf(leaf)
			if leaf == nil || id == 0 {
				t.Fatal("no active child cursor")
			}
			if reader.union.children[1].leaf != nil {
				t.Fatal("right branch opened eagerly")
			}
			// Retained consumer buffers survive cancellation/connection close.
			rec := r.RecordBatch()
			rec.Retain()
			defer rec.Release()
			if mode == "release" {
				r.Release()
			} else {
				defer r.Release()
				switch mode {
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
				if r.Next() || r.Err() == nil {
					t.Fatal("stream survived cancellation/close", r.Err())
				}
			}
			if rec.Column(0).(*array.Int64).Value(0) != 0 {
				t.Fatal("retained batch changed")
			}
			if h.delCursor(raw, a, id) {
				t.Fatal("child cursor leaked")
			}
		})
	}

	t.Run("release after child transition", func(t *testing.T) {
		r, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM "+a+" WHERE id<3 UNION ALL SELECT id FROM "+b, nil, opts)
		root := r.(*streamReader)
		leaf := unionActiveLeaf(root.union)
		id := cursorOf(leaf)
		if !root.union.children[0].closed || root.union.children[1].leaf != leaf || id == 0 {
			r.Release()
			t.Fatal("child transition did not close left/open right")
		}
		r.Release()
		if h.delCursor(raw, b, id) {
			t.Fatal("right cursor leaked")
		}
	})
	t.Run("concurrent Next and Cancel", func(t *testing.T) {
		st, r, _ := h.openStmtStream(h.ctx, h.conn, q, nil, opts)
		defer r.Release()
		done := make(chan error, 1)
		go func() {
			for r.Next() {
				_ = r.RecordBatch().NumRows()
			}
			done <- r.Err()
		}()
		if err := canceler(t, st).Cancel(h.ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if !isStatus(err, adbc.StatusCancelled) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Next/Cancel deadlocked")
		}
		options := h.conn.(interface {
			GetOption(context.Context, string) (string, error)
		})
		active, err := options.GetOption(h.ctx, OptionIntQueryWorkersActive)
		if err != nil || active != "0" {
			t.Fatal("worker tokens retained", active, err)
		}
	})
	t.Run("limit avoids later branch", func(t *testing.T) {
		r, n := h.openStream(h.ctx, h.conn, "SELECT id FROM "+a+" UNION ALL SELECT 1/(id-id) FROM "+b+" LIMIT 41", nil, opts)
		defer r.Release()
		vals, _, err := readInts(r)
		if n != -1 || len(vals) != 41 || err != nil {
			t.Fatal(n, len(vals), err)
		}
	})
	t.Run("later child error", func(t *testing.T) {
		r, n := h.openStream(h.ctx, h.conn, "SELECT id FROM "+a+" UNION ALL SELECT 1/(id-id) FROM "+b, nil, opts)
		defer r.Release()
		vals, _, err := readInts(r)
		if n != -1 || len(vals) == 0 || err == nil {
			t.Fatal(n, len(vals), err)
		}
		if unionActiveLeaf(r.(*streamReader).union) != nil {
			t.Fatal("failed child remains open")
		}
	})
	t.Run("first child error", func(t *testing.T) {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		st.SetOption(h.ctx, OptionIntStreamBatchRows, "37")
		st.SetSqlQuery(h.ctx, "SELECT 1/(id-id) FROM "+a+" UNION ALL SELECT id FROM "+b)
		r, _, err := st.ExecuteQuery(h.ctx)
		if r != nil || err == nil {
			if r != nil {
				r.Release()
			}
			t.Fatal("first-fill error was delayed", err)
		}
	})
}

func TestUnionAllStreamFallback(t *testing.T) {
	h := newSQLHarness(t)
	const a = "it_union_fallback"
	h.setupStreamTable(a, 35)
	defer h.dropTables(a)
	for _, q := range []string{
		"SELECT id FROM " + a + " UNION ALL SELECT id FROM " + a + " ORDER BY 1 DESC",
		"SELECT id FROM " + a + " UNION SELECT id FROM " + a,
		"SELECT id FROM " + a + " INTERSECT ALL SELECT id FROM " + a,
		"SELECT id FROM " + a + " UNION ALL SELECT 1",
		"SELECT id FROM " + a + " WHERE __rowid=1 UNION ALL SELECT id FROM " + a,
		"SELECT row_number() OVER () FROM " + a + " UNION ALL SELECT id FROM " + a,
		"SELECT id FROM " + a + " WHERE id IN (SELECT id FROM " + a + ") UNION ALL SELECT id FROM " + a,
	} {
		r, n := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "3"})
		if n < 0 {
			r.Release()
			t.Fatal("blocking/ineligible plan streamed", q)
		}
		unionValues(t, r)
		r.Release()
	}
}
