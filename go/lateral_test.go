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

import "testing"

// Postgres's errors for a derived table that reads an earlier FROM item
// without LATERAL (#114): "invalid reference", with a detail and a hint to
// add LATERAL. These are pg16's messages (DETAIL and HINT after "; ") for
// the same queries over tables t(a, b) and u(a, c), here CTEs.
func TestLateralReferenceErrors(t *testing.T) {
	const with = "WITH t(a, b) AS (SELECT 1, 2), u(a, c) AS (SELECT 1, 3) "
	const cannot = "but it cannot be referenced from this part of the query"
	entry := func(name, item string) string {
		return `error: invalid reference to FROM-clause entry for table "` + name + `"; there is an entry for table "` +
			item + `", ` + cannot
	}
	hinted := func(name, item string) string {
		return entry(name, item) + "; to reference that table, you must mark this subquery with LATERAL"
	}
	column := func(col, item string) string {
		return `there is a column named "` + col + `" in table "` + item + `", ` + cannot +
			"; to reference that column, you must mark this subquery with LATERAL"
	}
	const several = `there are columns named "a", but they are in tables that cannot be referenced from this part of the query; ` +
		"try using a table-qualified name"
	missing := func(name string) string { return `error: missing FROM-clause entry for table "` + name + `"` }

	cases := []struct{ q, want string }{
		// The issue's shape, in every clause and join form.
		{"SELECT * FROM t, (SELECT t.a) s", hinted("t", "t")},
		{"SELECT * FROM t JOIN (SELECT t.a) s ON true", hinted("t", "t")},
		{"SELECT * FROM t JOIN u ON true, (SELECT u.a) s", hinted("u", "u")},
		{"SELECT * FROM t, (SELECT * FROM u WHERE u.a = t.a) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT 1 GROUP BY t.a) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT t.zz) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT t.*) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT 1 AS k) s, (SELECT s.k) s2", hinted("s", "s")},
		{"SELECT * FROM t, (SELECT 1 AS k) s, (SELECT s.*) s2", hinted("s", "s")},
		// The entry is named by its alias; t names the item that reads t,
		// the first one if several do.
		{"SELECT * FROM t AS x, (SELECT x.a) s", hinted("x", "x")},
		{"SELECT * FROM t AS x, (SELECT t.a) s", hinted("t", "x")},
		{"SELECT * FROM t, t AS x, (SELECT t.a) s", hinted("t", "t")},
		{"SELECT * FROM t AS x, t, (SELECT t.a) s", hinted("t", "x")},
		// Deeper in the derived table: a subquery, a nested derived table,
		// a LATERAL item or a CTE of its own.
		{"SELECT * FROM t, (SELECT 1 FROM u WHERE EXISTS (SELECT t.a)) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT * FROM (SELECT t.a) s1) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT * FROM u, (SELECT t.a) s1) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT * FROM u, LATERAL (SELECT t.a) s1) s", hinted("t", "t")},
		{"SELECT * FROM t, LATERAL (SELECT * FROM u, (SELECT u.a) s1) s", hinted("u", "u")},
		{"SELECT * FROM t, (WITH c AS (SELECT t.a) SELECT * FROM c) s", hinted("t", "t")},
		{"SELECT * FROM t, (SELECT * FROM generate_series(1, (SELECT t.a))) g", hinted("t", "t")},
		// No hint where LATERAL wouldn't help: in a FROM function's
		// arguments (LATERAL already), or right of a RIGHT or FULL join.
		{"SELECT * FROM t, (SELECT * FROM generate_series(1, t.a)) g", entry("t", "t")},
		{"SELECT * FROM t RIGHT JOIN (SELECT t.a) s ON true", entry("t", "t")},
		{"SELECT * FROM t FULL JOIN (SELECT t.a) s ON true", entry("t", "t")},

		// An unqualified column of an earlier item.
		{"SELECT * FROM t, (SELECT a) s", `error: column "a" does not exist; ` + column("a", "t")},
		{"SELECT * FROM t AS x, (SELECT b) s", `error: column "b" does not exist; ` + column("b", "x")},
		{"SELECT * FROM t JOIN u USING (a), (SELECT b) s", `error: column "b" does not exist; ` + column("b", "t")},
		{"SELECT * FROM t, (SELECT * FROM (SELECT b) s1) s", `error: column "b" does not exist; ` + column("b", "t")},
		{"SELECT * FROM t, (SELECT b FROM u) s", `error: column "b" does not exist in table "u"; ` + column("b", "t")},
		{"SELECT * FROM t, (SELECT 1 FROM u ORDER BY b) s", `error: column "b" does not exist in table "u"; ` + column("b", "t")},
		{"SELECT * FROM t, u, (SELECT a) s", `error: column "a" does not exist; ` + several},
		{"SELECT * FROM t NATURAL JOIN u, (SELECT a) s", `error: column "a" does not exist; ` + several},
		{"SELECT * FROM t, (SELECT * FROM generate_series(1, b)) g",
			`error: column "b" does not exist; there is a column named "b" in table "t", ` + cannot},

		// Unchanged. Postgres says the entry is missing for an item after
		// the reference, even a LATERAL one's, and in a CTE's body, which
		// is planned before the FROM list.
		{"SELECT * FROM (SELECT t.a) s, t", missing("t")},
		{"SELECT * FROM t, LATERAL (SELECT u.a) s, u", missing("u")},
		{"SELECT * FROM t, (SELECT x.a) s", missing("x")},
		{"WITH c AS (SELECT t.a) SELECT * FROM (SELECT 1 AS a) t, c", missing("t")},
		{", c AS (SELECT t.a) SELECT * FROM t, (SELECT * FROM c) s", missing("t")},
		{", c AS (SELECT b) SELECT * FROM t, (SELECT * FROM c) s", `error: column "b" does not exist`},
		// Postgres runs these, where a derived table reads an enclosing
		// query, but the driver can't (see the README); its error doesn't
		// suggest LATERAL.
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM u, (SELECT t.a) s)", missing("t")},
		{"SELECT * FROM t, LATERAL (SELECT * FROM u, (SELECT t.a) s1) s", missing("t")},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM t, (SELECT t.a) s)", missing("t")},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM u, (SELECT b) s)", `error: column "b" does not exist`},

		// What works: LATERAL, and FROM functions, which are LATERAL
		// anyway.
		{"SELECT * FROM t, LATERAL (SELECT t.a + 1) s", "1|2|2"},
		{"SELECT * FROM t, generate_series(1, t.a) g", "1|2|1"},
		{"SELECT * FROM t, generate_series(1, (SELECT t.a)) g", "1|2|1"},
	}
	for _, c := range cases {
		q := with + c.q
		switch c.q[0] {
		case 'W': // its own WITH list
			q = c.q
		case ',': // more CTEs
			q = with[:len(with)-1] + c.q
		}
		if got := memQuery(q); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.q, got, c.want)
		}
	}
}
