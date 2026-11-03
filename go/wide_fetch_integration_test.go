// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestSQLWideHashFetchBothAPIs(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_wide_hash_fetch"
	h.dropTables(table)
	defs := []string{"id BIGINT", "label VARCHAR"}
	outputs := []string{"id AS alias", "label"}
	for i := range 62 {
		defs = append(defs, fmt.Sprintf("p%d BIGINT DEFAULT 9007199254740993", i))
		outputs = append(outputs, fmt.Sprintf("p%d", i))
	}
	h.exec("CREATE TABLE " + table + " (" + strings.Join(defs, ",") + ")")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	for start := 1; start <= 5000; start += 250 {
		var rows []string
		for id := start; id < start+250; id++ {
			value := int64(id)
			if id == 1 {
				value = 9007199254740993
			}
			rows = append(rows, fmt.Sprintf("(%d,'row%d')", value, id))
		}
		h.exec("INSERT INTO " + table + " (id,label) VALUES " + strings.Join(rows, ","))
	}
	h.exec("DELETE FROM " + table + " WHERE __rowid IN (4,4000)")
	h.exec("ALTER TABLE " + table + " ADD COLUMN stamp TIMESTAMP DEFAULT TIMESTAMP '2026-10-21 12:34:56.123456'")
	h.exec("UPDATE " + table + " SET p0=NULL,stamp=NULL WHERE __rowid=3")
	var nulls []string
	for _, field := range append([]string{"id", "label", "stamp"}, outputs[2:]...) {
		nulls = append(nulls, field+"=NULL")
	}
	h.exec("UPDATE " + table + " SET " + strings.Join(nulls, ",") + " WHERE __rowid=5000")
	h.exec("UPDATE " + table + " SET label='a\x00b' WHERE __rowid=1")
	raw := h.rawClient()
	st := &store{client: raw, snapshots: true}
	if err := addSnapshotHooks(raw)(h.ctx); err != nil {
		t.Fatal(err)
	}
	readCtx, pin, err := st.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := st.getTable(readCtx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, "stamp", "id AS duplicate", "__rowid")
	q := "SELECT " + strings.Join(outputs, ",") + " FROM " + table
	for _, tc := range []struct {
		name, suffix string
		fast         bool
	}{
		{"explicit range", " WHERE __rowid>=1 AND __rowid<=5000", true},
		{"inexact boundary", " WHERE __rowid>=9007199254740992", false},
		{"residual", " WHERE __rowid>=1 AND p0%2=1", false},
		{"pushed limit", " WHERE __rowid>=1 LIMIT 2 OFFSET 1", false},
		{"descending", " WHERE __rowid>=1 ORDER BY __rowid DESC", false},
	} {
		e := &executor{store: st, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
		st.searchCapsOnce.Do(func() { st.searchCaps = [2]int64{1_000_000, 1 << 31} })
		parsed, err := ParseScript(q + tc.suffix)
		if err != nil {
			t.Fatal(err)
		}
		p, err := e.planSelect(readCtx, parsed[0].Stmt.(*SelectStmt), nil)
		if err != nil {
			t.Fatal(err)
		}
		sc, err := e.prepareScan(readCtx, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		it, err := e.openScan(readCtx, sc.req, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: ranges=%v limit=%v sort=%+v residual=%T hashFetch=%v loadAll=%v", tc.name, sc.req.ranges, sc.req.limit, sc.req.sortBy, sc.req.where.residual, it.hashFetch, !it.hashFetch && it.store == nil && len(it.fetch) == 0)
		it.close(readCtx)
		if it.hashFetch != tc.fast {
			t.Fatalf("%s selected=%v, want %v", tc.name, it.hashFetch, tc.fast)
		}
	}
	stamp := arrow.Timestamp(time.Date(2026, 10, 21, 12, 34, 56, 123456000, time.UTC).UnixMicro())
	check := func(r array.RecordReader, ordered bool) {
		t.Helper()
		defer r.Release()
		sr, ok := r.(*streamReader)
		if !ok || !sr.it.hashFetch {
			t.Fatalf("wide fetch not selected: %T", r)
		}
		if sr.it.projection == nil || len(sr.it.projection.hashPositions) == 0 || sr.arrow == nil {
			t.Fatal("wide Arrow projection did not bind positional HASH values")
		}
		if sr.it.projection.consume != nil || sr.it.projection.consumeHash != nil {
			t.Fatal("projection callback escaped a fill")
		}
		seen := map[int64]bool{}
		var previous int64
		for r.Next() {
			rec := r.RecordBatch()
			for i := 0; i < int(rec.NumRows()); i++ {
				id := rec.Column(66).(*array.Int64).Value(i)
				if id < 1 || id > 5000 || id == 4 || id == 4000 || seen[id] {
					t.Fatalf("unexpected/duplicate internal ID %d", id)
				}
				seen[id] = true
				if ordered && id <= previous {
					t.Fatalf("implicit row order: %d after %d", id, previous)
				}
				previous = id
				for col := range 66 {
					a := rec.Column(col)
					null := id == 5000 || id == 3 && (col == 2 || col == 64)
					if a.IsNull(i) != null {
						t.Fatalf("row %d column %d NULL=%v, want %v", id, col, a.IsNull(i), null)
					}
					if null {
						continue
					}
					switch col {
					case 1:
						want := fmt.Sprintf("row%d", id)
						if id == 1 {
							want = "a\x00b"
						}
						if a.(*array.String).Value(i) != want {
							t.Fatalf("row %d: incorrect string", id)
						}
					case 64:
						if a.(*array.Timestamp).Value(i) != stamp {
							t.Fatalf("row %d: incorrect timestamp", id)
						}
					default:
						want := int64(9007199254740993)
						if (col == 0 || col == 65) && id != 1 {
							want = id
						}
						if a.(*array.Int64).Value(i) != want {
							t.Fatalf("row %d column %d: incorrect integer", id, col)
						}
					}
				}
			}
		}
		if r.Err() != nil || len(seen) != 4998 {
			t.Fatalf("rows=%d error=%v", len(seen), r.Err())
		}
	}
	r, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "127"})
	check(r, true)
	// An explicit range previously gained an unnecessary full residual when
	// ordinary scanning split it into cap-bounded ranges. Keep the same results
	// and the same positional HASH/Arrow strategy as the unrestricted scan.
	r, _ = h.openStream(h.ctx, h.conn, q+" WHERE __rowid>=1 AND __rowid<=5000", nil, map[string]string{OptionIntStreamBatchRows: "127"})
	check(r, true)
	_, parts := planPartitions(t, h, q, 1)
	r, err = h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	check(r, false)
	for _, pred := range []string{"__rowid>=1 AND __rowid<=5000", "__rowid>=-9223372036854775808 AND __rowid<=9223372036854775807", "__rowid>9007199254740992", "__rowid>=2 AND __rowid<9"} {
		fast, fs := h.query(q + " WHERE " + pred)
		generic, gs := h.query(q + " WHERE (" + pred + ") AND (id IS NULL OR id IS NOT NULL)")
		if !reflect.DeepEqual(fast, generic) || !fs.Equal(gs) {
			t.Fatalf("range differs from generic execution: %s", pred)
		}
	}
	h.expectRows(q + " WHERE __rowid>9000")
	limited, _ := h.query(q + " LIMIT 2 OFFSET 1")
	if len(limited) != 2 || !strings.HasSuffix(limited[0], "|2") || !strings.HasSuffix(limited[1], "|3") {
		t.Fatalf("wide LIMIT/OFFSET fallback returned %v", limited)
	}

	// A managed stream keeps its generation across a later SQL publication.
	r, _ = h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "127"})
	h.exec("UPDATE " + table + " SET label='new generation' WHERE __rowid=2")
	check(r, true)
	r, err = h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	check(r, false)
	h.exec("UPDATE " + table + " SET label='row2' WHERE __rowid=2")

	// Keep the unmanaged discovery/fetch race coverage on a separate physical
	// copy. Mutating published managed HASHes would violate their immutability.
	unmanaged := cloneMeta(meta)
	unmanaged.KeyPrefix = rowPrefix(defaultSchema, table+"_unmanaged_"+snapshotID())
	unmanaged.IndexName = indexName(defaultSchema, table+"_unmanaged_"+snapshotID())
	unmanaged.SnapshotOverlay, unmanaged.SnapshotSegments, unmanaged.SnapshotRows = false, nil, nil
	unmanaged.SnapshotLineage = ""
	directory := newSnapshotDeltaScan(st, meta, nil)
	copied := 0
	for {
		_, rows, err := directory.next(readCtx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		pipe := raw.Pipeline()
		for _, row := range rows {
			pipe.HSet(h.ctx, unmanaged.prefix()+row[rowIDField], map[string]string(row))
		}
		if len(rows) > 0 {
			if _, err := pipe.Exec(h.ctx); err != nil {
				t.Fatal(err)
			}
		}
		copied += len(rows)
	}
	if copied != 4998 {
		t.Fatalf("unmanaged fixture rows=%d", copied)
	}
	if err := raw.Do(h.ctx, snapshotScannedIndexArgs(unmanaged)...).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Do(h.ctx, "FT.DROPINDEX", unmanaged.index(), "DD").Err(); err != nil {
			t.Error(err)
		}
	})
	st, meta = &store{client: raw}, unmanaged
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := st.checkRangeReadiness(h.ctx, meta.index()); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Independent authoritative HASH changes after discovery: vanished rows
	// are omitted; complete current HASH values replace the cursor's values.
	need := map[string]bool{}
	for _, c := range meta.Columns {
		need[c.Name] = true
	}
	e := &executor{store: st}
	it, err := e.openScan(h.ctx, scanRequest{meta: meta, where: wherePlan{query: "*"}, need: need}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(h.ctx)
	if !it.hashFetch || len(it.cur.first) < 2 || it.cur.count != wideFetchChunk {
		t.Fatal("wide discovery was not bounded as expected")
	}
	deleted, updated := it.cur.first[0]["__key"], it.cur.first[1]["__key"]
	if err := st.client.Del(h.ctx, deleted).Err(); err != nil {
		t.Fatal(err)
	}
	if err := st.client.HSet(h.ctx, updated, "id", "-9223372036854775808").Err(); err != nil {
		t.Fatal(err)
	}
	found := false
	for {
		keys, rows, err := it.next(h.ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for i, key := range keys {
			if key == deleted {
				t.Fatal("returned a row deleted after discovery")
			}
			if key == updated {
				found = true
				if rows[i]["id"].I != -9223372036854775808 {
					t.Fatal("did not fetch the updated authoritative HASH value")
				}
			}
		}
	}
	if !found {
		t.Fatal("updated row missing")
	}
	// Repeat the discovery/fetch race with the positional callback active.
	func() {
		direct, err := e.openScan(h.ctx, scanRequest{meta: meta, where: wherePlan{query: "*"}, need: need}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer direct.close(h.ctx)
		deleted, updated := direct.cur.first[0]["__key"], direct.cur.first[1]["__key"]
		updatedID := direct.cur.first[1][rowIDField]
		direct.projection = newScanProjection(direct, []planItem{{expr: &ColumnRef{Name: "id"}}})
		decodedUpdated := false
		direct.projection.consumeHash = func(values []any) error {
			row := make([]Value, 1)
			if err := direct.projection.decodeHashInto(values, row); err != nil {
				return err
			}
			if values[0] == updatedID {
				decodedUpdated = row[0].I == -9223372036854775808
			}
			return nil
		}
		if err := st.client.Del(h.ctx, deleted).Err(); err != nil {
			t.Fatal(err)
		}
		if err := st.client.HSet(h.ctx, updated, "id", "-9223372036854775808").Err(); err != nil {
			t.Fatal(err)
		}
		seenUpdated := false
		for {
			keys, rows, err := direct.next(h.ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil || rows != nil {
				t.Fatalf("positional read: rows=%v error=%v", rows, err)
			}
			for _, key := range keys {
				if key == deleted {
					t.Fatal("positional read returned vanished row")
				}
				seenUpdated = seenUpdated || key == updated
			}
		}
		if !seenUpdated || !decodedUpdated {
			t.Fatal("positional read missed the authoritative update")
		}
	}()

	for _, api := range []string{"regular", "partition"} {
		for _, action := range []string{"release", "cancel"} {
			ctx, cancel := context.WithCancel(h.ctx)
			var r array.RecordReader
			if api == "regular" {
				r, _ = h.openStream(ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "7"})
			} else {
				r, err = h.conn.ReadPartition(ctx, parts.PartitionIDs[0])
				if err != nil {
					t.Fatal(err)
				}
			}
			if !r.Next() {
				t.Fatalf("%s/%s: %v", api, action, r.Err())
			}
			rec := r.RecordBatch()
			rec.Retain()
			first := rec.Column(0).(*array.Int64).Value(0)
			stringRow := 0
			for rec.Column(1).IsNull(stringRow) {
				stringRow++
			}
			label := rec.Column(1).(*array.String).Value(stringRow)
			if action == "cancel" {
				cancel()
				if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
					t.Fatalf("%s cancel: %v", api, r.Err())
				}
			}
			r.Release()
			if rec.Column(0).(*array.Int64).Value(0) != first {
				t.Fatal("retained Arrow record invalidated by release")
			}
			if rec.Column(1).(*array.String).Value(stringRow) != label {
				t.Fatal("retained Arrow string invalidated by release")
			}
			rec.Release()
			cancel()
		}
	}
}
