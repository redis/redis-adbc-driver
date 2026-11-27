// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// TPC-DS Q40/Q75/Q78/Q80 LEFT JOIN a sales fact to its returns table on the
// fact's own key columns, a leaf the streamed snowflake join can probe and
// NULL-extend. In the fixture, lineitems 121–125 and those with NULL order
// keys have no order, order key 5 has two orders, and partsupp has only
// every third supplier.
var outerStreamQueries = map[string]string{
	"leaf": `SELECT l_orderkey, l_qty, o_orderkey, o_custkey FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey, supplier
		WHERE l_suppkey = s_suppkey AND s_nationkey = 2`,
	"anti join": `SELECT l_orderkey, l_suppkey FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey, supplier
		WHERE l_suppkey = s_suppkey AND o_orderkey IS NULL`,
	"aggregates": `SELECT s_nationkey, SUM(l_price), SUM(COALESCE(o_custkey, 0)), SUM(o_custkey), COUNT(o_orderkey), COUNT(*)
		FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey, supplier WHERE l_suppkey = s_suppkey
		GROUP BY s_nationkey ORDER BY s_nationkey`,
	"outer-side ON filter": `SELECT l_orderkey, o_orderkey, o_custkey FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey AND o_custkey < 10, supplier
		WHERE l_suppkey = s_suppkey AND s_nationkey < 3`,
	"composite key": `SELECT l_partkey, l_suppkey, ps_cost FROM lineitem LEFT JOIN partsupp ON ps_partkey = l_partkey AND ps_suppkey = l_suppkey, supplier
		WHERE l_suppkey = s_suppkey AND s_nationkey = 1`,
}

func TestSnowflakeOuterLeafMatchesEagerJoin(t *testing.T) {
	for name, q := range outerStreamQueries {
		t.Run(name, func(t *testing.T) {
			e, p, jp := snowflakeTestShape(t, q)
			s, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where)
			if !ok {
				t.Fatal("outer leaf not admitted")
			}
			outer := 0
			for _, d := range s.dims {
				if d.outer {
					outer++
					if d.item == s.fact || !jp.items[d.item].nullable {
						t.Fatalf("outer dimension %d is the fact or not NULL-supplying", d.item)
					}
				}
			}
			if outer != 1 {
				t.Fatalf("%d outer dimensions, want 1", outer)
			}
			want, wantKind := snowflakeRun(t, q, 1<<62)
			got, kind := snowflakeRun(t, q, 1)
			if wantKind == "streaming_snowflake_join" || kind != "streaming_snowflake_join" {
				t.Fatalf("plan kinds eager %q, streamed %q", wantKind, kind)
			}
			if len(want) == 0 {
				t.Fatal("fixture query returned no rows")
			}
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v\nwant %v", got, want)
			}
		})
	}
}

func TestSnowflakeOuterLeafRejectsUnsafeShapes(t *testing.T) {
	for name, q := range map[string]string{
		// A preserved-side ON condition NULL-extends rows instead of removing them.
		"preserved-side ON filter": `SELECT COUNT(*) FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey AND l_qty > 2, supplier WHERE l_suppkey = s_suppkey`,
		"outer item with a child":  `SELECT COUNT(*) FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey JOIN customer ON o_custkey = c_custkey`,
		"non-equi ON":              `SELECT COUNT(*) FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey AND o_custkey > l_qty, supplier WHERE l_suppkey = s_suppkey`,
		// lineitem is the largest table, so it would be the fact.
		"NULL-supplying fact": `SELECT COUNT(*) FROM orders LEFT JOIN lineitem ON l_orderkey = o_orderkey, customer WHERE o_custkey = c_custkey`,
	} {
		t.Run(name, func(t *testing.T) {
			e, p, jp := snowflakeTestShape(t, q)
			if _, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where); ok {
				t.Fatal("unsafe outer shape admitted")
			}
		})
	}
}

// The parallel workers' probe (starRowProbe) NULL-extends an outer
// dimension: a fact row with no match, or a NULL key, keeps only its own
// and the inner dimensions' columns.
func TestStarRowProbeOuterDimension(t *testing.T) {
	inner := starDimension{factCols: []string{"f.a"}, dimCols: []string{"d.a"}, buckets: map[string][]map[string]Value{}}
	outer := starDimension{factCols: []string{"f.b"}, dimCols: []string{"r.b"}, buckets: map[string][]map[string]Value{}, outer: true}
	one, two := intValue(typeInt64, 1), intValue(typeInt64, 2)
	k1, _ := starRowKey(map[string]Value{"d.a": one}, []string{"d.a"})
	inner.buckets[k1] = []map[string]Value{{"d.a": one, "d.x": two}}
	kb, _ := starRowKey(map[string]Value{"r.b": two}, []string{"r.b"})
	outer.buckets[kb] = []map[string]Value{{"r.b": two, "r.y": one}, {"r.b": two, "r.y": two}}
	shape := &starShape{dims: []starDimension{inner, outer}}
	for name, c := range map[string]struct {
		fact map[string]Value
		want []map[string]Value
	}{
		"matched twice": {map[string]Value{"f.a": one, "f.b": two},
			[]map[string]Value{{"f.a": one, "f.b": two, "d.a": one, "d.x": two, "r.b": two, "r.y": one}, {"f.a": one, "f.b": two, "d.a": one, "d.x": two, "r.b": two, "r.y": two}}},
		"no match": {map[string]Value{"f.a": one, "f.b": one},
			[]map[string]Value{{"f.a": one, "f.b": one, "d.a": one, "d.x": two}}},
		"NULL key": {map[string]Value{"f.a": one, "f.b": nullValue(typeInt64)},
			[]map[string]Value{{"f.a": one, "f.b": nullValue(typeInt64), "d.a": one, "d.x": two}}},
		"inner miss": {map[string]Value{"f.a": two, "f.b": two}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			for _, owned := range []bool{false, true} {
				p := newStarRowProbe(shape)
				var got []map[string]Value
				emit := func(r map[string]Value) error { got = append(got, r); return nil }
				fact := map[string]Value{}
				for k, v := range c.fact {
					fact[k] = v
				}
				var err error
				if owned {
					err = p.ownedRow(context.Background(), fact, emit)
				} else {
					err = p.row(context.Background(), fact, emit)
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, c.want) {
					t.Fatalf("owned=%t: got %v\nwant %v", owned, got, c.want)
				}
			}
		})
	}
}
