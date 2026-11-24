// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// A narrowly admitted LEFT leaf can defer a pure projection until consumer
// demand is known. This does not turn the LEFT join into an inner join, stream
// its output, or permit arbitrary join-bearing UNION branches.
func (e *executor) deferredLeftJoinSafe(ctx context.Context, p *selectPlan) bool {
	if p == nil || p.meta == nil || p.meta.join == nil || p.sel == nil || len(p.sel.Joins) != 1 {
		return false
	}
	jp := p.meta.join
	if len(jp.items) != 2 || len(jp.merged) != 0 {
		return false
	}
	for i, it := range jp.items {
		if i == 0 && it.kind != "" || i == 1 && it.kind != "LEFT" || it.nullable != (i == 1) || it.lateral != nil || it.isolated || it.comma || it.natural || len(it.using) != 0 || len(it.scopeRels) != 0 || len(it.extra) != 0 || len(it.pushed) != 0 {
			return false
		}
		m := it.base
		if m == nil || m.view != nil || m.lazy != nil || m.join != nil || m.group != nil || m.Live || m.Unmanaged || len(m.PendingCleanup) != 0 {
			return false
		}
		if !m.isMem {
			scope := snapshotOf(ctx)
			if e.store == nil || !e.store.snapshots || scope == nil || scope.pin == nil || !managedRowIDTable(m) || m.SnapshotRows == nil || validateSnapshotDescriptor(ctx, m) != nil {
				return false
			}
			for _, seg := range m.snapshotSegments() {
				if snapshotOwns(ctx, seg.Prefix) {
					return false
				}
			}
		}
	}
	parts := conjuncts(jp.items[1].on, nil)
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		b, ok := part.(*Binary)
		if !ok || b.Op != "=" || b.nested {
			return false
		}
		left, lok := b.L.(*ColumnRef)
		right, rok := b.R.(*ColumnRef)
		if !lok || !rok || left.Outer != 0 || right.Outer != 0 || left.whole != nil || right.whole != nil {
			return false
		}
		l, lf := jp.items[0].view().column(left.Name)
		r, rf := jp.items[1].view().column(right.Name)
		if !lf || !rf {
			l, lf = jp.items[0].view().column(right.Name)
			r, rf = jp.items[1].view().column(left.Name)
		}
		if !lf || !rf || l.Type != r.Type || !(l.Type.Kind.isInteger() || l.Type.Kind == KindDate) {
			return false
		}
	}
	return true
}

// Recompute only proven preserved-left pushdown on a private copy. Mutating the
// cached join would contaminate another consumer/execution; merely attaching a
// residual to its memory metadata would not narrow the left scan at all.
func deferredLeftJoinDemand(base *tableMeta, where Expr) *tableMeta {
	source := base.join
	clone := *source
	clone.items = make([]*joinItem, len(source.items))
	for i, item := range source.items {
		copy := *item
		clone.items[i] = &copy
	}
	var left Expr
	for _, part := range conjuncts(where, nil) {
		if items, ok := clone.itemsOf(part); ok && len(items) == 1 && items[0] == 0 {
			left = andAll([]Expr{left, part})
		}
	}
	clone.planPushdown(left)
	meta := *base
	meta.join = &clone
	return &meta
}
