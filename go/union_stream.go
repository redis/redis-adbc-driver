// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io"
)

// unionStreamable deliberately excludes blocking leaves and ordering. Keep the
// pairwise tree instead of flattening it: coercion at an inner boundary can
// round a value before an outer boundary widens it again.
func (p *selectPlan) unionStreamable() bool {
	if len(p.sel.With) != 0 {
		return false
	}
	for _, it := range p.items {
		if !fixedUnionType(it.typ) {
			return false
		}
	}
	if p.noRows {
		return true
	}
	if sp := p.setop; sp != nil {
		return sp.op == "UNION" && sp.all && len(sp.order) == 0 &&
			sp.left.unionStreamable() && sp.right.unionStreamable()
	}
	if !p.streamable() || len(p.order) != 0 || !managedRowIDTable(p.meta) {
		return false
	}
	// Even an uncorrelated subquery can retain an input-sized IN/hash cache
	// or execute during scan preparation. Keep this path strictly row-local.
	rowLocal := true
	visit := func(expr Expr) {
		walkExprPruned(expr, func(expr Expr) bool {
			if _, ok := expr.(*Subquery); ok {
				rowLocal = false
			}
			return rowLocal
		})
	}
	visit(p.sel.Where)
	for _, it := range p.items {
		visit(it.expr)
	}
	return rowLocal
}

func fixedUnionType(t ColType) bool {
	if t.Kind == KindRecord || t.Kind == KindDecimal && t.Precision == 0 {
		return false
	}
	if t.Kind == KindArray {
		return fixedUnionType(t.element())
	}
	return true
}

// unionRows retains plan/scan descriptions for the tree but opens only its
// current leaf. Leaves produce Values directly, so no Arrow round-trip loses
// logical types or precision before the pairwise set-operation coercions.
type unionRows struct {
	exec       *executor
	plan       *selectPlan
	params     []Value
	opts       streamOpts
	scan       selectScan
	children   [2]*unionRows
	child      int
	leaf       *streamReader
	skip, left int64
	closed     bool
}

// prepareUnionRows performs no cursor reads. A key/HASH fallback discovered in
// any leaf sends the entire tree to the established materialized executor.
func (e *executor) prepareUnionRows(ctx context.Context, p *selectPlan, params []Value, so *streamOpts) (*unionRows, bool, error) {
	n := &unionRows{exec: e, plan: p, params: params, opts: *so, left: -1}
	if p.noRows {
		n.closed = true
		return n, true, nil
	}
	if sp := p.setop; sp != nil {
		for i, child := range []*selectPlan{sp.left, sp.right} {
			c, ok, err := e.prepareUnionRows(ctx, child, params, so)
			if err != nil || !ok {
				return nil, ok, err
			}
			n.children[i] = c
		}
		if p.sel.Offset != nil {
			n.skip = *p.sel.Offset
		}
		if p.sel.Limit != nil {
			n.left = *p.sel.Limit
		}
		return n, true, nil
	}
	sc, err := e.prepareScan(ctx, p, params)
	if err != nil {
		return nil, false, err
	}
	if sc.req.where.none {
		n.closed = true
		return n, true, nil
	}
	if sc.req.where.keys != nil || !sc.indexSort || sc.req.hashScan {
		return nil, false, nil
	}
	n.scan = sc
	return n, true, nil
}

func (e *executor) streamUnionAll(ctx context.Context, p *selectPlan, params []Value, so *streamOpts) (execResult, bool, error) {
	source, ok, err := e.prepareUnionRows(ctx, p, params, so)
	if err != nil || !ok {
		return execResult{}, ok, err
	}
	cols := so.cols
	if cols == nil {
		cols = p.columns()
	}
	sctx, cancel := context.WithCancelCause(ctx)
	r := &streamReader{ctx: sctx, cancel: cancel, exec: e, cache: e.cache, union: source,
		cols: cols, schema: resultSchema(cols), mem: so.mem, batchRows: so.batchRows, batchBytes: so.batchBytes, left: -1}
	r.refs.Store(1)
	if err := r.fill(); err != nil {
		r.Release()
		return execResult{}, true, err
	}
	if r.exhausted && (r.batchBytes == 0 || r.pendingBytes <= r.batchBytes || len(r.pending) <= 1) {
		rows := r.pending
		r.pending = nil
		r.Release()
		return execResult{isQuery: true, cols: cols, rows: rows, affected: int64(len(rows))}, true, nil
	}
	r.attachOwners(so.owners)
	return execResult{isQuery: true, cols: cols, stream: r, affected: -1}, true, nil
}

func (n *unionRows) close() {
	if n.closed {
		return
	}
	n.closed = true
	if n.leaf != nil {
		n.leaf.Release()
		n.leaf = nil
	}
	for _, child := range n.children {
		if child != nil {
			child.close()
		}
	}
}

func (n *unionRows) next(ctx context.Context) ([]Value, error) {
	for !n.closed {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		if n.plan.setop == nil {
			return n.nextLeaf(ctx)
		}
		if n.left == 0 || n.child == len(n.children) {
			n.close()
			break
		}
		row, err := n.children[n.child].next(ctx)
		if errors.Is(err, io.EOF) {
			n.children[n.child].close()
			n.child++
			continue
		}
		if err != nil {
			return nil, err
		}
		for i := range row {
			row[i], err = coerceIn(row[i], n.plan.items[i].typ, n.exec.zone())
			if err != nil {
				return nil, invalidArg(err)
			}
		}
		if n.skip > 0 {
			n.skip--
			continue
		}
		if n.left > 0 {
			n.left--
			if n.left == 0 {
				n.close()
			}
		}
		return row, nil
	}
	return nil, io.EOF
}

func (n *unionRows) nextLeaf(ctx context.Context) ([]Value, error) {
	if n.leaf == nil {
		so := n.opts
		so.cols = n.plan.columns()
		so.batchBytes = 0 // Apply accounting only after all pairwise coercions.
		r, err := n.exec.newScanReader(ctx, n.plan, n.scan.req, n.params, &so)
		if err != nil {
			return nil, err
		}
		// Use the generic Value path, with no early fit to an Arrow column. The
		// materialized set executor coerces the original Values at each pair.
		r.arrow = nil
		r.zone = utcZone
		if !n.scan.pushLimit && n.scan.req.late == nil {
			if off := n.plan.sel.Offset; off != nil {
				r.skip = *off
			}
			if lim := n.plan.sel.Limit; lim != nil {
				r.left = *lim
			}
		}
		n.leaf = r
	}
	r := n.leaf
	if len(r.pending) == 0 && !r.exhausted {
		if err := r.fill(); err != nil {
			return nil, err
		}
	}
	if len(r.pending) == 0 {
		n.close()
		return nil, io.EOF
	}
	row := r.pending[0]
	r.pending[0] = nil
	r.pending = r.pending[1:]
	return row, nil
}

func (r *streamReader) fillUnion() error {
	for !r.exhausted && len(r.pending) < r.batchRows && (r.batchBytes == 0 || r.pendingBytes < r.batchBytes) {
		row, err := r.union.next(r.ctx)
		if errors.Is(err, io.EOF) {
			r.exhausted = true
			break
		}
		if err != nil {
			return err
		}
		r.pending = append(r.pending, row)
		if r.batchBytes > 0 {
			r.pendingBytes = addBatchBytes(r.pendingBytes, r.rowBatchBytes(row))
		}
	}
	return nil
}
