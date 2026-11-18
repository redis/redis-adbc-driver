// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"os"
)

// A single direct COUNT DISTINCT needs only SQL equality keys. Reuse the
// external exact merge instead of retaining a cardinality-sized map. Source
// membership, live HASH images, decoding and residuals stay with the scan.
func (e *executor) unmanagedCountDistinct(ctx context.Context, plan *selectPlan, wp wherePlan, aggs []*Func, params []Value) ([]aggGroup, bool, error) {
	if !plan.meta.Unmanaged || e.store.flexEnabled() || e.gpu != nil || len(aggs) != 1 || len(plan.sel.GroupBy) != 0 || len(plan.extraNeed) != 0 || len(plan.outerAggs) != 0 || len(aggregateRepresentativeNeed(plan, aggs)) != 0 {
		return nil, false, nil
	}
	f := aggs[0]
	if f.Name != "COUNT" || !f.Distinct || f.Star || f.Filter != nil || len(f.Args) != 1 || len(f.OrderBy) != 0 || f.WithinGroup {
		return nil, false, nil
	}
	ref, ok := f.Args[0].(*ColumnRef)
	if !ok || ref.Outer != 0 || ref.whole != nil || ref.item != "" || ref.outerAgg != nil {
		return nil, false, nil
	}
	spill := tupleSpill{prefix: "distinct"}
	defer func() {
		if spill.dir != "" {
			_ = os.RemoveAll(spill.dir)
		}
	}()
	req := scanRequest{meta: plan.meta, where: wp, need: map[string]bool{ref.Name: true}}
	e.diagnostics.setPhase("unmanaged_distinct")
	handled, err := e.scanAggregateProjection(ctx, req, params, []planItem{{expr: ref}}, func(values []Value) error {
		if values[0].Null {
			return nil
		}
		if spill.dir == "" {
			dir, err := os.MkdirTemp("", "redis-adbc-distinct-*")
			if err != nil {
				return wrapRedis(err, "creating distinct spill directory failed")
			}
			spill.dir = dir
		}
		return spill.add(ctx, spillTuple{key: rowKey(values), count: 1})
	})
	if err != nil || !handled {
		return nil, handled, err
	}
	var count int64
	err = spill.merge(ctx, func(spillTuple) error {
		count++
		return nil
	})
	if err != nil {
		return nil, true, wrapRedis(err, "merging exact distinct inputs failed")
	}
	if err := cancelled(ctx); err != nil {
		return nil, true, err
	}
	return []aggGroup{{rep: map[string]Value{}, results: map[*Func]Value{f: intValue(typeInt64, count)}}}, true, nil
}
