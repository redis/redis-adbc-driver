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

// TPC-DS Q39's shape: a CTE over a grouped derived table, read twice, each
// reference filtered on a grouping column (inv1.d_moy = 1, inv2.d_moy = 2).
// The month filters belong below the GROUP BY, where they narrow the base
// tables, instead of after the whole CTE is computed. The references share
// one evaluation with the OR of their filters (m in {1, 2}: 7 of 8 groups);
// a reference without one (or a filter that reaches only the projection)
// still gets every row it needs.
const ctePushdownFixture = `WITH f(k, m, v) AS (VALUES (1,1,10),(1,1,11),(1,2,20),(2,1,30),(2,2,40),(2,3,41),(3,1,NULL),(3,2,5),(NULL,1,7)),
	inv AS (SELECT k, m, s, n FROM (SELECT k, m, SUM(v) s, COUNT(*) n FROM f GROUP BY k, m) foo WHERE n > 0) `

var ctePushdownCases = []struct {
	name    string
	query   string
	perItem []int // rows each FROM item's CTE relation holds, in FROM order
}{
	{"two references, grouping column", `SELECT a.k, a.s, b.s FROM inv a, inv b WHERE a.k = b.k AND a.m = 1 AND b.m = 1 + 1`, []int{7, 7}},
	{"inner join", `SELECT a.k, b.k FROM inv a JOIN inv b ON a.k = b.k WHERE a.m = 2 AND b.m IN (1, 3)`, []int{8, 8}},
	{"single reference", `SELECT k, s FROM inv WHERE inv.m = 3`, []int{1}},
	{"aggregate output stays above the GROUP BY", `SELECT a.k, a.s FROM inv a, inv b WHERE a.k = b.k AND a.s > 15 AND b.m = 1`, []int{7, 7}},
	{"not on the NULL-supplying side", `SELECT a.k, b.s FROM inv a LEFT JOIN inv b ON a.k = b.k WHERE a.m = 1 AND b.m = 2`, []int{8, 8}},
	{"not an unqualified reference", `SELECT a.k FROM inv a, f WHERE a.k = f.k AND s = 21`, []int{8, 9}},
	{"not with a subquery", `SELECT a.k FROM inv a WHERE a.m = (SELECT MIN(m) FROM f)`, []int{8}},
}

func ctePushdownPlan(t *testing.T, q string, enabled bool) []int {
	t.Helper()
	old := ctePushdownEnabled
	ctePushdownEnabled = enabled
	defer func() { ctePushdownEnabled = old }()
	_, p := deferredTestPlan(t, ctePushdownFixture+q)
	if p.meta.join == nil {
		return []int{len(p.meta.mem)}
	}
	var out []int
	for _, it := range p.meta.join.items {
		out = append(out, len(it.base.mem))
	}
	return out
}

func TestCTEPushdownPlan(t *testing.T) {
	for _, c := range ctePushdownCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ctePushdownPlan(t, c.query, true); !reflect.DeepEqual(got, c.perItem) {
				t.Fatalf("rows per FROM item %v, want %v", got, c.perItem)
			}
		})
	}
}

// Both references read the same evaluation, as unpushed ones share the
// CTE's: TPC-DS Q64's cross_sales joins 18 tables, too costly to run twice.
func TestCTEPushdownSharesOneEvaluation(t *testing.T) {
	_, p := deferredTestPlan(t, ctePushdownFixture+`SELECT a.k, b.k FROM inv a, inv b WHERE a.k = b.k AND a.m = 1 AND b.m = 2`)
	items := p.meta.join.items
	if items[0].base != items[1].base {
		t.Fatal("the two references were evaluated separately")
	}
	if n := len(items[0].base.mem); n != 7 {
		t.Fatalf("%d rows, want 7", n)
	}
}

func TestCTEPushdownResults(t *testing.T) {
	for _, c := range append(ctePushdownCases, []struct {
		name    string
		query   string
		perItem []int
	}{
		{"window function in the CTE", `SELECT x.k FROM (SELECT k, m, ROW_NUMBER() OVER (ORDER BY k, m) r FROM inv) x WHERE x.m = 1`, nil},
		{"limit in the body", `SELECT a.k FROM (SELECT k, m FROM inv ORDER BY k, m LIMIT 3) a WHERE a.m = 1`, nil},
		{"rollup below", `SELECT a.k, a.m, a.s FROM (SELECT k, m, SUM(v) s FROM f GROUP BY ROLLUP (k, m)) a WHERE a.m = 1`, nil},
	}...) {
		t.Run(c.name, func(t *testing.T) {
			want := ctePushdownRows(t, c.query, false)
			got := ctePushdownRows(t, c.query, true)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("rows with pushdown differ:\ngot  %v\nwant %v", got, want)
			}
		})
	}
}

// The same rules hold for a CTE whose body is not pushable: these keep
// every row of the CTE in their FROM item.
func TestCTEPushdownRejectsUnsafeBodies(t *testing.T) {
	for name, body := range map[string]string{
		"limit":            `SELECT k, m FROM f ORDER BY k, m LIMIT 4`,
		"window function":  `SELECT k, m, ROW_NUMBER() OVER (ORDER BY k) r FROM f`,
		"rollup":           `SELECT k, m, SUM(v) s FROM f GROUP BY ROLLUP (k, m)`,
		"volatile":         `SELECT k, m, random() r FROM f`,
		"distinct on":      `SELECT DISTINCT ON (k) k, m FROM f ORDER BY k, m`,
		"scalar aggregate": `SELECT MIN(m) m FROM f`,
	} {
		t.Run(name, func(t *testing.T) {
			q := `WITH f(k, m, v) AS (VALUES (1,1,10),(1,2,20),(2,1,30),(2,2,40)), c AS (` + body + `) SELECT x.m FROM c x WHERE x.m = 1`
			off := ctePushdownRowsRaw(t, q, false)
			on := ctePushdownRowsRaw(t, q, true)
			if !reflect.DeepEqual(on, off) {
				t.Fatalf("got %v\nwant %v", on, off)
			}
			_, pOff := deferredTestPlanWith(t, q, false)
			_, pOn := deferredTestPlanWith(t, q, true)
			if len(pOn.meta.mem) != len(pOff.meta.mem) {
				t.Fatalf("pushed into an unsafe body: %d rows, unpushed %d", len(pOn.meta.mem), len(pOff.meta.mem))
			}
		})
	}
}

func deferredTestPlanWith(t *testing.T, q string, enabled bool) (*executor, *selectPlan) {
	t.Helper()
	old := ctePushdownEnabled
	ctePushdownEnabled = enabled
	defer func() { ctePushdownEnabled = old }()
	return deferredTestPlan(t, q)
}

func ctePushdownRows(t *testing.T, q string, enabled bool) []string {
	t.Helper()
	return ctePushdownRowsRaw(t, ctePushdownFixture+q, enabled)
}

func ctePushdownRowsRaw(t *testing.T, q string, enabled bool) []string {
	t.Helper()
	old := ctePushdownEnabled
	ctePushdownEnabled = enabled
	defer func() { ctePushdownEnabled = old }()
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
