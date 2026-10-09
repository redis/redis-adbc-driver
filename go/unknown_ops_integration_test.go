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

// Integration tests for quoted string literals that are operands of
// arithmetic operators, which take a type from the operator and the other
// operand (unknown.go). The results are Postgres 16's, but for the
// driver's BIGINT integer literals and results (Postgres: integer and
// smallint), the scale of COALESCE(n, '0') of a NUMERIC(10,2) (1.00 where
// Postgres shows 1), its type names in errors (varchar, timestamp) and its
// messages for text that isn't a value of the literal's type (only their
// SQLSTATEs and the text are checked).

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func (h *sqlHarness) setupUnknownOperands() {
	h.dropTables("it_unkop")
	h.exec(`CREATE TABLE it_unkop (id INTEGER, g INTEGER, i2 SMALLINT, n NUMERIC(10,2), f DOUBLE PRECISION, b BOOLEAN,
		s VARCHAR(10), d DATE, tm TIME, ts TIMESTAMP, tz TIMESTAMPTZ, iv INTERVAL)`)
	h.exec(`INSERT INTO it_unkop VALUES
		(1, 1, 2, 1.25, 2.5, TRUE, 'a', '2024-01-31', '10:00:00', '2024-01-31 10:00:00', '2024-01-31 10:00:00+00', '1 day'),
		(2, 1, NULL, 2.50, NULL, NULL, NULL, '2024-02-01', NULL, '2024-02-01 10:00:00', NULL, NULL),
		(3, 2, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
}

// A quoted literal next to a value of type T is a T where the operator
// takes two, so that id + '1' adds 1, and otherwise the type of the one
// form the operator has for T: ts + '1 day' adds an interval.
func TestSQLUnknownOperands(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownOperands()
	i32 := arrow.PrimitiveTypes.Int32
	i64, f64, iv := arrow.PrimitiveTypes.Int64, arrow.PrimitiveTypes.Float64, arrow.FixedWidthTypes.MonthDayNanoInterval
	str := arrow.BinaryTypes.String
	for _, c := range []struct {
		sql  string
		typ  arrow.DataType // of the last column
		want []string
	}{
		{`SELECT '1' + 1`, i32, []string{"2"}},
		{`SELECT id + '1' FROM it_unkop ORDER BY id`, i32, []string{"2", "3", "4"}},
		{`SELECT '1' + id FROM it_unkop ORDER BY id`, i32, []string{"2", "3", "4"}},
		{`SELECT id, '10' - id, id * '-2', id / '2' FROM it_unkop ORDER BY id`, i32, []string{"1|9|-2|0", "2|8|-4|1", "3|7|-6|1"}},
		{`SELECT i2 + '1' FROM it_unkop WHERE id = 1`, arrow.PrimitiveTypes.Int16, []string{"3"}},
		{`SELECT f - '0.5', f + 'Infinity' FROM it_unkop WHERE id = 1`, f64, []string{"2|+Inf"}},
		// A NUMERIC literal keeps its own digits.
		{`SELECT n + '1.005', n * '2' FROM it_unkop ORDER BY id`, &arrow.Decimal128Type{Precision: 38, Scale: 2},
			[]string{"2.255|2.50", "3.505|5.00", "NULL|NULL"}},
		{`SELECT ' 1 ' + 1, '1e2' + 1.5`, &arrow.Decimal128Type{Precision: 38, Scale: 1}, []string{"2|101.5"}},
		{`SELECT d - '2024-01-01', '2024-03-01' - d FROM it_unkop ORDER BY id`, i64, []string{"30|30", "31|29", "NULL|NULL"}},
		{`SELECT CAST(ts + '1 day 02:00' AS VARCHAR), CAST(ts - '2024-01-01' AS VARCHAR) FROM it_unkop ORDER BY id`, str,
			[]string{"2024-02-01 12:00:00|30 days 10:00:00", "2024-02-02 12:00:00|31 days 10:00:00", "NULL|NULL"}},
		{`SELECT CAST(tz + '1 day' AS VARCHAR), CAST(tz - '2024-01-01 00:00:00+00' AS VARCHAR) FROM it_unkop WHERE id = 1`, str,
			[]string{"2024-02-01 10:00:00+00|30 days 10:00:00"}},
		{`SELECT CAST(tm + '01:30' AS VARCHAR), CAST(tm - '01:00' AS VARCHAR) FROM it_unkop WHERE id = 1`, str,
			[]string{"11:30:00|09:00:00"}},
		{`SELECT iv + '1 hour', iv * '2', '2' * iv, iv / '2', '3 days' - iv FROM it_unkop WHERE id = 3`, iv,
			[]string{"NULL|NULL|NULL|NULL|NULL"}},
		{`SELECT CAST(iv + '1 hour' AS VARCHAR), CAST(iv * '2' AS VARCHAR), CAST(iv / '2' AS VARCHAR) FROM it_unkop WHERE id = 1`, str,
			[]string{"1 day 01:00:00|2 days|12:00:00"}},
		// Nested, and next to a CASE whose literal takes its type.
		{`SELECT (id + '1') * '2', '1' - (id + '1') + '2' FROM it_unkop ORDER BY id`, i32, []string{"4|1", "6|0", "8|-1"}},
		{`SELECT CASE WHEN id = 1 THEN '5' ELSE id END + '1' FROM it_unkop ORDER BY id`, i32, []string{"6", "3", "4"}},
		{`SELECT COALESCE(n, '0') + '1' FROM it_unkop ORDER BY id`, &arrow.Decimal128Type{Precision: 38, Scale: 2},
			[]string{"2.25", "3.50", "1.00"}},
		// Grouping, aggregates, HAVING, ORDER BY, windows and subqueries.
		{`SELECT g + '1' AS k, COUNT(*) FROM it_unkop GROUP BY g + '1' ORDER BY k`, i64, []string{"2|2", "3|1"}},
		{`SELECT g, SUM(id) + '1' FROM it_unkop GROUP BY g HAVING SUM(id) + '0' > 2 ORDER BY g`, i64, []string{"1|4", "2|4"}},
		{`SELECT g FROM it_unkop GROUP BY g HAVING MAX(id) * '1' > 2`, arrow.PrimitiveTypes.Int32, []string{"2"}},
		{`SELECT MAX(id + '1'), SUM(id * '10'), COUNT(DISTINCT g * '0') FROM it_unkop`, i64, []string{"4|60|1"}},
		{`SELECT id FROM it_unkop ORDER BY id * '-1'`, arrow.PrimitiveTypes.Int32, []string{"3", "2", "1"}},
		{`SELECT id, SUM(id) OVER (ORDER BY id) + '10' FROM it_unkop ORDER BY id`, i64, []string{"1|11", "2|13", "3|16"}},
		{`SELECT id, (SELECT MAX(x.id) + '1' FROM it_unkop x WHERE x.g = it_unkop.g) FROM it_unkop ORDER BY id`, i32,
			[]string{"1|3", "2|3", "3|4"}},
		// WHERE, also as an index query.
		{`SELECT id FROM it_unkop WHERE id = '1' + 1`, arrow.PrimitiveTypes.Int32, []string{"2"}},
		{`SELECT id FROM it_unkop WHERE id + '1' = 3`, arrow.PrimitiveTypes.Int32, []string{"2"}},
		{`SELECT id FROM it_unkop WHERE ts >= TIMESTAMP '2024-01-31 00:00:00' + '1 day' ORDER BY id`, arrow.PrimitiveTypes.Int32, []string{"2"}},
		{`SELECT id FROM it_unkop WHERE d - '2024-01-01' = 31`, arrow.PrimitiveTypes.Int32, []string{"2"}},
	} {
		schema := h.expectRows(c.sql, c.want...)
		h.expectType(schema, c.sql, schema.NumFields()-1, c.typ)
	}
	for sql, want := range map[string]string{
		`SELECT id FROM it_unkop WHERE id = '1' + 1`:   "@id:[2 2]",
		`SELECT id FROM it_unkop WHERE id > '0' * 5`:   "@id:[(0 +inf]",
		`SELECT id FROM it_unkop WHERE id = 2 - '1'`:   "@id:[1 1]",
		`SELECT id FROM it_unkop WHERE n = '1' + 0.25`: "@n:[1.25 1.25]",
	} {
		if q, residual, _ := h.planOf(sql); q != want || residual {
			t.Errorf("%s: query %q, residual %v; want %s and none", sql, q, residual, want)
		}
	}
}

// The operator's errors, and the literal's, are reported when the statement
// is planned, also if no row reads them, with Postgres's SQLSTATEs.
func TestSQLUnknownOperandErrors(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownOperands()
	const unique, exist, text, rng = "42725", "42883", "22P02", "22003"
	arg, absent, data := adbc.StatusInvalidArgument, adbc.StatusNotFound, adbc.StatusInvalidData
	for _, c := range []struct {
		sql, state string
		code       adbc.Status
		msg        string
	}{
		{`SELECT '1' + '1'`, unique, arg, "operator is not unique: unknown + unknown"},
		{`SELECT NULL + '1'`, unique, arg, "operator is not unique: unknown + unknown"},
		{`SELECT -'1'`, unique, arg, "operator is not unique: - unknown"},
		{`SELECT d + '1 day' FROM it_unkop`, unique, arg, "operator is not unique: date + unknown"},
		{`SELECT d + '1' FROM it_unkop WHERE FALSE`, unique, arg, "operator is not unique: date + unknown"},
		{`SELECT id FROM it_unkop WHERE d + '1' > d`, unique, arg, "operator is not unique: date + unknown"},
		{`SELECT b + '1' FROM it_unkop`, exist, absent, "operator does not exist: boolean + unknown"},
		{`SELECT s - '1' FROM it_unkop`, exist, absent, "operator does not exist: varchar - unknown"},
		{`SELECT ts * '2' FROM it_unkop`, exist, absent, "operator does not exist: timestamp * unknown"},
		{`SELECT '2' / iv FROM it_unkop`, exist, absent, "operator does not exist: unknown / interval"},
		{`SELECT ROW(1, 2) + '1'`, exist, absent, "operator does not exist: record + unknown"},
		// The literal's own errors, with the driver's messages.
		{`SELECT 'x' + 1`, text, data, `"x"`},
		{`SELECT id + 'x' FROM it_unkop WHERE FALSE`, text, data, `"x"`},
		{`SELECT i2 + '40000' FROM it_unkop`, rng, data, `40000`},
		{`SELECT ts - '1 day' FROM it_unkop`, "22007", data, `"1 day"`},
		{`UPDATE it_unkop SET g = g + 'x' WHERE id = 99`, text, data, `"x"`},
		{`DELETE FROM it_unkop WHERE id = '2' + '2'`, unique, arg, "operator is not unique: unknown + unknown"},
	} {
		checkState(t, c.sql, h.stmtErr(c.sql, nil), c.code, c.state, c.msg)
	}
	h.expectRows(`SELECT COUNT(*), SUM(g) FROM it_unkop`, "3|4")
}

// INSERT, UPDATE, CREATE TABLE AS, views, DEFAULTs and CHECKs read the
// literal as its type; a view reads a literal that is a timestamp with time
// zone in the zone it was created in, as Postgres does.
func TestSQLUnknownOperandWrites(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownOperands()
	h.dropViews("it_unkop_v")
	h.dropTables("it_unkop_ctas", "it_unkop_def")
	h.exec(`INSERT INTO it_unkop (id, g) VALUES ('2' + 2, '5' * 2)`)
	h.exec(`UPDATE it_unkop SET g = g + '10' WHERE id = 3`)
	h.expectRows(`SELECT id, g FROM it_unkop WHERE id >= 3 ORDER BY id`, "3|12", "4|10")

	h.exec(`CREATE TABLE it_unkop_ctas AS SELECT id + '1' AS a, n * '2' AS b, ts + '1 day' AS c, d - '2024-01-01' AS e FROM it_unkop`)
	h.expectRows(`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'it_unkop_ctas' ORDER BY ordinal_position`,
		"a|INTEGER", "b|NUMERIC(38,2)", "c|TIMESTAMP(6)", "e|BIGINT")
	h.expectRows(`SELECT a, b, CAST(c AS VARCHAR), e FROM it_unkop_ctas ORDER BY a`,
		"2|2.50|2024-02-01 10:00:00|30", "3|5.00|2024-02-02 10:00:00|31", "4|NULL|NULL|NULL", "5|NULL|NULL|NULL")

	h.exec(`CREATE TABLE it_unkop_def (id INTEGER, v INTEGER DEFAULT '1' + 1, CHECK (v + '0' < 100))`)
	h.exec(`INSERT INTO it_unkop_def (id) VALUES (1)`)
	h.expectRows(`SELECT v FROM it_unkop_def`, "2")
	checkState(t, "CHECK", h.stmtErr(`INSERT INTO it_unkop_def VALUES (2, 150)`, nil), adbc.StatusIntegrity, "23514", "violates check constraint")

	h.exec(`SET TIME ZONE 'America/New_York'`)
	t.Cleanup(func() { h.exec(`SET TIME ZONE 'UTC'`) })
	h.expectRows(`SELECT CAST(tz - '2024-01-01 00:00' AS VARCHAR), CAST(tz + '1 hour' AS VARCHAR) FROM it_unkop WHERE id = 1`,
		"30 days 05:00:00|2024-01-31 06:00:00-05")
	h.exec(`CREATE VIEW it_unkop_v AS SELECT id, tz - '2024-01-01 00:00' AS diff FROM it_unkop`)
	h.exec(`SET TIME ZONE 'UTC'`)
	h.expectRows(`SELECT CAST(diff AS VARCHAR) FROM it_unkop_v WHERE id = 1`, "30 days 05:00:00")
	h.expectRows(`SELECT CAST(tz - '2024-01-01 00:00' AS VARCHAR) FROM it_unkop WHERE id = 1`, "30 days 10:00:00")
}

// A quoted literal next to a bound parameter takes the parameter's type,
// and GetParameterSchema gives a parameter next to a value of type T the
// type a literal would have.
func TestSQLUnknownOperandParams(t *testing.T) {
	h := newSQLHarness(t)
	h.setupUnknownOperands()
	mem := memory.DefaultAllocator
	b := array.NewInt32Builder(mem)
	defer b.Release()
	b.Append(5)
	arr := b.NewArray()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int32, Nullable: true}}, nil),
		[]arrow.Array{arr}, 1)
	defer rec.Release()
	for sql, want := range map[string]string{
		`SELECT ? + '1'`:  "6",
		`SELECT '10' - ?`: "5",
		`SELECT COUNT(*) FROM it_unkop WHERE id < ? - '2'`: "2",
	} {
		rdr, _, err := h.stmtWith(sql, rec).ExecuteQuery(h.ctx)
		if err != nil {
			t.Errorf("%s: %v", sql, err)
			continue
		}
		if !rdr.Next() {
			t.Errorf("%s: no rows: %v", sql, rdr.Err())
		} else if got := rdr.RecordBatch().Column(0).ValueStr(0); got != want {
			t.Errorf("%s of 5: %s, want %s", sql, got, want)
		}
		rdr.Release()
	}
	checkState(t, "? + 'x'", h.stmtErr(`SELECT ? + 'x'`, rec), adbc.StatusInvalidData, "22P02", `"x"`)

	schema, err := h.conn.GetTableSchema(h.ctx, nil, nil, "it_unkop")
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]string{}
	for _, f := range schema.Fields() {
		col[f.Name] = f.Type.String()
	}
	null, ivt := arrow.Null.String(), arrow.FixedWidthTypes.MonthDayNanoInterval.String()
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{`SELECT id * $1 FROM it_unkop`, []string{col["id"]}},
		{`SELECT ts + $1, $2 + ts FROM it_unkop`, []string{ivt, ivt}},
		{`SELECT ts - $1 FROM it_unkop`, []string{col["ts"]}},
		{`SELECT d - $1 FROM it_unkop`, []string{col["d"]}},
		{`SELECT iv * $1 FROM it_unkop`, []string{arrow.PrimitiveTypes.Float64.String()}},
		// No form or more than one, and two values without a type.
		{`SELECT d + $1, b + $2 FROM it_unkop`, []string{null, null}},
		{`SELECT $1 + '1'`, []string{null}},
	} {
		got, err := h.paramTypes(c.sql)
		if err != nil {
			t.Errorf("%s: %v", c.sql, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", c.sql, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: parameter %d is %s, want %s", c.sql, i+1, got[i], c.want[i])
			}
		}
	}
}
