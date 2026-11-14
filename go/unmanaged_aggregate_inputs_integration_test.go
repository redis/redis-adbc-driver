// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

func TestUnmanagedAggregateInputVectors(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	if err := raw.Do(h.ctx, "FT.ALTER", "app_idx", "SCHEMA", "ADD", "extra", "TAG").Err(); err != nil {
		t.Fatal(err)
	}
	binaryKey := "app:" + string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(binaryKey, 0) {
		t.Fatal("fixture lacks actual NUL key")
	}
	for key, fields := range map[string]map[string]any{
		"app:second": {"n": "10", rowIDField: "not a driver id"},
		"app:null":   {"extra": "only an unrelated field"},
		binaryKey:    {"n": "9007199254740993"},
	} {
		if err := raw.HSet(h.ctx, key, fields).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT CAST(COUNT(*) AS BIGINT),CAST(SUM(n) AS BIGINT) FROM live",
		"SELECT CAST(COUNT(*) AS BIGINT),CAST(SUM(n) AS BIGINT) FROM live WHERE n>=10",
	} {
		want := "4|9007199254741013"
		if query[len(query)-1] == '0' {
			want = "3|9007199254741013"
		}
		schema := h.expectRows(query, want)
		expectTypeIDs(t, schema, arrow.INT64, arrow.INT64)
		ctx, e, plan, wp := distinctSnapshotTestPlan(t, h, query)
		e.diagnostics = &executionDiagnostics{}
		groups, err := e.serialDriverAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil || len(groups) != 1 {
			t.Fatal(groups, err)
		}
		var profile map[string]any
		if p := e.diagnostics.plan.Load(); p == nil {
			t.Fatal("scalar input vector has no plan evidence")
		} else if err := json.Unmarshal(*p, &profile); err != nil || profile["kind"] != "typed_aggregate_inputs" {
			t.Fatal("scalar input vector not used", profile, err)
		}
		if _, _, ok := h.planOf(query); ok {
			t.Fatal("live unmanaged index acquired reducer permission")
		}
	}
	h.expectRows("SELECT n,CAST(COUNT(*) AS BIGINT),CAST(SUM(n) AS BIGINT) FROM live GROUP BY n ORDER BY n NULLS LAST", "10|2|20", "9007199254740993|1|9007199254740993", "NULL|1|NULL")
	ctx, e, plan, wp := distinctSnapshotTestPlan(t, h, "SELECT n,COUNT(*),SUM(n) FROM live GROUP BY n")
	req := scanRequest{meta: plan.meta, where: wp, need: map[string]bool{"n": true}}
	var got [][]Value
	pointers := map[uintptr]bool{}
	handled, err := e.scanGroupedAggregateValues(ctx, req, nil, plan.sel.GroupBy, plan.aggregateCalls(), map[string]bool{"n": true}, func(values []Value) error {
		pointers[reflect.ValueOf(values).Pointer()] = true
		got = append(got, slices.Clone(values))
		return nil
	})
	if !handled || err != nil || len(got) != 4 || len(pointers) != 1 {
		t.Fatal("group vector contents/reuse", handled, err, len(got), len(pointers))
	}
	for _, row := range got {
		if row[1].T.Kind != KindString || !unmanagedSourceKey(row[1].S) || !reflect.DeepEqual(row[0], row[2]) {
			t.Fatal("COUNT(*) or duplicate input did not use source value", row)
		}
	}
	sentinel := errors.New("stop aggregate input")
	handled, err = e.scanScalarAggregateValues(ctx, req, nil, plan.aggregateCalls(), func([]Value) error { return sentinel })
	if !handled || !errors.Is(err, sentinel) {
		t.Fatal("consumer failure lost", handled, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.scanScalarAggregateValues(cctx, req, nil, plan.aggregateCalls(), func([]Value) error { return nil }); err == nil {
		t.Fatal("cancellation ignored")
	}
	// Unsupported arithmetic preserves the whole-page expression evaluator.
	aggs := []*Func{{Name: "SUM", Args: []Expr{&Binary{Op: "/", L: &ColumnRef{Name: "n"}, R: &Literal{V: intValue(typeInt64, 0)}}}}}
	handled, err = e.scanScalarAggregateValues(ctx, req, nil, aggs, func([]Value) error { t.Fatal("unsupported input consumed"); return nil })
	if handled || err != nil {
		t.Fatal(handled, err)
	}
	h.expectRows("SELECT COUNT(*) FROM live", "4")
}
