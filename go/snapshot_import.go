// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
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
		scan := &keyScan{nodes: []goredis.Cmdable{s.client}, match: globEscape(prefix) + "*"}
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
func (s *store) importSnapshotTable(ctx context.Context, source *tableMeta, schema, name string, replace bool, next func(context.Context) ([]map[string]Value, bool, error)) error {
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
		if err := s.dropTable(wctx, schema, name, false); err != nil {
			return err
		}
	}
	target := &tableMeta{Schema: schema, Name: name, SnapshotImportSource: snapshotImportIdentity(source)}
	for _, col := range source.Columns {
		col.Field, col.Encoding, col.TextFormat = "", "", ""
		col.Missing, col.MissingThrough = "", 0
		col.TagValues, col.TagsChecked, col.NaNs = "", false, false
		target.Columns = append(target.Columns, col)
	}
	if _, err := s.createTable(wctx, target, false); err != nil {
		return err
	}
	for {
		rows, done, err := next(wctx)
		if err != nil {
			return err
		}
		values := make([][]Value, len(rows))
		for i, row := range rows {
			values[i] = make([]Value, len(target.Columns))
			for j, col := range target.Columns {
				v, ok := row[col.Name]
				if !ok {
					v = nullValue(col.Type)
				}
				values[i][j] = v
			}
		}
		if len(values) > 0 {
			if _, err := s.insertRows(wctx, target, values); err != nil {
				return err
			}
		}
		if done {
			break
		}
	}
	return s.publishSnapshot(wctx, pin, id)
}

// RefreshForeignIndexes replaces managed redis_foreign copies atomically per
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
	source := &store{client: s.client, foreignIndexes: true}
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
		for _, prefix := range indexInfoOf(reply).prefixes {
			if prefixesOverlap(prefix, metaPrefix) {
				return errorf(adbc.StatusInvalidArgument, "source index overlaps driver control storage")
			}
		}
		capture := &retainedCapture{store: source, budget: newRetainedBudget(64 << 20), maxRows: 100000, maxKeys: 200000}
		frozen, err := capture.table(sctx, meta)
		if err != nil {
			return err
		}
		if err := s.importSnapshotTable(ctx, meta, foreignSchema, name, true, func(context.Context) ([]map[string]Value, bool, error) { return frozen.mem, true, nil }); err != nil {
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
	if a.o.Index != "" && choice.index == nil {
		return nil, errorf(adbc.StatusInvalidArgument, "source index cannot serve this HASH collection")
	}
	a.app = choice.index
	if a.app != nil {
		a.index = a.app.name
	}
	meta, err := a.plan()
	if err != nil {
		return nil, err
	}
	a.res.ManagedCopy = true
	a.res.AppIndex = false
	a.res.Table = displaySchema(a.schema) + "." + a.table
	a.res.Report.Schema, a.res.Report.Table = a.schema, a.table
	a.res.Steps = []string{"copy observed application HASH rows into a private managed segment; atomically publish; keep source HASHes and indexes unchanged; repeat Apply to refresh"}
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
	seen := map[string]bool{}
	reader := func(ctx context.Context) ([]map[string]Value, bool, error) {
		page, done, err := keys.next(ctx)
		if err != nil {
			return nil, false, err
		}
		eligible := page[:0:0]
		for _, key := range page {
			if !seen[key] && isRowKey(a.o.Prefix, key) {
				seen[key] = true
				eligible = append(eligible, key)
			}
		}
		if len(seen) > 200000 {
			return nil, false, errorf(adbc.StatusInvalidState, "HASH import candidate-key budget exceeded")
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
	if err := a.c.st.importSnapshotTable(ctx, meta, a.schema, a.table, true, reader); err != nil {
		return nil, err
	}
	a.res.Applied = true
	return a.res, nil
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
