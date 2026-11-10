// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"slices"
)

func (r relation) columnKey(name string) string {
	if k, ok := r.keys[name]; ok {
		return k
	}
	return r.prefix + name
}
func (r relation) ownerPrefix() string {
	if r.owner != "" {
		return r.owner
	}
	return r.prefix
}
func (it *joinItem) relations() []relation {
	if it.scopeRels != nil {
		internal := it.rel()
		internal.name, internal.qualifiedOnly = "", true
		return append(slices.Clone(it.scopeRels), internal)
	}
	return []relation{it.rel()}
}
func selectFromItems(items []JoinClause) *SelectStmt {
	first := items[0]
	return &SelectStmt{Items: []SelectItem{{Star: true, Text: "*"}}, From: first.Table,
		FromSelect: first.Select, FromFunc: first.Func, FromAlias: first.Alias, FromColumns: first.Columns,
		FromLateral: first.Lateral, FromGroup: first.Group, Joins: items[1:]}
}

// A grouped table is planned as one join operand. Its own joins are executed
// before the containing join, preserving outer-join association. An alias
// hides all inner namespaces; without one the inner namespaces remain visible.
func (e *executor) addGroupedFrom(ctx context.Context, outer *joinPlan, jc JoinClause) error {
	sel := selectFromItems(jc.Group)
	meta, rels, inner, err := e.planJoin(ctx, sel)
	if err != nil {
		return err
	}
	pending := e.pendingSq
	e.pushScope(rels, "")
	err = e.bindJoin(ctx, sel, inner)
	e.popScope()
	e.pendingSq = pending
	if err != nil {
		return err
	}
	columns := inner.star
	if columns == nil {
		columns = meta.Columns
	}
	base := &tableMeta{Name: jc.Alias, isMem: true, Columns: slices.Clone(meta.Columns)}
	original := slices.Clone(base.Columns)
	name := jc.Alias
	if name != "" {
		base.Columns = slices.Clone(columns)
		original = slices.Clone(columns)
		base, err = renameColumns(base, name, jc.Columns)
		if err != nil {
			return err
		}
	} else {
		name = fmt.Sprintf("\x00group%d", len(outer.items))
	}
	base.group = &groupedRelation{plan: inner, names: map[string]string{}}
	for i, c := range original {
		base.group.names[c.Name] = base.Columns[i].Name
	}
	it, err := outer.add(jc.Kind, relation{name: name, aliased: true, meta: base})
	if err != nil {
		return err
	}
	it.natural, it.comma, it.using, it.usingAlias = jc.Natural, jc.Comma, jc.Using, jc.UsingAlias
	if jc.Alias == "" {
		it.output = slices.Clone(columns)
		for _, rel := range rels {
			if rel.meta.SnapshotSourceKey {
				c := snapshotSourceKeyColumn()
				c.label = keyField
				c.Name = rel.columnKey(keyField)
				c.Field = c.Name
				c.Indexed = false
				base.group.hidden = append(base.group.hidden, c)
			}
			if rel.meta.group != nil {
				for _, hidden := range rel.meta.group.hidden {
					hidden.Name = rel.columnKey(hidden.Name)
					hidden.Field = hidden.Name
					base.group.hidden = append(base.group.hidden, hidden)
				}
			}
		}
		it.scopeRels = slices.Clone(rels)
		for i := range it.scopeRels {
			r := &it.scopeRels[i]
			r.owner = it.prefix
			if r.presence != "" {
				r.presence = it.prefix + r.presence
			}
			if r.keys != nil {
				keys := map[string]string{}
				for n, k := range r.keys {
					keys[n] = it.prefix + k
				}
				r.keys = keys
			}
			r.prefix = it.prefix + r.prefix
		}
	}
	return nil
}

// groupedRelation delays a grouped operand until its containing query executes.
// It retains the original inner keys separately from aliases and outer prefixes.
type groupedRelation struct {
	plan   *joinPlan
	names  map[string]string
	prefix string
	hidden []columnMeta
}

func (e *executor) groupedRows(ctx context.Context, g *groupedRelation, params []Value) ([]map[string]Value, error) {
	need := map[string]bool{}
	for key := range g.names {
		need[key] = true
	}
	for _, c := range g.hidden {
		need[c.Name] = true
	}
	rows, err := e.runJoin(ctx, g.plan, need, params)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]Value, len(rows))
	for i, row := range rows {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		if err := retainedAllocate(ctx, int64(len(row)), 256); err != nil {
			return nil, err
		}
		m := make(map[string]Value, len(row))
		for key, v := range row {
			name, ok := g.names[key]
			if !ok {
				name = key
			}
			m[g.prefix+name] = v
		}
		out[i] = m
	}
	return out, nil
}
