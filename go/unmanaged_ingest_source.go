// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	"io"
	"strings"
	"unicode/utf8"
)

type unmanagedIngestSource struct {
	Prefix    string                   `json:"prefix"`
	Index     string                   `json:"index"`
	Encodings map[string]string        `json:"encodings,omitempty"`
	Replace   *unmanagedReplaceConsent `json:"replace,omitempty"`
}

func parseUnmanagedIngestSource(value string) (*unmanagedIngestSource, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if len(value) > 64<<10 {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged_source exceeds 64 KiB")
	}
	var cfg unmanagedIngestSource
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid unmanaged_source: %v", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged_source must contain one JSON object")
	}
	if cfg.Prefix == "" || cfg.Index == "" || !unmanagedSourceKey(cfg.Index) {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged_source requires an application prefix and index")
	}
	meta := &tableMeta{Schema: defaultSchema, Name: "source", KeyPrefix: cfg.Prefix, IndexName: cfg.Index, Adopted: true, KeyRows: true, AppIndex: true, Unmanaged: true, UnmanagedPrefixes: []string{cfg.Prefix}}
	if err := validateUnmanagedDescriptor(meta); err != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid unmanaged_source: %v", err)
	}
	for name, encoding := range cfg.Encodings {
		if name == "" || name == keyField || (encoding != "native" && encoding != "text") {
			return nil, errorf(adbc.StatusInvalidArgument, "unmanaged_source encodings require public columns and text/native values")
		}
	}
	return &cfg, nil
}

func (cfg *unmanagedIngestSource) matches(meta *tableMeta) error {
	if !meta.Unmanaged || meta.prefix() != cfg.Prefix || meta.index() != cfg.Index || len(meta.UnmanagedPrefixes) != 1 || meta.UnmanagedPrefixes[0] != cfg.Prefix {
		return errorf(adbc.StatusInvalidArgument, "unmanaged_source does not match the existing table's ownership or source identity")
	}
	for name, want := range cfg.Encodings {
		i, ok := meta.resolve(name)
		if !ok {
			return errorf(adbc.StatusInvalidArgument, "encoding column %q does not exist", name)
		}
		actual := meta.Columns[i].Encoding
		if actual == "" {
			actual = "native"
		}
		if actual != want {
			return errorf(adbc.StatusInvalidArgument, "unmanaged_source encoding differs for column %q", name)
		}
	}
	return nil
}

func (s *statementImpl) newUnmanagedIngestMeta(schema string) (*tableMeta, error) {
	return s.newSourceIngestMeta(schema, s.unmanagedSource, false)
}

func (s *statementImpl) newSourceIngestMeta(schema string, cfg *unmanagedIngestSource, live bool) (*tableMeta, error) {
	if s.params.Schema() == nil {
		return nil, errorf(adbc.StatusInvalidArgument, "Arrow input has no schema")
	}
	wanted, err := metaFromArrow(schema, s.ingest.TableName, s.params.Schema())
	if err != nil {
		return nil, err
	}
	meta := &tableMeta{Schema: schema, Name: s.ingest.TableName, KeyPrefix: cfg.Prefix, IndexName: cfg.Index, Adopted: true, KeyRows: true, AppIndex: true, Unmanaged: true, UnmanagedPrefixes: []string{cfg.Prefix}}
	seen := map[string]bool{}
	hasKey := false
	for _, c := range wanted.Columns {
		if seen[c.Name] {
			return nil, stateErrorf(stateDuplicateColumn, "duplicate Arrow column %q", c.Name)
		}
		seen[c.Name] = true
		if c.Name == keyField {
			if c.Type.Kind != KindString {
				return nil, errorf(adbc.StatusInvalidArgument, "unmanaged Arrow __key must be a string")
			}
			hasKey = true
			continue
		}
		if c.Name == "" || strings.HasPrefix(c.Name, "__") {
			return nil, errorf(adbc.StatusInvalidArgument, "column name %q is empty or reserved", c.Name)
		}
		meta.Columns = append(meta.Columns, c)
	}
	if (!hasKey && !live) || len(meta.Columns) == 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged creation requires __key and at least one public data column")
	}
	for name, encoding := range cfg.Encodings {
		i, ok := meta.resolve(name)
		if !ok {
			return nil, errorf(adbc.StatusInvalidArgument, "encoding column %q does not exist", name)
		}
		if encoding == "text" {
			if !textEncodedKind(meta.Columns[i].Type.Kind) {
				return nil, errorf(adbc.StatusInvalidArgument, "text encoding is only supported for boolean, date and time columns (%s)", name)
			}
			meta.Columns[i].Encoding = "text"
		}
	}
	if live {
		meta.Live, meta.Unmanaged, meta.AppIndex = true, false, false
		meta.LiveVersion = snapshotID()
		meta.UnmanagedPrefixes = nil
		if len(meta.Columns) > 1024 {
			return nil, errorf(adbc.StatusInvalidArgument, "live tables support at most 1024 columns")
		}
		// Text-encoded temporal/boolean fields and UUIDs cannot use the
		// numeric/native attribute layout. Match ordinary live adoption, even
		// when an explicit policy names one of these fields.
		noIndex := map[string]bool{}
		for _, c := range meta.Columns {
			noIndex[c.Name] = c.Encoding == "text" || c.Type.Alias == "uuid"
		}
		only := s.indexColumns
		if only != nil {
			only = map[string]bool{}
			for name := range s.indexColumns {
				if _, ok := meta.resolve(name); !ok {
					return nil, errorf(adbc.StatusInvalidArgument, "index column %q does not exist", name)
				}
				if !noIndex[name] {
					only[name] = true
				}
			}
		}
		if err := meta.applyIndexPolicy(only, noIndex); err != nil {
			return nil, err
		}
		for i := range meta.Columns {
			c := &meta.Columns[i]
			if !utf8.ValidString(c.Name) {
				return nil, errorf(adbc.StatusInvalidArgument, "live column names must be UTF-8")
			}
			c.NaNs = c.Type.Kind.isFloat()
			if c.Type.Kind == KindString {
				c.TagsChecked, c.TagValues = true, tagsTruncated
			}
		}
		raw, err := json.Marshal(liveRecord{State: "ready", Meta: meta})
		if err != nil {
			return nil, err
		}
		if len(raw) > 1<<20 {
			return nil, errorf(adbc.StatusInvalidArgument, "live descriptor exceeds 1 MiB")
		}
		return meta, nil
	}
	if s.indexColumns != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged creation builds a membership index; application column indexes must be configured separately")
	}
	return meta, validateUnmanagedDescriptor(meta)
}

func (s *store) requireEmptyUnmanagedPrefix(ctx context.Context, prefix string) error {
	scan, err := (&hashConn{client: s.client}).scanKeys(ctx, prefix, "")
	if err != nil {
		return err
	}
	for {
		keys, done, err := scan.next(ctx)
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			return errorf(adbc.StatusAlreadyExists, "unmanaged creation requires an empty application prefix")
		}
		if done {
			return nil
		}
	}
}

func (s *statementImpl) executeUnmanagedSourceIngest(ctx context.Context) (int64, error) {
	if s.ingest.Mode == adbc.OptionValueIngestModeReplace && s.unmanagedSource.Replace != nil {
		return s.executeUnmanagedReplace(ctx)
	}
	if s.unmanagedSource.Replace != nil {
		defer s.clearParams()
		return -1, errorf(adbc.StatusInvalidArgument, "unmanaged_source.replace requires replace mode")
	}
	fail := func(err error) (int64, error) { return 0, unmanagedIngestError(0, err) }
	entered := false
	defer func() {
		if !entered {
			s.clearParams()
		}
	}()
	opts := s.ingest
	st := s.conn.store
	cfg := s.unmanagedSource
	if !st.snapshots {
		return fail(unmanagedUnsupported("creation without a snapshot catalog"))
	}
	if snapshotOperationRequestOf(ctx) != nil {
		return fail(unmanagedUnsupported("retained operation preparation"))
	}
	if opts.Mode != adbc.OptionValueIngestModeCreate && opts.Mode != adbc.OptionValueIngestModeCreateAppend && opts.Mode != adbc.OptionValueIngestModeAppend {
		return fail(unmanagedUnsupported("Arrow replace"))
	}
	if opts.CatalogName != "" && opts.CatalogName != catalogName || opts.CatalogName == "" && s.conn.sess.currentCatalog() != catalogName {
		return fail(errorf(adbc.StatusNotFound, "catalog %q does not exist", opts.CatalogName))
	}
	schema := opts.SchemaName
	if schema == "" {
		schema = s.conn.sess.currentSchema()
	}
	if opts.Temporary || isTempAlias(schema) || isTempSchema(schema) {
		return fail(unmanagedUnsupported("temporary creation"))
	}
	if err := st.checkWritableSchema(schema); err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	var existing *tableMeta
	readCtx, pin, err := st.acquireIngestSnapshot(ctx, func(c context.Context) (*tableMeta, error) {
		var err error
		existing, err = st.getTableMetadata(c, schema, opts.TableName)
		var ae adbc.Error
		if errors.As(err, &ae) && ae.Code == adbc.StatusNotFound {
			existing = nil
			if opts.Mode == adbc.OptionValueIngestModeAppend {
				return nil, err
			}
			// Coordinate registration creation only. No Arrow input is consumed
			// under this catalog gate, which is released before direct row writes.
			return &tableMeta{Schema: schema, Name: opts.TableName}, nil
		}
		if err != nil {
			return nil, err
		}
		if !existing.Unmanaged || opts.Mode == adbc.OptionValueIngestModeCreate {
			return nil, nil
		}
		return existing, nil
	})
	if err != nil {
		return fail(err)
	}
	defer func() {
		if pin != nil {
			pin.release()
		}
	}()
	if existing != nil {
		if opts.Mode == adbc.OptionValueIngestModeCreate {
			return fail(stateErrorf(stateDuplicateTable, "table %q already exists", opts.TableName))
		}
		if err := cfg.matches(existing); err != nil {
			return fail(err)
		}
	} else {
		if n, err := st.redis(readCtx).Exists(readCtx, viewKey(schema, opts.TableName), sequenceKey(schema, opts.TableName), sqlIndexKey(schema, opts.TableName)).Result(); err != nil {
			return fail(err)
		} else if n != 0 {
			return fail(stateErrorf(stateDuplicateTable, "destination name is already used"))
		}
		if err := s.prepareExtensionInput(); err != nil {
			return fail(err)
		}
		meta, err := s.newUnmanagedIngestMeta(schema)
		if err != nil {
			return fail(err)
		}
		if err := st.requireEmptyUnmanagedPrefix(ctx, cfg.Prefix); err != nil {
			return fail(err)
		}
		if err := st.searchDo(ctx, cfg.Index, "FT.INFO", cfg.Index).Err(); err == nil {
			return fail(errorf(adbc.StatusAlreadyExists, "application index already exists; register the existing source explicitly"))
		} else {
			var native interface {
				error
				RedisError()
			}
			if !errors.As(err, &native) || !isUnknownIndex(err) {
				return fail(wrapRedis(err, "failed to check application index"))
			}
		}
		if err := pin.valid(readCtx); err != nil {
			return fail(err)
		}
		// NOINDEX retains matching HASHes in Search's document table, including
		// rows whose first field is NULL. No hidden row-ID fields are required.
		args := []any{"FT.CREATE", cfg.Index, "ON", "HASH", "PREFIX", 1, cfg.Prefix, "SKIPINITIALSCAN", "SCHEMA", meta.Columns[0].field(), "TAG", "NOINDEX"}
		attempted := func(err error) (int64, error) {
			var ae adbc.Error
			if !errors.As(err, &ae) {
				ae = adbc.Error{Code: adbc.StatusIO, Msg: err.Error()}
			}
			ae.Msg = "unmanaged creation may have left an application index or registration; inspect before retrying: " + ae.Msg
			return fail(ae)
		}
		if err := st.searchDo(ctx, cfg.Index, args...).Err(); err != nil {
			return attempted(wrapRedis(err, "application index creation failed; command was not replayed"))
		}
		// A key introduced between the first empty check and FT.CREATE could
		// have missed indexing. Refuse before registration/input rather than
		// advertise an incomplete live collection or replay an initial scan.
		if err := st.requireEmptyUnmanagedPrefix(ctx, cfg.Prefix); err != nil {
			return attempted(err)
		}
		if err := st.awaitIndexReady(ctx, cfg.Index); err != nil {
			return attempted(err)
		}
		if err := pin.valid(readCtx); err != nil {
			return attempted(err)
		}
		if err := st.registerUnmanagedMode(ctx, meta, true); err != nil {
			return attempted(err)
		}
		pin.release()
		pin = nil
		readCtx, pin, err = st.acquireSnapshot(ctx, true)
		if err != nil {
			return attempted(err)
		}
		existing, err = st.getTableMetadata(readCtx, schema, opts.TableName)
		if err != nil {
			return attempted(err)
		}
		before, beforeErr := json.Marshal(meta.Columns)
		after, afterErr := json.Marshal(existing.Columns)
		if existing.SnapshotAccessID != meta.SnapshotAccessID || cfg.matches(existing) != nil ||
			beforeErr != nil || afterErr != nil || !bytes.Equal(before, after) {
			return attempted(stateErrorf("40001", "unmanaged registration changed during creation"))
		}
	}
	// This pin fixes the destination identity even if another catalog root
	// drops/re-registers the name while application rows are being written.
	appendCtx := context.WithValue(readCtx, unmanagedWriteKey{}, metaKey(schema, opts.TableName))
	s.ingest.Mode = adbc.OptionValueIngestModeAppend
	defer func() { s.ingest = opts }()
	entered = true
	n, err := s.executeIngest(appendCtx)
	if err != nil && n < 0 {
		return fail(err)
	}
	return n, err
}
