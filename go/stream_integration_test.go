// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

const streamRows = 25_000 // three cursor pages of 10,000 rows

// setupStreamTable ingests table with n rows: id 0…n-1, label "r<id>" and
// qty id % 10, all indexed.
func (h *sqlHarness) setupStreamTable(table string, n int) {
	h.t.Helper()
	h.dropTables(table)
	mem := memory.DefaultAllocator
	ib, lb, qb := array.NewInt64Builder(mem), array.NewStringBuilder(mem), array.NewInt32Builder(mem)
	defer ib.Release()
	defer lb.Release()
	defer qb.Release()
	for i := range n {
		ib.Append(int64(i))
		lb.Append(fmt.Sprintf("r%d", i))
		qb.Append(int32(i % 10))
	}
	cols := []arrow.Array{ib.NewArray(), lb.NewArray(), qb.NewArray()}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "label", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "qty", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
	}, nil)
	rec := array.NewRecordBatch(schema, cols, int64(n))
	defer rec.Release()
	for _, c := range cols {
		c.Release()
	}
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, table); err != nil {
		h.t.Fatal(err)
	}
	if err := st.Bind(h.ctx, rec); err != nil {
		h.t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		h.t.Fatal(err)
	}
}

// openStream runs sql with the statement options, and returns its reader
// and row count.
func (h *sqlHarness) openStream(ctx context.Context, conn adbc.ConnectionWithContext, sql string, rec arrow.RecordBatch,
	opts map[string]string) (array.RecordReader, int64) {
	h.t.Helper()
	_, rdr, n := h.openStmtStream(ctx, conn, sql, rec, opts)
	return rdr, n
}

// openStmtStream is openStream, and returns the statement too.
func (h *sqlHarness) openStmtStream(ctx context.Context, conn adbc.ConnectionWithContext, sql string, rec arrow.RecordBatch,
	opts map[string]string) (adbc.StatementWithContext, array.RecordReader, int64) {
	h.t.Helper()
	st, err := conn.NewStatement(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { st.Close(ctx) })
	for k, v := range opts {
		if err := st.SetOption(ctx, k, v); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := st.SetSqlQuery(ctx, sql); err != nil {
		h.t.Fatal(err)
	}
	if rec != nil {
		if err := st.Bind(ctx, rec); err != nil {
			h.t.Fatal(err)
		}
	}
	rdr, n, err := st.ExecuteQuery(ctx)
	if err != nil {
		h.t.Fatalf("%s: %v", sql, err)
	}
	return st, rdr, n
}

// canceler is the statement's Cancel, which AdbcStatementCancel looks for
// the same way.
func canceler(t *testing.T, st adbc.StatementWithContext) driverbase.StatementCanceler {
	t.Helper()
	c, ok := st.(driverbase.StatementCanceler)
	if !ok {
		t.Fatalf("%T has no Cancel", st)
	}
	return c
}

func isStatus(err error, code adbc.Status) bool {
	var ae adbc.Error
	return errors.As(err, &ae) && ae.Code == code
}

// readInts reads a result to the end: its first column, and the size of
// each batch.
func readInts(rdr array.RecordReader) (vals []int64, batches []int, err error) {
	for rdr.Next() {
		rec := rdr.RecordBatch()
		col := rec.Column(0).(*array.Int64)
		for i := range col.Len() {
			vals = append(vals, col.Value(i))
		}
		batches = append(batches, int(rec.NumRows()))
	}
	return vals, batches, rdr.Err()
}

// cursorOf returns the id of a stream's open cursor, or 0.
func cursorOf(rdr array.RecordReader) int64 {
	if r, ok := inspectionReader(rdr).(*streamReader); ok && r.it != nil && r.it.cur != nil {
		return r.it.cur.cursor
	}
	return 0
}

// delCursor deletes a cursor of a table's index, on the node that runs its
// searches, and reports whether it was there. Resolve the current binding under
// one pin; retained-generation tests must carry their original physical index.
func (h *sqlHarness) delCursor(raw goredis.UniversalClient, table string, id int64) bool {
	h.t.Helper()
	st, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	meta, err := st.getTable(ctx, defaultSchema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	err = st.searchDo(ctx, meta.index(), "FT.CURSOR", "DEL", meta.index(), id).Err()
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "Cursor not found") || strings.Contains(err.Error(), "Cursor does not exist") {
		return false
	}
	h.t.Fatal("cursor inspection failed", err)
	return false
}

func sequence(from, n, step int64) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = from + int64(i)*step
	}
	return out
}

func TestSQLStreamResults(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_stream_t"
	h.setupStreamTable(table, streamRows)
	raw := h.rawClient()
	small := map[string]string{OptionIntStreamBatchRows: "1000"}
	// These checks exercise Search cursor ownership. An unordered managed
	// scan uses exact directories; cover that lifecycle separately below.
	cursorTable := table + " ORDER BY __rowid"

	t.Run("streams in insertion order", func(t *testing.T) {
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT id, label FROM "+cursorTable, nil, small)
		defer rdr.Release()
		if n != -1 {
			t.Errorf("row count %d, want -1 for a stream", n)
		}
		if !rdr.Next() || rdr.RecordBatch().NumRows() != 1000 {
			t.Fatalf("first batch: %v", rdr.Err())
		}
		id := cursorOf(rdr)
		if id == 0 {
			t.Fatal("after the first batch, the stream has no cursor")
		}
		ids, batches, err := readInts(rdr)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(sequence(0, 1000, 1), ids...)
		if !slices.Equal(ids, sequence(0, streamRows, 1)) {
			t.Errorf("got %d ids, not 0…%d in order", len(ids), streamRows-1)
		}
		if len(batches) != streamRows/1000-1 || slices.ContainsFunc(batches, func(b int) bool { return b != 1000 }) {
			t.Errorf("batches %v, want batches of 1000", batches)
		}
		if h.delCursor(raw, table, id) {
			t.Error("the cursor was open at the end")
		}
		if fieldNames(rdr.Schema()) != "id|label" {
			t.Errorf("schema %v", rdr.Schema())
		}
	})

	t.Run("ORDER BY in the index", func(t *testing.T) {
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT id FROM "+table+" ORDER BY id DESC", nil, small)
		defer rdr.Release()
		ids, _, err := readInts(rdr)
		if err != nil || n != -1 || !slices.Equal(ids, sequence(streamRows-1, streamRows, -1)) {
			t.Errorf("count %d, %d ids, %v", n, len(ids), err)
		}
	})

	t.Run("releasing early deletes the cursor", func(t *testing.T) {
		rdr, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM "+cursorTable, nil, small)
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		id := cursorOf(rdr)
		rdr.Release()
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after Release", id)
		}
	})

	t.Run("cancelling the context ends the stream", func(t *testing.T) {
		ctx, cancel := context.WithCancel(h.ctx)
		defer cancel()
		rdr, _ := h.openStream(ctx, h.conn, "SELECT id FROM "+cursorTable, nil, small)
		defer rdr.Release()
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		id := cursorOf(rdr)
		cancel()
		var ae adbc.Error
		if rdr.Next() || !errors.As(rdr.Err(), &ae) || ae.Code != adbc.StatusCancelled {
			t.Errorf("after cancel: err = %v, want Cancelled", rdr.Err())
		}
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after cancel", id)
		}
	})

	t.Run("closing the connection ends the stream", func(t *testing.T) {
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: adminURI()})
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		rdr, _ := h.openStream(h.ctx, conn, "SELECT id FROM "+cursorTable, nil, small)
		defer rdr.Release()
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		id := cursorOf(rdr)
		if err := conn.Close(h.ctx); err != nil {
			t.Fatal(err)
		}
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after Close", id)
		}
		var ae adbc.Error
		if rdr.Next() || !errors.As(rdr.Err(), &ae) || ae.Code != adbc.StatusInvalidState {
			t.Errorf("after Close: err = %v, want InvalidState", rdr.Err())
		}
	})

	t.Run("cancelling the statement ends the stream", func(t *testing.T) {
		st, rdr, _ := h.openStmtStream(h.ctx, h.conn, "SELECT id FROM "+cursorTable, nil, small)
		defer rdr.Release()
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		id := cursorOf(rdr)
		c := canceler(t, st)
		if err := c.Cancel(h.ctx); err != nil {
			t.Fatal(err)
		}
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after Cancel", id)
		}
		if rdr.Next() || !isStatus(rdr.Err(), adbc.StatusCancelled) {
			t.Errorf("after Cancel: err = %v, want Cancelled", rdr.Err())
		}
		if err := c.Cancel(h.ctx); !isStatus(err, adbc.StatusInvalidState) {
			t.Errorf("Cancel again: err = %v, want InvalidState", err)
		}
		// The statement runs again.
		again, _, err := st.ExecuteQuery(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer again.Release()
		if ids, _, err := readInts(again); err != nil || len(ids) != streamRows {
			t.Errorf("run again: %d ids, %v", len(ids), err)
		}
	})

	t.Run("cancelling the statement while Next runs", func(t *testing.T) {
		st, rdr, _ := h.openStmtStream(h.ctx, h.conn, "SELECT id FROM "+cursorTable, nil, small)
		defer rdr.Release()
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		id := cursorOf(rdr)
		read := make(chan error)
		go func() {
			_, _, err := readInts(rdr)
			read <- err
		}()
		cerr := canceler(t, st).Cancel(h.ctx)
		err := <-read
		switch {
		case cerr == nil:
			if !isStatus(err, adbc.StatusCancelled) {
				t.Errorf("after Cancel: err = %v, want Cancelled", err)
			}
		case isStatus(cerr, adbc.StatusInvalidState):
			t.Log("the stream ended before Cancel")
			if err != nil {
				t.Errorf("read before Cancel: %v", err)
			}
		default:
			t.Errorf("Cancel: %v", cerr)
		}
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after Cancel", id)
		}
	})

	t.Run("cancelling a statement with no stream", func(t *testing.T) {
		// A whole result, and a stream released.
		st, rdr, n := h.openStmtStream(h.ctx, h.conn, "SELECT id FROM "+table+" WHERE qty = 3", nil, nil)
		rdr.Release()
		if n == -1 {
			t.Fatal("the result was streamed")
		}
		if err := canceler(t, st).Cancel(h.ctx); !isStatus(err, adbc.StatusInvalidState) {
			t.Errorf("after a whole result: err = %v, want InvalidState", err)
		}
		st, rdr, _ = h.openStmtStream(h.ctx, h.conn, "SELECT id FROM "+cursorTable, nil, small)
		rdr.Release()
		if err := canceler(t, st).Cancel(h.ctx); !isStatus(err, adbc.StatusInvalidState) {
			t.Errorf("after Release: err = %v, want InvalidState", err)
		}
	})

	t.Run("LIMIT and OFFSET with a residual filter", func(t *testing.T) {
		// Fits in the first batch: the LIMIT closes the cursor at once.
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT id FROM "+table+" WHERE id % 7 = 3 LIMIT 5 OFFSET 2", nil, small)
		ids, _, err := readInts(rdr)
		rdr.Release()
		if err != nil || n != 5 || !slices.Equal(ids, []int64{17, 24, 31, 38, 45}) {
			t.Errorf("small LIMIT: %v (count %d), %v", ids, n, err)
		}
		// Streamed, and stopping in the second of three cursor pages.
		rdr, n = h.openStream(h.ctx, h.conn, "SELECT id FROM "+table+" WHERE id % 2 = 0 LIMIT 8000 OFFSET 100", nil, small)
		defer rdr.Release()
		id := cursorOf(rdr)
		ids, _, err = readInts(rdr)
		if err != nil || n != -1 || !slices.Equal(ids, sequence(200, 8000, 2)) {
			t.Errorf("streamed LIMIT: %d ids from %v (count %d), %v", len(ids), ids[:min(len(ids), 3)], n, err)
		}
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after the LIMIT", id)
		}
	})

	t.Run("an error on a later page", func(t *testing.T) {
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT 1000 / (id - 15000) FROM "+cursorTable, nil, small)
		defer rdr.Release()
		id := cursorOf(rdr)
		for rdr.Next() {
		}
		if n != -1 || rdr.Err() == nil || !strings.Contains(rdr.Err().Error(), "division by zero") {
			t.Errorf("count %d, err = %v, want division by zero", n, rdr.Err())
		}
		if id == 0 || h.delCursor(raw, table, id) {
			t.Errorf("cursor %d was open after the error", id)
		}
	})

	t.Run("a cursor that expired", func(t *testing.T) {
		rdr, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM "+cursorTable, nil, small)
		defer rdr.Release()
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		if !h.delCursor(raw, table, cursorOf(rdr)) {
			t.Fatal("the stream's cursor wasn't open")
		}
		for rdr.Next() {
		}
		if rdr.Err() == nil || !strings.Contains(rdr.Err().Error(), OptionStringStreamResults+"=false") {
			t.Errorf("err = %v, want the expired cursor's", rdr.Err())
		}
	})

	t.Run("a result in one batch is whole", func(t *testing.T) {
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT id FROM "+table+" WHERE qty = 3", nil, nil)
		defer rdr.Release()
		ids, _, err := readInts(rdr)
		if err != nil || n != streamRows/10 || !slices.Equal(ids, sequence(3, streamRows/10, 10)) {
			t.Errorf("count %d, %d ids, %v", n, len(ids), err)
		}
	})

	t.Run("bound parameters", func(t *testing.T) {
		sql := "SELECT id, label FROM " + table + " WHERE qty = $1"
		want, err := h.executeSchema(sql)
		if err != nil {
			t.Fatal(err)
		}
		b := array.NewInt32Builder(memory.DefaultAllocator)
		defer b.Release()
		b.Append(3)
		col := b.NewArray()
		defer col.Release()
		rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int32}}, nil),
			[]arrow.Array{col}, 1)
		defer rec.Release()
		rdr, n := h.openStream(h.ctx, h.conn, sql, rec, map[string]string{OptionIntStreamBatchRows: "500"})
		defer rdr.Release()
		ids, _, err := readInts(rdr)
		if err != nil || n != -1 || !slices.Equal(ids, sequence(3, streamRows/10, 10)) {
			t.Errorf("count %d, %d ids, %v", n, len(ids), err)
		}
		if !rdr.Schema().Equal(want) {
			t.Errorf("schema %v, want ExecuteSchema's %v", rdr.Schema(), want)
		}
	})

	t.Run("numeric NULL order follows visible candidates", func(t *testing.T) {
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT id, label FROM "+table+" ORDER BY qty NULLS FIRST, id", nil, small)
		defer rdr.Release()
		var expected []int64
		for qty := range 10 {
			expected = append(expected, sequence(int64(qty), streamRows/10, 10)...)
		}
		var actual []int64
		for rdr.Next() {
			rec := rdr.RecordBatch()
			ids, labels := rec.Column(0).(*array.Int64), rec.Column(1).(*array.String)
			for i := range ids.Len() {
				id := ids.Value(i)
				if labels.IsNull(i) || labels.Value(i) != fmt.Sprintf("r%d", id) {
					t.Fatalf("id/label mismatch at %d", id)
				}
				actual = append(actual, id)
			}
		}
		if n != -1 || rdr.Err() != nil || !slices.Equal(actual, expected) {
			t.Fatalf("null-free numeric order: count=%d rows=%d err=%v", n, len(actual), rdr.Err())
		}
		// An actual visible NULL still requires the exact materializing fallback.
		h.dropTables("it_stream_null_order")
		h.exec("CREATE TABLE it_stream_null_order(id BIGINT, qty INTEGER)")
		h.exec("INSERT INTO it_stream_null_order VALUES (1,3),(2,1),(0,NULL)")
		nulls, count := h.openStream(h.ctx, h.conn, "SELECT id FROM it_stream_null_order ORDER BY qty NULLS FIRST,id", nil, small)
		defer nulls.Release()
		ids, _, err := readInts(nulls)
		if count != 3 || err != nil || !slices.Equal(ids, []int64{0, 2, 1}) {
			t.Fatalf("visible NULL fallback: count=%d ids=%v err=%v", count, ids, err)
		}
	})

	t.Run("distinct streams complete unique ids", func(t *testing.T) {
		rdr, n := h.openStream(h.ctx, h.conn, "SELECT DISTINCT id FROM "+table, nil, map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1000"})
		defer rdr.Release()
		ids, batches, err := readInts(rdr)
		slices.Sort(ids)
		if err != nil || n != -1 || !slices.Equal(ids, sequence(0, streamRows, 1)) || slices.ContainsFunc(batches, func(n int) bool { return n > 1000 }) {
			t.Fatalf("distinct stream: count=%d ids=%d batches=%v err=%v", n, len(ids), batches, err)
		}
	})

	t.Run("materialized", func(t *testing.T) {
		h.dropTables("it_stream_k")
		h.exec("CREATE TABLE it_stream_k (k INTEGER, name VARCHAR)")
		h.exec("INSERT INTO it_stream_k VALUES (1, 'one'), (2, 'two')")
		for _, tc := range []struct {
			sql  string
			opts map[string]string
			rows int64
		}{
			{"SELECT id FROM " + table, map[string]string{OptionStringStreamResults: "false"}, streamRows},
			{"SELECT id FROM " + table + " ORDER BY id % 10, id", small, streamRows},
			{"SELECT id, label FROM " + table + " ORDER BY qty NULLS FIRST, id", map[string]string{OptionStringStreamResults: "false"}, streamRows},
			{"SELECT DISTINCT id FROM " + table, small, streamRows},
			{"SELECT id, ROW_NUMBER() OVER (ORDER BY id) FROM " + table, small, streamRows},
			{"SELECT t.id FROM " + table + " t JOIN it_stream_k k ON k.k = t.qty", small, 2 * streamRows / 10},
			{"SELECT id, (SELECT name FROM it_stream_k k WHERE k.k = t.qty) FROM " + table + " t", small, streamRows},
			{"SELECT id FROM " + table + " UNION ALL SELECT k FROM it_stream_k ORDER BY 1", small, streamRows + 2},
			{"SELECT qty, COUNT(*) FROM " + table + " GROUP BY qty", small, 10},
		} {
			rdr, n := h.openStream(h.ctx, h.conn, tc.sql, nil, tc.opts)
			var rows int64
			for rdr.Next() {
				rows += rdr.RecordBatch().NumRows()
			}
			if n != tc.rows || rows != tc.rows || rdr.Err() != nil {
				t.Errorf("%s: count %d, %d rows, %v; want %d, whole", tc.sql, n, rows, rdr.Err(), tc.rows)
			}
			rdr.Release()
		}
	})

	t.Run("options", func(t *testing.T) {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		for _, kv := range [][2]string{{OptionStringStreamResults, "maybe"}, {OptionIntStreamBatchRows, "0"}} {
			if err := st.SetOption(h.ctx, kv[0], kv[1]); err == nil {
				t.Errorf("%s=%s was accepted", kv[0], kv[1])
			}
		}
		opts := st.(adbc.GetSetOptionsWithContext)
		if v, _ := opts.GetOption(h.ctx, OptionStringStreamResults); v != "true" {
			t.Errorf("%s = %q, want true", OptionStringStreamResults, v)
		}
		if err := opts.SetOptionInt(h.ctx, OptionIntStreamBatchRows, 42); err != nil {
			t.Fatal(err)
		}
		if v, _ := opts.GetOptionInt(h.ctx, OptionIntStreamBatchRows); v != 42 {
			t.Errorf("%s = %d, want 42", OptionIntStreamBatchRows, v)
		}
	})
}

// Public segmented streams retain exact images across unrelated later
// publications. Arrow consumer buffers outlive both the pin and connection.
func TestSQLStreamSnapshotLifetime(t *testing.T) {
	const table = "it_stream_snapshot"
	for _, action := range []string{"finish", "release", "cancel", "connection"} {
		t.Run(action, func(t *testing.T) {
			h := newSQLHarness(t)
			h.setupStreamTable(table, streamRows)
			h.exec("UPDATE " + table + " SET label=NULL,qty=99 WHERE id=20000")
			h.exec("DELETE FROM " + table + " WHERE id=21000")
			raw := h.rawClient()
			reader := newSQLHarness(t)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			rdr, n := h.openStream(ctx, reader.conn, "SELECT id,label,qty FROM "+table+" ORDER BY __rowid", nil, map[string]string{OptionIntStreamBatchRows: "37"})
			defer func() {
				if rdr != nil {
					rdr.Release()
				}
			}()
			stream, ok := inspectionReader(rdr).(*streamReader)
			if !ok || n != -1 || stream.snapshot == nil || stream.it == nil || !stream.it.meta.SnapshotOverlay || len(stream.it.meta.snapshotSegments()) < 2 {
				t.Fatal("expected a pinned native segmented stream")
			}
			original := cloneMeta(stream.it.meta)
			token := stream.snapshot.token
			seen := int64(0)
			check := func(rec arrow.RecordBatch) {
				t.Helper()
				if rec.NumRows() > 37 {
					t.Fatal("unbounded stream batch", rec.NumRows())
				}
				for i := 0; i < int(rec.NumRows()); i++ {
					if seen == 21000 {
						seen++
					}
					id := rec.Column(0).(*array.Int64)
					label := rec.Column(1).(*array.String)
					qty := rec.Column(2).(*array.Int32)
					if id.IsNull(i) || id.Value(i) != seen {
						t.Fatal("stream identity changed", seen)
					}
					if seen == 20000 {
						if !label.IsNull(i) || qty.IsNull(i) || qty.Value(i) != 99 {
							t.Fatal("stream lost replacement/NULL", seen)
						}
					} else if label.IsNull(i) || label.Value(i) != fmt.Sprintf("r%d", seen) || qty.IsNull(i) || qty.Value(i) != int32(seen%10) {
						t.Fatal("stream value changed", seen)
					}
					seen++
				}
			}
			if !rdr.Next() {
				t.Fatal("first segmented batch", rdr.Err())
			}
			check(rdr.RecordBatch())
			retained := rdr.RecordBatch()
			retained.Retain()
			defer retained.Release()
			// Change values outside the already buffered first batch, then retire the
			// entire binding. None of these current images belong to the old stream.
			h.exec("UPDATE " + table + " SET label='later' WHERE id=10000")
			h.exec("TRUNCATE " + table)
			h.exec("INSERT INTO " + table + " VALUES (999999,'new',9)")
			h.exec("DROP TABLE " + table)
			h.exec("CREATE TABLE " + table + " (id BIGINT,label TEXT,qty INTEGER)")
			h.exec("INSERT INTO " + table + " VALUES (888888,'replacement',8)")
			snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
			if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
				t.Fatal("active stream lost reader control")
			}
			for _, seg := range original.snapshotSegments() {
				if count, err := raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(seg.Prefix))).Result(); err != nil || count != seg.Rows {
					t.Fatal("DDL reclaimed live stream source", count, err)
				}
			}
			switch action {
			case "finish":
				for rdr.Next() {
					check(rdr.RecordBatch())
				}
				if err := rdr.Err(); err != nil || seen != streamRows {
					t.Fatal("incomplete old stream", seen, err)
				}
			case "release":
				rdr.Release()
				rdr = nil
			case "cancel":
				cancel()
				if rdr.Next() || !isStatus(rdr.Err(), adbc.StatusCancelled) {
					t.Fatal("cancelled native stream survived", rdr.Err())
				}
			case "connection":
				if err := reader.conn.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
				if rdr.Next() || !isStatus(rdr.Err(), adbc.StatusInvalidState) {
					t.Fatal("closed native stream survived", rdr.Err())
				}
			}
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
				t.Fatal("finished stream retained reader control")
			}
			for _, seg := range original.snapshotSegments() {
				h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
			}
			if retained.NumRows() != 37 || retained.Column(0).(*array.Int64).Value(0) != 0 || retained.Column(1).(*array.String).Value(0) != "r0" {
				t.Fatal("retained Arrow buffer changed after cleanup")
			}
			h.expectRows("SELECT id,label,qty FROM "+table, "888888|replacement|8")
		})
	}
}

// Unordered managed reads own a directory iterator and a snapshot pin instead
// of a Search cursor. Both single-table and UNION readers must release them.
func TestSQLDirectoryStreamLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_directory_stream"
	h.setupStreamTable(table, streamRows)
	defer h.dropTables(table)
	raw := h.rawClient()
	for _, union := range []bool{false, true} {
		for _, action := range []string{"finish", "release", "context", "statement", "connection", "error"} {
			t.Run(fmt.Sprintf("union=%t/%s", union, action), func(t *testing.T) {
				reader := newSQLHarness(t)
				ctx, cancel := context.WithCancel(reader.ctx)
				defer cancel()
				expr := "id"
				if action == "error" {
					expr = "1000 / (id - 15000)"
				}
				q := "SELECT " + expr + " FROM " + table
				if union {
					q += " UNION ALL SELECT id FROM " + table
				}
				st, r, n := reader.openStmtStream(ctx, reader.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "1000"})
				released := false
				defer func() {
					if !released {
						r.Release()
					}
				}()
				if n != -1 || !r.Next() {
					t.Fatal("expected a live stream", n, r.Err())
				}
				root := inspectionReader(r).(*streamReader)
				leaf := root
				if union {
					leaf = unionActiveLeaf(root.union)
				}
				if leaf == nil || leaf.it == nil || leaf.it.delta == nil || leaf.it.cur != nil {
					t.Fatal("expected an exact-directory scan without a Search cursor")
				}
				it := leaf.it
				token := root.snapshot.token
				if !raw.HExists(ctx, snapshotCatalogKey, "p:"+token).Val() {
					t.Fatal("live stream has no snapshot pin")
				}
				rec := r.RecordBatch()
				rec.Retain()
				defer rec.Release()
				first := rec.Column(0).(*array.Int64).Value(0)
				switch action {
				case "finish":
					ids, _, err := readInts(r)
					want := sequence(1000, streamRows-1000, 1)
					if union {
						want = append(want, sequence(0, streamRows, 1)...)
					}
					if err != nil || !slices.Equal(ids, want) {
						t.Fatal("incomplete/out-of-order directory stream", len(ids), err)
					}
				case "release":
					r.Release()
					released = true
				case "context":
					cancel()
				case "statement":
					if err := canceler(t, st).Cancel(ctx); err != nil {
						t.Fatal(err)
					}
				case "connection":
					if err := reader.conn.Close(ctx); err != nil {
						t.Fatal(err)
					}
				case "error":
					for r.Next() {
					}
					if r.Err() == nil || !strings.Contains(r.Err().Error(), "division by zero") {
						t.Fatal("expected later-page division error", r.Err())
					}
				}
				if action == "context" || action == "statement" || action == "connection" {
					status := adbc.StatusCancelled
					if action == "connection" {
						status = adbc.StatusInvalidState
					}
					if r.Next() || !isStatus(r.Err(), status) {
						t.Fatal("stream survived cancellation/close", r.Err())
					}
				}
				if !root.closed || it.delta != nil || raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
					t.Fatal("directory iterator or snapshot pin leaked")
				}
				if rec.Column(0).(*array.Int64).Value(0) != first {
					t.Fatal("retained consumer batch changed")
				}
			})
		}
	}
}
