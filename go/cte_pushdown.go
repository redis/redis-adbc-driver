// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A WITH query is materialized once for all its references, before the
// referencing query's WHERE is planned, so a filter on one reference reads
// the CTE's whole input. TPC-DS Q39 computes an inventory CTE grouped by
// month over a whole year and then keeps two months, one per reference
// (inv1.d_moy = 1, inv2.d_moy = 2): at SF1 that is 2.34M inventory rows
// instead of 360,000.
//
// ctePushdowns finds, for each CTE among a SELECT's FROM items, the WHERE
// conjuncts that read only one of its references. When every reference to
// it has some, resolveFromItem evaluates the CTE once for all of them, with
// the OR of the references' conjuncts added to its body (pushIntoSelect):
// through its projection, the WHERE of a single derived table, and a GROUP
// BY when they read only its plain grouping columns. The references share
// that evaluation, as they share an unpushed one (TPC-DS Q64's 18-table
// cross_sales is too costly to compute per reference), and the outer WHERE
// still applies every conjunct to each, so only rows no reference could
// keep are dropped earlier. A CTE with a reference that has no pushable
// conjunct is materialized in full as before.
//
// Only FROM lists of inner (and comma) joins are considered, and only
// conjuncts that cannot fail when evaluated on rows the outer query would
// have dropped first: comparisons, AND, OR, NOT and IS [NOT] NULL of the
// reference's columns, with constant operands (pushableCopy).

// ctePushdownEnabled lets tests compare results with and without the
// pushdown of WHERE conjuncts into CTE references.
var ctePushdownEnabled = true

// ctePushdown is the filter a CTE's references in one FROM list share (the
// OR of each reference's pushable conjuncts, over the CTE's output names),
// and the evaluation resolveFromItem makes with it, once.
type ctePushdown struct {
	pred Expr
	done bool
	meta *tableMeta // nil when nothing was pushed: read the shared CTE
}

// ctePushdowns returns the shared pushdown of each CTE reference among sel's
// FROM items whose every reference has pushable WHERE conjuncts.
func ctePushdowns(sel *SelectStmt) map[*TableName]*ctePushdown {
	if !ctePushdownEnabled || sel.Where == nil || sel.SetOp != nil || sel.FromGroup != nil || sel.FromFunc != nil ||
		sel.FromLateral || sel.FromColumns != nil {
		return nil
	}
	type ref struct {
		table *TableName
		name  string
	}
	var refs []ref
	count := map[string]int{}
	add := func(t *TableName, alias string) {
		name := alias
		if t != nil && name == "" {
			name = t.Name
		}
		count[name]++
		if t != nil && t.Schema == "" && t.Catalog == "" {
			refs = append(refs, ref{t, name})
		}
	}
	add(sel.From, sel.FromAlias)
	for _, jc := range sel.Joins {
		switch jc.Kind {
		case "", "INNER", "CROSS":
		default:
			return nil // an outer join: its WHERE filters NULL-extended rows
		}
		if jc.Lateral || jc.Natural || len(jc.Using) > 0 || jc.Group != nil || jc.Func != nil || jc.Columns != nil {
			return nil
		}
		add(jc.Table, jc.Alias)
	}
	conds := map[*TableName][]Expr{}
	for _, c := range conjuncts(sel.Where, nil) {
		q, ok := soleQualifier(c)
		if !ok || count[q] != 1 {
			continue
		}
		for _, r := range refs {
			if r.name != q {
				continue
			}
			p, ok := pushableCopy(c, func(col *ColumnRef) (Expr, bool) {
				return &ColumnRef{Name: col.writtenName()}, true
			})
			if ok {
				conds[r.table] = append(conds[r.table], p)
			}
		}
	}
	byCTE := map[string][]*TableName{}
	for _, r := range refs {
		byCTE[r.table.Name] = append(byCTE[r.table.Name], r.table)
	}
	var out map[*TableName]*ctePushdown
	for _, tables := range byCTE {
		var alts []Expr
		for _, t := range tables {
			if len(conds[t]) == 0 {
				alts = nil
				break
			}
			alts = append(alts, andAll(conds[t]))
		}
		if len(alts) == 0 {
			continue
		}
		pred := alts[0]
		for _, x := range alts[1:] {
			pred = &Binary{Op: "OR", L: pred, R: x}
		}
		shared := &ctePushdown{pred: pred}
		if out == nil {
			out = map[*TableName]*ctePushdown{}
		}
		for _, t := range tables {
			out[t] = shared
		}
	}
	return out
}

// soleQualifier returns the qualifier every column of x is read through,
// when there is at least one column and they all have the same one.
func soleQualifier(x Expr) (string, bool) {
	q, ok := "", true
	walkExpr(x, func(n Expr) {
		c, isCol := n.(*ColumnRef)
		if !isCol {
			return
		}
		if c.Qualifier == "" || c.Schema != "" || c.Catalog != "" || c.Outer != 0 || q != "" && c.Qualifier != q {
			ok = false
		}
		q = c.Qualifier
	})
	return q, ok && q != ""
}

// pushableCopy copies x, a conjunct that cannot fail on any row, with each
// column replaced by col's result. It returns false for any other
// expression: only comparisons, AND, OR, NOT and IS [NOT] NULL, whose
// operands are columns or constants (literals and parameters, and
// arithmetic, negation or casts of constants only).
func pushableCopy(x Expr, col func(*ColumnRef) (Expr, bool)) (Expr, bool) {
	var cp func(Expr, bool) (Expr, bool)
	cp = func(x Expr, constant bool) (Expr, bool) {
		switch v := x.(type) {
		case *ColumnRef:
			if constant || v.whole != nil || v.outerAgg != nil || v.item != "" {
				return nil, false
			}
			return col(v)
		case *Literal:
			l := *v
			return &l, true
		case *Param:
			return v, true
		case *Binary:
			if v.nested || v.keyDemand {
				return nil, false
			}
			switch v.Op {
			case "AND", "OR", "=", "<>", "!=", "<", "<=", ">", ">=":
				if constant {
					return nil, false
				}
			case "+", "-", "*", "/", "%":
				if !constant {
					// Arithmetic of columns can fail (overflow, division by
					// zero) on a row the outer query never evaluates.
					if !isConstantTree(v) {
						return nil, false
					}
					constant = true
				}
			default:
				return nil, false
			}
			l, ok := cp(v.L, constant)
			if !ok {
				return nil, false
			}
			r, ok := cp(v.R, constant)
			if !ok {
				return nil, false
			}
			return &Binary{Op: v.Op, L: l, R: r}, true
		case *Unary:
			if v.Op != "NOT" && !isConstantTree(v) {
				return nil, false
			}
			if v.Op == "NOT" && constant {
				return nil, false
			}
			inner, ok := cp(v.X, constant || v.Op != "NOT")
			if !ok {
				return nil, false
			}
			return &Unary{Op: v.Op, X: inner}, true
		case *Cast:
			if v.OnError != nil || !isConstantTree(v.X) {
				return nil, false
			}
			inner, ok := cp(v.X, true)
			if !ok {
				return nil, false
			}
			c := *v
			c.X = inner
			return &c, true
		case *IsNull:
			if v.field || constant {
				return nil, false
			}
			inner, ok := cp(v.X, false)
			if !ok {
				return nil, false
			}
			return &IsNull{X: inner, Not: v.Not}, true
		}
		return nil, false
	}
	return cp(x, false)
}

// isConstantTree reports whether x reads no column and calls nothing.
func isConstantTree(x Expr) bool {
	ok := true
	walkExpr(x, func(n Expr) {
		switch n.(type) {
		case *ColumnRef, *Func, *Subquery, *WindowFunc, *RowExpr:
			ok = false
		}
	})
	return ok
}

// pushIntoSelect returns a copy of sel whose WHERE also has preds, which
// read sel's output columns by name (columns renames them, as a CTE's column
// list does), or false if none can be pushed. A predicate is pushed when
// every output it reads is a plain column of sel's FROM items, and, if sel
// groups, one of its plain GROUP BY columns: filtering a group's rows before
// grouping drops exactly the groups whose grouping value fails it. When
// sel's FROM is a single derived table, the pushed predicates also go into
// it the same way. Nothing is pushed through LIMIT/OFFSET, DISTINCT ON,
// grouping sets, windows, QUALIFY, set operations or volatile functions.
func pushIntoSelect(sel *SelectStmt, columns []string, preds []Expr) (*SelectStmt, bool) {
	if sel == nil || len(preds) == 0 || sel.SetOp != nil || sel.Values != nil || sel.Limit != nil || sel.Offset != nil ||
		sel.LimitExpr != nil || sel.OffsetExpr != nil || len(sel.DistinctOn) > 0 || len(sel.GroupingSets) > 0 ||
		sel.Qualify != nil || len(sel.Windows) > 0 || sel.FromFunc != nil || sel.FromLateral || sel.FromColumns != nil {
		return nil, false
	}
	grouped := len(sel.GroupBy) > 0 || sel.Having != nil
	outputs := map[string]*ColumnRef{}
	ambiguous := map[string]bool{}
	for i, it := range sel.Items {
		if it.Star {
			return nil, false
		}
		unsafe := false
		walkExpr(it.Expr, func(x Expr) {
			switch v := x.(type) {
			case *WindowFunc:
				unsafe = true
			case *Func:
				if volatileFuncs[v.Name] {
					unsafe = true
				}
				if aggregateFuncs[v.Name] {
					grouped = true
				}
			}
		})
		if unsafe {
			return nil, false
		}
		c, isCol := it.Expr.(*ColumnRef)
		name := it.Alias
		if name == "" && isCol {
			name = c.writtenName()
		}
		if i < len(columns) {
			name = columns[i]
		}
		if name == "" {
			continue
		}
		if _, dup := outputs[name]; dup || ambiguous[name] {
			delete(outputs, name)
			ambiguous[name] = true
			continue
		}
		if !isCol || c.Outer != 0 || c.whole != nil || c.outerAgg != nil || c.item != "" || c.writtenName() == "*" {
			ambiguous[name] = true
			continue
		}
		outputs[name] = c
	}
	group := sel.writtenGroupBy
	if group == nil {
		group = sel.GroupBy
	}
	grouping := func(c *ColumnRef) bool {
		for _, g := range group {
			if gc, ok := g.(*ColumnRef); ok && gc.writtenName() == c.writtenName() && gc.Qualifier == c.Qualifier && gc.Schema == c.Schema && gc.Catalog == c.Catalog {
				return true
			}
		}
		return false
	}
	var pushed []Expr
	for _, p := range preds {
		q, ok := pushableCopy(p, func(r *ColumnRef) (Expr, bool) {
			c, ok := outputs[r.writtenName()]
			if !ok || r.Qualifier != "" || grouped && !grouping(c) {
				return nil, false
			}
			return &ColumnRef{Name: c.writtenName(), Qualifier: c.Qualifier, Schema: c.Schema, Catalog: c.Catalog}, true
		})
		if ok {
			pushed = append(pushed, q)
		}
	}
	if len(pushed) == 0 {
		return nil, false
	}
	out := *sel
	where := pushed
	if sel.Where != nil {
		where = append([]Expr{sel.Where}, pushed...)
	}
	out.Where = andAll(where)
	if len(sel.Joins) == 0 && sel.FromGroup == nil && sel.FromSelect != nil {
		var inner []Expr
		for _, p := range pushed {
			q, ok := pushableCopy(p, func(r *ColumnRef) (Expr, bool) {
				if r.Schema != "" || r.Qualifier != "" && r.Qualifier != sel.FromAlias {
					return nil, false
				}
				return &ColumnRef{Name: r.writtenName()}, true
			})
			if ok {
				inner = append(inner, q)
			}
		}
		if sub, ok := pushIntoSelect(sel.FromSelect, nil, inner); ok {
			out.FromSelect = sub
		}
	}
	return &out, true
}

// pushedCTERelation materializes def for one reference with preds pushed
// into its body, or returns false to read the shared materialization: for
// a recursive or data-modifying CTE, a body nothing can be pushed into, a
// CTE that reads an enclosing query (planned lazily), or a pushed body that
// fails to plan for a reason other than the statement's own end or Redis.
func (e *executor) pushedCTERelation(ctx context.Context, def *CTE, list int, preds []Expr) (*tableMeta, bool, error) {
	if def.Statement != nil || def.Recursive || def.Search != nil || def.Cycle != nil || def.Select == nil {
		return nil, false, nil
	}
	body, ok := pushIntoSelect(def.Select, def.Columns, preds)
	if !ok {
		return nil, false, nil
	}
	pushed := *def
	pushed.Select = body
	level := min(e.cteLevel[list], len(e.scopes))
	m, err := e.planCTE(ctx, &pushed, list, level)
	if err != nil {
		var ae adbc.Error
		if cancelled(ctx) != nil || errors.As(err, &ae) && (ae.Code == adbc.StatusIO || ae.Code == adbc.StatusTimeout || ae.Code == adbc.StatusCancelled) {
			return nil, false, err
		}
		return nil, false, nil
	}
	if m.lazy != nil {
		return nil, false, nil
	}
	return m, true, nil
}
