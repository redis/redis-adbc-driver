// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func filteredComparison(op, name string, value int64) Expr {
	return &Binary{Op: op, L: &ColumnRef{Name: name}, R: &Literal{V: intValue(typeInt64, value)}}
}

func TestUnmanagedFilteredProjectionParity(t *testing.T) {
	for _, predicate := range []Expr{
		filteredComparison(">=", "big", 9007199254740993),
		&Binary{Op: "AND", L: filteredComparison(">", "big", 0), R: &ColumnRef{Name: "flag"}},
		&Binary{Op: "OR", L: &ColumnRef{Name: "flag"}, R: filteredComparison("<", "big", 0)},
		&Unary{Op: "NOT", X: &IsNull{X: &ColumnRef{Name: "big"}}},
		&Binary{Op: "IS DISTINCT FROM", L: &ColumnRef{Name: "big"}, R: &Param{Index: 0}},
		&Binary{Op: "AND", L: &ColumnRef{Name: "flag"}, R: &Binary{Op: "=", L: &ColumnRef{Name: "big"}, R: &ColumnRef{Name: "flag"}}},
		&Binary{Op: "OR", L: &ColumnRef{Name: "flag"}, R: &Binary{Op: "=", L: &ColumnRef{Name: "big"}, R: &ColumnRef{Name: "flag"}}},
	} {
		it, items := unmanagedProjectionFixture()
		it.residual = predicate
		it.env.params = []Value{nullValue(typeInt64)}
		it.diagnostics = &executionDiagnostics{}
		it.projection = newScanProjection(it, items)
		if it.projection == nil || it.projection.predicate == nil {
			t.Fatalf("eligible predicate missing: %+v", predicate)
		}
		for _, raw := range []aggRow{
			{keyField: "app:\x00λ", "stored_big": "9007199254740993", "stored_label": "a\x00雪", "flag": "yes"},
			{keyField: "app:false", "stored_big": "-9223372036854775808", "flag": "no"},
			{keyField: "app:null"},
			{keyField: "app:late-error", "stored_big": "-1", "flag": "false", "stamp": "malformed timestamp"},
			{keyField: "app:early-error", "stored_big": "malformed", "flag": "false", "stamp": "later malformed"},
			{keyField: "app:bad-bool", "stored_big": "1", "flag": "bad"},
			{keyField: metaPrefix + "forbidden", "stored_big": "1"},
		} {
			clear(it.projection.rows)
			it.projection.rows = it.projection.rows[:0]
			keys, _, gotErr := it.keep([]string{raw[keyField]}, []aggRow{raw})
			values, wantErr := decodeRow(it.meta, raw, it.need)
			var want [][]Value
			if wantErr == nil {
				env := &evalEnv{ctx: context.Background(), row: values, types: it.meta.types(), params: it.env.params}
				v, err := env.eval(predicate)
				if err != nil {
					wantErr = invalidArg(err)
				} else if matched, valid := truthy(v); valid && matched {
					row, err := evalItems(env, items)
					wantErr = err
					want = append(want, row)
				}
			}
			if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
				t.Fatalf("predicate=%+v raw=%v: errors got=%v want=%v", predicate, raw, gotErr, wantErr)
			}
			var gotADBC, wantADBC adbc.Error
			if errors.As(gotErr, &gotADBC) != errors.As(wantErr, &wantADBC) || gotADBC.Code != wantADBC.Code || gotADBC.SqlState != wantADBC.SqlState {
				t.Fatalf("status/SQLSTATE changed: got=%+v want=%+v", gotADBC, wantADBC)
			}
			if wantErr == nil && (len(keys) != len(want) || len(it.projection.rows) != len(want) || len(want) != 0 && !reflect.DeepEqual(it.projection.rows, want)) {
				t.Fatalf("predicate=%+v raw=%v: rows got=%v want=%v", predicate, raw, it.projection.rows, want)
			}
		}
	}
}

func TestUnmanagedFilteredProjectionTypedParams(t *testing.T) {
	for _, parameter := range []Value{intValue(typeInt64, 9007199254740993), nullValue(typeInt64), boolValue(true)} {
		it, items := unmanagedProjectionFixture()
		it.residual = &Binary{Op: "=", L: &ColumnRef{Name: "big"}, R: &Param{Index: 0}}
		it.env.params = []Value{parameter}
		it.projection = newScanProjection(it, items)
		if it.projection == nil {
			t.Fatal("typed parameter fallback", parameter)
		}
		raw := aggRow{keyField: "app:parameter", "stored_big": "9007199254740993"}
		_, _, gotErr := it.keep([]string{raw[keyField]}, []aggRow{raw})
		values, err := decodeRow(it.meta, raw, it.need)
		if err != nil {
			t.Fatal(err)
		}
		v, wantErr := (&evalEnv{row: values, types: it.meta.types(), params: []Value{parameter}}).eval(it.residual)
		if wantErr != nil {
			wantErr = invalidArg(wantErr)
		}
		matched, valid := truthy(v)
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || gotErr == nil && (len(it.projection.rows) == 1) != (matched && valid) {
			t.Fatalf("parameter=%v got=%v/%v want=%v/%v", parameter, it.projection.rows, gotErr, v, wantErr)
		}
	}
}

func TestUnmanagedFilteredProjectionFallbacks(t *testing.T) {
	for _, predicate := range []Expr{
		&Binary{Op: ">", L: &Binary{Op: "+", L: &ColumnRef{Name: "big"}, R: &Literal{V: intValue(typeInt64, 1)}}, R: &Literal{V: intValue(typeInt64, 0)}},
		&Binary{Op: "=", L: &ColumnRef{Name: "big"}, R: &Literal{Unknown: true, V: stringValue("1")}},
		&Binary{Op: "=", L: &ColumnRef{Name: "label"}, R: &Literal{V: stringValue("x")}},
		&Binary{Op: "=", L: &ColumnRef{Name: "big"}, R: &Param{Index: 0}},
		&IsNull{X: &ColumnRef{Name: "missing"}},
		&IsNull{X: &ColumnRef{Name: "big", Outer: 1}},
		&Func{Name: "RANDOM"},
	} {
		it, items := unmanagedProjectionFixture()
		it.residual = predicate
		if newScanProjection(it, items) != nil {
			t.Fatalf("unsupported predicate accepted: %+v", predicate)
		}
	}
	it, items := unmanagedProjectionFixture()
	it.residual = filteredComparison(">", "big", 0)
	for range 32 {
		it.residual = &Unary{Op: "NOT", X: it.residual}
	}
	if newScanProjection(it, items) != nil {
		t.Fatal("unbounded predicate admitted")
	}
}

func TestUnmanagedFilteredArrowAccountingAndTail(t *testing.T) {
	it, items := unmanagedProjectionFixture()
	it.residual = filteredComparison(">=", "big", 2)
	it.diagnostics = &executionDiagnostics{}
	it.projection = newScanProjection(it, items)
	if it.projection == nil {
		t.Fatal("filtered projection absent")
	}
	cols := []resultColumn{{keyField, typeString}, {"label", typeString}, {"big", typeInt64}, {"duplicate", typeString}, {"flag", typeBool}, {"stamp", ColType{Kind: KindTimestamp}}, {"blob", ColType{Kind: KindBinary}}, {"key_again", typeString}}
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1, skip: 1, left: 1, arrow: &scanArrow{scratch: make([]Value, len(cols)), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
	defer r.arrow.release()
	it.projection.predicateRow = r.arrow.scratch
	it.projection.consumeMatched = r.appendDecodedArrow
	keys, rows, err := it.keep([]string{"app:1", "app:2", "app:\x00λ", "app:4"}, []aggRow{
		{keyField: "app:1", "stored_big": "1"}, {keyField: "app:2", "stored_big": "2"}, {keyField: "app:\x00λ", "stored_big": "3", "stored_label": "雪\x00"}, {keyField: "app:4", "stored_big": "4"},
	})
	if err != nil || len(keys) != 3 || len(rows) != 0 || len(r.arrow.ready) != 1 || r.left != 0 || r.skip != 0 || it.diagnostics.processed.Load() != 4 || it.diagnostics.matched.Load() != 3 {
		t.Fatalf("keys=%v rows=%v error=%v ready=%d skip/left=%d/%d counters=%d/%d", keys, rows, err, len(r.arrow.ready), r.skip, r.left, it.diagnostics.processed.Load(), it.diagnostics.matched.Load())
	}
	rec := r.arrow.ready[0]
	rec.Retain()
	defer rec.Release()
	_, _, err = it.keep([]string{"app:rejected-bad-tail"}, []aggRow{{keyField: "app:rejected-bad-tail", "stored_big": "0", "stamp": "malformed timestamp"}})
	if !isStatus(err, adbc.StatusInternal) {
		t.Fatalf("LIMIT/nonmatching tail skipped decode error: %v", err)
	}
	r.arrow.release()
	if rec.Column(0).(*array.String).Value(0) != "app:\x00λ" || rec.Column(1).(*array.String).Value(0) != "雪\x00" || rec.Column(2).(*array.Int64).Value(0) != 3 {
		t.Fatal("retained filtered Arrow output changed")
	}
}
