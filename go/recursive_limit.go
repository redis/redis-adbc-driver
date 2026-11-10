// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// A bound belongs to one direct consumer, not to a shared CTE definition/cache.
// Only single-row frontiers are admitted: eager evaluation of a wider iteration
// could evaluate expressions in rows the consumer never requests.
type recursiveDemandKey struct{}
type recursiveDemand struct {
	def  *CTE
	rows int
}

func demandedRecursiveRows(ctx context.Context, def *CTE) int {
	if d, ok := ctx.Value(recursiveDemandKey{}).(recursiveDemand); ok && d.def == def {
		return d.rows
	}
	return 0
}

func (e *executor) recursiveConsumerContext(ctx context.Context, sel *SelectStmt) context.Context {
	if e.planOnly || len(sel.With) != 1 || !plainRecursiveSelect(sel) || sel.Where != nil || sel.Limit == nil || *sel.Limit <= 0 || sel.From == nil || sel.From.Schema != "" || sel.From.Catalog != "" {
		return ctx
	}
	for _, it := range sel.Items {
		if !it.Star {
			if _, ok := it.Expr.(*ColumnRef); !ok {
				return ctx
			}
		}
	}
	def, _ := e.lookupCTE(sel.From.Name)
	if def == nil || def != &sel.With[0] || !def.Recursive || def.Statement != nil || def.Search != nil || def.Cycle != nil || def.Select == nil {
		return ctx
	}
	body := def.Select
	op := body.SetOp
	if op == nil || op.Op != "UNION" || len(body.With) > 0 || len(body.OrderBy) > 0 || body.Limit != nil || body.Offset != nil || body.LimitExpr != nil || body.OffsetExpr != nil {
		return ctx
	}
	if !singleRecursiveTerm(op.Left, def.Name, false) || !singleRecursiveTerm(op.Right, def.Name, true) {
		return ctx
	}
	// Pagination has already been resolved/validated. Subtraction avoids overflow.
	n := *sel.Limit
	if n > int64(maxRecursiveRows) {
		return ctx
	}
	if sel.Offset != nil {
		if *sel.Offset > int64(maxRecursiveRows)-n {
			return ctx
		}
		n += *sel.Offset
	}
	return context.WithValue(ctx, recursiveDemandKey{}, recursiveDemand{def: def, rows: int(n)})
}

func plainRecursiveSelect(s *SelectStmt) bool {
	return s != nil && s.SetOp == nil && s.FromSelect == nil && s.FromFunc == nil && s.FromGroup == nil && len(s.Joins) == 0 && !s.FromLateral && s.FromColumns == nil && len(s.GroupBy) == 0 && s.GroupingSets == nil && s.Having == nil && s.Qualify == nil && len(s.Windows) == 0 && !s.Distinct && len(s.DistinctOn) == 0 && len(s.OrderBy) == 0 && s.Values == nil
}

func singleRecursiveTerm(s *SelectStmt, name string, recursive bool) bool {
	if s == nil || len(s.With) > 0 || s.Limit != nil || s.Offset != nil || s.LimitExpr != nil || s.OffsetExpr != nil {
		return false
	}
	copy := *s
	copy.Values = nil
	if !plainRecursiveSelect(&copy) {
		return false
	}
	if recursive {
		if s.From == nil || s.From.Name != name || s.From.Schema != "" || s.From.Catalog != "" || s.Values != nil {
			return false
		}
	} else if s.From != nil || s.Values != nil && len(s.Values) != 1 {
		return false
	}
	safe := true
	inspect := func(expr Expr) {
		walkExpr(expr, func(x Expr) {
			switch v := x.(type) {
			case *Subquery, *WindowFunc:
				safe = false
			case *Func:
				if aggregateFuncs[v.Name] || isSRF(v.Name) {
					safe = false
				}
			}
		})
	}
	for _, it := range s.Items {
		inspect(it.Expr)
	}
	for _, row := range s.Values {
		for _, expr := range row {
			inspect(expr)
		}
	}
	inspect(s.Where)
	return safe
}
