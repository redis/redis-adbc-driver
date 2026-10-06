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

// Unit tests for quoted string literals, which take the type of the values
// next to them (unknown.go). They need no Redis server.

import (
	"strings"
	"testing"
)

// unknownTestCols are the columns the expressions of these tests read.
var unknownTestCols = map[string]ColType{"i": typeInt32, "s": {Kind: KindString, Length: 10}, "d": typeDate,
	"n": decimalType(10, 2), "b": typeBool, "tz": typeTimestampTZ, "iv": typeInterval}

// A quoted string literal has no type of its own; other literals and casts
// of one have one.
func TestUnknownLiteralFlag(t *testing.T) {
	for _, c := range []struct {
		expr    string
		unknown bool
	}{
		{"'1'", true}, {"$$1$$", true}, {"1", false}, {"1.5", false}, {"NULL", false}, {"TRUE", false},
		{"DATE '2024-01-01'", false}, {"X'01'", false}, {"CAST('1' AS VARCHAR)", false}, {"'1'::text", false},
	} {
		if got := unknownLiteral(parseTestExpr(t, c.expr)); got != c.unknown {
			t.Errorf("%s: unknownLiteral = %v, want %v", c.expr, got, c.unknown)
		}
	}
}

// A CASE, COALESCE, GREATEST, LEAST, NULLIF, IIF or LAG with quoted
// literals has the type of its other values, as in Postgres 16, and text if
// it has no others.
func TestUnknownLiteralTypes(t *testing.T) {
	for _, c := range []struct{ expr, typ string }{
		{"CASE WHEN i = 1 THEN '1' ELSE i END", "INTEGER"},
		{"CASE WHEN i = 1 THEN '1' ELSE 1 END", "BIGINT"},
		{"CASE WHEN i = 1 THEN '1' ELSE 1.5 END", "NUMERIC(2,1)"},
		{"CASE WHEN i = 1 THEN '1' END", "VARCHAR"},
		{"CASE WHEN i = 1 THEN '1' ELSE '2' END", "VARCHAR"},
		{"CASE WHEN i = 1 THEN NULL ELSE '2' END", "VARCHAR"},
		{"CASE WHEN i = 1 THEN NULL END", "NULL"},
		{"CASE i WHEN 1 THEN '5' ELSE i * 10 END", "BIGINT"},
		{"CASE WHEN i = 1 THEN d ELSE '2024-01-01' END", "DATE"},
		{"CASE WHEN i = 1 THEN n ELSE '2.5' END", "NUMERIC(10,2)"},
		// With the digits the literal needs, so that it isn't rounded.
		{"CASE WHEN i = 1 THEN n ELSE '2.567' END", "NUMERIC(38,3)"},
		{"COALESCE(n, '12345678901')", "NUMERIC(38,2)"},
		{"NULLIF('1.555', n)", "NUMERIC(38,3)"},
		{"CASE WHEN i = 1 THEN s ELSE 'abc' END", "VARCHAR"}, // the literal has no length
		{"CASE WHEN i = 1 THEN b ELSE 'false' END", "BOOLEAN"},
		{"CASE WHEN i = 1 THEN iv ELSE '2 hours' END", "INTERVAL"},
		{"CASE WHEN i = 1 THEN tz ELSE '2024-01-01' END", typeTimestampTZ.SQLName()},
		{"CASE WHEN i = 1 THEN '1' ELSE CASE WHEN i = 2 THEN i END END", "INTEGER"},
		{"COALESCE(i, '0')", "INTEGER"},
		{"COALESCE(NULL, '2', 1)", "BIGINT"},
		{"COALESCE(CASE WHEN i = 1 THEN NULL END, '2', 1)", "BIGINT"},
		{"COALESCE('a', 'b')", "VARCHAR"},
		{"GREATEST('1', '2', 3)", "BIGINT"},
		{"LEAST(d, '2024-01-01')", "DATE"},
		{"NULLIF('1', i)", "INTEGER"},
		{"NULLIF(i, '1')", "INTEGER"},
		{"NULLIF('1', '2')", "VARCHAR"},
		{"IIF(i = 1, '1', i)", "INTEGER"},
		{"LAG(i, 1, '0') OVER (ORDER BY i)", "INTEGER"},
	} {
		got, err := inferType(parseTestExpr(t, c.expr), unknownTestCols, nil)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if got.SQLName() != c.typ {
			t.Errorf("%s: %s, want %s", c.expr, got.SQLName(), c.typ)
		}
	}
}

// Typed values without a common type are an error, as in Postgres, and so
// is a quoted literal that isn't a value of the type it takes, also where no
// row reads it.
func TestUnknownLiteralErrors(t *testing.T) {
	cols := func() map[string]ColType { return unknownTestCols }
	for _, c := range []struct{ expr, err string }{
		{"CASE WHEN i = 1 THEN s ELSE 1 END", "CASE types bigint and varchar cannot be matched"},
		{"CASE WHEN i = 1 THEN CAST('1' AS VARCHAR) ELSE 1 END", "CASE types bigint and varchar cannot be matched"},
		// A CASE of literals alone is text.
		{"CASE WHEN i = 1 THEN 1 ELSE CASE WHEN i = 2 THEN '2' END END", "CASE types varchar and bigint cannot be matched"},
		{"COALESCE(CASE WHEN i = 1 THEN '1' END, 1)", "COALESCE types varchar and bigint cannot be matched"},
		{"CASE WHEN i = 1 THEN d ELSE INTERVAL '1 day' END", "CASE types interval and date cannot be matched"},
		{"GREATEST(s, 1)", "GREATEST types varchar and bigint cannot be matched"},
		{"IIF(i = 1, s, 1)", "IIF types varchar and bigint cannot be matched"},
		{"1 + COALESCE(s, i)", "COALESCE types varchar and integer cannot be matched"},
		{"CASE WHEN i = 1 THEN 'x' ELSE 1 END", `invalid input syntax for type bigint: "x"`},
		{"CASE WHEN FALSE THEN 'x' ELSE i END", `invalid input syntax for type integer: "x"`},
		{"i + CASE WHEN i = 1 THEN 'x' ELSE 1 END", `invalid input syntax for type bigint: "x"`},
		{"CASE WHEN i = 1 THEN b ELSE 'maybe' END", `cannot convert "maybe" to BOOLEAN`},
		{"COALESCE(d, 'soon')", `invalid date "soon"`},
		{"NULLIF('a', i)", `invalid input syntax for type integer: "a"`},
		{"NULLIF(i, 'a')", `invalid input syntax for type integer: "a"`},
		{"LAG(i, 1, 'x') OVER (ORDER BY i)", `invalid input syntax for type integer: "x"`},
	} {
		if err := checkUnknowns(parseTestExpr(t, c.expr), cols, nil); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: error %v, want one containing %q", c.expr, err, c.err)
		}
	}
	for _, expr := range []string{
		"CASE WHEN i = 1 THEN '1' ELSE i END", "CASE WHEN i = 1 THEN 'x' END", "COALESCE(s, 'x')",
		"CASE WHEN i = 1 THEN tz ELSE '2024-01-01 10:00' END", "NULLIF(s, 'x')", "LAG(s, 1, 'x') OVER ()",
		"CASE WHEN i = 1 THEN NULL END + 1",
	} {
		if err := checkUnknowns(parseTestExpr(t, expr), cols, nil); err != nil {
			t.Errorf("%s: %v", expr, err)
		}
	}
}

// Evaluated, a quoted literal is a value of its expression's type, so that
// one expression's values are of one kind, which arithmetic, grouping,
// DISTINCT, sorting and set operations see.
func TestUnknownLiteralValues(t *testing.T) {
	ids := `(SELECT 1 AS id UNION ALL SELECT 2) v`
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE id END FROM ` + ids, []string{"1", "2"}},
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE 1 END + 1 FROM ` + ids, []string{"2", "2"}},
		{`SELECT COALESCE(CASE WHEN id = 2 THEN id END, '0') + 1 FROM ` + ids, []string{"1", "3"}},
		{`SELECT GREATEST(CASE WHEN id = 2 THEN id END, '5'), LEAST(id, '5') FROM ` + ids, []string{"5|1", "5|2"}},
		{`SELECT NULLIF('1', id) FROM ` + ids, []string{"NULL", "1"}},
		{`SELECT IIF(id = 1, '7', id) FROM ` + ids, []string{"7", "2"}},
		{`SELECT LAG(id, 1, '0') OVER (ORDER BY id) + 1 FROM ` + ids, []string{"1", "2"}},
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE 1 END AS c, COUNT(*), COUNT(DISTINCT CASE WHEN id = 1 THEN '1' ELSE 1 END) FROM ` +
			ids + ` GROUP BY 1`, []string{"1|2|1"}},
		{`SELECT id, COUNT(*) OVER (PARTITION BY CASE WHEN id = 1 THEN '1' ELSE 1 END) FROM ` + ids, []string{"1|2", "2|2"}},
		{`SELECT CASE WHEN id = 1 THEN '10' ELSE 9 END AS c FROM ` + ids + ` ORDER BY c`, []string{"9", "10"}},
		{`SELECT CASE WHEN COUNT(*) > 1 THEN '7' ELSE 2 END + 1 FROM ` + ids, []string{"8"}},
		{`SELECT '1' UNION SELECT 1`, []string{"1"}},
		{`SELECT 3 UNION ALL SELECT '1'`, []string{"3", "1"}},
		{`SELECT x + 1 FROM (SELECT '1' AS x UNION ALL SELECT 3) s`, []string{"2", "4"}},
	} {
		stmts, err := ParseScript(c.sql)
		if err != nil {
			t.Fatalf("%s: %v", c.sql, err)
		}
		rows, err := runInMemory(stmts[0].Stmt.(*SelectStmt))
		if err != nil {
			t.Errorf("%s: %v", c.sql, err)
			continue
		}
		var got []string
		for _, r := range rows {
			cells := make([]string, len(r))
			for i, v := range r {
				switch {
				case v.Null:
					cells[i] = "NULL"
				case !v.T.Kind.isInteger():
					t.Errorf("%s: %s is a %s", c.sql, v.Text(), v.T.SQLName())
					fallthrough
				default:
					cells[i] = v.Text()
				}
			}
			got = append(got, strings.Join(cells, "|"))
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s\n got: %q\nwant: %q", c.sql, got, c.want)
		}
	}
}

// A set operation's column of quoted literals takes the other branch's
// type, and a literal that isn't one of its values is an error.
func TestUnknownLiteralSetOps(t *testing.T) {
	// A NUMERIC with the digits the literal needs.
	sql := `SELECT CAST(1.5 AS NUMERIC(10,2)) UNION ALL SELECT '2.567'`
	stmts, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := runInMemory(stmts[0].Stmt.(*SelectStmt))
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r[0].Text()+" "+r[0].T.SQLName())
	}
	if want := "1.500 NUMERIC(38,3),2.567 NUMERIC(38,3)"; strings.Join(got, ",") != want {
		t.Errorf("%s: %q, want %q", sql, got, want)
	}
	for _, c := range []struct{ sql, err string }{
		{`SELECT 'a' UNION SELECT 3`, `invalid input syntax for type bigint: "a"`},
		{`SELECT 'a' WHERE FALSE UNION SELECT 3`, `invalid input syntax for type bigint: "a"`},
		// The first UNION's column is text.
		{`SELECT '1' UNION SELECT '2' UNION SELECT 3`, "types VARCHAR and BIGINT cannot be matched"},
	} {
		stmts, err := ParseScript(c.sql)
		if err != nil {
			t.Fatalf("%s: %v", c.sql, err)
		}
		if _, err := runInMemory(stmts[0].Stmt.(*SelectStmt)); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: error %v, want one containing %q", c.sql, err, c.err)
		}
	}
}
