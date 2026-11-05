// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
			var wantArray arrow.Array
			var err error
			if containsTaggedNested(c.want.DataType()) {
				var rows []json.RawMessage
				for _, col := range columns {
					data, err := col.MarshalJSON()
					if err != nil {
						t.Fatal(err)
					}
					var batch []json.RawMessage
					if err := json.Unmarshal(data, &batch); err != nil {
						t.Fatal(err)
					}
					rows = append(rows, batch...)
				}
				data, e := json.Marshal(rows)
				if e != nil {
					t.Fatal(e)
				}
				if rows == nil {
					data = []byte("[]")
				}
				wantArray = inventoryJSON(t, memory.DefaultAllocator, c.want.DataType(), string(data))
			} else {
				wantArray, err = array.Concatenate(columns, memory.DefaultAllocator)
			}
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
			if containsTaggedNested(c.want.DataType()) {
				h.expectRows("SELECT COUNT(*) FROM "+table+" WHERE v = v", fmt.Sprint(want.NumRows()-int64(want.Column(0).NullN())))
				h.expectRows("SELECT COUNT(DISTINCT v) FROM "+table, fmt.Sprint(c.want.Len()-c.want.NullN()))
				h.expectError("SELECT v FROM "+table+" WHERE v", "Map/Union")
				for _, format := range []string{"stream", "file"} {
					path := filepath.Join(t.TempDir(), "query.arrow")
					n, err := writeIPCOutput(h.ctx, path, format, memory.DefaultAllocator, func() (array.RecordReader, error) {
						reader, _ := h.openStream(h.ctx, worker.conn, query+" ORDER BY __rowid", nil, nil)
						return reader, nil
					})
					if err != nil || n != want.NumRows() {
						t.Fatal("query IPC", n, err)
					}
					reader, err := openIPCInput(h.ctx, path, format, memory.DefaultAllocator)
					if err != nil {
						t.Fatal(err)
					}
					assertInventoryReader(t, reader, want)
					reader.Release()
				}
				// These scalars must reject even for empty result sets, rather than
				// interpreting the container's unused scalar payload as empty text.
				for _, expr := range []string{"UPPER(v)", "CONCAT(v)", "TO_JSON(v)", "CAST(v AS VARCHAR)", "v || 'x'"} {
					h.expectError("SELECT "+expr+" FROM "+table+" WHERE false", "Map/Union")
				}
			}
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
			r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			assertInventoryUnorderedReader(t, r, want)
			r.Release()
			if containsTaggedNested(c.want.DataType()) {
				// A second independent replay uses the same portable descriptor.
				replay := newSQLHarness(t)
				r, err = replay.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
				if err != nil {
					t.Fatal(err)
				}
				assertInventoryUnorderedReader(t, r, want)
				r.Release()
				ctx, cancel := context.WithCancel(h.ctx)
				reader, _ := h.openStream(ctx, worker.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "1"})
				if !reader.Next() {
					t.Fatal(reader.Err())
				}
				held := reader.RecordBatch()
				held.Retain()
				before, _ := held.Column(0).MarshalJSON()
				cancel()
				if reader.Next() || reader.Err() == nil {
					t.Fatal("cancelled nested reader returned a batch")
				}
				reader.Release()
				after, _ := held.Column(0).MarshalJSON()
				held.Release()
				if string(before) != string(after) {
					t.Fatal("cancellation invalidated retained batch")
				}
				// A complete subsequent query demonstrates usable connection state.
				reader, _ = h.openStream(h.ctx, worker.conn, query+" ORDER BY __rowid", nil, nil)
				assertInventoryReader(t, reader, want)
				reader.Release()
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
			if containsTaggedNested(c.want.DataType()) {
				if !isStatus(err, adbc.StatusNotImplemented) || !strings.Contains(err.Error(), "Map/Union retained") {
					t.Fatal("retained admission", err)
				}
				if retParts.NumPartitions != 0 || len(retParts.PartitionIDs) != 0 {
					t.Fatal("rejected retained result returned descriptors")
				}
				// Revocation keeps a zero-byte tombstone until the upload lease
				// expires, preventing a racing producer from publishing afterward.
				quota, e := storage.client.ZRangeWithScores(h.ctx, storage.prefix+"quota-bytes", 0, -1).Result()
				if e != nil {
					t.Fatal(e)
				}
				for _, entry := range quota {
					if entry.Score != 0 {
						t.Fatal("retained rejection reserved payload bytes", entry.Score)
					}
					job := fmt.Sprint(entry.Member)
					raw, e := storage.client.Get(h.ctx, storage.jobKey(job, "meta")).Result()
					if e != nil {
						t.Fatal(e)
					}
					var tombstone struct {
						State string `json:"state"`
					}
					if e = json.Unmarshal([]byte(raw), &tombstone); e != nil || tombstone.State != "revoked" {
						t.Fatal("missing revocation tombstone", raw, e)
					}
					if n, e := storage.client.Exists(h.ctx, storage.jobKey(job, "manifest")).Result(); e != nil || n != 0 {
						t.Fatal("rejected result retained a manifest", n, e)
					}
				}
				return
			}
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
			if ingest {
				h.expectRows("SELECT COUNT(*) FROM it_layout_expansion_guard", "0")
			}
		}
	}
}
