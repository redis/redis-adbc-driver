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
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// recordRows is a relation with NULLs in either column, for sorting and
// grouping on (a, b).
const recordRows = "WITH t(id, a, b) AS (SELECT 1, 1, 'x' UNION ALL SELECT 2, 1, NULL UNION ALL SELECT 3, NULL, 'y' " +
	"UNION ALL SELECT 4, NULL, NULL UNION ALL SELECT 5, 2, 'a' UNION ALL SELECT 6, 1, 'x') "

// Rows as values, with the results Postgres 16 gives.
func TestRecordValues(t *testing.T) {
	cases := []struct{ q, want string }{
		// Text, as record_out writes it.
		{"SELECT (1, 2), ROW(1, 'x'), ROW(), ROW(1)", "(1,2)|(1,x)|()|(1)"},
		{`SELECT (1, 'a b', NULL, '', 'x"y', 'a,b', 'p\q', '(x)')`, `(1,"a b",,"","x""y","a,b","p\\q","(x)")`},
		{"SELECT ((1, 2), 3), ROW(ROW(1, 2), ROW(NULL, NULL)), ((1, 'a b'), 'c')",
			`("(1,2)",3)|("(1,2)","(,)")|("(1,""a b"")",c)`},
		{"SELECT (1, 2.50, true, DATE '2024-01-01', TIMESTAMP '2024-01-01 10:00', INTERVAL '1 day 2 hours', " +
			"TIME '10:00:00.5', X'616263', CAST('x' AS CHAR(3)), TIMESTAMPTZ '2024-01-01 10:00:00+05')",
			`(1,2.50,t,2024-01-01,"2024-01-01 10:00:00","1 day 02:00:00",10:00:00.5,"\\x616263","x  ","2024-01-01 05:00:00+00")`},
		{"SELECT ROW(1, 2)::text, CAST(ROW(1, 2) AS VARCHAR(3)), ROW(1, 2)::record, CAST(NULL AS RECORD), length(ROW(1, 2)::text)",
			"(1,2)|(1,|(1,2)|NULL|5"},
		{"SELECT 'x' || ROW(1, 2), ROW(1, 2) || 'x', concat(ROW(1, 'a b'), '!'), 'a' || ROW(NULL, 'b')", `x(1,2)|(1,2)x|(1,"a b")!|a(,b)`},
		// Fields: f1, f2, …
		{"SELECT (ROW(1, 2)).f1, (ROW(1, ROW(2, 3))).f2, (ROW(1, 'a')).F2, (ROW(1, NULL)).f2 IS NULL, (ROW(1, 2)).f1 + 1",
			"1|(2,3)|a|true|2"},
		{"SELECT (s.r).f2, (r).f1 FROM (SELECT ROW(1, 'x') r) s", "x|1"},
		// IS [NOT] NULL: every field NULL / none; a field that is a record
		// is not NULL.
		{"SELECT ROW(1, 2) IS NULL, ROW(NULL, NULL) IS NULL, ROW(1, NULL) IS NULL, ROW(1, NULL) IS NOT NULL, " +
			"ROW(NULL, NULL) IS NOT NULL, ROW() IS NULL, ROW() IS NOT NULL, ROW(NULL) IS NULL",
			"false|true|false|false|false|true|true|true"},
		{"SELECT ROW(ROW(NULL, NULL), NULL) IS NULL, ROW(ROW(NULL, 1), NULL) IS NULL, ROW(ROW(NULL, NULL), 1) IS NOT NULL, " +
			"ROW(ROW(NULL, 1), 1) IS NOT NULL",
			"false|false|true|true"},
		{"WITH s(r) AS (SELECT ROW(1, NULL) UNION ALL SELECT ROW(NULL, NULL) UNION ALL SELECT NULL UNION ALL SELECT ROW(1, 2)) " +
			"SELECT r, r IS NULL, r IS NOT NULL, (r, 1) IS NULL, (r, 1) IS NOT NULL FROM s",
			"(1,)|false|false|false|true\n(,)|true|false|false|true\nNULL|true|false|false|false\n(1,2)|false|true|false|true"},
		// Nested rows compare as records: a NULL field equals a NULL field
		// and is greater than any value.
		{"SELECT ROW(ROW(1, 2), 3) = ROW(ROW(1, 2), 3), ROW(ROW(1, NULL), 3) = ROW(ROW(1, NULL), 3), " +
			"ROW(ROW(1, NULL), 3) = ROW(ROW(1, 2), 3), ROW(ROW(1, NULL), 3) = ROW(ROW(2, NULL), 3)",
			"true|true|false|false"},
		{"SELECT ROW(ROW(1, NULL), 3) < ROW(ROW(2, NULL), 3), ROW(ROW(1, NULL), 3) < ROW(ROW(1, 2), 3), " +
			"ROW(ROW(1, NULL), 3) > ROW(ROW(1, 2), 3), ROW(ROW(1, NULL), 3) IS DISTINCT FROM ROW(ROW(1, NULL), 3), " +
			"((1, NULL), 3) IN (((1, NULL), 3)), ((1, 2), 3) IN (SELECT ROW(1, 2), 3), ROW(ROW(), 1) = ROW(ROW(), 1)",
			"true|false|true|false|true|true|true"},
		// Rows compared with rows are still compared item by item, NULL when
		// an item is.
		{"SELECT (1, NULL) = (1, NULL), ROW(1, NULL) IN (ROW(1, NULL)), ROW(1, 2) <> ROW(1, NULL), ROW(ROW(1, 2)) <> ROW(ROW(1, NULL))",
			"NULL|NULL|NULL|true"},
		// Record values compared with each other and with rows.
		{"WITH t(r) AS (SELECT ROW(1, NULL)) SELECT r = r, r = ROW(1, NULL), r <> (1, 2), r < ROW(2, NULL), r < ROW(1, 2), " +
			"r IS DISTINCT FROM ROW(1, NULL), r = NULL, r IS DISTINCT FROM NULL FROM t",
			"true|true|true|true|false|false|NULL|true"},
		{"SELECT (SELECT ROW(1, 2)) < (SELECT ROW(2)), (SELECT ROW(1, NULL)) IN (SELECT ROW(1, NULL)), ROW('a'::char(3)) = ROW('a'::varchar)",
			"true|true|true"},
		// Functions that take any type.
		{"SELECT COALESCE(NULL, ROW(1, 2)), NULLIF(ROW(1, 2), ROW(1, 2)), NULLIF(ROW(1, NULL), ROW(1, NULL)), " +
			"GREATEST(ROW(1, 2), ROW(1, 3), ROW(1, NULL)), LEAST(ROW(1, 2), ROW(1, NULL)), IIF(false, ROW(1), ROW(2))",
			"(1,2)|NULL|NULL|(1,)|(1,2)|(2)"},
		{"SELECT CASE WHEN true THEN ROW(1, 2) ELSE ROW(3, 4) END, CASE ROW(1, 2) WHEN ROW(1, 2) THEN 'y' ELSE 'n' END, " +
			"CASE WHEN false THEN ROW(1, 2) END IS NULL",
			"(1,2)|y|true"},
		{"SELECT to_json(ROW(1, 'a', NULL)), to_json(ROW(ROW(1, 2), 'a')), to_jsonb(ROW(1, 'a')), json_build_object('k', ROW(1, 2))",
			`{"f1":1,"f2":"a","f3":null}|{"f1":{"f1":1,"f2":2},"f2":"a"}|{"f1": 1, "f2": "a"}|{"k" : {"f1":1,"f2":2}}`},
		// Set operations compare records with NULL fields equal.
		{"SELECT (1, 2) UNION SELECT (1, 2) UNION SELECT (1, NULL) UNION SELECT (1, NULL) ORDER BY 1", "(1,2)\n(1,)"},
		{"SELECT ROW(1, NULL) INTERSECT SELECT ROW(1, NULL)", "(1,)"},
		{"SELECT (1, 'a') UNION ALL SELECT (2, 3)", "(1,a)\n(2,3)"},
	}
	for _, c := range cases {
		if got := memQuery(c.q); got != c.want {
			t.Errorf("%s\n got: %q\nwant: %q", c.q, got, c.want)
		}
	}
}

// Sorting, DISTINCT, GROUP BY and windows on records: field by field, NULL
// fields last in ascending order and first in descending, as Postgres sorts
// records (other NULLs sort last either way).
func TestRecordOrder(t *testing.T) {
	cases := []struct{ q, want string }{
		{recordRows + "SELECT id FROM t ORDER BY (a, b), id", "1\n6\n2\n5\n3\n4"},
		{recordRows + "SELECT id FROM t ORDER BY (a, b) DESC, id", "4\n3\n5\n2\n1\n6"},
		{recordRows + "SELECT id FROM t ORDER BY ROW(a, b) NULLS FIRST, id", "1\n6\n2\n5\n3\n4"},
		{recordRows + "SELECT id, (a, b) FROM t ORDER BY 2 DESC, 1 LIMIT 2", "4|(,)\n3|(,y)"},
		// NULL records: last ascending, first descending, as in Postgres.
		{"WITH s(r) AS (SELECT ROW(1, NULL) UNION ALL SELECT ROW(1, 2) UNION ALL SELECT NULL UNION ALL SELECT ROW(0, 5)) " +
			"SELECT r FROM s ORDER BY r", "(0,5)\n(1,2)\n(1,)\nNULL"},
		{"WITH s(r) AS (SELECT ROW(1, NULL) UNION ALL SELECT ROW(1, 2) UNION ALL SELECT NULL UNION ALL SELECT ROW(0, 5)) " +
			"SELECT r FROM s ORDER BY r DESC", "NULL\n(1,)\n(1,2)\n(0,5)"},
		{"WITH s(r) AS (SELECT ROW(1, NULL) UNION ALL SELECT ROW(1, 2) UNION ALL SELECT NULL UNION ALL SELECT ROW(0, 5)) " +
			"SELECT r FROM s ORDER BY r DESC NULLS LAST", "(1,)\n(1,2)\n(0,5)\nNULL"},
		{"SELECT ROW(1, 'x') UNION SELECT ROW(1, 'x') UNION SELECT ROW(2, 'y') UNION SELECT NULL ORDER BY 1 DESC", "NULL\n(2,y)\n(1,x)"},
		// DISTINCT and GROUP BY: NULL fields are equal.
		{recordRows + "SELECT DISTINCT (a, b) FROM t ORDER BY 1", "(1,x)\n(1,)\n(2,a)\n(,y)\n(,)"},
		{recordRows + "SELECT (a, b), count(*) FROM t GROUP BY ROW(a, b) ORDER BY 1", "(1,x)|2\n(1,)|1\n(2,a)|1\n(,y)|1\n(,)|1"},
		// GROUP BY (a, b) groups by a and b, as in Postgres.
		{recordRows + "SELECT (a, b), count(*) FROM t GROUP BY (a, b) ORDER BY (a, b) DESC", "(,)|1\n(,y)|1\n(2,a)|1\n(1,)|1\n(1,x)|2"},
		{recordRows + "SELECT r, count(*) FROM (SELECT (a, b) AS r FROM t) s GROUP BY r ORDER BY r DESC",
			"(,)|1\n(,y)|1\n(2,a)|1\n(1,)|1\n(1,x)|2"},
		{recordRows + "SELECT (r).f1, (r).f2, count(*) FROM (SELECT ROW(a, b) AS r FROM t) s GROUP BY r ORDER BY r",
			"1|x|2\n1|NULL|1\n2|a|1\nNULL|y|1\nNULL|NULL|1"},
		{recordRows + "SELECT count(ROW(a, b)), count(DISTINCT ROW(a, b)), string_agg(ROW(a, b)::text, ';' ORDER BY id) FROM t",
			"6|5|(1,x);(1,);(,y);(,);(2,a);(1,x)"},
		{recordRows + "SELECT DISTINCT ON ((a, b)) id FROM t ORDER BY (a, b), id DESC", "6\n2\n5\n3\n4"},
		// Windows.
		{recordRows + "SELECT id, row_number() OVER (PARTITION BY (a, b) ORDER BY id), " +
			"row_number() OVER (ORDER BY (a, b) DESC, id) FROM t ORDER BY id",
			"1|1|5\n2|1|4\n3|1|2\n4|1|1\n5|1|3\n6|2|6"},
		// A record column of a derived table, in WHERE and a join.
		{recordRows + "SELECT id FROM (SELECT id, (a, b) AS r FROM t) s WHERE r = (1, NULL) OR r IS NULL ORDER BY id", "2\n4"},
		{recordRows + "SELECT x.id, y.id FROM (SELECT id, (a, b) AS r FROM t) x JOIN (SELECT id, (a, b) AS r FROM t) y " +
			"ON x.r = y.r AND x.id < y.id ORDER BY 1, 2",
			"1|6"},
		{recordRows + "SELECT id FROM (SELECT id, (a, b) AS r FROM t) s WHERE r IN (SELECT ROW(1, NULL) UNION ALL SELECT ROW(NULL, 'y')) ORDER BY id",
			"2\n3"},
	}
	for _, c := range cases {
		if got := memQuery(c.q); got != c.want {
			t.Errorf("%s\n got: %q\nwant: %q", c.q, got, c.want)
		}
	}
}

// A record is an Arrow struct whose children f1 … fn are nullable and have
// the fields' types; a NULL record is a NULL struct, unlike one whose
// fields are NULL. The column of a row constructor is named "row", and that
// of a field its name.
func TestRecordArrow(t *testing.T) {
	stmts, err := ParseScript("SELECT (1, 'x'), ROW(1)::record AS c, ROW(ROW(2.5, NULL), CAST(NULL AS DATE)) AS n, " +
		"CASE WHEN false THEN ROW(1, 'y') END AS z, ROW() AS e, (ROW(1, 'x')).f2")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e := &executor{cache: newExecCache()}
	plan, err := e.planSelect(ctx, stmts[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.runSelect(ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := buildRecord(memory.DefaultAllocator, plan.columns(), rows)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Release()
	wantSchema := "schema:\n  fields: 6\n" +
		"    - row: type=struct<f1: int32 nullable, f2: utf8 nullable>, nullable\n" +
		"    - c: type=struct<f1: int32 nullable>, nullable\n" +
		"    - n: type=struct<f1: struct<f1: decimal(2, 1) nullable, f2: null nullable> nullable, f2: date32 nullable>, nullable\n" +
		"    - z: type=struct<f1: int32 nullable, f2: utf8 nullable>, nullable\n" +
		"    - e: type=struct<>, nullable\n" +
		"    - f2: type=utf8, nullable"
	if got := rec.Schema().String(); got != wantSchema {
		t.Errorf("schema:\n got: %s\nwant: %s", got, wantSchema)
	}
	row := rec.Column(0).(*array.Struct)
	if row.IsNull(0) || row.Field(0).(*array.Int32).Value(0) != 1 || row.Field(1).(*array.String).Value(0) != "x" {
		t.Errorf("(1, 'x') is %v", row)
	}
	n := rec.Column(2).(*array.Struct)
	inner := n.Field(0).(*array.Struct)
	if n.IsNull(0) || inner.IsNull(0) || inner.Field(1).DataType().ID() != arrow.NULL || !n.Field(1).IsNull(0) {
		t.Errorf("ROW(ROW(2.5, NULL), NULL) is %v", n)
	}
	if got := inner.Field(0).(*array.Decimal128).Value(0).BigInt().String(); got != "25" {
		t.Errorf("2.5 is %s", got)
	}
	z := rec.Column(3).(*array.Struct)
	if !z.IsNull(0) || !z.Field(0).IsNull(0) {
		t.Errorf("a NULL record is %v", z)
	}
	if rec.Column(4).IsNull(0) || rec.Column(4).DataType().(*arrow.StructType).NumFields() != 0 {
		t.Errorf("ROW() is %v", rec.Column(4))
	}
	if got := rec.Column(5).(*array.String).Value(0); got != "x" {
		t.Errorf("(ROW(1, 'x')).f2 is %q", got)
	}
}

// Where records can't go, with Postgres's errors.
func TestRecordStorageErrors(t *testing.T) {
	for _, c := range []struct{ sql, want string }{
		{"CREATE TABLE t (r RECORD)", `column "r" has pseudo-type record`},
		{"ALTER TABLE t ADD COLUMN r record", `column "r" has pseudo-type record`},
	} {
		if _, err := ParseScript(c.sql); err == nil || errText(err) != c.want {
			t.Errorf("%s: got %v, want %s", c.sql, err, c.want)
		}
	}
	if _, err := parseTypeText("RECORD"); err == nil {
		t.Error("RECORD is a column type")
	}
	if _, err := colTypeFromArrow(arrow.StructOf(arrow.Field{Name: "f1", Type: arrow.PrimitiveTypes.Int64})); err == nil {
		t.Error("an Arrow struct is a column type")
	}
}
