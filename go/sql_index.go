// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func sqlIndexKey(schema, name string) string {
	return metaPrefix + "sql-index:" + tableKeySuffix(schema, name)
}

type CreateIndexStmt struct {
	Name        TableName
	Table       TableName
	Columns     []string
	IfNotExists bool
}
type DropIndexStmt struct {
	Names    []TableName
	IfExists bool
}

func (*CreateIndexStmt) stmtNode() {}
func (*DropIndexStmt) stmtNode()   {}

type namedIndex struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

func (p *parser) parseCreateIndex() (Stmt, error) {
	if p.acceptKeyword("UNIQUE") {
		return nil, stateErrorf(stateFeatureNotSupported, "UNIQUE indexes are not supported")
	}
	if err := p.expectKeyword("INDEX"); err != nil {
		return nil, err
	}
	p.acceptKeyword("CONCURRENTLY")
	st := &CreateIndexStmt{IfNotExists: p.acceptKeyword("IF", "NOT", "EXISTS")}
	var err error
	if st.Name, err = p.parseTableName(); err != nil {
		return nil, err
	}
	if err = p.expectKeyword("ON"); err != nil {
		return nil, err
	}
	if st.Table, err = p.parseTableName(); err != nil {
		return nil, err
	}
	if p.acceptKeyword("USING") {
		method, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		if method != "btree" && method != "hash" {
			return nil, stateErrorf(stateFeatureNotSupported, "index method %q is not supported", method)
		}
	}
	if st.Columns, err = p.parseColumnList(); err != nil {
		if p.isOp("(") || p.isOp("+") || p.isOp("-") || p.isOp("*") {
			return nil, stateErrorf(stateFeatureNotSupported, "expression indexes are not supported")
		}
		return nil, err
	}
	if len(st.Columns) == 0 {
		return nil, syntaxErr("an index needs at least one column")
	}
	if p.acceptKeyword("INCLUDE") {
		if _, err = p.parseColumnList(); err != nil {
			return nil, err
		}
	}
	if p.acceptKeyword("WITH") {
		if err = p.expectOp("("); err != nil {
			return nil, err
		}
		depth := 1
		for depth > 0 {
			t := p.next()
			if t.kind == tokEOF {
				return nil, syntaxErr("unterminated index options")
			}
			if t.text == "(" {
				depth++
			}
			if t.text == ")" {
				depth--
			}
		}
	}
	if p.isKeyword("WHERE") {
		return nil, stateErrorf(stateFeatureNotSupported, "partial indexes are not supported")
	}
	return st, nil
}

func (p *parser) parseDropIndex() (Stmt, error) {
	p.acceptKeyword("CONCURRENTLY")
	st := &DropIndexStmt{IfExists: p.acceptKeyword("IF", "EXISTS")}
	for {
		n, err := p.parseTableName()
		if err != nil {
			return nil, err
		}
		st.Names = append(st.Names, n)
		if !p.acceptOp(",") {
			break
		}
	}
	p.parseDropBehavior()
	return st, nil
}

func (e *executor) findNamedIndex(ctx context.Context, schema, name string) (*tableMeta, error) {
	names, err := e.store.listTables(ctx, schema)
	if err != nil {
		return nil, err
	}
	metas, err := e.store.getTables(ctx, schema, names)
	if err != nil {
		return nil, err
	}
	for _, m := range metas {
		if m != nil && slices.ContainsFunc(m.NamedIndexes, func(i namedIndex) bool { return i.Name == name }) {
			return m, nil
		}
	}
	return nil, nil
}

func (e *executor) runCreateIndex(ctx context.Context, st *CreateIndexStmt) error {
	m, err := e.loadTable(ctx, st.Table)
	if err != nil {
		return err
	}
	if m.AppIndex || m.Foreign {
		return errorf(adbc.StatusNotImplemented, "cannot alter an application-owned index")
	}
	if err = movingErr(m); err != nil {
		return err
	}
	if st.Name.Schema != "" && st.Name.Schema != m.Schema {
		return stateErrorf(stateInvalidParameter, "index must be in the table's schema")
	}
	existing, err := e.findNamedIndex(ctx, m.Schema, st.Name.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		if st.IfNotExists {
			return nil
		}
		return stateErrorf(stateDuplicateTable, "relation %q already exists", st.Name.Name)
	}
	exists, err := e.store.tableExists(ctx, m.Schema, st.Name.Name)
	if err != nil {
		return err
	}
	if exists {
		if st.IfNotExists {
			return nil
		}
		return stateErrorf(stateDuplicateTable, "relation %q already exists", st.Name.Name)
	}
	fields := []string{}
	for _, name := range st.Columns {
		i, ok := m.resolve(name)
		if !ok {
			return stateErrorf(stateUndefinedColumn, "column %q does not exist", name)
		}
		c := m.Columns[i]
		if !indexable(c.Type) || !simpleName(c.field()) {
			return stateErrorf(stateFeatureNotSupported, "column %q cannot be indexed", name)
		}
		fields = append(fields, c.field())
	}
	registryKey := sqlIndexKey(m.Schema, st.Name.Name)
	registered := false
	return e.store.updateTableTx(ctx, m.Schema, m.Name, false, func(cur *tableMeta, _ int64) error {
		registered = false
		n, err := e.store.redis(ctx).Exists(ctx, registryKey, metaKey(m.Schema, st.Name.Name), viewKey(m.Schema, st.Name.Name), sequenceKey(m.Schema, st.Name.Name)).Result()
		if err != nil {
			return err
		}
		if n > 0 {
			if st.IfNotExists {
				return nil
			}
			return stateErrorf(stateDuplicateTable, "relation %q already exists", st.Name.Name)
		}
		if !cur.sameKeysAs(m) {
			return errorf(adbc.StatusIO, "table changed while creating index; try again")
		}
		for _, idx := range cur.NamedIndexes {
			if idx.Name == st.Name.Name {
				if st.IfNotExists {
					return nil
				}
				return stateErrorf(stateDuplicateTable, "relation %q already exists", idx.Name)
			}
		}
		count := 0
		for _, c := range cur.Columns {
			if c.Indexed {
				count++
			}
		}
		for _, field := range fields {
			i := slices.IndexFunc(cur.Columns, func(c columnMeta) bool { return c.field() == field })
			if i < 0 {
				return stateErrorf(stateUndefinedColumn, "indexed column no longer exists")
			}
			c := &cur.Columns[i]
			if c.Indexed {
				continue
			}
			count++
			if count > maxIndexed {
				return stateErrorf(stateFeatureNotSupported, "too many indexed columns")
			}
			args := append([]any{"FT.ALTER", cur.index(), "SCHEMA", "ADD"}, indexAttrArgs(*c)...)
			if err := e.store.searchDo(ctx, cur.index(), args...).Err(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return wrapRedis(err, "failed to add index column")
			}
			c.Indexed = true
		}
		if err := e.store.awaitIndexReady(ctx, cur.index()); err != nil {
			return err
		}
		registered = true
		cur.NamedIndexes = append(slices.Clone(cur.NamedIndexes), namedIndex{Name: st.Name.Name, Fields: slices.Clone(fields)})
		return nil
	}, func(p goredis.Pipeliner) {
		if registered {
			p.Set(ctx, registryKey, m.prefix(), 0)
		}
	}, registryKey, metaKey(m.Schema, st.Name.Name), viewKey(m.Schema, st.Name.Name), sequenceKey(m.Schema, st.Name.Name))
}

func (e *executor) runDropIndex(ctx context.Context, st *DropIndexStmt) error {
	for _, name := range st.Names {
		schema, _, err := e.resolveTable(name)
		if err != nil {
			return err
		}
		m, err := e.findNamedIndex(ctx, schema, name.Name)
		if name.Schema == "" && e.store.hasTempObjects() {
			tmp, tmpErr := e.findNamedIndex(ctx, e.store.tempSchema(), name.Name)
			if tmpErr != nil {
				return tmpErr
			}
			if tmp != nil {
				m, schema = tmp, tmp.Schema
			}
		}
		if err != nil {
			return err
		}
		if m == nil {
			if st.IfExists {
				continue
			}
			return stateErrorf(stateUndefinedObject, "index %q does not exist", name.Name)
		}
		if err = e.store.updateTableTx(ctx, m.Schema, m.Name, false, func(cur *tableMeta, _ int64) error {
			cur.NamedIndexes = slices.DeleteFunc(slices.Clone(cur.NamedIndexes), func(i namedIndex) bool { return i.Name == name.Name })
			return nil
		}, func(p goredis.Pipeliner) { p.Del(ctx, sqlIndexKey(schema, name.Name)) }, sqlIndexKey(schema, name.Name)); err != nil {
			return err
		}
	}
	return nil
}
