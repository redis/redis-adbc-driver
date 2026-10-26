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

// Integration tests for quoted string literals, which take the type of the
// values next to them (unknown.go). The results are Postgres 16's, but for
// the scale of a NUMERIC(10,2) and a literal (Postgres's type has none, and
// shows 2.5 and 0), the driver's errors for text that doesn't convert, and
// BIGINT for Postgres's INTEGER literals.

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func (h *sqlHarness) setupUnknownLiterals() {
	h.dropTables("it_unklit")
	h.exec(`CREATE TABLE it_unklit (id INTEGER, s VARCHAR(10), d DATE, n NUMERIC(10,2), b BOOLEAN, tz TIMESTAMPTZ)`)
	h.exec(`INSERT INTO it_unklit VALUES (1, 'a', '2024-01-01', 1.50, TRUE, '2024-01-01 00:00:00+00'),
		(2, NULL, NULL, NULL, NULL, NULL)`)
}

// expectType checks the type of a result's column i.
func (h *sqlHarness) expectType(schema *arrow.Schema, sql string, i int, want arrow.DataType) {
	h.t.Helper()
	if got := schema.Field(i).Type; !arrow.TypeEqual(got, want) {
		h.t.Errorf("%s: column %d is %s, want %s", sql, i+1, got, want)
	}
}

// A CASE, COALESCE, GREATEST, LEAST, NULLIF or LAG of a quoted literal and
// an integer is an integer, and the literal is one of its integers.
func TestSQLUnknownLiterals(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownLiterals()
	for _, c := range []struct {
		sql  string
		typ  arrow.DataType
		want []string
	}{
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE 1 END FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int64, []string{"1", "1"}},
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE 1 END + 1 FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int64, []string{"2", "2"}},
		// The CASE is named id, after its ELSE, so ORDER BY id sorts by it.
		{`SELECT CASE WHEN id = 1 THEN '5' ELSE id END FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int32, []string{"2", "5"}},
		{`SELECT COALESCE(CASE WHEN id = 2 THEN id END, '0') + 1 FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int64, []string{"1", "3"}},
		{`SELECT COALESCE(CASE WHEN id = 2 THEN id END, '0') FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int32, []string{"0", "2"}},
		{`SELECT GREATEST(CASE WHEN id = 2 THEN id END, '5') FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int32, []string{"5", "5"}},
		{`SELECT LEAST(id, '5') + 1 FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int64, []string{"2", "3"}},
		{`SELECT NULLIF('1', id) FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int32, []string{"NULL", "1"}},
		{`SELECT LAG(id, 1, '0') OVER (ORDER BY id) + 1 FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int64, []string{"1", "2"}},
		{`SELECT CASE WHEN id = 1 THEN d ELSE '2024-02-01' END FROM it_unklit ORDER BY id`, arrow.FixedWidthTypes.Date32,
			[]string{"2024-01-01", "2024-02-01"}},
		{`SELECT CASE WHEN id = 1 THEN b ELSE 'false' END FROM it_unklit ORDER BY id`, arrow.FixedWidthTypes.Boolean,
			[]string{"true", "false"}},
		{`SELECT CASE WHEN id = 1 THEN n ELSE '2.5' END FROM it_unklit ORDER BY id`, &arrow.Decimal128Type{Precision: 10, Scale: 2},
			[]string{"1.50", "2.50"}},
		{`SELECT CASE WHEN id = 1 THEN n ELSE '2.567' END FROM it_unklit ORDER BY id`, &arrow.Decimal128Type{Precision: 38, Scale: 3},
			[]string{"1.500", "2.567"}},
		// Without a typed value, text.
		{`SELECT CASE WHEN id = 1 THEN '1' END FROM it_unklit ORDER BY id`, arrow.BinaryTypes.String, []string{"1", "NULL"}},
		{`SELECT COALESCE(NULL, '2', 1) FROM it_unklit ORDER BY id`, arrow.PrimitiveTypes.Int64, []string{"2", "2"}},
		// Grouping, DISTINCT and windows see one value.
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE 1 END AS c, COUNT(*) FROM it_unklit GROUP BY 1`, arrow.PrimitiveTypes.Int64, []string{"1|2"}},
		{`SELECT COUNT(DISTINCT CASE WHEN id = 1 THEN '1' ELSE 1 END) FROM it_unklit`, arrow.PrimitiveTypes.Int64, []string{"1"}},
		{`SELECT COUNT(*) FROM (SELECT DISTINCT CASE WHEN id = 1 THEN '1' ELSE 1 END FROM it_unklit) s`, arrow.PrimitiveTypes.Int64, []string{"1"}},
		{`SELECT COUNT(*) OVER (PARTITION BY CASE WHEN id = 1 THEN '1' ELSE 1 END) FROM it_unklit ORDER BY id`,
			arrow.PrimitiveTypes.Int64, []string{"2", "2"}},
		{`SELECT MIN(CASE WHEN id = 1 THEN '10' ELSE 9 END), MAX(CASE WHEN id = 1 THEN '10' ELSE 9 END) FROM it_unklit`,
			arrow.PrimitiveTypes.Int64, []string{"9|10"}},
		{`SELECT CASE WHEN COUNT(*) > 1 THEN '7' ELSE 2 END + 1 FROM it_unklit`, arrow.PrimitiveTypes.Int64, []string{"8"}},
		// Set operations.
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE 1 END FROM it_unklit UNION SELECT 1`, arrow.PrimitiveTypes.Int64, []string{"1"}},
		{`SELECT '1' UNION SELECT 3 ORDER BY 1`, arrow.PrimitiveTypes.Int64, []string{"1", "3"}},
		{`SELECT d FROM it_unklit WHERE id = 1 UNION SELECT '2024-05-05' ORDER BY 1`, arrow.FixedWidthTypes.Date32,
			[]string{"2024-01-01", "2024-05-05"}},
		// The index is queried with the integer.
		{`SELECT id FROM it_unklit WHERE id = CASE WHEN TRUE THEN '1' ELSE 2 END`, arrow.PrimitiveTypes.Int32, []string{"1"}},
		{`SELECT id FROM it_unklit WHERE COALESCE(CASE WHEN id = 1 THEN id END, '5') > 3`, arrow.PrimitiveTypes.Int32, []string{"2"}},
	} {
		schema := h.expectRows(c.sql, c.want...)
		h.expectType(schema, c.sql, schema.NumFields()-1, c.typ)
	}
	if q, residual, _ := h.planOf(`SELECT id FROM it_unklit WHERE id = CASE WHEN TRUE THEN '1' ELSE 2 END`); q != "@id:[1 1]" || residual {
		t.Errorf("WHERE id = CASE … '1' …: query %q, residual %v; want @id:[1 1] and none", q, residual)
	}
	for _, c := range []struct{ sql, err string }{
		{`SELECT CASE WHEN id = 1 THEN s ELSE 1 END FROM it_unklit`, "CASE types bigint and varchar cannot be matched"},
		{`SELECT id FROM it_unklit WHERE CASE WHEN id = 1 THEN s ELSE 1 END = 'a'`, "CASE types bigint and varchar cannot be matched"},
		{`SELECT COALESCE(CASE WHEN id = 1 THEN '1' END, 1) FROM it_unklit`, "COALESCE types varchar and bigint cannot be matched"},
		{`SELECT COALESCE(s, 1) FROM it_unklit`, "COALESCE types varchar and bigint cannot be matched"},
		// A literal that isn't an integer is an error, also where no row reads it.
		{`SELECT CASE WHEN id = 1 THEN 'x' ELSE 1 END FROM it_unklit WHERE FALSE`, `invalid input syntax for type bigint: "x"`},
		{`SELECT id FROM it_unklit WHERE id = CASE WHEN id = 1 THEN 'x' ELSE 2 END`, `invalid input syntax for type bigint: "x"`},
		{`SELECT CASE WHEN id = 1 THEN b ELSE 'maybe' END FROM it_unklit`, `cannot convert "maybe" to BOOLEAN`},
		{`SELECT 'a' UNION SELECT 3`, `invalid input syntax for type bigint: "a"`},
	} {
		h.expectError(c.sql, c.err)
	}
	// With Postgres's SQLSTATEs: datatype_mismatch, invalid_text_representation.
	for _, c := range []struct {
		sql, state string
		code       adbc.Status
	}{
		{`SELECT CASE WHEN id = 1 THEN s ELSE 1 END FROM it_unklit`, "42804", adbc.StatusInvalidArgument},
		{`SELECT id FROM it_unklit WHERE COALESCE(s, 1) = 1`, "42804", adbc.StatusInvalidArgument},
		{`SELECT '1' UNION SELECT '2' UNION SELECT 3`, "42804", adbc.StatusInvalidArgument},
		{`SELECT CASE WHEN id = 1 THEN 'x' ELSE 1 END FROM it_unklit`, "22P02", adbc.StatusInvalidData},
	} {
		checkState(t, c.sql, h.stmtErr(c.sql, nil), c.code, c.state, "")
	}
}

// CREATE TABLE AS, INSERT, UPDATE and views take an expression's values as
// its type.
func TestSQLUnknownLiteralWrites(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownLiterals()
	h.dropViews("it_unklit_v")
	h.dropTables("it_unklit_ctas")
	h.exec(`CREATE TABLE it_unklit_ctas AS SELECT id, CASE WHEN id = 1 THEN '1' ELSE id END AS c,
		COALESCE(n, '0') AS m, CASE WHEN id = 1 THEN '1' END AS t FROM it_unklit`)
	h.expectRows(`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'it_unklit_ctas' ORDER BY ordinal_position`,
		"id|INTEGER", "c|INTEGER", "m|NUMERIC(10,2)", "t|VARCHAR")
	h.expectRows(`SELECT c + 1, m, t FROM it_unklit_ctas ORDER BY id`, "2|1.50|1", "3|0.00|NULL")
	h.exec(`INSERT INTO it_unklit (id, n) VALUES (CASE WHEN TRUE THEN '7' ELSE 1 END, 1)`)
	h.exec(`UPDATE it_unklit SET n = CASE WHEN n > 0 THEN '3.25' ELSE n END WHERE id = 7`)
	h.expectRows(`SELECT id, n FROM it_unklit WHERE id = 7`, "7|3.25")
	h.exec(`CREATE VIEW it_unklit_v AS SELECT id, CASE WHEN id = 1 THEN '1' ELSE id END AS c FROM it_unklit`)
	schema := h.expectRows(`SELECT c + 1 FROM it_unklit_v ORDER BY id`, "2", "3", "8")
	h.expectType(schema, "it_unklit_v", 0, arrow.PrimitiveTypes.Int64)
}

// A quoted literal that becomes a timestamp with time zone is a local time
// in the session time zone, and a view reads it in the zone it was created
// in, as Postgres does.
func TestSQLUnknownLiteralTimeZones(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownLiterals()
	h.dropViews("it_unklit_tzv")
	h.exec(`SET TIME ZONE 'America/New_York'`)
	t.Cleanup(func() { h.exec(`SET TIME ZONE 'UTC'`) })
	c := `CASE WHEN id = 1 THEN tz ELSE '2024-01-01 10:00' END`
	h.expectRows(`SELECT CAST(`+c+` AS VARCHAR), CAST(COALESCE(tz, '2024-07-01 10:00') AS VARCHAR) FROM it_unklit ORDER BY id`,
		"2023-12-31 19:00:00-05|2023-12-31 19:00:00-05", "2024-01-01 10:00:00-05|2024-07-01 10:00:00-04")
	h.exec(`CREATE VIEW it_unklit_tzv AS SELECT id, ` + c + ` AS c FROM it_unklit`)
	h.exec(`SET TIME ZONE 'UTC'`)
	h.expectRows(`SELECT CAST(c AS VARCHAR) FROM it_unklit_tzv ORDER BY id`, "2024-01-01 00:00:00+00", "2024-01-01 15:00:00+00")
	h.expectRows(`SELECT CAST(`+c+` AS VARCHAR) FROM it_unklit ORDER BY id`, "2024-01-01 00:00:00+00", "2024-01-01 10:00:00+00")
}

// A quoted literal next to a bound parameter takes the parameter's type.
func TestSQLUnknownLiteralParams(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownLiterals()
	mem := memory.DefaultAllocator
	b := array.NewInt32Builder(mem)
	defer b.Release()
	b.AppendNull()
	arr := b.NewArray()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int32, Nullable: true}}, nil),
		[]arrow.Array{arr}, 1)
	defer rec.Release()
	rdr, _, err := h.stmtWith(`SELECT COALESCE(?, '0') + 1`, rec).ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Release()
	if !rdr.Next() {
		t.Fatalf("no rows: %v", rdr.Err())
	}
	col := rdr.RecordBatch().Column(0)
	if got := col.ValueStr(0); got != "1" || !arrow.TypeEqual(col.DataType(), arrow.PrimitiveTypes.Int64) {
		t.Errorf("COALESCE(?, '0') + 1 of a NULL INTEGER: %s (%s), want 1 (int64)", got, col.DataType())
	}
}
