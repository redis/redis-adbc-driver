// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"io"
	"time"
)

// Only direct scalar accumulators can consume one ephemeral typed input vector.
// No callback may retain the slice; accumulator values own their data. false
// means no rows consumed and the caller must retain the established page path.
func (e *executor) scanScalarAggregateValues(ctx context.Context, req scanRequest, params []Value, aggs []*Func, visit func([]Value) error) (bool, error) {
	if !directScalarAggregateInputs(aggs) {
		return false, nil
	}
	items := aggregateInputItems(req.meta, aggs)
	return e.scanAggregateProjection(ctx, req, params, items, visit)
}

func aggregateInputItems(meta *tableMeta, aggs []*Func) []planItem {
	items := make([]planItem, len(aggs))
	for i, f := range aggs {
		if f.Star {
			field := rowIDField
			if meta != nil && meta.Unmanaged {
				field = keyField
			}
			items[i] = planItem{expr: &ColumnRef{Name: field}}
		} else {
			items[i] = planItem{expr: f.Args[0]}
		}
	}
	return items
}

// Direct grouping keys and accumulator inputs can share one ephemeral vector.
// Keep expressions/FILTER/ordered-set and representative dependencies on their
// established whole-page path, preserving evaluation/error order.
func (e *executor) scanGroupedAggregateValues(ctx context.Context, req scanRequest, params []Value, groups []Expr, aggs []*Func, repNeed map[string]bool, visit func([]Value) error) (bool, error) {
	if len(groups) == 0 || !directScalarAggregateInputs(aggs) {
		return false, nil
	}
	items := make([]planItem, 0, len(groups)+len(aggs))
	names := map[string]bool{}
	for _, expr := range groups {
		ref, ok := expr.(*ColumnRef)
		if !ok || ref.Outer != 0 || ref.whole != nil || ref.item != "" || ref.outerAgg != nil {
			return false, nil
		}
		names[ref.Name] = true
		items = append(items, planItem{expr: expr})
	}
	for name := range repNeed {
		if !names[name] {
			return false, nil
		}
	}
	items = append(items, aggregateInputItems(req.meta, aggs)...)
	return e.scanAggregateProjection(ctx, req, params, items, visit)
}

func (e *executor) scanAggregateProjection(ctx context.Context, req scanRequest, params []Value, items []planItem, visit func([]Value) error) (bool, error) {
	unmanaged := req.meta != nil && req.meta.Unmanaged
	if req.need == nil || !managedRowIDTable(req.meta) && !unmanaged || len(req.meta.PendingCleanup) > 0 || req.where.residual != nil && !unmanaged || req.where.keys != nil || req.where.none || req.hashScan || req.ties != nil || req.late != nil || req.ranges != nil && !req.meta.SnapshotOverlay || req.limit != nil || len(req.sortBy) > 0 {
		return false, nil
	}
	started := time.Now()
	it, err := e.openScan(ctx, req, params)
	if e.telemetry != nil {
		e.telemetry.fetch.Add(int64(time.Since(started)))
	}
	if err != nil {
		return true, err
	}
	defer it.close(ctx)
	p := newScanProjection(it, items)
	if p == nil {
		return false, nil
	}
	it.projection = p
	e.diagnostics.setPlan(map[string]any{"kind": "typed_aggregate_inputs", "live_unmanaged": unmanaged, "input_slots": len(items), "bound_residual": p.predicate != nil})
	if len(it.fetch) > 0 && p.hashFields == nil && p.bindHash(it.fetch) {
		p.hashExclusive = true
	}
	scratch := make([]Value, len(items))
	var reduction time.Duration
	consume := func() error {
		if e.telemetry == nil {
			return visit(scratch)
		}
		e.telemetry.rows.Add(1)
		before := time.Now()
		err := visit(scratch)
		reduction += time.Since(before)
		return err
	}
	if p.predicate != nil {
		p.predicateRow = scratch
		p.consumeMatched = consume
	}
	p.consume = func(raw aggRow) error {
		if err := p.decodeInto(raw, scratch); err != nil {
			return err
		}
		return consume()
	}
	if p.hashFields != nil {
		p.consumeHash = func(raw []any) error {
			if err := p.decodeHashInto(raw, scratch); err != nil {
				return err
			}
			return consume()
		}
	}
	defer func() { p.consume = nil; p.consumeHash = nil; p.consumeMatched = nil; p.predicateRow = nil }()
	for {
		reduction = 0
		started = time.Now()
		_, _, err := it.next(ctx)
		if e.telemetry != nil {
			e.telemetry.fetch.Add(int64(time.Since(started) - reduction))
		}
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return true, err
		}
	}
}

// Expression input errors must retain the established whole-page decode before
// reduction order, including serial replay of parallel arithmetic errors.
// Direct column accumulators cannot raise a per-row expression error; SUM/AVG
// overflow remains a result-time check after all inputs have been consumed.
func directScalarAggregateInputs(aggs []*Func) bool {
	if len(aggs) == 0 {
		return false
	}
	for _, f := range aggs {
		if f.Filter != nil || f.Distinct || len(f.OrderBy) > 0 || f.WithinGroup {
			return false
		}
		switch f.Name {
		case "COUNT", "SUM", "AVG", "MIN", "MAX":
		default:
			return false
		}
		if f.Star {
			if f.Name != "COUNT" {
				return false
			}
			continue
		}
		if len(f.Args) != 1 {
			return false
		}
		c, ok := f.Args[0].(*ColumnRef)
		if !ok || c.Outer != 0 || c.item != "" || c.outerAgg != nil {
			return false
		}
	}
	return true
}
