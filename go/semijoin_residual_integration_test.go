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
	"sort"
	"strings"
	"testing"
)

// A correlated EXISTS whose body also compares inner columns with outer
// columns (TPC-H Q21's `l2.l_suppkey <> l1.l_suppkey`) runs as a semi-join
// that evaluates those comparisons on the outer row's group; its results
// must be those of running it per outer row. Before, it ran per outer row,
// which timed out Q21 (issue #587).
func TestSQLSemiJoinResiduals(t *testing.T) {
	// Probe mechanics: keep probing inner sides wholeBuildByCost would build (#662).
	keepSemiProbes(t)
	h := newSQLHarness(t)
	tables := []string{"it_sjr_l", "it_sjr_big", "it_sjr_s", "it_sjr_ord"}
	drop := func() {
		for _, name := range tables {
			h.exec("DROP TABLE IF EXISTS " + name)
		}
	}
	drop()
	t.Cleanup(drop)

	// Lines of orders ok from suppliers sk: one supplier twice (1), several
	// suppliers (2, 3, 5 with a repeated one), a NULL supplier (4, 7), NULL
	// orders, a NULL receipt date (6) and a NaN.
	h.exec(`CREATE TABLE it_sjr_l (id INTEGER, ok BIGINT, sk BIGINT, rd DATE, cd DATE, s VARCHAR, f DOUBLE, c CHAR(3), z INTEGER)`)
	late, onTime := "DATE '2024-03-02', DATE '2024-03-01'", "DATE '2024-02-28', DATE '2024-03-01'"
	h.exec(`INSERT INTO it_sjr_l VALUES
		(1, 1, 10, ` + late + `, 'a', 1.5, 'x', 1), (2, 1, 10, ` + onTime + `, 'a', 2.5, 'x', 1),
		(3, 2, 10, ` + late + `, 'a', 1.0, 'x', 1), (4, 2, 20, ` + onTime + `, 'b', 2.0, 'y', 1),
		(5, 2, 30, ` + onTime + `, 'b', NULL, 'y', 1),
		(6, 3, 10, ` + late + `, 'a', 3.0, 'x', 1), (7, 3, 20, ` + late + `, 'c', CAST('NaN' AS DOUBLE), 'z', 1),
		(8, 4, 10, ` + late + `, 'a', 1.0, 'x', 1), (9, 4, NULL, ` + late + `, NULL, 1.0, NULL, 1),
		(10, 5, 10, ` + late + `, 'a', 1.0, 'x', 1), (11, 5, 20, ` + onTime + `, 'b', 1.0, 'y', 1),
		(12, 5, 20, ` + onTime + `, 'b', 1.0, 'y', 1),
		(13, NULL, 10, ` + late + `, 'a', 1.0, 'x', 1), (14, NULL, 20, ` + late + `, 'b', 1.0, 'y', 1),
		(15, 6, 10, ` + late + `, 'a', 1.0, 'x', 1), (16, 6, 20, NULL, DATE '2024-03-01', 'b', 1.0, 'y', 1),
		(17, 7, NULL, ` + late + `, 'a', 1.0, 'x', 1), (18, 7, 20, ` + onTime + `, 'b', 1.0, 'y', 1),
		(19, 9, 90, ` + late + `, 'q', 1.0, 'x', 0)`)

	// work runs a query and returns its rows and subquery work.
	work := func(sql string) ([]string, subqueryWork) {
		t.Helper()
		r, s, i := subqueryStats.runs.Load(), subqueryStats.semiJoins.Load(), subqueryStats.inSets.Load()
		rows, _ := h.query(sql)
		return rows, subqueryWork{subqueryStats.runs.Load() - r, subqueryStats.semiJoins.Load() - s, subqueryStats.inSets.Load() - i}
	}
	// same runs sql (a {{R}} in each subquery body) decorrelated and, as the
	// oracle, per outer row (RANDOM() keeps a subquery from decorrelating),
	// and checks they agree. It returns the decorrelated query's work.
	same := func(what, sql string) subqueryWork {
		t.Helper()
		got, w := work(strings.ReplaceAll(sql, "{{R}}", ""))
		want, ow := work(strings.ReplaceAll(sql, "{{R}}", " AND RANDOM() >= 0"))
		if ow.semiJoins != 0 || ow.runs == 0 {
			t.Fatalf("%s: oracle work %+v, want per-row runs only", what, ow)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %q, per-row %q", what, got, want)
		}
		return w
	}
	semi := func(what, sql string, semiJoins int64) {
		t.Helper()
		if w := same(what, sql); w.runs != 0 || w.semiJoins != semiJoins {
			t.Errorf("%s: %+v, want %d semi-join(s) and no per-row runs", what, w, semiJoins)
		}
	}
	perRow := func(what, sql string) {
		t.Helper()
		if w := same(what, sql); w.semiJoins != 0 || w.runs < 2 {
			t.Errorf("%s: %+v, want per-row runs and no semi-join", what, w)
		}
	}

	// TPC-H Q21's shape. Only lines 3 (order 2), 10 (order 5, the other
	// supplier twice) and 15 (order 6, whose other line has a NULL receipt
	// date) wait alone: a NULL supplier or order never compares TRUE.
	q21 := `SELECT l1.id FROM it_sjr_l l1 WHERE l1.rd > l1.cd
		AND EXISTS (SELECT * FROM it_sjr_l l2 WHERE l2.ok = l1.ok AND l2.sk <> l1.sk{{R}})
		AND NOT EXISTS (SELECT * FROM it_sjr_l l3 WHERE l3.ok = l1.ok AND l3.sk <> l1.sk AND l3.rd > l3.cd{{R}})
		ORDER BY l1.id`
	h.expectRows(strings.ReplaceAll(q21, "{{R}}", ""), "3", "10", "15")
	semi("Q21 shape", q21, 2)
	// In the select list; the outer column on the left.
	semi("select list", `SELECT l1.id,
			EXISTS (SELECT 1 FROM it_sjr_l l2 WHERE l2.ok = l1.ok AND l2.sk <> l1.sk{{R}}),
			NOT EXISTS (SELECT 1 FROM it_sjr_l l2 WHERE l1.ok = l2.ok AND l1.rd < l2.rd{{R}})
		FROM it_sjr_l l1 ORDER BY l1.id`, 2)
	// Ordering comparisons, several residuals, strings, doubles (NaN, NULL),
	// composite keys and integers of different widths.
	semi("dates and suppliers", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.rd >= l1.rd AND l2.sk > l1.sk{{R}}) ORDER BY l1.id`, 1)
	semi("strings and doubles", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.s <> l1.s AND l2.f <= l1.f{{R}}) ORDER BY l1.id`, 1)
	semi("composite key", `SELECT l1.id FROM it_sjr_l l1 WHERE NOT EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.s = l1.s AND l2.id <> l1.id{{R}}) ORDER BY l1.id`, 1)
	semi("bigint against integer", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.sk > l1.id AND l2.id >= l1.z{{R}}) ORDER BY l1.id`, 1)

	// Shapes that keep running per outer row: columns of different classes,
	// expressions, IN, OR, NOT, CHAR, no equality, and a body that fails on a
	// row (10 / 0) no outer row matches.
	perRow("double against bigint", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.f <> l1.sk{{R}}) ORDER BY l1.id`)
	perRow("inner expression", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.sk + 0 <> l1.sk{{R}}) ORDER BY l1.id`)
	perRow("outer expression", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.sk <> l1.sk + 0{{R}}) ORDER BY l1.id`)
	perRow("IN", `SELECT l1.id FROM it_sjr_l l1 WHERE l1.sk IN (SELECT l2.sk FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.rd > l1.rd{{R}}) ORDER BY l1.id`)
	perRow("OR", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND (l2.sk <> l1.sk OR l2.rd > l1.rd){{R}}) ORDER BY l1.id`)
	perRow("NOT", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND NOT (l2.sk = l1.sk){{R}}) ORDER BY l1.id`)
	perRow("CHAR", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.c <> l1.c{{R}}) ORDER BY l1.id`)
	perRow("no equality", `SELECT l1.id FROM it_sjr_l l1 WHERE EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.sk <> l1.sk AND l2.rd > l1.rd{{R}}) ORDER BY l1.id`)
	failing := `SELECT l1.id FROM it_sjr_l l1 WHERE l1.ok <> 9 AND EXISTS (SELECT 1 FROM it_sjr_l l2
		WHERE l2.ok = l1.ok AND l2.sk <> l1.sk AND 10 / l2.z > 0{{R}}) ORDER BY l1.id`
	if w := same("failing body", failing); w.semiJoins != 1 || w.runs < 2 {
		t.Errorf("failing body: %+v, want 1 semi-join attempt, then per-row runs", w)
	}

	// A large inner side (more than semiJoinEagerRows rows) is probed per
	// page of outer rows, scanned or joined (TPC-H Q21 joins its outer
	// side), with several batches of keys.
	type line struct{ ok, sk, late int } // 0: NULL; late -1: NULL receipt date
	var lines []line
	for ok := 1; ok <= 800; ok++ {
		for j := 0; j < 1+ok%5; j++ {
			l := line{ok: ok, sk: 1 + (ok/4)%6} // one supplier when ok%4 == 0
			if ok%4 != 0 {
				l.sk = 1 + (ok/2+j*(1+ok%4))%6
			}
			if (ok*13+j)%37 == 0 {
				l.sk = 0
			}
			if (ok*7+j)%4 == 0 {
				l.late = 1
			}
			if (ok*(j+1))%41 == 7 {
				l.late = -1
			}
			lines = append(lines, l)
		}
	}
	lines = append(lines, line{ok: 0, sk: 1, late: 1}, line{ok: 0, sk: 2, late: 1})
	if len(lines) <= semiJoinEagerRows {
		t.Fatalf("%d lines do not exceed the eager build size", len(lines))
	}
	sqlInt := func(v int) string {
		if v == 0 {
			return "NULL"
		}
		return fmt.Sprint(v)
	}
	var vals []string
	for _, l := range lines {
		rd := "DATE '2024-02-28'"
		switch l.late {
		case 1:
			rd = "DATE '2024-03-02'"
		case -1:
			rd = "NULL"
		}
		vals = append(vals, fmt.Sprintf("(%s, %s, %s, DATE '2024-03-01')", sqlInt(l.ok), sqlInt(l.sk), rd))
	}
	h.exec("CREATE TABLE it_sjr_big (ok BIGINT, sk BIGINT, rd DATE, cd DATE)")
	h.exec("INSERT INTO it_sjr_big VALUES " + strings.Join(vals, ", "))
	h.exec("CREATE TABLE it_sjr_s (sk BIGINT, name VARCHAR)")
	h.exec("INSERT INTO it_sjr_s VALUES (1, 'S1'), (2, 'S2'), (3, 'S3'), (4, 'S4'), (5, 'S5'), (6, 'S6')")
	vals = vals[:0]
	for ok := 1; ok <= 800; ok++ {
		st := "O"
		if ok%5 != 0 {
			st = "F"
		}
		vals = append(vals, fmt.Sprintf("(%d, '%s')", ok, st))
	}
	h.exec("CREATE TABLE it_sjr_ord (ok BIGINT, st VARCHAR)")
	h.exec("INSERT INTO it_sjr_ord VALUES " + strings.Join(vals, ", "))

	// waits reports whether line l1 is the only late supplier of a
	// multi-supplier order, with SQL's NULL rules.
	waits := func(l1 line) bool {
		if l1.ok == 0 || l1.sk == 0 || l1.late != 1 {
			return false
		}
		other, otherLate := false, false
		for _, l := range lines {
			if l.ok == l1.ok && l.sk != 0 && l.sk != l1.sk {
				other = true
				otherLate = otherLate || l.late == 1
			}
		}
		return other && !otherLate
	}
	count := 0
	perSupplier := map[int]int{}
	for _, l := range lines {
		if waits(l) {
			count++
			if l.ok%5 != 0 { // status F
				perSupplier[l.sk]++
			}
		}
	}
	sks := make([]int, 0, len(perSupplier))
	for sk := range perSupplier {
		sks = append(sks, sk)
	}
	// ORDER BY numwait DESC, s.name (S1 … S6 sort as their numbers).
	sort.Slice(sks, func(i, j int) bool {
		if a, b := perSupplier[sks[i]], perSupplier[sks[j]]; a != b {
			return a > b
		}
		return sks[i] < sks[j]
	})
	var grouped []string
	for _, sk := range sks {
		grouped = append(grouped, fmt.Sprintf("S%d|%d", sk, perSupplier[sk]))
	}
	if count < 50 || len(grouped) < 3 {
		t.Fatalf("fixture has %d waiting lines in %d groups; want a meaningful result", count, len(grouped))
	}
	probed := func(what string, w subqueryWork) {
		t.Helper()
		if w.runs != 0 || w.semiJoins < 3 {
			t.Errorf("%s: %+v, want bounded page probes (more than one batch) and no per-row runs", what, w)
		}
	}
	exists := `EXISTS (SELECT * FROM it_sjr_big l2 WHERE l2.ok = l1.ok AND l2.sk <> l1.sk{{R}})
		AND NOT EXISTS (SELECT * FROM it_sjr_big l3 WHERE l3.ok = l1.ok AND l3.sk <> l1.sk AND l3.rd > l3.cd{{R}})`
	scanned := `SELECT COUNT(*) FROM it_sjr_big l1 WHERE l1.rd > l1.cd AND ` + exists
	probed("scanned outer", h.expectWork(strings.ReplaceAll(scanned, "{{R}}", ""), fmt.Sprint(count)))
	joined := `SELECT s.name, COUNT(*) AS numwait FROM it_sjr_s s, it_sjr_big l1, it_sjr_ord o
		WHERE s.sk = l1.sk AND o.ok = l1.ok AND o.st = 'F' AND l1.rd > l1.cd AND ` + exists + `
		GROUP BY s.name ORDER BY numwait DESC, s.name`
	probed("joined outer", h.expectWork(strings.ReplaceAll(joined, "{{R}}", ""), grouped...))
	if w := same("joined outer", joined); w.runs != 0 {
		t.Errorf("joined outer: %+v, want no per-row runs", w)
	}
	// Each group's rows decide, not just whether the key has any: an order
	// with a later line from a higher supplier.
	if w := same("joined ordering residuals", `SELECT l1.ok, l1.sk FROM it_sjr_big l1 JOIN it_sjr_ord o ON o.ok = l1.ok
		WHERE o.st = 'F' AND l1.ok <= 300 AND EXISTS (SELECT 1 FROM it_sjr_big l2
			WHERE l2.ok = l1.ok AND l2.sk > l1.sk AND l2.rd >= l1.rd{{R}})
		ORDER BY l1.ok, l1.sk`); w.runs != 0 || w.semiJoins == 0 {
		t.Errorf("joined ordering residuals: %+v, want page probes and no per-row runs", w)
	}
}
