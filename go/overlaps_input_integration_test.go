// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLOverlapsInputParameters(t *testing.T) {
	h := newSQLHarness(t)
	for _, tc := range []struct {
		sql    string
		values []string
		want   string
	}{
		{"select ($1::timestamp,$2::timestamp) overlaps (timestamp '2024-01-02',timestamp '2024-01-03')", []string{"2024-01-01", "2024-01-02"}, "false"},
		{"select (pg_input_error_info($1::text,$2::text)).sql_error_code", []string{"1e4000", "float8"}, "22003"},
		{"select sql_error_code from pg_input_error_info($1::text,$2::text)", []string{"bad", "int4"}, "22P02"},
	} {
		st := h.stmtWith(tc.sql, nil)
		schema := arrow.NewSchema([]arrow.Field{{Type: arrow.BinaryTypes.String, Nullable: true}, {Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
		b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		for i, s := range tc.values {
			b.Field(i).(*array.StringBuilder).Append(s)
		}
		rec := b.NewRecordBatch()
		b.Release()
		err := st.Bind(h.ctx, rec)
		rec.Release()
		if err != nil {
			t.Fatal(err)
		}
		rdr, _, err := st.ExecuteQuery(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !rdr.Next() || rdr.RecordBatch().Column(0).ValueStr(0) != tc.want {
			t.Errorf("%s: expected %s", tc.sql, tc.want)
		}
		if rdr.Next() || rdr.Err() != nil {
			t.Errorf("%s: unexpected trailing rows/error: %v", tc.sql, rdr.Err())
		}
		rdr.Release()
	}
	schema := h.expectRows("select * from pg_input_error_info('bad','int4') where false")
	for i, name := range inputErrorNames {
		f := schema.Field(i)
		if f.Name != name || !f.Nullable || f.Type.ID() != arrow.STRING {
			t.Errorf("diagnostic field %d: %v", i, f)
		}
	}
	h.expectRows("select to_json(pg_input_error_info(null,'int4'))", "NULL")
}
