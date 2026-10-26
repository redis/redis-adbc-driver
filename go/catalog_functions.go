// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

func catalogFuncType(name string) (ColType, bool) {
	switch name {
	case "FORMAT_TYPE", "PG_GET_EXPR", "PG_GET_CONSTRAINTDEF", "PG_GET_INDEXDEF", "PG_GET_VIEWDEF", "OBJ_DESCRIPTION", "COL_DESCRIPTION", "PG_GET_SERIAL_SEQUENCE", "QUOTE_IDENT":
		return typeString, true
	case "TO_REGCLASS", "TO_REGTYPE", "TO_REGPROC", "TO_REGNAMESPACE":
		return ColType{Kind: KindString, Alias: strings.ToLower(strings.TrimPrefix(name, "TO_"))}, true
	case "PG_TABLE_IS_VISIBLE", "PG_TYPE_IS_VISIBLE", "PG_COLLATION_IS_VISIBLE", "HAS_TABLE_PRIVILEGE", "HAS_SCHEMA_PRIVILEGE", "HAS_COLUMN_PRIVILEGE":
		return typeBool, true
	case "PG_RELATION_SIZE", "PG_TOTAL_RELATION_SIZE":
		return typeInt64, true
	}
	return ColType{}, false
}

func formatCatalogType(id, mod int64) string {
	for _, t := range builtinCatalogTypes() {
		if postgresTypeOID(t) != id {
			continue
		}
		name, _ := postgresTypeNames(t)
		switch t.Kind {
		case KindString:
			if mod >= 4 && !t.Text {
				return fmt.Sprintf("%s(%d)", name, mod-4)
			}
		case KindDecimal:
			if mod >= 4 {
				n := mod - 4
				return fmt.Sprintf("numeric(%d,%d)", n>>16, n&65535)
			}
		case KindTime, KindTimeTZ, KindTimestamp:
			if mod >= 0 {
				if i := strings.Index(name, " "); i >= 0 {
					return fmt.Sprintf("%s(%d)%s", name[:i], mod, name[i:])
				}
				return fmt.Sprintf("%s(%d)", name, mod)
			}
		}
		return name
	}
	switch id {
	case 26:
		return "oid"
	case 18:
		return `"char"`
	case 19:
		return "name"
	case 2205:
		return "regclass"
	case 2206:
		return "regtype"
	}
	return "???"
}

func (env *evalEnv) catalogExecutor() *executor {
	if env.exec != nil {
		return env.exec
	}
	return &executor{schema: defaultSchema, sess: newSession(defaultSchema)}
}
func (env *evalEnv) catalogContext() context.Context {
	if env.ctx != nil {
		return env.ctx
	}
	return context.Background()
}

func (env *evalEnv) castOID(v Value, t ColType) (Value, error) {
	if v.Null {
		return nullValue(t), nil
	}
	if t.Alias == "oid" {
		if v.T.Alias != "" {
			return intValue(t, v.I), nil
		}
		out, err := Coerce(v, ColType{Kind: KindInt64})
		out.T = t
		return out, err
	}
	e := env.catalogExecutor()
	c, err := e.catalog(env.catalogContext())
	if err != nil {
		return Value{}, err
	}
	name, id := v.Text(), v.I
	byID := v.T.Kind.isInteger()
	if !byID {
		if n, err := strconv.ParseInt(name, 10, 64); err == nil {
			id = n
			byID = true
		}
	}
	var rel, field string
	switch t.Alias {
	case "regclass":
		rel, field = "pg_class", "relname"
	case "regtype":
		rel, field = "pg_type", "typname"
	case "regproc":
		rel, field = "pg_proc", "proname"
	case "regnamespace":
		rel, field = "pg_namespace", "nspname"
	}
	schema := ""
	if !byID {
		if toks, err := lex(name); err == nil {
			p := &parser{src: name, toks: toks}
			if q, err := p.parseTableName(); err == nil {
				schema, name = q.Schema, q.Name
			}
		}
	}
	if t.Alias == "regtype" && !byID {
		for _, typ := range builtinCatalogTypes() {
			pg, udt := postgresTypeNames(typ)
			if pg == name {
				name = udt
				break
			}
		}
	}
	for _, r := range c.rows[rel] {
		match := r["oid"].I == id
		if !byID {
			match = r[field].S == name
			if match && t.Alias != "regnamespace" {
				ns := int64(11)
				if t.Alias == "regclass" {
					ns = r["relnamespace"].I
				} else if t.Alias == "regtype" {
					ns = r["typnamespace"].I
				}
				if schema != "" {
					match = ns == c.namespaceOID(schema)
				} else {
					match = ns == 11 || ns == c.namespaceOID(e.schema) || (e.store != nil && ns == c.namespaceOID(e.store.tempSchema()))
				}
			}
		}
		if !match {
			continue
		}
		out := r[field].S
		if t.Alias == "regclass" && r["relnamespace"].I != c.namespaceOID(e.schema) && r["relnamespace"].I != 11 {
			for _, ns := range c.rows["pg_namespace"] {
				if ns["oid"].I == r["relnamespace"].I {
					out = qualifiedName(ns["nspname"].S, out)
				}
			}
		}
		return Value{T: t, I: r["oid"].I, S: out}, nil
	}
	if byID {
		return Value{T: t, I: id, S: strconv.FormatInt(id, 10)}, nil
	}
	if t.Alias == "regclass" {
		return Value{}, stateErrorf(stateUndefinedTable, "relation %q does not exist", v.Text())
	}
	return Value{}, stateErrorf(stateUndefinedObject, "%s %q does not exist", strings.TrimPrefix(t.Alias, "reg"), v.Text())
}

func (env *evalEnv) evalCatalogFunc(f *Func) (Value, bool, error) {
	t, ok := catalogFuncType(f.Name)
	if !ok {
		return Value{}, false, nil
	}
	if err := checkArity(f); err != nil {
		return Value{}, true, err
	}
	a, err := env.evalItems(f.Args)
	if err != nil {
		return Value{}, true, err
	}
	if len(a) > 0 && a[0].Null {
		return nullValue(t), true, nil
	}
	if strings.HasPrefix(f.Name, "TO_REG") {
		v, err := env.castOID(a[0], t)
		if err != nil && (sqlState(err) == stateUndefinedObject || sqlState(err) == stateUndefinedTable) {
			return nullValue(t), true, nil
		}
		return v, true, err
	}
	if f.Name == "FORMAT_TYPE" {
		mod := int64(-1)
		if !a[1].Null {
			mod = a[1].I
		}
		return str(formatCatalogType(a[0].I, mod)), true, nil
	}
	if f.Name == "PG_GET_EXPR" {
		return a[0], true, nil
	}
	if f.Name == "QUOTE_IDENT" {
		return str(quoteSQLName(a[0].S)), true, nil
	}
	if strings.HasPrefix(f.Name, "HAS_") {
		for _, v := range a {
			if v.Null {
				return nullValue(t), true, nil
			}
		}
		return boolValue(true), true, nil
	}
	if f.Name == "PG_GET_SERIAL_SEQUENCE" {
		return nullValue(typeString), true, nil
	}
	e := env.catalogExecutor()
	c, err := e.catalog(env.catalogContext())
	if err != nil {
		return Value{}, true, err
	}
	id := a[0].I
	if (f.Name == "PG_GET_VIEWDEF" || f.Name == "PG_RELATION_SIZE" || f.Name == "PG_TOTAL_RELATION_SIZE") && a[0].T.Kind == KindString && a[0].T.Alias == "" {
		v, err := env.castOID(a[0], ColType{Kind: KindString, Alias: "regclass"})
		if err != nil {
			return Value{}, true, err
		}
		id = v.I
	}
	switch f.Name {
	case "PG_COLLATION_IS_VISIBLE":
		for _, r := range c.rows["pg_collation"] {
			if r["oid"].I == id {
				return boolValue(true), true, nil
			}
		}
		return nullValue(typeBool), true, nil
	case "PG_TYPE_IS_VISIBLE":
		for _, r := range c.rows["pg_type"] {
			if r["oid"].I == id {
				return boolValue(true), true, nil
			}
		}
		return nullValue(typeBool), true, nil
	case "PG_TABLE_IS_VISIBLE":
		for _, r := range c.rows["pg_class"] {
			if r["oid"].I == id {
				return boolValue(r["relnamespace"].I == c.namespaceOID(e.schema) || r["relnamespace"].I == 11 || (e.store != nil && r["relnamespace"].I == c.namespaceOID(e.store.tempSchema()))), true, nil
			}
		}
		return nullValue(typeBool), true, nil
	case "OBJ_DESCRIPTION", "COL_DESCRIPTION":
		sub := int64(0)
		if f.Name == "COL_DESCRIPTION" {
			if a[1].Null {
				return nullValue(t), true, nil
			}
			sub = a[1].I
		}
		for _, r := range c.rows["pg_description"] {
			if r["objoid"].I == id && r["objsubid"].I == sub {
				return r["description"], true, nil
			}
		}
		return nullValue(t), true, nil
	case "PG_GET_VIEWDEF", "PG_GET_INDEXDEF", "PG_GET_CONSTRAINTDEF":
		if def, ok := c.definitions[id]; ok {
			if f.Name == "PG_GET_INDEXDEF" && len(a) > 1 && a[1].I > 0 {
				for _, r := range c.rows["pg_index"] {
					if r["indexrelid"].I == id {
						m := c.tables[r["indrelid"].I]
						n := a[1].I - 1
						if n >= 0 && int(n) < len(r["indkey"].fields()) {
							pos := r["indkey"].fields()[n].I - 1
							return str(quoteSQLName(m.Columns[pos].Name)), true, nil
						}
					}
				}
			}
			return str(def), true, nil
		}
		return nullValue(t), true, nil
	case "PG_RELATION_SIZE", "PG_TOTAL_RELATION_SIZE":
		if e.store == nil {
			return oidValue(0), true, nil
		}
		m := c.tables[id]
		if m == nil {
			for _, r := range c.rows["pg_index"] {
				if r["indexrelid"].I == id {
					m := c.tables[r["indrelid"].I]
					if m != nil {
						n, err := e.store.catalogIndexBytes(env.catalogContext(), m.index())
						return oidValue(n), true, err
					}
				}
			}
			return nullValue(t), true, nil
		}
		rows, err := e.store.aggregate(env.catalogContext(), &aggRequest{index: m.index(), query: "*", rowFilter: m.rowFilter(), load: []string{"__key"}})
		if err != nil {
			return Value{}, true, err
		}
		n := int64(0)
		for start := 0; start < len(rows); start += pipelineChunk {
			pipe := e.store.client.Pipeline()
			var cmds []*goredis.IntCmd
			for _, row := range rows[start:min(start+pipelineChunk, len(rows))] {
				cmds = append(cmds, pipe.MemoryUsage(env.catalogContext(), row["__key"]))
			}
			if _, err := pipe.Exec(env.catalogContext()); err != nil && !errors.Is(err, goredis.Nil) {
				return Value{}, true, wrapRedis(err, "failed to measure relation")
			}
			for _, cmd := range cmds {
				v, err := cmd.Result()
				if err != nil && !errors.Is(err, goredis.Nil) {
					return Value{}, true, wrapRedis(err, "failed to measure relation")
				}
				n += v
			}
		}
		if f.Name == "PG_TOTAL_RELATION_SIZE" {
			size, err := e.store.catalogIndexBytes(env.catalogContext(), m.index())
			if err != nil {
				return Value{}, true, err
			}
			n += size
		}
		return oidValue(n), true, nil
	}
	return nullValue(t), true, nil
}

func (s *store) catalogIndexBytes(ctx context.Context, index string) (int64, error) {
	reply, err := s.searchDo(ctx, index, "FT.INFO", index).Result()
	if err != nil {
		return 0, wrapRedis(err, "failed to measure search index")
	}
	fields, _ := redisReplyFields(reply)
	mb := func(name string) float64 { f, _ := strconv.ParseFloat(fmt.Sprint(fields[name]), 64); return f }
	total := mb("total_index_memory_sz_mb")
	if _, ok := fields["total_index_memory_sz_mb"]; !ok {
		for _, name := range []string{"inverted_sz_mb", "vector_index_sz_mb", "offset_vectors_sz_mb", "doc_table_size_mb", "sortable_values_size_mb", "key_table_size_mb"} {
			total += mb(name)
		}
	}
	return int64(total * 1024 * 1024), nil
}
