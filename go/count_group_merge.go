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
	"errors"
	"math"
	"slices"
)

// A COUNT(*) tuple key is the complete SQL group key. The external merge has
// already combined every run's multiplicities, so losing top-K groups need no
// source decoding, timestamp evaluation, accumulator, or representative map.
func (e *executor) reduceCountGroups(ctx context.Context, plan *selectPlan, p *tupleAggregatePlan, aggs []*Func, params []Value, spill *tupleSpill) ([]aggGroup, bool, error) {
	if !p.countGroups {
		return nil, false, nil
	}
	k := aggregateTopLimit(plan, math.MaxInt)
	if len(plan.order) == 0 && plan.sel.Limit != nil && *plan.sel.Limit > 0 {
		off := int64(0)
		if plan.sel.Offset != nil {
			off = *plan.sel.Offset
		}
		if off >= 0 && off < math.MaxInt && *plan.sel.Limit < math.MaxInt-off {
			k = int(off + *plan.sel.Limit)
		}
	}
	if k == 0 {
		return nil, false, nil
	}
	for _, o := range plan.order {
		switch x := o.expr.(type) {
		case *Literal:
		case *Func:
			found := false
			for _, f := range aggs {
				found = found || x == f
			}
			if !found {
				return nil, false, nil
			}
		default:
			return nil, false, nil
		}
	}
	env := e.newEnv(ctx, plan.meta.types(), params)
	env.aggs = make(map[*Func]Value, len(aggs))
	top := aggregateTopK{order: plan.order}
	kept := map[int]aggGroup{}
	ordinal := 0
	keys := make([]Value, 0, len(plan.order))
	stop := errors.New("count output complete")
	err := spill.merge(ctx, func(row spillTuple) error {
		value := intValue(typeInt64, row.count)
		for _, f := range aggs {
			env.aggs[f] = value
		}
		if plan.having != nil {
			v, err := env.eval(plan.having)
			if err != nil {
				return invalidArg(err)
			}
			if b, ok := truthy(v); !ok || !b {
				return nil
			}
		}
		keys = keys[:0]
		for _, o := range plan.order {
			v, err := env.eval(o.expr)
			if err != nil {
				return invalidArg(err)
			}
			keys = append(keys, v)
		}
		candidate := aggregateOutput{ordinal: ordinal, keys: keys}
		ordinal++
		if len(top.rows) == k && !top.better(candidate, top.rows[0]) {
			return nil
		}
		vals, err := p.values(row)
		if err != nil {
			return err
		}
		group := aggGroup{rep: p.row(vals), results: make(map[*Func]Value, len(aggs))}
		for _, f := range aggs {
			group.results[f] = value
		}
		candidate.keys = slices.Clone(keys)
		if len(top.rows) < k {
			heap.Push(&top, candidate)
		} else {
			delete(kept, top.rows[0].ordinal)
			top.rows[0] = candidate
			heap.Fix(&top, 0)
		}
		kept[candidate.ordinal] = group
		if len(plan.order) == 0 && len(top.rows) == k {
			return stop
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, true, err
	}
	out := make([]aggGroup, 0, len(top.rows))
	for _, row := range top.rows {
		out = append(out, kept[row.ordinal])
	}
	return out, true, nil
}
