// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"maps"
	"math"
)

// deferredUnionRowBound is a nonexecuting upper bound for the already guarded
// single-consumer projection/UNION tree. It neither pushes consumer year filters
// through shared aggregates nor materializes a source. Unknown layouts refuse.
func (e *executor) deferredUnionRowBound(ctx context.Context, m *tableMeta) (int64, bool) {
	if m == nil || m.view == nil || !m.view.unionDemand || !e.deferredUnionSafe(ctx, m.view.plan) {
		return 0, false
	}
	return e.deferredPlanRowBound(ctx, m.view.plan)
}
func (e *executor) deferredPlanRowBound(ctx context.Context, p *selectPlan) (int64, bool) {
	if p.setop != nil {
		a, ok := e.deferredPlanRowBound(ctx, p.setop.left)
		if !ok {
			return 0, false
		}
		b, ok := e.deferredPlanRowBound(ctx, p.setop.right)
		if !ok || b > math.MaxInt64-a {
			return 0, false
		}
		return a + b, true
	}
	m := p.meta
	if m == nil || m.lazy != nil || m.join != nil || m.group != nil || m.Live || m.Unmanaged {
		return 0, false
	}
	if m.view != nil {
		return e.deferredUnionRowBound(ctx, m)
	}
	if m.isMem {
		return int64(len(m.mem)), true
	}
	scope := snapshotOf(ctx)
	if e.store == nil || !e.store.snapshots || scope == nil || scope.pin == nil || !managedRowIDTable(m) || m.SnapshotRows == nil || len(m.PendingCleanup) != 0 || validateSnapshotDescriptor(ctx, m) != nil {
		return 0, false
	}
	var rows int64
	for _, seg := range m.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) || seg.Rows < 0 || seg.Rows > math.MaxInt64-rows {
			return 0, false
		}
		rows += seg.Rows
	}
	return rows, true
}

// scanDeferredUnionPages visits ephemeral bounded projection pages. The caller
// must not retain a page without its ordinary retained-memory admission. false
// means no input consumed; the established materialization path is unchanged.
func (e *executor) scanDeferredUnionPages(ctx context.Context, req scanRequest, params []Value, visit func([]map[string]Value) error) (bool, error) {
	// Retained execution uses cumulative coordinator allocation accounting.
	// Keep its established materialization/admission path until ephemeral
	// UNION projection accounting has a separate bounded execution proof.
	if ctx != nil && ctx.Value(retainedAllocationKey{}) != nil {
		return false, nil
	}
	if req.meta == nil || req.meta.view == nil || !req.meta.view.unionDemand || req.livePartition != nil || req.unmanagedPartition != nil || req.authoritative || len(req.dependencies) != 0 || len(req.visibility) != 0 || req.where.keys != nil || req.hashScan || req.ties != nil || req.late != nil || req.ranges != nil || req.limit != nil || len(req.sortBy) != 0 {
		return false, nil
	}
	if _, ok := e.deferredUnionRowBound(ctx, req.meta); !ok {
		return false, nil
	}
	if err := cancelled(ctx); err != nil {
		return true, err
	}
	if req.where.none {
		return true, nil
	}
	need := maps.Clone(req.need)
	if need == nil {
		need = map[string]bool{}
	}
	columnRefs(req.where.residual, need)
	env := e.newEnv(ctx, req.meta.types(), params)
	var pending error
	consume := func(rows []map[string]Value) error {
		if pending != nil {
			return nil
		}
		if req.where.residual != nil {
			kept := rows[:0]
			for _, row := range rows {
				if err := cancelled(ctx); err != nil {
					return err
				}
				env.row = row
				v, err := env.eval(req.where.residual)
				if err != nil {
					pending = invalidArg(err)
					return nil
				}
				if b, ok := truthy(v); ok && b {
					kept = append(kept, row)
				}
			}
			rows = kept
		}
		if len(rows) > 0 {
			pending = visit(rows)
		}
		return nil
	}
	// Like eager UNION materialization, every source error precedes a consumer
	// error. A pending consumer failure stops callbacks, not source validation.
	if err := e.streamDeferredUnionView(ctx, req.meta.view, req.where.residual, need, params, consume); err != nil {
		return true, err
	}
	if err := cancelled(ctx); err != nil {
		return true, err
	}
	return true, pending
}

func (e *executor) streamDeferredUnionView(ctx context.Context, lv *lazyView, predicate Expr, need map[string]bool, params []Value, visit func([]map[string]Value) error) error {
	predicate = deferredUnionPredicate(lv, predicate)
	if sp := lv.plan.setop; sp != nil {
		for _, branch := range []*selectPlan{sp.left, sp.right} {
			err := e.streamDeferredUnionView(ctx, &lazyView{plan: branch, names: lv.names, unionDemand: true}, predicate, need, params, func(rows []map[string]Value) error {
				for _, row := range rows {
					for i, name := range lv.names {
						v, ok := row[name]
						if !ok {
							continue
						}
						value, err := coerceIn(v, lv.plan.items[i].typ, e.zone())
						if err != nil {
							return invalidArg(err)
						}
						row[name] = value
					}
				}
				return visit(rows)
			})
			if err != nil {
				return err
			}
		}
		return nil
	}
	p := lv.plan
	base := p.meta
	repl := make(map[string]Expr, len(lv.names))
	for i, it := range p.items {
		repl[lv.names[i]] = it.expr
	}
	where := p.sel.Where
	for _, part := range conjuncts(predicate, nil) {
		if pushed, ok := substitute(part, repl); ok {
			where = andAll([]Expr{where, pushed})
		}
	}
	wp, err := e.planWhere(ctx, where, base, params)
	if err != nil {
		return err
	}
	baseNeed := maps.Clone(p.extraNeed)
	if baseNeed == nil {
		baseNeed = map[string]bool{}
	}
	for i, it := range p.items {
		if need[lv.names[i]] {
			columnRefs(it.expr, baseNeed)
		}
	}
	env := e.newEnv(ctx, base.types(), params)
	project := func(rows []map[string]Value) error {
		for start := 0; start < len(rows); start += pipelineChunk {
			end := min(start+pipelineChunk, len(rows))
			out := make([]map[string]Value, 0, end-start)
			for _, row := range rows[start:end] {
				if err := cancelled(ctx); err != nil {
					return err
				}
				env.row = row
				m := make(map[string]Value, len(need))
				for i, it := range p.items {
					if need[lv.names[i]] {
						v, err := env.eval(it.expr)
						if err != nil {
							return invalidArg(err)
						}
						m[lv.names[i]] = v
					}
				}
				out = append(out, m)
			}
			if err := visit(out); err != nil {
				return err
			}
		}
		return nil
	}
	subreq := scanRequest{meta: base, where: wp, need: baseNeed}
	if handled, err := e.scanDeferredUnionPages(ctx, subreq, params, project); handled {
		return err
	}
	return e.scanPages(ctx, subreq, params, project)
}
