// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLFactorial(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_factorial")
	h.exec("CREATE TABLE it_factorial (id INTEGER,n BIGINT,f NUMERIC(76,0) DEFAULT factorial(5))")
	h.exec("INSERT INTO it_factorial (id,n) VALUES (1,0),(2,5),(3,NULL)")
	schema := h.expectRows("SELECT factorial(n) FROM it_factorial ORDER BY id", "1", "120", "NULL")
	expectTypes(t, schema, &arrow.Decimal256Type{Precision: 76, Scale: 0})
	h.expectRows("SELECT f FROM it_factorial ORDER BY id", "120", "120", "120")
	h.expectRows("SELECT id FROM it_factorial WHERE factorial(n)=120", "2")
	h.exec("UPDATE it_factorial SET f=factorial(n)")
	h.expectRows("SELECT f FROM it_factorial ORDER BY id", "1", "120", "NULL")
	schema = h.expectRows("SELECT factorial(n) FROM it_factorial WHERE FALSE")
	expectTypes(t, schema, &arrow.Decimal256Type{Precision: 76, Scale: 0})
	for _, q := range []string{"SELECT factorial(NULL::text) IS NULL FROM it_factorial WHERE FALSE", "SELECT CASE WHEN FALSE THEN factorial(NULL::numeric) ELSE 1 END FROM it_factorial"} {
		if _, err := h.executeSchema(q); sqlState(err) != stateUndefinedFunction {
			t.Errorf("%s: %v", q, err)
		}
	}
	// A failed row update must not publish an earlier staged row.
	h.exec("UPDATE it_factorial SET n=CASE WHEN id=2 THEN -1 ELSE n END")
	st := h.stmtWith("UPDATE it_factorial SET f=factorial(n)", nil)
	if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateNumericRange {
		t.Fatalf("update: %v", err)
	}
	h.expectRows("SELECT f FROM it_factorial ORDER BY id", "1", "120", "NULL")
	h.dropTables("it_factorial_bad_default")
	st = h.stmtWith("CREATE TABLE it_factorial_bad_default (i NUMERIC DEFAULT CASE WHEN FALSE THEN factorial(NULL::text) ELSE 1 END)", nil)
	if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateUndefinedFunction {
		t.Fatalf("default signature: %v", err)
	}
	h.expectError("SELECT * FROM it_factorial_bad_default", "does not exist")
}

func TestSQLBoundFactorialReuse(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT factorial($1)", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		typ    arrow.DataType
		values []string
		empty  bool
		state  string
	}{
		{"smallint", arrow.PrimitiveTypes.Int16, []string{"0", "5"}, false, ""},
		{"integer", arrow.PrimitiveTypes.Int32, []string{"1", "6"}, false, ""},
		{"bigint", arrow.PrimitiveTypes.Int64, []string{"5", "20"}, false, ""},
		{"negative", arrow.PrimitiveTypes.Int64, []string{"-1"}, false, stateNumericRange},
		{"range", arrow.PrimitiveTypes.Int64, []string{"57"}, false, stateNumericRange},
		{"typed-text", arrow.BinaryTypes.String, []string{"5"}, false, stateUndefinedFunction},
		{"empty-text", arrow.BinaryTypes.String, nil, true, stateUndefinedFunction},
		{"float", arrow.PrimitiveTypes.Float64, []string{"5"}, false, stateUndefinedFunction},
		{"empty-bigint", arrow.PrimitiveTypes.Int64, nil, true, ""},
		{"untyped-null", arrow.Null, nil, false, ""},
		{"reuse", arrow.PrimitiveTypes.Int64, []string{"0", "5"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: tc.typ, Nullable: true}}, nil))
			defer b.Release()
			for _, v := range tc.values {
				if err := b.Field(0).AppendValueFromString(v); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.empty {
				b.Field(0).AppendNull()
			}
			rec := b.NewRecordBatch()
			defer rec.Release()
			before := ""
			if rec.NumRows() > 0 {
				before = rec.Column(0).ValueStr(0)
			}
			if err := st.Bind(h.ctx, rec); err != nil {
				t.Fatal(err)
			}
			rdr, _, err := st.ExecuteQuery(h.ctx)
			if tc.state != "" {
				if rdr != nil {
					rdr.Release()
				}
				if sqlState(err) != tc.state {
					t.Fatalf("%v, want %s", err, tc.state)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer rdr.Release()
			expectTypes(t, rdr.Schema(), &arrow.Decimal256Type{Precision: 76, Scale: 0})
			expected := map[string]string{"0": "1", "1": "1", "5": "120", "6": "720", "20": "2432902008176640000"}
			rows := 0
			for rdr.Next() {
				col := rdr.RecordBatch().Column(0)
				for i := 0; i < col.Len(); i++ {
					if rows == len(tc.values) {
						if !col.IsNull(i) {
							t.Fatal("NULL lost")
						}
					} else if col.ValueStr(i) != expected[tc.values[rows]] {
						t.Fatalf("row %d: %s", rows, col.ValueStr(i))
					}
					rows++
				}
			}
			if rdr.Err() != nil {
				t.Fatal(rdr.Err())
			}
			want := len(tc.values) + 1
			if tc.empty {
				want = 0
			}
			if rows != want {
				t.Fatalf("rows %d, want %d", rows, want)
			}
			if rec.NumRows() > 0 && rec.Column(0).ValueStr(0) != before {
				t.Fatal("bound input changed")
			}
		})
	}
	types, err := h.paramTypes("SELECT factorial($1)")
	if err != nil || len(types) != 1 || types[0] != arrow.PrimitiveTypes.Int64.String() {
		t.Fatalf("parameters: %v %v", types, err)
	}
}
