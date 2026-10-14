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
	"container/heap"
	"context"
	"math"
	"sort"
)

// Ordered narrow projections whose index ordering is lossy retain only the
// final OFFSET+LIMIT rows. Every residual is checked before heap admission.
func (e *executor) selectTopK(ctx context.Context, plan *selectPlan, sc selectScan, params []Value) ([][]Value, bool, error) {
	k := aggregateTopLimit(plan, math.MaxInt)
	if k == 0 || sc.indexSort || plan.hasCorrelated() || len(plan.extraNeed) != 0 || plan.qualify != nil {
		return nil, false, nil
	}
	// Evaluation of volatile projection expressions stays on the established
	// path. Simple columns also keep retained rows narrow.
	for _, it := range plan.items {
		switch it.expr.(type) {
		case *ColumnRef, *Literal:
		default:
			return nil, false, nil
		}
	}
	for _, o := range plan.order {
		switch o.expr.(type) {
		case *ColumnRef:
		default:
			return nil, false, nil
		}
	}
	env := e.newEnv(ctx, plan.meta.types(), params)
	e.diagnostics.setPhase("select_topk")
	top := aggregateTopK{order: plan.order}
	ordinal := 0
	err := e.scanPages(ctx, sc.req, params, func(rows []map[string]Value) error {
		for _, vals := range rows {
			if err := cancelled(ctx); err != nil {
				return err
			}
			env.row = vals
			row := aggregateOutput{ordinal: ordinal}
			ordinal++
			for _, o := range plan.order {
				v, err := env.eval(o.expr)
				if err != nil {
					return invalidArg(err)
				}
				row.keys = append(row.keys, v)
			}
			if len(top.rows) == k && !top.better(row, top.rows[0]) {
				continue
			}
			var err error
			row.vals, err = evalItems(env, plan.items)
			if err != nil {
				return err
			}
			if len(top.rows) < k {
				heap.Push(&top, row)
			} else {
				top.rows[0] = row
				heap.Fix(&top, 0)
			}
		}
		return nil
	})
	if err != nil {
		return nil, true, err
	}
	sort.Slice(top.rows, func(i, j int) bool { return top.better(top.rows[i], top.rows[j]) })
	kept := applyLimit(top.rows, plan.sel.Offset, plan.sel.Limit)
	out := make([][]Value, len(kept))
	for i, r := range kept {
		out[i] = r.vals
	}
	return out, true, nil
}
