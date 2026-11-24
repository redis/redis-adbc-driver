// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

const leftUnionFixture = `WITH r(d,item,ord,ret) AS (VALUES (1,1,10,CAST(1.25 AS DECIMAL(7,2))),(1,1,10,CAST(1.25 AS DECIMAL(7,2))),(2,2,20,CAST(2.50 AS DECIMAL(7,2))),(1,NULL,30,CAST(3.00 AS DECIMAL(7,2))),(1,3,NULL,CAST(4.00 AS DECIMAL(7,2)))), s(site,item,ord) AS (VALUES (7,1,10),(8,1,10),(NULL,2,20),(9,NULL,30),(10,3,NULL))`
const leftUnionBody = `SELECT d,CAST(7 AS INTEGER) AS site,ret FROM r UNION ALL SELECT r.d,s.site,r.ret FROM r LEFT JOIN s ON r.item=s.item AND r.ord=s.ord`

func leftUnionPlan(t *testing.T) (*executor, *selectPlan, *lazyView, *selectPlan) {
	t.Helper()
	e, p := deferredTestPlan(t, leftUnionFixture+` SELECT * FROM (`+leftUnionBody+`) u`)
	m := p.meta
	if m.view == nil && m.join != nil && len(m.join.items) == 1 {
		m = m.join.items[0].base
	}
	if m.view == nil || !m.view.unionDemand || m.view.plan.setop == nil {
		t.Fatal("pure LEFT leaf prevented parent UNION deferral")
	}
	leaf := m.view.plan.setop.right
	if !e.deferredLeftJoinSafe(context.Background(), leaf) {
		t.Fatal("pure compound-key LEFT leaf rejected")
	}
	return e, p, m.view, leaf
}

func TestDeferredLeftUnionParentSelectionAndResults(t *testing.T) {
	_, _, _, _ = leftUnionPlan(t)
	q := leftUnionFixture + `, dates(d) AS (VALUES (1)), sites(site) AS (VALUES (7)) SELECT COUNT(*),SUM(u.ret) FROM (` + leftUnionBody + `) u,dates,sites WHERE u.d=dates.d AND u.site=sites.site`
	_, p := deferredTestPlan(t, q)
	found := false
	for _, it := range p.meta.join.items {
		if it.base.view != nil && it.base.view.unionDemand {
			found = true
		}
	}
	if !found {
		t.Fatal("actual parent join eagerly materialized the LEFT-bearing UNION")
	}
	if got := memQuery(q); got != "6|12.00" {
		t.Fatalf("compound duplicate/NULL parent join changed: %s", got)
	}
	// NULL sites include both an unmatched compound key and a matched NULL site.
	if got := memQuery(leftUnionFixture + ` SELECT COUNT(*) FROM (` + leftUnionBody + `) u WHERE site IS NULL`); got != "3" {
		t.Fatal(got)
	}
}

func TestDeferredLeftDemandClonesAndPushesOnlyPreservedSide(t *testing.T) {
	_, _, _, leaf := leftUnionPlan(t)
	jp := leaf.meta.join
	where := &Binary{Op: "AND", L: &Binary{Op: "=", L: &ColumnRef{Name: jp.items[0].prefix + "d"}, R: &Literal{V: intValue(typeInt32, 1)}}, R: &Binary{Op: "=", L: &ColumnRef{Name: jp.items[1].prefix + "site"}, R: &Literal{V: intValue(typeInt32, 7)}}}
	cloned := deferredLeftJoinDemand(leaf.meta, where)
	if cloned == leaf.meta || cloned.join == jp || cloned.join.items[0] == jp.items[0] || len(cloned.join.items[0].pushed) != 1 || len(cloned.join.items[1].pushed) != 0 {
		t.Fatal("left demand not isolated or nullable right filter pushed")
	}
	for _, it := range jp.items {
		if len(it.pushed) != 0 || len(it.extra) != 0 {
			t.Fatal("cached original join was mutated")
		}
	}
}

func TestDeferredLeftDemandNarrowsBeforeJoinWork(t *testing.T) {
	e, _, _, leaf := leftUnionPlan(t)
	left := leaf.meta.join.items[0].base
	retained := left.mem[0]
	left.mem = nil
	for range 2000 {
		row := map[string]Value{}
		for k, v := range retained {
			row[k] = v
		}
		row["d"] = intValue(typeInt32, 2)
		left.mem = append(left.mem, row)
	}
	left.mem = append(left.mem, retained)
	ctx := context.WithValue(context.Background(), retainedAllocationKey{}, newRetainedBudget(20000))
	lv := &lazyView{plan: leaf, names: []string{"d", "site", "ret"}, unionDemand: true}
	filter := &Binary{Op: "AND", L: &Binary{Op: "=", L: &ColumnRef{Name: "d"}, R: &Literal{V: intValue(typeInt32, 1)}}, R: &Binary{Op: "=", L: &ColumnRef{Name: "site"}, R: &Literal{V: intValue(typeInt32, 7)}}}
	rows, err := e.runView(ctx, lv, filter, map[string]bool{"d": true, "site": true, "ret": true}, nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("date demand was applied only after expensive join: rows%d %v", len(rows), err)
	}
	if len(leaf.meta.join.items[0].pushed) != 0 || len(leaf.meta.join.items[1].pushed) != 0 {
		t.Fatal("execution mutated reusable source plan")
	}
}

func TestDeferredLeftUnionNoInputAndNullExtension(t *testing.T) {
	e, _, _, leaf := leftUnionPlan(t)
	lv := &lazyView{plan: leaf, names: []string{"d", "site", "ret"}, unionDemand: true}
	ctx := context.WithValue(context.Background(), retainedAllocationKey{}, newRetainedBudget(0))
	rows, err := e.runView(ctx, lv, &Literal{V: boolValue(false)}, map[string]bool{"site": true}, nil)
	if err != nil || len(rows) != 0 {
		t.Fatalf("no-input leaf allocated join output: %v %v", rows, err)
	}
	if got := memQuery(leftUnionFixture + ` SELECT COUNT(*) FROM (` + leftUnionBody + `) u WHERE d=1 AND site IS NULL`); got != "2" {
		t.Fatalf("NULL compound keys must not match: %s", got)
	}
}

func TestDeferredLeftUnionUnsupportedFallback(t *testing.T) {
	for _, tail := range []string{
		`SELECT r.d,s.site,r.ret FROM r RIGHT JOIN s ON r.item=s.item AND r.ord=s.ord`,
		`SELECT r.d,s.site,r.ret FROM r FULL JOIN s ON r.item=s.item AND r.ord=s.ord`,
		`SELECT r.d,s.site,r.ret FROM r LEFT JOIN s ON r.item+1=s.item AND r.ord=s.ord`,
		`SELECT r.d,s.site,r.ret FROM r LEFT JOIN s ON r.item>s.item`,
		`SELECT r.d,s.site,r.ret FROM r LEFT JOIN s ON CAST(r.item AS BIGINT)=CAST(s.item AS BIGINT)`,
	} {
		_, p := deferredTestPlan(t, leftUnionFixture+` SELECT * FROM (SELECT d,CAST(7 AS INTEGER) AS site,ret FROM r UNION ALL `+tail+`) u`)
		m := p.meta
		if m.view == nil && m.join != nil && len(m.join.items) == 1 {
			m = m.join.items[0].base
		}
		if m.view != nil && m.view.unionDemand {
			t.Fatalf("unsupported LEFT/outer leaf deferred: %s", tail)
		}
	}
	// Volatile or error-capable projections retain eager behavior.
	q := leftUnionFixture + ` SELECT * FROM (SELECT d,CAST(7 AS INTEGER) AS site,random() AS ret FROM r UNION ALL SELECT r.d,s.site,random() FROM r LEFT JOIN s ON r.item=s.item AND r.ord=s.ord) u`
	_, p := deferredTestPlan(t, q)
	if p.meta.view != nil && p.meta.view.unionDemand {
		t.Fatal("volatile projection deferred")
	}
	q = leftUnionFixture + ` SELECT d FROM (SELECT d,CAST(7 AS INTEGER) AS site,ret FROM r UNION ALL SELECT r.d,s.site,ret/(r.d-2) FROM r LEFT JOIN s ON r.item=s.item AND r.ord=s.ord) u WHERE d=1`
	if got := memQuery(q); !strings.Contains(got, "division by zero") {
		t.Fatalf("projected error suppressed: %s", got)
	}
}

func TestDeferredLeftNativeSourceRequiresPinnedPublished(t *testing.T) {
	e, _, _, leaf := leftUnionPlan(t)
	native, ctx, h := snapshotPageFixture(t)
	native.meta.Columns = append([]columnMeta(nil), leaf.meta.join.items[0].base.Columns...)
	leaf.meta.join.items[0].base = native.meta
	e.store = native.store
	h.mutate = func(_ goredis.Cmder) error { t.Fatal("eligibility must not read native data"); return nil }
	if !e.deferredLeftJoinSafe(ctx, leaf) {
		t.Fatal("pinned published source rejected")
	}
	scope := snapshotOf(ctx)
	scope.write = true
	scope.owned = map[string]bool{snapshotDataID(native.meta.prefix()): true}
	if e.deferredLeftJoinSafe(ctx, leaf) {
		t.Fatal("private mutable source admitted")
	}
	scope.write = false
	scope.pin = nil
	if e.deferredLeftJoinSafe(ctx, leaf) {
		t.Fatal("unpinned native source admitted")
	}
}
