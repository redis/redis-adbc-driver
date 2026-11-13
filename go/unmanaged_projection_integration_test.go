// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Same 105-column/null/exact-value corpus as managed ownership tests, but the
// source HASHes/index belong to an application and have no internal row ID.
func unmanagedWideProjectionFixture(t *testing.T, rows int) (*sqlHarness, string, string) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const prefix = "unmanaged_projection_source:"
	const index = "unmanaged_projection_source_idx"
	const table = "unmanaged_projection_live"
	rec := memoryRecord(rows)
	defer rec.Release()
	types := map[string]string{}
	names := make([]string, rec.NumCols())
	for col, f := range rec.Schema().Fields() {
		names[col] = `"` + f.Name + `"`
		types[f.Name] = "BIGINT"
		if col == 104 {
			types[f.Name] = "TEXT"
		}
	}
	for start := 0; start < rows; start += 256 {
		pipe := raw.Pipeline()
		for row := start; row < min(start+256, rows); row++ {
			fields := map[string]any{}
			for col, f := range rec.Schema().Fields() {
				v, err := valueAt(rec.Column(col), row)
				if err != nil {
					t.Fatal(err)
				}
				if !v.Null {
					fields[f.Name] = encodeStored(v)
				}
			}
			pipe.HSet(h.ctx, unmanagedWideProjectionKey(int64(row+1)), fields)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Do(h.ctx, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "id", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, index); err != nil {
		t.Fatal(err)
	}
	_, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Mode: "unmanaged", Prefix: prefix, Index: index, Table: table, Types: types, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	// Arbitrary application fields with internal-looking names are not IDs.
	if err := raw.HSet(h.ctx, unmanagedWideProjectionKey(1), rowIDField, "application-owned, not integer").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.exec("DROP TABLE IF EXISTS " + table)
		if raw.Exists(h.ctx, unmanagedWideProjectionKey(1)).Val() != 1 || raw.HGet(h.ctx, unmanagedWideProjectionKey(1), rowIDField).Val() != "application-owned, not integer" {
			t.Error("read/drop changed application source")
		}
		if raw.Do(h.ctx, "FT.INFO", index).Err() != nil {
			t.Error("read/drop changed application index")
		}
	})
	return h, "SELECT " + strings.Join(names, ",") + " FROM " + table, table
}

func unmanagedWideProjectionKey(id int64) string {
	if id == 17 {
		return "unmanaged_projection_source:\x00λ"
	}
	return fmt.Sprintf("unmanaged_projection_source:%d", id)
}

func TestUnmanagedWideProjectionArrowLifetime(t *testing.T) {
	h, query, _ := unmanagedWideProjectionFixture(t, 9001)
	for _, action := range []string{"finish", "release", "cancel"} {
		t.Run(action, func(t *testing.T) {
			st, reader, count := h.openStmtStream(h.ctx, h.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "127"})
			released := false
			defer func() {
				if !released {
					reader.Release()
				}
			}()
			if count != -1 || !reader.Next() {
				t.Fatal("fixture did not stream", count, reader.Err())
			}
			stream, ok := inspectionReader(reader).(*streamReader)
			if !ok || stream.it == nil || stream.it.projection == nil || stream.arrow == nil || stream.it.hashFetch || stream.it.unmanagedHashFetch || len(stream.pending) != 0 {
				t.Fatal("unfiltered unmanaged LOAD* did not choose direct Arrow projection")
			}
			retained := reader.RecordBatch()
			retained.Retain()
			defer retained.Release()
			seen := make([]bool, 9001)
			validateMemoryRecord(t, retained, seen)
			if !reader.Next() {
				t.Fatal("missing second batch", reader.Err())
			}
			validateMemoryRecord(t, reader.RecordBatch(), seen)
			switch action {
			case "finish":
				for reader.Next() {
					validateMemoryRecord(t, reader.RecordBatch(), seen)
				}
				if reader.Err() != nil {
					t.Fatal(reader.Err())
				}
				for _, ok := range seen {
					if !ok {
						t.Fatal("incomplete source result")
					}
				}
			case "cancel":
				if err := canceler(t, st).Cancel(h.ctx); err != nil {
					t.Fatal(err)
				}
				if reader.Next() || !isStatus(reader.Err(), adbc.StatusCancelled) {
					t.Fatal("cancel delivered buffered output", reader.Err())
				}
			}
			reader.Release()
			released = true
			validateMemoryRecord(t, retained, make([]bool, 9001))
		})
	}
}

func TestUnmanagedProjectionKeyAndPredicateFallback(t *testing.T) {
	h, _, table := unmanagedWideProjectionFixture(t, 257)
	// Full scans expose the real source key through the direct projection.
	query := "SELECT __key,id,c104 FROM " + table
	r, _ := h.openStream(h.ctx, h.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "7"})
	stream, ok := inspectionReader(r).(*streamReader)
	if !ok || stream.it == nil || stream.it.projection == nil || stream.arrow == nil {
		r.Release()
		t.Fatal("full live HASH scan did not choose key projection")
	}
	keysSeen := make([]bool, 258)
	for r.Next() {
		rec := r.RecordBatch()
		for row := 0; row < int(rec.NumRows()); row++ {
			key, ke := valueAt(rec.Column(0), row)
			id, ie := valueAt(rec.Column(1), row)
			if ke != nil || ie != nil || id.I < 1 || id.I > 257 || keysSeen[id.I] || key.S != unmanagedWideProjectionKey(id.I) {
				r.Release()
				t.Fatal("source key changed", key, id, ke, ie)
			}
			keysSeen[id.I] = true
		}
	}
	if r.Err() != nil {
		r.Release()
		t.Fatal(r.Err())
	}
	r.Release()
	for id := 1; id <= 257; id++ {
		if !keysSeen[id] {
			t.Fatal("missing source key", id)
		}
	}
	// Explicit point predicates retain their source-value residual fallback too.
	query = "SELECT __key,id,c104 FROM " + table + " WHERE __key='" + strings.ReplaceAll(unmanagedWideProjectionKey(17), "'", "''") + "'"
	r, _ = h.openStream(h.ctx, h.conn, query, nil, nil)
	if !r.Next() {
		r.Release()
		t.Fatal("missing binary key row", r.Err())
	}
	rec := r.RecordBatch()
	key, err := valueAt(rec.Column(0), 0)
	if err != nil || key.S != unmanagedWideProjectionKey(17) || rec.NumRows() != 1 {
		r.Release()
		t.Fatal("binary source key changed", key, err)
	}
	if r.Next() || r.Err() != nil {
		r.Release()
		t.Fatal("unexpected extra key row", r.Err())
	}
	r.Release()
	// Simple integer predicates remain live-HASH rechecked while projecting
	// accepted rows directly, rather than retaining decoded row maps.
	r, _ = h.openStream(h.ctx, h.conn, "SELECT id FROM "+table+" WHERE id >= 2", nil, map[string]string{OptionIntStreamBatchRows: "7"})
	stream, ok = inspectionReader(r).(*streamReader)
	if !ok || stream.it.projection == nil || stream.it.projection.predicate == nil || stream.arrow == nil || !stream.it.unmanagedHashFetch {
		r.Release()
		t.Fatal("eligible live residual did not use filtered Arrow projection")
	}
	seen := make([]bool, 258)
	for r.Next() {
		rec := r.RecordBatch()
		for row := 0; row < int(rec.NumRows()); row++ {
			v, err := valueAt(rec.Column(0), row)
			if err != nil || v.I < 2 || v.I > 257 || seen[v.I] {
				r.Release()
				t.Fatal("bad live residual row", v, err)
			}
			seen[v.I] = true
		}
	}
	if r.Err() != nil {
		r.Release()
		t.Fatal(r.Err())
	}
	r.Release()
	for id := 2; id <= 257; id++ {
		if !seen[id] {
			t.Fatal("missing residual row", id)
		}
	}
}
