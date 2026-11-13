// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Wait before reserving a workspace, planning SQL or consuming Arrow input.
// Release a full lineage's write-intent pin while maintenance makes room;
// retaining it would block the very compaction publication we need to admit.
// Only admission is repeated, never statement execution or input consumption.
func (s *store) acquireWritableSnapshot(ctx context.Context, target func(context.Context) (*tableMeta, error)) (context.Context, *snapshotPin, error) {
	return s.acquireWritableSnapshotMode(ctx, target, false)
}

// Appends coordinate before Arrow input is consumed. The hint lives on the
// existing leased writer pin, whose root/reference/expiry semantics are unchanged.
func (s *store) acquireIngestSnapshot(ctx context.Context, target func(context.Context) (*tableMeta, error)) (context.Context, *snapshotPin, error) {
	return s.acquireWritableSnapshotMode(ctx, target, true)
}

func snapshotIngestAdmissionTarget(meta *tableMeta) string {
	digest := sha256.Sum256([]byte(metaKey(meta.Schema, meta.Name)))
	return fmt.Sprintf("%x", digest)
}

func (s *store) acquireWritableSnapshotMode(ctx context.Context, target func(context.Context) (*tableMeta, error), ingest bool) (context.Context, *snapshotPin, error) {
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
		if meta != nil && meta.Unmanaged {
			return sctx, pin, nil // Catalog pin only; application writers coordinate themselves.
		}
		s.mu.Lock()
		worker := s.compactor != nil
		s.mu.Unlock()
		space := meta == nil || len(meta.snapshotSegments()) < snapshotMaxSegments || !worker
		if space {
			if !ingest || meta == nil {
				return sctx, pin, nil
			}
			n, err := s.snapshotControl(sctx, "admit_ingest", pin.token, "", pin.root, snapshotIngestAdmissionTarget(meta)).Int()
			if err != nil {
				pin.release()
				return ctx, nil, wrapRedis(transportContextError(ctx, err), "failed to admit snapshot ingest before input consumption")
			}
			if n == 1 {
				return sctx, pin, nil
			}
			if n != 0 {
				pin.release()
				return ctx, nil, errorf(adbc.StatusInvalidState, "snapshot ingest admission lease lost before input consumption")
			}
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
		if schema == "" || isTempAlias(schema) {
			return nil, nil
		}
		return &tableMeta{Schema: schema, Name: opts.TableName}, nil
	}
	return meta, err
}
