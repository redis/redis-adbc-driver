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
	"strings"
	"testing"
)

// Columns without an alias are named as in Postgres (FigureColname, see
// colname.go): the result's, RETURNING's, and those of derived tables,
// CTEs, views and CREATE TABLE … AS. The names, rows and errors are pg16's
// for the same tables.
func TestSQLResultColumnNames(t *testing.T) {
	h := newSQLHarness(t)
	h.dropViews("it_cn_v", "it_cn_v2")
	h.dropTables("it_cn_t", "it_cn_ctas", "it_cn_dup")
	h.exec("CREATE TABLE it_cn_t (id INTEGER, iv INTERVAL, v VARCHAR)")
	h.exec("INSERT INTO it_cn_t VALUES (1, '1 day', 'a'), (2, '26 hours', 'b'), (3, '1 mon', 'c')")

	schema := h.expectRows(`SELECT id, count(*), CAST(iv AS VARCHAR), iv::text, upper(v), v || 'x', id + 1, 1, 'a',
			CAST(id + 1 AS VARCHAR), CASE WHEN id > 1 THEN v END, CASE WHEN id > 1 THEN 'x' ELSE v END,
			CASE WHEN id > 1 THEN v ELSE 'z' END, EXISTS (SELECT 1), (SELECT max(id) FROM it_cn_t), EXTRACT(day FROM iv),
			it_cn_t.v AS w, -id, NOT true, coalesce(v, 'n'), v IS NULL, v LIKE 'a%', (SELECT 1 + 1)
		FROM it_cn_t WHERE id = 1 GROUP BY id, iv, v`,
		"1|1|1 day|1 day|A|ax|2|1|a|2|NULL|a|z|true|3|1|a|-1|false|a|false|true|2")
	if got, want := fieldNames(schema), "id|count|iv|iv|upper|?column?|?column?|?column?|?column?|varchar|case|v|case|"+
		"exists|max|extract|w|?column?|?column?|coalesce|?column?|?column?|?column?"; got != want {
		t.Errorf("column names:\n got: %s\nwant: %s", got, want)
	}
	if schema, err := h.executeSchema(`SELECT count(*), max(id), id::text FROM it_cn_t GROUP BY id`); err != nil {
		t.Errorf("ExecuteSchema: %v", err)
	} else if got := fieldNames(schema); got != "count|max|id" {
		t.Errorf("ExecuteSchema column names: %s, want count|max|id", got)
	}

	// Derived tables and CTEs, also recursive ones.
	h.expectRows(`SELECT * FROM (SELECT CAST(iv AS VARCHAR) FROM it_cn_t) s ORDER BY iv`, "1 day", "1 mon", "26:00:00")
	h.expectRows(`SELECT count FROM (SELECT count(*) FROM it_cn_t) s`, "3")
	h.expectRows(`SELECT s.upper FROM (SELECT upper(v) FROM it_cn_t WHERE id = 2) s`, "B")
	h.expectRows(`WITH c AS (SELECT id, upper(v), id * 10 FROM it_cn_t) SELECT upper, "?column?" FROM c WHERE id = 2`, "B|20")
	h.expectRows(`WITH RECURSIVE r AS (SELECT 1 UNION ALL SELECT "?column?" + 1 FROM r WHERE "?column?" < 3)
		SELECT * FROM r ORDER BY 1`, "1", "2", "3")
	h.expectRows(`SELECT l.max FROM it_cn_t, LATERAL (SELECT max(id) FROM it_cn_t x WHERE x.id < it_cn_t.id) l
		WHERE it_cn_t.id = 3`, "2")

	// Views and CREATE TABLE … AS store the names.
	h.exec(`CREATE VIEW it_cn_v AS SELECT id, upper(v), CAST(iv AS VARCHAR) FROM it_cn_t`)
	h.expectRows(`SELECT upper, iv FROM it_cn_v ORDER BY id`, "A|1 day", "B|26:00:00", "C|1 mon")
	h.exec(`CREATE TABLE it_cn_ctas AS SELECT id, count(*), upper(v), id + 1, CAST(id AS VARCHAR(5)) AS s
		FROM it_cn_t GROUP BY id, v`)
	h.expectRows(`SELECT table_name, column_name FROM information_schema.columns
			WHERE table_name IN ('it_cn_v', 'it_cn_ctas') ORDER BY table_name, ordinal_position`,
		"it_cn_ctas|id", "it_cn_ctas|count", "it_cn_ctas|upper", "it_cn_ctas|?column?", "it_cn_ctas|s",
		"it_cn_v|id", "it_cn_v|upper", "it_cn_v|iv")
	h.expectRows(`SELECT id, count, upper, "?column?" FROM it_cn_ctas ORDER BY id`, "1|1|A|2", "2|1|B|3", "3|1|C|4")
	h.expectRows(`SELECT id FROM it_cn_ctas WHERE "?column?" > 2 ORDER BY "?column?" DESC`, "3", "2")

	// Names that several columns have: a derived table or CTE keeps them
	// all, and a reference to one is ambiguous, as in Postgres.
	schema = h.expectRows(`SELECT * FROM (SELECT id, id, 1, 2 FROM it_cn_t WHERE id < 3) s ORDER BY 1`, "1|1|1|2", "2|2|1|2")
	if got := fieldNames(schema); got != "id|id|?column?|?column?" {
		t.Errorf("derived table column names: %s, want id|id|?column?|?column?", got)
	}
	schema = h.expectRows(`WITH c AS (SELECT count(*), count(v) FROM it_cn_t) SELECT * FROM c`, "3|3")
	if got := fieldNames(schema); got != "count|count" {
		t.Errorf("CTE column names: %s, want count|count", got)
	}
	h.expectRows(`SELECT DISTINCT * FROM (SELECT 1, 2 FROM it_cn_t) s`, "1|2")
	h.expectRows(`SELECT * FROM (SELECT 1 AS a, 2 AS a) s(x)`, "1|2")
	h.expectRows(`SELECT a FROM (SELECT 1, 2) s(b, a)`, "2")
	h.expectRows(`SELECT * FROM (SELECT 1 AS a, 2 AS b) s NATURAL JOIN (SELECT 1, 2) r`, "1|2|1|2")
	h.expectRows(`SELECT * FROM (SELECT v, v FROM it_cn_t) s, (SELECT 1, 2) r ORDER BY 1`, "a|a|1|2", "b|b|1|2", "c|c|1|2")
	for _, c := range []struct{ sql, err string }{
		{`SELECT id FROM (SELECT id, id FROM it_cn_t) s`, `column reference "id" is ambiguous`},
		{`SELECT s.id FROM (SELECT id, id FROM it_cn_t) s`, `column reference "id" is ambiguous`},
		{`SELECT * FROM (SELECT id, id FROM it_cn_t) s WHERE id = 1`, `column reference "id" is ambiguous`},
		{`SELECT 1 FROM (SELECT id, id FROM it_cn_t) s ORDER BY id`, `column reference "id" is ambiguous`},
		{`SELECT count(*) FROM (SELECT id, id FROM it_cn_t) s GROUP BY id`, `column reference "id" is ambiguous`},
		{`SELECT * FROM (SELECT id, id FROM it_cn_t) s ORDER BY id`, `ORDER BY "id" is ambiguous`},
		{`SELECT "?column?" FROM (SELECT 1, 2) s`, `column reference "?column?" is ambiguous`},
		{`WITH c AS (SELECT count(*), count(v) FROM it_cn_t) SELECT count FROM c`, `column reference "count" is ambiguous`},
		{`WITH c(a, a) AS (SELECT 1, 2) SELECT a FROM c`, `column reference "a" is ambiguous`},
		{`SELECT a FROM (SELECT 1, 2) s(a, a)`, `column reference "a" is ambiguous`},
		{`SELECT (SELECT "?column?" FROM (SELECT 1, 2) s)`, `column reference "?column?" is ambiguous`},
		{`SELECT * FROM (SELECT 1 AS x, 2 AS x) s, LATERAL (SELECT s.x) l`, `column reference "x" is ambiguous`},
		{`SELECT * FROM (SELECT 1 AS a, 2 AS a) s JOIN (SELECT 1 AS a) r USING (a)`,
			`common column name "a" appears more than once in left table`},
		{`SELECT * FROM (SELECT 1 AS a) r JOIN (SELECT 1 AS a, 2 AS a) s USING (a)`,
			`common column name "a" appears more than once in right table`},
		{`SELECT * FROM (SELECT 1, 2) s NATURAL JOIN (SELECT 1 AS "?column?") r`,
			`common column name "?column?" appears more than once in left table`},
		{`SELECT * FROM (SELECT 1 AS x, 2 AS x) s GROUP BY ROLLUP (1)`,
			`column "s.x" must appear in the GROUP BY clause or be used in an aggregate function`},
	} {
		h.expectError(c.sql, c.err)
	}

	// A table or view can't have them; nothing is created.
	h.expectError(`CREATE TABLE it_cn_dup AS SELECT count(*), count(v) FROM it_cn_t`, `column "count" specified more than once`)
	h.expectError(`CREATE TABLE it_cn_dup AS SELECT * FROM (SELECT 1, 2) s`, `column "?column?" specified more than once`)
	h.expectError(`CREATE VIEW it_cn_v2 AS SELECT id, id::text FROM it_cn_t`, `column "id" specified more than once`)
	h.expectError(`CREATE VIEW it_cn_v2 (a, a) AS SELECT 1, 2`, `column "a" specified more than once`)
	h.expectRows(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('it_cn_dup', 'it_cn_v2')`, "0")
	h.exec(`CREATE TABLE it_cn_dup AS SELECT count(*), count(v) AS count_v FROM it_cn_t`)
	h.expectRows(`SELECT count, count_v FROM it_cn_dup`, "3|3")
}

// A duplicate name's column is read under a name of its own, which no
// result or error shows (see nameColumns).
func TestSQLDuplicateColumnLabels(t *testing.T) {
	h := newSQLHarness(t)
	for _, sql := range []string{
		`SELECT * FROM (SELECT 1, 2) s`,
		`SELECT s.* FROM (SELECT 1, 2) s JOIN (SELECT 1 AS "?column?") r ON true`,
		`SELECT * FROM (SELECT 1, 2) s NATURAL FULL JOIN (SELECT 3 AS x) r`,
		`SELECT * FROM (SELECT 1, 2) s, LATERAL (SELECT 3, 4) l`,
		`SELECT *, row_number() OVER (ORDER BY 1) FROM (SELECT 1, 2) s`,
		`SELECT * FROM (SELECT 1, 2) s UNION ALL SELECT 3, 4`,
	} {
		_, schema := h.query(sql)
		if names := fieldNames(schema); strings.ContainsRune(names, 0) || !strings.Contains(names, "?column?|?column?") {
			t.Errorf("%s: column names %q", sql, names)
		}
	}
}
