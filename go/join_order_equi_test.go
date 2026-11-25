// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"reflect"
	"testing"
)

// TPC-DS Q15/Q45 shape: date ⋈ fact ⋈ customer ⋈ address, plus an OR that
// reads only address and fact. The OR becomes evaluable as soon as address
// is the next table, but it is no join key: choosing address there pairs
// every joined row with every address row (#649).
func equiOrderFixture(t *testing.T, residual Expr) (*executor, *joinPlan, []wherePlan) {
	t.Helper()
	rows := func(n int) []map[string]Value { return make([]map[string]Value, n) }
	ints := func(names ...string) []columnMeta {
		c := make([]columnMeta, len(names))
		for i, n := range names {
			c[i] = columnMeta{Name: n, Type: typeInt32}
		}
		return c
	}
	jp := &joinPlan{}
	for _, r := range []relation{
		{name: "f", meta: &tableMeta{isMem: true, Columns: ints("date", "cust", "price"), mem: rows(1000)}},
		{name: "c", meta: &tableMeta{isMem: true, Columns: ints("key", "addr"), mem: rows(100)}},
		{name: "a", meta: &tableMeta{isMem: true, Columns: ints("key", "zip"), mem: rows(50)}},
		{name: "d", meta: &tableMeta{isMem: true, Columns: ints("key"), mem: rows(10)}},
	} {
		if _, err := jp.add("INNER", r); err != nil {
			t.Fatal(err)
		}
	}
	eq := func(l, r string) Expr { return &Binary{Op: "=", L: &ColumnRef{Name: l}, R: &ColumnRef{Name: r}} }
	jp.items[0].extra = []Expr{eq("f.cust", "c.key"), eq("c.addr", "a.key"), residual, eq("f.date", "d.key")}
	return &executor{cache: newExecCache()}, jp, make([]wherePlan, 4)
}

func TestJoinOrderPrefersEquiJoinOverCrossTableResidual(t *testing.T) {
	lit := &Literal{V: intValue(typeInt64, 5)}
	for name, residual := range map[string]Expr{
		"or":        &Binary{Op: "OR", L: &Binary{Op: "=", L: &ColumnRef{Name: "a.zip"}, R: lit}, R: &Binary{Op: ">", L: &ColumnRef{Name: "f.price"}, R: lit}},
		"not_equal": &Binary{Op: "<>", L: &ColumnRef{Name: "a.zip"}, R: &ColumnRef{Name: "f.price"}},
	} {
		t.Run(name, func(t *testing.T) {
			e, jp, wp := equiOrderFixture(t, residual)
			steps, err := e.joinOrder(context.Background(), jp, wp)
			if err != nil {
				t.Fatal(err)
			}
			var order []int
			for _, st := range steps {
				order = append(order, st.item)
			}
			// date, fact (date key), customer (customer key), address (address key).
			if !reflect.DeepEqual(order, []int{3, 0, 1, 2}) {
				t.Fatalf("join order %v pairs address with no join key", order)
			}
			// The residual still applies once, at the address step, with its key.
			if got := steps[3].conds; len(got) != 2 || got[0] != jp.items[0].extra[1] || got[1] != residual {
				t.Fatalf("address step conditions = %v", got)
			}
		})
	}
}

func TestJoinOrderStillUsesResidualWhenNoKeyConnects(t *testing.T) {
	// Without an equality, a cross-table condition still beats an unrelated table.
	e, jp, wp := equiOrderFixture(t, &Binary{Op: "<", L: &ColumnRef{Name: "a.zip"}, R: &ColumnRef{Name: "d.key"}})
	jp.items[0].extra = []Expr{jp.items[0].extra[2], &Binary{Op: "=", L: &ColumnRef{Name: "f.cust"}, R: &ColumnRef{Name: "c.key"}}}
	steps, err := e.joinOrder(context.Background(), jp, wp)
	if err != nil {
		t.Fatal(err)
	}
	var order []int
	for _, st := range steps {
		order = append(order, st.item)
	}
	if !reflect.DeepEqual(order, []int{3, 2, 1, 0}) {
		t.Fatalf("join order %v", order)
	}
}
