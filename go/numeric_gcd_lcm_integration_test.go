// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLNumericGCDLCM(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_numeric_gcd_lcm")
	h.exec("CREATE TABLE it_numeric_gcd_lcm (id INTEGER, a NUMERIC(76,2), b NUMERIC(76,3))")
	h.exec("INSERT INTO it_numeric_gcd_lcm VALUES (1,1.20,0.450),(2,-1.20,0.450),(3,0,0),(4,NULL,0.450)")
	schema := h.expectRows("SELECT gcd(a,b),lcm(a,b) FROM it_numeric_gcd_lcm ORDER BY id",
		"0.150|3.600", "0.150|3.600", "0.000|0.000", "NULL|NULL")
	typ := &arrow.Decimal256Type{Precision: 76, Scale: 3}
	expectTypes(t, schema, typ, typ)
	empty, err := h.executeSchema("SELECT gcd(a,b),lcm(a,b) FROM it_numeric_gcd_lcm WHERE false")
	if err != nil {
		t.Fatal(err)
	}
	expectTypes(t, empty, typ, typ)
	h.exec("UPDATE it_numeric_gcd_lcm SET a=gcd(a,b) WHERE id=1")
	h.expectRows("SELECT a,b FROM it_numeric_gcd_lcm WHERE id=1", "0.15|0.450")
}

func TestSQLBoundNumericGCDLCM(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT gcd($1,$2),lcm($1,$2)", nil)
	aType := &arrow.Decimal128Type{Precision: 10, Scale: 2}
	bType := &arrow.Decimal128Type{Precision: 10, Scale: 3}
	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: aType}, {Name: "", Type: bType}}, nil))
	defer builder.Release()
	builder.Field(0).(*array.Decimal128Builder).AppendValues([]decimal128.Num{decimal128.FromI64(120), {}, {}}, []bool{true, false, true})
	builder.Field(1).(*array.Decimal128Builder).AppendValues([]decimal128.Num{decimal128.FromI64(450), decimal128.FromI64(450), {}}, nil)
	record := builder.NewRecordBatch()
	defer record.Release()
	if err := st.Bind(h.ctx, record); err != nil {
		t.Fatal(err)
	}
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	typ := &arrow.Decimal256Type{Precision: 76, Scale: 3}
	expectTypes(t, reader.Schema(), typ, typ)
	if !reader.Next() || reader.RecordBatch().NumRows() != 3 {
		t.Fatalf("bound rows: %v", reader.Err())
	}
	batch := reader.RecordBatch()
	for col, wants := range [][]string{{"150", "(null)", "0"}, {"3600", "(null)", "0"}} {
		values := batch.Column(col).(*array.Decimal256)
		for row, want := range wants {
			if row == 1 {
				if !values.IsNull(row) {
					t.Fatal("missing bound NULL")
				}
				continue
			}
			if got := values.Value(row).BigInt().String(); got != want {
				t.Fatalf("bound row %d col %d: %s, want %s", row, col, got, want)
			}
		}
	}
	if reader.Next() || reader.Err() != nil {
		t.Fatalf("unexpected trailing rows: %v", reader.Err())
	}
	if got := record.Column(0).(*array.Decimal128).Value(0); got != decimal128.FromI64(120) {
		t.Fatal("caller input changed")
	}
}
