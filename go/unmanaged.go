// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// OptionStringForeignIndexesMode selects managed copies (default) or live
// unmanaged registrations for foreign-index discovery and explicit refresh.
const OptionStringForeignIndexesMode = "adbc.redis.foreign_indexes_mode"

func parseTableMode(mode string) (string, error) {
	switch mode {
	case "", "managed":
		return "managed", nil
	case "unmanaged":
		return mode, nil
	default:
		return "", errorf(adbc.StatusInvalidArgument, "table mode must be managed or unmanaged")
	}
}

func unmanagedUnsupported(operation string) error {
	return errorf(adbc.StatusNotImplemented, "%s is not supported for unmanaged tables", operation)
}

func unmanagedSourceKey(key string) bool {
	return key != "" && !strings.HasPrefix(key, metaPrefix) &&
		!strings.Contains(key, snapshotRowMarker) &&
		!strings.HasPrefix(key, "adbc:schemas") &&
		!strings.HasPrefix(key, "adbc:meta:") &&
		!strings.HasPrefix(key, "adbc:seq:") &&
		!strings.HasPrefix(key, "adbc:tables:")
}

func validateUnmanagedDescriptor(meta *tableMeta) error {
	if !meta.Adopted || !meta.KeyRows || !meta.AppIndex || meta.SnapshotRows != nil ||
		len(meta.SnapshotSegments) != 0 || meta.SnapshotOverlay || meta.SnapshotLineage != "" ||
		meta.SnapshotImportSource != "" || meta.SnapshotSourceKey || meta.RekeyTo != "" ||
		!unmanagedSourceKey(meta.prefix()) || meta.index() == "" ||
		strings.HasPrefix(meta.index(), "adbc_snapshot") ||
		len(meta.UnmanagedPrefixes) == 0 || !slices.Contains(meta.UnmanagedPrefixes, meta.prefix()) ||
		meta.UnmanagedForeign != (meta.Schema == foreignSchema) {
		return errorf(adbc.StatusIO, "invalid unmanaged table descriptor")
	}
	for _, prefix := range meta.UnmanagedPrefixes {
		if !unmanagedSourceKey(prefix) || prefixesOverlap(prefix, metaPrefix) {
			return errorf(adbc.StatusInvalidArgument, "unmanaged source overlaps driver storage")
		}
	}
	return nil
}

// Check the registered source definition each time. There is deliberately no
// immutable index proof cache for application-owned indexes or row values.
func (s *store) checkUnmanagedIndex(ctx context.Context, meta *tableMeta) error {
	if err := validateUnmanagedDescriptor(meta); err != nil {
		return err
	}
	reply, err := s.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
	if err != nil {
		return wrapRedis(err, "unmanaged source index is unavailable")
	}
	info := indexInfoOf(reply)
	if info.keyType != "HASH" || !slices.Equal(info.prefixes, meta.UnmanagedPrefixes) || info.filter != meta.UnmanagedFilter {
		return errorf(adbc.StatusInvalidState, "unmanaged source index definition changed; unregister and register the table again")
	}
	if info.indexing || info.percent < 1 || info.failures != 0 {
		return errorf(adbc.StatusInvalidState, "unmanaged source index is not ready or has indexing failures")
	}
	return nil
}

func (s *store) loadUnmanagedIndex(ctx context.Context, meta *tableMeta) error {
	if err := s.checkUnmanagedIndex(ctx, meta); err != nil {
		return err
	}
	// The index supplies membership; SQL evaluates predicates and aggregates on
	// authoritative HASH values. Mutable source attributes have no immutable
	// value/NaN/tag proofs suitable for managed pushdown shortcuts.
	for i := range meta.Columns {
		meta.Columns[i].Indexed, meta.Columns[i].App = false, nil
		meta.Columns[i].TagsChecked = true
		meta.Columns[i].TagValues = tagsTruncated
		meta.Columns[i].NaNs = true
	}
	return nil
}

// Register metadata atomically, with no owned row/index edges. Re-registration
// refreshes the schema only; it never converts a managed table or copies rows.
func (s *store) registerUnmanaged(ctx context.Context, meta *tableMeta) error {
	return s.registerUnmanagedMode(ctx, meta, false)
}

func (s *store) registerUnmanagedMode(ctx context.Context, meta *tableMeta, createOnly bool) error {
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	if err := s.initializeSnapshotImport(ctx); err != nil {
		return err
	}
	sctx, pin, err := s.acquireSnapshot(ctx, true)
	if err != nil {
		return err
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(sctx, pin)
	if err != nil {
		return err
	}
	defer s.abortSnapshot(pin, id)
	if meta.Schema != foreignSchema {
		if err := s.checkWritableSchema(meta.Schema); err != nil {
			return err
		}
	}
	if isTempSchema(meta.Schema) || isTempAlias(meta.Schema) {
		return unmanagedUnsupported("temporary registration")
	}
	if exists, err := s.tableExists(wctx, meta.Schema, meta.Name); err != nil {
		return err
	} else if exists {
		if createOnly {
			return stateErrorf(stateDuplicateTable, "table %q already exists", meta.Name)
		}
		old, err := s.getTableMetadata(wctx, meta.Schema, meta.Name)
		if err != nil {
			return err
		}
		if !old.Unmanaged || old.index() != meta.index() || old.prefix() != meta.prefix() || old.UnmanagedForeign != meta.UnmanagedForeign {
			return errorf(adbc.StatusInvalidState, "destination ownership mode or source differs; choose another table or explicitly DROP its registration first")
		}
		meta.SnapshotAccessID, meta.OID, meta.IndexOID = old.SnapshotAccessID, old.OID, old.IndexOID
	} else {
		for _, key := range []string{viewKey(meta.Schema, meta.Name), sequenceKey(meta.Schema, meta.Name), sqlIndexKey(meta.Schema, meta.Name)} {
			if n, err := s.redis(wctx).Exists(wctx, key).Result(); err != nil {
				return err
			} else if n != 0 {
				return stateErrorf(stateDuplicateTable, "destination name is already used")
			}
		}
		meta.SnapshotAccessID = snapshotID()
	}
	if err := s.loadUnmanagedIndex(wctx, meta); err != nil {
		return err
	}
	if s.flexEnabled() {
		// Verify the exact singleton membership command before advertising a
		// registration. This reads index membership without changing the source.
		if _, err := s.flexUnmanagedMembers(wctx, meta, []string{meta.prefix()}); err != nil {
			return err
		}
	}
	if err := s.assignCatalogOIDs(wctx, meta); err != nil {
		return err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	p := s.redis(wctx).Pipeline()
	p.Set(wctx, metaKey(meta.Schema, meta.Name), raw, 0)
	p.SAdd(wctx, tablesKey(meta.Schema), meta.Name)
	p.SAdd(wctx, schemasKey, meta.Schema)
	if _, err := p.Exec(wctx); err != nil {
		return err
	}
	return s.publishSnapshot(wctx, pin, id)
}

func (a *adopter) unmanagedAdopt(ctx context.Context) (*HashAdoption, error) {
	if a.o.operation != nil {
		return nil, unmanagedUnsupported("retained operation preparation")
	}
	if a.o.OwnIndex || a.o.IndexColumns != nil || a.o.TrustStrings {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged adoption uses an existing application index; own-index, index-columns and trust-strings are unsupported")
	}
	if !unmanagedSourceKey(a.o.Prefix) || prefixesOverlap(a.o.Prefix, metaPrefix) {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged source overlaps driver storage")
	}
	if a.c.st.flexEnabled() {
		probe := context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
		if err := a.c.st.configureSearch(probe, true); err != nil {
			return nil, err
		}
	}
	a.c.server(ctx, &a.res.Report.Server)
	a.schema, a.table = a.o.Schema, a.o.Table
	if a.schema == "" {
		a.schema = a.c.db.schema
	}
	if a.table == "" {
		a.table = TableNameForPrefix(a.o.Prefix)
	}
	if err := a.read(ctx); err != nil {
		return nil, err
	}
	indexes, _, err := a.c.searchIndexes(ctx)
	if err != nil {
		return nil, err
	}
	choice := chooseAppIndex(indexes, a.cols, a.o.Prefix, a.stats.keys, a.o.Index, false)
	cl := Checklist{Name: ChecklistSQLInPlace, Title: "SQL, unmanaged live HASHes"}
	if a.c.st.flexEnabled() {
		cl.add(CheckOK, "plain-index Flex probe passed; source reads also require SCAN and FT.SEARCH INKEYS", "")
	} else {
		a.c.checkServer(&cl, a.res.Report)
	}
	if a.c.st.snapshots {
		if exists, err := a.c.st.tableExists(ctx, a.schema, a.table); err != nil {
			return nil, err
		} else if exists {
			old, err := a.c.st.getTableMetadata(ctx, a.schema, a.table)
			if err != nil {
				return nil, err
			}
			if !old.Unmanaged || choice.index == nil || old.index() != choice.index.name || old.prefix() != a.o.Prefix {
				cl.add(CheckBlocker, "destination ownership mode or source differs", "choose another table or explicitly DROP its registration first")
			}
		}
	}
	if choice.index == nil || choice.blocker != "" {
		cl.add(CheckBlocker, "a compatible application-owned HASH Search index is required", "create an index on exactly this prefix without a FILTER; select it with -index")
	}
	cl.add(CheckWarn, "SQL writes change source HASHes; reads and multi-row writes have no statement snapshot or rollback", "INSERT requires __key; application writers and expiry remain independent")
	command := adoptCommand(a.o.Prefix, a.schema, a.table, a.c.db.schema, a.o.Types, a.o.Renames)
	for _, name := range sortedKeys(a.o.Encodings) {
		command += " -encoding " + shellQuote(name+"="+a.o.Encodings[name])
	}
	if choice.index != nil {
		command += " -index " + shellQuote(choice.index.name)
	}
	cl.finish(command + " -mode unmanaged -apply")
	a.res.Report.Schema, a.res.Report.Table = a.schema, a.table
	a.res.Table = displaySchema(a.schema) + "." + a.table
	a.res.Report.Checks = []Checklist{cl}
	steps := []string{"register live source HASHes and their application index without copying or taking ownership; SQL INSERT/UPDATE/DELETE write source HASHes; DROP removes only the registration"}
	a.res.Steps = steps
	if !cl.Ready {
		if a.o.Apply {
			return a.res, errorf(adbc.StatusInvalidState, "unmanaged adoption is blocked; inspect the report checklist")
		}
		return a.res, nil
	}
	a.app, a.index = choice.index, choice.index.name
	meta, err := a.plan()
	if err != nil {
		return a.res, err
	}
	meta.Unmanaged, meta.FoldedIdentifiers = true, true
	meta.UnmanagedPrefixes = slices.Clone(a.app.prefixes)
	meta.UnmanagedFilter = a.app.filter
	a.res.Steps = steps
	a.res.Index, a.res.AppIndex = a.index, true
	if !a.o.Apply {
		return a.res, nil
	}
	if err := a.c.st.registerUnmanaged(ctx, meta); err != nil {
		return a.res, err
	}
	a.res.Applied = true
	return a.res, nil
}

// Only the top-level unmanaged target is writable. Nested unmanaged writes
// cannot escape into a managed workspace or acquire transactional semantics.
type unmanagedWriteKey struct{}

func unmanagedWriteAllowed(ctx context.Context, meta *tableMeta) bool {
	target, _ := ctx.Value(unmanagedWriteKey{}).(string)
	return target == metaKey(meta.Schema, meta.Name)
}

func unmanagedRowContext(ctx context.Context, meta *tableMeta, keys []string) (context.Context, error) {
	if !meta.Unmanaged {
		return ctx, nil
	}
	if !unmanagedWriteAllowed(ctx, meta) {
		return nil, unmanagedUnsupported("nested or staged writes")
	}
	for _, key := range keys {
		if !unmanagedSourceKey(key) {
			return nil, errorf(adbc.StatusInvalidArgument, "unmanaged row overlaps driver storage")
		}
		member := false
		for _, prefix := range meta.UnmanagedPrefixes {
			member = member || strings.HasPrefix(key, prefix)
		}
		if !member {
			return nil, errorf(adbc.StatusInvalidArgument, "unmanaged row is outside the registered source prefixes")
		}
	}
	if scope := snapshotOf(ctx); scope == nil || scope.pin == nil {
		return nil, errorf(adbc.StatusInvalidState, "unmanaged writes require a catalog pin")
	} else if err := scope.pin.valid(ctx); err != nil {
		return nil, err
	}
	// Only row mutation commands lose the metadata scope. Source keys may occupy
	// arbitrary cluster slots and must never enter the managed {meta} Lua fence.
	return context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil)), nil
}

func (s *store) updateUnmanagedRows(ctx context.Context, meta *tableMeta, changes []rowChange) error {
	keys := make([]string, len(changes))
	for i, change := range changes {
		keys[i] = change.key
	}
	if _, err := unmanagedRowContext(ctx, meta, keys); err != nil {
		return err
	}
	if err := s.checkKeyRowsKept(ctx, meta, changes); err != nil {
		return err
	}
	for _, change := range changes {
		rowCtx, err := unmanagedRowContext(ctx, meta, []string{change.key})
		if err != nil {
			return err
		}
		args := []any{"EVAL", keyRowUpdateLua, 1, change.key}
		args = append(args, keyRowArgs(change)...)
		n, err := once(rowCtx, s.client, args...).Int()
		if err != nil {
			return wrapRedis(err, "unmanaged UPDATE failed; source writes may be partially applied")
		}
		if n == -1 {
			return errEmptiesRow(meta, change.key)
		}
	}
	return s.checkKeys(ctx, meta, true)
}

const unmanagedDeleteLua = `
if redis.call('TYPE', KEYS[1]).ok ~= 'hash' then return 0 end
return redis.call('DEL', KEYS[1])
`

func (s *store) deleteUnmanagedRows(ctx context.Context, meta *tableMeta, keys []string) error {
	if _, err := unmanagedRowContext(ctx, meta, keys); err != nil {
		return err
	}
	for _, key := range keys {
		rowCtx, err := unmanagedRowContext(ctx, meta, []string{key})
		if err != nil {
			return err
		}
		if err := once(rowCtx, s.client, "EVAL", unmanagedDeleteLua, 1, key).Err(); err != nil {
			return wrapRedis(err, "unmanaged DELETE failed; source writes may be partially applied")
		}
	}
	return s.checkKeys(ctx, meta, true)
}
