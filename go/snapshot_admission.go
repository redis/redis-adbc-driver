// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Wait before reserving a workspace, planning SQL or consuming Arrow input.
// Release a full lineage's write-intent pin while maintenance makes room;
// retaining it would block the very compaction publication we need to admit.
// Only admission is repeated, never statement execution or input consumption.
func (s *store) acquireWritableSnapshot(ctx context.Context, target func(context.Context) (*tableMeta, error)) (context.Context, *snapshotPin, error) {
	delay := 5 * time.Millisecond
	for {
		sctx, pin, err := s.acquireSnapshot(ctx, true)
		if err != nil || target == nil {
			return sctx, pin, err
		}
		meta, err := target(sctx)
		if err != nil {
			pin.release()
			return ctx, nil, err
		}
		s.mu.Lock()
		worker := s.compactor != nil
		s.mu.Unlock()
		if meta == nil || len(meta.snapshotSegments()) < snapshotMaxSegments || !worker {
			return sctx, pin, nil
		}
		pin.release()
		s.wakeSnapshotCompactor()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, nil, cancelled(ctx)
		case <-timer.C:
		}
		delay = min(delay*2, 100*time.Millisecond)
	}
}
func (e *executor) snapshotWriteTarget(ctx context.Context, target *TableName) (*tableMeta, error) {
	if err := e.store.refreshSnapshotTemp(ctx); err != nil {
		return nil, err
	}
	schema, table, err := e.resolveTable(*target)
	if err != nil {
		return nil, err
	}
	return e.store.getTable(ctx, schema, table)
}
func (s *statementImpl) snapshotIngestTarget(ctx context.Context) (*tableMeta, error) {
	opts := s.ingest
	if opts.Mode != adbc.OptionValueIngestModeAppend && opts.Mode != adbc.OptionValueIngestModeCreateAppend {
		return nil, nil
	}
	if opts.CatalogName != "" && opts.CatalogName != catalogName {
		return nil, nil
	}
	schema := opts.SchemaName
	switch {
	case opts.Temporary || isTempAlias(schema):
		if schema != "" && !isTempAlias(schema) {
			return nil, nil
		}
		schema = s.conn.store.tempSchema()
	case schema == "":
		schema = s.conn.sess.currentSchema()
	}
	meta, err := s.conn.store.getTable(ctx, schema, opts.TableName)
	var ae adbc.Error
	if errors.As(err, &ae) && ae.Code == adbc.StatusNotFound {
		return nil, nil
	}
	return meta, err
}
