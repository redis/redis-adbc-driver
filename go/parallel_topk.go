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
	"math"
	"sort"
	"sync"

	goredis "github.com/redis/go-redis/v9"
)

// parallelSelectTopK scans disjoint managed row-ID ranges and retains only the
// best OFFSET+LIMIT rows in each worker. It is the exact bounded alternative
// when Redis cannot sort exactly (for example, because one value contains a
// NUL byte), and when an exact index sort cannot push LIMIT because a residual
// predicate must be checked first. The final range is open-ended so concurrent
// inserts follow the same inclusion rule as partitioned aggregates.
func (e *executor) parallelSelectTopK(ctx context.Context, plan *selectPlan, sc selectScan, params []Value) (out [][]Value, handled bool, resultErr error) {
	k := aggregateTopLimit(plan, math.MaxInt)
	meta := plan.meta
	// selectTopKShape rejects an index-ordered scan because the serial path can
	// normally stream it. A residual prevents the index LIMIT, though, and makes
	// Redis sort the complete match set before that stream can return its first
	// page. Partition by managed row ID in that case and sort only each bounded
	// worker heap. Keep a genuinely pushed LIMIT on the established index path.
	shapeScan := sc
	shapeScan.indexSort = false
	if sc.pushLimit || !selectTopKShape(plan, shapeScan, k) || meta.isMem || meta.Adopted || meta.KeyRows || meta.AppIndex ||
		sc.req.where.none || sc.req.where.keys != nil || !parallelPredicate(sc.req.where.residual) {
		return nil, false, nil
	}
	last, err := e.store.client.Get(ctx, seqKey(meta.Schema, meta.Name)).Int64()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, wrapRedis(err, "reading row sequence failed")
	}
	if last < aggregatePartitionRows*2 || last >= exactDoubleLimit {
		return nil, false, nil
	}
	workers := min(effectiveQueryWorkers(e.queryWorkers), int((last+aggregatePartitionRows-1)/aggregatePartitionRows))
	if e.workerBudget != nil {
		grant, err := e.workerBudget.acquireBounded(ctx, e.queryWorkers, workers)
		if err != nil {
			return nil, true, cancelled(ctx)
		}
		workers = grant
		defer e.workerBudget.release(grant)
	}
	if workers < 2 {
		return nil, false, nil
	}
	if e.workerGrant != nil {
		e.workerGrant.Store(int64(workers))
	}
	e.diagnostics.setPhase("parallel_select_topk")
	e.diagnostics.setPlan(map[string]any{"kind": "parallel_select_topk", "workers": workers,
		"partition_rows": aggregatePartitionRows, "residual": sc.req.where.residual, "sql_limit": plan.sel.Limit,
		"sql_offset": plan.sel.Offset})

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan int64)
	tops := make([]aggregateTopK, workers)
	var wg sync.WaitGroup
	for w := range workers {
		tops[w].order = plan.order
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := *e
			local.cache = newExecCache()
			env := local.newEnv(ctx, meta.types(), params)
			ordinal := 0
			for lo := range jobs {
				hi := min(last, lo+aggregatePartitionRows-1)
				where := partitionWhere(sc.req.where, lo, hi, hi == last)
				req := scanRequest{meta: meta, where: where, need: sc.req.need}
				err := local.scanPages(ctx, req, params, func(rows []map[string]Value) error {
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
						row.vals, err = evalItems(env, plan.items)
						if err != nil {
							return err
						}
						retainTopK(&tops[w], row, k)
					}
					return nil
				})
				if err != nil {
					cancel(err)
					return
				}
			}
		}(w)
	}
dispatch:
	for lo := int64(1); lo <= last; lo += aggregatePartitionRows {
		select {
		case jobs <- lo:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if cause := context.Cause(ctx); cause != nil {
		return nil, true, cause
	}

	merged := aggregateTopK{order: plan.order}
	ordinal := 0
	for w := range tops {
		for _, row := range tops[w].rows {
			row.ordinal = ordinal
			ordinal++
			retainTopK(&merged, row, k)
		}
	}
	sort.Slice(merged.rows, func(i, j int) bool { return merged.better(merged.rows[i], merged.rows[j]) })
	kept := applyLimit(merged.rows, plan.sel.Offset, plan.sel.Limit)
	out = make([][]Value, len(kept))
	for i, row := range kept {
		out[i] = row.vals
	}
	return out, true, nil
}
