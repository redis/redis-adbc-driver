// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestStreamByteBudgetSettings(t *testing.T) {
	var s streamSettings
	if s.get(OptionIntStreamBatchBytes) != "0" || s.rows() != 65536 {
		t.Fatal(s)
	}
	for _, v := range []string{"1", "0", "9223372036854775807"} {
		if err := s.set(OptionIntStreamBatchBytes, v); err != nil || s.get(OptionIntStreamBatchBytes) != v {
			t.Fatal(v, err, s)
		}
	}
	for _, v := range []string{"-1", "9223372036854775808", "1.5", ""} {
		if err := s.set(OptionIntStreamBatchBytes, v); err == nil || s.batchBytes != math.MaxInt64 {
			t.Fatal(v, err, s)
		}
	}
	if addBatchBytes(math.MaxInt64-1, 3) != math.MaxInt64 {
		t.Fatal("overflow")
	}
}

func TestStreamByteBudgetAccounting(t *testing.T) {
	cases := []struct {
		v    Value
		want int64
	}{
		{intValue(typeInt64, math.MaxInt64), 256}, {nullValue(typeString), 256},
		{stringValue("a\x00b"), 262}, {binaryValue("abcd"), 264},
		{decimalValue(big.NewInt(256), 38, 0), 290},
		{recordValue([]Value{stringValue("x"), nullValue(typeInt32)}), 770},
	}
	for _, tc := range cases {
		r := &streamReader{cols: []resultColumn{{"v", tc.v.T}}}
		if n := r.rowBatchBytes([]Value{tc.v}); n != tc.want {
			t.Fatalf("prebound accounting %d want %d", n, tc.want)
		}
		if n := batchRowBytes([]Value{tc.v}); n != tc.want {
			t.Fatalf("%v: %d want %d", tc.v, n, tc.want)
		}
	}
}

func TestScanArrowByteBudget(t *testing.T) {
	for _, budget := range []int64{1, 520, 1024, math.MaxInt64} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			it, items := projectionFixture()
			it.projection = newScanProjection(it, items[:1])
			cols := []resultColumn{{"label", typeString}}
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 3, batchBytes: budget, left: -1,
				arrow: &scanArrow{scratch: make([]Value, 1), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
			defer r.arrow.release()
			labels := []string{"a\x00b", "", strings.Repeat("z", 4096), "last", "tail"}
			for _, v := range labels {
				if err := r.appendArrow(aggRow{rowIDField: "1", "id": "1", "old_label": v}); err != nil {
					t.Fatal(err)
				}
			}
			if r.arrow.rows > 0 {
				r.arrow.ready = append(r.arrow.ready, r.arrow.builder.NewRecordBatch())
			}
			at := 0
			for _, rec := range r.arrow.ready {
				if rec.NumRows() == 0 || rec.NumRows() > 3 {
					t.Fatal("row bound", rec.NumRows())
				}
				var cost int64
				for i := 0; i < int(rec.NumRows()); i++ {
					got := rec.Column(0).(*array.String).Value(i)
					if got != labels[at] {
						t.Fatal(at, got)
					}
					at++
					cost += 256 + 2*int64(len(got))
				}
				if rec.NumRows() > 1 && cost > budget {
					t.Fatal("byte bound", cost, budget)
				}
			}
			if at != len(labels) {
				t.Fatal("lost rows", at)
			}
			keep := r.arrow.ready[0]
			keep.Retain()
			r.arrow.release()
			if keep.Column(0).(*array.String).Value(0) != labels[0] {
				t.Fatal("ownership")
			}
			keep.Release()
		})
	}
}
