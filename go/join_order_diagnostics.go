// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"math"
)

// Only opt-in diagnostics allocates this sanitized plan. Input positions refer
// to the existing SQL graph; no names, predicates, physical keys or URLs escape.
func (e *executor) recordJoinOrderDiagnostics(ctx context.Context, jp *joinPlan, wps []wherePlan, counts []int64, steps []joinStep) {
	if e.diagnostics == nil || e.diagnostics.metricsOnly {
		return
	}
	inputs := make([]map[string]any, len(jp.items))
	for i, it := range jp.items {
		m := it.base
		sourceRows := int64(-1)
		if m.isMem {
			sourceRows = int64(len(m.mem))
		} else if m.SnapshotRows != nil {
			sourceRows = 0
			for _, seg := range m.snapshotSegments() {
				if seg.Rows < 0 || seg.Rows > math.MaxInt64-sourceRows {
					sourceRows = -1
					break
				}
				sourceRows += seg.Rows
			}
		}
		wp := wps[i]
		inputs[i] = map[string]any{
			"input": i, "source_rows": sourceRows, "candidate_rows": counts[i],
			"in_memory": m.isMem, "snapshot_overlay": m.SnapshotOverlay,
			"index_filter": wp.query != "" && wp.query != "*", "residual_filter": wp.residual != nil,
			"local_filter_count":     len(it.pushed),
			"compiled_scalar_filter": wp.residual != nil && bindScanPredicate(wp.residual, it.view().types(), e.params, e.zone()) != nil,
			"large_published_native": counts[i] > maxUnionTerms && e.publishedJoinSource(ctx, m),
		}
	}
	order := make([]int, len(steps))
	for i, step := range steps {
		order[i] = step.item
	}
	var scope *snapshotScope
	if ctx != nil {
		scope = snapshotOf(ctx)
	}
	scopeRead := map[string]bool{"snapshot": scope != nil, "pin": false, "shared_pin": false, "store_match": false, "maintenance": false, "operation": false, "write": false, "retained": e.retained != nil, "partition_executor": e.partitionDirectTable != nil || e.partitionForeignTable != ""}
	if scope != nil {
		scopeRead["operation"] = scope.operation
		scopeRead["write"] = scope.write
		scopeRead["pin"] = scope.pin != nil
		scopeRead["shared_pin"] = scope.pin != nil && scope.pin.shared != nil
		scopeRead["store_match"] = scope.store == e.store
		scopeRead["maintenance"] = scope.maintenance
	}
	if ctx != nil && ctx.Value(retainedAllocationKey{}) != nil {
		scopeRead["retained"] = true
	}
	e.diagnostics.setPlan(map[string]any{"kind": "lookup_join_order", "inputs": inputs, "order": order, "scope": scopeRead})
}
