// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"math"
)

// Every server reducer sees only visible versions. Partial groups then merge
// through exact accumulators; AVG merges SUM plus non-NULL COUNT, never averages
// of averages. Existing index exactness/type proofs apply to every partial.
func (e *executor) snapshotIndexAggregate(ctx context.Context, plan *selectPlan, wp wherePlan, aggs []*Func) ([]aggGroup, bool, error) {
	if e.pushdown == PushdownNone || wp.residual != nil || wp.keys != nil || wp.none || len(plan.extraNeed) > 0 {
		return nil, false, nil
	}
	queries, ok, err := e.snapshotIndexQueries(ctx, plan.meta)
	if err != nil || !ok {
		return nil, false, err
	}
	sub := append([]*Func(nil), aggs...)
	sums := map[*Func]*Func{}
	counts := map[*Func]*Func{}
	for i, f := range aggs {
		switch f.Name {
		case "COUNT", "SUM", "MIN", "MAX", "BOOL_OR", "BOOL_AND", "EVERY":
		case "AVG":
			sum, count := *f, *f
			sum.Name, count.Name = "SUM", "COUNT"
			sums[f], counts[f] = &sum, &count
			sub[i] = &sum
			sub = append(sub, &count)
		default:
			return nil, false, nil
		}
	}
	var groupCols []string
	for _, x := range plan.sel.GroupBy {
		col, ok := x.(*ColumnRef)
		if !ok {
			return nil, false, nil
		}
		groupCols = append(groupCols, col.Name)
	}
	type partial struct {
		group aggGroup
		acc   []*accumulator
	}
	var order []string
	groups := map[string]*partial{}
	for _, query := range queries {
		meta := *plan.meta
		meta.KeyPrefix, meta.IndexName = query.segment.Prefix, query.segment.Index
		meta.SnapshotOverlay = false
		meta.SnapshotSegments = nil
		physical := *plan
		physical.meta = &meta
		where := wp
		where.query = andSnapshotQuery(wp.query, query.query)
		result, ok, err := e.indexAggregate(ctx, &physical, where, sub)
		if err != nil || !ok {
			return nil, false, err
		}
		for _, g := range result {
			vals := make([]Value, len(groupCols))
			for i, col := range groupCols {
				vals[i] = g.rep[col]
			}
			key := rowKey(vals)
			dst := groups[key]
			if dst == nil {
				dst = &partial{group: aggGroup{rep: g.rep, results: map[*Func]Value{}}, acc: make([]*accumulator, len(aggs))}
				for i, f := range aggs {
					reduce := f
					if f.Name == "BOOL_OR" || f.Name == "BOOL_AND" || f.Name == "EVERY" {
						copy := *f
						copy.Name = "MIN"
						if f.Name == "BOOL_OR" {
							copy.Name = "MAX"
						}
						reduce = &copy
					}
					dst.acc[i] = newAccumulator(reduce)
				}
				groups[key] = dst
				order = append(order, key)
			}
			for i, f := range aggs {
				a := dst.acc[i]
				if f.Name == "COUNT" {
					n := g.results[f].I
					if n < 0 || a.count > math.MaxInt64-n {
						return nil, false, nil
					}
					a.count += n
					continue
				}
				value := g.results[f]
				if f.Name == "AVG" {
					value = g.results[sums[f]]
					n := g.results[counts[f]].I
					if n < 0 || a.count > math.MaxInt64-n {
						return nil, false, nil
					}
					if !value.Null {
						a.sum.add(value)
						a.count += n
						a.sum.n = a.count
					}
					continue
				}
				a.add(value)
			}
		}
	}
	if len(order) == 0 && len(groupCols) == 0 {
		// Use the established empty-input semantics, including unsupported shapes.
		return nil, false, nil
	}
	out := make([]aggGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		for i, f := range aggs {
			typ, err := inferType(f, plan.meta.types(), nil)
			if err != nil {
				return nil, false, invalidArg(err)
			}
			value, err := g.acc[i].result(nil, typ)
			if err != nil {
				return nil, false, err
			}
			g.group.results[f] = value
		}
		out = append(out, g.group)
	}
	e.diagnostics.setPlan(map[string]any{"kind": "snapshot_index_aggregate", "visible_index_queries": len(queries)})
	return out, true, nil
}
