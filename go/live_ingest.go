// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
)

// OptionStringIngestLiveSource selects an application prefix for Arrow creation
// in redis_live or a named live schema. Replacement additionally requires the current index identity
// and explicit dedicated-collection consent.
const OptionStringIngestLiveSource = "adbc.redis.ingest.live_source"

func parseLiveIngestSource(value string) (*unmanagedIngestSource, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if len(value) > 64<<10 {
		return nil, errorf(adbc.StatusInvalidArgument, "live_source exceeds 64 KiB")
	}
	var cfg unmanagedIngestSource
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid live_source: %v", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errorf(adbc.StatusInvalidArgument, "live_source requires one JSON object")
	}
	if err := validLivePrefix(cfg.Prefix); err != nil {
		return nil, err
	}
	if cfg.Replace != nil && cfg.Index == "" {
		return nil, errorf(adbc.StatusInvalidArgument, "live replacement requires the current driver-owned index")
	}
	if cfg.Replace == nil && cfg.Index != "" {
		return nil, errorf(adbc.StatusInvalidArgument, "live creation generates its driver-owned index")
	}
	for name, enc := range cfg.Encodings {
		if name == "" || strings.HasPrefix(name, "__") || enc != "native" && enc != "text" {
			return nil, errorf(adbc.StatusInvalidArgument, "invalid live_source encoding")
		}
	}
	return &cfg, nil
}

func (cfg *unmanagedIngestSource) matchesLive(m *tableMeta) error {
	if !m.Live || cfg.Prefix != m.prefix() || cfg.Index != "" && cfg.Index != m.index() {
		return errorf(adbc.StatusInvalidArgument, "live_source does not match the registered source identity")
	}
	copy := *cfg
	copy.Index = m.index()
	// Encoding validation is shared with application-owned index registrations.
	other := *m
	other.Unmanaged = true
	other.UnmanagedPrefixes = []string{m.prefix()}
	return copy.matches(&other)
}

func (s *statementImpl) isLiveIngest(ctx context.Context) (bool, error) {
	if !s.conn.store.liveTables {
		return false, nil
	}
	if s.ingest.SchemaName == liveSchema {
		return true, nil
	}
	if s.ingest.Temporary || s.ingest.CatalogName != "" && s.ingest.CatalogName != catalogName || s.ingest.CatalogName == "" && s.conn.sess.currentCatalog() != catalogName {
		return false, nil
	}
	schema := s.ingest.SchemaName
	if schema == "" {
		schema = s.conn.sess.currentSchema()
	}
	_, ok, err := s.conn.store.liveRefFor(ctx, schema)
	return ok, err
}

func (s *statementImpl) executeLiveIngest(ctx context.Context) (count int64, resultErr error) {
	defer s.clearParams()
	defer func() {
		if resultErr == nil {
			return
		}
		var ae adbc.Error
		if errors.As(resultErr, &ae) {
			for _, d := range ae.Details {
				if d.Key() == "adbc.redis.ingest.acknowledged_rows" {
					return
				}
			}
		}
		resultErr = sourceIngestError("live", count, resultErr)
	}()
	st, opts, cfg := s.conn.store, s.ingest, s.liveSource
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	schema := opts.SchemaName
	if schema == "" {
		schema = s.conn.sess.currentSchema()
	}
	if !st.liveTables || opts.Temporary || s.unmanagedSource != nil {
		return 0, errorf(adbc.StatusInvalidArgument, "live ingestion requires live_tables=true, a live schema and no unmanaged_source")
	}
	if snapshotOf(ctx) != nil || s.conn.store.hasSharedReadView() || snapshotOperationRequestOf(ctx) != nil {
		return 0, errorf(adbc.StatusNotImplemented, "live ingestion cannot use staged, shared or retained operations")
	}
	if !utf8.ValidString(opts.TableName) || strings.ContainsRune(opts.TableName, 0) {
		return 0, errorf(adbc.StatusInvalidArgument, "invalid live table name")
	}
	if opts.CatalogName != "" && opts.CatalogName != catalogName || opts.CatalogName == "" && s.conn.sess.currentCatalog() != catalogName {
		return 0, errorf(adbc.StatusNotFound, "catalog does not exist")
	}
	ref, live, err := st.liveRefFor(ctx, schema)
	if err != nil {
		return 0, err
	}
	if !live {
		return 0, errorf(adbc.StatusInvalidArgument, "live ingestion requires live_tables=true, a live schema and no unmanaged_source")
	}
	if cfg != nil && cfg.Replace != nil && opts.Mode != adbc.OptionValueIngestModeReplace {
		return 0, errorf(adbc.StatusInvalidArgument, "live_source.replace requires replace mode")
	}
	if opts.Mode == adbc.OptionValueIngestModeReplace {
		if cfg == nil || cfg.Replace == nil {
			return 0, errorf(adbc.StatusInvalidArgument, "live replacement requires live_source and explicit consent")
		}
		return s.executeSourceReplace(ctx, cfg, true, ref)
	}
	if opts.Mode != adbc.OptionValueIngestModeCreate && opts.Mode != adbc.OptionValueIngestModeCreateAppend && opts.Mode != adbc.OptionValueIngestModeAppend {
		return 0, errorf(adbc.StatusInvalidArgument, "unknown live ingest mode")
	}
	if err := st.checkLiveNamespace(ctx); err != nil {
		return 0, err
	}
	if err := s.prepareExtensionInput(); err != nil {
		return 0, err
	}
	_, record, err := st.readLiveRecord(ctx, ref, opts.TableName)
	var ae adbc.Error
	if err != nil && (!errors.As(err, &ae) || ae.Code != adbc.StatusNotFound) {
		return 0, err
	}
	if record != nil {
		if s.indexColumns != nil {
			return 0, errorf(adbc.StatusInvalidArgument, "live Arrow append cannot change the registered index column policy")
		}
		if opts.Mode == adbc.OptionValueIngestModeCreate {
			return 0, stateErrorf(stateDuplicateTable, "live table already exists")
		}
		if record.State != "ready" {
			return 0, errorf(adbc.StatusInvalidState, "live registration is %s", record.State)
		}
		if cfg != nil {
			if err := cfg.matchesLive(record.Meta); err != nil {
				return 0, err
			}
		}
	} else {
		if opts.Mode == adbc.OptionValueIngestModeAppend {
			return 0, tableNotFound(schema, opts.TableName)
		}
		if cfg == nil {
			return 0, errorf(adbc.StatusInvalidArgument, "live creation requires live_source with an empty application prefix")
		}
		meta, err := s.newSourceIngestMeta(ref.schema, cfg, true)
		if err != nil {
			return 0, err
		}
		meta.LiveSchemaID = ref.id
		meta.IndexName = liveIndexPrefix + snapshotID()
		if _, err := st.createLiveRegistration(ctx, meta); err != nil {
			return 0, err
		}
		record = &liveRecord{State: "ready", Meta: meta}
	}
	return s.appendLiveArrow(ctx, record.Meta)
}

const liveSchemaReserveLua = `
if redis.call('EXISTS',KEYS[2]) == 1 then return -2 end
if redis.call('HEXISTS',KEYS[1],ARGV[1]) == 1 then return 0 end
if redis.call('HLEN',KEYS[1]) >= tonumber(ARGV[3]) then return -1 end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]); return 1`

// reserveLive records a building registration before index creation. A named
// schema's reservation is refused while its DROP SCHEMA tombstone exists and is
// withdrawn if the schema was dropped after the caller resolved it.
func (s *store) reserveLive(ctx context.Context, meta *tableMeta) (string, error) {
	raw, err := marshalLiveRecord(liveRecord{State: "building", Meta: meta})
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 {
		return "", errorf(adbc.StatusInvalidArgument, "live descriptor exceeds 1 MiB")
	}
	ref := liveRefOf(meta)
	var n int
	if ref.id == "" {
		n, err = once(ctx, s.client, "FCALL", redisFunctionName(`
if redis.call('HEXISTS',KEYS[1],ARGV[1]) == 1 then return 0 end
if redis.call('HLEN',KEYS[1]) >= tonumber(ARGV[3]) then return -1 end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]); return 1`), 1, liveRegistry, meta.Name, string(raw), liveMaxTables).Int()
	} else {
		n, err = once(ctx, s.client, "FCALL", redisFunctionName(liveSchemaReserveLua), 2, ref.registry(), ref.stateKey(), meta.Name, string(raw), liveMaxTables).Int()
	}
	if err != nil {
		return "", wrapRedis(err, "live registration outcome uncertain; inspect before retrying")
	}
	if n == -2 {
		return "", errorf(adbc.StatusInvalidState, "live schema %q is being dropped", meta.Schema)
	}
	if n != 1 {
		return "", errorf(adbc.StatusAlreadyExists, "live table already registered or capacity reached")
	}
	if err := s.confirmLiveReservation(ctx, meta, string(raw)); err != nil {
		return "", err
	}
	return string(raw), nil
}

// Retain the replacement schema before submitting its index creation. A lost
// FT.CREATE reply can then be resolved with the ordinary live resume lifecycle.
func (s *store) stageLiveReplacement(ctx context.Context, before string, meta *tableMeta) (string, error) {
	raw, err := marshalLiveRecord(liveRecord{State: "building", Meta: meta})
	if err != nil {
		return "", err
	}
	n, err := once(ctx, s.client, "FCALL", redisFunctionName(`if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[2] then return 0 end; redis.call('HSET',KEYS[1],ARGV[1],ARGV[3]); return 1`), 1, liveRefOf(meta).registry(), meta.Name, before, string(raw)).Int()
	if err != nil {
		return "", wrapRedis(err, "live replacement staging outcome uncertain; registration retained")
	}
	if n != 1 {
		return "", stateErrorf("40001", "live registration changed during replacement")
	}
	return string(raw), nil
}

func (s *statementImpl) appendLiveArrow(ctx context.Context, meta *tableMeta) (count int64, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = sourceIngestError("live", count, resultErr)
		}
	}()
	if err := s.prepareExtensionInput(); err != nil {
		return 0, err
	}
	if err := s.conn.store.validateLiveIndex(ctx, meta); err != nil {
		return 0, err
	}
	schema := s.params.Schema()
	if schema == nil {
		return 0, errorf(adbc.StatusInvalidArgument, "live Arrow input has no schema")
	}
	wanted, err := metaFromArrow(meta.Schema, meta.Name, schema)
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
	st := s.conn.store.liveExecutionStore(ctx)
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
