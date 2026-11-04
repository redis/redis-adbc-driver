// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Immutable, visibility-filtered numeric indexes can prove that NULL ordering
// is immaterial for one pinned candidate set. An unavailable optional count
// proof keeps the ordinary exact fallback. Metadata failures, incomplete indexes
// and cancellation refuse the read before a bounded sort or limit is admitted.
func (e *executor) snapshotOrderNoNulls(ctx context.Context, meta *tableMeta, col columnMeta, filter string, queries []snapshotIndexQuery) ([]snapshotIndexQuery, bool, error) {
	if err := cancelled(ctx); err != nil {
		return nil, false, err
	}
	if !e.snapshotNullOrderEligible(ctx, meta, col) {
		return nil, false, nil
	}
	if queries == nil {
		// Baselines use the same exact directory/bounds proof as overlays. This
		// copy affects planning only, never the persisted table or its indexes.
		indexed := *meta
		indexed.SnapshotOverlay = true
		var ok bool
		var err error
		queries, ok, err = e.snapshotIndexQueries(ctx, &indexed)
		if err != nil {
			if ce := cancelled(ctx); ce != nil {
				return nil, false, ce
			}
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
		if queries == nil {
			queries = []snapshotIndexQuery{}
		}
	}
	for _, q := range queries {
		reply, err := e.store.searchDo(ctx, q.segment.Index, "FT.INFO", q.segment.Index).Result()
		if err != nil {
			if ce := cancelled(ctx); ce != nil {
				return nil, false, ce
			}
			return nil, false, wrapRedis(err, "snapshot numeric ordering index validation failed")
		}
		if err := cancelled(ctx); err != nil {
			return nil, false, err
		}
		if fields, ok := redisReplyFields(reply); ok && snapshotReplyWarning(fields) {
			return nil, false, errorf(adbc.StatusIO, "snapshot numeric ordering received partial index metadata")
		}
		info := indexInfoOf(reply)
		// The zero-missing result is useful only when every registered image is
		// indexed. Visibility masks exclude replaced images and tombstones.
		if readyErr := checkRangeReadinessReply(reply); readyErr != nil {
			return nil, false, readyErr
		}
		if info.failures != 0 || info.numDocs != q.segment.Rows || len(info.prefixes) != 1 || info.prefixes[0] != q.segment.Prefix {
			return nil, false, errorf(adbc.StatusIO, "snapshot numeric ordering found an incomplete or invalid immutable index")
		}
		missing := andSnapshotQuery(andSnapshotQuery(q.query, filter), fmt.Sprintf("-@%s:[-inf +inf]", col.field()))
		count, err := e.store.countMatches(ctx, q.segment.Index, missing)
		if err != nil || count != 0 {
			return nil, false, cancelled(ctx)
		}
	}
	if err := cancelled(ctx); err != nil {
		return nil, false, err
	}
	return queries, true, nil
}

func (e *executor) snapshotNullOrderEligible(ctx context.Context, meta *tableMeta, col columnMeta) bool {
	if snapshotOf(ctx) == nil || e.store == nil || !e.store.snapshots || e.pushdown == PushdownNone || meta.isMem || meta.KeyRows || meta.AppIndex || len(meta.PendingCleanup) > 0 {
		return false
	}
	return col.Indexed && col.Type.Kind != KindString && simpleName(col.field()) && col.MissingThrough == 0 && col.sortsExactly()
}
