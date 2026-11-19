// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Recognize live targets before managed admission or input consumption. Live
// registrations and source keys do not belong to the managed generation graph.
func (s *statementImpl) isLiveIngest() bool {
	if !s.conn.store.liveTables {
		return false
	}
	if s.ingest.SchemaName == liveSchema {
		return true
	}
	return s.ingest.SchemaName == "" && !s.ingest.Temporary && s.conn.sess.currentSchema() == liveSchema
}

func (s *statementImpl) executeLiveIngest(ctx context.Context) (count int64, resultErr error) {
	defer s.clearParams()
	defer func() {
		if resultErr != nil {
			resultErr = sourceIngestError("live", count, resultErr)
		}
	}()
	if snapshotOf(ctx) != nil || s.conn.store.hasSharedReadView() {
		return 0, errorf(adbc.StatusNotImplemented, "live Arrow ingestion cannot use a shared snapshot or staged execution")
	}
	if snapshotOperationRequestOf(ctx) != nil {
		return 0, errorf(adbc.StatusNotImplemented, "live Arrow ingestion cannot prepare retained operations")
	}
	if s.unmanagedSource != nil {
		return 0, errorf(adbc.StatusInvalidArgument, "live Arrow ingestion cannot use unmanaged_source configuration")
	}
	if s.ingest.Temporary {
		return 0, errorf(adbc.StatusNotImplemented, "live Arrow ingestion cannot target temporary tables")
	}
	if s.ingest.CatalogName != "" && s.ingest.CatalogName != catalogName {
		return 0, errorf(adbc.StatusNotFound, "catalog %q does not exist", s.ingest.CatalogName)
	}
	if s.ingest.Mode != adbc.OptionValueIngestModeAppend && s.ingest.Mode != adbc.OptionValueIngestModeCreateAppend {
		return 0, errorf(adbc.StatusNotImplemented, "live Arrow ingestion supports append and create_append on registered tables; creation and replacement are unsupported")
	}
	if s.indexColumns != nil {
		return 0, errorf(adbc.StatusInvalidArgument, "live Arrow append cannot change the registered index column policy")
	}
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	meta, err := s.conn.store.liveTable(ctx, s.ingest.TableName)
	if err != nil {
		return 0, err
	}
	if err := s.prepareExtensionInput(); err != nil {
		return 0, err
	}
	schema := s.params.Schema()
	if schema == nil {
		return 0, errorf(adbc.StatusInvalidArgument, "live Arrow input has no schema")
	}
	wanted, err := metaFromArrow(liveSchema, meta.Name, schema)
	if err != nil {
		return 0, err
	}
	meta = insertMeta(meta)
	mapping := make([]int, schema.NumFields())
	given := make([]bool, len(meta.Columns))
	for i, f := range schema.Fields() {
		idx, ok := meta.resolve(f.Name)
		if !ok {
			return 0, errorf(adbc.StatusInvalidArgument, "column %q does not exist in live table %q", f.Name, meta.Name)
		}
		if given[idx] {
			return 0, errorf(adbc.StatusInvalidArgument, "duplicate Arrow input column %q", f.Name)
		}
		if f.Name == keyField && wanted.Columns[i].Type.Kind != KindString {
			return 0, errorf(adbc.StatusInvalidArgument, "live Arrow __key must be a string")
		}
		if !storedRecordAppendCompatible(wanted.Columns[i].Type, meta.Columns[idx].Type) {
			return 0, stateErrorf(stateDatatypeMismatch, "stored struct schema must match field names, types and nullability (column %q)", f.Name)
		}
		mapping[i], given[idx] = idx, true
	}
	// Use the current execution's non-replaying, cancellation-owned transport.
	// No shared connection/store state is changed to bypass managed semantics.
	st := &store{client: s.conn.store.redis(ctx), liveTables: true, liveExecution: true}
	e := s.executor()
	e.store = st
	defs := e.columnDefaults(ctx, meta)
	checks, err := e.tableChecks(ctx, meta)
	if err != nil {
		return 0, err
	}
	for {
		if err := cancelled(ctx); err != nil {
			return count, err
		}
		if err := st.checkLiveIdentity(ctx, meta); err != nil {
			return count, err
		}
		if !s.params.Next() {
			break
		}
		rec := s.params.RecordBatch()
		if err := validateInputRecord(rec); err != nil {
			return count, errorf(adbc.StatusInvalidArgument, "invalid live Arrow input: %v", err)
		}
		if !rec.Schema().Equal(schema) {
			return count, errorf(adbc.StatusInvalidArgument, "live Arrow stream schema changed between batches")
		}
		for first := int64(0); first < rec.NumRows(); first += snapshotIngestConvertRows {
			part := rec.NewSlice(first, min(first+snapshotIngestConvertRows, rec.NumRows()))
			written, err := func() (int64, error) {
				defer part.Release()
				n := int(part.NumRows())
				rows := make([][]Value, n)
				values := make([]Value, n*len(meta.Columns))
				for r := range rows {
					if err := cancelled(ctx); err != nil {
						return 0, err
					}
					row := values[r*len(meta.Columns) : (r+1)*len(meta.Columns) : (r+1)*len(meta.Columns)]
					if err := defs.fill(row, given); err != nil {
						return 0, err
					}
					rows[r] = row
				}
				for c, target := range mapping {
					if err := cancelled(ctx); err != nil {
						return 0, err
					}
					col := meta.Columns[target]
					if err := ingestColumn(part.Column(c), col.Type, e.zone(), rows, target); err != nil {
						return 0, columnErr(col.Name, err)
					}
				}
				for _, row := range rows {
					if err := checkNewRow(meta, checks, row); err != nil {
						return 0, err
					}
				}
				if err := st.prepareKeyRows(ctx, meta, rows); err != nil {
					return 0, err
				}
				return st.writeSourceKeyRows(ctx, meta, rows)
			}()
			count += written
			if err != nil {
				return count, err
			}
		}
	}
	if err := s.params.Err(); err != nil && !errors.Is(err, io.EOF) {
		return count, wrapErr(adbc.StatusIO, err, "failed to read live Arrow input")
	}
	return count, st.checkLiveIdentity(ctx, meta)
}
