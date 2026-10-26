// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestStreamByteBudget(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_stream_byte_budget"
	h.setupStreamTable(table, 12000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("UPDATE " + table + " SET label='" + strings.Repeat("z", 20000) + "' WHERE id=100")
	h.exec("UPDATE " + table + " SET label=NULL WHERE id=101")
	const budget int64 = 4096
	opts := map[string]string{OptionIntStreamBatchRows: "7", OptionIntStreamBatchBytes: "4096"}
	worker := openWith(t, os.Getenv("REDIS_URI"), opts)
	verify := func(rec arrow.RecordBatch, expression bool) {
		t.Helper()
		ids, labels := rec.Column(0).(*array.Int64), rec.Column(1).(*array.String)
		for i := 0; i < ids.Len(); i++ {
			id := ids.Value(i)
			if id < 0 || id >= 12000 {
				t.Fatal("invalid id", id)
			}
			if id == 101 {
				if !labels.IsNull(i) {
					t.Fatal("NULL lost")
				}
				continue
			}
			want := fmt.Sprintf("r%d", id)
			if id == 100 {
				want = strings.Repeat("z", 20000)
			}
			if expression {
				want += "!"
			}
			if labels.IsNull(i) || labels.Value(i) != want {
				t.Fatal("wrong value", id)
			}
		}
	}
	check := func(r array.RecordReader, expected int64, expression bool) {
		t.Helper()
		defer r.Release()
		var count int64
		for r.Next() {
			rec := r.RecordBatch()
			verify(rec, expression)
			var cost int64
			for i := 0; i < int(rec.NumRows()); i++ {
				for j := 0; j < int(rec.NumCols()); j++ {
					v, err := valueAt(rec.Column(j), i)
					if err != nil {
						t.Fatal(err)
					}
					cost = addBatchBytes(cost, batchValueBytes(v))
				}
			}
			if rec.NumRows() == 0 && expected != 0 || rec.NumRows() > 7 || rec.NumRows() > 1 && cost > budget {
				t.Fatalf("batch rows=%d accounted=%d", rec.NumRows(), cost)
			}
			count += rec.NumRows()
		}
		if r.Err() != nil || count != expected {
			t.Fatal(count, expected, r.Err())
		}
	}
	for _, q := range []string{"SELECT id,label FROM " + table, "SELECT id, label || '!' AS label FROM " + table} {
		r, _ := h.openStream(h.ctx, h.conn, q, nil, opts)
		check(r, 12000, strings.Contains(q, "||"))
		_, parts := planPartitions(t, h, q, 4)
		// Every descriptor uses connection-local inherited byte and row settings.
		seen := map[int64]bool{}
		for _, id := range parts.PartitionIDs {
			r, err := worker.conn.ReadPartition(h.ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			var rows int64
			// Independently check all partition batches and accumulate actual row count.
			for r.Next() {
				rec := r.RecordBatch()
				verify(rec, strings.Contains(q, "||"))
				for _, key := range rec.Column(0).(*array.Int64).Int64Values() {
					if seen[key] {
						t.Fatal("duplicate", key)
					}
					seen[key] = true
				}
				var cost int64
				for i := 0; i < int(rec.NumRows()); i++ {
					for j := 0; j < int(rec.NumCols()); j++ {
						v, err := valueAt(rec.Column(j), i)
						if err != nil {
							t.Fatal(err)
						}
						cost += batchValueBytes(v)
					}
				}
				if rec.NumRows() > 7 || rec.NumRows() > 1 && cost > budget {
					t.Fatal("partition budget", cost, rec.NumRows())
				}
				rows += rec.NumRows()
			}
			if r.Err() != nil {
				t.Fatal(r.Err())
			}
			r.Release()
			if rows == 0 {
				t.Fatal("empty descriptor")
			}
		}
		if len(seen) != 12000 {
			t.Fatal("partition membership", len(seen))
		}
	}
	for _, suffix := range []string{" LIMIT 0", " WHERE id<0", " LIMIT 9 OFFSET 95"} {
		r, _ := h.openStream(h.ctx, h.conn, "SELECT id,label FROM "+table+suffix, nil, opts)
		n := int64(0)
		if strings.Contains(suffix, "LIMIT 9") {
			n = 9
		}
		check(r, n, false)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	r, _ := h.openStream(ctx, h.conn, "SELECT id,label FROM "+table, nil, opts)
	if !r.Next() {
		t.Fatal(r.Err())
	}
	cancel()
	if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
		t.Fatal("cancel", r.Err())
	}
	r.Release()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	typed := st.(adbc.GetSetOptionsWithContext)
	if err := typed.SetOptionInt(h.ctx, OptionIntStreamBatchBytes, 17); err != nil {
		t.Fatal(err)
	}
	if n, err := typed.GetOptionInt(h.ctx, OptionIntStreamBatchBytes); err != nil || n != 17 {
		t.Fatal(n, err)
	}
}
