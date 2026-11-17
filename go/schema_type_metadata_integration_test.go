// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

func TestSQLDeclarationMetadataSurfaces(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`CREATE SCHEMA "TypeMetadata453"`)
	defer h.exec(`DROP SCHEMA "TypeMetadata453" CASCADE`)
	h.exec(`CREATE TABLE "TypeMetadata453"."EmptyTable" (amount NUMERIC(12,2), label VARCHAR(17), padded CHAR(4), occurred TIMESTAMP(2), clock TIME(4), zoned TIMESTAMP(5) WITH TIME ZONE)`)
	h.exec(`COMMENT ON COLUMN "TypeMetadata453"."EmptyTable".label IS 'label comment'`)
	h.exec(`CREATE VIEW "TypeMetadata453"."EmptyView" AS SELECT * FROM "TypeMetadata453"."EmptyTable"`)
	h.exec(`CREATE TEMP TABLE "TempMetadata453" (amount NUMERIC(12,2), label VARCHAR(17), padded CHAR(4), occurred TIMESTAMP(2), clock TIME(4), zoned TIMESTAMP(5) WITH TIME ZONE)`)
	defer h.exec(`DROP TABLE "TempMetadata453"`)
	h.exec(`CREATE TEMP VIEW "TempViewMetadata453" AS SELECT * FROM "TempMetadata453"`)
	defer h.exec(`DROP VIEW "TempViewMetadata453"`)
	want := []string{"NUMERIC(12,2)", "VARCHAR(17)", "CHAR(4)", "TIMESTAMP(2)", "TIME(4)", "TIMESTAMP(5) WITH TIME ZONE"}
	check := func(t *testing.T, s *arrow.Schema) {
		t.Helper()
		if s.NumFields() != len(want) {
			t.Fatalf("unexpected fields: %v", s)
		}
		for i, f := range s.Fields() {
			if got, ok := f.Metadata.GetValue(typeNameKey); !ok || got != want[i] {
				t.Errorf("%s: type = %q (%v), want %s", f.Name, got, ok, want[i])
			}
		}
	}
	for _, rel := range []struct{ schema, name, sql string }{
		{"TypeMetadata453", "EmptyTable", `"TypeMetadata453"."EmptyTable"`},
		{"TypeMetadata453", "EmptyView", `"TypeMetadata453"."EmptyView"`},
		{"pg_temp", "TempMetadata453", `"TempMetadata453"`},
		{"pg_temp", "TempViewMetadata453", `"TempViewMetadata453"`},
	} {
		t.Run(rel.name, func(t *testing.T) {
			h := &sqlHarness{t: t, ctx: h.ctx, conn: h.conn}
			schema, err := h.conn.GetTableSchema(h.ctx, ptr("redis"), &rel.schema, rel.name)
			if err != nil {
				t.Fatal(err)
			}
			check(t, schema)
			if rel.name == "EmptyTable" {
				if got, _ := schema.Field(1).Metadata.GetValue(remarksKey); got != "label comment" {
					t.Fatal("lost comment", schema)
				}
			}
			// ExecuteSchema and both empty-result execution paths must agree before
			// any rows exist. Fully consume readers and check their terminal error.
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, "SELECT * FROM "+rel.sql); err != nil {
				t.Fatal(err)
			}
			s, err := st.(adbc.StatementExecuteSchemaWithContext).ExecuteSchema(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			check(t, s)
			for _, stream := range []string{"true", "false"} {
				rdr, _ := h.openStream(h.ctx, h.conn, "SELECT * FROM "+rel.sql, nil, map[string]string{OptionStringStreamResults: stream})
				check(t, rdr.Schema())
				for rdr.Next() {
					if rdr.RecordBatch().NumRows() != 0 {
						t.Error("expected empty table")
					}
				}
				err := rdr.Err()
				rdr.Release()
				if err != nil {
					t.Fatal(err)
				}
			}
			// These callbacks populate standard ADBC GetObjects xdbc_type_name.
			impl := st.(*statementImpl).conn
			tables, err := impl.GetTablesForDBSchema(h.ctx, "redis", rel.schema, &rel.name, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(tables) != 1 || len(tables[0].TableColumns) != len(want) {
				t.Fatalf("GetObjects columns: %+v", tables)
			}
			for i, col := range tables[0].TableColumns {
				if col.XdbcTypeName == nil || *col.XdbcTypeName != want[i] {
					t.Errorf("GetObjects type %v, want %s", col.XdbcTypeName, want[i])
				}
			}
		})
	}
}
