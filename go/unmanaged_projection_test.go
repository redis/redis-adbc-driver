// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func unmanagedProjectionFixture() (*scanIter, []planItem) {
	m := &tableMeta{Schema: defaultSchema, Name: "live", KeyPrefix: "app:", IndexName: "app_idx", Adopted: true, KeyRows: true, AppIndex: true, Unmanaged: true, UnmanagedPrefixes: []string{"app:"}, Columns: []columnMeta{
		{Name: "big", Field: "stored_big", Type: typeInt64},
		{Name: "label", Field: "stored_label", Type: typeString},
		{Name: "flag", Type: typeBool, Encoding: "text"},
		{Name: "stamp", Type: ColType{Kind: KindTimestamp}, Encoding: "text"},
		{Name: "blob", Type: ColType{Kind: KindBinary}},
	}}
	it := &scanIter{meta: m, need: map[string]bool{"big": true, "label": true, "flag": true, "stamp": true, "blob": true, keyField: true}, env: &evalEnv{ctx: context.Background()}}
	var items []planItem
	for _, n := range []string{keyField, "label", "big", "label", "flag", "stamp", "blob", keyField} {
		items = append(items, planItem{expr: &ColumnRef{Name: n}})
	}
	return it, items
}

func TestUnmanagedScanProjectionValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(fmt.Sprint("wide=", wide), func(t *testing.T) {
			it, items := unmanagedProjectionFixture()
			if wide {
				items = append(items, items...)
				for len(items) < 64 {
					items = append(items, items...)
				}
			}
			p := newScanProjection(it, items)
			if p == nil || p.hashFields != nil {
				t.Fatal("unmanaged scan did not select explicit application-key projection")
			}
			for _, raw := range []aggRow{
				{keyField: "app:\x00é", rowIDField: "not an integer", "stored_big": "9007199254740993", "stored_label": "a\x00雪", "flag": "YES", "stamp": "2026-11-13 12:34:56.123456", "blob": "\x00\xff"},
				{keyField: "app:empty", "stored_big": "-9223372036854775808", "stored_label": "", "flag": "false"},
				{keyField: "app:missing", "stored_big": "9223372036854775807"},
				{keyField: "app:bad-int", "stored_big": "bad"},
				{keyField: "app:bad-bool", "flag": "not boolean"},
				{keyField: "app:bad-time", "stamp": "not timestamp"},
				{keyField: metaPrefix + "bad", "stored_big": "1"},
				{},
			} {
				got, ge := p.decode(raw)
				values, we := decodeRow(it.meta, raw, it.need)
				var want []Value
				if we == nil {
					want, we = evalItems(&evalEnv{row: values, types: it.meta.types()}, items)
				}
				if fmt.Sprint(ge) != fmt.Sprint(we) || !reflect.DeepEqual(got, want) {
					t.Fatalf("raw=%v\ngot=%v %v\nwant=%v %v", raw, got, ge, want, we)
				}
			}
			// Decode all required inputs before output, including malformed unselected fields.
			p = newScanProjection(it, []planItem{{expr: &ColumnRef{Name: "label"}}})
			if _, err := p.decode(aggRow{keyField: "app:bad", "stored_big": "bad"}); err == nil {
				t.Fatal("ignored invalid required input")
			}
		})
	}
}

func TestUnmanagedScanProjectionFallbacks(t *testing.T) {
	for _, change := range []func(*scanIter){
		func(it *scanIter) { it.residual = &Literal{} },
		func(it *scanIter) { it.ties = &tieScan{} },
		func(it *scanIter) { it.late = &lateProjection{} },
		func(it *scanIter) { it.flexUnmanaged = &flexUnmanagedScan{} },
		func(it *scanIter) { it.hashFetch = true },
		func(it *scanIter) { it.meta.Unmanaged = false },
		func(it *scanIter) { it.meta.KeyRows = false },
		func(it *scanIter) { it.meta.AppIndex = false },
		func(it *scanIter) { it.meta.SnapshotSourceKey = true },
		func(it *scanIter) { it.meta.SnapshotOverlay = true },
		func(it *scanIter) { it.meta.Rewriting = true },
		func(it *scanIter) { it.meta.RekeyTo = "new:" },
	} {
		it, items := unmanagedProjectionFixture()
		change(it)
		if newScanProjection(it, items) != nil {
			t.Fatal("unsupported source entered unmanaged projection")
		}
	}
	it, _ := unmanagedProjectionFixture()
	for _, expr := range []Expr{&ColumnRef{Name: rowIDField}, &Literal{}, &ColumnRef{Name: "label", Outer: 1}} {
		if newScanProjection(it, []planItem{{expr: expr}}) != nil {
			t.Fatalf("accepted unsupported expression %T %+v", expr, expr)
		}
	}
}

func TestUnmanagedScanProjectionArrowOwnershipAndErrors(t *testing.T) {
	it, items := unmanagedProjectionFixture()
	it.projection = newScanProjection(it, items)
	if it.projection == nil {
		t.Fatal("unmanaged projection absent")
	}
	cols := []resultColumn{{keyField, typeString}, {"label", typeString}, {"big", typeInt64}, {"duplicate", typeString}, {"flag", typeBool}, {"stamp", ColType{Kind: KindTimestamp}}, {"blob", ColType{Kind: KindBinary}}, {"key_again", typeString}}
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1, left: -1, arrow: &scanArrow{scratch: make([]Value, len(cols)), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
	defer r.arrow.release()
	raw := aggRow{keyField: "app:\x00key", "stored_big": "9007199254740993", "stored_label": "雪\x00", rowIDField: "arbitrary app field"}
	if err := r.appendArrow(raw); err != nil {
		t.Fatal(err)
	}
	rec := r.arrow.ready[0]
	rec.Retain()
	defer rec.Release()
	// Scratch reuse and release must not alter already retained Arrow data.
	raw[keyField], raw["stored_label"] = "app:second", "next"
	if err := r.appendArrow(raw); err != nil {
		t.Fatal(err)
	}
	raw[keyField] = metaPrefix + "must-reject"
	if err := r.appendArrow(raw); !isStatus(err, adbc.StatusInvalidArgument) {
		t.Fatalf("namespace error=%v", err)
	}
	r.arrow.release()
	if rec.Column(0).(*array.String).Value(0) != "app:\x00key" || rec.Column(1).(*array.String).Value(0) != "雪\x00" || rec.Column(2).(*array.Int64).Value(0) != 9007199254740993 {
		t.Fatal("retained output changed")
	}
}

func TestUnmanagedProjectionKeepCancellationAndNilRows(t *testing.T) {
	it, items := unmanagedProjectionFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	it.env.ctx = ctx
	it.projection = newScanProjection(it, items)
	it.diagnostics = &executionDiagnostics{}
	calls := 0
	it.projection.consume = func(raw aggRow) error { calls++; cancel(); return nil }
	_, _, err := it.keep([]string{"app:gone", "app:one", "app:two"}, []aggRow{nil, {keyField: "app:one"}, {keyField: "app:two"}})
	if !isStatus(err, adbc.StatusCancelled) || calls != 1 || it.diagnostics.processed.Load() != 3 {
		t.Fatalf("calls=%d err=%v diagnostics=%+v", calls, err, it.diagnostics)
	}
	it, items = unmanagedProjectionFixture()
	it.projection = newScanProjection(it, items)
	keys, rows, err := it.keep([]string{"app:gone", "app:all-null"}, []aggRow{nil, {keyField: "app:all-null", "unrelated": "present"}})
	if err != nil || len(keys) != 1 || keys[0] != "app:all-null" || len(rows) != 0 || len(it.projection.rows) != 1 || !it.projection.rows[0][2].Null {
		t.Fatalf("keys=%v rows=%v err=%v", keys, rows, err)
	}
}

func BenchmarkUnmanagedWideArrowConversion(b *testing.B) {
	for _, direct := range []bool{false, true} {
		b.Run(fmt.Sprintf("direct_%t", direct), func(b *testing.B) {
			m := &tableMeta{Schema: defaultSchema, Name: "live", KeyPrefix: "app:", IndexName: "idx", Adopted: true, KeyRows: true, AppIndex: true, Unmanaged: true, UnmanagedPrefixes: []string{"app:"}}
			it := &scanIter{meta: m, need: map[string]bool{}}
			raw := aggRow{keyField: "app:key", rowIDField: "not an internal ID"}
			var items []planItem
			var cols []resultColumn
			for col := 0; col < 105; col++ {
				name := fmt.Sprintf("c%d", col)
				m.Columns = append(m.Columns, columnMeta{Name: name, Type: typeInt64})
				it.need[name] = true
				items = append(items, planItem{expr: &ColumnRef{Name: name}})
				cols = append(cols, resultColumn{Name: name, Type: typeInt64})
				raw[name] = "9007199254740993"
			}
			it.projection = newScanProjection(it, items)
			if it.projection == nil {
				b.Fatal("projection absent")
			}
			convert := func() arrow.RecordBatch {
				if direct {
					r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 256, left: -1, arrow: &scanArrow{scratch: make([]Value, len(cols)), builder: array.NewRecordBuilder(memory.DefaultAllocator, resultSchema(cols))}}
					for range 256 {
						if err := r.appendArrow(raw); err != nil {
							b.Fatal(err)
						}
					}
					if len(r.arrow.ready) != 1 {
						b.Fatal("incomplete direct output")
					}
					rec := r.arrow.ready[0]
					r.arrow.ready = nil
					r.arrow.release()
					return rec
				}
				rows := make([][]Value, 0, 256)
				for range 256 {
					vals, err := decodeRow(m, raw, it.need)
					if err != nil {
						b.Fatal(err)
					}
					row, err := evalItems(&evalEnv{row: vals}, items)
					if err != nil {
						b.Fatal(err)
					}
					rows = append(rows, row)
				}
				rec, err := buildRecord(memory.DefaultAllocator, cols, rows)
				if err != nil {
					b.Fatal(err)
				}
				return rec
			}
			validate := func(rec arrow.RecordBatch) {
				defer rec.Release()
				if rec.NumRows() != 256 || rec.NumCols() != 105 {
					b.Fatal("incomplete output")
				}
				for col := 0; col < 105; col++ {
					a, ok := rec.Column(col).(*array.Int64)
					if !ok {
						b.Fatal("wrong physical type")
					}
					for row := 0; row < 256; row++ {
						if a.IsNull(row) || a.Value(row) != 9007199254740993 {
							b.Fatal("wrong value")
						}
					}
				}
			}
			validate(convert())
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rec := convert()
				rec.Release()
			}
			b.StopTimer()
			validate(convert())
		})
	}
}

func TestUnmanagedWideProjectionExternalErrorParity(t *testing.T) {
	it, _ := unmanagedProjectionFixture()
	it.meta.Columns = append(it.meta.Columns, []columnMeta{{Name: "small", Type: typeInt16}, {Name: "native_bool", Type: typeBool}, {Name: "day", Type: ColType{Kind: KindDate}}, {Name: "clock", Type: ColType{Kind: KindTime}}, {Name: "later", Type: typeInt64}}...)
	for _, c := range it.meta.Columns {
		it.need[c.Name] = true
	}
	var items []planItem
	for range 10 {
		for _, c := range it.meta.Columns {
			items = append(items, planItem{expr: &ColumnRef{Name: c.Name}})
		}
	}
	p := newScanProjection(it, items)
	if p == nil {
		t.Fatal("wide unmanaged projection absent")
	}
	for _, early := range []struct{ field, value string }{{"small", "32768"}, {"small", "-32769"}, {"native_bool", "2"}, {"native_bool", "not boolean"}, {"day", "9223372036854775807"}, {"day", "bad date"}, {"clock", "9223372036854775807"}, {"clock", "bad time"}} {
		for _, laterBad := range []bool{false, true} {
			raw := aggRow{keyField: "app:external", early.field: early.value}
			if laterBad {
				raw["later"] = "bad later integer"
			}
			got, ge := p.decode(raw)
			values, we := decodeRow(it.meta, raw, it.need)
			var want []Value
			if we == nil {
				want, we = evalItems(&evalEnv{row: values, types: it.meta.types()}, items)
			}
			if fmt.Sprint(ge) != fmt.Sprint(we) || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s=%q laterBad=%t: got=%v %v / want=%v %v", early.field, early.value, laterBad, got, ge, want, we)
			}
		}
	}
}
