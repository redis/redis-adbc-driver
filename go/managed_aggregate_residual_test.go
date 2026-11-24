// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestManagedAggregateResidualParity(t *testing.T) {
	for _, predicate := range []Expr{
		filteredComparison(">=", "id", 9007199254740993),
		&Binary{Op: "AND", L: filteredComparison(">=", "qty", 1), R: filteredComparison("<=", "qty", 20)},
		&Binary{Op: "OR", L: &IsNull{X: &ColumnRef{Name: "id"}}, R: filteredComparison("<", "qty", 0)},
		&Binary{Op: "IS DISTINCT FROM", L: &ColumnRef{Name: "id"}, R: &Param{Index: 0}},
	} {
		it, items := projectionFixture()
		it.residual = predicate
		it.env = &evalEnv{ctx: context.Background(), types: it.meta.types(), params: []Value{nullValue(typeInt64)}}
		if newScanProjection(it, items) != nil {
			t.Fatal("ordinary managed projection unexpectedly changed")
		}
		it.projection = newScanProjectionWithManagedPredicate(it, items, true)
		if it.projection == nil || it.projection.predicate == nil {
			t.Fatal("scalar aggregate predicate did not bind")
		}
		for _, raw := range []aggRow{
			{rowIDField: "1", "id": "9007199254740993", "old_label": "duplicate"},
			{rowIDField: "2", "id": "9007199254740993", "old_label": "duplicate"},
			{rowIDField: "3", "id": "-9223372036854775808", nullMarker("qty"): "1"},
			{rowIDField: "11", "qty": "-1"},
			{rowIDField: "12", "qty": "21", "stamp": "invalid"},
			{rowIDField: "bad"},
		} {
			it.projection.rows = nil
			_, _, gotErr := it.keep([]string{"physical"}, []aggRow{raw})
			values, wantErr := decodeRow(it.meta, raw, it.need)
			var want [][]Value
			if wantErr == nil {
				env := &evalEnv{ctx: context.Background(), types: it.meta.types(), params: it.env.params, row: values}
				v, err := env.eval(predicate)
				wantErr = err
				if matched, valid := truthy(v); err == nil && matched && valid {
					row, err := evalItems(env, items)
					wantErr = err
					want = append(want, row)
				}
			}
			if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || gotErr == nil && !reflect.DeepEqual(it.projection.rows, want) {
				t.Fatalf("raw=%v got=%v/%v want=%v/%v", raw, it.projection.rows, gotErr, want, wantErr)
			}
		}
	}
}

func TestManagedAggregateResidualFallback(t *testing.T) {
	for _, predicate := range []Expr{
		&Func{Name: "RANDOM"},
		&Binary{Op: "=", L: &Binary{Op: "/", L: &Literal{V: intValue(typeInt64, 1)}, R: &Literal{V: intValue(typeInt64, 0)}}, R: &Literal{V: intValue(typeInt64, 1)}},
		&Binary{Op: "=", L: &ColumnRef{Name: "label"}, R: &Literal{V: stringValue("a")}},
	} {
		it, items := projectionFixture()
		it.env = &evalEnv{ctx: context.Background(), types: it.meta.types()}
		it.residual = predicate
		if newScanProjectionWithManagedPredicate(it, items, true) != nil {
			t.Fatal("unsafe predicate admitted", predicate)
		}
	}
}
