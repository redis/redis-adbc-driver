// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLIsNormalized(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_is_normalized")
	h.exec("CREATE TABLE it_is_normalized (id INTEGER,s TEXT)")
	h.exec("INSERT INTO it_is_normalized VALUES (1,'ä'),(2,'ä'),(3,'ﬃ'),(4,NULL)")
	schema := h.expectRows("SELECT s IS NORMALIZED,s IS NOT NFD NORMALIZED,is_normalized(s,'NFKC') FROM it_is_normalized ORDER BY id", "false|false|false", "true|true|true", "true|false|false", "NULL|NULL|NULL")
	expectTypes(t, schema, arrow.FixedWidthTypes.Boolean, arrow.FixedWidthTypes.Boolean, arrow.FixedWidthTypes.Boolean)
	h.expectRows("SELECT id FROM it_is_normalized WHERE s IS NORMALIZED ORDER BY id", "2", "3")
	h.expectRows("SELECT s IS NORMALIZED,count(*) FROM it_is_normalized GROUP BY s IS NORMALIZED ORDER BY s IS NORMALIZED NULLS LAST", "false|1", "true|2", "NULL|1")
	h.exec("UPDATE it_is_normalized SET s=normalize(s) WHERE s IS NOT NORMALIZED")
	h.expectRows("SELECT id FROM it_is_normalized WHERE s IS NORMALIZED ORDER BY id", "1", "2", "3")
	schema = h.expectRows("SELECT s IS NORMALIZED,s IS NOT NFD NORMALIZED FROM it_is_normalized WHERE FALSE")
	expectTypes(t, schema, arrow.FixedWidthTypes.Boolean, arrow.FixedWidthTypes.Boolean)
	for _, q := range []string{"SELECT 1 IS NORMALIZED FROM it_is_normalized WHERE FALSE", "SELECT (NULL::integer IS NORMALIZED) IS NULL FROM it_is_normalized WHERE FALSE"} {
		if _, err := h.executeSchema(q); sqlState(err) != stateUndefinedFunction {
			t.Errorf("%s: %v", q, err)
		}
	}
}

func TestSQLIsNormalizedBoundReuse(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT $1 IS NORMALIZED,$1 IS NOT NFD NORMALIZED,is_normalized($1,$2)", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, form, state    string
		integer, empty, null bool
	}{
		{name: "unicode", form: "NFD"}, {name: "bad-form", form: "bad", state: stateInvalidParameter},
		{name: "integer", integer: true, form: "NFD", state: stateUndefinedFunction},
		{name: "empty-integer", integer: true, empty: true, form: "NFD", state: stateUndefinedFunction},
		{name: "null", null: true}, {name: "empty", empty: true}, {name: "reuse", form: "NFD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ := arrow.DataType(arrow.BinaryTypes.String)
			if tc.integer {
				typ = arrow.PrimitiveTypes.Int32
			}
			b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: typ, Nullable: true}, {Name: "", Type: arrow.BinaryTypes.String, Nullable: true}}, nil))
			defer b.Release()
			if !tc.empty {
				if tc.null {
					b.Field(0).AppendNull()
					b.Field(1).AppendNull()
				} else {
					value := "ä"
					if tc.integer {
						value = "1"
					}
					if err := b.Field(0).AppendValueFromString(value); err != nil {
						t.Fatal(err)
					}
					if err := b.Field(1).AppendValueFromString(tc.form); err != nil {
						t.Fatal(err)
					}
				}
			}
			record := b.NewRecordBatch()
			defer record.Release()
			before := ""
			if !tc.empty {
				before = record.Column(0).ValueStr(0)
			}
			if err := st.Bind(h.ctx, record); err != nil {
				t.Fatal(err)
			}
			reader, _, err := st.ExecuteQuery(h.ctx)
			if tc.state != "" {
				if reader != nil {
					reader.Release()
				}
				if sqlState(err) != tc.state {
					t.Fatalf("%v, want %s", err, tc.state)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Release()
			expectTypes(t, reader.Schema(), arrow.FixedWidthTypes.Boolean, arrow.FixedWidthTypes.Boolean, arrow.FixedWidthTypes.Boolean)
			rows := int64(0)
			for reader.Next() {
				batch := reader.RecordBatch()
				rows += batch.NumRows()
				for i := int64(0); i < batch.NumRows(); i++ {
					for j, want := range []string{"true", "true", "false"} {
						col := batch.Column(j)
						if tc.null {
							if !col.IsNull(int(i)) {
								t.Fatal("expected NULL")
							}
						} else if col.ValueStr(int(i)) != want {
							t.Fatalf("column %d: %s", j, col)
						}
					}
				}
			}
			if reader.Err() != nil {
				t.Fatal(reader.Err())
			}
			wantRows := int64(1)
			if tc.empty {
				wantRows = 0
			}
			if rows != wantRows {
				t.Fatalf("rows %d, want %d", rows, wantRows)
			}
			if !tc.empty && record.Column(0).ValueStr(0) != before {
				t.Fatal("bound input changed")
			}
		})
	}
	types, err := h.paramTypes("SELECT $1 IS NORMALIZED,is_normalized($1,$2)")
	if err != nil || len(types) != 2 {
		t.Fatalf("parameter types %v %v", types, err)
	}
	for _, typ := range types {
		if typ != arrow.BinaryTypes.String.String() {
			t.Fatalf("parameter type %s", typ)
		}
	}
}
