// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type RefreshMatViewStmt struct {
	Name         TableName
	NoData       bool
	Concurrently bool
}
type DropMatViewStmt struct {
	Name     TableName
	IfExists bool
}
type RenameMatViewStmt struct {
	Name, To TableName
	IfExists bool
}

func (*RefreshMatViewStmt) stmtNode() {}
func (*DropMatViewStmt) stmtNode()    {}
func (*RenameMatViewStmt) stmtNode()  {}

func (p *parser) parseRefreshMatView() (Stmt, error) {
	if err := p.expectKeyword("REFRESH", "MATERIALIZED", "VIEW"); err != nil {
		return nil, err
	}
	st := &RefreshMatViewStmt{Concurrently: p.acceptKeyword("CONCURRENTLY")}
	var err error
	if st.Name, err = p.parseTableName(); err != nil {
		return nil, err
	}
	st.NoData, err = p.parseWithData()
	return st, err
}
func (p *parser) parseWithData() (bool, error) {
	if !p.acceptKeyword("WITH") {
		return false, nil
	}
	no := p.acceptKeyword("NO")
	return no, p.expectKeyword("DATA")
}
func (p *parser) parseDropMatView() (Stmt, error) {
	if err := p.expectKeyword("VIEW"); err != nil {
		return nil, err
	}
	st := &DropMatViewStmt{IfExists: p.acceptKeyword("IF", "EXISTS")}
	var err error
	st.Name, err = p.parseTableName()
	p.parseDropBehavior()
	return st, err
}
func (p *parser) parseRenameMatView() (Stmt, error) {
	if err := p.expectKeyword("VIEW"); err != nil {
		return nil, err
	}
	st := &RenameMatViewStmt{IfExists: p.acceptKeyword("IF", "EXISTS")}
	var err error
	if st.Name, err = p.parseTableName(); err != nil {
		return nil, err
	}
	if err = p.expectKeyword("RENAME", "TO"); err != nil {
		return nil, err
	}
	st.To, err = p.parseTableName()
	return st, err
}
func matViewWriteError(m *tableMeta) error {
	return stateErrorf("42809", "cannot change materialized view %q", m.Name)
}
func (e *executor) matView(ctx context.Context, t TableName) (*tableMeta, error) {
	schema, name, err := e.resolveTable(t)
	if err != nil {
		return nil, err
	}
	m, err := e.store.getTable(ctx, schema, name)
	if err != nil {
		return nil, err
	}
	if m.MaterializedSQL == "" {
		return nil, stateErrorf("42809", "%q is not a materialized view", name)
	}
	return m, nil
}

func (e *executor) runCreateMatView(ctx context.Context, st *CreateViewStmt) (int64, error) {
	if st.OrReplace || st.Temporary {
		return 0, stateErrorf(stateFeatureNotSupported, "OR REPLACE and TEMP materialized views are not supported")
	}
	schema, name, err := e.resolveCreate(ctx, st.Name, false)
	if err != nil {
		return 0, err
	}
	if exists, err := e.store.tableExists(ctx, schema, name); err != nil {
		return 0, err
	} else if exists && st.IfNotExists {
		return 0, nil
	}
	// Bind the definition in its schema, as for permanent ordinary views.
	saved, noTemp := e.schema, e.noTemp
	e.schema, e.noTemp = schema, true
	defer func() { e.schema, e.noTemp = saved, noTemp }()
	var plan *selectPlan
	popPlan := e.planningOnly(true)
	text, err := e.foldView(st.Text, st.Select, func(sel *SelectStmt) error { var err error; plan, err = e.planSelect(ctx, sel, nil); return err })
	popPlan()
	if err != nil {
		return 0, err
	}
	if err = checkStorableColumns(plan.columns()); err != nil {
		return 0, err
	}
	m := &tableMeta{Schema: schema, Name: name, MaterializedSQL: text, Unpopulated: true}
	for i, c := range plan.columns() {
		n := c.Name
		if st.Columns != nil {
			if len(st.Columns) != len(plan.columns()) {
				return 0, stateErrorf(stateInvalidParameter, "wrong number of materialized view column names")
			}
			n = st.Columns[i]
		}
		typ := c.Type
		if typ.Kind == KindNull {
			typ = typeString
		}
		m.Columns = append(m.Columns, columnMeta{Name: n, Type: typ, Nullable: true})
	}
	if err = m.applyIndexPolicy(nil, nil); err != nil {
		return 0, err
	}
	created, err := e.store.createTable(ctx, m, st.IfNotExists)
	if err != nil || !created {
		return 0, err
	}
	if st.NoData {
		return 0, nil
	}
	n, err := e.refreshMatView(ctx, m, false)
	// Failed population leaves an unpopulated object that can be refreshed;
	// do not drop a name another connection could already have replaced.
	return n, err
}

// Build under a fresh registered prefix, then publish that generation in one
// metadata transaction. A failed build leaves the previous generation in place.
func (e *executor) refreshMatView(ctx context.Context, m *tableMeta, noData bool) (int64, error) {
	parsed, err := ParseScript(m.MaterializedSQL)
	if err != nil {
		return 0, err
	}
	sel, ok := parsed[0].Stmt.(*SelectStmt)
	if !ok {
		return 0, stateErrorf(stateInvalidParameter, "invalid materialized view definition")
	}
	oldSchema, noTemp := e.schema, e.noTemp
	e.schema, e.noTemp = m.Schema, true
	defer func() { e.schema, e.noTemp = oldSchema, noTemp }()
	plan, err := e.planSelect(ctx, sel, nil)
	if err != nil {
		return 0, err
	}
	cols := plan.columns()
	if len(cols) != len(m.Columns) {
		return 0, stateErrorf(stateDatatypeMismatch, "materialized view query changed its column count")
	}
	var rows [][]Value
	if !noData {
		rows, err = e.runSelect(ctx, plan, nil)
		if err != nil {
			return 0, err
		}
	}
	token, err := randomUUID()
	if err != nil {
		return 0, err
	}
	stage := cloneMeta(m)
	stage.Name = "__adbc_mv_" + strings.ReplaceAll(token.S, "-", "")
	stage.KeyPrefix, stage.IndexName = "", ""
	stage.OID, stage.IndexOID = 0, 0
	stage.NamedIndexes = nil
	stage.SnapshotSegments, stage.SnapshotRows, stage.SnapshotOverlay = nil, nil, false
	stage.SnapshotLineage = ""
	stage.MaterializedSQL = ""
	stage.Unpopulated = false
	for i := range stage.Columns {
		stage.Columns[i].Missing = ""
		stage.Columns[i].MissingThrough = 0
	}
	if _, err = e.store.createTable(ctx, stage, false); err != nil {
		return 0, err
	}
	published := false
	defer func() {
		if !published {
			_ = e.store.dropTable(context.WithoutCancel(ctx), stage.Schema, stage.Name, true)
		}
	}()
	for _, row := range rows {
		for j, v := range row {
			cv, err := coerceIn(v, m.Columns[j].Type, e.zone())
			if err != nil {
				return 0, err
			}
			row[j] = cv
		}
	}
	n, err := e.store.insertRows(ctx, stage, rows)
	if err != nil {
		return 0, err
	}
	key := metaKey(m.Schema, m.Name)
	sent := false
	err = e.store.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
		cur, err := e.store.getTableWith(ctx, tx, m.Schema, m.Name)
		if err != nil {
			return err
		}
		if !cur.sameKeysAs(m) || cur.MaterializedSQL != m.MaterializedSQL {
			return stateErrorf("55000", "materialized view changed during refresh; try again")
		}
		cur.KeyPrefix, cur.IndexName, cur.PrefixGen = stage.KeyPrefix, stage.IndexName, 0
		if e.store.snapshots {
			cur.SnapshotLineage = stage.SnapshotLineage
			cur.SnapshotSegments, cur.SnapshotRows, cur.SnapshotOverlay = stage.SnapshotSegments, stage.SnapshotRows, stage.SnapshotOverlay
		}
		cur.Unpopulated = noData
		for i := range cur.Columns {
			cur.Columns[i].Missing = ""
			cur.Columns[i].MissingThrough = 0
		}
		raw, err := marshalMeta(cur)
		if err != nil {
			return err
		}
		count, err := tx.Get(ctx, seqKey(stage.Schema, stage.Name)).Result()
		if errors.Is(err, goredis.Nil) {
			count = "0"
		} else if err != nil {
			return err
		}
		sent = true
		_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
			p.Set(ctx, key, raw, 0)
			p.Set(ctx, seqKey(m.Schema, m.Name), count, 0)
			p.Del(ctx, metaKey(stage.Schema, stage.Name), seqKey(stage.Schema, stage.Name))
			p.SRem(ctx, tablesKey(stage.Schema), stage.Name)
			if !e.store.snapshots {
				p.SRem(ctx, prefixesKey, m.prefix())
				p.SRem(ctx, indexesKey, m.index())
				p.HIncrBy(ctx, releasedKey, m.prefix(), 1)
			}
			return nil
		})
		return err
	}, key)
	if err != nil {
		if sent && !errors.Is(err, goredis.TxFailedErr) {
			published = true
		}
		return 0, wrapRedis(err, "failed to publish materialized view")
	}
	published = true
	if e.store.snapshots {
		return n, nil
	}
	if err = e.store.searchDo(ctx, m.index(), "FT.DROPINDEX", m.index(), "DD").Err(); err != nil && !isUnknownIndex(err) {
		return n, wrapRedis(err, "view refreshed but deleting old rows failed")
	}
	return n, nil
}

func (e *executor) runRefreshMatView(ctx context.Context, st *RefreshMatViewStmt) (int64, error) {
	m, err := e.matView(ctx, st.Name)
	if err != nil {
		return 0, err
	}
	if st.Concurrently && (st.NoData || m.Unpopulated) {
		return 0, stateErrorf(stateFeatureNotSupported, "CONCURRENTLY requires a populated materialized view and WITH DATA")
	}
	return e.refreshMatView(ctx, m, st.NoData)
}
func (e *executor) runDropMatView(ctx context.Context, st *DropMatViewStmt) error {
	m, err := e.matView(ctx, st.Name)
	if err != nil {
		var ae adbc.Error
		if st.IfExists && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
			return nil
		}
		return err
	}
	return e.store.dropTable(ctx, m.Schema, m.Name, st.IfExists)
}
func (e *executor) runRenameMatView(ctx context.Context, st *RenameMatViewStmt) error {
	m, err := e.matView(ctx, st.Name)
	if err != nil {
		var ae adbc.Error
		if st.IfExists && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
			return nil
		}
		return err
	}
	return e.renameTable(ctx, m, st.To)
}
