// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"errors"
	"io"
	"maps"
)

// scanPages visits indexed rows a cursor page at a time and closes the cursor
// on success, cancellation or an accumulator error. In-memory relations keep
// their existing materialization; direct key lookups are visited in batches.
func (e *executor) scanPages(ctx context.Context, req scanRequest, params []Value, visit func([]map[string]Value) error) error {
	if req.where.none {
		return nil
	}
	if req.meta.isMem {
		_, rows, err := e.scanMem(ctx, req, params)
		if err != nil {
			return err
		}
		return visit(rows)
	}
	if req.where.keys != nil {
		keys := req.where.keys
		for start := 0; start < len(keys); start += pipelineChunk {
			if err := cancelled(ctx); err != nil {
				return err
			}
			req.where.keys = keys[start:min(start+pipelineChunk, len(keys))]
			_, rows, err := e.scan(ctx, req, params)
			if err != nil {
				return err
			}
			if err := visit(rows); err != nil {
				return err
			}
		}
		return nil
	}
	it, err := e.openScan(ctx, req, params)
	if err != nil {
		return err
	}
	defer it.close(ctx)
	for {
		_, rows, err := it.next(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := visit(rows); err != nil {
			return err
		}
	}
}

// aggregateRepresentativeNeed excludes per-row aggregate inputs and filters.
// Retain correlated-subquery dependencies and ordered-set direct arguments,
// which are evaluated at the group's representative row when reducing it.
func aggregateRepresentativeNeed(plan *selectPlan, aggs []*Func) map[string]bool {
	need := maps.Clone(plan.extraNeed)
	if need == nil {
		need = map[string]bool{}
	}
	refs := func(x Expr) {
		walkExprPruned(x, func(n Expr) bool {
			if f, ok := n.(*Func); ok && aggregateFuncs[f.Name] {
				return false
			}
			if c, ok := n.(*ColumnRef); ok && c.Outer == 0 {
				need[c.Name] = true
			}
			return true
		})
	}
	for _, it := range plan.items {
		refs(it.expr)
	}
	for _, o := range plan.order {
		refs(o.expr)
	}
	refs(plan.having)
	refs(plan.qualify)
	for _, w := range plan.windows {
		for _, x := range w.fn.children() {
			refs(x)
		}
	}
	for _, f := range aggs {
		if orderedSetFuncs[f.Name] {
			for _, x := range f.Args {
				columnRefs(x, need)
			}
		}
	}
	return need
}

// aggregateOutput retains only the projected values and ordering keys.
type aggregateOutput struct {
	vals, keys []Value
	ordinal    int // preserve the previous stable order for equal keys
}

// aggregateTopK is a worst-first heap, bounded by OFFSET + LIMIT.
type aggregateTopK struct {
	rows  []aggregateOutput
	order []planOrder
}

func (h *aggregateTopK) better(a, b aggregateOutput) bool {
	if lessKeys(a.keys, b.keys, h.order) {
		return true
	}
	if lessKeys(b.keys, a.keys, h.order) {
		return false
	}
	return a.ordinal < b.ordinal
}
func (h aggregateTopK) Len() int           { return len(h.rows) }
func (h aggregateTopK) Less(i, j int) bool { return h.better(h.rows[j], h.rows[i]) }
func (h aggregateTopK) Swap(i, j int)      { h.rows[i], h.rows[j] = h.rows[j], h.rows[i] }
func (h *aggregateTopK) Push(x any)        { h.rows = append(h.rows, x.(aggregateOutput)) }
func (h *aggregateTopK) Pop() any {
	n := len(h.rows) - 1
	row := h.rows[n]
	h.rows[n] = aggregateOutput{}
	h.rows = h.rows[:n]
	return row
}

func aggregateTopLimit(plan *selectPlan, n int) int {
	if plan.windowed() || plan.distinct || len(plan.order) == 0 || plan.sel.Limit == nil || *plan.sel.Limit <= 0 {
		return 0
	}
	off := int64(0)
	if plan.sel.Offset != nil {
		off = *plan.sel.Offset
	}
	// Bounds avoid overflow and skip a heap that would retain every row.
	if off < 0 || off >= int64(n) || *plan.sel.Limit >= int64(n)-off {
		return 0
	}
	return int(off + *plan.sel.Limit)
}
