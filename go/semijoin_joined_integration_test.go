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
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A correlated EXISTS / IN whose body joins tables (TPC-DS Q10/Q35/Q69: a
// fact and date_dim) runs as a semi-join, with the results of running it per
// outer row. Before, any join in the body kept the per-row path, which ran a
// join per customer and timed out Q35 (issue #649).
func TestSQLSemiJoinJoinedBodies(t *testing.T) {
	h := newSQLHarness(t)
	tables := []string{"it_sjj_c", "it_sjj_s", "it_sjj_w", "it_sjj_d", "it_sjj_big", "it_sjj_ids"}
	drop := func() {
		for _, name := range tables {
			h.exec("DROP TABLE IF EXISTS " + name)
		}
	}
	drop()
	t.Cleanup(drop)

	// Customers 1–8 (and a NULL one); store and web sales on days of 2001
	// and 2002, a NULL customer and a sale on a day missing from it_sjj_d.
	h.exec(`CREATE TABLE it_sjj_c (id INTEGER, g VARCHAR)`)
	h.exec(`INSERT INTO it_sjj_c VALUES (1, 'a'), (2, 'a'), (3, 'b'), (4, 'b'), (5, 'c'), (6, 'c'), (7, NULL), (8, 'a'), (NULL, 'z')`)
	h.exec(`CREATE TABLE it_sjj_d (day INTEGER, yr INTEGER, q INTEGER)`)
	h.exec(`INSERT INTO it_sjj_d VALUES (1, 2001, 1), (2, 2001, 4), (3, 2002, 1), (4, 2002, 2), (5, 2002, 4)`)
	h.exec(`CREATE TABLE it_sjj_s (cust INTEGER, day INTEGER, amt INTEGER)`)
	h.exec(`INSERT INTO it_sjj_s VALUES (1, 3, 10), (1, 3, 11), (2, 1, 5), (3, 4, 7), (4, 5, 9), (5, 99, 1), (NULL, 3, 2), (8, 4, 3)`)
	h.exec(`CREATE TABLE it_sjj_w (cust INTEGER, day INTEGER)`)
	h.exec(`INSERT INTO it_sjj_w VALUES (2, 3), (3, 1), (6, 4), (8, 5), (NULL, 4)`)

	type work struct{ runs, semiJoins int64 }
	run := func(sql string) ([]string, work) {
		t.Helper()
		r, s := subqueryStats.runs.Load(), subqueryStats.semiJoins.Load()
		rows, _ := h.query(sql)
		return rows, work{subqueryStats.runs.Load() - r, subqueryStats.semiJoins.Load() - s}
	}
	// same checks the decorrelated query against the per-row oracle
	// (RANDOM() keeps each body from decorrelating) and returns its work.
	same := func(what, sql string) work {
		t.Helper()
		got, w := run(strings.ReplaceAll(sql, "{{R}}", ""))
		want, ow := run(strings.ReplaceAll(sql, "{{R}}", " AND RANDOM() >= 0"))
		if ow.semiJoins != 0 || ow.runs == 0 {
			t.Fatalf("%s: oracle work %+v, want per-row runs only", what, ow)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %q, per-row %q", what, got, want)
		}
		return w
	}
	semi := func(what, sql string, semiJoins int64, want ...string) {
		t.Helper()
		if w := same(what, sql); w.runs != 0 || w.semiJoins != semiJoins {
			t.Errorf("%s: %+v, want %d semi-join(s) and no per-row runs", what, w, semiJoins)
		}
		if got, _ := run(strings.ReplaceAll(sql, "{{R}}", "")); want != nil && !slices.Equal(got, want) {
			t.Errorf("%s: got %q, want %q", what, got, want)
		}
	}

	in2002 := "s.day = d.day AND d.yr = 2002 AND d.q < 4"
	semi("comma join", `SELECT c.id FROM it_sjj_c c WHERE EXISTS (SELECT * FROM it_sjj_s s, it_sjj_d d
		WHERE c.id = s.cust AND `+in2002+`{{R}}) ORDER BY c.id`, 1, "1", "3", "8")
	semi("explicit join", `SELECT c.id FROM it_sjj_c c WHERE EXISTS (SELECT 1 FROM it_sjj_s s JOIN it_sjj_d d ON s.day = d.day
		WHERE s.cust = c.id AND d.yr = 2002 AND d.q < 4{{R}}) ORDER BY c.id`, 1, "1", "3", "8")
	semi("not exists", `SELECT c.id FROM it_sjj_c c WHERE NOT EXISTS (SELECT * FROM it_sjj_s s, it_sjj_d d
		WHERE c.id = s.cust AND `+in2002+`{{R}}) ORDER BY c.id NULLS LAST`, 1, "2", "4", "5", "6", "7", "NULL")
	// Q35's shape: an EXISTS and an OR of two EXISTS over different facts.
	semi("exists and or", `SELECT c.id FROM it_sjj_c c WHERE EXISTS (SELECT * FROM it_sjj_s s, it_sjj_d d
		WHERE c.id = s.cust AND `+in2002+`{{R}}) AND (EXISTS (SELECT * FROM it_sjj_w s, it_sjj_d d
		WHERE c.id = s.cust AND `+in2002+`{{R}}) OR EXISTS (SELECT * FROM it_sjj_s s, it_sjj_d d
		WHERE c.id = s.cust AND s.amt > 8 AND `+in2002+`{{R}})) ORDER BY c.id`, 3, "1")
	// A LEFT JOIN in the body, and a second correlation key.
	semi("left join and two keys", `SELECT c.id FROM it_sjj_c c WHERE EXISTS (SELECT * FROM it_sjj_s s
		LEFT JOIN it_sjj_d d ON s.day = d.day WHERE s.cust = c.id AND d.yr IS NULL AND s.amt = c.id{{R}}) ORDER BY c.id`, 1)
	semi("in", `SELECT c.id FROM it_sjj_c c WHERE c.g IN (SELECT x.g FROM it_sjj_s s, it_sjj_c x
		WHERE s.cust = x.id AND s.day = c.id{{R}}) ORDER BY c.id NULLS LAST`, 1)

	// An outer reference in the body's JOIN … ON keeps the per-row path.
	if w := same("outer in on", `SELECT c.id FROM it_sjj_c c WHERE EXISTS (SELECT 1 FROM it_sjj_s s JOIN it_sjj_d d
		ON s.day = d.day AND d.day > c.id - 5 WHERE s.cust = c.id{{R}}) ORDER BY c.id`); w.semiJoins != 0 || w.runs < 2 {
		t.Errorf("outer reference in ON: %+v, want per-row runs only", w)
	}

	// A body estimated above semiJoinEagerRows runs per outer row until those
	// runs have cost about as much as building it, then is built once.
	h.exec(`CREATE TABLE it_sjj_big (cust INTEGER, day INTEGER)`)
	var b strings.Builder
	for i := range 3 * semiJoinEagerRows {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%d, %d)", i%40, 1+i%5)
	}
	h.exec("INSERT INTO it_sjj_big VALUES " + b.String())
	h.exec(`CREATE TABLE it_sjj_ids (id INTEGER)`)
	b.Reset()
	for i := range 80 {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%d)", i)
	}
	h.exec("INSERT INTO it_sjj_ids VALUES " + b.String())
	w := same("adaptive build", `SELECT count(*) FROM it_sjj_ids c WHERE EXISTS (SELECT * FROM it_sjj_big s, it_sjj_d d
		WHERE s.cust = c.id AND s.day = d.day{{R}})`)
	if w.semiJoins != 1 || w.runs < 1 || w.runs >= 80 {
		t.Errorf("adaptive build: %+v, want some per-row runs, then one build", w)
	}
}
