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
	if !directScalarAggregateInputs(aggs) || req.need == nil || !managedRowIDTable(req.meta) || len(req.meta.PendingCleanup) > 0 || req.where.residual != nil || req.where.keys != nil || req.where.none || req.hashScan || req.ties != nil || req.late != nil || req.ranges != nil || req.limit != nil || len(req.sortBy) > 0 {
		return false, nil
	}
	items := make([]planItem, len(aggs))
	for i, f := range aggs {
		if f.Star {
			items[i] = planItem{expr: &ColumnRef{Name: rowIDField}}
		} else {
			items[i] = planItem{expr: f.Args[0]}
		}
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
	if len(it.fetch) > 0 && p.hashFields == nil && p.bindHash(it.fetch) {
		p.hashExclusive = true
	}
	scratch := make([]Value, len(aggs))
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
	defer func() { p.consume = nil; p.consumeHash = nil }()
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
