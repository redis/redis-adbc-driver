// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"strings"
)

// resolveWholeRow is the fallback after column lookup: columns take precedence
// over an equally named relation. Qualified stars in value positions explicitly
// request the whole relation. The expansion reads public columns only.
func (e *executor) resolveWholeRow(c *ColumnRef) (bool, error) {
	if c.Qualifier != "" && c.Name != "*" {
		return false, nil
	}
	name := c.Name
	if c.Name == "*" {
		name = c.Qualifier
	}
	q := qualName{name: name, schema: c.Schema, catalog: c.Catalog}
	if err := q.checkCatalog("*"); err != nil {
		return true, err
	}
	for level := len(e.scopes) - 1; level >= 0; level-- {
		var match *relation
		for i := range e.scopes[level].rels {
			rel := &e.scopes[level].rels[i]
			if e.names(q, *rel) {
				if match != nil {
					return true, stateErrorf("42P09", "table reference %q is ambiguous", name)
				}
				match = rel
			}
		}
		if match == nil {
			continue
		}
		row := &RowExpr{up: len(e.scopes) - 1 - level, presence: match.presence}
		fields := make([]structField, len(match.meta.Columns))
		for i, col := range match.meta.Columns {
			key := match.columnKey(col.Name)
			ref := &ColumnRef{Name: key, Outer: row.up, OuterType: col.Type}
			// These are canonical, already resolved keys. Bind the enclosing written
			// reference again on each execution, rather than rebinding these keys.
			row.Items = append(row.Items, ref)
			fields[i] = structField{Name: col.outputName(), Type: col.Type, Nullable: true}
			e.noteRead(match.meta, col.Name)
			if row.up > 0 {
				e.scopes[level].needs[key] = true
				e.correlate(key, row.up)
			}
		}
		if row.up > 0 && row.presence != "" {
			e.scopes[level].needs[row.presence] = true
			e.correlate(row.presence, row.up)
		}
		row.typ = queryRecordOf(fields)
		c.whole = row
		return true, nil
	}
	if c.Name == "*" {
		return true, e.missingFromEntry(q)
	}
	return false, nil
}

// Unlike Arrow-ingested named structs, query records may have duplicate field
// names and are never accepted as stored SQL column types.
func queryRecordOf(fields []structField) ColType {
	var key strings.Builder
	key.WriteString("query:")
	types := make([]ColType, len(fields))
	for i, f := range fields {
		fmt.Fprintf(&key, "%d:%s;", len(f.Name), f.Name)
		types[i] = f.Type
	}
	key.WriteString(recordSignature(types))
	r, _ := recordTypes.LoadOrStore(key.String(), &recordType{query: true, fields: types, named: append([]structField{}, fields...)})
	return ColType{Kind: KindRecord, Rec: r.(*recordType)}
}

// ROW(t.*) expands fields, while a function argument t.* is one composite.
func (e *executor) expandRowStars(row *RowExpr) error {
	var items []Expr
	written := row.Items
	if row.writtenItems != nil {
		written = row.writtenItems
	}
	for _, it := range written {
		ref, ok := it.(*ColumnRef)
		if !ok || ref.Name != "*" {
			items = append(items, it)
			continue
		}
		if row.writtenItems == nil {
			row.writtenItems = append([]Expr(nil), written...)
		}
		if err := e.resolveColumn(ref); err != nil {
			return err
		}
		if ref.whole == nil {
			return fmt.Errorf("qualified star did not resolve to a relation")
		}
		// Positional extraction preserves duplicate public field names.
		for i := range ref.whole.Items {
			items = append(items, &FieldSelect{X: ref, Index: i})
		}
	}
	row.Items = items
	return nil
}
