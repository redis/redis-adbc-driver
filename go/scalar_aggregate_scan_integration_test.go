// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestSQLScalarAggregateScanEquivalent(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_scalar_reuse"
	h.setupStreamTable(table, 3500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("DELETE FROM " + table + " WHERE id IN (2,100,3499)")
	h.exec("UPDATE " + table + " SET id=9007199254740993,label='a\x00b',qty=NULL WHERE id=3")
	h.exec("ALTER TABLE " + table + " ADD COLUMN d SMALLINT DEFAULT 11")
	h.exec("UPDATE " + table + " SET d=NULL WHERE id=4")
	for _, narrow := range []bool{false, true} {
		names := []string{"id", "label", "qty", "d"}
		if narrow {
			names = names[:1]
		}
		var aggs []*Func
		need := map[string]bool{}
		for _, name := range names {
			need[name] = true
			aggs = append(aggs, &Func{Name: "MIN", Args: []Expr{&ColumnRef{Name: name}}})
		}
		aggs = append(aggs, &Func{Name: "COUNT", Star: true})
		for _, where := range []string{"", " WHERE qty<>0", " WHERE id%2=0"} {
			e, p, wp := distinctTestPlan(t, h, "SELECT * FROM "+table+where)
			req := scanRequest{meta: p.meta, where: wp, need: need}
			want := map[int64][]Value{}
			rowValues := func(row map[string]Value) []Value {
				var vals []Value
				for _, name := range names {
					vals = append(vals, row[name])
				}
				return append(vals, row[rowIDField])
			}
			if err := e.scanPages(h.ctx, req, nil, func(rows []map[string]Value) error {
				for _, r := range rows {
					want[r[rowIDField].I] = rowValues(r)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			got := map[int64][]Value{}
			pointers := map[uintptr]bool{}
			handled, err := e.scanScalarAggregateValues(h.ctx, req, nil, aggs, func(values []Value) error {
				pointers[reflect.ValueOf(values).Pointer()] = true
				got[values[len(values)-1].I] = slices.Clone(values)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if wp.residual != nil {
				if handled {
					t.Fatal("residual lost fallback")
				}
				continue
			}
			if !handled || len(pointers) != 1 || !reflect.DeepEqual(got, want) {
				t.Fatal("values/reuse differ", narrow, where, handled, len(got), len(want), len(pointers))
			}
		}
	}
	e, p, wp := distinctTestPlan(t, h, "SELECT * FROM "+table)
	req := scanRequest{meta: p.meta, where: wp, need: map[string]bool{"id": true}}
	aggs := []*Func{{Name: "AVG", Args: []Expr{&ColumnRef{Name: "id"}}}}
	sentinel := errors.New("consumer failure")
	handled, err := e.scanScalarAggregateValues(h.ctx, req, nil, aggs, func([]Value) error { return sentinel })
	if !handled || !errors.Is(err, sentinel) {
		t.Fatal(handled, err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, err := e.scanScalarAggregateValues(ctx, req, nil, aggs, func([]Value) error { return nil }); err == nil {
		t.Fatal("cancellation ignored")
	}
	// Expressions retain whole-page decode before evaluation, including serial
	// replay of arithmetic overflow. The optimized callback cannot run early.
	aggs[0].Args[0] = &Binary{Op: "+", L: &ColumnRef{Name: "id"}, R: &Literal{V: intValue(typeInt64, 1)}}
	handled, err = e.scanScalarAggregateValues(h.ctx, req, nil, aggs, func([]Value) error { t.Fatal("expression callback escaped guard"); return nil })
	if handled || err != nil {
		t.Fatal(handled, err)
	}
}
