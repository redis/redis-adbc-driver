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

// outerFromWith holds the tables of the tests of derived tables and CTEs
// that read enclosing queries, here CTEs.
const outerFromWith = "WITH t(a, b) AS (SELECT 1, 2 UNION ALL SELECT 2, 3 UNION ALL SELECT 3, NULL), " +
	"u(a, c) AS (SELECT 1, 3 UNION ALL SELECT 2, 5 UNION ALL SELECT 2, 6) "

// A derived table or CTE may read the columns of the queries around the
// one whose FROM or WITH list holds it, as in Postgres (#114). These are
// pg16's results for the same queries over tables t(a, b) and u(a, c) with
// these rows.
func TestOuterReferencesFromFrom(t *testing.T) {
	cases := []struct{ q, want string }{
		// EXISTS, IN and scalar subqueries, also where the semi-join path
		// would apply (see semijoin.go).
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM u, (SELECT t.a) s WHERE s.a = u.a) ORDER BY a", "1|2\n2|3"},
		{"SELECT * FROM t WHERE NOT EXISTS (SELECT 1 FROM (SELECT * FROM u WHERE u.a = t.a) s) ORDER BY 1", "3|NULL"},
		{"SELECT * FROM t WHERE EXISTS (SELECT 1 FROM (SELECT * FROM u WHERE u.c > t.b) s WHERE s.a = t.a) ORDER BY 1", "1|2\n2|3"},
		{"SELECT * FROM t WHERE a IN (SELECT s.x FROM (SELECT t.b - 1 AS x) s) ORDER BY a", "1|2\n2|3"},
		{"SELECT * FROM t WHERE a NOT IN (SELECT s.x FROM (SELECT t.b AS x UNION ALL SELECT 9) s) ORDER BY 1", "1|2\n2|3"},
		{"SELECT a, (SELECT x * 10 FROM (SELECT t.a AS x) s) FROM t ORDER BY a", "1|10\n2|20\n3|30"},
		{"SELECT a FROM t WHERE (SELECT x FROM (SELECT t.a AS x) s) > 1 ORDER BY 1", "2\n3"},
		{"SELECT a FROM t ORDER BY (SELECT x FROM (SELECT -t.a AS x) s)", "3\n2\n1"},
		// Joins: every kind, USING, an enclosing outer join's NULLs, and
		// LATERAL items and FROM functions around and inside.
		{"SELECT a, (SELECT count(*) FROM u, (SELECT t.a AS x) s WHERE u.a = s.x) FROM t ORDER BY a", "1|1\n2|2\n3|0"},
		{"SELECT a, (SELECT string_agg(u.c::text, ',' ORDER BY u.c) FROM u JOIN (SELECT t.a AS k) s ON u.a = s.k) FROM t ORDER BY 1", "1|3\n2|5,6\n3|NULL"},
		{"SELECT a, (SELECT count(*) FROM u LEFT JOIN (SELECT t.b AS k) s ON u.c = s.k WHERE s.k IS NULL) FROM t ORDER BY 1", "1|3\n2|2\n3|3"},
		{"SELECT a, (SELECT count(*) FROM u FULL JOIN (SELECT t.a AS k) s ON u.a = s.k) FROM t ORDER BY 1", "1|3\n2|3\n3|4"},
		{"SELECT a, (SELECT count(*) FROM u RIGHT JOIN (SELECT t.a AS k) s ON u.a = s.k) FROM t ORDER BY 1", "1|1\n2|2\n3|1"},
		{"SELECT a, (SELECT count(*) FROM u JOIN (SELECT t.a AS a) s USING (a)) FROM t ORDER BY 1", "1|1\n2|2\n3|0"},
		{"SELECT t.a, u.c, (SELECT x FROM (SELECT t.a + u.c AS x) s) FROM t JOIN u ON t.a = u.a ORDER BY 1, 2", "1|3|4\n2|5|7\n2|6|8"},
		{"SELECT t.a, u.c, (SELECT x FROM (SELECT u.c AS x) s) FROM t LEFT JOIN u ON t.a = u.a ORDER BY 1, 2", "1|3|3\n2|5|5\n2|6|6\n3|NULL|NULL"},
		{"SELECT t.a, l.* FROM t, LATERAL (SELECT * FROM u, (SELECT t.a AS k) s WHERE u.a = s.k) l ORDER BY 1, 3", "1|1|3|1\n2|2|5|2\n2|2|6|2"},
		{"SELECT a, (SELECT sum(y) FROM (SELECT * FROM u, LATERAL (SELECT u.c + t.a AS y) l) s) FROM t ORDER BY 1", "1|17\n2|20\n3|23"},
		{"SELECT a, (SELECT count(*) FROM (SELECT * FROM generate_series(1, t.a)) g) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
		// Aggregates (an outer level's too, which make it an aggregate query),
		// window functions, DISTINCT ON, set operations, column aliases.
		{"SELECT a, (SELECT sum(c) FROM (SELECT c FROM u WHERE u.a = t.a) s) FROM t ORDER BY 1", "1|3\n2|11\n3|NULL"},
		{"SELECT a, (SELECT max(n) FROM (SELECT u.a, count(*) n FROM u WHERE u.c > t.b GROUP BY u.a) s) FROM t ORDER BY 1", "1|2\n2|2\n3|NULL"},
		{"SELECT (SELECT x FROM (SELECT max(t.a) AS x) s) FROM t", "3"},
		{"SELECT b IS NULL, (SELECT x FROM (SELECT max(t.a) AS x) s) FROM t GROUP BY b IS NULL ORDER BY 1", "false|2\ntrue|3"},
		{"SELECT a, (SELECT n FROM (SELECT sum(t.a) AS n) s) FROM t GROUP BY a ORDER BY 1", "1|1\n2|2\n3|3"},
		{"SELECT a, (SELECT max(r) FROM (SELECT row_number() OVER (ORDER BY c) + t.a AS r FROM u) s) FROM t ORDER BY 1", "1|4\n2|5\n3|6"},
		{"SELECT a, (SELECT c FROM (SELECT DISTINCT ON (u.a) u.a, u.c FROM u WHERE u.c > t.a ORDER BY u.a, u.c DESC) s WHERE s.a = 2) FROM t ORDER BY 1", "1|6\n2|6\n3|6"},
		{"SELECT a, (SELECT sum(v) FROM (SELECT t.a AS v UNION ALL SELECT t.b) s) FROM t ORDER BY 1", "1|3\n2|5\n3|3"},
		{"SELECT a, (SELECT y FROM (SELECT t.a + 1) s(y)) FROM t ORDER BY 1", "1|2\n2|3\n3|4"},
		// Nested levels, and which item a name reads: never an earlier item of
		// the derived table's own level, but an enclosing query's item of
		// that name, or the nearest level that has the column.
		{"SELECT a, (SELECT (SELECT y FROM (SELECT t.a + u.c AS y) s2) FROM u WHERE u.a = t.a ORDER BY c LIMIT 1) FROM t ORDER BY 1", "1|4\n2|7\n3|NULL"},
		{"SELECT a, (SELECT z FROM (SELECT * FROM (SELECT t.a * 2 AS z) s1) s2) FROM t ORDER BY 1", "1|2\n2|4\n3|6"},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM u WHERE u.a = t.a AND EXISTS (SELECT 1 FROM (SELECT u.c - t.b AS d) s WHERE s.d > 2)) ORDER BY 1", "2|3"},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM t AS t2, (SELECT t.a) s WHERE t2.a = s.a + 1) ORDER BY 1", "1|2\n2|3"},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM t, (SELECT t.a) s WHERE s.a = 3) ORDER BY 1", "3|NULL"},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM u, (SELECT b) s WHERE s.b = u.c) ORDER BY 1", "2|3"},
		{"SELECT * FROM t WHERE EXISTS (SELECT * FROM u WHERE EXISTS (SELECT * FROM (SELECT a) s WHERE s.a = 2)) ORDER BY 1", "1|2\n2|3\n3|NULL"},
		// CTEs: read where they are defined, from nested queries, derived
		// tables and other CTEs (twice, from one run), in a derived table, a
		// LATERAL item or another CTE's body.
		{"SELECT a, (WITH c AS (SELECT t.a * 10 AS x) SELECT x FROM c) FROM t ORDER BY 1", "1|10\n2|20\n3|30"},
		{"SELECT * FROM t WHERE EXISTS (WITH c AS (SELECT * FROM u WHERE u.a = t.a) SELECT 1 FROM c) ORDER BY 1", "1|2\n2|3"},
		{"SELECT * FROM t WHERE a IN (WITH c AS (SELECT u.a FROM u WHERE u.c > t.b) SELECT a FROM c) ORDER BY 1", "1|2\n2|3"},
		{"SELECT a, (WITH c AS (SELECT t.a AS x) SELECT count(*) FROM c, c AS c2) FROM t ORDER BY 1", "1|1\n2|1\n3|1"},
		{"SELECT a, (WITH c AS (SELECT t.a AS x) SELECT (SELECT x FROM c)) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
		{"SELECT a, (WITH c AS (SELECT t.a AS x) SELECT (SELECT (SELECT x FROM c))) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
		{"SELECT a, (WITH c AS (SELECT t.b AS x) SELECT y FROM (SELECT x + 1 AS y FROM c) s) FROM t ORDER BY 1", "1|3\n2|4\n3|NULL"},
		{"SELECT * FROM t WHERE EXISTS (WITH c AS (SELECT t.a AS x) SELECT 1 FROM u WHERE EXISTS (SELECT 1 FROM c WHERE c.x = u.a)) ORDER BY 1", "1|2\n2|3"},
		{"SELECT a, (WITH c AS (SELECT t.a AS x), d AS (SELECT x * 2 AS y FROM c) SELECT y FROM d) FROM t ORDER BY 1", "1|2\n2|4\n3|6"},
		{"SELECT a, (WITH c AS (WITH d AS (SELECT t.a AS x) SELECT x + 1 AS y FROM d) SELECT y FROM c) FROM t ORDER BY 1", "1|2\n2|3\n3|4"},
		{"SELECT a, (SELECT y FROM (WITH c AS (SELECT t.a + 100 AS y) SELECT y FROM c) s) FROM t ORDER BY 1", "1|101\n2|102\n3|103"},
		{"SELECT t.a, l.y FROM t, LATERAL (WITH c AS (SELECT t.a * 3 AS y) SELECT y FROM c) l ORDER BY 1", "1|3\n2|6\n3|9"},
		{"SELECT (WITH c AS (SELECT max(t.a) AS m) SELECT m FROM c) FROM t", "3"},
		// Recursive CTEs: either term, a derived table or subquery in the
		// recursive one (one that reads the working table is planned again
		// for each run, see recursive.go), CYCLE.
		{"SELECT a, (WITH RECURSIVE r(n) AS (SELECT t.a UNION ALL SELECT n + 1 FROM r WHERE n < 3) SELECT count(*) FROM r) FROM t ORDER BY 1", "1|3\n2|2\n3|1"},
		{"SELECT a, (WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r WHERE n < t.a) SELECT max(n) FROM r) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
		{"SELECT a, (WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r, (SELECT t.a AS lim) s WHERE n < s.lim) SELECT max(n) FROM r) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
		{"SELECT a, (WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT s.n + 1 FROM (SELECT n FROM r WHERE n < t.a) s) SELECT max(n) FROM r) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
		{"SELECT a, (WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r WHERE n < (SELECT max(u.c) FROM u WHERE u.a = t.a)) SELECT max(n) FROM r) FROM t ORDER BY 1", "1|3\n2|6\n3|1"},
		{"SELECT a, (WITH RECURSIVE r(n) AS (SELECT t.a UNION ALL SELECT n % 3 + 1 FROM r) CYCLE n SET is_cycle USING path SELECT count(*) FROM r) FROM t ORDER BY 1", "1|4\n2|4\n3|4"},
		{"SELECT a, (SELECT max(n) FROM (WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r WHERE n < t.a) SELECT n FROM r) s) FROM t ORDER BY 1", "1|1\n2|2\n3|3"},
	}
	for _, c := range cases {
		if got := memQuery(outerFromWith + c.q); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.q, got, c.want)
		}
	}
}

// A derived table or CTE that reads an enclosing query runs once per
// distinct set of the outer values it reads, like a correlated subquery,
// and one that doesn't runs once.
func TestOuterReferencesFromFromRuns(t *testing.T) {
	const with = "WITH t(a) AS (SELECT 1 UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 2 UNION ALL SELECT 3) "
	cases := []struct {
		q, want string
		runs    int64 // subquery bodies run
	}{
		// The scalar subquery and its derived table, for each of 1, 2, 3.
		{"SELECT sum((SELECT x FROM (SELECT t.a * 10 AS x) s)) FROM t", "90", 6},
		// The derived table runs once, while planning.
		{"SELECT sum((SELECT x + t.a FROM (SELECT 10 AS x) s)) FROM t", "59", 3},
		// Both references read one run of the CTE.
		{"SELECT sum((WITH c AS (SELECT t.a AS x) SELECT count(*) FROM c, c AS c2 WHERE c.x = c2.x)) FROM t", "5", 6},
		{"SELECT sum((WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r WHERE n < t.a) SELECT max(n) FROM r)) FROM t",
			"9", 6},
	}
	for _, c := range cases {
		before := subqueryStats.runs.Load()
		got := memQuery(with + c.q)
		runs := subqueryStats.runs.Load() - before
		if got != c.want || runs != c.runs {
			t.Errorf("%s:\n got %s (%d runs)\nwant %s (%d runs)", c.q, got, runs, c.want, c.runs)
		}
	}
}
