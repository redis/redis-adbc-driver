// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Reject potentially materializing FROM items before ordinary planning can
// execute derived tables/CTEs/views. This deliberately supports a conservative
// subset: unordered managed/live-table leaves and managed UNION ALL trees.
func (e *executor) requireIncrementalSelect(ctx context.Context, s *SelectStmt) error {
	return e.requireIncrementalSource(ctx, s, true)
}

func (e *executor) requireIncrementalSource(ctx context.Context, s *SelectStmt, allowLive bool) error {
	bad := func() error {
		return errorf(adbc.StatusNotImplemented, "stream_required rejects blocking state, subqueries, views, joins and materializing fallback; use an eligible unordered managed/live-table SELECT, bounded managed DISTINCT, or managed UNION ALL")
	}
	if len(s.With) > 0 || len(s.OrderBy) > 0 || len(s.GroupBy) > 0 || s.GroupingSets != nil || s.Having != nil || len(s.DistinctOn) > 0 || len(s.Windows) > 0 || s.Qualify != nil {
		return bad()
	}
	found := false
	visit := func(x Expr) {
		walkExprPruned(x, func(x Expr) bool {
			if _, ok := x.(*Subquery); ok {
				found = true
			}
			return !found
		})
	}
	// Pagination is resolved before scan planning; a scalar subquery here
	// must not materialize before the incremental admission decision.
	visit(s.LimitExpr)
	visit(s.OffsetExpr)
	if found {
		return bad()
	}
	if s.SetOp != nil {
		if s.SetOp.Op != "UNION" || !s.SetOp.All {
			return bad()
		}
		if err := e.requireIncrementalSource(ctx, s.SetOp.Left, false); err != nil {
			return err
		}
		return e.requireIncrementalSource(ctx, s.SetOp.Right, false)
	}
	if s.From == nil || s.FromSelect != nil || s.FromFunc != nil || len(s.Joins) > 0 || len(s.Values) > 0 {
		return bad()
	}
	visit(s.Where)
	for _, item := range s.Items {
		visit(item.Expr)
	}
	if found {
		return bad()
	}
	meta, err := e.loadTable(ctx, *s.From)
	if err != nil {
		return bad()
	}
	if !managedRowIDTable(meta) && !(allowLive && meta.Live) {
		return bad()
	}
	return nil
}
