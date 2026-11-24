// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"strings"
	"testing"
)

func deferredTestPlan(t *testing.T, q string) (*executor, *selectPlan) {
	t.Helper()
	ps, err := ParseScript(q)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{cache: newExecCache()}
	p, err := e.planSelect(context.Background(), ps[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

func TestDeferredUnionStructureAndResults(t *testing.T) {
	const prefix = `WITH a(d,v) AS (VALUES (1,10),(1,10),(2,20),(NULL,30)) `
	q := prefix + `SELECT d,v FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u WHERE d=1 ORDER BY v`
	e, p := deferredTestPlan(t, q)
	if p.meta.view == nil || !p.meta.view.unionDemand || len(p.meta.mem) != 0 {
		t.Fatal("UNION source was eagerly materialized")
	}
	if p.meta.view.plan.setop == nil {
		t.Fatal("lost pairwise UNION plan")
	}
	rows, err := e.runSelect(context.Background(), p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("duplicates lost: %v", rows)
	}
	// Observe the deferred source itself, before the consumer rechecks its
	// residual: the restriction must already have reached both branches.
	filtered, err := e.runView(context.Background(), p.meta.view, p.sel.Where, map[string]bool{"d": true, "v": true}, nil)
	if err != nil || len(filtered) != 4 {
		t.Fatalf("branch demand not applied: %d rows, %v", len(filtered), err)
	}
	if got := memQuery(prefix + `SELECT COUNT(*) FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u WHERE d IS NULL`); got != "2" {
		t.Fatal(got)
	}
	// A projected single-consumer CTE also remains deferred.
	_, p = deferredTestPlan(t, prefix+`, u AS (SELECT d,v FROM a UNION ALL SELECT d,v FROM a), x AS (SELECT d,v FROM u) SELECT * FROM x WHERE d=1`)
	if p.meta.view == nil || !p.meta.view.unionDemand {
		t.Fatal("projection lost deferred UNION")
	}
}

func TestDeferredUnionJoinMultiplicityAndNulls(t *testing.T) {
	const base = `WITH a(d,v) AS (VALUES (1,10),(1,10),(2,20),(NULL,30)), dims(d) AS (VALUES (1),(1),(3),(NULL)) `
	cases := []struct{ q, want string }{
		{`SELECT COUNT(*) FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u JOIN dims ON u.d=dims.d`, "8"},
		{`SELECT COUNT(*) FROM dims LEFT JOIN (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u ON u.d=dims.d`, "10"},
		{`SELECT COUNT(*) FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u LEFT JOIN dims ON u.d=dims.d`, "12"},
	}
	for _, tt := range cases {
		if got := memQuery(base + tt.q); got != tt.want {
			t.Errorf("%s: got %s want %s", tt.q, got, tt.want)
		}
	}
}

func TestDeferredUnionConservativeFallback(t *testing.T) {
	const prefix = `WITH a(d,v) AS (VALUES (1,10),(2,20)) `
	cases := []string{
		`SELECT * FROM (SELECT d,random() AS v FROM a UNION ALL SELECT d,random() FROM a) u WHERE d=1`,
		`SELECT * FROM (SELECT d,CAST(v AS NUMERIC(12,2)) AS v FROM a UNION ALL SELECT d,CAST(v AS NUMERIC(12,2)) FROM a) u WHERE d=1`,
		`SELECT * FROM (SELECT d,v FROM a UNION ALL SELECT d,CAST(v AS BIGINT) FROM a) u WHERE d=1`,
		`SELECT * FROM (SELECT d,v FROM a UNION SELECT d,v FROM a) u WHERE d=1`,
		`SELECT * FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a LIMIT 1) u WHERE d=1`,
		`SELECT * FROM (SELECT d,SUM(v) AS v FROM a GROUP BY d UNION ALL SELECT d,SUM(v) FROM a GROUP BY d) u WHERE d=1`,
	}
	for _, q := range cases {
		_, p := deferredTestPlan(t, prefix+q)
		if p.meta.view != nil && p.meta.view.unionDemand {
			t.Errorf("unsafe deferral: %s", q)
		}
	}
	// A shared definition retains one complete cached materialization.
	e, _ := deferredTestPlan(t, prefix+`, u AS (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) SELECT x.d FROM u x JOIN u y ON x.d=y.d`)
	found := false
	for def, m := range e.cache.ctes {
		if def.Name == "u" {
			found = true
			if m.view != nil || len(m.mem) != 4 {
				t.Fatal("shared CTE was deferred or truncated")
			}
		}
	}
	if !found {
		t.Fatal("shared materialization missing")
	}
	// Projected errors must not disappear behind a selective outer condition.
	got := memQuery(prefix + `SELECT d FROM (SELECT d,1/(d-2) AS v FROM a UNION ALL SELECT d,v FROM a) u WHERE d=1`)
	if !strings.Contains(got, "division by zero") {
		t.Fatal(got)
	}
	// Correlated execution remains in the existing lateral path.
	got = memQuery(prefix + `SELECT a.d,u.d FROM a,LATERAL (SELECT a.d AS d UNION ALL SELECT a.d) u ORDER BY a.d`)
	if got != "1|1\n1|1\n2|2\n2|2" {
		t.Fatal(got)
	}
}

func TestDeferredUnionTypedDemand(t *testing.T) {
	ctx := context.Background()
	e := &executor{cache: newExecCache()}
	env := e.newEnv(ctx, map[string]ColType{"l": typeInt64}, nil)
	view := &tableMeta{Columns: []columnMeta{{Name: "r", Type: typeInt64}}}
	pairs := []equiPair{{left: &ColumnRef{Name: "l"}, right: &ColumnRef{Name: "r"}}}
	demand, err := e.deferredJoinDemand(ctx, env, pairs, []map[string]Value{{"l": intValue(typeInt64, 1)}, {"l": intValue(typeInt64, 1)}, {"l": {T: typeInt64, Null: true}}}, view)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := demand.(*Binary)
	if !ok || b.Op != "=" {
		t.Fatalf("typed duplicate-deduplicated demand: %#v", demand)
	}
	if lit, ok := b.R.(*Literal); !ok || lit.V.T != typeInt64 || lit.V.I != 1 {
		t.Fatal("lost typed key")
	}
	demand, err = e.deferredJoinDemand(ctx, env, pairs, []map[string]Value{{"l": intValue(typeInt32, 1)}}, view)
	if err != nil || demand != nil {
		t.Fatal("unsafe cross-type demand")
	}
	lv := &lazyView{names: []string{"r"}, plan: &selectPlan{items: []planItem{{typ: typeInt64}}}}
	bad := &Binary{Op: "=", L: &ColumnRef{Name: "r"}, R: &Binary{Op: "/", L: &Literal{V: intValue(typeInt64, 1)}, R: &Literal{V: intValue(typeInt64, 0)}}}
	if deferredUnionPredicate(lv, bad) != nil {
		t.Fatal("error-capable constraint pushed")
	}
}

func TestDeferredUnionTypedConstantsAndRollup(t *testing.T) {
	q := `WITH a(d,v) AS (VALUES (1,CAST(10 AS DECIMAL(7,2))),(2,CAST(20 AS DECIMAL(7,2)))) SELECT * FROM (SELECT d,v,CAST(0 AS DECIMAL(7,2)) AS z FROM a UNION ALL SELECT d,CAST(0 AS DECIMAL(7,2)),v FROM a) u WHERE d=1`
	_, p := deferredTestPlan(t, q)
	if p.meta.view == nil || !p.meta.view.unionDemand {
		t.Fatal("safe typed constant projection was not deferred")
	}
	if got := memQuery(q); got != "1|10.00|0.00\n1|0.00|10.00" {
		t.Fatal(got)
	}
	q = `WITH a(d,v) AS (VALUES (1,10),(1,10),(2,20),(NULL,30)) SELECT GROUPING(d),d,SUM(v) FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u GROUP BY ROLLUP(d) ORDER BY GROUPING(d),d NULLS FIRST`
	if got := memQuery(q); got != "0|NULL|60\n0|1|40\n0|2|40\n1|NULL|140" {
		t.Fatal(got)
	}
	// A LEFT-joined UNION branch is intentionally outside this optimization.
	_, p = deferredTestPlan(t, `WITH a(d,v) AS (VALUES (1,10),(2,20)) SELECT * FROM (SELECT d,v FROM a UNION ALL SELECT x.d,x.v FROM a x LEFT JOIN a y ON x.d=y.d) u`)
	if p.meta.view != nil && p.meta.view.unionDemand {
		t.Fatal("outer-joined branch was deferred")
	}
}

func TestDeferredUnionSharedWeekBoundary(t *testing.T) {
	// A shared weekly aggregate must include both sides of a year boundary.
	// Neither consumer may prune the fact source to its own year's dates.
	q := `WITH dates(d,w,y) AS (VALUES (1,7,2000),(2,7,2001)), a(d,v) AS (VALUES (1,10),(2,20)), u AS (SELECT d,v FROM a UNION ALL SELECT d,v FROM a), weekly AS (SELECT dates.w,SUM(u.v) AS total FROM u JOIN dates ON u.d=dates.d GROUP BY dates.w) SELECT x.total,y.total FROM (SELECT weekly.w,weekly.total FROM weekly JOIN dates ON weekly.w=dates.w WHERE dates.y=2000) x JOIN (SELECT weekly.w,weekly.total FROM weekly JOIN dates ON weekly.w=dates.w WHERE dates.y=2001) y ON x.w=y.w`
	if got := memQuery(q); got != "60|60" {
		t.Fatal(got)
	}
}
