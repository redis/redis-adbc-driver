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

// Derived tables and CTEs that read enclosing queries (#114), over tables:
// the same queries as outer_from_test.go's, and statements that change a
// table. The results and errors are pg16's for the same statements on
// tables t(a, b) and u(a, c) with these rows.

import "testing"

func (h *sqlHarness) setupOuterFrom() {
	h.t.Helper()
	h.exec(`DROP SCHEMA IF EXISTS it_of CASCADE`)
	h.exec(`CREATE SCHEMA it_of`)
	h.t.Cleanup(func() { h.exec(`DROP SCHEMA IF EXISTS it_of CASCADE`) })
	h.exec(`CREATE TABLE it_of.t (a INTEGER, b INTEGER)`)
	h.exec(`INSERT INTO it_of.t VALUES (1, 2), (2, 3), (3, NULL)`)
	h.exec(`CREATE TABLE it_of.u (a INTEGER, c INTEGER)`)
	h.exec(`INSERT INTO it_of.u VALUES (1, 3), (2, 5), (2, 6)`)
}

// expectExecErrorText runs a statement with ExecuteUpdate and checks that it
// fails with exactly the message want (without the driver's prefix).
func (h *sqlHarness) expectExecErrorText(sql, want string) {
	h.t.Helper()
	err := h.execErr(sql)
	if err == nil {
		h.t.Errorf("%s: no error, want %q", sql, want)
		return
	}
	if got := errText(err); got != want {
		h.t.Errorf("%s\n error: %q\n  want: %q", sql, got, want)
	}
}

func TestSQLOuterReferencesFromFrom(t *testing.T) {
	h := newSQLHarness(t)
	h.setupOuterFrom()

	// The issue's query.
	h.expectRows(`SELECT * FROM it_of.t WHERE EXISTS (SELECT * FROM it_of.u, (SELECT t.a) s) ORDER BY a`,
		"1|2", "2|3", "3|NULL")
	h.expectRows(`SELECT * FROM it_of.t WHERE EXISTS (SELECT * FROM it_of.u, (SELECT t.a) s WHERE s.a = u.a) ORDER BY a`,
		"1|2", "2|3")
	h.expectRows(`SELECT a, (SELECT count(*) FROM it_of.u, (SELECT t.a AS x) s WHERE u.a = s.x) FROM it_of.t ORDER BY a`,
		"1|1", "2|2", "3|0")
	// The derived table filters u in its index, with t's values.
	h.expectRows(`SELECT * FROM it_of.t WHERE EXISTS (SELECT 1 FROM (SELECT * FROM it_of.u WHERE u.c > t.b) s WHERE s.a = t.a) ORDER BY 1`,
		"1|2", "2|3")
	h.expectRows(`SELECT * FROM it_of.t WHERE a IN (SELECT s.a FROM (SELECT u.a FROM it_of.u WHERE u.a = t.a AND u.c > t.b) s) ORDER BY 1`,
		"1|2", "2|3")
	h.expectRows(`SELECT a, (SELECT string_agg(u.c::text, ',' ORDER BY u.c) FROM it_of.u JOIN (SELECT t.a AS k) s ON u.a = s.k)
		FROM it_of.t ORDER BY 1`, "1|3", "2|5,6", "3|NULL")
	h.expectRows(`SELECT a, (SELECT count(*) FROM it_of.u FULL JOIN (SELECT t.a AS k) s ON u.a = s.k) FROM it_of.t ORDER BY 1`,
		"1|3", "2|3", "3|4")
	h.expectRows(`SELECT * FROM it_of.t WHERE EXISTS (SELECT * FROM it_of.u WHERE u.a = t.a
		AND EXISTS (SELECT 1 FROM (SELECT u.c - t.b AS d) s WHERE s.d > 2)) ORDER BY 1`, "2|3")
	// CTEs, read where they are defined and from a nested query, and a
	// recursive one.
	h.expectRows(`SELECT a, (WITH c AS (SELECT * FROM it_of.u WHERE u.a = t.a) SELECT count(*) FROM c) FROM it_of.t ORDER BY 1`,
		"1|1", "2|2", "3|0")
	h.expectRows(`SELECT a, (WITH c AS (SELECT t.a AS x) SELECT (SELECT x FROM c)) FROM it_of.t ORDER BY 1`,
		"1|1", "2|2", "3|3")
	h.expectRows(`SELECT a, (WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r WHERE n < t.a) SELECT max(n) FROM r)
		FROM it_of.t ORDER BY 1`, "1|1", "2|2", "3|3")
	// A CTE's body sees the WITH lists around its own, not those of the
	// queries that read it (it read the inner one).
	h.expectErrorText(`WITH c AS (SELECT * FROM it_of_nope) SELECT * FROM (WITH it_of_nope AS (SELECT 1 AS k) SELECT * FROM c) s`,
		`table "public"."it_of_nope" does not exist`)
}

func TestSQLOuterReferencesFromFromDML(t *testing.T) {
	h := newSQLHarness(t)
	h.setupOuterFrom()
	const all = `SELECT * FROM it_of.t ORDER BY a`
	original := []string{"1|2", "2|3", "3|NULL"}

	// Subqueries of SET, WHERE and RETURNING read the target row from their
	// derived tables and CTEs.
	h.expectReturning(`UPDATE it_of.t SET b = (SELECT max(s.a) FROM it_of.u, (SELECT t.a) s) RETURNING *`, "a|b",
		"1|1", "2|2", "3|3")
	h.expectReturning(`UPDATE it_of.t SET b = (WITH c AS (SELECT u.c FROM it_of.u WHERE u.a = t.a) SELECT sum(c) FROM c) RETURNING *`,
		"a|b", "1|3", "2|11", "3|NULL")
	h.exec(`UPDATE it_of.t SET b = a + 1 WHERE a < 3`)
	h.expectReturning(`UPDATE it_of.t SET b = 0 FROM it_of.u WHERE EXISTS (SELECT * FROM (SELECT t.a, u.c) s WHERE s.a = u.a)
		RETURNING t.*`, "a|b", "1|0", "2|0")
	h.exec(`UPDATE it_of.t SET b = a + 1 WHERE a < 3`)
	h.expectRows(all, original...)
	n := h.exec(`MERGE INTO it_of.t USING it_of.u ON t.a = u.a
		WHEN MATCHED AND EXISTS (SELECT 1 FROM (SELECT u.c, t.b) s WHERE s.c > s.b + 2) THEN UPDATE SET b = -1`)
	if n != 1 {
		t.Errorf("MERGE: %d rows, want 1", n)
	}
	h.expectRows(all, "1|2", "2|-1", "3|NULL")
	h.exec(`UPDATE it_of.t SET b = 3 WHERE a = 2`)
	h.expectReturning(`DELETE FROM it_of.t WHERE EXISTS (SELECT * FROM it_of.u, (SELECT t.a) s WHERE s.a = u.a) RETURNING *`, "a|b",
		"1|2", "2|3")
	h.expectReturning(`DELETE FROM it_of.t RETURNING a, (SELECT y FROM (SELECT t.a * 7 AS y) s) AS y`, "a|y", "3|21")
	h.expectReturning(`INSERT INTO it_of.t SELECT a + 10, (SELECT x FROM (SELECT u.c * 2 AS x) s) FROM it_of.u RETURNING *`, "a|b",
		"11|6", "12|10", "12|12")
	h.exec(`DELETE FROM it_of.t`)
	h.exec(`INSERT INTO it_of.t VALUES (1, 2), (2, 3), (3, NULL)`)

	// The FROM items of UPDATE, DELETE and MERGE can't read the target:
	// Postgres's errors, without a hint (LATERAL wouldn't help).
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, (SELECT t.a) s`, invalidEntry("t", "t"))
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM (SELECT t.a) s`, invalidEntry("t", "t"))
	h.expectExecErrorText(`UPDATE it_of.t x SET b = 0 FROM it_of.u, (SELECT t.a) s`, invalidEntry("t", "x"))
	h.expectExecErrorText(`UPDATE it_of.t x SET b = 0 FROM it_of.u, (SELECT x.a) s`, invalidEntry("x", "x"))
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, (SELECT 1 FROM it_of.u u2 WHERE EXISTS (SELECT t.a)) s`,
		invalidEntry("t", "t"))
	h.expectExecErrorText(`DELETE FROM it_of.t USING it_of.u, (SELECT t.a) s`, invalidEntry("t", "t"))
	h.expectExecErrorText(`MERGE INTO it_of.t USING (SELECT t.a) s ON true WHEN MATCHED THEN DELETE`, invalidEntry("t", "t"))
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, (SELECT b) s`,
		`column "b" does not exist; there is a column named "b" in table "t", but it cannot be referenced from this part of the query`)
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, (SELECT a) s`,
		`column "a" does not exist; there are columns named "a", but they are in tables that cannot be referenced from this part `+
			`of the query; try using a table-qualified name`)
	// An earlier FROM item: LATERAL would help.
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, (SELECT u.a) s`, lateralRef("u", "u"))
	// A LATERAL item or a FROM function finds the target of UPDATE or
	// DELETE, but may not read it, even if an earlier item has the column
	// (the driver used to read u.a here).
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, LATERAL (SELECT t.a) s`, invalidEntry("t", "t"))
	h.expectExecErrorText(`UPDATE it_of.t SET b = 0 FROM it_of.u, LATERAL (SELECT a) s`, invalidEntry("t", "t"))
	h.expectExecErrorText(`UPDATE it_of.t x SET b = 0 FROM it_of.u, LATERAL (SELECT t.a) s`, invalidEntry("x", "x"))
	h.expectExecErrorText(`DELETE FROM it_of.t USING it_of.u, generate_series(1, t.a) g`, invalidEntry("t", "t"))
	h.expectRows(all, original...)
}
