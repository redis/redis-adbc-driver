// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

type snapshotImportKey struct{}

func snapshotImportIdentity(source *tableMeta) string {
	origin := [][]byte{[]byte("hash-prefix"), []byte(source.prefix())}
	if source.Foreign {
		origin = [][]byte{[]byte("foreign-index"), []byte(source.index())}
	}
	raw, _ := json.Marshal(origin)
	return string(raw)
}

func snapshotImporting(ctx context.Context) bool {
	v, _ := ctx.Value(snapshotImportKey{}).(bool)
	return v
}

func (s *store) checkSnapshotImportNamespace(ctx context.Context) error {
	for _, prefix := range []string{metaPrefix, "adbc:schemas", "adbc:meta:", "adbc:seq:", "adbc:tables:"} {
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
				return errorf(adbc.StatusInvalidState, "existing driver metadata cannot be imported or upgraded")
			}
			if done {
				break
			}
		}
	}
	return nil
}

func (s *store) initializeSnapshotImport(ctx context.Context) error {
	if s.snapshots {
		return nil
	}
	return s.openSnapshotCatalog(ctx, true, true)
}

// External scans freeze the rows observed during refresh; concurrent application
// writes do not share a transaction with capture. Once published, the managed
// copy has exactly the same immutable storage and atomic reads as any SQL table.
func (s *store) importSnapshotTable(ctx context.Context, source *tableMeta, schema, name string, replace bool, only map[string]bool, next func(context.Context) ([]map[string]Value, bool, error)) error {
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
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
	wctx = context.WithValue(wctx, snapshotImportKey{}, true)
	access := ""
	var oid int64
	if exists, err := s.tableExists(wctx, schema, name); err != nil {
		return err
	} else if exists {
		old, err := s.getTable(wctx, schema, name)
		if err != nil {
			return err
		}
		if replace && old.SnapshotImportSource != snapshotImportIdentity(source) {
			return errorf(adbc.StatusInvalidState, "refresh target is not a managed copy of this source")
		}
		if !replace {
			return stateErrorf(stateDuplicateTable, "table %q.%q already exists", schema, name)
		}
		if old.Unmanaged {
			return errorf(adbc.StatusInvalidState, "cannot replace an unmanaged registration with a managed copy")
		}
		access, oid = old.SnapshotAccessID, old.OID
		if err := s.dropTable(wctx, schema, name, false); err != nil {
			return err
		}
	}
	target := &tableMeta{Schema: schema, Name: name, SnapshotAccessID: access, OID: oid, SnapshotImportSource: snapshotImportIdentity(source), SnapshotSourceKey: source.KeyRows || source.SnapshotSourceKey}
	for _, col := range source.Columns {
		col.Field, col.Encoding, col.TextFormat = "", "", ""
		col.Missing, col.MissingThrough = "", 0
		col.TagValues, col.TagsChecked, col.NaNs = "", false, false
		target.Columns = append(target.Columns, col)
	}
	if err := target.applyIndexPolicy(only, nil); err != nil {
		return err
	}
	if _, err := s.createTable(wctx, target, false); err != nil {
		return err
	}
	spool := newSnapshotImportSpool(wctx, s, target)
	wctx = context.WithValue(wctx, snapshotImportSpoolKey{}, spool)
	if err := spool.begin(wctx); err != nil {
		return err
	}
	for {
		rows, done, err := next(wctx)
		if err != nil {
			return err
		}
		if err := spool.append(wctx, rows); err != nil {
			return err
		}
		if done {
			break
		}
	}
	if err := spool.complete(wctx); err != nil {
		return err
	}
	if request := snapshotOperationRequestOf(wctx); request != nil {
		n, err := spool.ready(wctx)
		if err != nil {
			return err
		}
		return s.prepareSnapshotOperation(wctx, request, "import", n, target)
	}
	if err := spool.apply(wctx); err != nil {
		return err
	}
	return s.publishSnapshot(wctx, pin, id)
}

// RefreshForeignIndexes refreshes redis_foreign managed copies or unmanaged
// schema registrations, according to foreign_indexes_mode, atomically per
// index. It never alters the source HASHes or Search definitions. Enabling
// foreign_indexes imports missing copies; call this API (or the connection
// refresh option) explicitly to observe subsequent application changes.
func RefreshForeignIndexes(ctx context.Context, options map[string]string) error {
	c, err := dialHashes(ctx, options)
	if err != nil {
		return err
	}
	defer c.close(ctx)
	if err := c.st.initializeSnapshotImport(ctx); err != nil {
		return err
	}
	return c.st.refreshSnapshotForeign(ctx, true)
}

const OptionStringForeignIndexesRefresh = "adbc.redis.foreign_indexes_refresh"

func (s *store) refreshSnapshotForeign(ctx context.Context, replace bool) error {
	if s.flexEnabled() && s.foreignIndexesMode != "unmanaged" {
		return flexUnsupported("managed foreign indexes")
	}
	source := &store{client: s.client, foreignIndexes: true, flex: s.flex}
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	// Discovery needs the managed index registry, so pin it even though source
	// index/HASH reads themselves remain outside the managed snapshot contract.
	sctx, pin, err := s.acquireSnapshot(ctx)
	if err != nil {
		return err
	}
	defer pin.release()
	names, err := source.foreignNames(sctx)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, name := range names {
		present[name] = true
		if snapshotDataID(name) != "" {
			continue
		}
		if !replace {
			exists, err := s.tableExists(sctx, foreignSchema, name)
			if err != nil {
				return err
			}
			if exists {
				old, err := s.getTableMetadata(sctx, foreignSchema, name)
				if err != nil {
					return err
				}
				if old.Unmanaged != (s.foreignIndexesMode == "unmanaged") {
					return errorf(adbc.StatusInvalidState, "foreign table ownership mode differs; explicitly DROP its registration before changing modes")
				}
				continue
			}
		}
		meta, err := source.foreignTable(sctx, name)
		if err != nil {
			return err
		}
		reply, err := source.searchDo(sctx, name, "FT.INFO", name).Result()
		if err != nil {
			return err
		}
		info := indexInfoOf(reply)
		for _, prefix := range info.prefixes {
			if prefixesOverlap(prefix, metaPrefix) {
				return errorf(adbc.StatusInvalidArgument, "source index overlaps driver control storage")
			}
		}
		if s.foreignIndexesMode == "unmanaged" {
			meta.Unmanaged, meta.UnmanagedForeign = true, true
			meta.UnmanagedPrefixes, meta.UnmanagedFilter = info.prefixes, info.filter
			if err := s.registerUnmanaged(ctx, meta); err != nil {
				return err
			}
			continue
		}
		reader, err := s.snapshotForeignImportReader(sctx, source, meta)
		if err != nil {
			return err
		}
		if err := s.importSnapshotTable(ctx, meta, foreignSchema, name, true, nil, reader); err != nil {
			return err
		}
	}
	if replace {
		copies, err := s.listTables(sctx, foreignSchema)
		if err != nil {
			return err
		}
		for _, name := range copies {
			if !present[name] {
				if err := s.removeSnapshotForeign(ctx, name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// snapshotAdopt imports application-owned rows instead of registering a mutable
// external layout in a managed manifest. Repeating Apply explicitly refreshes.
func (a *adopter) snapshotAdopt(ctx context.Context) (*HashAdoption, error) {
	a.c.server(ctx, &a.res.Report.Server)
	if strings.HasPrefix(a.o.Prefix, metaPrefix) || strings.HasPrefix(metaPrefix, a.o.Prefix) {
		return nil, errorf(adbc.StatusInvalidArgument, "source prefix overlaps driver control storage")
	}
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
	var indexes []searchIndex
	if !a.o.OwnIndex {
		var err error
		indexes, _, err = a.c.searchIndexes(ctx)
		if err != nil {
			return nil, err
		}
	}
	choice := chooseAppIndex(indexes, a.cols, a.o.Prefix, a.stats.keys, a.o.Index, a.o.OwnIndex)
	if choice.index != nil {
		a.index = choice.index.name
		a.res.SourceIndex = choice.index.name
	}
	meta := &tableMeta{KeyPrefix: a.o.Prefix}
	if len(a.cols) > 0 {
		var err error
		meta, err = a.plan()
		if err != nil {
			return nil, err
		}
	}
	// Source membership/decoding and the private native index have independent
	// policies. Do not apply legacy in-place index restrictions to this plan.
	a.app = choice.index
	meta.AppIndex = a.app != nil
	var only map[string]bool
	if a.o.IndexColumns != nil {
		only = map[string]bool{}
		for _, name := range a.o.IndexColumns {
			only[name] = true
		}
	}
	a.res.ManagedCopy = true
	a.res.AppIndex = false
	a.res.Table = displaySchema(a.schema) + "." + a.table
	a.res.Report.Schema, a.res.Report.Table = a.schema, a.table
	a.res.Steps = []string{"copy observed application HASH rows into a private managed segment; atomically publish; keep source HASHes and indexes unchanged; repeat Apply to refresh"}
	cl, err := a.snapshotAdoptionChecklist(ctx, meta, choice)
	if err != nil {
		return nil, err
	}
	a.res.Report.Checks = []Checklist{cl}
	if !cl.Ready {
		if a.o.Apply {
			return a.res, errorf(adbc.StatusInvalidState, "managed HASH import is blocked; inspect the report checklist")
		}
		return a.res, nil
	}
	if !a.o.Apply {
		return a.res, nil
	}
	if err := a.c.st.initializeSnapshotImport(ctx); err != nil {
		return nil, err
	}
	keys, err := a.c.scanKeys(ctx, a.o.Prefix, "hash")
	if err != nil {
		return nil, err
	}
	reader := func(ctx context.Context) ([]map[string]Value, bool, error) {
		page, done, err := keys.next(ctx)
		if err != nil {
			return nil, false, err
		}
		eligible := page[:0:0]
		for _, key := range page {
			if isRowKey(a.o.Prefix, key) {
				eligible = append(eligible, key)
			}
		}
		eligible, err = snapshotImportUnseen(ctx, eligible)
		if err != nil {
			return nil, false, err
		}
		if a.app != nil {
			capture := &retainedCapture{store: &store{client: a.c.client}}
			eligible, err = capture.members(ctx, meta, eligible)
			if err != nil {
				return nil, false, err
			}
		}
		hashes, err := a.c.readHashes(ctx, eligible)
		if err != nil {
			return nil, false, err
		}
		rows := make([]map[string]Value, 0, len(hashes))
		for _, hash := range hashes {
			raw := aggRow{"__key": hash.key}
			for i, f := range hash.fields {
				raw[f] = hash.values[i]
			}
			row, err := decodeRow(meta, raw, nil)
			if err != nil {
				return nil, false, err
			}
			rows = append(rows, row)
		}
		return rows, done, nil
	}
	if a.o.operation != nil {
		ctx = context.WithValue(ctx, snapshotOperationRequestKey{}, a.o.operation)
	}
	if err := a.c.st.importSnapshotTable(ctx, meta, a.schema, a.table, true, only, reader); err != nil {
		return a.res, err
	}
	a.res.Applied = a.o.operation == nil
	return a.res, nil
}

// Keep the public adoption checklist identifier, while describing its mandatory
// managed-copy semantics. Planning must never initialize or publish a catalog.
func (a *adopter) snapshotAdoptionChecklist(ctx context.Context, source *tableMeta, choice appIndexChoice) (Checklist, error) {
	cl := Checklist{Name: ChecklistSQLInPlace, Title: "SQL, imported as a managed copy (redis-adbc adopt)"}
	a.c.checkServer(&cl, a.res.Report)
	if choice.blocker != "" {
		cl.add(CheckBlocker, choice.blocker, "choose a compatible source index or capture HASHes directly with -own-index")
	}
	for _, name := range sortedKeys(choice.unusable) {
		cl.add(CheckWarn, fmt.Sprintf("source index %s cannot capture this HASH collection: %s", name, choice.unusable[name]), "")
	}
	if choice.index != nil {
		cl.add(CheckOK, "capture source rows through the application's index "+choice.index.name+"; SQL uses a private managed index", "")
	} else if choice.blocker == "" {
		cl.add(CheckOK, "capture source HASHes directly; SQL uses a private managed index", "")
	}
	switch {
	case a.stats.keys == 0:
		cl.add(CheckBlocker, fmt.Sprintf("there are no HASH keys under %s", shellQuote(a.o.Prefix)), "check the source prefix")
	case a.stats.rows == 0:
		cl.add(CheckBlocker, "no source HASHes qualify as rows", "choose a prefix whose row keys have no further ':'")
	default:
		cl.add(CheckOK, fmt.Sprintf("%s under %s %s rows", plural(a.stats.rows, "HASH", "HASHes"), shellQuote(a.o.Prefix), verb(a.stats.rows, "is", "are")), "")
		if a.stats.nested > 0 {
			cl.add(CheckWarn, fmt.Sprintf("%s (such as %s) %s another ':' after %s, and %s out of the table",
				plural(a.stats.nested, "key", "keys"), a.stats.nestedEx, verb(a.stats.nested, "has", "have"), shellQuote(a.o.Prefix), verb(a.stats.nested, "stays", "stay")), "")
		}
	}
	for _, col := range a.cols {
		if col.override != "" && col.guess != nil && col.guess.misfits > 0 {
			cl.add(CheckBlocker, fmt.Sprintf("-type %s=%s: %s do not fit the source encoding", col.name, col.override, plural(int64(col.guess.misfits), "value", "values")),
				"choose a compatible type or encoding; source values are never changed")
		}
	}
	if len(a.cols) == 0 {
		if int64(a.res.Report.Sampled) < a.stats.rows {
			cl.add(CheckBlocker, "the inspection sample has no fields from qualifying source rows", "use check -sample -1 to inspect all source rows")
		} else {
			cl.add(CheckBlocker, "the source has no fields that can become columns", "choose a collection with readable fields")
		}
	}
	checkColumnNames(&cl, a.cols)
	if isTempSchema(a.schema) || isTempAlias(a.schema) {
		cl.add(CheckBlocker, "the import destination is a temporary schema", "choose a permanent schema")
	}
	if a.c.st.snapshots {
		schema, err := a.c.st.schemaExists(ctx, a.schema)
		if err != nil {
			return cl, err
		}
		if !schema {
			cl.add(CheckBlocker, "the destination schema does not exist", "create the schema before importing")
		}
		view, err := a.c.st.viewExists(ctx, a.schema, a.table)
		if err != nil {
			return cl, err
		}
		table, err := a.c.st.tableExists(ctx, a.schema, a.table)
		if err != nil {
			return cl, err
		}
		switch {
		case view:
			cl.add(CheckBlocker, a.res.Table+" is a view", "choose another destination")
		case table:
			old, err := a.c.st.getTable(ctx, a.schema, a.table)
			if err != nil {
				return cl, err
			}
			if old.SnapshotImportSource != snapshotImportIdentity(source) {
				cl.add(CheckBlocker, a.res.Table+" is not a managed copy of this source", "choose another destination")
			} else {
				cl.add(CheckOK, "repeat Apply explicitly refreshes the existing managed copy", "")
			}
		}
	}
	cl.add(CheckMissing, "a private managed copy and its own Search index", "Apply publishes the observed source rows atomically")
	cl.add(CheckOK, "source HASHes and application indexes remain unchanged", "")
	cl.add(CheckWarn, "capture is a scan of observed data, not an atomic snapshot of application writers", "pause source writes if a point-in-time source image is required")
	command := adoptCommand(a.o.Prefix, a.schema, a.table, a.c.db.schema, a.o.Types, a.o.Renames)
	for _, name := range sortedKeys(a.o.Encodings) {
		command += " -encoding " + shellQuote(name+"="+a.o.Encodings[name])
	}
	if a.o.Index != "" {
		command += " -index " + shellQuote(a.o.Index)
	} else if a.o.OwnIndex {
		command += " -own-index"
	}
	if len(a.o.IndexColumns) > 0 {
		command += " -index-columns " + shellQuote(strings.Join(a.o.IndexColumns, ","))
	}
	if a.o.TrustStrings {
		command += " -trust-strings"
	}
	cl.finish(command + " -apply")
	return cl, nil
}

func (s *store) removeSnapshotForeign(ctx context.Context, name string) error {
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
	wctx = context.WithValue(wctx, snapshotImportKey{}, true)
	if err := s.dropTable(wctx, foreignSchema, name, true); err != nil {
		return err
	}
	return s.publishSnapshot(wctx, pin, id)
}
