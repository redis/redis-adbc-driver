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

import "context"

const pgCatalog = "pg_catalog"

func isPgCatalog(schema string) bool { return schema == pgCatalog }

var pgCatalogColumns = map[string][]resultColumn{
	"pg_namespace": {{Name: "nspname", Type: typeString}},
	"pg_class":     {{Name: "relname", Type: typeString}, {Name: "relkind", Type: typeString}},
	"pg_tables":    {{Name: "schemaname", Type: typeString}, {Name: "tablename", Type: typeString}},
	"pg_views":     {{Name: "schemaname", Type: typeString}, {Name: "viewname", Type: typeString}, {Name: "definition", Type: typeString}},
	"pg_matviews":  {{Name: "schemaname", Type: typeString}, {Name: "matviewname", Type: typeString}},
	"pg_indexes":   {{Name: "schemaname", Type: typeString}, {Name: "tablename", Type: typeString}, {Name: "indexname", Type: typeString}},
}

func pgCatalogRelation(name string) bool {
	_, ok := pgCatalogColumns[name]
	return ok
}

// pgCatalogTable builds the small catalog relations that ordinary clients
// use for schema and relation discovery. They are computed from the same
// metadata as information_schema and are read-only.
func (e *executor) pgCatalogTable(ctx context.Context, name string) (*tableMeta, error) {
	cols, ok := pgCatalogColumns[name]
	if !ok {
		return nil, tableNotFound(pgCatalog, name)
	}
	if e.planOnly {
		return memTable(name, cols, nil, nil)
	}
	schemas, err := e.store.listSchemas(ctx)
	if err != nil {
		return nil, err
	}
	var rows [][]Value
	if name == "pg_namespace" {
		for _, schema := range append(schemas, infoSchema, pgCatalog) {
			rows = append(rows, []Value{stringValue(schema)})
		}
		return memTable(name, cols, rows, nil)
	}
	for _, schema := range schemas {
		tables, err := e.store.listTables(ctx, schema)
		if err != nil {
			return nil, err
		}
		views, err := e.store.listViews(ctx, schema)
		if err != nil {
			return nil, err
		}
		switch name {
		case "pg_class":
			for _, table := range tables {
				rows = append(rows, []Value{stringValue(table), stringValue("r")})
			}
			for _, view := range views {
				rows = append(rows, []Value{stringValue(view), stringValue("v")})
			}
		case "pg_tables":
			for _, table := range tables {
				rows = append(rows, []Value{stringValue(schema), stringValue(table)})
			}
		case "pg_views":
			for _, view := range views {
				vm, err := e.store.getView(ctx, schema, view)
				if err == nil {
					rows = append(rows, []Value{stringValue(schema), stringValue(view), stringValue(vm.SQL)})
				}
			}
		case "pg_indexes":
			metas, err := e.store.getTables(ctx, schema, tables)
			if err != nil {
				return nil, err
			}
			for i, table := range tables {
				if metas[i] != nil {
					rows = append(rows, []Value{stringValue(schema), stringValue(table), stringValue(metas[i].index())})
				}
			}
		}
	}
	return memTable(name, cols, rows, nil)
}
