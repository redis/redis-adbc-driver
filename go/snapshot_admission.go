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
// Managed SQL DML and Arrow appends share per-table admission; catalog writers
// coordinate database-wide. Only admission is repeated, never statement
// execution or input consumption.
func (s *store) acquireWritableSnapshot(ctx context.Context, target func(context.Context) (*tableMeta, error)) (context.Context, *snapshotPin, error) {
	return s.acquireTableWriteSnapshot(ctx, target)
}

// Appends coordinate before Arrow input is consumed. The hint lives on the
// existing leased writer pin, whose root/reference/expiry semantics are unchanged.
func (s *store) acquireIngestSnapshot(ctx context.Context, target func(context.Context) (*tableMeta, error)) (context.Context, *snapshotPin, error) {
	return s.acquireTableWriteSnapshot(ctx, target)
}

func snapshotIngestAdmissionTarget(meta *tableMeta) string {
	digest := sha256.Sum256([]byte(metaKey(meta.Schema, meta.Name)))
	return fmt.Sprintf("%x", digest)
}

func (s *store) acquireTableWriteSnapshot(ctx context.Context, target func(context.Context) (*tableMeta, error)) (context.Context, *snapshotPin, error) {
	return s.acquireTableWriteSnapshotMode(ctx, target, false)
}

func (s *store) acquireTableWriteSnapshotMode(ctx context.Context, target func(context.Context) (*tableMeta, error), appendOnly bool) (context.Context, *snapshotPin, error) {
	delay := 5 * time.Millisecond
	var turn *snapshotWriterTurn
	var turnTarget string
	defer func() { turn.release() }()
	for {
		sctx, pin, err := s.acquireSnapshot(ctx, true)
		if err != nil {
			return sctx, pin, err
		}
		var meta *tableMeta
		if target != nil {
			meta, err = target(sctx)
		}
		if err != nil {
			pin.release()
			return ctx, nil, err
		}
		if meta != nil && meta.Unmanaged {
			return sctx, pin, nil // Catalog pin only; application writers coordinate themselves.
		}
		parallel := appendOnly && snapshotParallelAppendEligible(meta)
		if meta != nil && !parallel && s.writerDomain != ([32]byte{}) {
			target := snapshotIngestAdmissionTarget(meta)
			if turn != nil && turnTarget != target {
				turn.release()
				turn = nil
			}
			if turn == nil {
				turn, err = snapshotWriteTurns.join(ctx, s.writerDomain, target)
				if err != nil {
					pin.release()
					return ctx, nil, err
				}
				turnTarget = target
				if turn != nil {
					select {
					case <-turn.ready:
					default:
						pin.release()
						if err := turn.wait(ctx); err != nil {
							return ctx, nil, err
						}
						continue // Handoff requires a fresh pin and target validation.
					}
				}
			}
		}
		s.mu.Lock()
		worker := s.compactor != nil
		s.mu.Unlock()
		space := meta == nil || len(meta.snapshotSegments()) < snapshotMaxSegments || !worker
		if space {
			// Catalog writers share membership sets, registries and the OID
			// allocator even for distinct names. Coordinate before execution,
			// using the renewable pin as ownership. A waiter releases its stale
			// pin and resolves again; no SQL or Arrow input has run yet.
			op, admissionTarget := "admit_catalog", ""
			if meta != nil && meta.KeyPrefix != "" {
				op, admissionTarget = "admit_ingest", snapshotIngestAdmissionTarget(meta)
			}
			if parallel {
				op = "admit_append"
				admissionTarget, err = s.snapshotAppendAdmission(sctx, meta)
				if err != nil {
					pin.release()
					return ctx, nil, err
				}
			}
			// Keep the existing control operation/pin field so current Arrow
			// clients and SQL DML coordinate without a storage-format change.
			n, err := s.snapshotControl(sctx, op, pin.token, "", pin.root, admissionTarget).Int()
			if err != nil {
				pin.release()
				return ctx, nil, wrapRedis(transportContextError(ctx, err), "failed to admit snapshot write before execution or input consumption")
			}
			if n == 1 {
				if parallel {
					pin.appendTarget = meta
				}
				pin.writerTurn, turn = turn, nil
				return sctx, pin, nil
			}
			if n != 0 {
				pin.release()
				return ctx, nil, errorf(adbc.StatusInvalidState, "snapshot write admission lease lost before execution or input consumption")
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
	if _, err := e.targetCatalog(ctx, target.Catalog); err != nil {
		return nil, err
	}
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
	catalog, err := s.conn.executor().targetCatalog(ctx, opts.CatalogName)
	if err != nil {
		return nil, err
	}
	if err := validateLogicalSchema(opts.SchemaName); err != nil {
		return nil, err
	}
	if opts.Mode != adbc.OptionValueIngestModeAppend && opts.Mode != adbc.OptionValueIngestModeCreateAppend {
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
	if !isTempSchema(schema) {
		schema = catalogSchema(catalog, schema)
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
