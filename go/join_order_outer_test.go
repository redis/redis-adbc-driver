// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"reflect"
	"testing"
)

// TPC-DS Q72 shape: inner joins written fact-first, then LEFT JOINs. Every
// LEFT JOIN reads all rows of the inner joins before it, so those may be
// reordered; keeping the written order joined the fact to a large table on
// one key first (#649).
func outerOrderFixture(t *testing.T, tail ...string) (*executor, *joinPlan, []wherePlan) {
	t.Helper()
	rows := func(n int) []map[string]Value { return make([]map[string]Value, n) }
	ints := func(names ...string) []columnMeta {
		c := make([]columnMeta, len(names))
		for i, n := range names {
			c[i] = columnMeta{Name: n, Type: typeInt32}
		}
		return c
	}
	eq := func(l, r string) Expr { return &Binary{Op: "=", L: &ColumnRef{Name: l}, R: &ColumnRef{Name: r}} }
	jp := &joinPlan{}
	items := []struct {
		kind string
		rel  relation
		on   Expr
	}{
		{"", relation{name: "f", meta: &tableMeta{isMem: true, Columns: ints("item", "date", "promo"), mem: rows(1000)}}, nil},
		{"INNER", relation{name: "inv", meta: &tableMeta{isMem: true, Columns: ints("item"), mem: rows(5000)}}, eq("f.item", "inv.item")},
		{"INNER", relation{name: "d", meta: &tableMeta{isMem: true, Columns: ints("key"), mem: rows(10)}}, eq("f.date", "d.key")},
	}
	for i, k := range tail {
		items = append(items, struct {
			kind string
			rel  relation
			on   Expr
		}{k, relation{name: "p" + string(rune('0'+i)), meta: &tableMeta{isMem: true, Columns: ints("key"), mem: rows(3)}}, eq("f.promo", "p"+string(rune('0'+i))+".key")})
	}
	for _, x := range items {
		it, err := jp.add(x.kind, x.rel)
		if err != nil {
			t.Fatal(err)
		}
		it.on = x.on
	}
	return &executor{cache: newExecCache()}, jp, make([]wherePlan, len(items))
}

func stepOrder(steps []joinStep) (order []int, kinds []string) {
	for _, st := range steps {
		order = append(order, st.item)
		kinds = append(kinds, st.kind)
	}
	return order, kinds
}

func TestJoinOrderReordersInnerJoinsBeforeLeftJoins(t *testing.T) {
	e, jp, wp := outerOrderFixture(t, "LEFT", "LEFT")
	steps, err := e.joinOrder(context.Background(), jp, wp)
	if err != nil {
		t.Fatal(err)
	}
	order, kinds := stepOrder(steps)
	// Selective d first, then f on its key, then inv; LEFT JOINs stay last, in order.
	if !reflect.DeepEqual(order, []int{2, 0, 1, 3, 4}) || !reflect.DeepEqual(kinds, []string{"", "INNER", "INNER", "LEFT", "LEFT"}) {
		t.Fatalf("order %v kinds %v", order, kinds)
	}
	// Each condition once, where its items are first joined: d's ON joins f.
	for i, from := range map[int]int{1: 2, 2: 1, 3: 3, 4: 4} {
		if len(steps[i].conds) != 1 || steps[i].conds[0] != jp.items[from].on {
			t.Fatalf("step %d conditions = %v", i, steps[i].conds)
		}
	}
	if len(steps[0].conds) != 0 {
		t.Fatalf("first step conditions = %v", steps[0].conds)
	}
}

func TestJoinOrderKeepsWrittenOrderAroundOtherOuterJoins(t *testing.T) {
	for _, tail := range [][]string{{"RIGHT"}, {"FULL"}, {"LEFT", "INNER"}} {
		e, jp, wp := outerOrderFixture(t, tail...)
		steps, err := e.joinOrder(context.Background(), jp, wp)
		if err != nil {
			t.Fatal(err)
		}
		order, _ := stepOrder(steps)
		want := []int{0, 1, 2}
		for i := range tail {
			want = append(want, 3+i)
		}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("%v: order %v", tail, order)
		}
	}
}
