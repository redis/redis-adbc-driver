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

// GROUP BY, HAVING and QUALIFY expressions read the select list's aliases
// (which Postgres doesn't, but for a bare GROUP BY name) only for a bare
// name no FROM item of the query has, also in a join; a qualified name is
// never an alias. The results and errors are pg16's for the same queries
// over tables (rows in the same order), except where noted. Each query is
// planned twice, as a cached statement is.
func TestAliasesInExpressions(t *testing.T) {
	const with = "WITH t(id, v, x) AS (SELECT 1, 'a', 1.2 UNION ALL SELECT 2, 'A', 1.4 UNION ALL SELECT 3, 'b', 2.0 " +
		"UNION ALL SELECT 4, NULL, 2.6), " +
		"u(k, w) AS (SELECT 1, 'p' UNION ALL SELECT 2, 'q'), " +
		"u2(k, v) AS (SELECT 1, 'p' UNION ALL SELECT 2, 'q') "
	const oneEach = "1|1\n2|1\n3|1\n4|1"
	cases := []struct{ q, want string }{
		// An input column comes first: HAVING x is t.x, also in a join.
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t GROUP BY t.x HAVING x > 1.3 ORDER BY 1, 2", "1|1\n2|1\n3|1"},
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t JOIN u ON true GROUP BY t.x HAVING x > 1.3 ORDER BY 1, 2", "1|2\n2|2\n3|2"},
		{"SELECT k, upper(w) AS w, count(*) FROM u JOIN t ON t.id = u.k GROUP BY k, u.w HAVING w = 'p'", "1|P|1"},
		// pg16: column "t.x" must appear in the GROUP BY clause or be used in
		// an aggregate function (the driver reads it from the group's first
		// row); it groups by t.x + 0 too.
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t JOIN u ON true GROUP BY x + 0 ORDER BY 1, 2", "1|2\n1|2\n2|2\n3|2"},
		// A qualified name is the input column.
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t JOIN u ON true GROUP BY t.x HAVING t.x > 1.3 ORDER BY 1, 2",
			"1|2\n2|2\n3|2"},
		{"SELECT CAST(x AS INTEGER) AS x, count(*) FROM t JOIN u ON true GROUP BY t.x, 1 ORDER BY 1, 2", "1|2\n1|2\n2|2\n3|2"},
		// QUALIFY (which Postgres doesn't have) reads names the same way.
		{"SELECT id * 10 AS id FROM t QUALIFY id = 1", "10"},
		{"SELECT t.id * 10 AS k FROM t JOIN u ON t.id = u.k QUALIFY k = 1", "10"},

		// A name no FROM item has is the alias's; pg16: column "y" (or "s")
		// does not exist.
		{"SELECT x AS y, count(*) FROM t JOIN u ON true GROUP BY y HAVING y > 1.3 ORDER BY 1", "1.4|2\n2.0|2\n2.6|2"},
		{"SELECT upper(v) AS s, count(*) FROM t JOIN u ON t.id = u.k GROUP BY lower(s) ORDER BY 1", "A|2"},
		// Also one an enclosing query has; pg16 reads u.k: 1|0, 2|0.
		{"SELECT k, (SELECT count(*) FROM (SELECT t.id * 10 AS k FROM t GROUP BY t.id HAVING k > 10) q) FROM u ORDER BY 1",
			"1|3\n2|3"},
		// And one two FROM items have: the driver keeps both columns of JOIN
		// … USING (k), which Postgres merges; pg16 reads the merged k: no
		// rows.
		{"SELECT u.k * 10 AS k, count(*) FROM u JOIN u2 USING (k) GROUP BY u.k HAVING k > 10 ORDER BY 1", "20|1"},

		// GROUP BY is resolved from the items as written each time: GROUP BY
		// w after id AS w (t.id, of the enclosing query) groups by t.id, and
		// planned again it isn't read as the alias id, max(v).
		{"SELECT id, (SELECT count(*) FROM (SELECT id AS w, max(v) AS id FROM u2 GROUP BY w) q) FROM t ORDER BY 1", oneEach},
		{"SELECT id, (SELECT count(*) FROM (SELECT CAST(id AS VARCHAR) AS w, max(v) AS id FROM u2 GROUP BY w) q) FROM t ORDER BY 1",
			oneEach},
		{"SELECT id, (SELECT max(w) FROM (SELECT id + k AS w, max(v) AS id FROM u2 GROUP BY w) q) FROM t ORDER BY 1",
			"1|3\n2|4\n3|5\n4|6"},
		{"SELECT id, (SELECT count(*) FROM (SELECT id AS w, max(v) AS id FROM u2 GROUP BY ROLLUP (w)) q) FROM t ORDER BY 1",
			"1|2\n2|2\n3|2\n4|2"},
		// A GROUP BY item named by position or alias binds the item again,
		// planning its subqueries again.
		{"SELECT id, (SELECT count(*) FROM (SELECT id AS w, max(v) AS id FROM u2 GROUP BY w) q) FROM t GROUP BY 1, 2 ORDER BY 1",
			oneEach},
		{"SELECT id, (SELECT count(*) FROM (SELECT id AS w, max(v) AS id FROM u2 GROUP BY w) q) AS c FROM t GROUP BY id, c ORDER BY 1",
			oneEach},
		{"SELECT CAST(x AS INTEGER) AS c, count(*) FROM t GROUP BY ROLLUP (c) ORDER BY 1, 2", "1|2\n2|1\n3|1\nNULL|4"},
	}
	for _, c := range cases {
		stmts, err := ParseScript(with + c.q)
		if err != nil {
			t.Errorf("%s: %v", c.q, err)
			continue
		}
		sel := stmts[0].Stmt.(*SelectStmt)
		for i := range 2 {
			if got := memRun(sel); got != c.want {
				t.Errorf("%s (plan %d):\n got %q\nwant %q", c.q, i+1, got, c.want)
				break
			}
		}
	}
}
