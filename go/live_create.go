// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/google/uuid"
)

// defaultLivePrefix is the application prefix of a SQL-created live table:
// "<schema>:<table>:", with each part escaped like managed key parts so it
// contains no other ':' and a schema named adbc stays outside driver storage.
func defaultLivePrefix(schema, table string) string {
	return snapshotSchemaPart(schema) + ":" + escapeKeyPart(table) + ":"
}

// liveCreatePrefix applies the WITH (prefix = '…') storage parameter, the only
// one live tables accept.
func liveCreatePrefix(schema, table string, opts []StorageOption) (string, error) {
	prefix := defaultLivePrefix(schema, table)
	seen := false
	for _, o := range opts {
		if o.Name != "prefix" {
			return "", stateErrorf(stateFeatureNotSupported, "unsupported live table storage parameter %q (supported: prefix)", o.Name)
		}
		if seen {
			return "", errorf(adbc.StatusInvalidArgument, "storage parameter prefix is specified more than once")
		}
		seen, prefix = true, o.Value
	}
	if err := validLivePrefix(prefix); err != nil {
		return "", err
	}
	return prefix, nil
}

func validLiveColumnName(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "__") {
		return errorf(adbc.StatusInvalidArgument, "live column name %q is empty, reserved or not valid UTF-8", name)
	}
	return nil
}

// newLiveTableMeta is the descriptor of a new SQL-created live table. Every
// column is nullable: a HASH field may be absent, and direct Redis writes
// cannot be held to NOT NULL, CHECK, DEFAULT or key constraints.
func newLiveTableMeta(ref liveRef, name, prefix, comment string, cols []columnMeta, noIndex map[string]bool) (*tableMeta, error) {
	meta := &tableMeta{Schema: ref.schema, LiveSchemaID: ref.id, Name: name, Comment: comment, KeyPrefix: prefix, IndexName: liveIndexPrefix + snapshotID(), Adopted: true, KeyRows: true, Live: true, LiveVersion: snapshotID()}
	if len(cols) == 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "table %q must have at least one column", name)
	}
	seen := map[string]bool{}
	for _, c := range cols {
		if err := validLiveColumnName(c.Name); err != nil {
			return nil, err
		}
		if seen[c.Name] {
			return nil, stateErrorf(stateDuplicateColumn, "column %q specified more than once", c.Name)
		}
		seen[c.Name] = true
		c.Nullable = true
		meta.Columns = append(meta.Columns, c)
	}
	if err := finishLiveMeta(meta, nil, noIndex); err != nil {
		return nil, err
	}
	return meta, nil
}

// finishLiveMeta applies the live index policy and Search facts shared by
// Arrow and SQL live creation. Text-encoded temporal/boolean fields and UUIDs
// never use the native attribute layout. Application writes can introduce
// NaNs or new TAG values at any time, so those facts start pessimistic.
func finishLiveMeta(meta *tableMeta, only, noIndex map[string]bool) error {
	if len(meta.Columns) > 1024 {
		return errorf(adbc.StatusInvalidArgument, "live tables support at most 1024 columns")
	}
	excluded := map[string]bool{}
	for name, v := range noIndex {
		excluded[name] = v
	}
	for _, c := range meta.Columns {
		if c.Encoding == "text" || c.Type.Alias == "uuid" {
			excluded[c.Name] = true
		}
	}
	if only != nil {
		filtered := map[string]bool{}
		for name := range only {
			if !excluded[name] {
				filtered[name] = true
			}
		}
		only = filtered
	}
	if err := meta.applyIndexPolicy(only, excluded); err != nil {
		return err
	}
	for i := range meta.Columns {
		c := &meta.Columns[i]
		if !utf8.ValidString(c.Name) {
			return errorf(adbc.StatusInvalidArgument, "live column names must be UTF-8")
		}
		c.NaNs = c.Type.Kind.isFloat()
		if c.Type.Kind == KindString {
			c.TagsChecked, c.TagValues = true, tagsTruncated
		}
	}
	raw, err := marshalLiveRecord(liveRecord{State: "ready", Meta: meta})
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errorf(adbc.StatusInvalidArgument, "live descriptor exceeds 1 MiB")
	}
	return nil
}

// requireUnusedLivePrefix refuses a SQL-created table whose prefix already
// holds keys or is covered by any Search index. Every live registration owns
// an index on its exact prefix, so this also refuses the prefix of a renamed
// live table and application indexes that would capture the new rows.
func (s *store) requireUnusedLivePrefix(ctx context.Context, prefix string) error {
	if err := s.requireEmptyUnmanagedPrefix(ctx, prefix); err != nil {
		var ae adbc.Error
		if asAdbc(err, &ae) && ae.Code == adbc.StatusAlreadyExists {
			return errorf(adbc.StatusAlreadyExists, "live prefix %q already holds keys; choose another WITH (prefix = …) or register the existing collection", prefix)
		}
		return err
	}
	indexes, unreadable, err := (&hashConn{client: s.client, st: s}).searchIndexes(ctx)
	if err != nil {
		return err
	}
	if unreadable != 0 {
		return errorf(adbc.StatusInvalidState, "cannot verify that no Search index covers prefix %q: %d index definitions are unreadable", prefix, unreadable)
	}
	for _, index := range indexes {
		for _, p := range index.prefixes {
			if prefixesOverlap(p, prefix) {
				return errorf(adbc.StatusInvalidState, "Search index %q already covers live prefix %q; choose another WITH (prefix = …)", index.name, prefix)
			}
		}
	}
	return nil
}

// createLiveRegistration reserves, indexes and publishes a new registration on
// an empty prefix. It is not atomic: a failure can leave a building
// registration or its index, recoverable with live resume/unregister. An
// uncertain index creation is never replayed.
func (s *store) createLiveRegistration(ctx context.Context, meta *tableMeta) (string, error) {
	if err := s.requireEmptyUnmanagedPrefix(ctx, meta.prefix()); err != nil {
		return "", err
	}
	before, err := s.reserveLive(ctx, meta)
	if err != nil {
		return "", err
	}
	if err := s.searchDo(ctx, meta.index(), liveIndexArgs(meta)...).Err(); err != nil {
		return "", wrapRedis(err, "live index creation failed; registration retained; do not replay")
	}
	if err := s.requireEmptyUnmanagedPrefix(ctx, meta.prefix()); err != nil {
		return "", err
	}
	if err := s.awaitIndexReady(ctx, meta.index()); err != nil {
		return "", err
	}
	return before, s.publishLive(ctx, meta.Name, before, meta)
}

// tryCreateLiveTable runs CREATE TABLE in a live schema. The SQL definition
// becomes a live registration on an empty application prefix with a fresh
// driver-owned index; no hidden fields are written.
func (e *executor) tryCreateLiveTable(ctx context.Context, st *CreateTableStmt, numParams int) (bool, execResult, error) {
	if !e.store.liveTables || st.Temporary {
		return false, execResult{}, nil
	}
	ref, name, ok, err := e.liveTableTarget(ctx, st.Table)
	if !ok || err != nil {
		return err != nil, execResult{}, err
	}
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return true, execResult{}, errorf(adbc.StatusInvalidArgument, "a live table name is required")
	}
	if e.store.flexEnabled() {
		return true, execResult{}, flexUnsupported("live HASH tables")
	}
	ctx, cancel := statementOperationContext(ctx, e.sess)
	defer cancel()
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	if _, _, err := e.store.readLiveRecord(ctx, ref, name); err == nil {
		if st.IfNotExists {
			return true, execResult{affected: -1}, nil
		}
		return true, execResult{}, stateErrorf(stateDuplicateTable, "relation %q already exists", name)
	} else if ae := (adbc.Error{}); !asAdbc(err, &ae) || ae.Code != adbc.StatusNotFound {
		return true, execResult{}, err
	}
	prefix, err := liveCreatePrefix(ref.schema, name, st.Options)
	if err != nil {
		return true, execResult{}, err
	}
	if st.AsSelect != nil {
		n, err := e.createLiveTableAs(ctx, st, ref, name, prefix, numParams)
		return true, execResult{affected: n}, err
	}
	if len(st.Checks)+len(st.PrimaryKeys)+len(st.Declarations) > 0 {
		return true, execResult{}, liveConstraintUnsupported()
	}
	var cols []columnMeta
	noIndex := map[string]bool{}
	for _, c := range st.Columns {
		if c.Serial || c.Identity != "" || c.Sequence.Name != "" || c.NotNull || c.Default != nil || len(c.Checks)+len(c.PrimaryKeys)+len(c.Declarations) > 0 {
			return true, execResult{}, liveConstraintUnsupported()
		}
		cols = append(cols, columnMeta{Name: c.Name, Type: c.Type, Comment: c.Comment})
		noIndex[c.Name] = c.NoIndex
	}
	meta, err := newLiveTableMeta(ref, name, prefix, st.Comment, cols, noIndex)
	if err != nil {
		return true, execResult{}, err
	}
	if err := e.store.requireUnusedLivePrefix(ctx, prefix); err != nil {
		return true, execResult{}, err
	}
	_, err = e.store.createLiveRegistration(ctx, meta)
	return true, execResult{affected: -1}, err
}

func liveConstraintUnsupported() error {
	return stateErrorf(stateFeatureNotSupported, "live tables support column names, types, NULL, NOINDEX and COMMENT only; NOT NULL, DEFAULT, identity, CHECK and key constraints cannot be enforced against direct Redis writes")
}

// createLiveTableAs runs CREATE TABLE … AS SELECT into a live schema. Like
// other live writes, the query may read only live tables; it is planned and
// evaluated in the isolated live executor before anything is created. Rows
// are then inserted with the ordinary non-replaying live key-row writes, so
// a failure can leave the new table with some of its rows.
func (e *executor) createLiveTableAs(ctx context.Context, st *CreateTableStmt, ref liveRef, name, prefix string, numParams int) (int64, error) {
	if numParams > 0 {
		return 0, errorf(adbc.StatusNotImplemented, "CREATE TABLE … AS in a live schema cannot use parameters")
	}
	if err := e.store.requireUnusedLivePrefix(ctx, prefix); err != nil {
		return 0, err
	}
	local := *e
	local.store = e.store.liveExecutionStore(ctx)
	plan, err := local.planSelect(ctx, st.AsSelect, nil)
	if err != nil {
		return 0, err
	}
	if err := checkStorableColumns(plan.columns()); err != nil {
		return 0, err
	}
	rows, err := local.runSelect(ctx, plan, nil)
	if err != nil {
		return 0, err
	}
	resultCols := plan.columns()
	resolveNumericColumns(resultCols, rows)
	cols := make([]columnMeta, len(resultCols))
	for j, c := range resultCols {
		t := c.Type
		if t.Kind == KindNull {
			t = typeString // SELECT NULL AS x: no type information
		}
		if t.Kind == KindString && t.Length > 0 && !fitsLength(rows, j, t) {
			t = t.withoutTypmod()
		}
		cols[j] = columnMeta{Name: c.Name, Type: t}
	}
	meta, err := newLiveTableMeta(ref, name, prefix, "", cols, nil)
	if err != nil {
		return 0, err
	}
	keyed := insertMeta(meta)
	ki, _ := keyed.resolve(keyField)
	values := make([][]Value, 0, len(rows))
	for _, row := range rows {
		if err := cancelled(ctx); err != nil {
			return 0, err
		}
		out := make([]Value, len(keyed.Columns))
		fields := 0
		for j, v := range row {
			cv, err := coerceIn(v, meta.Columns[j].Type, e.zone())
			if err != nil {
				return 0, columnErr(meta.Columns[j].Name, err)
			}
			out[j] = cv
			if !cv.Null {
				fields++
			}
		}
		if fields == 0 {
			return 0, errorf(adbc.StatusIntegrity, "CREATE TABLE … AS row has only NULL values; a live row needs at least one field")
		}
		out[ki] = stringValue(prefix + uuid.NewString())
		values = append(values, out)
	}
	// Creation rechecks that the prefix is still empty around index creation.
	if _, err := e.store.createLiveRegistration(ctx, meta); err != nil {
		return 0, err
	}
	n, err := local.store.insertRows(ctx, keyed, values)
	if err != nil {
		return n, wrapErr(adbc.StatusIO, err, fmt.Sprintf("live table %s.%s was created; %d rows were acknowledged before the failure (a lower bound) and are not rolled back", ref.schema, name, n))
	}
	return n, nil
}
