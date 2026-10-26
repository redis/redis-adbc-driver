// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLScanProjectionBothAPIs(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_scan_projection"
	h.setupStreamTable(table, 1150)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("DELETE FROM " + table + " WHERE id IN (1,3,1149)")
	h.exec("UPDATE " + table + " SET id=9007199254740993, qty=NULL WHERE __rowid=1")
	h.exec("ALTER TABLE " + table + " ADD COLUMN stamp TIMESTAMP DEFAULT TIMESTAMP '2026-10-21 12:34:56.123456'")
	h.exec("UPDATE " + table + " SET stamp=NULL WHERE __rowid=3")
	read := func(r array.RecordReader) [][]Value {
		t.Helper()
		defer r.Release()
		var rows [][]Value
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
				rows = append(rows, row)
			}
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	for _, q := range []string{
		"SELECT label AS alias, id, id AS duplicate, qty, stamp, __rowid FROM " + table,
		"SELECT label FROM " + table + " LIMIT 100 OFFSET 10",
	} {
		t.Run(q, func(t *testing.T) {
			r, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: "false"})
			want := read(r)
			r, _ = h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "10"})
			sr, ok := r.(*streamReader)
			if !ok || sr.it.projection == nil {
				t.Fatalf("ordinary API did not select projection: %T", r)
			}
			if sr.arrow == nil || len(sr.pending) != 0 || len(sr.it.projection.rows) != 0 {
				t.Fatal("simple projection still buffers projected rows")
			}
			if got := read(r); !reflect.DeepEqual(got, want) {
				t.Fatalf("ordinary API: %d rows, want %d", len(got), len(want))
			}
		})
	}
	q := "SELECT label AS alias, id, id AS duplicate, qty, stamp, __rowid FROM " + table
	r, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: "false"})
	want := read(r)
	_, parts := planPartitions(t, h, q, 1)
	d, err := decodePartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	d.ScanCap = 127 // force several sparse, independently checked cursor ranges
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	r, err = h.conn.ReadPartition(h.ctx, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if sr, ok := r.(*streamReader); !ok || sr.it.projection == nil {
		t.Fatalf("partition API did not select projection: %T", r)
	}
	got := read(r)
	// Descriptors promise complete disjoint ranges, not the coordinator's
	// unordered cross-shard scan order. Compare every value by internal ID.
	byID := func(a, b []Value) int {
		if a[len(a)-1].I < b[len(b)-1].I {
			return -1
		}
		if a[len(a)-1].I > b[len(b)-1].I {
			return 1
		}
		return 0
	}
	slices.SortFunc(got, byID)
	slices.SortFunc(want, byID)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partition API: %d rows, want %d", len(got), len(want))
	}
}

func TestSQLScanArrowEarlyReleaseBothAPIs(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_scan_arrow_release"
	h.setupStreamTable(table, 1150)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	for _, api := range []string{"regular", "partition"} {
		for _, budget := range []string{"0", "1100"} {
			t.Run(api+"/bytes="+budget, func(t *testing.T) {
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)
				db, err := NewDriver(mem).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionIntStreamBatchRows: "7", OptionIntStreamBatchBytes: budget})
				if err != nil {
					t.Fatal(err)
				}
				conn, err := db.Open(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close(h.ctx)
				h2 := &sqlHarness{t: t, ctx: h.ctx, conn: conn}
				for _, action := range []string{"release", "cancel"} {
					ctx, cancel := context.WithCancel(h.ctx)
					q := "SELECT id,label FROM " + table
					var r array.RecordReader
					if api == "regular" {
						r, _ = h2.openStream(ctx, conn, q, nil, map[string]string{OptionIntStreamBatchRows: "7", OptionIntStreamBatchBytes: budget})
					} else {
						_, parts := planPartitions(t, h2, q, 1)
						r, err = conn.ReadPartition(ctx, parts.PartitionIDs[0])
						if err != nil {
							t.Fatal(err)
						}
					}
					expected := int64(7)
					if budget != "0" {
						expected = 2
					}
					if !r.Next() || r.RecordBatch().NumRows() != expected {
						t.Fatalf("first batch: %v", r.Err())
					}
					rec := r.RecordBatch()
					rec.Retain()
					value := rec.Column(1).(*array.String).Value(0)
					if action == "cancel" {
						cancel()
						if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
							t.Fatalf("cancel: %v", r.Err())
						}
					}
					r.Release()
					if rec.Column(1).(*array.String).Value(0) != value {
						t.Fatal("release invalidated retained output")
					}
					rec.Release()
					cancel()
				}
			})
		}
	}
}
