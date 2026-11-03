// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "context"

// A private segment has no Search index until publication seals its row images.
// Keep its eventual overlay flag separately: private SQL must always resolve
// authoritative directories, even for a future single-source baseline index.
type snapshotIndexState struct{ overlay, deleted bool }

func deferSnapshotIndex(ctx context.Context, meta *tableMeta) bool {
	scope := snapshotOf(ctx)
	if !snapshotOwns(ctx, meta.prefix()) {
		return false
	}
	data := snapshotDataID(meta.prefix())
	scope.mu.Lock()
	if scope.deferredIndexes == nil {
		scope.deferredIndexes = map[string]*snapshotIndexState{}
	}
	if _, exists := scope.deferredIndexes[data]; !exists {
		scope.deferredIndexes[data] = &snapshotIndexState{overlay: meta.SnapshotOverlay}
	}
	scope.mu.Unlock()
	meta.SnapshotOverlay = true
	zero := int64(0)
	meta.SnapshotRows = &zero
	return true
}
func snapshotDeferredIndex(ctx context.Context, meta *tableMeta) (overlay, pending bool) {
	scope := snapshotOf(ctx)
	if scope == nil {
		return false, false
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	state, found := scope.deferredIndexes[snapshotDataID(meta.prefix())]
	pending = found
	if pending {
		overlay = state.overlay || state.deleted
	}
	return
}
func finishSnapshotIndex(ctx context.Context, meta *tableMeta) {
	scope := snapshotOf(ctx)
	scope.mu.Lock()
	delete(scope.deferredIndexes, snapshotDataID(meta.prefix()))
	scope.mu.Unlock()
}

// Route capability discovery through an existing immutable input. A deferred
// output index is deliberately absent, including from Software's global registry.
func snapshotSourceIndex(ctx context.Context, meta *tableMeta) string {
	if _, pending := snapshotDeferredIndex(ctx, meta); pending {
		for _, seg := range meta.snapshotSegments() {
			if !snapshotOwns(ctx, seg.Prefix) {
				return seg.Index
			}
		}
	}
	return meta.index()
}

func snapshotNoopDML(ctx context.Context, ps ParsedStmt) bool {
	switch ps.Stmt.(type) {
	case *UpdateStmt, *DeleteStmt:
	default:
		return false
	}
	scope := snapshotOf(ctx)
	scope.mu.Lock()
	defer scope.mu.Unlock()
	return scope.writes == 0
}
