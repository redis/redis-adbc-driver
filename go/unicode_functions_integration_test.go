// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLUnicodeFunctions(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_unicode_functions")
	h.exec("CREATE TABLE it_unicode_functions (id INTEGER,s TEXT,e TEXT)")
	h.exec(`INSERT INTO it_unicode_functions VALUES (1,'ä','\u0061'),(2,'ﬃ','\uD83D\uDE00'),(3,NULL,NULL)`)
	schema := h.expectRows("SELECT normalize(s),normalize(s,NFKD),unistr(e) FROM it_unicode_functions ORDER BY id", "ä|ä|a", "ﬃ|ffi|😀", "NULL|NULL|NULL")
	expectTypes(t, schema, arrow.BinaryTypes.String, arrow.BinaryTypes.String, arrow.BinaryTypes.String)
	h.expectRows("SELECT id FROM it_unicode_functions WHERE normalize(s)='ä'", "1")
	h.expectRows("SELECT normalize(s),count(*) FROM it_unicode_functions GROUP BY normalize(s) ORDER BY normalize(s) NULLS LAST", "ä|1", "ﬃ|1", "NULL|1")
	h.exec("UPDATE it_unicode_functions SET s=normalize(s,NFKC),e=unistr(e)")
	h.expectRows("SELECT s,e FROM it_unicode_functions ORDER BY id", "ä|a", "ffi|😀", "NULL|NULL")
	schema = h.expectRows("SELECT normalize(s),unistr(e) FROM it_unicode_functions WHERE FALSE")
	expectTypes(t, schema, arrow.BinaryTypes.String, arrow.BinaryTypes.String)
	for _, q := range []string{"SELECT normalize(NULL::integer) IS NULL FROM it_unicode_functions WHERE FALSE", "SELECT CASE WHEN FALSE THEN unistr(NULL::integer) ELSE '' END FROM it_unicode_functions"} {
		if _, err := h.executeSchema(q); sqlState(err) != stateUndefinedFunction {
			t.Errorf("%s: %v", q, err)
		}
	}
	// Valid defaults and lazy DEFAULT branches retain normal evaluation.
	h.dropTables("it_unicode_defaults")
	h.exec(`CREATE TABLE it_unicode_defaults (s TEXT DEFAULT unistr('\u0061'), n TEXT DEFAULT CASE WHEN FALSE THEN unistr('\xyz') ELSE normalize('ä') END)`)
	h.exec("INSERT INTO it_unicode_defaults DEFAULT VALUES")
	h.expectRows("SELECT * FROM it_unicode_defaults", "a|ä")

}

func TestSQLUnicodeBoundReuse(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT normalize($1),unistr($2),pg_catalog.normalize($1,$3)", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                 string
		values               []string
		integer, empty, null bool
		state                string
	}{
		{name: "unicode", values: []string{"ä", `\uD83D\uDE00`, "NFD"}},
		{name: "bad-escape", values: []string{"ä", `\xyz`, "NFC"}, state: stateSyntax},
		{name: "bad-form", values: []string{"ä", "a", "bad"}, state: stateInvalidParameter},
		{name: "wrong-type", integer: true, state: stateUndefinedFunction},
		{name: "empty-wrong-type", integer: true, empty: true, state: stateUndefinedFunction},
		{name: "null", null: true}, {name: "empty", empty: true},
		{name: "reuse-after-error", values: []string{"ä", `\uD83D\uDE00`, "NFD"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ := arrow.DataType(arrow.BinaryTypes.String)
			if tc.integer {
				typ = arrow.PrimitiveTypes.Int32
			}
			b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: typ, Nullable: true}, {Name: "", Type: arrow.BinaryTypes.String, Nullable: true}, {Name: "", Type: arrow.BinaryTypes.String, Nullable: true}}, nil))
			defer b.Release()
			if !tc.empty {
				for i := 0; i < 3; i++ {
					if tc.null {
						b.Field(i).AppendNull()
						continue
					}
					value := "1"
					if len(tc.values) > 0 {
						value = tc.values[i]
					}
					if err := b.Field(i).AppendValueFromString(value); err != nil {
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
			expectTypes(t, reader.Schema(), arrow.BinaryTypes.String, arrow.BinaryTypes.String, arrow.BinaryTypes.String)
			rows := int64(0)
			for reader.Next() {
				batch := reader.RecordBatch()
				rows += batch.NumRows()
				for i := int64(0); i < batch.NumRows(); i++ {
					for j, want := range []string{"ä", "😀", "ä"} {
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
	types, err := h.paramTypes("SELECT normalize($1),unistr($2),pg_catalog.normalize($1,$3)")
	if err != nil || len(types) != 3 {
		t.Fatalf("parameter types: %v %v", types, err)
	}
	for _, typ := range types {
		if typ != arrow.BinaryTypes.String.String() {
			t.Fatalf("parameter type: %s", typ)
		}
	}
}
