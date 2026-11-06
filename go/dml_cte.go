// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"maps"
	"reflect"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

func statementWith(st Stmt) []CTE {
	switch s := st.(type) {
	case *SelectStmt:
		return s.With
	case *InsertStmt:
		return s.With
	case *UpdateStmt:
		return s.With
	case *DeleteStmt:
		return s.With
	case *MergeStmt:
		return s.With
	}
	return nil
}

// Capture physical inputs before this statement performs its first write.
// Snapshot catalogs stage all modifying bodies in one private workspace and
// publish only after the outer statement succeeds.
func (e *executor) prepareDMLCTEs(ctx context.Context, st Stmt) (bool, error) {
	with := statementWith(st)
	has := false
	for _, c := range with {
		has = has || c.Statement != nil
	}
	if !has {
		return false, nil
	}
	snapshots := map[string]*tableMeta{}
	seenViews := map[string]bool{}
	var capture func(reflect.Value) error
	capture = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		if v.Type() == reflect.TypeOf(TableName{}) {
			t := v.Interface().(TableName)
			if t.Name == "" {
				return nil
			}
			schema, name, err := e.resolveTable(t)
			if err != nil {
				return err
			}
			if isInfoSchema(schema) || isPgCatalog(schema) {
				return nil
			}
			key := metaKey(schema, name)
			if snapshots[key] != nil {
				return nil
			}
			m, err := e.store.getTable(ctx, schema, name)
			if err != nil {
				var ae adbc.Error
				if !asAdbc(err, &ae) || ae.Code != adbc.StatusNotFound {
					return err
				}
				if seenViews[key] {
					return nil
				}
				seenViews[key] = true
				view, err := e.store.getView(ctx, schema, name)
				if err != nil {
					if asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
						return nil
					}
					return err
				}
				parsed, err := ParseScript(view.SQL)
				if err != nil {
					return err
				}
				old, noTemp := e.schema, e.noTemp
				e.schema, e.noTemp = view.Schema, !isTempSchema(view.Schema)
				if view.DefaultSchema != "" {
					e.schema = view.DefaultSchema
				}
				defer func() { e.schema, e.noTemp = old, noTemp }()
				return capture(reflect.ValueOf(parsed[0].Stmt))
			}
			if m.MaterializedSQL != "" && m.Unpopulated {
				return stateErrorf("55000", "materialized view %q has not been populated", m.Name)
			}
			m, err = e.store.pinMatview(ctx, m)
			if err != nil {
				return err
			}
			need := map[string]bool{rowIDField: true, keyField: true}
			for _, c := range m.Columns {
				need[c.Name] = true
			}
			keys, rows, err := e.scan(ctx, scanRequest{meta: m, where: wherePlan{query: "*"}, need: need}, e.params)
			if err != nil {
				return err
			}
			snap := cloneMeta(m)
			snap.isMem, snap.retainedSource = true, true
			snap.mem = rows
			snap.sourceKeys = keys
			snapshots[key] = snap
			return nil
		}
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if !v.IsNil() {
				return capture(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					if err := capture(v.Field(i)); err != nil {
						return err
					}
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if err := capture(v.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := capture(reflect.ValueOf(st)); err != nil {
		return true, err
	}
	e.dmlSnapshots = snapshots
	pop, err := e.pushCTEs(with)
	if err != nil {
		return true, err
	}
	defer pop()
	// Validate each body's expressions before any writes. Planning cannot run a
	// data-modifying CTE, including through a referenced SELECT CTE.
	for i := range with {
		if with[i].Statement != nil {
			if err := e.describeParams(ctx, with[i].Statement); err != nil {
				return true, err
			}
		}
	}
	if err := e.describeParams(ctx, st); err != nil {
		return true, err
	}
	for i := range with {
		if with[i].Statement != nil {
			def := &with[i]
			if _, ok := e.cache.ctes[def]; !ok {
				m, err := e.runDMLCTE(ctx, def)
				if err != nil {
					return true, err
				}
				e.cache.ctes[def] = m
			}
		}
	}
	e.returning = nil
	return true, nil
}

func (e *executor) runDMLCTE(ctx context.Context, def *CTE) (*tableMeta, error) {
	if def.Recursive {
		return nil, stateErrorf(stateFeatureNotSupported, "data-modifying CTEs cannot be recursive")
	}
	saved := e.returning
	e.returning = nil
	defer func() { e.returning = saved }()
	if e.planOnly {
		cols, _, err := e.resultColumns(ctx, def.Statement, e.paramTypes)
		if err != nil {
			return nil, err
		}
		return memTable(def.Name, cols, nil, def.Columns)
	}
	if e.dmlSnapshots == nil {
		return nil, stateErrorf(stateFeatureNotSupported, "data-modifying WITH must be at the top level")
	}
	if e.dmlRunning == nil {
		e.dmlRunning = map[*CTE]bool{}
	}
	if e.dmlRunning[def] {
		return nil, stateErrorf(stateFeatureNotSupported, "recursive data-modifying WITH is not supported")
	}
	e.dmlRunning[def] = true
	defer delete(e.dmlRunning, def)
	if e.store.snapshots && !e.planOnly {
		var target TableName
		switch st := def.Statement.(type) {
		case *InsertStmt:
			target = st.Table
		case *UpdateStmt:
			target = st.Table
		case *DeleteStmt:
			target = st.Table
		case *MergeStmt:
			target = st.Table
		}
		schema, name, err := e.resolveTable(target)
		if err != nil {
			return nil, err
		}
		if err := e.store.prepareSnapshotTable(ctx, schema, name); err != nil {
			return nil, err
		}
		key := metaKey(schema, name)
		if frozen := e.dmlSnapshots[key]; frozen != nil {
			current, err := e.store.getTable(ctx, schema, name)
			if err != nil {
				return nil, err
			}
			// SQL still reads the statement's frozen values. Target identities
			// point at the private delta, never at its published input HASHes.
			image := *frozen
			image.KeyPrefix, image.IndexName, image.PrefixGen = current.KeyPrefix, current.IndexName, current.PrefixGen
			image.SnapshotSegments, image.SnapshotRows, image.SnapshotOverlay = current.SnapshotSegments, current.SnapshotRows, current.SnapshotOverlay
			image.sourceKeys = make([]string, len(frozen.sourceKeys))
			for i, oldKey := range frozen.sourceKeys {
				id, ok := strings.CutPrefix(oldKey, frozen.prefix())
				if !ok {
					return nil, errorf(adbc.StatusIO, "invalid modifying CTE row identity")
				}
				image.sourceKeys[i] = current.prefix() + id
			}
			e.dmlSnapshots[key] = &image
		}
	}
	var n int64
	var err error
	switch st := def.Statement.(type) {
	case *InsertStmt:
		n, err = e.runInsert(ctx, st, e.params)
	case *UpdateStmt:
		n, err = e.runUpdate(ctx, st, e.params)
	case *DeleteStmt:
		n, err = e.runDelete(ctx, st, e.params)
	case *MergeStmt:
		n, err = e.runMerge(ctx, st, e.params)
	default:
		return nil, stateErrorf(stateFeatureNotSupported, "invalid data-modifying WITH body")
	}
	res, err := e.dmlResult(n, err)
	if err != nil {
		return nil, err
	}
	return memTable(def.Name, res.cols, res.rows, def.Columns)
}

func cloneMemRows(rows []map[string]Value) []map[string]Value {
	out := make([]map[string]Value, len(rows))
	for i, r := range rows {
		out[i] = maps.Clone(r)
	}
	return out
}
