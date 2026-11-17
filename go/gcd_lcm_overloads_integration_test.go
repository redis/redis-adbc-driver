// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLGCDLCMEmptyPlanning(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_gcd_overloads")
	h.dropTables("it_gcd_bad_default")
	h.exec("CREATE TABLE it_gcd_overloads (s SMALLINT,i INTEGER,b BIGINT,n NUMERIC(10,3),f DOUBLE PRECISION,t TEXT,bo BOOLEAN)")
	for _, f := range []string{"gcd", "lcm"} {
		st := h.stmtWith("CREATE TABLE it_gcd_bad_default (i INTEGER DEFAULT CASE WHEN FALSE THEN "+f+"(NULL,NULL) ELSE 1 END)", nil)
		if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateAmbiguousFunction {
			t.Fatalf("invalid default %s: %v", f, err)
		}
		h.expectError("SELECT * FROM it_gcd_bad_default", "does not exist")
		for _, tc := range []struct{ args, state string }{
			{"s,s", stateAmbiguousFunction}, {"NULL,NULL", stateAmbiguousFunction},
			{"t,i", stateUndefinedFunction}, {"n,bo", stateUndefinedFunction}, {"f,i", stateUndefinedFunction},
			{"NULL::integer,'bad'", stateInvalidText}, {"i,'2147483648'", stateNumericRange},
		} {
			_, err := h.executeSchema("SELECT " + f + "(" + tc.args + ") FROM it_gcd_overloads WHERE FALSE")
			if sqlState(err) != tc.state {
				t.Errorf("%s(%s): %v, want %s", f, tc.args, err, tc.state)
			}
		}
		for _, q := range []string{
			"SELECT " + f + "(s,s) IS NULL FROM it_gcd_overloads WHERE FALSE",
			"SELECT CAST(" + f + "(s,s) AS TEXT) FROM it_gcd_overloads",
			"SELECT 1 FROM it_gcd_overloads WHERE " + f + "(s,s) IS NULL",
		} {
			if _, err := h.executeSchema(q); sqlState(err) != stateAmbiguousFunction {
				t.Errorf("%s: %v", q, err)
			}
		}
	}
	schema := h.expectRows("SELECT gcd(s,i),lcm(i,b),gcd(n,'0.0001') FROM it_gcd_overloads")
	expectTypes(t, schema, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64, &arrow.Decimal256Type{Precision: 76, Scale: 4})
	if _, err := h.executeSchema("SELECT min(gcd(s,s)) FROM it_gcd_overloads"); sqlState(err) != stateAmbiguousFunction {
		t.Fatalf("aggregate child: %v", err)
	}
}

func TestSQLGCDLCMPreparedReuse(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT gcd($1,$2),lcm($1,$2)", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	i16, i32, i64 := arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64
	dec := &arrow.Decimal128Type{Precision: 12, Scale: 2}
	for _, tc := range []struct {
		name      string
		a, b, out arrow.DataType
		state     string
		empty     bool
	}{
		{"integer", i32, i32, i32, "", false},
		{"widen", i16, i32, i32, "", false},
		{"decimal", dec, i64, &arrow.Decimal256Type{Precision: 76, Scale: 2}, "", false},
		{"smallint", i16, i16, nil, stateAmbiguousFunction, false},
		{"text", arrow.BinaryTypes.String, i32, nil, stateUndefinedFunction, false},
		{"float", arrow.PrimitiveTypes.Float64, i32, nil, stateUndefinedFunction, false},
		{"unknown", arrow.Null, arrow.Null, nil, stateAmbiguousFunction, false},
		{"smallint-empty", i16, i16, nil, stateAmbiguousFunction, true},
		{"text-empty", arrow.BinaryTypes.String, i32, nil, stateUndefinedFunction, true},
		{"integer-empty", i32, i32, i32, "", true},
		{"reuse-after-errors", i32, i64, i64, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: tc.a, Nullable: true}, {Name: "", Type: tc.b, Nullable: true}}, nil))
			defer builder.Release()
			if !tc.empty {
				for i, v := range []string{"12", "18"} {
					if builder.Field(i).Type().ID() == arrow.NULL {
						builder.Field(i).AppendNull()
					} else if err := builder.Field(i).AppendValueFromString(v); err != nil {
						t.Fatal(err)
					}
					builder.Field(i).AppendNull()
				}
			}
			record := builder.NewRecordBatch()
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
			expectTypes(t, reader.Schema(), tc.out, tc.out)
			if !tc.empty {
				if !reader.Next() || reader.RecordBatch().NumRows() != 2 {
					t.Fatalf("rows: %v", reader.Err())
				}
				for i, want := range []string{"6", "36"} {
					col := reader.RecordBatch().Column(i)
					if col.ValueStr(0) != want || !col.IsNull(1) {
						t.Fatalf("column %d: %s", i, col)
					}
				}
				if record.Column(0).ValueStr(0) != before {
					t.Fatal("bound input changed")
				}
			}
			// Arrow streams can contain empty batches. Drain them while
			// rejecting every additional data row, including for empty binds.
			for reader.Next() {
				if reader.RecordBatch().NumRows() != 0 {
					t.Fatalf("unexpected trailing data: %s", reader.RecordBatch())
				}
			}
			if reader.Err() != nil {
				t.Fatalf("trailing result: %v", reader.Err())
			}
		})
	}
	types, err := h.paramTypes("SELECT gcd($1::integer,$2::bigint)")
	if err != nil || len(types) != 2 || types[0] != i32.String() || types[1] != i64.String() {
		t.Fatalf("parameter schema: %v %v", types, err)
	}
	for _, q := range []string{"SELECT gcd($1,12::integer)", "SELECT lcm(12::bigint,$1)"} {
		types, err := h.paramTypes(q)
		want := i32.String()
		if q == "SELECT lcm(12::bigint,$1)" {
			want = i64.String()
		}
		if err != nil || len(types) != 1 || types[0] != want {
			t.Fatalf("%s parameter schema: %v %v", q, types, err)
		}
	}
	types, err = h.paramTypes("SELECT gcd($1,1.2::numeric(5,1))")
	if err != nil || len(types) != 1 || types[0] != arrow.Null.String() {
		t.Fatalf("unconstrained numeric parameter must not guess scale: %v %v", types, err)
	}
}
