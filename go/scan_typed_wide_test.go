// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestTypedProjectionWideDefaultsAndAliases(t *testing.T) {
	it, base := projectionFixture()
	var items []planItem
	for range 11 {
		items = append(items, base...)
	}
	p := newScanProjection(it, items)
	for _, raw := range []aggRow{
		{rowIDField: "1", "id": "9007199254740993", "old_label": "a\x00b", "stamp": "1234567890123456"},
		{rowIDField: "2", "id": "-9223372036854775808", nullMarker("qty"): "1"},
		{rowIDField: "11", "id": "9223372036854775807", "qty": "2147483648"},
		{rowIDField: "1", "id": "bad"},
	} {
		got, err := p.decode(raw)
		decoded, werr := decodeRow(it.meta, raw, it.need)
		var want []Value
		if werr == nil {
			want, werr = evalItems(&evalEnv{row: decoded, types: it.meta.types()}, items)
		}
		if fmt.Sprint(err) != fmt.Sprint(werr) || !reflect.DeepEqual(got, want) {
			t.Fatalf("typed defaults/aliases differ: %v %v / %v %v", got, err, want, werr)
		}
	}
}

func TestTypedWideArrowLifetimeAndErrorOrder(t *testing.T) {
	for _, bytes := range []int64{0, 256} {
		t.Run(fmt.Sprint(bytes), func(t *testing.T) {
			meta := &tableMeta{Schema: defaultSchema, Name: "typed", KeyPrefix: "typed:"}
			it := &scanIter{meta: meta, need: map[string]bool{}}
			var items []planItem
			var cols []resultColumn
			raw := aggRow{rowIDField: "1"}
			for i := range 64 {
				n := fmt.Sprintf("c%d", i)
				meta.Columns = append(meta.Columns, columnMeta{Name: n, Type: typeInt32})
				it.need[n], raw[n] = true, "12"
				items = append(items, planItem{expr: &ColumnRef{Name: n}})
				cols = append(cols, resultColumn{Name: n, Type: typeInt32})
			}
			it.projection = newScanProjection(it, items)
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1, batchBytes: bytes, left: -1,
				arrow: &scanArrow{scratch: make([]Value, 64), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
			defer r.arrow.release()
			r.batchSizer = newBatchSizer(cols)
			if err := r.appendArrow(raw); err != nil {
				t.Fatal(err)
			}
			if len(r.arrow.typed) != 64 {
				t.Fatal("wide path was not specialized")
			}
			rec := r.arrow.ready[0]
			rec.Retain()
			defer rec.Release()
			// A width overflow must still pass through output validation.
			raw["c63"] = "2147483648"
			if err := r.appendArrow(raw); err == nil {
				t.Fatal("lost integer overflow")
			}
			// Required-field decode errors win over output coercion errors.
			raw["c62"] = "bad"
			if err := r.appendArrow(raw); err == nil || !strings.Contains(err.Error(), "c62") {
				t.Fatalf("wrong error order: %v", err)
			}
			r.arrow.release()
			if got := rec.Column(0).(*array.Int32).Value(0); got != 12 {
				t.Fatalf("retained buffer changed: %d", got)
			}
		})
	}
}
