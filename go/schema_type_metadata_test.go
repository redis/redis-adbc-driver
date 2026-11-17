// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// Descriptive SQL declarations must survive the Arrow wire format without
// becoming extension types or changing the physical ingestion contract.
func TestSQLTypeFieldMetadata(t *testing.T) {
	for _, declaration := range []string{
		"VARCHAR(17)", "CHAR(4)", "NUMERIC(12,2)", "TIMESTAMP(2)", "TIMESTAMP(3)",
		"TIMESTAMP(5) WITH TIME ZONE", "TIME(0)", "TIME(4)", "INTEGER", "BOOLEAN",
		"JSON", "JSONB", "UUID", "TIME(2) WITH TIME ZONE", "VARCHAR(17)[]",
	} {
		t.Run(declaration, func(t *testing.T) {
			typ, err := colTypeFromSQL(sqlTypeSpecOf(t, declaration))
			if err != nil {
				t.Fatal(err)
			}
			field := arrowField("MixedCase", typ, true)
			if got, ok := field.Metadata.GetValue(typeNameKey); !ok || got != declaration {
				t.Fatalf("type declaration = %q, present %v; want %q", got, ok, declaration)
			}
			if !arrow.TypeEqual(field.Type, typ.ArrowType()) {
				t.Fatal("physical type changed", field.Type)
			}
			if !typ.Kind.isJSON() && field.Metadata.FindKey("ARROW:extension:name") >= 0 {
				t.Fatal("ordinary field became extension", field)
			}
			// Existing JSON/UUID/timetz mappings remain semantic; ordinary SQL type
			// metadata remains descriptive, so VARCHAR(n) input still maps to strings.
			got, err := colTypeFromField(field)
			if err != nil {
				t.Fatal(err)
			}
			want, err := colTypeFromArrow(field.Type)
			if typ.Kind.isJSON() || typ.Kind == KindTimeTZ || typ.Alias == "uuid" {
				want, err = typ, nil
			}
			if err != nil || !arrow.TypeEqual(got.ArrowType(), want.ArrowType()) || got.SQLName() != want.SQLName() {
				t.Fatalf("ingestion type = %v, want %v (%v)", got, want, err)
			}
			var wire bytes.Buffer
			writer := ipc.NewWriter(&wire, ipc.WithSchema(arrow.NewSchema([]arrow.Field{field}, nil)))
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			reader, err := ipc.NewReader(&wire)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Release()
			decoded := reader.Schema().Field(0)
			// Arrow registers JSON as an extension on decode and consumes the
			// extension keys; the SQL declaration remains field metadata.
			if typ.Kind.isJSON() {
				if got, _ := decoded.Metadata.GetValue(typeNameKey); got != declaration {
					t.Fatal("IPC lost JSON declaration", decoded)
				}
				return
			}
			if !decoded.Equal(field) {
				t.Fatal("IPC lost field metadata", reader.Schema())
			}
		})
	}
}
