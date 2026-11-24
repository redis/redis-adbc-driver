// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestJoinOrderDiagnosticsActualQ8CountsAndSelection(t *testing.T) {
	e, p, ctx, _ := nativeQ8OrderFixture(t)
	e.diagnostics = &executionDiagnostics{}
	jp := p.meta.join
	wps := make([]wherePlan, len(jp.items))
	for i, it := range jp.items {
		var err error
		wps[i], err = e.planWhere(ctx, andAll(it.pushed), it.view(), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.joinOrder(ctx, jp, wps); err != nil {
		t.Fatal(err)
	}
	raw := e.diagnostics.plan.Load()
	if raw == nil {
		t.Fatal("opt-in diagnostics missed real parsed join selection")
	}
	var marker map[string]any
	if err := json.Unmarshal(*raw, &marker); err != nil {
		t.Fatal(err)
	}
	if marker["kind"] != "lookup_join_order" || !reflect.DeepEqual(marker["order"], []any{float64(3), float64(2), float64(1), float64(0)}) {
		t.Fatalf("wrong actual order: %v", marker)
	}
	inputs := marker["inputs"].([]any)
	for i, want := range []float64{2880404, 91, 12, 5} {
		entry := inputs[i].(map[string]any)
		if entry["candidate_rows"] != want || entry["source_rows"] != want || entry["in_memory"] != (i != 0) || entry["snapshot_overlay"] != false {
			t.Fatalf("input%d=%v", i, entry)
		}
	}
	if !inputs[0].(map[string]any)["large_published_native"].(bool) || !inputs[1].(map[string]any)["compiled_scalar_filter"].(bool) {
		t.Fatal("eligibility/local compiled filter flags missing")
	}
	scope := marker["scope"].(map[string]any)
	if scope["snapshot"] != true || scope["operation"] != false || scope["write"] != false {
		t.Fatalf("wrong source scope: %v", scope)
	}
	serialized := string(*raw)
	for _, forbidden := range []string{jp.items[0].base.prefix(), jp.items[0].base.index(), "http://", "https://", "@ss_"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("diagnostics leaked non-sanitized source material: %s", fmt.Sprintf("%q", forbidden))
		}
	}
}

func TestJoinOrderDiagnosticsOptInBeforeAssembly(t *testing.T) {
	e := &executor{}
	// Invalid source arguments are intentional: opt-out must return before any
	// metadata, scope, source proof or plan allocation can be attempted.
	e.recordJoinOrderDiagnostics(nil, nil, nil, nil, nil)
	e.diagnostics = &executionDiagnostics{metricsOnly: true}
	e.recordJoinOrderDiagnostics(nil, nil, nil, nil, nil)
	if e.diagnostics.plan.Load() != nil {
		t.Fatal("metrics-only request assembled an execution plan")
	}
}
