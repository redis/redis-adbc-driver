// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Portable/retained SQL without ORDER BY has no row-order contract. Compare
// complete independently encoded Arrow rows, preserving duplicates, NULLs,
// empty lists and nested shape, and still require exact batch schemas.
func assertInventoryUnorderedReader(t *testing.T, reader array.RecordReader, want arrow.RecordBatch) {
	t.Helper()
	rowJSON := func(a arrow.Array, i int) string {
		row := array.NewSlice(a, int64(i), int64(i+1))
		defer row.Release()
		data, err := row.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	var expected, actual []string
	for i := 0; i < int(want.NumRows()); i++ {
		expected = append(expected, rowJSON(want.Column(0), i))
	}
	if !reader.Schema().Equal(want.Schema()) {
		t.Fatal("schema changed", reader.Schema(), want.Schema())
	}
	for reader.Next() {
		r := reader.RecordBatch()
		if !r.Schema().Equal(want.Schema()) {
			t.Fatal("batch schema changed", r.Schema(), want.Schema())
		}
		for i := 0; i < int(r.NumRows()); i++ {
			actual = append(actual, rowJSON(r.Column(0), i))
		}
	}
	if reader.Err() != nil {
		t.Fatal(reader.Err())
	}
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		t.Fatal("values/duplicate counts changed", actual, expected)
	}
}

func TestArrowLayoutInventoryStorage(t *testing.T) {
	cases := layoutInventoryCases(t, memory.DefaultAllocator)
	defer releaseInventory(cases)
	checkCanonicalStorage(t, cases, "it_layout_inventory")
}

func checkCanonicalStorage(t *testing.T, cases []layoutInventoryCase, prefix string) {
	t.Helper()
	h := newSQLHarness(t)
	worker := newSQLHarness(t) // separate metadata cache and partition reader
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Shared connections are sequential, but helper failures belong to the
			// current case rather than calling FailNow on its parent test.
			h := &sqlHarness{t: t, ctx: h.ctx, conn: h.conn}
			table := fmt.Sprintf("%s_%d", prefix, i)
			h.exec("DROP TABLE IF EXISTS " + table)
			t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
			bounds := [][2]int64{{0, int64(c.input.Len())}, {1, int64(c.input.Len())}, {0, 0}}
			var expectedBatches []arrow.RecordBatch
			defer func() {
				for _, r := range expectedBatches {
					r.Release()
				}
			}()
			for j, bound := range bounds {
				a := array.NewSlice(c.input, bound[0], bound[1])
				rec := inventoryRecord(a)
				a.Release()
				st, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				mode := adbc.OptionValueIngestModeAppend
				if j == 0 || c.want == nil {
					mode = adbc.OptionValueIngestModeCreate
				}
				for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
					if err := st.SetOption(h.ctx, k, v); err != nil {
						t.Fatal(err)
					}
				}
				if j%2 == 0 {
					err = st.Bind(h.ctx, rec)
				} else {
					reader, e := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
					if e != nil {
						t.Fatal(e)
					}
					err = st.BindStream(h.ctx, reader)
				}
				rec.Release()
				if err == nil {
					var n int64
					n, err = st.ExecuteUpdate(h.ctx)
					if err == nil && n != bound[1]-bound[0] {
						t.Fatal(n)
					}
				}
				st.Close(h.ctx)
				if c.want == nil {
					if !isStatus(err, adbc.StatusNotImplemented) {
						t.Fatal("unsupported ingestion", err)
					}
					if _, e := worker.conn.GetTableSchema(h.ctx, nil, nil, table); !isStatus(e, adbc.StatusNotFound) {
						t.Fatal("rejection created a table", e)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				want := array.NewSlice(c.want, bound[0], bound[1])
				expectedBatches = append(expectedBatches, inventoryRecord(want))
				want.Release()
			}
			if c.want == nil {
				return
			}
			// Independent ordinary Arrow concatenation forms the full create+append oracle.
			columns := make([]arrow.Array, len(expectedBatches))
			for j, r := range expectedBatches {
				columns[j] = r.Column(0)
			}
			wantArray, err := array.Concatenate(columns, memory.DefaultAllocator)
			if err != nil {
				t.Fatal(err)
			}
			want := inventoryRecord(wantArray)
			wantArray.Release()
			defer want.Release()
			schema, err := worker.conn.GetTableSchema(h.ctx, nil, nil, table)
			if err != nil || !schema.Equal(want.Schema()) {
				t.Fatal("metadata reload", schema, err)
			}
			query := "SELECT v FROM " + table
			r, _ := h.openStream(h.ctx, worker.conn, query+" ORDER BY __rowid", nil, map[string]string{OptionIntStreamBatchRows: "1"})
			assertInventoryReader(t, r, want)
			r.Release()
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, query); err != nil {
				t.Fatal(err)
			}
			if err := st.SetOption(h.ctx, OptionIntPartitionCount, "1"); err != nil {
				t.Fatal(err)
			}
			partSchema, parts, _, err := st.ExecutePartitions(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !partSchema.Equal(want.Schema()) || parts.NumPartitions != 1 {
				t.Fatal(partSchema, parts.NumPartitions)
			}
			group, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringPartitionSnapshotID)
			if err != nil || group == "" {
				t.Fatal("live partition release ID missing", group, err)
			}
			release := func() { h.exec("DROP PARTITION SNAPSHOT '" + group + "'") }
			t.Cleanup(release)
			r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			assertInventoryUnorderedReader(t, r, want)
			r.Release()
			// Statement Close intentionally preserves descriptors for independent
			// workers. Release this completed set before the next case publishes.
			release()
			if stale, err := worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
				stale.Release()
				t.Fatal("released live partition remained readable")
			} else if !strings.Contains(err.Error(), "partition snapshot expired or released") {
				t.Fatal("released partition failed for an unrelated reason", err)
			}
			// Retained publication serializes canonical output, never these physical inputs.
			storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
			publisher := retainedAPIConnection(t, storage.namespace, false)
			readerConn := retainedAPIConnection(t, storage.namespace, true)
			pub, err := publisher.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pub.Close(h.ctx)
			if err := pub.SetSqlQuery(h.ctx, query); err != nil {
				t.Fatal(err)
			}
			for k, v := range map[string]string{OptionStringPartitionResultMode: "retained", OptionIntPartitionCount: "1", OptionIntStreamBatchRows: "2"} {
				if err := pub.SetOption(h.ctx, k, v); err != nil {
					t.Fatal(err)
				}
			}
			retSchema, retParts, _, err := pub.ExecutePartitions(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !retSchema.Equal(want.Schema()) || retParts.NumPartitions != 1 {
				t.Fatal(retSchema, retParts.NumPartitions)
			}
			// Prove the reader uses retained canonical IPC, independent of live source rows.
			h.exec("TRUNCATE TABLE " + table)
			r, err = readerConn.ReadPartition(h.ctx, retParts.PartitionIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			assertInventoryUnorderedReader(t, r, want)
			r.Release()
		})
	}
}

func TestArrowLayoutExpansionRejectedBeforeRows(t *testing.T) {
	h := newSQLHarness(t)
	raw := retainedTestClient(t, h.rawClient())
	ends := inventoryJSON(t, memory.DefaultAllocator, arrow.PrimitiveTypes.Int64, fmt.Sprintf("[%d]", maxNormalizedInputValues+1))
	defer ends.Release()
	values := inventoryJSON(t, memory.DefaultAllocator, arrow.PrimitiveTypes.Int64, `[7]`)
	defer values.Release()
	a := array.NewRunEndEncodedArray(ends, values, maxNormalizedInputValues+1, 0)
	defer a.Release()
	rec := inventoryRecord(a)
	defer rec.Release()
	for _, stream := range []bool{false, true} {
		for _, ingest := range []bool{false, true} {
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ingest {
				const table = "it_layout_expansion_guard"
				h.exec("DROP TABLE IF EXISTS " + table)
				defer h.exec("DROP TABLE IF EXISTS " + table)
				if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, table); err != nil {
					t.Fatal(err)
				}
			} else if err := st.SetSqlQuery(h.ctx, "SELECT ? AS v"); err != nil {
				t.Fatal(err)
			}
			root, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				r, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
				if err != nil {
					t.Fatal(err)
				}
				err = st.BindStream(h.ctx, r)
			} else {
				err = st.Bind(h.ctx, rec)
			}
			if err != nil {
				t.Fatal(err)
			}
			if ingest {
				_, err = st.ExecuteUpdate(h.ctx)
			} else {
				var r array.RecordReader
				r, _, err = st.ExecuteQuery(h.ctx)
				if r != nil {
					r.Release()
					t.Fatal("oversized parameters returned a reader")
				}
			}
			st.Close(h.ctx)
			if !isStatus(err, adbc.StatusInvalidArgument) || !strings.Contains(err.Error(), "Arrow normalization exceeds") {
				t.Fatal(stream, ingest, err)
			}
			after, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
			if err != nil || after != root {
				t.Fatal("rejected Arrow input published metadata", stream, ingest, err)
			}
			if ingest {
				if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, "it_layout_expansion_guard"); !isStatus(err, adbc.StatusNotFound) {
					t.Fatal("rejected Arrow input left a table", stream, err)
				}
				h.exec("CREATE TABLE it_layout_expansion_guard (v BIGINT)")
				h.exec("INSERT INTO it_layout_expansion_guard VALUES (7)")
				h.expectRows("SELECT v FROM it_layout_expansion_guard", "7")
			}
		}
	}
}
