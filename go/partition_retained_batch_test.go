// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestRetainedArrowBatchAccounting(t *testing.T) {
	recordType := recordOf([]ColType{typeBinary, arrayOf(typeString)})
	cols := []resultColumn{{"nested", recordType}, {"decimal128", decimalType(38, 0)}, {"decimal256", decimalType(76, 0)}}
	var rows [][]Value
	for _, label := range []string{"first", "a\x00é", strings.Repeat("x", 8192), "last"} {
		nested := recordValue([]Value{binaryValue("\x00\xff"), arrayValue([]Value{stringValue(label), nullValue(typeString)}, typeString)})
		nested.T = recordType
		rows = append(rows, []Value{nested, decimalValue(big.NewInt(-256), 38, 0), decimalValue(new(big.Int).Lsh(big.NewInt(1), 200), 76, 0)})
	}
	rows = append(rows, []Value{nullValue(recordType), nullValue(decimalType(38, 0)), nullValue(decimalType(76, 0))})
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	record, err := buildRecord(mem, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()
	// Nonzero offsets exercise sliced struct children and nested list offsets.
	for start := range rows {
		slice := record.NewSlice(int64(start), int64(len(rows)))
		for i, row := range rows[start:] {
			if got, want := retainedArrowRowBytes(slice, int64(i)), batchRowBytes(row); got != want {
				t.Fatalf("slice %d row %d: got %d want %d", start, i, got, want)
			}
		}
		slice.Release()
	}
}

func TestRetainedReaderBatchByteLimit(t *testing.T) {
	labels := []string{"first", "a\x00é", strings.Repeat("x", 8192), "last", "tail"}
	for _, budget := range []int64{0, 1, 520, 900, 4096} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			var rows [][]Value
			for _, label := range labels {
				rows = append(rows, []Value{stringValue(label)})
			}
			record, err := buildRecord(mem, []resultColumn{{"label", typeString}}, rows)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			r := &retainedReader{ctx: ctx, cancel: cancel, pending: record, schema: record.Schema(), batchRows: 3, batchBytes: budget}
			r.refs.Store(1)
			defer r.Release()
			at := 0
			for r.Next() {
				batch := r.RecordBatch()
				if batch.NumRows() < 1 || batch.NumRows() > 3 {
					t.Fatal("invalid batch row count", batch.NumRows())
				}
				var cost int64
				for i := int64(0); i < batch.NumRows(); i++ {
					if got := batch.Column(0).(*array.String).Value(int(i)); got != labels[at] {
						t.Fatal("changed row", at, got)
					}
					cost += batchRowBytes(rows[at])
					at++
				}
				if budget > 0 && batch.NumRows() > 1 && cost > budget {
					t.Fatal("byte limit exceeded", cost, budget)
				}
			}
			if r.Err() != nil || at != len(labels) {
				t.Fatal("incomplete replay", at, r.Err())
			}
		})
	}
}
