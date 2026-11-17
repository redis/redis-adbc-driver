// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"testing"
)

func TestSQLMinScale(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_min_scale")
	h.exec("CREATE TABLE it_min_scale (id INTEGER,n NUMERIC(76,6))")
	h.exec("INSERT INTO it_min_scale VALUES (1,1.23000),(2,-0.000001),(3,0.000),(4,NULL)")
	schema := h.expectRows("SELECT min_scale(n) FROM it_min_scale ORDER BY id", "2", "6", "0", "NULL")
	expectTypes(t, schema, arrow.PrimitiveTypes.Int32)
	schema = h.expectRows("SELECT min_scale(n) FROM it_min_scale WHERE false")
	expectTypes(t, schema, arrow.PrimitiveTypes.Int32)
	h.expectRows("SELECT min_scale('1.23000'),min_scale('1e-1000'),min_scale(NULL)", "2|1000|NULL")
}

func TestSQLBoundMinScale(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT min_scale($1)", nil)
	typ := &arrow.Decimal128Type{Precision: 12, Scale: 5}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: typ, Nullable: true}}, nil))
	defer b.Release()
	b.Field(0).(*array.Decimal128Builder).AppendValues([]decimal128.Num{decimal128.FromI64(123000), decimal128.FromI64(-123000), {}, {}}, []bool{true, true, true, false})
	rec := b.NewRecordBatch()
	defer rec.Release()
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Release()
	expectTypes(t, rdr.Schema(), arrow.PrimitiveTypes.Int32)
	if !rdr.Next() || rdr.RecordBatch().NumRows() != 4 {
		t.Fatalf("bound rows: %v", rdr.Err())
	}
	a := rdr.RecordBatch().Column(0).(*array.Int32)
	for i, want := range []int32{2, 2, 0} {
		if a.IsNull(i) || a.Value(i) != want {
			t.Errorf("row %d: %s", i, a.ValueStr(i))
		}
	}
	if !a.IsNull(3) {
		t.Error("bound NULL lost")
	}
	if rdr.Next() || rdr.Err() != nil {
		t.Fatalf("trailing rows: %v", rdr.Err())
	}
	if rec.Column(0).(*array.Decimal128).Value(0) != decimal128.FromI64(123000) {
		t.Error("caller input changed")
	}
}
