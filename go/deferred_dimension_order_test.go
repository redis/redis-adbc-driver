// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func deferredOrderFixture(t *testing.T, days, stores int) (*executor, *joinPlan, []wherePlan) {
	t.Helper()
	e := &executor{cache: newExecCache()}
	jp := &joinPlan{}
	fact := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "date", Type: typeInt32}, {Name: "store", Type: typeInt32}}, view: &lazyView{unionDemand: true}}
	date := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "key", Type: typeInt32}}, mem: make([]map[string]Value, days)}
	store := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "key", Type: typeInt32}}, mem: make([]map[string]Value, stores)}
	for _, r := range []relation{{name: "f", meta: fact}, {name: "d", meta: date}, {name: "s", meta: store}} {
		if _, err := jp.add("INNER", r); err != nil {
			t.Fatal(err)
		}
	}
	jp.items[0].extra = []Expr{&Binary{Op: "=", L: &ColumnRef{Name: "f.date"}, R: &ColumnRef{Name: "d.key"}}, &Binary{Op: "=", L: &ColumnRef{Name: "f.store"}, R: &ColumnRef{Name: "s.key"}}}
	return e, jp, make([]wherePlan, 3)
}

func TestDeferredDimensionOrderCollectsBoundedStarKeys(t *testing.T) {
	e, jp, wp := deferredOrderFixture(t, 15, 6)
	steps, err := e.joinOrder(context.Background(), jp, wp)
	if err != nil {
		t.Fatal(err)
	}
	var order []int
	for _, st := range steps {
		order = append(order, st.item)
	}
	if !reflect.DeepEqual(order, []int{2, 1, 0}) {
		t.Fatalf("connected fact displaced selective date dimension: %v", order)
	}
	if len(steps[2].conds) != 2 {
		t.Fatal("real equijoin conditions were lost")
	}
}

func TestDeferredDimensionOrderCapAndOuterFallback(t *testing.T) {
	e, jp, wp := deferredOrderFixture(t, 200, 6)
	steps, err := e.joinOrder(context.Background(), jp, wp)
	if err != nil || steps[0].item != 2 || steps[1].item != 0 {
		t.Fatalf("oversized disconnected product selected: %v %v", steps, err)
	}
	e, jp, wp = deferredOrderFixture(t, 15, 6)
	jp.items[1].kind = "LEFT"
	steps, err = e.joinOrder(context.Background(), jp, wp)
	if err != nil || steps[0].item != 0 || steps[1].item != 1 || steps[2].item != 2 || steps[1].kind != "LEFT" {
		t.Fatalf("outer join order changed: %v %v", steps, err)
	}
}

func TestDeferredDimensionOrderConservativeSources(t *testing.T) {
	for _, kind := range []string{"ordinary_target", "unmanaged", "live", "lazy", "unknown", "connected_dimension", "over_cap"} {
		t.Run(kind, func(t *testing.T) {
			e, jp, _ := deferredOrderFixture(t, 15, 6)
			counts := []int64{1 << 60, 15, 6}
			connected := []bool{true, false, false}
			chosen := map[int]bool{2: true}
			product := int64(6)
			switch kind {
			case "ordinary_target":
				jp.items[0].base.view = nil
			case "unmanaged":
				jp.items[1].base.Unmanaged = true
			case "live":
				jp.items[1].base.Live = true
			case "lazy":
				jp.items[1].base.lazy = &lazyRelation{}
			case "unknown":
				jp.items[1].base.isMem = false
			case "connected_dimension":
				connected[1] = true
			case "over_cap":
				product = maxUnionTerms + 1
			}
			if got := e.boundedDeferredDimension(context.Background(), jp, counts, chosen, connected, 0, product); got != 0 {
				t.Fatalf("unsafe source advanced: %d", got)
			}
		})
	}
}

func TestDeferredDimensionProductSaturates(t *testing.T) {
	for _, tt := range []struct{ a, b, want int64 }{{1, 1, 1}, {6, 15, 90}, {25, 40, 1000}, {26, 40, 1001}, {math.MaxInt64, math.MaxInt64, 1001}, {1, math.MaxInt64, 1001}, {0, math.MaxInt64, 0}, {1000, 0, 0}, {-1, 5, 1001}} {
		if got := deferredDimensionProduct(tt.a, tt.b); got != tt.want {
			t.Fatalf("%d*%d=%d want%d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDeferredDimensionOrderPreservesMultiplicityNullAndResidual(t *testing.T) {
	q := `WITH sales(d,s,v) AS (VALUES (1,1,10),(1,1,10),(2,1,20),(NULL,1,30),(1,NULL,40)), dates(d) AS (VALUES (1),(1)), stores(s) AS (VALUES (1)) SELECT COUNT(*),SUM(u.v) FROM (SELECT d,s,v FROM sales UNION ALL SELECT d,s,v FROM sales) u, dates,stores WHERE u.d=dates.d AND u.s=stores.s AND u.v+1=11`
	if got := memQuery(q); got != "8|80" {
		t.Fatal(got)
	}
	q = `WITH a(d,s,v) AS (VALUES (1,1,10),(NULL,1,30)), dates(d) AS (VALUES (NULL)), stores(s) AS (VALUES (1)) SELECT COUNT(*) FROM (SELECT d,s,v FROM a UNION ALL SELECT d,s,v FROM a) u,dates,stores WHERE u.d=dates.d AND u.s=stores.s`
	if got := memQuery(q); got != "0" {
		t.Fatal(got)
	}
}

func TestDeferredCompositeDemandKeepsCorrelatedJoinAndExactIntegers(t *testing.T) {
	q := `WITH dims(a,b) AS (VALUES (CAST(1 AS BIGINT),CAST(10 AS BIGINT)),(1,10),(2,20),(NULL,20),(2,NULL)), sales(a,b,v) AS (VALUES (CAST(1 AS BIGINT),CAST(10 AS BIGINT),1),(1,20,100),(2,10,1000),(2,20,10),(NULL,20,10000)) SELECT COUNT(*),SUM(u.v) FROM (SELECT a,b,v FROM sales UNION ALL SELECT a,b,v FROM sales) u JOIN dims ON u.a=dims.a AND u.b=dims.b`
	if got := memQuery(q); got != "6|24" {
		t.Fatal(got)
	}
	q = `WITH dims(a,b) AS (VALUES (CAST(9007199254740993 AS BIGINT),CAST(7 AS BIGINT))), sales(a,b,v) AS (VALUES (CAST(9007199254740993 AS BIGINT),CAST(7 AS BIGINT),1),(9007199254740992,7,100)) SELECT SUM(u.v) FROM (SELECT a,b,v FROM sales UNION ALL SELECT a,b,v FROM sales) u JOIN dims ON u.a=dims.a AND u.b=dims.b`
	if got := memQuery(q); got != "2" {
		t.Fatal(got)
	}
}

func TestDeferredCompositeDemandPerPairCapAndUnsafeTypes(t *testing.T) {
	ctx := context.Background()
	e := &executor{cache: newExecCache()}
	env := e.newEnv(ctx, map[string]ColType{"la": typeInt64, "lb": typeInt64}, nil)
	view := &tableMeta{Columns: []columnMeta{{Name: "ra", Type: typeInt64}, {Name: "rb", Type: typeInt64}}}
	pairs := []equiPair{{left: &ColumnRef{Name: "la"}, right: &ColumnRef{Name: "ra"}}, {left: &ColumnRef{Name: "lb"}, right: &ColumnRef{Name: "rb"}}}
	left := []map[string]Value{{"la": intValue(typeInt64, 1), "lb": intValue(typeInt64, 10)}, {"la": intValue(typeInt64, 2), "lb": intValue(typeInt64, 20)}}
	demand, err := e.deferredJoinDemand(ctx, env, pairs, left, view)
	if err != nil {
		t.Fatal(err)
	}
	both, ok := demand.(*Binary)
	if !ok || both.Op != "AND" {
		t.Fatalf("not all safe pairs: %#v", demand)
	}
	rightEnv := e.newEnv(ctx, view.types(), nil)
	rightEnv.row = map[string]Value{"ra": intValue(typeInt64, 1), "rb": intValue(typeInt64, 20)}
	v, err := rightEnv.eval(demand)
	if err != nil || v.Null || v.I == 0 {
		t.Fatalf("independent keysets must admit cross-combinations for real join to recheck: %v %v", v, err)
	}
	// An unsafe pair is ignored independently; the other safe key remains useful.
	left[0]["la"] = intValue(typeInt32, 1)
	demand, err = e.deferredJoinDemand(ctx, env, pairs, left, view)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]bool{}
	columnRefs(demand, refs)
	if refs["ra"] || !refs["rb"] {
		t.Fatalf("unsafe pair poisoned safe demand: %v", refs)
	}
	left = nil
	for i := 0; i <= maxUnionTerms; i++ {
		left = append(left, map[string]Value{"la": intValue(typeInt64, int64(i)), "lb": intValue(typeInt64, 7)})
	}
	demand, err = e.deferredJoinDemand(ctx, env, pairs, left, view)
	if err != nil {
		t.Fatal(err)
	}
	refs = map[string]bool{}
	columnRefs(demand, refs)
	if refs["ra"] || !refs["rb"] {
		t.Fatalf("over-cap pair discarded other safe pair: %v", refs)
	}
	// A proven empty keyset makes matching impossible even when another pair
	// overflows its cap. Repeated or mixed NULL keys never fabricate equality.
	for _, row := range left {
		row["lb"] = Value{T: typeInt64, Null: true}
	}
	demand, err = e.deferredJoinDemand(ctx, env, pairs, left, view)
	lit, ok := demand.(*Literal)
	if err != nil || !ok || lit.V.Null || lit.V.I != 0 {
		t.Fatalf("empty proven keyset should be FALSE: %#v %v", demand, err)
	}
}
