// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func projectionFixture() (*scanIter, []planItem) {
	meta := &tableMeta{Schema: defaultSchema, Name: "projection", KeyPrefix: "projection:", Columns: []columnMeta{
		{Name: "id", Type: typeInt64},
		{Name: "label", Field: "old_label", Type: typeString},
		{Name: "qty", Type: typeInt32, Missing: "7", MissingThrough: 10},
		{Name: "stamp", Type: ColType{Kind: KindTimestamp}},
	}}
	it := &scanIter{meta: meta, need: map[string]bool{"id": true, "label": true, "qty": true, "stamp": true}}
	var items []planItem
	for _, name := range []string{"label", "id", "label", "qty", "stamp", rowIDField} {
		items = append(items, planItem{expr: &ColumnRef{Name: name}})
	}
	return it, items
}

func TestScanProjectionValuesAndErrors(t *testing.T) {
	it, items := projectionFixture()
	p := newScanProjection(it, items)
	if p == nil {
		t.Fatal("plain managed projection was rejected")
	}
	for _, raw := range []aggRow{
		{rowIDField: "1", "id": "9007199254740993", "old_label": "a\x00b", "stamp": "1234567890123456"},
		{rowIDField: "2", "id": "-9223372036854775808", nullMarker("qty"): "1"},
		{rowIDField: "11", "id": "9223372036854775807", "old_label": "", "qty": "-4"},
		{rowIDField: "12", "id": "1"},
		{},
		{rowIDField: "3", "id": "bad"},
		{rowIDField: "bad", "id": "1"},
		{rowIDField: "4", "stamp": "bad"},
	} {
		got, gotErr := p.decode(raw)
		vals, wantErr := decodeRow(it.meta, raw, it.need)
		var want []Value
		if wantErr == nil {
			want, wantErr = evalItems(&evalEnv{row: vals, types: it.meta.types()}, items)
		}
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || !reflect.DeepEqual(got, want) {
			t.Fatalf("raw=%v\ngot %v, %v\nwant %v, %v", raw, got, gotErr, want, wantErr)
		}
	}
	// Required but unselected inputs must still be decoded, including errors.
	p = newScanProjection(it, items[:1])
	if _, err := p.decode(aggRow{rowIDField: "1", "id": "bad"}); err == nil {
		t.Fatal("ignored invalid required input")
	}
}

func TestScanProjectionFallbacks(t *testing.T) {
	for _, change := range []func(*scanIter){
		func(it *scanIter) { it.residual = &Literal{} },
		func(it *scanIter) { it.ties = &tieScan{} },
		func(it *scanIter) { it.late = &lateProjection{} },
		func(it *scanIter) { it.meta.KeyRows = true },
		func(it *scanIter) { it.meta.Adopted = true },
		func(it *scanIter) { it.meta.Foreign = true },
		func(it *scanIter) { it.meta.AppIndex = true },
	} {
		it, items := projectionFixture()
		change(it)
		if newScanProjection(it, items) != nil {
			t.Fatal("unsupported scan entered fast path")
		}
	}
	it, _ := projectionFixture()
	for _, expr := range []Expr{&Literal{}, &ColumnRef{Name: "missing"}, &ColumnRef{Name: "id", Outer: 1}} {
		if newScanProjection(it, []planItem{{expr: expr}}) != nil {
			t.Fatalf("accepted %T %+v", expr, expr)
		}
	}
}

func TestScanProjectionKeepCancellationAndDiagnostics(t *testing.T) {
	it, items := projectionFixture()
	ctx, cancel := context.WithCancel(context.Background())
	it.env = &evalEnv{ctx: ctx}
	it.projection = newScanProjection(it, items)
	it.diagnostics = &executionDiagnostics{}
	keys, rows, err := it.keep([]string{"gone", "live"}, []aggRow{nil, {rowIDField: "1", "id": "9007199254740993"}})
	if err != nil || len(keys) != 1 || len(rows) != 0 || len(it.projection.rows) != 1 || it.diagnostics.processed.Load() != 2 || it.diagnostics.matched.Load() != 1 {
		t.Fatalf("keys=%v rows=%v error=%v diagnostics=%+v", keys, rows, err, it.diagnostics)
	}
	cancel()
	if _, _, err := it.keep([]string{"live"}, []aggRow{{rowIDField: "1"}}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

var projectionBenchmarkRow []Value

func BenchmarkWideScanProjection(b *testing.B) {
	meta := &tableMeta{Schema: defaultSchema, Name: "wide", KeyPrefix: "wide:"}
	it := &scanIter{meta: meta, need: map[string]bool{}}
	raw := aggRow{rowIDField: "1"}
	var items []planItem
	for i := 0; i < 105; i++ {
		name := fmt.Sprintf("c%d", i)
		meta.Columns = append(meta.Columns, columnMeta{Name: name, Type: typeString})
		it.need[name] = true
		raw[name] = "payload-0123456789"
		items = append(items, planItem{expr: &ColumnRef{Name: name}})
	}
	p := newScanProjection(it, items)
	for _, fast := range []bool{false, true} {
		b.Run(fmt.Sprint("slots=", fast), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if fast {
					projectionBenchmarkRow, err = p.decode(raw)
				} else {
					vals, e := decodeRow(meta, raw, it.need)
					if e != nil {
						b.Fatal(e)
					}
					projectionBenchmarkRow, err = evalItems(&evalEnv{row: vals}, items)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
