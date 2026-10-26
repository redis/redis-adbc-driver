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
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"

	goredis "github.com/redis/go-redis/v9"
)

// Automatic exploration ceilings follow runtime capacity, including container
// limits, with bounded headroom for workers waiting on Redis/network I/O.
func effectiveQueryWorkers(configured int) int {
	if configured > 0 {
		return configured
	}
	return min(32, max(1, runtime.GOMAXPROCS(0))*2)
}

// parallelPredicate excludes expressions that execute subqueries, consume
// randomness, or mutate execution state. Each partition owns its executor.
func parallelPredicate(x Expr) bool {
	switch v := x.(type) {
	case nil, *Literal:
		return true
	case *ColumnRef:
		return v.Outer == 0
	case *Func:
		if v.Name != "LIKE" && v.Name != "ILIKE" {
			return false
		}
		for _, x := range v.Args {
			if !parallelPredicate(x) {
				return false
			}
		}
		return true
	case *Unary:
		return v.Op == "NOT" && parallelPredicate(v.X)
	case *Binary:
		switch v.Op {
		case "AND", "OR", "=", "!=", "<>", "<", "<=", ">", ">=":
			return parallelPredicate(v.L) && parallelPredicate(v.R)
		}
	}
	return false
}

func parallelAggregates(aggs []*Func, types map[string]ColType) bool {
	if len(aggs) == 0 {
		return false
	}
	inputs := compileAggregateInputs(aggs, types)
	for i, f := range aggs {
		if f.Distinct || f.Filter != nil || len(f.OrderBy) > 0 || f.WithinGroup {
			return false
		}
		switch f.Name {
		case "COUNT", "SUM", "AVG", "MIN", "MAX":
		default:
			return false
		}
		if f.Star {
			if f.Name != "COUNT" {
				return false
			}
			continue
		}
		if len(f.Args) != 1 {
			return false
		}
		col, ok := f.Args[0].(*ColumnRef)
		if !ok {
			// Only the checked, pure integer DAG is safe to evaluate in
			// independent partitions. Never algebraically rewrite SUM(x+c).
			if inputs.roots[i] < 0 {
				return false
			}
			continue
		}
		if col.Outer != 0 {
			return false
		}
		// Interval MIN/MAX retain the last equivalent representation; partition
		// order must not change that. Interval sums also need a separate merger.
		if types[col.Name].Kind == KindInterval {
			return false
		}
	}
	return true
}

// mergeExactSum aligns integer units before adding: no intermediate floating
// point rounding, even for cancellation, decimal scales, NaNs and infinities.
func mergeExactSum(dst, src *exactSum) {
	if src.n == 0 {
		return
	}
	if src.scale > dst.scale {
		dst.raiseScale(src.scale)
	}
	if src.exp < dst.exp {
		dst.lowerExp(src.exp)
	}
	var v big.Int
	v.Set(&src.sum)
	if dst.scale > src.scale {
		v.Mul(&v, pow5(dst.scale-src.scale))
	}
	if src.exp > dst.exp {
		v.Lsh(&v, uint(src.exp-dst.exp))
	}
	dst.sum.Add(&dst.sum, &v)
	dst.n += src.n
	dst.nan += src.nan
	dst.inf += src.inf
	dst.ninf += src.ninf
}

func mergeAccumulator(dst, src *accumulator) {
	count := dst.count + src.count
	switch dst.fn.Name {
	case "SUM", "AVG":
		mergeExactSum(dst.sum, src.sum)
	case "MIN", "MAX":
		if src.has {
			dst.add(src.best)
		}
	}
	dst.count = count
}

// parallelAggregate runs independent, disjoint row-ID ranges through the
// existing scan path. Each owns its cursor, row maps, environment and reducers;
// the only shared objects are immutable plans and the concurrency-safe client.
// Adopted/memory tables, grouped queries and unsupported expressions stay serial.
func (e *executor) parallelAggregate(ctx context.Context, plan *selectPlan, wp wherePlan, aggs []*Func, params []Value) (groups []aggGroup, handled bool, resultErr error) {
	meta := plan.meta
	if len(meta.PendingCleanup) > 0 {
		return nil, false, nil
	}
	workers := effectiveQueryWorkers(e.queryWorkers)
	if e.workerBudget != nil {
		workers = min(workers, e.workerBudget.capacity())
	}
	if meta.isMem || meta.Adopted || meta.KeyRows || meta.AppIndex || len(plan.sel.GroupBy) > 0 || len(plan.extraNeed) > 0 || len(plan.outerAggs) > 0 || wp.none || wp.keys != nil || !parallelPredicate(wp.residual) || !parallelAggregates(aggs, meta.types()) {
		return nil, false, nil
	}
	// The sequence is an upper bound, not a count: deleted rows and gaps do not
	// matter. The last range extends to +inf so concurrent inserts aren't lost.
	last, err := e.store.redis(ctx).Get(ctx, seqKey(meta.Schema, meta.Name)).Int64()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, wrapRedis(err, "reading row sequence failed")
	}
	if last < 2048 || last >= exactDoubleLimit {
		return nil, false, nil
	}
	// Measure actual fetched rows, never sequence IDs (which can have gaps).
	if e.workerBudget != nil && (e.queryWorkers == 0 || e.workerBudget.automatic()) {
		key := meta.Schema + "." + meta.Name + ":" + wp.query + fmt.Sprint(last/4096)
		for _, f := range aggs {
			key += ":" + f.Name
			for _, a := range f.Args {
				encoded, _ := json.Marshal(a)
				key += ":" + string(encoded)
			}
		}
		stats := &scanTelemetry{}
		local := *e
		local.telemetry = stats
		e = &local
		start := e.workerBudget.begin(key)
		defer func() { e.workerBudget.finish(start, stats, resultErr != nil) }()
	}
	// Even a one-worker eligible scan participates in the shared budget.
	if e.workerBudget != nil {
		grant, err := e.workerBudget.acquireBounded(ctx, e.queryWorkers, int(min(last/1024, 32)))
		if err != nil {
			return nil, true, cancelled(ctx)
		}
		workers = grant
		if e.workerGrant != nil {
			e.workerGrant.Store(int64(grant))
		}
		defer e.workerBudget.release(grant)
	}
	if workers == 1 {
		groups, err := e.serialDriverAggregate(ctx, plan, wp, aggs, params)
		return groups, true, err
	}
	if e.workerGrant != nil {
		e.workerGrant.Store(int64(workers))
	}
	e.diagnostics.setPhase("parallel_aggregate")
	e.diagnostics.setPlan(map[string]any{"kind": "parallel_exact", "workers": workers, "checked_integer_inputs": true, "residual": wp.residual})
	types := meta.types()
	aggTypes := make([]ColType, len(aggs))
	need := map[string]bool{}
	for i, f := range aggs {
		columnRefs(f, need)
		aggTypes[i], err = inferType(f, types, nil)
		if err != nil {
			return nil, true, invalidArg(err)
		}
	}
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	partial := make([][]*accumulator, workers)
	errs := make([]error, workers)
	inputErrors := make([]bool, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := *e
			local.cache = newExecCache()
			inputs := compileAggregateInputs(aggs, types)
			reducers := make([]*accumulator, len(aggs))
			for i, f := range aggs {
				reducers[i] = newAccumulator(f)
			}
			lo, hi := "-inf", "+inf"
			step := last / int64(workers)
			if w > 0 {
				lo = fmt.Sprint(int64(w)*step + 1)
			}
			if w < workers-1 {
				hi = fmt.Sprint(int64(w+1) * step)
			}
			part := wp
			part.query = fmt.Sprintf("@%s:[%s %s]", rowIDField, lo, hi)
			if wp.query != "*" {
				part.query = fmt.Sprintf("(%s) %s", wp.query, part.query)
			}

			handled, scanErr := local.scanScalarAggregateValues(ctx, scanRequest{meta: meta, where: part, need: need}, params, aggs, func(values []Value) error {
				if err := cancelled(ctx); err != nil {
					return err
				}
				for i, acc := range reducers {
					acc.add(values[i])
				}
				return nil
			})
			if handled {
				errs[w] = scanErr
			} else {
				errs[w] = local.scanPages(ctx, scanRequest{meta: meta, where: part, need: need}, params, func(rows []map[string]Value) error {
					for _, row := range rows {
						if err := cancelled(ctx); err != nil {
							return err
						}
						inputs.nextRow()
						for i, f := range aggs {
							var v Value
							if root := inputs.roots[i]; root >= 0 {
								var err error
								v, err = inputs.value(root, row)
								if err != nil {
									inputErrors[w] = true
									cancel()
									return invalidArg(err)
								}
							} else if !f.Star {
								v = row[f.Args[0].(*ColumnRef).Name]
							}
							reducers[i].add(v)
						}
					}
					return nil
				})
			}
			partial[w] = reducers
		}(worker)
	}
	wg.Wait()
	for _, failed := range inputErrors {
		if failed {
			// Parallel completion order must not choose the reported SQL
			// error. Replay the pure inputs in the established serial scan
			// order, including residuals and aggregate argument order.
			groups, err := e.serialDriverAggregate(parentCtx, plan, wp, aggs, params)
			return groups, true, err
		}
	}
	for _, err := range errs {
		if err != nil {
			return nil, true, err
		}
	}
	env := e.newEnv(ctx, types, params)
	results := map[*Func]Value{}
	for i, f := range aggs {
		reducer := newAccumulator(f)
		for w := 0; w < workers; w++ {
			mergeAccumulator(reducer, partial[w][i])
		}
		value, err := reducer.result(env, aggTypes[i])
		if err != nil {
			return nil, true, invalidArg(err)
		}
		results[f] = value
	}
	return []aggGroup{{rep: map[string]Value{}, results: results}}, true, nil
}
