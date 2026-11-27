// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// The fixture's lineitems with order keys 121–125 (and NULL keys) have no
// order, so `lineitem LEFT JOIN orders` adds rows with NULL o_* columns,
// and orders with o % 17 == 0 have a NULL o_custkey.
const reduceFrom = `FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey `

var reduceCases = []struct {
	name    string
	where   string
	reduced bool
}{
	{"equality", `WHERE o_custkey = 3`, true},
	{"join to a later item", `WHERE o_custkey = l_qty`, true},
	{"one conjunct of several", `WHERE l_qty > 1 AND o_custkey > 2`, true},
	{"between", `WHERE o_custkey BETWEEN 1 AND 5`, true},
	{"in list", `WHERE o_custkey IN (1, 2, 3)`, true},
	{"is not null", `WHERE o_custkey IS NOT NULL`, true},
	{"date comparison", `WHERE o_orderdate < CAST('1994-06-01' AS DATE)`, true},
	{"arithmetic", `WHERE o_custkey + 1 > 2`, true},
	{"negation", `WHERE NOT (o_custkey = 3)`, true},
	{"cast", `WHERE CAST(o_custkey AS BIGINT) = 3`, true},
	{"or of strict sides", `WHERE o_custkey = 1 OR o_custkey = 2`, true},
	{"is null", `WHERE o_custkey IS NULL`, false},
	{"is null of the key", `WHERE o_orderkey IS NULL`, false},
	{"coalesce", `WHERE COALESCE(o_custkey, 0) = 0`, false},
	{"or with the preserved side", `WHERE o_custkey = 3 OR l_qty = 1`, false},
	{"preserved side only", `WHERE l_qty = 1`, false},
	{"case", `WHERE CASE WHEN o_custkey IS NULL THEN 0 ELSE 1 END = 0`, false},
	{"is distinct from", `WHERE o_custkey IS DISTINCT FROM 3`, false},
	{"subquery", `WHERE o_custkey = (SELECT MIN(c_custkey) FROM customer)`, false},
}

func TestReduceOuterJoinsPlan(t *testing.T) {
	for _, c := range reduceCases {
		t.Run(c.name, func(t *testing.T) {
			_, _, jp := snowflakeTestShape(t, `SELECT COUNT(*) `+reduceFrom+c.where)
			o := jp.items[snowflakeItem(t, jp, "orders.")]
			if got := o.kind == "INNER" && !o.nullable; got != c.reduced {
				t.Fatalf("orders kind %q nullable %t; want reduced=%t", o.kind, o.nullable, c.reduced)
			}
		})
	}
	t.Run("not with a FULL join", func(t *testing.T) {
		_, _, jp := snowflakeTestShape(t, `SELECT COUNT(*) `+reduceFrom+
			`FULL JOIN customer ON o_custkey = c_custkey WHERE o_custkey = 3`)
		if o := jp.items[snowflakeItem(t, jp, "orders.")]; o.kind != "LEFT" {
			t.Fatalf("orders kind %q, want LEFT", o.kind)
		}
	})
}

// The reduction must not change any result: every case returns the same
// rows with and without it, including the ones that keep NULL-extended rows.
func TestReduceOuterJoinsResults(t *testing.T) {
	for _, c := range reduceCases {
		t.Run(c.name, func(t *testing.T) {
			q := snowflakeFixture() + `SELECT l_orderkey, l_qty, o_orderkey, o_custkey, o_orderdate ` + reduceFrom + c.where
			want := reduceRun(t, q, false)
			got := reduceRun(t, q, true)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("reduced rows differ:\ngot  %v\nwant %v", got, want)
			}
		})
	}
}

// Q93's shape: a LEFT JOIN whose NULL-supplying table a WHERE join keeps,
// and a third table reached through it. Once reduced, the streamed
// snowflake join may plan it; before, the outer join was rejected.
func TestReduceOuterJoinsAdmitsStreamedJoin(t *testing.T) {
	q := `SELECT o_custkey, SUM(l_price) FROM lineitem LEFT OUTER JOIN orders ON l_orderkey = o_orderkey, customer
		WHERE o_custkey = c_custkey AND c_nationkey = 2 GROUP BY o_custkey ORDER BY o_custkey`
	e, p, jp := snowflakeTestShape(t, q)
	if _, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where); !ok {
		t.Fatal("the reduced join was not admitted")
	}
	want, _ := snowflakeRun(t, q, 1<<62)
	got, kind := snowflakeRun(t, q, 1)
	if kind != "streaming_snowflake_join" {
		t.Fatalf("plan kind %q, want streaming_snowflake_join", kind)
	}
	if len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func reduceRun(t *testing.T, q string, reduce bool) []string {
	t.Helper()
	old := reduceOuterJoinsEnabled
	reduceOuterJoinsEnabled = reduce
	defer func() { reduceOuterJoinsEnabled = old }()
	ps, err := ParseScript(q)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{cache: newExecCache()}
	p, err := e.planSelect(context.Background(), ps[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.runSelect(context.Background(), p, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprint(r)
	}
	sort.Strings(out)
	return out
}
