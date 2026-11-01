// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"github.com/apache/arrow-adbc/go/adbc"
)

// Reject potentially materializing FROM items before ordinary planning can
// execute derived tables/CTEs/views. This deliberately supports a conservative
// subset: unordered managed-table leaves and unordered UNION ALL trees.
func (e *executor) requireIncrementalSelect(ctx context.Context, s *SelectStmt) error {
	bad := func() error {
		return errorf(adbc.StatusNotImplemented, "stream_required rejects blocking state, subqueries, views, joins and materializing fallback; use an unordered managed-table SELECT or UNION ALL")
	}
	if len(s.With) > 0 || len(s.OrderBy) > 0 || len(s.GroupBy) > 0 || s.GroupingSets != nil || s.Having != nil || s.Distinct || len(s.DistinctOn) > 0 || len(s.Windows) > 0 || s.Qualify != nil {
		return bad()
	}
	if s.SetOp != nil {
		if s.SetOp.Op != "UNION" || !s.SetOp.All {
			return bad()
		}
		if err := e.requireIncrementalSelect(ctx, s.SetOp.Left); err != nil {
			return err
		}
		return e.requireIncrementalSelect(ctx, s.SetOp.Right)
	}
	if s.From == nil || s.FromSelect != nil || s.FromFunc != nil || len(s.Joins) > 0 || len(s.Values) > 0 {
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
	if !managedRowIDTable(meta) {
		return bad()
	}
	return nil
}
