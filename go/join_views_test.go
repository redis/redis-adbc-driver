// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func joinViewTestSource() *tableMeta {
	return &tableMeta{isMem: true, Columns: []columnMeta{
		{Name: "id", Type: typeInt64}, {Name: "label", Type: typeString},
	}, mem: []map[string]Value{
		{"id": intValue(typeInt64, 1), "label": stringValue("first"), "__key": stringValue("source:first")},
		{"id": intValue(typeInt64, 1), "label": stringValue("again"), "__key": stringValue("source:again")},
		{"id": intValue(typeInt64, 2), "label": stringValue("second"), "__key": stringValue("source:second")},
		{"id": nullValue(typeInt64), "label": stringValue("null"), "__key": nullValue(typeString)},
	}}
}

type joinViewCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (ctx *joinViewCancelContext) Err() error {
	ctx.checks++
	if ctx.checks == 3 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestJoinViewsInitiallyRetainNoQualifiedRows(t *testing.T) {
	base := joinViewTestSource()
	var views []*tableMeta
	for _, prefix := range []string{"a.", "b.", "c.", "d.", "e.", "f."} {
		it := &joinItem{base: base, prefix: prefix}
		view := it.view()
		views = append(views, view)
		if len(view.Columns) != len(base.Columns) || view.Columns[0].Name != prefix+"id" || view.Columns[0].field() != "id" {
			t.Fatalf("qualified metadata missing for %s", prefix)
		}
	}
	for _, view := range views {
		if len(view.mem) != 0 {
			t.Fatalf("unscanned alias retained %d qualified rows; six CTE aliases must not retain six complete copies", len(view.mem))
		}
	}
	if len(base.mem) != 4 || base.mem[0]["label"].S != "first" {
		t.Fatal("building metadata views changed the cached CTE")
	}
}

func TestJoinViewsRepeatedCTEResults(t *testing.T) {
	const source = "WITH a(id,v) AS (VALUES (1,'x'),(1,'x'),(2,'y'),(NULL,'z')) "
	for _, tc := range []struct{ query, want string }{
		{"SELECT a.id,b.id FROM a,a b WHERE a.id=b.id ORDER BY a.id", "1|1\n1|1\n1|1\n1|1\n2|2"},
		{"SELECT a.id,b.id FROM a LEFT JOIN a b ON a.id=b.id ORDER BY a.id NULLS LAST", "1|1\n1|1\n1|1\n1|1\n2|2\nNULL|NULL"},
		{"SELECT a.id,b.id FROM a FULL JOIN a b ON a.id=b.id ORDER BY a.id NULLS LAST,b.id NULLS LAST", "1|1\n1|1\n1|1\n1|1\n2|2\nNULL|NULL\nNULL|NULL"},
		{"SELECT a.v,b.v FROM a JOIN a b ON a.id=b.id WHERE a.v='y' AND b.v='x'", ""},
		{"SELECT a.id,b.id FROM a JOIN (SELECT id FROM a WHERE false) b ON true", ""},
		{"SELECT a.id,b.id FROM a LEFT JOIN (SELECT id FROM a WHERE false) b ON true ORDER BY a.id NULLS LAST", "1|NULL\n1|NULL\n2|NULL\nNULL|NULL"},
		{"SELECT a.id,b.id,c.id FROM a,a b,a c WHERE a.id=b.id AND b.id=c.id AND a.id=2", "2|2|2"},
		{"SELECT a.id,x.id FROM a LEFT JOIN LATERAL (SELECT a.id) x ON a.id=2 ORDER BY a.id NULLS LAST", "1|NULL\n1|NULL\n2|2\nNULL|NULL"},
		{"SELECT a.id,b.id,c.id FROM a, (a b FULL JOIN (VALUES (3)) c(id) ON b.id=c.id) ORDER BY a.id NULLS LAST,b.id NULLS LAST,c.id NULLS LAST LIMIT 1", "1|1|NULL"},
		{"SELECT a.id,b.id FROM a,a b WHERE a.id=2 AND 1/(b.id-2)=1", "error: division by zero"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := memQuery(source + tc.query); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJoinViewsCostingUsesCachedSourceRows(t *testing.T) {
	large := joinViewTestSource()
	small := &tableMeta{isMem: true, Columns: large.Columns, mem: large.mem[:1]}
	jp := &joinPlan{items: []*joinItem{
		{base: large, prefix: "a."},
		{base: small, prefix: "b.", kind: "CROSS"},
	}}
	steps, err := (&executor{}).joinOrder(context.Background(), jp, []wherePlan{{query: "*"}, {query: "*"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].item != 1 {
		t.Fatalf("join order did not use real cached relation sizes: %+v", steps)
	}
}

func TestJoinViewScanReleasesInputButRetainsMatches(t *testing.T) {
	base := joinViewTestSource()
	base.sourceKeys = []string{"physical:first", "physical:again", "physical:second", "physical:null"}
	it := &joinItem{base: base, prefix: "a."}
	view := it.view()
	e := &executor{store: &store{}}
	where := wherePlan{query: "*", residual: &Binary{Op: "=", L: &ColumnRef{Name: "a.id"}, R: &Literal{V: intValue(typeInt64, 2)}}}
	keys, rows, err := e.scanJoinItem(context.Background(), it, scanRequest{meta: view, where: where}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.mem) != 0 {
		t.Fatal("view retained the full qualified input after filtering")
	}
	if len(rows) != 1 || rows[0]["a.label"].S != "second" || rows[0]["a.__key"].S != "source:second" {
		t.Fatalf("matched rows or provenance were lost: %+v", rows)
	}
	if len(keys) != 1 || keys[0] != "physical:second" {
		t.Fatalf("physical row keys lost alignment with matching rows: %v", keys)
	}
	rows[0]["a.label"] = stringValue("changed")
	rows[0]["presence"] = boolValue(true)
	if base.mem[2]["label"].S != "second" {
		t.Fatal("qualified scan mutated shared CTE values")
	}
	if _, ok := base.mem[2]["presence"]; ok {
		t.Fatal("join tag leaked into the cached CTE")
	}
	other := &joinItem{base: base, prefix: "b."}
	otherView := other.view()
	_, again, err := e.scanJoinItem(context.Background(), other, scanRequest{meta: otherView, where: wherePlan{query: "*"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherView.mem) != 0 || len(again) != 4 || again[2]["b.label"].S != "second" {
		t.Fatal("a later alias did not retain its independent matching row maps")
	}
}

func TestJoinViewScanReleasesInputOnErrorOrCancellation(t *testing.T) {
	base := joinViewTestSource()
	it := &joinItem{base: base, prefix: "a."}
	e := &executor{store: &store{}}
	view := it.view()
	_, _, err := e.scanJoinItem(context.Background(), it, scanRequest{meta: view, where: wherePlan{query: "*", residual: &Binary{
		Op: "=", L: &Binary{Op: "/", L: &Literal{V: intValue(typeInt64, 1)}, R: &Literal{V: intValue(typeInt64, 0)}},
		R: &Literal{V: intValue(typeInt64, 1)},
	}}}, nil)
	if err == nil || len(view.mem) != 0 {
		t.Fatalf("predicate error must release the qualified input: err=%v rows=%d", err, len(view.mem))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = e.scanJoinItem(ctx, it, scanRequest{meta: view, where: wherePlan{query: "*"}}, nil)
	if !isStatus(err, adbc.StatusCancelled) || sqlState(err) != stateQueryCanceled || len(view.mem) != 0 {
		t.Fatalf("cancellation must not retain qualified rows: err=%v rows=%d", err, len(view.mem))
	}
	active, cancel := context.WithCancel(context.Background())
	defer cancel()
	duringCopy := &joinViewCancelContext{Context: active, cancel: cancel}
	_, _, err = e.scanJoinItem(duringCopy, it, scanRequest{meta: view, where: wherePlan{query: "*"}}, nil)
	if !isStatus(err, adbc.StatusCancelled) || duringCopy.checks < 3 || len(view.mem) != 0 {
		t.Fatalf("cancellation during qualification must release partial input: err=%v checks=%d rows=%d", err, duringCopy.checks, len(view.mem))
	}
	if len(base.mem) != 4 || base.mem[0]["label"].S != "first" {
		t.Fatal("failed scans mutated the cached source")
	}
}

func TestJoinViewsTagsDoNotMutateCachedRows(t *testing.T) {
	base := joinViewTestSource()
	jp := &joinPlan{items: []*joinItem{
		{base: base, prefix: "a.", tag: "presence_a"},
		{base: base, prefix: "b.", tag: "presence_b", kind: "LEFT", nullable: true,
			on: &Binary{Op: "=", L: &ColumnRef{Name: "a.id"}, R: &ColumnRef{Name: "b.id"}}},
	}}
	rows, err := (&executor{store: &store{}}).runJoin(context.Background(), jp, map[string]bool{"a.id": true, "b.id": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("self-alias LEFT join changed duplicate/NULL multiplicity: got %d rows", len(rows))
	}
	matched := 0
	for _, row := range rows {
		if value, ok := row["presence_a"]; !ok || value != boolValue(true) {
			t.Fatal("left row's presence tag was lost")
		}
		if _, ok := row["presence_b"]; ok {
			matched++
		}
	}
	if matched != 5 {
		t.Fatalf("right presence tags do not distinguish unmatched NULL row: got %d matches", matched)
	}
	for _, row := range base.mem {
		if _, ok := row["presence_a"]; ok {
			t.Fatal("left alias tag mutated cached CTE source")
		}
		if _, ok := row["presence_b"]; ok {
			t.Fatal("right alias tag mutated cached CTE source")
		}
	}
}
