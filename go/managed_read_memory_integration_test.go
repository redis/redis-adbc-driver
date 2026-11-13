// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func memoryString(id int64) (string, bool) {
	if id%19 == 0 {
		return "", true
	}
	if id%23 == 0 {
		return "", false
	}
	return fmt.Sprintf("row-%d\x00λ", id), false
}

func memoryRecord(rows int) arrow.RecordBatch {
	base := followupRecord(rows)
	defer base.Release()
	fields := base.Schema().Fields()
	fields[104].Type = arrow.BinaryTypes.String
	columns := make([]arrow.Array, len(fields))
	for i := range columns {
		columns[i] = base.Column(i)
	}
	b := array.NewStringBuilder(memory.DefaultAllocator)
	for id := int64(1); id <= int64(rows); id++ {
		value, null := memoryString(id)
		if null {
			b.AppendNull()
		} else {
			b.Append(value)
		}
	}
	strings := b.NewArray()
	b.Release()
	defer strings.Release()
	columns[104] = strings
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, int64(rows))
}

func validateMemoryRecord(t *testing.T, rec arrow.RecordBatch, seen []bool) {
	t.Helper()
	if rec.NumCols() != 105 {
		t.Fatal("wrong memory fixture width", rec.NumCols())
	}
	ids := rec.Column(0).(*array.Int64)
	for row := 0; row < int(rec.NumRows()); row++ {
		id := ids.Value(row)
		if id < 1 || id > int64(len(seen)) || seen[id-1] {
			t.Fatal("invalid or duplicate memory fixture row", id)
		}
		seen[id-1] = true
		for col := 0; col < 104; col++ {
			a := rec.Column(col).(*array.Int64)
			null := col == 1 && id%17 == 0
			if a.IsNull(row) != null || !null && a.Value(row) != followupValue(id, col) {
				t.Fatal("incorrect memory fixture integer/NULL", id, col)
			}
		}
		a := rec.Column(104).(*array.String)
		value, null := memoryString(id)
		if a.IsNull(row) != null || !null && a.Value(row) != value {
			t.Fatal("incorrect memory fixture string/NULL", id)
		}
	}
}

func TestManagedHashReplyReleaseArrowOwnership(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		for _, mode := range []string{"finish", "release", "cancel"} {
			t.Run(fmt.Sprintf("candidate_%t/%s", candidate, mode), func(t *testing.T) {
				h, _, _ := newSnapshotHarnessMode(t, false, false)
				rec := memoryRecord(9001)
				defer rec.Release()
				if err := followupIngest(h.ctx, h.conn, "memory_ownership", adbc.OptionValueIngestModeCreate, rec); err != nil {
					t.Fatal(err)
				}
				query := "SELECT * FROM memory_ownership"
				if candidate {
					query += " WHERE id >= 1"
				}
				st, reader, count := h.openStmtStream(h.ctx, h.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "512"})
				released := false
				defer func() {
					if !released {
						reader.Release()
					}
				}()
				if count != -1 || !reader.Next() {
					t.Fatal("fixture did not stream", count, reader.Err())
				}
				stream := inspectionReader(reader).(*streamReader)
				if stream.it == nil || !stream.it.hashFetch || (stream.it.prefetch != nil) != scanPrefetchEnabled {
					t.Fatal("fixture did not select the build's expected raw HASH lookahead")
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
				switch mode {
				case "finish":
					for reader.Next() {
						validateMemoryRecord(t, reader.RecordBatch(), seen)
					}
					if err := reader.Err(); err != nil {
						t.Fatal(err)
					}
					for _, present := range seen {
						if !present {
							t.Fatal("incomplete memory fixture result")
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
				if stream.it != nil && stream.it.prefetch != nil {
					t.Fatal("raw-page producer outlived reader teardown")
				}
				// Every value remains exact after page release, Next and complete
				// teardown; NULL/empty/binary strings must remain distinct.
				validateMemoryRecord(t, retained, make([]bool, 9001))
			})
		}
	}
}
