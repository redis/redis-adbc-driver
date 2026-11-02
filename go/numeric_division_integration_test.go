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

import (
	"math/big"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLDecimalDivisionSchema(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_numeric_division")
	h.exec("CREATE TABLE it_numeric_division (n NUMERIC(38,0), d NUMERIC(38,4))")
	h.exec("INSERT INTO it_numeric_division VALUES (1, 3), (10000, 0.0001)")
	schema := h.expectRows("SELECT n / d FROM it_numeric_division ORDER BY n",
		"0.33333333333333333333", "100000000.00000000000000000000")
	if got := schema.Field(0).Type; !arrow.TypeEqual(got, &arrow.Decimal128Type{Precision: 29, Scale: 20}) {
		t.Fatalf("resolved division schema: %s", got)
	}
	empty, err := h.executeSchema("SELECT n / d FROM it_numeric_division")
	if err != nil {
		t.Fatal(err)
	}
	h.expectNoRows("SELECT n / d FROM it_numeric_division WHERE false", schemaText(empty))
}

func TestSQLBoundDecimalDivisionScales(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT CAST(1 AS NUMERIC(10,0)) / $1", nil)
	typ := &arrow.Decimal128Type{Precision: 20, Scale: 4}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: typ}}, nil))
	defer b.Release()
	b.Field(0).(*array.Decimal128Builder).AppendValues([]decimal128.Num{decimal128.FromI64(1), decimal128.FromI64(1000000000000)}, nil)
	record := b.NewRecordBatch()
	defer record.Release()
	if err := st.Bind(h.ctx, record); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Release()
	if got := rdr.Schema().Field(0).Type; !arrow.TypeEqual(got, &arrow.Decimal128Type{Precision: 33, Scale: 28}) {
		t.Fatalf("combined division schema: %s", got)
	}
	if !rdr.Next() || rdr.RecordBatch().NumRows() != 2 {
		t.Fatalf("bound division rows: %v", rdr.Err())
	}
	for i, want := range []string{"10000", "0.00000001"} {
		got, ok := new(big.Rat).SetString(rdr.RecordBatch().Column(0).ValueStr(i))
		expected, _ := new(big.Rat).SetString(want)
		if !ok || got.Cmp(expected) != 0 {
			t.Fatalf("bound row %d: %v, want %s", i, got, want)
		}
	}
}
