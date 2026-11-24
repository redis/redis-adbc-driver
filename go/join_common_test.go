// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"os"
	"strings"
	"testing"
)

func commonJoinFixture() *joinPlan {
	return &joinPlan{items: []*joinItem{
		{prefix: "l.", base: &tableMeta{Columns: []columnMeta{
			{Name: "l_partkey", Type: typeInt64}, {Name: "l_quantity", Type: decimalType(15, 2)},
			{Name: "l_shipmode", Type: typeString}, {Name: "l_shipinstruct", Type: typeString},
		}}},
		{prefix: "p.", kind: "CROSS", base: &tableMeta{Columns: []columnMeta{
			{Name: "p_partkey", Type: typeInt64}, {Name: "p_brand", Type: typeString},
			{Name: "p_container", Type: typeString}, {Name: "p_size", Type: typeInt32},
		}}},
	}}
}

func bindCommonFixture(expr Expr) {
	walkExpr(expr, func(e Expr) {
		if c, ok := e.(*ColumnRef); ok {
			if strings.HasPrefix(c.Name, "l_") {
				c.Name = "l." + c.Name
			} else if strings.HasPrefix(c.Name, "p_") {
				c.Name = "p." + c.Name
			}
		}
	})
}

func TestJoinCommonQ19Plan(t *testing.T) {
	sql, err := os.ReadFile("testdata/olap/tpch-q19.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := ParseScript(string(sql))
	if err != nil {
		t.Fatal(err)
	}
	where := stmts[0].Stmt.(*SelectStmt).Where
	bindCommonFixture(where)
	jp := commonJoinFixture()
	jp.planPushdown(where)
	var equality bool
	for _, e := range jp.items[1].extra {
		b, ok := e.(*Binary)
		equality = equality || (ok && b.Op == "=" && exprEqual(b.L, &ColumnRef{Name: "p.p_partkey"}) && exprEqual(b.R, &ColumnRef{Name: "l.l_partkey"}))
	}
	if !equality {
		t.Fatal("Q19's common equality must be exposed to hash-join key discovery")
	}
	if len(jp.items[0].pushed) != 2 {
		t.Fatalf("ship-mode and ship-instruction filters: got %d pushed conditions", len(jp.items[0].pushed))
	}
	if jp.items[1].extra[0] != where {
		t.Fatal("original OR must remain an exact residual")
	}
	jp.items[0].nullable = true
	jp.planPushdown(where)
	if len(jp.items[0].pushed) != 0 || len(jp.items[1].extra) != 0 {
		t.Fatal("common WHERE conditions crossed a null-supplying join boundary")
	}
}

func TestJoinCommonSafety(t *testing.T) {
	for _, tail := range []string{
		"random() > 0.5", "1 / 0 = 1", "CAST(p_brand AS INTEGER) = 1",
		"EXISTS(SELECT 1)", "l_quantity = 'invalid'", "l_quantity = $1",
	} {
		expr := parseTestExpr(t, "(p_partkey=l_partkey AND p_size=1) OR (p_partkey=l_partkey AND "+tail+")")
		bindCommonFixture(expr)
		if got := commonJoinFixture().pushdownConjuncts(expr); len(got) != 1 || got[0] != expr {
			t.Errorf("unsafe OR extracted conditions for %s", tail)
		}
	}
	expr := parseTestExpr(t, "(p_partkey=l_partkey AND p_size=1) OR p_size=2")
	bindCommonFixture(expr)
	if got := commonJoinFixture().pushdownConjuncts(expr); len(got) != 1 {
		t.Fatal("equality absent from one OR branch was extracted")
	}
}

func TestJoinCommonResults(t *testing.T) {
	// Duplicate keys, NULL join keys, NULL filter values and unmatched outer rows.
	const with = "WITH a(k,v) AS (VALUES (1,1),(1,1),(2,2),(3,NULL),(NULL,1)), " +
		"b(k,v) AS (VALUES (1,1),(2,2),(4,4),(NULL,1)) "
	for _, tc := range []struct{ query, want string }{
		{"SELECT a.k,b.k FROM a,b WHERE (a.k=b.k AND a.v=1) OR (a.k=b.k AND b.v=2) ORDER BY a.k", "1|1\n1|1\n2|2"},
		{"SELECT a.k,b.k FROM a LEFT JOIN b ON a.k=b.k WHERE (a.v=1 AND b.v=1) OR (a.v=1 AND b.v=2) ORDER BY a.k", "1|1\n1|1"},
		{"SELECT a.k,b.k FROM a FULL JOIN b ON a.k=b.k WHERE (a.v=1 AND b.v=1) OR (a.v=1 AND b.v=2) ORDER BY a.k", "1|1\n1|1"},
	} {
		if got := memQuery(with + tc.query); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.query, got, tc.want)
		}
	}
}
