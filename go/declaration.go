// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"fmt"
	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"slices"
	"strings"
)

// declarationMeta records declarations, never integrity enforcement or indexes.
// Stable column fields follow renames but cannot bind replacement columns.
// References that cannot be resolved at declaration time stay unresolved.
type declarationMeta struct {
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	Fields          []string  `json:"fields"`
	Reference       TableName `json:"reference,omitempty"`
	Target          string    `json:"target,omitempty"`
	Self            bool      `json:"self,omitempty"`
	ReferenceFields []string  `json:"reference_fields,omitempty"`
}

func cloneDeclarations(in []declarationMeta) []declarationMeta {
	out := slices.Clone(in)
	for i := range out {
		out[i].Fields = slices.Clone(out[i].Fields)
		out[i].ReferenceFields = slices.Clone(out[i].ReferenceFields)
	}
	return out
}

// Persisted managed OIDs survive logical rename, prefix rekey and rewrites.
// Application descriptors also require their registration epoch because their
// name-derived fallback OID can be reused after unregister/register.
func declarationIdentity(m *tableMeta) string {
	if m.Live {
		return fmt.Sprintf("live:%d:%s:%s", tableOID(m), m.index(), m.LiveVersion)
	}
	if m.Unmanaged {
		return fmt.Sprintf("unmanaged:%d:%s", tableOID(m), m.SnapshotAccessID)
	}
	if m.Foreign {
		return ""
	} // discovered indexes have no durable registration
	return fmt.Sprintf("table:%d", tableOID(m))
}
func (e *executor) declarationReferences(ctx context.Context, source *tableMeta, defs []DeclarationDef) (map[TableName]*tableMeta, error) {
	refs := map[TableName]*tableMeta{}
	for _, d := range defs {
		if d.Kind != "FOREIGN KEY" {
			continue
		}
		if _, ok := refs[d.Reference]; ok {
			continue
		}
		schema, name, err := e.resolveTable(d.Reference)
		if err != nil {
			var ae adbc.Error
			if asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
				refs[d.Reference] = nil
				continue
			}
			return nil, err
		}
		if schema == source.Schema && name == source.Name {
			refs[d.Reference] = source
			continue
		}
		target, err := e.store.getTableMetadata(ctx, schema, name)
		if err != nil {
			var ae adbc.Error
			if asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
				refs[d.Reference] = nil
				continue
			}
			return nil, err
		}
		refs[d.Reference] = target
	}
	return refs, nil
}
func declarationFields(m *tableMeta, cols []string) ([]string, error) {
	out := make([]string, 0, len(cols))
	for _, name := range cols {
		i, ok := m.resolve(name)
		if !ok {
			return nil, columnMissing(name, m.Name)
		}
		f := m.Columns[i].field()
		if slices.Contains(out, f) {
			return nil, stateErrorf(stateDuplicateColumn, "column %q appears twice in constraint", name)
		}
		out = append(out, f)
	}
	return out, nil
}
func addDeclarations(m *tableMeta, defs []DeclarationDef, refs map[TableName]*tableMeta, res *alterResult) error {
	used := map[string]bool{}
	for _, d := range m.Declarations {
		used[d.Name] = true
	}
	for _, d := range m.Checks {
		used[d.Name] = true
	}
	if m.PrimaryKey != nil {
		used[m.PrimaryKey.Name] = true
	}
	for _, d := range defs {
		if d.Name != "" {
			if used[d.Name] {
				return stateErrorf(stateDuplicateObject, "constraint %q for relation %q already exists", d.Name, m.Name)
			}
			used[d.Name] = true
		}
	}
	for _, d := range defs {
		fields, err := declarationFields(m, d.Columns)
		if err != nil {
			return err
		}
		name := d.Name
		if name == "" {
			suffix := "_key"
			if d.Kind == "FOREIGN KEY" {
				suffix = "_fkey"
			}
			base := m.Name + "_" + strings.Join(d.Columns, "_") + suffix
			name = base
			for n := 1; used[name]; n++ {
				name = fmt.Sprintf("%s%d", base, n)
			}
			used[name] = true
		}
		decl := declarationMeta{Name: name, Kind: d.Kind, Fields: fields, Reference: d.Reference}
		if d.Kind == "FOREIGN KEY" {
			if target := refs[d.Reference]; target != nil {
				if target.Schema == m.Schema && target.Name == m.Name {
					target = m
				}
				cols := d.ReferenceColumns
				if len(cols) == 0 {
					cols = target.keyColumns()
				}
				// Preserve historical acceptance of missing keys/columns, but emit reference
				// usage only when every ordered pair can be resolved truthfully.
				refFields, refErr := declarationFields(target, cols)
				if refErr == nil && len(refFields) == len(fields) {
					decl.Self = target == m || target.Schema == m.Schema && target.Name == m.Name
					decl.Target = declarationIdentity(target)
					decl.ReferenceFields = refFields
				}
			}
		}
		m.Declarations = append(m.Declarations, decl)
		if res != nil {
			res.log("declare %s constraint %q fields %q target %q", decl.Kind, decl.Name, decl.Fields, decl.Target)
		}
	}
	return nil
}
func declarationColumnNames(m *tableMeta, fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		i := slices.IndexFunc(m.Columns, func(c columnMeta) bool { return c.field() == f })
		if i < 0 {
			return nil
		}
		out = append(out, m.Columns[i].Name)
	}
	return out
}

// Read the caller's pinned root, so old snapshots retain their old names.
func (s *store) declarationTargets(ctx context.Context, metas []*tableMeta) (map[string]*tableMeta, error) {
	out := map[string]*tableMeta{}
	needed := false
	for _, m := range metas {
		if m == nil {
			continue
		}
		for _, d := range m.Declarations {
			needed = needed || d.Target != "" && !d.Self
		}
	}
	if !needed {
		return out, nil
	}
	schemas, err := s.listSchemas(ctx)
	if err != nil {
		return nil, err
	}
	schemas = append(schemas, s.tempSchema())
	for _, schema := range schemas {
		names, err := s.listTables(ctx, schema)
		if err != nil {
			return nil, err
		}
		tables, err := s.getTables(ctx, schema, names)
		if err != nil {
			return nil, err
		}
		for _, m := range tables {
			if m != nil {
				id := declarationIdentity(m)
				if id != "" {
					out[id] = m
				}
			}
		}
	}
	return out, nil
}
func declarationInfo(m *tableMeta, d declarationMeta, targets map[string]*tableMeta) driverbase.ConstraintInfo {
	name := d.Name
	info := driverbase.ConstraintInfo{ConstraintName: &name, ConstraintType: d.Kind, ConstraintColumnNames: driverbase.RequiredList(declarationColumnNames(m, d.Fields))}
	if d.Kind != "FOREIGN KEY" {
		return info
	}
	target := targets[d.Target]
	if d.Self {
		target = m
	}
	if target == nil || len(d.ReferenceFields) == 0 {
		return info
	}
	cols := declarationColumnNames(target, d.ReferenceFields)
	if len(cols) != len(d.Fields) {
		return info
	}
	catalog, schema := schemaCatalog(target.Schema), displaySchema(target.Schema)
	for _, col := range cols {
		info.ConstraintColumnUsage = append(info.ConstraintColumnUsage, driverbase.ConstraintColumnUsage{ForeignKeyCatalog: &catalog, ForeignKeyDbSchema: &schema, ForeignKeyTable: target.Name, ForeignKeyColumn: col})
	}
	return info
}
