// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
)

// tryLiveDDL runs SQL DDL that targets live schemas or live tables. It runs
// before the managed statement workspace exists: live DDL changes the live
// control namespace and publishes its own catalog changes, so a later no-op
// managed publication can never report failure for a committed change.
func (e *executor) tryLiveDDL(ctx context.Context, ps ParsedStmt) (bool, execResult, error) {
	switch st := ps.Stmt.(type) {
	case *CreateSchemaStmt:
		if !e.store.schemaOwnershipLive {
			if st.Name == liveSchema && e.store.liveTables && e.currentCatalog() == catalogName {
				if st.IfNotExists {
					return true, execResult{affected: -1}, nil
				}
				return true, execResult{}, stateErrorf(stateDuplicateSchema, "schema %q already exists", liveSchema)
			}
			return false, execResult{}, nil
		}
		if err := e.liveDDLAllowed(); err != nil {
			return true, execResult{}, err
		}
		if err := validateLogicalSchema(st.Name); err != nil {
			return true, execResult{}, err
		}
		ctx, cancel := statementOperationContext(ctx, e.sess)
		defer cancel()
		return true, execResult{affected: -1}, e.store.createLiveSchema(ctx, st.Name, st.IfNotExists)
	case *DropSchemaStmt:
		ref, ok, err := e.liveSchemaTarget(ctx, st.Name)
		if !ok || err != nil {
			return err != nil, execResult{}, err
		}
		ctx, cancel := statementOperationContext(ctx, e.sess)
		defer cancel()
		return true, execResult{affected: -1}, e.store.dropLiveSchema(ctx, ref.schema, st.IfExists, st.Cascade)
	case *AlterSchemaStmt:
		ref, ok, err := e.liveSchemaTarget(ctx, st.Name)
		if !ok || err != nil {
			return err != nil, execResult{}, err
		}
		if err := validateLogicalSchema(st.NewName); err != nil {
			return true, execResult{}, err
		}
		ctx, cancel := statementOperationContext(ctx, e.sess)
		defer cancel()
		return true, execResult{affected: -1}, e.store.renameLiveSchema(ctx, ref.schema, st.NewName)
	case *CreateTableStmt:
		return e.tryCreateLiveTable(ctx, st, ps.NumParams)
	case *DropTableStmt:
		return e.tryDropLiveTables(ctx, st)
	case *AlterTableStmt:
		return e.tryAlterLiveTable(ctx, st)
	}
	return false, execResult{}, nil
}

func (e *executor) liveDDLAllowed() error {
	if !e.store.liveTables {
		return errorf(adbc.StatusInvalidState, "%s=live requires %s=true", OptionStringSchemaOwnership, OptionStringLiveTables)
	}
	if e.currentCatalog() != catalogName {
		return errorf(adbc.StatusNotImplemented, "live schemas require the redis catalog")
	}
	return nil
}

// liveSchemaTarget resolves a named live schema for schema-level DDL. The
// built-in redis_live is not a target: it cannot be dropped or renamed.
func (e *executor) liveSchemaTarget(ctx context.Context, name string) (liveRef, bool, error) {
	if !e.store.liveTables || e.currentCatalog() != catalogName || name == liveSchema {
		return liveRef{}, false, nil
	}
	return e.store.liveSchemaRef(ctx, name)
}

// liveTableTarget resolves a table name to a live registry, if it names one.
func (e *executor) liveTableTarget(ctx context.Context, t TableName) (liveRef, string, bool, error) {
	if t.Catalog != "" && t.Catalog != catalogName || t.Catalog == "" && e.currentCatalog() != catalogName {
		return liveRef{}, "", false, nil
	}
	schema, name, err := e.resolveTable(t)
	if err != nil {
		return liveRef{}, "", false, nil // the ordinary path reports resolution errors
	}
	ref, ok, err := e.store.liveRefFor(ctx, schema)
	return ref, name, ok, err
}

// DROP TABLE on live tables unregisters them: the driver-owned index (without
// DD) and registration are removed, while every source HASH, TTL and unknown
// field stays. Live and other targets cannot be mixed in one statement.
func (e *executor) tryDropLiveTables(ctx context.Context, st *DropTableStmt) (bool, execResult, error) {
	if !e.store.liveTables {
		return false, execResult{}, nil
	}
	type target struct {
		ref  liveRef
		name string
	}
	var live []target
	others := 0
	for _, t := range append([]TableName{st.Table}, st.Tables...) {
		ref, name, ok, err := e.liveTableTarget(ctx, t)
		if err != nil {
			return true, execResult{}, err
		}
		if ok {
			live = append(live, target{ref, name})
		} else {
			others++
		}
	}
	if len(live) == 0 {
		return false, execResult{}, nil
	}
	if others > 0 {
		return true, execResult{}, stateErrorf(stateFeatureNotSupported, "DROP TABLE cannot combine live tables with other tables; drop them in separate statements")
	}
	ctx, cancel := statementOperationContext(ctx, e.sess)
	defer cancel()
	// Preflight every registration before the first removal.
	for _, t := range live {
		_, r, err := e.store.readLiveRecord(ctx, t.ref, t.name)
		if err != nil {
			var ae adbc.Error
			if st.IfExists && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
				continue
			}
			return true, execResult{}, err
		}
		if err := r.unregisterable(); err != nil {
			return true, execResult{}, err
		}
	}
	for _, t := range live {
		if err := e.store.dropLiveTables(ctx, t.ref, []string{t.name}, st.IfExists); err != nil {
			return true, execResult{}, err
		}
	}
	return true, execResult{affected: -1}, nil
}

// Live tables support only ALTER TABLE … RENAME TO, which renames the SQL
// registration. Column and index changes use the explicit evolution workflow.
func (e *executor) tryAlterLiveTable(ctx context.Context, st *AlterTableStmt) (bool, execResult, error) {
	if !e.store.liveTables {
		return false, execResult{}, nil
	}
	ref, name, ok, err := e.liveTableTarget(ctx, st.Table)
	if !ok || err != nil {
		return err != nil, execResult{}, err
	}
	if st.View {
		return true, execResult{}, stateErrorf(stateWrongObjectType, "%q.%q is a live table, not a view", ref.schema, name)
	}
	if len(st.Cmds) != 1 || st.Cmds[0].Action != AlterRenameTable {
		return true, execResult{}, stateErrorf(stateFeatureNotSupported, "live tables support only ALTER TABLE … RENAME TO; use redis-adbc live evolve for column and index changes")
	}
	to := st.Cmds[0].NewTable
	if to.Catalog != "" && to.Catalog != catalogName || to.Schema != "" && to.Schema != ref.schema {
		return true, execResult{}, stateErrorf(stateFeatureNotSupported, "a live table cannot be moved to another schema")
	}
	ctx, cancel := statementOperationContext(ctx, e.sess)
	defer cancel()
	err = e.store.renameLiveTable(ctx, ref, name, to.Name)
	var ae adbc.Error
	if err != nil && st.IfExists && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
		return true, execResult{affected: -1}, nil
	}
	return true, execResult{affected: -1}, err
}

// alterSchemaUnsupported explains why an ALTER SCHEMA reached the managed path.
func (e *executor) alterSchemaUnsupported(ctx context.Context, st *AlterSchemaStmt) error {
	schema := catalogSchema(e.currentCatalog(), st.Name)
	if st.Name == liveSchema && e.store.liveTables && e.currentCatalog() == catalogName {
		return virtualReadOnly(liveSchema)
	}
	if live, err := e.store.isLiveSchemaName(ctx, schema); err != nil {
		return err
	} else if live {
		return errorf(adbc.StatusInvalidState, "schema %q holds live tables; rename it with %s=true", st.Name, OptionStringLiveTables)
	}
	return stateErrorf(stateFeatureNotSupported, "ALTER SCHEMA … RENAME TO is supported only for live schemas")
}
