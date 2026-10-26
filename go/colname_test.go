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
	"strings"
	"testing"
)

// The names Postgres gives unaliased output columns (FigureColname): pg16's
// column names for SELECT <expr> FROM t, with t(v INTEGER, s TEXT, d DATE,
// ts TIMESTAMPTZ, iv INTERVAL) and t1(x INTEGER).
func TestFigureColname(t *testing.T) {
	const with = "WITH t(v, s, d, ts, iv) AS (SELECT 1, 'a', DATE '2020-01-01', TIMESTAMPTZ '2020-01-01 00:00:00+00', " +
		"INTERVAL '1 day'), t1(x) AS (SELECT 1) "
	cases := []struct{ expr, want string }{
		// A column, also in parentheses.
		{"v", "v"},
		{"t.v", "v"},
		{"((v))", "v"},
		// A cast: its operand's name if it has one, else its type's.
		{"CAST(iv AS VARCHAR)", "iv"},
		{"iv::text", "iv"},
		{"CAST(CAST(v AS TEXT) AS INTEGER)", "v"},
		{"CAST(v + 1 AS VARCHAR)", "varchar"},
		{"CAST(1 AS INTEGER)", "int4"},
		{"1::int8", "int8"},
		{"CAST(1 AS SMALLINT)", "int2"},
		{"CAST(1 AS REAL)", "float4"},
		{"CAST(1 AS FLOAT(10))", "float4"},
		{"CAST(1 AS FLOAT)", "float8"},
		{"CAST(1 AS DOUBLE PRECISION)", "float8"},
		{"CAST(1 AS DECIMAL)", "numeric"},
		{"CAST(1 AS NUMERIC(3,1))", "numeric"},
		{"CAST(1 AS VARCHAR)", "varchar"},
		{"CAST(1 AS CHARACTER VARYING)", "varchar"},
		{"CAST(1 AS VARCHAR(3))", "varchar"},
		{"CAST(1 AS CHAR)", "bpchar"},
		{"CAST(1 AS CHAR(3))", "bpchar"},
		{"1::bpchar", "bpchar"},
		{"CAST(1 AS TEXT)", "text"},
		{"CAST(1 AS BOOLEAN)", "bool"},
		{"CAST('x' AS BYTEA)", "bytea"},
		{"CAST('2020-01-01' AS DATE)", "date"},
		{"CAST('01:00' AS TIME)", "time"},
		{"CAST('01:00' AS TIME WITH TIME ZONE)", "timetz"},
		{"CAST('2020-01-01' AS TIMESTAMP)", "timestamp"},
		{"CAST('2020-01-01' AS TIMESTAMP WITH TIME ZONE)", "timestamptz"},
		{"CAST('1 day' AS INTERVAL)", "interval"},
		{"CAST('1' AS JSON)", "json"},
		{"CAST('1' AS JSONB)", "jsonb"},
		{"CAST(s AS JSON)", "s"},
		// Typed literals are casts of their strings.
		{"DATE '2020-01-01'", "date"},
		{"TIMESTAMP '2020-01-01'", "timestamp"},
		{"TIMESTAMP(3) '2020-01-01'", "timestamp"},
		{"TIMESTAMPTZ '2020-01-01'", "timestamptz"},
		{"TIMESTAMP WITH TIME ZONE '2020-01-01 00:00:00+00'", "timestamptz"},
		{"TIME '01:00'", "time"},
		{"TIMETZ '01:00'", "timetz"},
		{"INTERVAL '1 day'", "interval"},
		{"INTERVAL '1' DAY", "interval"},
		// Other literals and operators have no name.
		{"true", "?column?"},
		{"null", "?column?"},
		{"1", "?column?"},
		{"-1", "?column?"},
		{"'a'", "?column?"},
		{"-v", "?column?"},
		{"1 + 2", "?column?"},
		{"s || 'b'", "?column?"},
		{"5 % 2", "?column?"},
		{"s LIKE 'a'", "?column?"},
		{"s NOT LIKE 'a'", "?column?"},
		{"s ILIKE 'a'", "?column?"},
		{"s SIMILAR TO 'a'", "?column?"},
		{"s ~ 'a'", "?column?"},
		{"s !~ 'a'", "?column?"},
		{"v BETWEEN 0 AND 2", "?column?"},
		{"v IN (1, 2)", "?column?"},
		{"v IS NULL", "?column?"},
		{"NOT true", "?column?"},
		{"v IS DISTINCT FROM 2", "?column?"},
		{`'{"a":1}'::json -> 'a'`, "?column?"},
		{`'{"a":1}'::json ->> 'a'`, "?column?"},
		{"s IS JSON", "?column?"},
		// Calls, also of aggregates and window functions, and the special
		// forms Postgres parses as calls.
		{"UPPER(s)", "upper"},
		{"count(*)", "count"},
		{"sum(v) FILTER (WHERE v > 0)", "sum"},
		{"string_agg(s, ',' ORDER BY s)", "string_agg"},
		{"row_number() OVER ()", "row_number"},
		{"sum(v) OVER ()", "sum"},
		{"coalesce(v, 2)", "coalesce"},
		{"nullif(v, 2)", "nullif"},
		{"greatest(v, 2)", "greatest"},
		{"mod(5, 2)", "mod"},
		{"now()", "now"},
		{"extract(year FROM d)", "extract"},
		{"date_part('year', d)", "date_part"},
		{"substring(s FROM 1 FOR 2)", "substring"},
		{"position('a' IN s)", "position"},
		{"trim(BOTH FROM s)", "btrim"},
		{"trim(LEADING FROM s)", "ltrim"},
		{"trim(TRAILING FROM s)", "rtrim"},
		{"trim(s)", "btrim"},
		{"ts AT TIME ZONE 'UTC'", "timezone"},
		{"current_date", "current_date"},
		{"current_timestamp(3)", "current_timestamp"},
		{"localtimestamp", "localtimestamp"},
		{"json_build_object('a', 1)", "json_build_object"},
		{"json_object('a': 1)", "json_object"},
		{"json_array(1, 2)", "json_array"},
		// CASE: its ELSE's name if that is a good one, else "case".
		{"CASE WHEN true THEN 1 END", "case"},
		{"CASE WHEN true THEN 1 ELSE 2 END", "case"},
		{"CASE WHEN true THEN 1 ELSE CAST(2 AS INT8) END", "case"},
		{"CASE WHEN true THEN 1 ELSE v END", "v"},
		{"CASE v WHEN 1 THEN 'a' ELSE upper(s) END", "upper"},
		// A scalar subquery is named by its column, also when that has no
		// name; EXISTS by "exists".
		{"(SELECT 1)", "?column?"},
		{"(SELECT 1 AS q)", "q"},
		{"(SELECT upper('x'))", "upper"},
		{"(SELECT x FROM t1)", "x"},
		{"(SELECT * FROM t1)", "x"},
		{"(SELECT t1.* FROM t1)", "x"},
		{"((SELECT iv FROM t))::text", "iv"},
		{"(SELECT 1 UNION SELECT 2 LIMIT 1)", "?column?"},
		{"(SELECT count(*) AS c FROM t UNION SELECT 2 LIMIT 1)", "c"},
		{"(SELECT max(t.v) FROM t1)", "max"}, // an aggregate of the outer query
		{"(SELECT max(t.v) + 1 FROM t1)", "?column?"},
		// A row, and a field of one.
		{"(v, s)", "row"},
		{"ROW(v)", "row"},
		{"CAST(ROW(v, s) AS VARCHAR)", "row"},
		{"(SELECT ROW(1, 2))", "row"},
		{"(ROW(v, s)).f2", "f2"},
		{"(ROW(v, s)).F2", "f2"},
		{"CAST((ROW(v, s)).f1 AS VARCHAR)", "f1"},
		{"CASE WHEN true THEN v ELSE (ROW(v, s)).f1 END", "f1"},
		{"COALESCE(ROW(v, s), ROW(2, 'b'))", "coalesce"},
		{"EXISTS (SELECT 1)", "exists"},
		{"NOT EXISTS (SELECT 1)", "?column?"},
		{"1 IN (SELECT 1)", "?column?"},
		{"v = ANY (SELECT 1)", "?column?"},
	}
	for _, c := range cases {
		q := with + "SELECT " + c.expr + " FROM t"
		stmts, err := ParseScript(q)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		sel := stmts[0].Stmt.(*SelectStmt)
		// A cached statement is planned again, over the bound expressions.
		for range 2 {
			e := &executor{cache: newExecCache()}
			plan, err := e.planSelect(context.Background(), sel, nil)
			if err != nil {
				t.Errorf("%s: %v", c.expr, err)
				break
			}
			if got := plan.columns()[0].Name; got != c.want {
				t.Errorf("%s: named %q, want %q", c.expr, got, c.want)
				break
			}
		}
	}
}

// A bare name in ORDER BY and DISTINCT ON is an output column's name before
// it is an input column's, also where the output column is named by its
// expression. The results and errors are pg16's for the same queries over
// tables (rows in the same order).
func TestOrderByOutputNames(t *testing.T) {
	const with = "WITH ivg(id, iv) AS (SELECT 1, INTERVAL '1 mon' UNION ALL SELECT 2, INTERVAL '30 days' " +
		"UNION ALL SELECT 3, INTERVAL '1 day 02:00:00' UNION ALL SELECT 4, INTERVAL '26 hours' " +
		"UNION ALL SELECT 5, INTERVAL '1 day' UNION ALL SELECT 6, INTERVAL '24 hours'), " +
		"n(id) AS (SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 10 UNION ALL SELECT 11) "
	const asText = "1 day\n1 day 02:00:00\n1 mon\n24:00:00\n26:00:00\n30 days"
	const asInterval = "1 day\n24:00:00\n1 day 02:00:00\n26:00:00\n1 mon\n30 days"
	ambiguous := func(clause, name string) string { return "error: " + clause + ` "` + name + `" is ambiguous` }
	cases := []struct{ q, want string }{
		// The text, not the interval: the output column is named iv.
		{"SELECT CAST(iv AS VARCHAR) FROM ivg ORDER BY iv", asText},
		{"SELECT iv::text FROM ivg ORDER BY iv DESC LIMIT 2", "30 days\n26:00:00"},
		{"SELECT CAST(iv AS VARCHAR) FROM (SELECT INTERVAL '1 mon' AS iv UNION ALL SELECT INTERVAL '26 hours') v ORDER BY iv",
			"1 mon\n26:00:00"},
		{"SELECT CAST(id AS VARCHAR) FROM n ORDER BY id", "1\n10\n11\n2"},
		{"SELECT CAST(id AS VARCHAR) FROM n GROUP BY id ORDER BY id", "1\n10\n11\n2"},
		{"SELECT upper(CAST(iv AS VARCHAR)) FROM ivg ORDER BY upper LIMIT 2", "1 DAY\n1 DAY 02:00:00"},
		{"SELECT count(*) FROM ivg ORDER BY count", "6"},
		{`SELECT 1 ORDER BY "?column?"`, "1"},
		// A qualified name, a position or an expression reads the input.
		{"SELECT CAST(iv AS VARCHAR) FROM ivg ORDER BY ivg.iv", asInterval},
		{"SELECT CAST(iv AS VARCHAR) FROM ivg ORDER BY 1", asText},
		{"SELECT CAST(id AS VARCHAR) FROM n ORDER BY id + 0", "1\n2\n10\n11"},
		{"SELECT id AS x FROM n ORDER BY x + 0", `error: column "x" does not exist in table "n"`}, // pg16 doesn't name the table
		// Columns of the same name must be the same expression.
		{"SELECT id, id FROM n ORDER BY id", "1|1\n2|2\n10|10\n11|11"},
		{"SELECT id AS x, id AS x FROM n ORDER BY x", "1|1\n2|2\n10|10\n11|11"},
		{"SELECT id + 1 AS x, id + 1 AS x FROM n ORDER BY x LIMIT 1", "2|2"},
		{"SELECT id, iv AS id FROM ivg ORDER BY id", ambiguous("ORDER BY", "id")},
		{"SELECT CAST(iv AS VARCHAR), iv FROM ivg ORDER BY iv", ambiguous("ORDER BY", "iv")},
		{"SELECT iv, CAST(iv AS VARCHAR) FROM ivg ORDER BY iv", ambiguous("ORDER BY", "iv")},
		{"SELECT *, CAST(iv AS VARCHAR) FROM ivg ORDER BY iv", ambiguous("ORDER BY", "iv")},
		{"SELECT a.id, b.id FROM n a, n b ORDER BY id", ambiguous("ORDER BY", "id")},
		{"SELECT upper('a'), lower('b') AS upper FROM ivg ORDER BY upper", ambiguous("ORDER BY", "upper")},
		{`SELECT 1, 2 ORDER BY "?column?"`, ambiguous("ORDER BY", "?column?")},
		{`SELECT 1, 1 ORDER BY "?column?"`, "1|1"},
		// A row is named row, and a field of one by the field.
		{"SELECT CAST((ROW(id, iv)).f2 AS VARCHAR) FROM ivg ORDER BY f2", asText},
		{`SELECT ROW(id) FROM n ORDER BY "row" DESC`, "(11)\n(10)\n(2)\n(1)"},
		{`SELECT (id, id), ROW(id + 1) FROM n ORDER BY "row"`, ambiguous("ORDER BY", "row")},
		// A subquery's ORDER BY names its own columns.
		{"SELECT (SELECT CAST(i.iv AS VARCHAR) FROM ivg i ORDER BY iv LIMIT 1)", "1 day"},

		// SELECT DISTINCT: ORDER BY iv is the select list's text.
		{"SELECT DISTINCT CAST(iv AS VARCHAR) FROM ivg ORDER BY iv", asText},
		{"SELECT DISTINCT iv::text FROM ivg ORDER BY iv DESC LIMIT 2", "30 days\n26:00:00"},
		{"SELECT DISTINCT CAST(iv AS VARCHAR) FROM ivg ORDER BY ivg.iv",
			"error: for SELECT DISTINCT, ORDER BY expressions must appear in select list"},
		{"SELECT DISTINCT iv FROM ivg ORDER BY CAST(iv AS VARCHAR)",
			"error: for SELECT DISTINCT, ORDER BY expressions must appear in select list"},
		// DISTINCT ON names columns as ORDER BY does: one row per text.
		{"SELECT DISTINCT ON (iv) CAST(iv AS VARCHAR), id FROM ivg ORDER BY iv, id",
			"1 day|5\n1 day 02:00:00|3\n1 mon|1\n24:00:00|6\n26:00:00|4\n30 days|2"},
		{"SELECT DISTINCT ON (ivg.iv) CAST(iv AS VARCHAR) FROM ivg ORDER BY ivg.iv", "1 day\n1 day 02:00:00\n1 mon"},
		{"SELECT DISTINCT ON (iv) CAST(iv AS VARCHAR) FROM ivg ORDER BY ivg.iv",
			"error: SELECT DISTINCT ON expressions must match initial ORDER BY expressions"},
		{"SELECT DISTINCT ON (f1) CAST((ROW(iv, id)).f1 AS VARCHAR), id FROM ivg ORDER BY f1, id DESC",
			"1 day|5\n1 day 02:00:00|3\n1 mon|1\n24:00:00|6\n26:00:00|4\n30 days|2"},
		{"SELECT DISTINCT ON (id) id, iv AS id FROM ivg ORDER BY 1", ambiguous("DISTINCT ON", "id")},
		{"SELECT DISTINCT ON (iv) iv::text, iv FROM ivg ORDER BY iv", ambiguous("ORDER BY", "iv")},

		// A set operation's columns are named by its first branch's.
		{"SELECT CAST(iv AS VARCHAR) FROM ivg UNION ALL SELECT '0' ORDER BY iv", "0\n" + asText},
		{"SELECT count(*) FROM ivg UNION SELECT 7 ORDER BY count", "6\n7"},
		{`SELECT 1 UNION SELECT 2 ORDER BY "?column?" DESC`, "2\n1"},
		{"SELECT CAST(id AS VARCHAR) FROM n UNION SELECT '3' ORDER BY id", "1\n10\n11\n2\n3"},
		{"SELECT (ROW(CAST(id AS VARCHAR))).f1 FROM n UNION SELECT '3' ORDER BY f1", "1\n10\n11\n2\n3"},
	}
	for _, c := range cases {
		if got := memQuery(with + c.q); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.q, got, c.want)
		}
	}
}

// A bare name in GROUP BY is an input column's before it is an output
// column's, also where the output column is named by its expression, as in
// Postgres. The results and errors are pg16's for the same queries over
// tables (rows in the same order), except where noted. Each query is
// planned twice, as a cached statement is.
func TestGroupByOutputNames(t *testing.T) {
	const with = "WITH t(id, v, x) AS (SELECT 1, 'a', 1.2 UNION ALL SELECT 2, 'A', 1.4 UNION ALL SELECT 3, 'b', 2.0 " +
		"UNION ALL SELECT 4, NULL, 2.6), " +
		"ivg(id, iv) AS (SELECT 1, INTERVAL '1 mon' UNION ALL SELECT 2, INTERVAL '30 days' " +
		"UNION ALL SELECT 3, INTERVAL '1 day 02:00:00' UNION ALL SELECT 4, INTERVAL '26 hours' " +
		"UNION ALL SELECT 5, INTERVAL '1 day' UNION ALL SELECT 6, INTERVAL '24 hours'), " +
		"u(k, w) AS (SELECT 1, 'p' UNION ALL SELECT 2, 'q') "
	ambiguous := func(name string) string { return `error: GROUP BY "` + name + `" is ambiguous` }
	const aggregate = "error: aggregate functions are not allowed in GROUP BY"
	const window = "error: window functions are not allowed in GROUP BY"
	cases := []struct{ q, want string }{
		// No input column has the name: the output column of that name.
		{"SELECT upper(CAST(iv AS VARCHAR)) FROM ivg GROUP BY upper ORDER BY 1",
			"1 DAY\n1 DAY 02:00:00\n1 MON\n24:00:00\n26:00:00\n30 DAYS"},
		{"SELECT upper(v), count(*) FROM t GROUP BY upper ORDER BY 1", "A|2\nB|1\nNULL|1"},
		{"SELECT CAST(x + 0 AS INTEGER), count(*) FROM t GROUP BY int4 ORDER BY 1", "1|2\n2|1\n3|1"},
		{`SELECT id % 2, count(*) FROM t GROUP BY "?column?" ORDER BY 1`, "0|2\n1|2"},
		{"SELECT (SELECT 'k' AS z), count(*) FROM t GROUP BY z", "k|4"},
		{"SELECT (SELECT max(id) FROM t), count(*) FROM t GROUP BY max", "4|4"}, // the subquery's aggregate
		{"SELECT upper(v), count(*) FROM t GROUP BY upper HAVING count(*) > 1", "A|2"},
		{"SELECT upper(t.v), count(*) FROM t JOIN u ON t.id = u.k GROUP BY upper ORDER BY 1", "A|2"},
		{"SELECT count(*) FROM (SELECT CAST(iv AS VARCHAR) AS s FROM ivg GROUP BY s) g", "6"},
		{"SELECT upper(v), count(*) FROM t GROUP BY ROLLUP (upper) ORDER BY 1, 2", "A|2\nB|1\nNULL|1\nNULL|4"},
		{`SELECT upper(v), id % 2, count(*) FROM t GROUP BY GROUPING SETS ((upper), ("?column?")) ORDER BY 1, 2`,
			"A|NULL|2\nB|NULL|1\nNULL|0|2\nNULL|1|2\nNULL|NULL|1"},
		{"SELECT DISTINCT upper(v) FROM t ORDER BY upper", "A\nB\nNULL"},

		// An input column comes first, and isn't ambiguous with an output
		// column of its name.
		{"SELECT CAST(x AS INTEGER), count(*) FROM t GROUP BY x ORDER BY 1, 2", "1|1\n1|1\n2|1\n3|1"},
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t GROUP BY x ORDER BY 1, 2", "1|1\n1|1\n2|1\n3|1"},
		{"SELECT CAST(x AS INTEGER) AS y, count(*) FROM t GROUP BY y ORDER BY 1", "1|2\n2|1\n3|1"},
		{"SELECT upper(v) AS v, count(*) FROM t GROUP BY v ORDER BY 1, 2", "A|1\nA|1\nB|1\nNULL|1"},
		{"SELECT count(*) FROM (SELECT CAST(iv AS VARCHAR) FROM ivg GROUP BY iv) g", "3"},
		// Also in a join, where the alias came first before.
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t JOIN u ON true GROUP BY x ORDER BY 1, 2", "1|2\n1|2\n2|2\n3|2"},
		// Only this query's input: an enclosing query's column comes after.
		{"SELECT o.upper, (SELECT count(*) FROM (SELECT upper(v) FROM t GROUP BY upper) q) FROM (SELECT 1 AS upper) o", "1|3"},
		{"SELECT k, (SELECT count(*) FROM (SELECT upper(v) FROM t WHERE id > k GROUP BY upper) q) FROM u ORDER BY 1", "1|3\n2|2"},
		{"SELECT k, (SELECT count(*) FROM t GROUP BY k) FROM u ORDER BY 1", "1|4\n2|4"},
		// A name two FROM items have: the driver keeps both columns of
		// JOIN … USING (id), which Postgres merges.
		{"SELECT t.id, count(*) FROM t JOIN t t2 USING (id) GROUP BY id ORDER BY 1", "1|1\n2|1\n3|1\n4|1"},
		// A qualified name, or a name in an expression, isn't an output
		// column's.
		{"SELECT upper(v) FROM t GROUP BY t.upper", `error: column "t.upper" does not exist in table "t"`}, // pg16: column t.upper does not exist
		{"SELECT upper(v), count(*) FROM t GROUP BY lower(upper)", `error: column "upper" does not exist in table "t"`},

		// Output columns of the same name must be the same expression.
		{"SELECT upper(v), upper(v) FROM t GROUP BY upper ORDER BY 1", "A|A\nB|B\nNULL|NULL"},
		{"SELECT id % 2 AS k, id % 2 AS k, count(*) FROM t GROUP BY k ORDER BY 1", "0|0|2\n1|1|2"},
		{"SELECT upper(v), lower(v) AS upper FROM t GROUP BY upper", ambiguous("upper")},
		{"SELECT id + 1 AS k, id + 2 AS k FROM t GROUP BY k", ambiguous("k")}, // before: the last one

		// The output column may not be an aggregate or a window function.
		{"SELECT count(*) FROM t GROUP BY count", aggregate},
		{"SELECT count(*) AS c FROM t GROUP BY c", aggregate},
		{"SELECT count(*), upper(v) FROM t GROUP BY upper, count", aggregate},
		{"SELECT row_number() OVER () FROM t GROUP BY row_number", window},
		{"SELECT row_number() OVER () AS r FROM t GROUP BY r", window},

		// The driver also reads an alias in an expression; pg16: column "u"
		// does not exist.
		{"SELECT upper(v) AS u, count(*) FROM t GROUP BY lower(u) ORDER BY 1", `error: column "t.v" must appear in the GROUP BY clause or be used in an aggregate function`},
	}
	for _, c := range cases {
		stmts, err := ParseScript(with + c.q)
		if err != nil {
			t.Errorf("%s: %v", c.q, err)
			continue
		}
		sel := stmts[0].Stmt.(*SelectStmt)
		for range 2 {
			if got := memRun(sel); got != c.want {
				t.Errorf("%s:\n got %q\nwant %q", c.q, got, c.want)
				break
			}
		}
	}
}

// Derived tables and CTEs name their columns as Postgres does, and keep the
// names that several columns have: only a reference to one of those is an
// error. The names, rows and errors are pg16's.
func TestDerivedColumnNames(t *testing.T) {
	cases := []struct{ q, want string }{
		{"SELECT count FROM (SELECT count(*) FROM (SELECT 1 UNION ALL SELECT 2) v) s", "count: 2"},
		{`WITH c AS (SELECT upper('a'), 1 + 1) SELECT upper, "?column?" FROM c`, "upper|?column?: A|2"},
		{"SELECT * FROM (SELECT 1, 2) s", "?column?|?column?: 1|2"},
		{"SELECT * FROM (SELECT 1 AS a, 2 AS a) s(x)", "x|a: 1|2"},
		{"SELECT * FROM (SELECT 1, 2) s(a, a)", "a|a: 1|2"},
		{"SELECT a FROM (SELECT 1, 2) s(b, a)", "a: 2"},
		{"SELECT * FROM (SELECT 1, 2) s1 JOIN (SELECT 3, 4) s2 ON true", "?column?|?column?|?column?|?column?: 1|2|3|4"},
		{"SELECT * FROM (SELECT 1 AS a, 2 AS b) s NATURAL JOIN (SELECT 1, 2) r", "a|b|?column?|?column?: 1|2|1|2"},
		{"SELECT DISTINCT * FROM (SELECT 1, 2 UNION ALL SELECT 1, 2) s", "?column?|?column?: 1|2"},
		{`SELECT "?column?" FROM (SELECT 1, 2) s`, `error: column reference "?column?" is ambiguous`},
		{`SELECT s."?column?" FROM (SELECT 1, 2) s`, `error: column reference "?column?" is ambiguous`},
		{`SELECT * FROM (SELECT 1, 2) s ORDER BY "?column?"`, `error: ORDER BY "?column?" is ambiguous`},
		{`SELECT * FROM (SELECT 1, 2) s JOIN (SELECT 1 AS "?column?") r USING ("?column?")`,
			`error: common column name "?column?" appears more than once in left table`},
	}
	for _, c := range cases {
		if got := memNamedQuery(c.q); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.q, got, c.want)
		}
	}
}

// memNamedQuery is memQuery, with the result's column names first:
// "a|b: 1|2".
func memNamedQuery(q string) string {
	stmts, err := ParseScript(q)
	if err != nil {
		return "error: " + errText(err)
	}
	e := &executor{cache: newExecCache()}
	plan, err := e.planSelect(context.Background(), stmts[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		return "error: " + errText(err)
	}
	var names []string
	for _, c := range plan.columns() {
		names = append(names, c.Name)
	}
	return strings.Join(names, "|") + ": " + memQuery(q)
}

// A column held under a name of its own, since another one has its name
// (nameColumns), is named in errors by that name.
func TestColumnLabel(t *testing.T) {
	for key, want := range map[string]string{
		"s.x":                  "s.x",
		"t\x001.x":             "t.x",
		"s.?column?\x002":      "s.?column?",
		"t\x001.?column?\x002": "t.?column?",
		"\x00natural1.x":       "\x00natural1.x",
		"\x00agg3":             "\x00agg3",
		"a\x00":                "a\x00",
	} {
		if got := keyLabel(key); got != want {
			t.Errorf("keyLabel(%q) = %q, want %q", key, got, want)
		}
	}
}
