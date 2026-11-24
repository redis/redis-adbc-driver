// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"math"
	"strings"
	"sync"
	"sync/atomic"
)

// Probe immutable prepared buckets with worker-owned maps. No evaluator/cache,
// accumulator, AST or source row is shared for mutation.
func probeStarRow(ctx context.Context, shape *starShape, row map[string]Value, emit func(map[string]Value) error) error {
	return newStarRowProbe(shape).row(ctx, row, emit)
}

var errStarProbeStopped = errors.New("star probing stopped after consumer failure")

type starParallelResult struct {
	rows chan []map[string]Value
	err  error
}

// One single-page queue per worker bounds joined output even when a later range
// finishes first. The coordinator visits ranges in order, so one reducer and
// group discovery order remain unchanged. A consumer error stops callbacks and
// probing, but source scans finish so later source errors keep their precedence.
func parallelStarJoinPages(ctx context.Context, shape *starShape, workers int, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (error, error) {
	return parallelStarJoinPagesMode(ctx, shape, workers, false, scan, visit)
}
func parallelOwnedStarJoinPages(ctx context.Context, shape *starShape, workers int, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (error, error) {
	return parallelStarJoinPagesMode(ctx, shape, workers, true, scan, visit)
}
func parallelStarJoinPagesMode(ctx context.Context, shape *starShape, workers int, ownedFacts bool, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (sourceErr, consumerErr error) {
	child, cancel := context.WithCancel(ctx)
	results := make([]starParallelResult, workers)
	var stopProbing atomic.Bool
	var wg sync.WaitGroup
	// Panic unwinding must join workers before the outer grant can release.
	defer func() { cancel(); wg.Wait() }()
	for w := range results {
		results[w].rows = make(chan []map[string]Value, 1)
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			defer close(results[w].rows)
			probe := newStarRowProbe(shape)
			probeRow := probe.row
			if ownedFacts {
				probeRow = probe.ownedRow
			}
			out := make([]map[string]Value, 0, starOutputPage)
			flush := func() error {
				if len(out) == 0 {
					return nil
				}
				if stopProbing.Load() {
					out = nil
					return nil
				}
				select {
				case results[w].rows <- out:
					out = make([]map[string]Value, 0, starOutputPage)
					return nil
				case <-child.Done():
					return cancelled(child)
				}
			}
			emit := func(row map[string]Value) error {
				if stopProbing.Load() {
					return errStarProbeStopped
				}
				out = append(out, row)
				if len(out) == starOutputPage {
					return flush()
				}
				return nil
			}
			err := scan(child, w, func(page []map[string]Value) error {
				for _, row := range page {
					if err := cancelled(child); err != nil {
						return err
					}
					if !stopProbing.Load() {
						if err := probeRow(child, row, emit); err != nil && err != errStarProbeStopped {
							return err
						}
					}
				}
				return nil
			})
			if err == nil {
				err = flush()
			}
			results[w].err = err
		}(w)
	}
	// Do not cancel peers for an ordinary source error: every admitted immutable
	// range is bounded and source error precedence is deterministic by range.
	for w := range results {
		for page := range results[w].rows {
			if ctx.Err() != nil {
				stopProbing.Store(true)
				cancel()
			}
			if ctx.Err() == nil && consumerErr == nil && sourceErr == nil {
				if err := visit(page); err != nil {
					consumerErr = err
					stopProbing.Store(true)
				}
			}
			if ctx.Err() != nil {
				stopProbing.Store(true)
				cancel()
			}
		}
		if results[w].err != nil && sourceErr == nil {
			sourceErr = results[w].err
			stopProbing.Store(true)
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		return cancelled(ctx), consumerErr
	}
	return sourceErr, consumerErr
}

// This first parallel path is deliberately limited to broad direct published
// native facts. Selective queries enter only after a coordinator proof that
// complete candidate count/bytes exceed the existing budgets. Workers never
// repeat candidate discovery; admitted selective and UNION paths stay serial.
func (e *executor) scanParallelStarFacts(ctx context.Context, shape *starShape, jp *joinPlan, params []Value, visit func([]map[string]Value) error) (handled bool, resultErr error) {
	it := jp.items[shape.fact]
	base := it.base
	scope := snapshotOf(ctx)
	if e.workerBudget == nil || e.store == nil || !e.store.snapshots || scope == nil || scope.pin == nil || base.isMem || base.view != nil || !managedRowIDTable(base) || snapshotDataID(base.prefix()) == "" || len(base.PendingCleanup) > 0 {
		return false, nil
	}
	for _, seg := range base.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			return false, nil
		}
	}
	view := it.view()
	need := map[string]bool{}
	for c := range shape.need {
		if strings.HasPrefix(c, it.prefix) {
			need[c] = true
		}
	}
	predicate := andAll(shape.filters[shape.fact])
	wp, err := e.planWhere(ctx, predicate, view, params)
	if err != nil {
		return true, err
	}
	if wp.none || wp.keys != nil {
		return false, nil
	}
	if wp.query != "*" {
		if predicate == nil {
			return false, nil
		}
		over, err := e.store.completeFlexOverBudget(ctx, view, wp.query)
		if err != nil {
			return true, err
		}
		if !over {
			return false, nil
		}
	}
	last, err := e.store.rowSequence(ctx, base)
	if err == goredis.Nil {
		return false, nil
	}
	if err != nil {
		return true, wrapRedis(err, "reading streamed fact row sequence failed")
	}
	if last < 2048 {
		return false, nil
	}
	grant, err := e.workerBudget.acquireBounded(ctx, e.queryWorkers, int(min(last/1024, 32)))
	if err != nil {
		return true, cancelled(ctx)
	}
	defer e.workerBudget.release(grant)
	if e.workerGrant != nil {
		e.workerGrant.Store(int64(grant))
	}
	e.diagnostics.setPhase("parallel_streaming_star_join")
	e.diagnostics.setPlan(map[string]any{"kind": "parallel_streaming_star_join", "workers": grant, "dimension_count": len(shape.dims), "output_page_rows": starOutputPage, "exact_directory_ranges": true})
	// Use existing exact directories even for a single plain managed generation;
	// no numeric row-ID Search interval or floating point bound is introduced.
	copy := *view
	copy.SnapshotOverlay = true
	view = &copy
	wp.query, wp.residual, wp.original = "*", predicate, predicate
	// This CPU-only direct request has no projection/ties/late/keys and keep()
	// allocates each typed map via decodeRow. No downstream cursor/progress path
	// reads the map again; native row IDs and residuals are consumed beforehand.
	run := parallelStarJoinPages
	factCapacity := 0
	if e.gpu == nil {
		factCapacity = ownedFactRowCapacity(len(shape.need))
		run = parallelOwnedStarJoinPages
	}
	sourceErr, consumerErr := run(ctx, shape, grant, func(workerCtx context.Context, w int, consume func([]map[string]Value) error) error {
		bounds := starFactRange(last, grant, w)
		local := *e
		local.cache = newExecCache()
		req := scanRequest{meta: view, where: wp, need: need, ranges: &bounds, rowCapacity: factCapacity}
		return local.scanPages(workerCtx, req, params, consume)
	}, visit)
	// All worker iterators are closed/joined before final lease validation and
	// before the deferred grant release. A source/lease error precedes reduction.
	if sourceErr != nil {
		return true, sourceErr
	}
	if err := scope.pin.valid(ctx); err != nil {
		return true, err
	}
	return true, consumerErr
}

func starFactRange(last int64, workers, w int) rowIDRangeScan {
	step := last / int64(workers)
	r := rowIDRangeScan{lo: int64(w)*step + 1, hi: int64(w+1) * step}
	if w == workers-1 {
		r.hi = math.MaxInt64
	}
	return r
}

func ownedFactRowCapacity(needed int) int {
	if needed > 0 && needed < maxOwnedFactRowCapacity {
		return needed + 1
	}
	return 0
}
