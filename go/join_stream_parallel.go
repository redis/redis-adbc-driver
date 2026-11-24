// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"math"
	"strings"
)

// Probe immutable prepared buckets with worker-owned maps. No evaluator/cache,
// accumulator, AST or source row is shared for mutation.
func probeStarRow(ctx context.Context, shape *starShape, row map[string]Value, emit func(map[string]Value) error) error {
	return newStarRowProbe(shape).row(ctx, row, emit)
}

var errStarProbeStopped = errors.New("star probing stopped after consumer failure")

// Legacy shared-safe entry points retain coarse chunk compatibility.
func parallelStarJoinPages(ctx context.Context, shape *starShape, workers int, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (error, error) {
	return parallelStarJoinPagesMode(ctx, shape, workers, false, scan, visit)
}
func parallelOwnedStarJoinPages(ctx context.Context, shape *starShape, workers int, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (error, error) {
	return parallelStarJoinPagesMode(ctx, shape, workers, true, scan, visit)
}
func parallelStarJoinPagesMode(ctx context.Context, shape *starShape, workers int, ownedFacts bool, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (error, error) {
	return parallelStarJoinChunksMode(ctx, shape, workers, workers, ownedFacts, scan, visit)
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
	// Use existing exact directories even for a single plain managed generation;
	// no numeric row-ID Search interval or floating point bound is introduced.
	copy := *view
	copy.SnapshotOverlay = true
	view = &copy
	wp.query, wp.residual, wp.original = "*", predicate, predicate
	// This CPU-only direct request has no projection/ties/late/keys and keep()
	// allocates each typed map via decodeRow. No downstream cursor/progress path
	// reads the map again; native row IDs and residuals are consumed beforehand.
	ownedFacts := e.gpu == nil
	factCapacity := 0
	chunks := grant
	if ownedFacts {
		factCapacity = ownedFactRowCapacity(len(shape.need))
		chunks = starFactChunkCount(last, grant)
	}
	// Each round-robin worker alone accesses its executor/cache. Keep source
	// bindings across chunks without sharing evaluator state between workers.
	locals := make([]executor, grant)
	for w := range locals {
		locals[w] = *e
		locals[w].cache = newExecCache()
	}
	e.diagnostics.setPlan(map[string]any{"kind": "parallel_streaming_star_join", "workers": grant, "dimension_count": len(shape.dims), "output_page_rows": starOutputPage, "exact_directory_ranges": true, "chunk_count": chunks, "chunk_row_target": starChunkRows, "source_cache_per_worker": true})
	sourceErr, consumerErr := parallelStarJoinChunksMode(ctx, shape, grant, chunks, ownedFacts, func(workerCtx context.Context, chunk int, consume func([]map[string]Value) error) error {
		bounds := starFactRange(last, chunks, chunk)
		if ownedFacts {
			bounds = starFactChunkRange(last, chunks, chunk)
		}
		if ownedFacts {
			bounds.cap = starChunkRows
		}
		local := &locals[chunk%grant]
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
