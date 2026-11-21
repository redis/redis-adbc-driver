// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Public indexed columns can acquire new TAG/NaN facts while input is consumed.
// Restrict early indexes to the stable hidden numeric layout. In particular,
// imported provenance introduces a hidden TAG and stays on the deferred path.
func snapshotAppendIndexEligible(meta *tableMeta, mode string) bool {
	if meta == nil || meta.Unmanaged || meta.Live || meta.SnapshotSourceKey ||
		(mode != adbc.OptionValueIngestModeAppend && mode != adbc.OptionValueIngestModeCreateAppend) {
		return false
	}
	for _, column := range meta.Columns {
		if column.Indexed {
			return false
		}
	}
	return true
}

// This runs before the first Next, not after the first batch. Complete-image
// atomic writes populate the fresh index; the normal final publication proofs
// still run. Never retry creation after an uncertain outcome or backfill an
// already populated private segment.
func (s *store) prepareSnapshotAppendIndex(ctx context.Context, meta *tableMeta, mode string) error {
	scope := snapshotOf(ctx)
	if !snapshotAppendIndexEligible(meta, mode) || scope == nil || !scope.write ||
		scope.operation || snapshotOperationRequestOf(ctx) != nil || !snapshotOwns(ctx, meta.prefix()) {
		return nil
	}
	data := snapshotDataID(meta.prefix())
	scope.mu.Lock()
	state := scope.deferredIndexes[data]
	eligible := state != nil && !state.appendIndexed
	scope.mu.Unlock()
	if !eligible {
		return nil
	}
	count, err := s.redis(ctx).ZCard(ctx, snapshotMembers(data)).Result()
	if err != nil {
		return wrapRedis(err, "failed to verify empty append index directory")
	}
	if count != 0 {
		return errorf(adbc.StatusInvalidState, "early append index requires an empty private directory")
	}
	if err := s.searchDo(ctx, meta.index(), indexCreateArgs(meta)...).Err(); err != nil {
		return wrapRedis(transportContextError(ctx, err), "failed to create empty append index; operation was not replayed")
	}
	scope.mu.Lock()
	state.appendIndexed = true
	scope.mu.Unlock()
	return nil
}

func snapshotAppendIndexed(ctx context.Context, meta *tableMeta) bool {
	scope := snapshotOf(ctx)
	if scope == nil {
		return false
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	state := scope.deferredIndexes[snapshotDataID(meta.prefix())]
	return state != nil && state.appendIndexed
}
