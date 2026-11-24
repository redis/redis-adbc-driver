// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"testing"
)

const unionWeeklyFixture = `WITH dates(d,w,y,day) AS (VALUES (1,7,2000,'Sunday'),(2,7,2001,'Monday'),(3,8,2001,'Sunday')), a(d,v) AS (VALUES (1,CAST(10.05 AS DECIMAL(7,2))),(1,CAST(10.05 AS DECIMAL(7,2))),(2,CAST(2.20 AS DECIMAL(7,2))),(3,CAST(NULL AS DECIMAL(7,2)))), u AS (SELECT d,v FROM (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) projected)`
const unionWeeklyAggregate = `SELECT dates.w,SUM(CASE WHEN dates.day='Sunday' THEN u.v ELSE NULL END) AS sun,SUM(CASE WHEN dates.day='Monday' THEN u.v ELSE NULL END) AS mon FROM u,dates WHERE u.d=dates.d GROUP BY dates.w`

func TestUnionStarWeeklyCaseVisitorComposition(t *testing.T) {
	e, p := deferredTestPlan(t, unionWeeklyFixture+" "+unionWeeklyAggregate+" ORDER BY dates.w")
	ctx := context.Background()
	if !streamingStarAggregate(p, p.aggregateCalls()) {
		t.Fatal("exact weekly CASE plan failed aggregate gate")
	}
	need := map[string]bool{}
	for _, c := range p.meta.Columns {
		need[c.Name] = true
	}
	wp, err := e.planWhere(ctx, p.sel.Where, p.meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	shape, ok := e.planStarShape(ctx, p.meta.join, need, wp.residual)
	if !ok || p.meta.join.items[shape.fact].base.view == nil || !p.meta.join.items[shape.fact].base.view.unionDemand {
		t.Fatal("deferred UNION was not admitted as the bounded streamed fact")
	}
	rows, maxPage := 0, 0
	err = e.scanPages(ctx, scanRequest{meta: p.meta, where: wp, need: need, streamJoin: true}, nil, func(page []map[string]Value) error { rows += len(page); maxPage = max(maxPage, len(page)); return nil })
	if err != nil || rows != 8 || maxPage > starOutputPage {
		t.Fatalf("composed visitor rows%d max%d err%v", rows, maxPage, err)
	}
	if got := memQuery(unionWeeklyFixture + " " + unionWeeklyAggregate + " ORDER BY dates.w"); got != "7|40.20|4.40\n8|NULL|NULL" {
		t.Fatalf("exact complete weekly result: %s", got)
	}
}

func TestUnionStarSharedWeeklyBoundaryMaterializesOnce(t *testing.T) {
	q := unionWeeklyFixture + `, weekly AS (` + unionWeeklyAggregate + `) SELECT x.sun,y.mon FROM (SELECT weekly.w,weekly.sun FROM weekly,dates WHERE weekly.w=dates.w AND dates.y=2000) x JOIN (SELECT weekly.w,weekly.mon FROM weekly,dates WHERE weekly.w=dates.w AND dates.y=2001) y ON x.w=y.w WHERE x.w=7`
	e, _ := deferredTestPlan(t, q)
	weekly := 0
	for def, m := range e.cache.ctes {
		if def.Name == "weekly" {
			weekly++
			if m.view != nil || len(m.mem) != 2 {
				t.Fatal("shared weekly source became consumer-filtered/deferred")
			}
		}
	}
	if weekly != 1 {
		t.Fatal("shared weekly materialization missing")
	}
	if got := memQuery(q); got != "40.20|4.40" {
		t.Fatalf("consumer year pruned a boundary week: %s", got)
	}
}

func TestUnionStarRejectsDeferredDimensionBeforeReading(t *testing.T) {
	e, req := starTestFixture(23)
	_, source := deferredTestPlan(t, `WITH a(id,v) AS (VALUES (CAST(1 AS BIGINT),CAST(2 AS BIGINT))) SELECT * FROM (SELECT id,v FROM a UNION ALL SELECT id,v FROM a) u`)
	req.meta.join.items[0].base = source.meta
	if _, ok := e.planStarShape(context.Background(), req.meta.join, req.need, req.where.residual); ok {
		t.Fatal("deferred UNION admitted as a retained dimension")
	}
}

func TestStarParsedPlanSelectsStreamingDiagnostic(t *testing.T) {
	e, p := deferredTestPlan(t, `WITH d(id,g) AS (VALUES (1,'a'),(2,'b')), f(id,v) AS (VALUES (1,10),(1,20),(2,30),(3,40),(NULL,50)) SELECT d.g,SUM(f.v) FROM d,f WHERE d.id=f.id GROUP BY d.g ORDER BY d.g`)
	tags := 0
	for _, it := range p.meta.join.items {
		if it.tag != "" {
			tags++
		}
	}
	if tags != 2 {
		t.Fatal("fixture must retain real parsed presence tags")
	}
	e.diagnostics = &executionDiagnostics{}
	rows, err := e.runSelect(context.Background(), p, nil)
	if err != nil || len(rows) != 2 {
		t.Fatalf("parsed aggregate: %v %v", rows, err)
	}
	raw := e.diagnostics.plan.Load()
	if raw == nil {
		t.Fatal("real parsed query silently used eager fallback")
	}
	var marker map[string]any
	if err := json.Unmarshal(*raw, &marker); err != nil {
		t.Fatal(err)
	}
	if marker["kind"] != "streaming_star_join" || marker["dimension_count"] != float64(1) || marker["output_page_rows"] != float64(starOutputPage) || len(marker) != 3 {
		t.Fatalf("unexpected/public-unsafe stream marker: %v", marker)
	}
	need := map[string]bool{}
	for _, it := range p.meta.join.items {
		need[it.tag] = true
	}
	if _, ok := e.planStarShape(context.Background(), p.meta.join, need, p.sel.Where); ok {
		t.Fatal("requested presence/whole-row dependency bypassed fallback")
	}
}
