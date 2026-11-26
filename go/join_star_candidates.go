// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// With Flex, a selective fact or large dimension filter used to read its
// complete Search candidates in one serial scan, and a filter over the
// candidate budget read the whole table. Cold flash reads are slow per
// connection, so either way a parallel reader's filter cost far more than its
// candidates. sharedFlexCandidates discovers the complete candidate set once,
// under the larger shared budget, and the reader's workers each scan a
// row-ID slice of it (snapshotCandidateScan.split). Every slice still
// resolves newest-version visibility, fetches its rows and checks the
// complete predicate on each one, exactly like the serial candidate scan.

// flexReaderPlan evaluates a parallel reader's Flex filter once
// (fetchCompleteFlexCandidates). over proves the ordinary scan budget refuses
// its complete candidates (completeFlexOverBudget), so the reader uses
// exact-directory ranges. Otherwise, a set within flexSharedCandidates that
// fills at least two chunks is materialized once (cands, bounds). Neither: the
// reader keeps its existing plan. If Search refuses the fetch size, the
// filter is counted first, then fetched.
func (e *executor) flexReaderPlan(ctx context.Context, view *tableMeta, query string) (cands *snapshotCandidateScan, bounds [][2]string, over bool, err error) {
	if e.store == nil || !e.store.flexEnabled() || query == "" || query == "*" {
		return nil, nil, false, nil
	}
	cands, counts, err := fetchCompleteFlexCandidates(ctx, e.store, view, query, flexSharedCandidates)
	if err != nil {
		return nil, nil, false, err
	}
	if counts == nil {
		if counts, err = e.store.completeFlexCandidateCounts(ctx, view, query); err != nil || counts == nil {
			return nil, nil, false, err
		}
		if !counts.refused(flexSharedCandidates) && counts.total() >= 2*starChunkRows {
			cands, bounds, err = e.sharedFlexCandidates(ctx, view, query, counts)
			return cands, bounds, counts.refused(flexScanCandidates), err
		}
		cands = nil
	}
	over = counts.refused(flexScanCandidates)
	if cands == nil || counts.total() < 2*starChunkRows {
		return nil, nil, over, nil
	}
	if bounds = cands.chunkBounds(int(starChunkRows)); len(bounds) < 2 {
		return nil, nil, over, nil
	}
	return cands, bounds, over, nil
}

// sharedFlexCandidates returns a Flex filter's complete candidates and their
// row-ID chunks of at most starChunkRows IDs. It returns nil, and the caller
// keeps its existing plan, without a plain index, when the set exceeds
// flexSharedCandidates or Search's own result limit, or when it fills fewer
// than two chunks. counts are flexReaderPlan's, so the fetch is the only
// further evaluation of query (openFlexSnapshotCandidatesCounted).
func (e *executor) sharedFlexCandidates(ctx context.Context, view *tableMeta, query string, counts *flexCandidateCounts) (*snapshotCandidateScan, [][2]string, error) {
	cands, err := openFlexSnapshotCandidatesCounted(ctx, e.store, view, query, nil, nil, flexSharedCandidates, counts)
	if err != nil || cands == nil {
		return nil, nil, err
	}
	bounds := cands.chunkBounds(int(starChunkRows))
	if len(bounds) < 2 {
		return nil, nil, nil
	}
	return cands, bounds, nil
}

// scanParallelStarCandidates is scanParallelStarFacts over a fact's shared
// complete Flex candidates: one ordered chunk per candidate row-ID slice.
func (e *executor) scanParallelStarCandidates(ctx context.Context, shape *starShape, view *tableMeta, wp wherePlan, predicate Expr, need map[string]bool, params []Value, cands *snapshotCandidateScan, bounds [][2]string, visit func([]map[string]Value) error) (bool, error) {
	scope := snapshotOf(ctx)
	grant, err := e.workerBudget.acquireBounded(ctx, e.queryWorkers, min(len(bounds), 32))
	if err != nil {
		return true, cancelled(ctx)
	}
	defer e.workerBudget.release(grant)
	if e.workerGrant != nil {
		e.workerGrant.Store(int64(grant))
	}
	e.diagnostics.setPhase("parallel_streaming_star_join")
	// The Search query only selected the candidates; every row is checked
	// against the complete predicate.
	wp.residual, wp.original = predicate, predicate
	ownedFacts := e.gpu == nil
	factCapacity := 0
	if ownedFacts {
		factCapacity = ownedFactRowCapacity(len(shape.need))
	}
	locals := make([]executor, grant)
	for w := range locals {
		locals[w] = *e
		locals[w].cache = newExecCache()
	}
	marker := map[string]any{"kind": "parallel_streaming_star_join", "workers": grant, "dimension_count": len(shape.dims), "output_page_rows": starOutputPage, "complete_flex_candidates": true, "chunk_count": len(bounds), "chunk_row_target": starChunkRows, "source_cache_per_worker": true}
	if shape.snowflake {
		marker["snowflake"], marker["parallel_dimensions"] = true, shape.parallelDimensions
	}
	e.diagnostics.setPlan(marker)
	sourceErr, consumerErr := parallelStarJoinChunksMode(ctx, shape, grant, len(bounds), ownedFacts, func(workerCtx context.Context, chunk int, consume func([]map[string]Value) error) error {
		local := &locals[chunk%grant]
		req := scanRequest{meta: view, where: wp, need: need, rowCapacity: factCapacity, flexCandidates: cands.split(bounds[chunk][0], bounds[chunk][1], nil)}
		return local.scanPages(workerCtx, req, params, consume)
	}, visit)
	if sourceErr != nil {
		return true, sourceErr
	}
	if err := scope.pin.valid(ctx); err != nil {
		return true, err
	}
	return true, consumerErr
}
