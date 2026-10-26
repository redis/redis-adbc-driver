// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestScanArrowValuesAndOwnership(t *testing.T) {
	it, items := projectionFixture()
	it.projection = newScanProjection(it, items)
	cols := []resultColumn{{"label", typeString}, {"id", typeInt64}, {"duplicate", typeString}, {"qty", typeInt32}, {"stamp", ColType{Kind: KindTimestamp}}, {rowIDField, typeInt64}}
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 2, left: -1,
		arrow: &scanArrow{scratch: make([]Value, len(cols)), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
	defer r.arrow.release()
	var expected [][]Value
	for _, raw := range []aggRow{
		{rowIDField: "1", "id": "9007199254740993", "old_label": "a\x00b", "stamp": "1234567890123456"},
		{rowIDField: "2", "id": "-9223372036854775808", nullMarker("qty"): "1"},
		{rowIDField: "11", "id": "9223372036854775807", "old_label": "", "qty": "-4"},
	} {
		row, err := it.projection.decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, row)
		if err := r.appendArrow(raw); err != nil {
			t.Fatal(err)
		}
	}
	r.arrow.ready = append(r.arrow.ready, r.arrow.builder.NewRecordBatch())
	for i, rec := range r.arrow.ready {
		lo, hi := i*2, min(i*2+2, len(expected))
		want, err := buildRecord(mem, cols, expected[lo:hi])
		if err != nil {
			t.Fatal(err)
		}
		if !array.RecordEqual(rec, want) {
			t.Errorf("batch %d differs: %v / %v", i, rec, want)
		}
		want.Release()
	}
	// Explicitly retained output must outlive scratch reuse and reader release.
	retained := r.arrow.ready[0]
	retained.Retain()
	r.arrow.release()
	if got := retained.Column(0).(*array.String).Value(0); got != "a\x00b" {
		t.Errorf("retained string changed: %q", got)
	}
	retained.Release()
}

func TestScanArrowSkippedAndPartialErrors(t *testing.T) {
	for _, mode := range []string{"offset", "limit", "partial"} {
		t.Run(mode, func(t *testing.T) {
			it, items := projectionFixture()
			it.projection = newScanProjection(it, items[:1])
			cols := []resultColumn{{"label", typeString}}
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 2, left: -1,
				arrow: &scanArrow{scratch: make([]Value, 1), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
			defer r.arrow.release()
			switch mode {
			case "offset":
				r.skip = 1
			case "limit":
				r.left = 0
			case "partial":
				if err := r.appendArrow(aggRow{rowIDField: "1", "id": "1", "old_label": "ok"}); err != nil {
					t.Fatal(err)
				}
			}
			// id is required but unselected. Skipping it must not hide corruption.
			if err := r.appendArrow(aggRow{rowIDField: "2", "id": "bad"}); err == nil {
				t.Fatal("ignored invalid unselected field")
			}
		})
	}
}

func TestScanArrowPartialBuilderError(t *testing.T) {
	it, items := projectionFixture()
	it.projection = newScanProjection(it, items[:2])
	// Force a result-type coercion error after the first column was appended.
	cols := []resultColumn{{"label", typeString}, {"id", typeInt16}}
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 2, left: -1,
		arrow: &scanArrow{scratch: make([]Value, 2), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
	defer r.arrow.release()
	if err := r.appendArrow(aggRow{rowIDField: "1", "id": "9007199254740993", "old_label": "ok"}); err == nil {
		t.Fatal("ignored result coercion error")
	}
	if r.arrow.rows != 0 || len(r.arrow.ready) != 0 {
		t.Fatal("published partial row")
	}
}

// Both alternatives include typed decode, fitting, coercion and complete Arrow
// construction. Raw reply maps are prebuilt and excluded from this diagnostic.
func BenchmarkScanArrow(b *testing.B) {
	for _, width := range []int{3, 105} {
		meta := &tableMeta{Schema: defaultSchema, Name: "wide", KeyPrefix: "wide:"}
		it := &scanIter{meta: meta, need: map[string]bool{}}
		raw := aggRow{rowIDField: "1"}
		var items []planItem
		var cols []resultColumn
		for i := 0; i < width; i++ {
			name := fmt.Sprintf("c%d", i)
			meta.Columns = append(meta.Columns, columnMeta{Name: name, Type: typeString})
			it.need[name], raw[name] = true, "payload-0123456789"
			items = append(items, planItem{expr: &ColumnRef{Name: name}})
			cols = append(cols, resultColumn{Name: name, Type: typeString})
		}
		it.projection = newScanProjection(it, items)
		for _, direct := range []bool{false, true} {
			b.Run(fmt.Sprintf("columns=%d/direct=%t", width, direct), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if direct {
						r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1024, left: -1,
							arrow: &scanArrow{scratch: make([]Value, width), builder: array.NewRecordBuilder(memory.DefaultAllocator, resultSchema(cols))}}
						for range 1024 {
							if err := r.appendArrow(raw); err != nil {
								b.Fatal(err)
							}
						}
						r.arrow.release()
					} else {
						rows := make([][]Value, 1024)
						for i := range rows {
							var err error
							rows[i], err = it.projection.decode(raw)
							if err != nil {
								b.Fatal(err)
							}
							if err := fitRow(rows[i], cols, utcZone); err != nil {
								b.Fatal(err)
							}
						}
						rec, err := buildRecord(memory.DefaultAllocator, cols, rows)
						if err != nil {
							b.Fatal(err)
						}
						rec.Release()
					}
				}
			})
		}
	}
}
