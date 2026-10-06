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
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setupRowValues creates it_rv_t (indexed), it_rv_n (the same rows, not
// indexed), the source it_rv_s and the empty it_rv_e.
func (h *sqlHarness) setupRowValues() {
	h.t.Helper()
	h.dropTables("it_rv_t", "it_rv_n", "it_rv_s", "it_rv_e")
	rows := `(1, 1, 'a', 10), (2, 1, 'b', 20), (3, 2, 'a', 30), (4, 2, NULL, 40), (5, NULL, 'a', 50), (6, 3, 'c', 60)`
	h.exec("CREATE TABLE it_rv_t (id INTEGER, k1 INTEGER, k2 VARCHAR, v INTEGER)")
	h.exec("INSERT INTO it_rv_t VALUES " + rows)
	h.exec("CREATE TABLE it_rv_n (id INTEGER, k1 INTEGER NOINDEX, k2 VARCHAR NOINDEX, v INTEGER NOINDEX)")
	h.exec("INSERT INTO it_rv_n VALUES " + rows)
	h.exec("CREATE TABLE it_rv_s (k1 INTEGER, k2 VARCHAR)")
	h.exec("INSERT INTO it_rv_s VALUES (1, 'a'), (2, NULL), (3, 'c'), (9, 'z')")
	h.exec("CREATE TABLE it_rv_e (id INTEGER, k1 INTEGER, k2 VARCHAR, v INTEGER)")
}

// Every operator on rows of a table, with NULLs, on an indexed table (pushed
// down where the index can answer) and on one without an index (evaluated
// by the driver), which must agree. it_rv_t holds
//
//	id | k1   | k2
//	 1 | 1    | a
//	 2 | 1    | b
//	 3 | 2    | a
//	 4 | 2    | NULL
//	 5 | NULL | a
//	 6 | 3    | c
//
// and it_rv_s (1, a), (2, NULL), (3, c), (9, z).
func TestSQLRowValueOperators(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRowValues()
	for _, tbl := range []string{"it_rv_t", "it_rv_n"} {
		q := func(where string) string { return "SELECT id FROM " + tbl + " WHERE " + where + " ORDER BY id" }
		h.expectRows(q("(k1, k2) = (1, 'a')"), "1")
		h.expectRows(q("ROW(k1, k2) = ROW(2, 'a')"), "3")
		h.expectRows(q("(k1, k2) <> (1, 'a')"), "2", "3", "4", "6")
		h.expectRows(q("(k1, k2) > (1, 'a')"), "2", "3", "4", "6")
		h.expectRows(q("(k1, k2) >= (2, 'a')"), "3", "6")
		h.expectRows(q("(k1, k2) < (2, 'b')"), "1", "2", "3")
		h.expectRows(q("(k1, k2) <= (1, 'b')"), "1", "2")
		h.expectRows(q("(k1, k2) BETWEEN (1, 'b') AND (2, 'a')"), "2", "3")
		h.expectRows(q("(k1, k2) IS NOT DISTINCT FROM (2, NULL)"), "4")
		h.expectRows(q("(k1, k2) IS DISTINCT FROM (2, NULL)"), "1", "2", "3", "5", "6")
		h.expectRows(q("k2 IS DISTINCT FROM 'a'"), "2", "4", "6")
		h.expectRows(q("k1 IS NOT DISTINCT FROM NULL"), "5")
		h.expectRows(q("(k1, k2) IS NULL"))
		h.expectRows(q("(k1, k2) IS NOT NULL"), "1", "2", "3", "6")
		h.expectRows(q("NOT (k1, k2) IS NOT NULL"), "4", "5")
		// IN lists: a NULL item never matches.
		h.expectRows(q("(k1, k2) IN ((1, 'b'), (2, NULL), (3, 'c'))"), "2", "6")
		h.expectRows(q("(k1, k2) NOT IN ((1, 'b'), (3, 'c'))"), "1", "3", "4", "5")
		h.expectRows(q("(k1, k2) NOT IN ((1, 'b'), (NULL, 'c'))"), "1", "3", "5")
		// IN subqueries: rows 3, 4 and 5 compare as NULL with (2, NULL) or
		// (1, a).
		h.expectRows(q("(k1, k2) IN (SELECT k1, k2 FROM it_rv_s)"), "1", "6")
		h.expectRows(q("(k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_s)"), "2")
		h.expectRows(q("(k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_s WHERE k2 IS NOT NULL)"), "2", "3", "4")
		h.expectRows(q("(k1, k2) IN (SELECT k1, k2 FROM it_rv_s WHERE false)"))
		h.expectRows(q("(k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_s WHERE false)"), "1", "2", "3", "4", "5", "6")
		h.expectRows(q("(k1, k2) = ANY (SELECT k1, k2 FROM it_rv_s)"), "1", "6")
		h.expectRows(q("(k1, k2) = SOME (SELECT k1, k2 FROM it_rv_s)"), "1", "6")
		h.expectRows(q("(k1, k2) <> ALL (SELECT k1, k2 FROM it_rv_s)"), "2")
		h.expectRows(q("(k1, k2) < ANY (SELECT k1, k2 FROM it_rv_s WHERE k2 IS NOT NULL)"), "1", "2", "3", "4", "6")
		h.expectRows(q("(k1, k2) >= ALL (SELECT k1, k2 FROM it_rv_s WHERE k1 < 3)"), "6")
		// Correlated: row 5's subquery has no rows (s.k1 = NULL), so NOT IN
		// is true for it.
		w := h.expectWork(q("(k1, k2) IN (SELECT s.k1, s.k2 FROM it_rv_s s WHERE s.k1 = "+tbl+".k1)"), "1", "6")
		if w.semiJoins != 1 || w.runs != 0 {
			t.Errorf("%s: correlated row IN: %+v, want one semi-join and no per-row runs", tbl, w)
		}
		h.expectRows(q("(k1, k2) NOT IN (SELECT s.k1, s.k2 FROM it_rv_s s WHERE s.k1 = "+tbl+".k1)"), "2", "5")
		w = h.expectWork(q("(k1, k2) NOT IN (SELECT s.k1, s.k2 FROM it_rv_s s WHERE s.k1 + 0 = "+tbl+".k1 + 0)"), "2", "5")
		if w.semiJoins != 0 || w.runs != 4 {
			t.Errorf("%s: correlated row NOT IN per row: %+v, want 4 runs (one per distinct k1)", tbl, w)
		}
		// EXISTS with a row comparison in its WHERE.
		h.expectRows(q("EXISTS (SELECT 1 FROM it_rv_s s WHERE (s.k1, s.k2) = ("+tbl+".k1, "+tbl+".k2))"), "1", "6")
		h.expectRows(q("NOT EXISTS (SELECT 1 FROM it_rv_s s WHERE (s.k1, s.k2) = ("+tbl+".k1, "+tbl+".k2))"), "2", "3", "4", "5")
		// A row compared with a subquery's one row.
		h.expectRows(q("(k1, k2) = (SELECT k1, k2 FROM it_rv_s WHERE k1 = 3)"), "6")
		h.expectRows(q("(k1, k2) = (SELECT k1, k2 FROM it_rv_s WHERE k1 = 7)"))
		h.expectRows(q("(k1, k2) > (SELECT k1, k2 FROM it_rv_s WHERE k1 = 2)"), "6")
		h.expectRows(q("(k1, k2) = (SELECT s.k1, s.k2 FROM it_rv_s s WHERE s.k1 = "+tbl+".k1)"), "1", "6")
		// Parameters.
		if got := h.queryInts("SELECT id FROM "+tbl+" WHERE (k1, v) >= ($1, $2) ORDER BY id", 2, 40); !slices.Equal(got, []string{"4", "6"}) {
			t.Errorf("%s: (k1, v) >= ($1, $2) returned %q, want 4 and 6", tbl, got)
		}
		if got := h.queryInts("SELECT id FROM "+tbl+" WHERE (k1, v) IN ((?, ?), (?, ?)) ORDER BY id", 1, 20, 3, 60); !slices.Equal(got, []string{"2", "6"}) {
			t.Errorf("%s: (k1, v) IN ((?, ?), (?, ?)) returned %q, want 2 and 6", tbl, got)
		}

		// The SELECT list, CASE, HAVING, ORDER BY.
		h.expectRows(`SELECT id, (k1, k2) IN (SELECT k1, k2 FROM it_rv_s), (k1, k2) = (2, 'a'), (k1, k2) < (2, 'a'),
				(k1, k2) IS NOT DISTINCT FROM (NULL, 'a')
			FROM `+tbl+` ORDER BY id`,
			"1|true|false|true|false", "2|false|false|true|false", "3|NULL|true|false|false",
			"4|NULL|NULL|NULL|false", "5|NULL|NULL|NULL|true", "6|true|false|false|false")
		h.expectRows(`SELECT id, CASE (k1, k2) WHEN (1, 'a') THEN 'first' WHEN (3, 'c') THEN 'last' ELSE 'other' END,
				CASE WHEN (k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_s) THEN 'new' ELSE 'old' END
			FROM `+tbl+` ORDER BY id`,
			"1|first|old", "2|other|new", "3|other|old", "4|other|old", "5|other|old", "6|last|old")
		h.expectRows("SELECT k1, COUNT(*) FROM "+tbl+" GROUP BY k1 HAVING (k1, COUNT(*)) IN ((1, 2), (2, 2)) ORDER BY k1",
			"1|2", "2|2")
		h.expectRows("SELECT k1, COUNT(*) FROM "+tbl+" GROUP BY k1 HAVING (k1, COUNT(*)) > (1, 2) ORDER BY k1",
			"2|2", "3|1")
		h.expectRows("SELECT id FROM "+tbl+" WHERE k1 IS NOT NULL ORDER BY (k1, k2) < (2, 'a') DESC, id",
			"1", "2", "3", "6", "4")
		// Joins on rows.
		h.expectRows("SELECT t.id, s.k1 FROM "+tbl+" t JOIN it_rv_s s ON (t.k1, t.k2) = (s.k1, s.k2) ORDER BY t.id",
			"1|1", "6|3")
		h.expectRows("SELECT t.id, s.k2 FROM "+tbl+" t LEFT JOIN it_rv_s s ON (t.k1, t.k2) = (s.k1, s.k2) ORDER BY t.id",
			"1|a", "2|NULL", "3|NULL", "4|NULL", "5|NULL", "6|c")
		h.expectRows("SELECT t.id, s.k1 FROM "+tbl+" t JOIN it_rv_s s ON (t.k1, t.k2) IS NOT DISTINCT FROM (s.k1, s.k2) ORDER BY t.id",
			"1|1", "4|2", "6|3")
	}
}

// The index answers row equalities and row IN on indexed columns; a row IN
// over a subquery with no complete row reads nothing; and a constant row
// comparison that is never true reads nothing.
func TestSQLRowValuePushdown(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRowValues()
	c := h.rawClient()
	for _, tc := range []struct {
		where, query string
		residual     bool
	}{
		{"(k1, k2) = (1, 'a')", "@k1:[1 1] @k2:{a}", false},
		{"(k1, k2) IN ((1, 'b'), (3, 'c'))", "(@k1:[1 1] | @k1:[3 3]) @k2:{b | c}", true},
		{"(k1, k2) IN (SELECT k1, k2 FROM it_rv_s)", "(@k1:[1 1] | @k1:[3 3] | @k1:[9 9]) @k2:{a | c | z}", true},
		{"(k1, v) IN (SELECT k1, 10 FROM it_rv_s)", "(@k1:[1 1] | @k1:[2 2] | @k1:[3 3] | @k1:[9 9]) (@v:[10 10])", true},
		{"(k1, k2) IN (SELECT k1, k2 FROM it_rv_s WHERE k2 IS NULL)", noMatchQuery, true},
		{"(k1, k2) IN (SELECT k1, k2 FROM it_rv_s WHERE false)", noMatchQuery, true},
		{"(k1 + 0, k2) IN (SELECT k1, k2 FROM it_rv_s)", "@k2:{a | c | z}", true},
		{"(k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_s)", "*", true},
		// An ordering comparison: the index finds the rows on the first
		// item's side of the bound, and the rows are checked.
		{"(k1, k2) > (1, 'a')", "@k1:[1 +inf]", true},
		{"(k1, k2) >= (2, 'a')", "@k1:[2 +inf]", true},
		{"(k1, k2) < (2, 'b')", "@k1:[-inf 2]", true},
		{"(k1, k2) <= (1, 'b')", "@k1:[-inf 1]", true},
		{"(1, 'a') < (k1, k2)", "@k1:[1 +inf]", true},
		{"(v, k1, k2) > (20, 1, 'a')", "@v:[20 +inf]", true},
		{"(k1, k2) BETWEEN (1, 'b') AND (2, 'a')", "@k1:[1 +inf] @k1:[-inf 2]", true},
		{"(k1, k2) > (1, 'a') AND (k1, k2) < (3, 'a') AND k2 = 'a'", "@k1:[1 +inf] @k1:[-inf 3] @k2:{a}", true},
		{"(k1, k2) > (1.5, 'a')", "@k1:[2 +inf]", true}, // inclusive at the rounded value
		{"(k1, k2) > (CAST(1 AS DOUBLE PRECISION), 'a')", "@k1:[1 +inf]", true},
		{"(k1, k2) > (CAST(NULL AS INTEGER), 'a')", "*", true},
		{"(k2, k1) > ('a', 1)", "*", true}, // strings are only looked up
		{"(k1 + 0, k2) > (1, 'a')", "*", true},
		{"(k1, k2) > (id, 'a')", "*", true},
		{"k1 > 1 OR (k1 = 1 AND k2 > 'a')", "*", true}, // not a row comparison
		{"NOT (k1, k2) <= (1, 'a')", "*", true},
	} {
		q, residual := h.pushedWhere(c, "SELECT id FROM it_rv_t WHERE "+tc.where)
		if q != tc.query || residual != tc.residual {
			t.Errorf("%s: pushed %q (residual %v), want %q (residual %v)", tc.where, q, residual, tc.query, tc.residual)
		}
	}
	// With parameters, as keyset pagination writes it.
	for _, tc := range []struct {
		where  string
		params []Value
		query  string
	}{
		{"(k1, id) > ($1, $2)", []Value{intValue(typeInt64, 2), intValue(typeInt64, 3)}, "@k1:[2 +inf]"},
		{"(k1, id) <= (?, ?)", []Value{intValue(typeInt64, 2), intValue(typeInt64, 3)}, "@k1:[-inf 2]"},
		{"(k1, k2) >= ($1, $2)", []Value{stringValue("2"), stringValue("a")}, "@k1:[2 +inf]"},
		{"(k1, k2) >= ($1, $2)", []Value{nullValue(typeInt64), stringValue("a")}, "*"},
	} {
		q, residual := h.pushedWhere(c, "SELECT id FROM it_rv_t WHERE "+tc.where, tc.params...)
		if q != tc.query || !residual {
			t.Errorf("%s %v: pushed %q (residual %v), want %q (residual true)", tc.where, tc.params, q, residual, tc.query)
		}
	}
	// A long row IN list: an index union, and a hash set for the rows.
	list := []string{"(1, 'a')", "(2, 'a')"}
	for i := 0; i < inListMin; i++ {
		list = append(list, fmt.Sprintf("(%d, 'x%d')", 10+i, i))
	}
	inList := "(k1, k2) IN (" + strings.Join(list, ", ") + ")"
	if q, residual := h.pushedWhere(c, "SELECT id FROM it_rv_t WHERE "+inList); !strings.HasPrefix(q, "(@k1:[1 1] | @k1:[2 2] | @k1:[10 10] | ") ||
		!strings.Contains(q, ") @k2:{a | a | x0 | x1 | ") || !residual {
		t.Errorf("long row IN list: pushed %q (residual %v)", q, residual)
	}
	before := subqueryStats.inLists.Load()
	h.expectRows("SELECT id FROM it_rv_t WHERE "+inList+" ORDER BY id", "1", "3")
	if n := subqueryStats.inLists.Load() - before; n != 2 {
		t.Errorf("long row IN list: %d rows answered by its hash set, want 2 (rows 1 and 3, which the index finds)", n)
	}
	// On the table without an index nothing is pushed, except that no row
	// can match.
	if q, _ := h.pushedWhere(c, "SELECT id FROM it_rv_n WHERE (k1, k2) IN (SELECT k1, k2 FROM it_rv_s)"); q != "*" {
		t.Errorf("unindexed row IN pushed %q", q)
	}
	if q, _ := h.pushedWhere(c, "SELECT id FROM it_rv_n WHERE (k1, k2) IN (SELECT k1, k2 FROM it_rv_s WHERE k2 IS NULL)"); q != noMatchQuery {
		t.Errorf("unindexed row IN over no complete row pushed %q", q)
	}
	// Planned without reading (see empty.go).
	for _, sql := range []string{
		"SELECT * FROM it_rv_t WHERE (1, 2) = (1, 3)",
		"SELECT * FROM it_rv_t WHERE (1, NULL) = (1, 2)",
		"SELECT * FROM it_rv_t WHERE (k1, k2) IN (SELECT k1, k2 FROM it_rv_s WHERE false)",
		"SELECT * FROM it_rv_t WHERE (k1, k2) = (SELECT k1, k2 FROM it_rv_s) AND (1, 2) > (2, 1)",
		"SELECT COUNT(*) FROM it_rv_t WHERE ROW(1) IS NULL",
	} {
		h.expectNoReads(c, sql, func() { h.query(sql) })
	}
	h.expectRows("SELECT COUNT(*) FROM it_rv_t WHERE (1, 2) < (1, 3)", "6")
	h.expectRows("SELECT COUNT(*) FROM it_rv_t WHERE (1, 2) = (1, 3)", "0")
}

// The bound a row comparison pushes to the index finds every row the
// comparison is true for, whatever the first item's type and values: NULLs,
// NaN and the infinities, a fraction or a double compared with an integer
// column, integers beyond 2^53, NUMERIC rounding, a date compared with a
// timestamp, a timestamp with time zone with a TIMESTAMP, text, and a
// column's missing value. A copy of the table without an index (it_rv_ksn)
// gives the answers.
func TestSQLRowValueKeysetBound(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rv_ks", "it_rv_ksn")
	cols := []struct {
		name, typ string
		values    []string // the rows' values, in turn
		consts    []string // the bounds compared with
	}{
		{"i", "BIGINT", []string{"1", "2", "NULL", "-3", "9007199254740993", "9007199254740992", "2"},
			[]string{"2", "1.5", "-3", "9007199254740992", "9007199254740993", "CAST(2 AS DOUBLE PRECISION)", "'2'"}},
		{"f", "DOUBLE PRECISION", []string{"1.5", "'NaN'", "'Infinity'", "'-Infinity'", "NULL", "2", "-0.5"},
			[]string{"1.5", "'NaN'", "'Infinity'", "'-Infinity'", "2", "CAST(-0.5 AS REAL)"}},
		{"d", "NUMERIC(6,2)", []string{"1.25", "2.50", "NULL", "-1.00", "2.51", "9999.99"},
			[]string{"2.5", "2.505", "1.249", "-1", "CAST(2.5 AS DOUBLE PRECISION)"}},
		{"dt", "DATE", []string{"DATE '2024-01-01'", "DATE '2024-01-02'", "NULL", "DATE '2023-12-31'"},
			[]string{"DATE '2024-01-02'", "TIMESTAMP '2024-01-01 12:00:00'", "TIMESTAMPTZ '2024-01-02 03:00:00+05'", "'2024-01-01'"}},
		{"ts", "TIMESTAMP", []string{"TIMESTAMP '2024-01-01 10:00:00'", "TIMESTAMP '2024-01-02 00:00:00'", "NULL",
			"TIMESTAMP '2024-01-01 23:59:59.999999'"},
			[]string{"TIMESTAMP '2024-01-01 10:00:00'", "DATE '2024-01-02'", "TIMESTAMPTZ '2024-01-01 12:00:00+02'",
				"'2024-01-02 00:00:00'"}},
	}
	var defs, plain []string
	for _, c := range cols {
		defs = append(defs, c.name+" "+c.typ)
		plain = append(plain, c.name+" "+c.typ+" NOINDEX")
	}
	h.exec("CREATE TABLE it_rv_ks (id INTEGER, " + strings.Join(defs, ", ") + ")")
	h.exec("CREATE TABLE it_rv_ksn (id INTEGER NOINDEX, " + strings.Join(plain, ", ") + ")")
	var rows []string
	for id := 1; id <= 30; id++ {
		row := []string{strconv.Itoa(id)}
		for k, c := range cols {
			// Strides prime to the number of values: each column takes all
			// of them, in different combinations.
			row = append(row, c.values[(id*[]int{1, 3, 5, 7, 11}[k]+k)%len(c.values)])
		}
		rows = append(rows, "("+strings.Join(row, ", ")+")")
	}
	// m reads its DEFAULT, 5, in the rows from before it was added.
	for _, tbl := range []string{"it_rv_ks", "it_rv_ksn"} {
		h.exec("INSERT INTO " + tbl + " VALUES " + strings.Join(rows, ", "))
		typ := "INTEGER DEFAULT 5"
		if tbl == "it_rv_ksn" {
			typ += " NOINDEX"
		}
		h.exec("ALTER TABLE " + tbl + " ADD COLUMN m " + typ)
		h.exec("INSERT INTO " + tbl + " (id, i, m) VALUES (31, 1, 4), (32, 2, 6), (33, 3, NULL), (34, 4, 5)")
	}
	cols = append(cols, struct {
		name, typ      string
		values, consts []string
	}{name: "m", consts: []string{"5", "4", "6", "4.5"}})
	c := h.rawClient()
	checked, pushed := 0, 0
	for _, col := range cols {
		for _, x := range col.consts {
			for k, op := range []string{"<", "<=", ">", ">="} {
				conds := []string{fmt.Sprintf("(%s, id) %s (%s, 10)", col.name, op, x)}
				if k%2 == 0 {
					conds = append(conds, fmt.Sprintf("(%s, i, id) %s (%s, 2, 10)", col.name, op, x))
				} else {
					conds = append(conds, fmt.Sprintf("(%s, id) %s (%s, NULL)", col.name, op, x))
				}
				for _, cond := range conds {
					checked++
					if q, _ := h.pushedWhere(c, "SELECT id FROM it_rv_ks WHERE "+cond); q != "*" {
						pushed++
					}
					q := func(tbl string) string { return "SELECT id FROM " + tbl + " WHERE " + cond + " ORDER BY id" }
					want, _ := h.query(q("it_rv_ksn"))
					h.expectRows(q("it_rv_ks"), want...)
				}
			}
		}
	}
	if pushed != checked {
		t.Errorf("%d of %d comparisons pushed a bound, want all", pushed, checked)
	}
	// Keyset pagination with parameters, five rows a page, both ways (below
	// 2^53, which the index sorts as doubles).
	for _, tbl := range []string{"it_rv_ks", "it_rv_ksn"} {
		all, _ := h.query("SELECT i, id FROM " + tbl + " WHERE i < 1000 ORDER BY i, id")
		var got []string
		last := []int64{math.MinInt64, 0}
		for len(got) <= len(all) {
			page := h.queryInts("SELECT i, id FROM "+tbl+" WHERE (i, id) > ($1, $2) AND i < 1000 ORDER BY i, id LIMIT 5", last...)
			if len(page) == 0 {
				break
			}
			got = append(got, page...)
			i, id, _ := strings.Cut(page[len(page)-1], "|")
			last = []int64{mustInt(t, i), mustInt(t, id)}
		}
		if !slices.Equal(got, all) {
			t.Errorf("%s: pages %q, want %q", tbl, got, all)
		}
		var back []string
		last = []int64{math.MaxInt64, 0}
		for len(back) <= len(all) {
			page := h.queryInts("SELECT i, id FROM "+tbl+" WHERE (i, id) <= (?, ?) AND i < 1000 ORDER BY i DESC, id DESC LIMIT 5", last...)
			if len(page) == 0 {
				break
			}
			back = append(back, page...)
			i, id, _ := strings.Cut(page[len(page)-1], "|")
			last = []int64{mustInt(t, i), mustInt(t, id) - 1}
		}
		slices.Reverse(back)
		if !slices.Equal(back, all) {
			t.Errorf("%s: pages backwards %q, want %q", tbl, back, all)
		}
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A row IN subquery probes a hash set of its rows, built once per statement;
// the answers are those of comparing row by row, with NULLs in either row,
// and with values of other types (cross-type equality, conversions).
func TestSQLRowValueHashSet(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rv_k", "it_rv_p")
	// it_rv_k: (i, 'v<i>') for i = 1 … 20, (NULL, 'v5') and (7, NULL).
	h.exec("CREATE TABLE it_rv_k (a INTEGER NOINDEX, b VARCHAR NOINDEX)")
	var vals []string
	for i := 1; i <= 20; i++ {
		vals = append(vals, fmt.Sprintf("(%d, 'v%d')", i, i))
	}
	h.exec("INSERT INTO it_rv_k VALUES " + strings.Join(vals, ", ") + ", (NULL, 'v5'), (7, NULL)")
	h.exec(`CREATE TABLE it_rv_p (id INTEGER, a INTEGER NOINDEX, b VARCHAR NOINDEX, d NUMERIC(10,2) NOINDEX,
		f DOUBLE NOINDEX)`)
	h.exec(`INSERT INTO it_rv_p VALUES (1, 1, 'v1', 1, 1), (2, 2, 'v3', 2, 2), (3, 9, 'v5', 9, 9), (4, 7, 'x', 7, 7),
		(5, NULL, 'v4', NULL, NULL), (6, NULL, 'zz', NULL, NULL), (7, NULL, NULL, NULL, NULL), (8, 21, 'v21', 21, 21)`)
	in := func(x, where string) string {
		return fmt.Sprintf("SELECT id, %s IN (SELECT a, b FROM it_rv_k%s), %s NOT IN (SELECT a, b FROM it_rv_k%s) FROM it_rv_p ORDER BY id",
			x, where, x, where)
	}
	// All of it_rv_k: probe 3 agrees with (NULL, v5) on b, 4 with (7, NULL)
	// on a, 5 with (4, v4) on b; 6 and 7 have no item in common with
	// (7, NULL).
	all := []string{"1|true|false", "2|false|true", "3|NULL|NULL", "4|NULL|NULL", "5|NULL|NULL",
		"6|NULL|NULL", "7|NULL|NULL", "8|false|true"}
	w := h.expectWork(in("(a, b)", ""), all...)
	if w.runs != 2 || w.inSets != 2 {
		t.Errorf("row IN subqueries: %+v, want 2 runs and 2 hash sets", w)
	}
	// Without (7, NULL).
	notNullB := []string{"1|true|false", "2|false|true", "3|NULL|NULL", "4|false|true", "5|NULL|NULL",
		"6|false|true", "7|NULL|NULL", "8|false|true"}
	h.expectRows(in("(a, b)", " WHERE b IS NOT NULL"), notNullB...)
	// Complete rows only.
	complete := []string{"1|true|false", "2|false|true", "3|false|true", "4|false|true", "5|NULL|NULL",
		"6|false|true", "7|NULL|NULL", "8|false|true"}
	h.expectRows(in("(a, b)", " WHERE a IS NOT NULL AND b IS NOT NULL"), complete...)
	// NUMERIC compared with INTEGER (hashed), DOUBLE with INTEGER and text
	// with INTEGER (compared row by row): the same answers.
	for _, x := range []string{"(d, b)", "(f, b)", "(CAST(a AS VARCHAR), b)"} {
		h.expectRows(in(x, ""), all...)
	}
	// A WHERE probe, also as a DELETE.
	h.expectRows("SELECT id FROM it_rv_p WHERE (a, b) IN (SELECT a, b FROM it_rv_k) ORDER BY id", "1")
	h.expectRows("SELECT id FROM it_rv_p WHERE (a, b) NOT IN (SELECT a, b FROM it_rv_k WHERE b IS NOT NULL) ORDER BY id",
		"2", "4", "6", "8")

	// The same rows written as a literal list (at least inListMin of them)
	// probe a hash set too, built once per list, with the same answers.
	inList := func(x string, rows []string) string {
		list := strings.Join(rows, ", ")
		return fmt.Sprintf("SELECT id, %s IN (%s), %s NOT IN (%s) FROM it_rv_p ORDER BY id", x, list, x, list)
	}
	withNulls := append(slices.Clip(vals), "(NULL, 'v5')", "(7, NULL)")
	before := subqueryStats.inLists.Load()
	w = h.expectWork(inList("(a, b)", withNulls), all...)
	if n := subqueryStats.inLists.Load() - before; w.runs != 0 || w.inSets != 2 || n != 16 {
		t.Errorf("row IN lists: %+v and %d rows answered by a hash set, want no runs, 2 hash sets and 16 rows", w, n)
	}
	h.expectRows(inList("(a, b)", withNulls[:21]), notNullB...)
	h.expectRows(inList("(a, b)", vals), complete...)
	var swapped []string
	for _, r := range withNulls {
		a, b, _ := strings.Cut(strings.Trim(r, "()"), ", ")
		swapped = append(swapped, "ROW("+b+", "+a+")")
	}
	h.expectRows(inList("ROW(b, a)", swapped), all...)
	for _, x := range []string{"(d, b)", "(f, b)", "(CAST(a AS VARCHAR), b)"} {
		h.expectRows(inList(x, withNulls), all...)
	}
	// Three items, and a WHERE probe.
	var three []string
	for _, r := range vals {
		three = append(three, strings.Replace(r, ")", ", 0)", 1))
	}
	h.expectRows("SELECT id FROM it_rv_p WHERE (a, b, 0) IN ("+strings.Join(three, ", ")+") ORDER BY id", "1")
	h.expectRows("SELECT id FROM it_rv_p WHERE (a, b, 0) NOT IN ("+strings.Join(three, ", ")+") ORDER BY id",
		"2", "3", "4", "6", "8")
	// Text compared with numbers can fail to convert: such a row is
	// compared row by row, with the error the chain of comparisons gives.
	h.expectError(inList("(b, a)", withNulls), `invalid input syntax for type bigint: "v`)
}

// Rows in the conditions of UPDATE, DELETE and MERGE, with RETURNING.
func TestSQLRowValueDML(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRowValues()
	for _, tbl := range []string{"it_rv_t", "it_rv_n"} {
		if n := h.exec("UPDATE " + tbl + " SET v = v + 1 WHERE (k1, k2) IN (SELECT k1, k2 FROM it_rv_s)"); n != 2 {
			t.Errorf("%s: UPDATE … IN updated %d rows, want 2", tbl, n)
		}
		if n := h.exec("UPDATE " + tbl + " SET v = v + 100 FROM it_rv_s s WHERE (" + tbl + ".k1, " + tbl + ".k2) = (s.k1, s.k2) AND (s.k1, s.k2) <> (3, 'c')"); n != 1 {
			t.Errorf("%s: UPDATE … FROM updated %d rows, want 1", tbl, n)
		}
		if n := h.exec("UPDATE " + tbl + " SET v = CASE WHEN (k1, k2) > (1, 'b') THEN v * 10 ELSE v END WHERE (k1, k2) IS NOT NULL"); n != 4 {
			t.Errorf("%s: UPDATE … CASE updated %d rows, want 4", tbl, n)
		}
		h.expectRows("SELECT id, v FROM "+tbl+" ORDER BY id", "1|111", "2|20", "3|300", "4|40", "5|50", "6|610")
		if n := h.exec("DELETE FROM " + tbl + " AS d WHERE (d.k1, d.k2) = (2, 'a')"); n != 1 {
			t.Errorf("%s: DELETE … = deleted %d rows, want 1", tbl, n)
		}
		if n := h.exec("DELETE FROM " + tbl + " USING it_rv_s s WHERE (" + tbl + ".k1, " + tbl + ".k2) = (s.k1, s.k2)"); n != 2 {
			t.Errorf("%s: DELETE … USING deleted %d rows, want 2", tbl, n)
		}
		h.expectRows("SELECT id FROM "+tbl+" ORDER BY id", "2", "4", "5")
		h.expectRows("DELETE FROM "+tbl+" WHERE (k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_s) RETURNING id", "2")
		// MERGE ON a row: (1, a) and (3, c) are back as new rows, (2, NULL)
		// and (9, z) too, since NULL never matches.
		// MERGE ON a row: (1, a) and (3, c) match; (2, NULL) and (9, z)
		// don't, since NULL never matches.
		h.exec("INSERT INTO " + tbl + " VALUES (1, 1, 'a', 10), (6, 3, 'c', 60)")
		got, _ := h.query(`MERGE INTO ` + tbl + ` t USING it_rv_s s ON (t.k1, t.k2) = (s.k1, s.k2)
			WHEN MATCHED AND (t.id, t.v) < (5, 0) THEN DELETE
			WHEN MATCHED THEN UPDATE SET v = 0
			WHEN NOT MATCHED THEN INSERT (id, k1, k2, v) VALUES (100 + s.k1, s.k1, s.k2, -1)
			RETURNING merge_action(), id`)
		slices.Sort(got)
		if want := []string{"DELETE|1", "INSERT|102", "INSERT|109", "UPDATE|6"}; !slices.Equal(got, want) {
			t.Errorf("%s: MERGE returned %q, want %q", tbl, got, want)
		}
		h.expectRows("SELECT id, k1, k2, v FROM "+tbl+" ORDER BY id",
			"4|2|NULL|40", "5|NULL|a|50", "6|3|c|0", "102|2|NULL|-1", "109|9|z|-1")
	}
}

// dbt's delete+insert incremental strategy with a list unique_key
// (default__get_delete_insert_merge_sql in dbt-core 1.9, as rendered by
// the redis_adbc adapter: the target is schema.table, the source a
// temporary table), for a 2- and a 3-column key. As in Postgres, a target
// row with a NULL key item is never deleted, so a source row with the same
// key is inserted next to it.
func TestSQLRowValueDbtDeleteInsert(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rv_dbt2", "it_rv_dbt3")
	dropTmp := func() {
		h.exec("DROP TABLE IF EXISTS pg_temp.it_rv_dbt2__dbt_tmp")
		h.exec("DROP TABLE IF EXISTS pg_temp.it_rv_dbt3__dbt_tmp")
	}
	t.Cleanup(dropTmp)
	script := func(target, source, key, cols, predicates string) string {
		quoted := strings.Split(cols, ", ")
		for i, c := range quoted {
			quoted[i] = `"` + c + `"`
		}
		q := strings.Join(quoted, ", ")
		return `delete from ` + target + ` as DBT_INTERNAL_DEST
        where (` + key + `) in (
            select distinct ` + key + `
            from ` + source + ` as DBT_INTERNAL_SOURCE
        )` + predicates + `;



    insert into ` + target + ` (` + q + `)
    (
        select ` + q + `
        from ` + source + `
    )`
	}

	// Two columns. The first run built the table; the second run's source
	// updates (1, a), adds (2, b), and has rows with NULL keys.
	h.exec("CREATE TABLE it_rv_dbt2 (k1 INTEGER, k2 VARCHAR, v INTEGER)")
	h.exec("INSERT INTO it_rv_dbt2 VALUES (1, 'a', 10), (1, 'b', 20), (2, 'a', 30), (NULL, 'a', 40), (3, NULL, 50)")
	h.exec(`create temporary table it_rv_dbt2__dbt_tmp as (
		select 1 as k1, 'a' as k2, 11 as v union all select 2, 'b', 21
		union all select NULL, 'a', 41 union all select 3, NULL, 51)`)
	h.exec(script("public.it_rv_dbt2", "it_rv_dbt2__dbt_tmp", "k1, k2", "k1, k2, v", ""))
	h.expectRows("SELECT k1, k2, v FROM it_rv_dbt2 ORDER BY k1, k2, v",
		"1|a|11", "1|b|20", "2|a|30", "2|b|21", "3|NULL|50", "3|NULL|51", "NULL|a|40", "NULL|a|41")

	// Three columns, with incremental_predicates.
	h.exec("CREATE TABLE it_rv_dbt3 (k1 INTEGER, k2 VARCHAR, k3 DATE, v INTEGER)")
	h.exec(`INSERT INTO it_rv_dbt3 VALUES (1, 'a', DATE '2024-01-01', 1), (1, 'a', DATE '2024-01-02', 2),
		(1, 'b', DATE '2024-01-01', 3), (2, 'a', NULL, 4), (2, 'a', DATE '2024-01-01', 5), (1, 'b', DATE '2024-01-03', 200)`)
	h.exec(`create temporary table it_rv_dbt3__dbt_tmp as (
		select 1 as k1, 'a' as k2, DATE '2024-01-02' as k3, 20 as v
		union all select 1, 'b', DATE '2024-01-01', 30
		union all select 2, 'a', NULL, 40
		union all select 3, 'c', DATE '2024-03-01', 50
		union all select 1, 'b', DATE '2024-01-03', 60)`)
	h.exec(script("public.it_rv_dbt3", "it_rv_dbt3__dbt_tmp", "k1, k2, k3", "k1, k2, k3, v", `

                and DBT_INTERNAL_DEST.v < 100
            `))
	// (1, b, 2024-01-03) isn't deleted: its v is 200.
	h.expectRows("SELECT k1, k2, k3, v FROM it_rv_dbt3 ORDER BY k1, k2, k3, v",
		"1|a|2024-01-01|1", "1|a|2024-01-02|20", "1|b|2024-01-01|30", "1|b|2024-01-03|60", "1|b|2024-01-03|200",
		"2|a|2024-01-01|5", "2|a|NULL|4", "2|a|NULL|40", "3|c|2024-03-01|50")
	dropTmp()
}

// Row errors are reported when the statement is planned: also on an empty
// table, in dbt's `where false limit 0` form and by ExecuteSchema; a write
// that fails changes nothing.
func TestSQLRowValueErrors(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRowValues()
	cases := []struct{ where, want string }{
		{"(k1, k2) IN (SELECT k1 FROM it_rv_s)", "subquery has too few columns"},
		{"(k1, k2) IN (SELECT k1, k2, k1 FROM it_rv_s)", "subquery has too many columns"},
		{"k1 IN (SELECT k1, k2 FROM it_rv_s)", "subquery has too many columns"},
		{"(k1, k2) <> ALL (SELECT k1 FROM it_rv_s)", "subquery has too few columns"},
		{"(k1, k2) = (SELECT k1 FROM it_rv_s)", "subquery has too few columns"},
		{"(k1, k2) = (1, 'a', 2)", "unequal number of entries in row expressions"},
		{"(k1, k2) IN ((1, 'a'), (2))", "operator does not exist: record = bigint"},
		{"(k1, k2) = k1", "operator does not exist: record = integer"},
		{"k2 = (k1, k2)", "operator does not exist: varchar = record"},
		{"(k1, k2)", "argument of WHERE must be type boolean, not type record"},
		{"upper((k1, k2)) = 'a'", "function upper(record) does not exist"},
		{"(SELECT k1, k2 FROM it_rv_s LIMIT 1) = (k1, k2)", "subquery must return only one column"},
		{"(k1, nosuch) = (1, 2)", `column "nosuch" does not exist in table "it_rv_e"`},
		{"(k1, k2) IN (SELECT k1, nosuch FROM it_rv_s)", `column "nosuch" does not exist in table "it_rv_s"`},
	}
	for _, c := range cases {
		for _, sql := range []string{
			"SELECT * FROM it_rv_e WHERE " + c.where,
			"SELECT * FROM it_rv_t WHERE " + c.where,
			"select * from (SELECT * FROM it_rv_e WHERE " + c.where + ") as __dbt_sbq where false limit 0",
		} {
			want := c.want
			if strings.Contains(want, `in table "it_rv_e"`) && strings.Contains(sql, "it_rv_t") {
				want = strings.Replace(want, "it_rv_e", "it_rv_t", 1)
			}
			h.expectErrorText(sql, want)
		}
		want := c.want
		h.expectSchemaErrorText("SELECT * FROM it_rv_e WHERE "+c.where, want)
	}
	for _, c := range []struct{ sql, want string }{
		{"DELETE FROM it_rv_t WHERE (k1, k2) IN (SELECT k1 FROM it_rv_s)", "subquery has too few columns"},
		{"UPDATE it_rv_t SET v = 0 WHERE (k1, k2) = (1, 'a', 2)", "unequal number of entries in row expressions"},
		{"UPDATE it_rv_t SET v = CASE WHEN (k1, k2) = 1 THEN 0 END", "operator does not exist: record = bigint"},
		{"UPDATE it_rv_t SET v = (1, 2)", `column "v" is of type integer but expression is of type record`},
		{"MERGE INTO it_rv_t t USING it_rv_s s ON (t.k1, t.k2) = (s.k1) WHEN MATCHED THEN DELETE",
			"operator does not exist: record = integer"},
		{"INSERT INTO it_rv_t (id) SELECT 1 WHERE (1, 2) IN (SELECT 1)", "subquery has too few columns"},
		{"INSERT INTO it_rv_t (id) VALUES ((1, 2))", `column "id" is of type integer but expression is of type record`},
		{"INSERT INTO it_rv_t (id) SELECT (1, 2)", `column "id" is of type integer but expression is of type record`},
	} {
		h.expectErrorText(c.sql, c.want)
	}
	h.expectRows("SELECT id, v FROM it_rv_t ORDER BY id", "1|10", "2|20", "3|30", "4|40", "5|50", "6|60")
	// A CHECK on a row, and a view.
	h.dropTables("it_rv_c")
	h.exec("CREATE TABLE it_rv_c (a INTEGER, b INTEGER, CHECK ((a, b) <> (0, 0)))")
	h.exec("INSERT INTO it_rv_c VALUES (0, 1), (1, 0), (NULL, 0)")
	h.expectError("INSERT INTO it_rv_c VALUES (0, 0)", `new row for relation "it_rv_c" violates check constraint "it_rv_c_check"`)
	h.exec("DROP VIEW IF EXISTS it_rv_v")
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS it_rv_v") })
	h.exec("CREATE VIEW it_rv_v AS SELECT id, k1, k2 FROM it_rv_t WHERE (k1, k2) IN (SELECT k1, k2 FROM it_rv_s)")
	h.expectRows("SELECT id FROM it_rv_v ORDER BY id", "1", "6")
	h.expectRows("SELECT id FROM it_rv_v WHERE (k1, k2) > (1, 'a') ORDER BY id", "6")
	h.expectErrorText("CREATE VIEW it_rv_v2 AS SELECT (k1, k2) FROM it_rv_t", `column "row" has pseudo-type record`)
}

// dbt's delete+insert of 10,000 keys into a table of 200,000 rows (1,000
// into 20,000 unless REDIS_ROW_VALUES_ROWS is set): the IN subquery runs
// once and is probed as a hash set, and the results match the other ways of
// writing it. The keys are scattered (each key column has more than
// maxUnionTerms distinct values at 200,000 rows, so every row is read) or
// clustered (k1 has at most 1,000, so the index fetches only the rows with
// those values). Literal row IN lists probe a hash set too, and keyset
// pagination reads each page from the index from the last page's bound on.
// The timings are logged (go test -v).
func TestSQLRowValueScale(t *testing.T) {
	h := newSQLHarness(t)
	n := 20000
	if s := os.Getenv("REDIS_ROW_VALUES_ROWS"); s != "" {
		var err error
		if n, err = strconv.Atoi(s); err != nil || n%200 != 0 {
			t.Fatalf("REDIS_ROW_VALUES_ROWS=%s: want a multiple of 200", s)
		}
	}
	keys := n / 20
	h.dropTables("it_rv_big", "it_rv_big2", "it_rv_big3", "it_rv_page")
	t.Cleanup(func() {
		h.exec("DROP TABLE IF EXISTS pg_temp.it_rv_big__dbt_tmp")
		h.exec("DROP TABLE IF EXISTS pg_temp.it_rv_big2__dbt_tmp")
	})
	start := time.Now()
	// (k1, k2) is unique: k1 = v / 10 and k2 = v % 1009.
	h.exec(fmt.Sprintf(`CREATE TABLE it_rv_big AS SELECT g / 10 AS k1, 'k' || (g %% 1009) AS k2, g AS v
		FROM generate_series(0, %d) AS s(g)`, n-1))
	t.Logf("loaded %d rows in %v", n, time.Since(start).Round(time.Millisecond))
	for _, tbl := range []string{"it_rv_big2", "it_rv_big3"} {
		h.exec("CREATE TABLE " + tbl + " AS SELECT * FROM it_rv_big")
	}

	// Literal row IN lists of scattered keys: 2,000 rows, more than the
	// index takes as a union, so every row is read and probes the list's
	// hash set; and 1,000, which the index answers as a union of each
	// column's values, the rows it finds probing the hash set.
	var list []string
	distinct := map[int]bool{}
	for j := 0; j < 2*maxUnionTerms; j++ {
		g := j * 7919 % n
		distinct[g] = true
		list = append(list, fmt.Sprintf("(%d, 'k%d')", g/10, g%1009))
	}
	timedList := func(what, sql string, want, probes int) {
		t.Helper()
		start := time.Now()
		l, i := subqueryStats.inLists.Load(), subqueryStats.inSets.Load()
		h.expectRows(sql, strconv.Itoa(want))
		got := subqueryStats.inLists.Load() - l
		t.Logf("%s: %v (rows answered by the list's hash set %d, hash sets %d)", what, time.Since(start).Round(time.Millisecond),
			got, subqueryStats.inSets.Load()-i)
		if probes > 0 && got != int64(probes) || got == 0 {
			t.Errorf("%s: %d rows answered by the list's hash set, want %d", what, got, probes)
		}
	}
	long := "(k1, k2) IN (" + strings.Join(list, ", ") + ")"
	timedList("count … literal row IN list of 2,000 rows", "SELECT COUNT(*) FROM it_rv_big WHERE "+long, len(distinct), n)
	timedList("count … literal row NOT IN list of 2,000 rows", "SELECT COUNT(*) FROM it_rv_big WHERE NOT "+long, n-len(distinct), n)
	distinct = map[int]bool{}
	for j := 0; j < maxUnionTerms; j++ {
		distinct[j*7919%n] = true
	}
	timedList("count … literal row IN list of 1,000 rows (an index union)",
		"SELECT COUNT(*) FROM it_rv_big WHERE (k1, k2) IN ("+strings.Join(list[:maxUnionTerms], ", ")+")", len(distinct), 0)

	// Keyset pagination, 1,000 rows a page, with parameters: the index
	// finds the rows from the last page's ts on (ts has four rows a value).
	h.exec(fmt.Sprintf(`CREATE TABLE it_rv_page AS SELECT g / 4 AS ts, g AS id FROM generate_series(0, %d) AS s(g)`, n-1))
	start = time.Now()
	seen, last := 0, []int64{-1, -1}
	for seen <= n {
		page := h.queryInts("SELECT ts, id FROM it_rv_page WHERE (ts, id) > ($1, $2) ORDER BY ts, id LIMIT 1000", last...)
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			if want := fmt.Sprintf("%d|%d", seen/4, seen); r != want {
				t.Fatalf("keyset pagination after %v: row %s, want %s", last, r, want)
			}
			seen++
		}
		ts, id, _ := strings.Cut(page[len(page)-1], "|")
		last = []int64{mustInt(t, ts), mustInt(t, id)}
	}
	if seen != n {
		t.Errorf("keyset pagination read %d rows, want %d", seen, n)
	}
	t.Logf("keyset pagination, %d pages of 1,000 rows: %v", (n+999)/1000, time.Since(start).Round(time.Millisecond))
	start = time.Now()
	for i := 0; i < 10; i++ {
		at := int64(n - 100*(i+1) - 1)
		if page := h.queryInts("SELECT ts, id FROM it_rv_page WHERE (ts, id) > (?, ?) ORDER BY ts, id LIMIT 100", at/4, at); len(page) != 100 {
			t.Errorf("keyset page after (%d, %d): %d rows, want 100", at/4, at, len(page))
		}
	}
	t.Logf("keyset pagination, one of the last 10 pages of 100 rows: %v", (time.Since(start) / 10).Round(time.Millisecond))
	h.exec(`create temporary table it_rv_big__dbt_tmp as (select k1, k2, v + 1 as v from it_rv_big where v % 20 = 0)`)
	h.exec(fmt.Sprintf(`create temporary table it_rv_big2__dbt_tmp as (select k1, k2, v + 1 as v from it_rv_big where k1 < %d)`, n/200))
	for _, tmp := range []string{"it_rv_big__dbt_tmp", "it_rv_big2__dbt_tmp"} {
		h.expectRows("SELECT COUNT(*) FROM "+tmp, strconv.Itoa(keys))
	}
	timed := func(what, sql string, want int64) {
		t.Helper()
		start := time.Now()
		r, s, i := subqueryStats.runs.Load(), subqueryStats.semiJoins.Load(), subqueryStats.inSets.Load()
		if got := h.exec(sql); got != want {
			t.Errorf("%s: %d rows, want %d", what, got, want)
		}
		t.Logf("%s: %v (subquery runs %d, semi-joins %d, hash sets %d)", what, time.Since(start).Round(time.Millisecond),
			subqueryStats.runs.Load()-r, subqueryStats.semiJoins.Load()-s, subqueryStats.inSets.Load()-i)
	}
	deleteSQL := func(target, source string) string {
		return `delete from public.` + target + ` as DBT_INTERNAL_DEST
        where (k1, k2) in (
            select distinct k1, k2
            from ` + source + ` as DBT_INTERNAL_SOURCE
        )`
	}
	insertSQL := func(target, source string) string {
		return `insert into public.` + target + ` ("k1", "k2", "v") (select "k1", "k2", "v" from ` + source + `)`
	}
	r, i := subqueryStats.runs.Load(), subqueryStats.inSets.Load()
	timed("dbt delete, scattered keys", deleteSQL("it_rv_big", "it_rv_big__dbt_tmp"), int64(keys))
	if runs, sets := subqueryStats.runs.Load()-r, subqueryStats.inSets.Load()-i; runs != 1 || sets != 1 {
		t.Errorf("row IN delete: %d subquery runs and %d hash sets, want 1 and 1", runs, sets)
	}
	timed("dbt insert", insertSQL("it_rv_big", "it_rv_big__dbt_tmp"), int64(keys))
	timed("dbt delete, clustered keys", deleteSQL("it_rv_big2", "it_rv_big2__dbt_tmp"), int64(keys))
	timed("dbt insert", insertSQL("it_rv_big2", "it_rv_big2__dbt_tmp"), int64(keys))
	timed("delete … using (the adapter's workaround), scattered keys", `delete from public.it_rv_big3 as DBT_INTERNAL_DEST
		using it_rv_big__dbt_tmp as DBT_INTERNAL_SOURCE
		where DBT_INTERNAL_SOURCE.k1 = DBT_INTERNAL_DEST.k1 and DBT_INTERNAL_SOURCE.k2 = DBT_INTERNAL_DEST.k2`, int64(keys))
	h.exec(insertSQL("it_rv_big3", "it_rv_big__dbt_tmp"))
	timed("delete … where exists, scattered keys", `delete from public.it_rv_big3 as d where exists (
		select 1 from it_rv_big__dbt_tmp s where (s.k1, s.k2) = (d.k1, d.k2))`, int64(keys))
	start = time.Now()
	h.expectRows("SELECT COUNT(*) FROM it_rv_big WHERE (k1, k2) NOT IN (SELECT k1, k2 FROM it_rv_big__dbt_tmp)",
		strconv.Itoa(n-keys))
	t.Logf("count … row NOT IN, scattered keys: %v", time.Since(start).Round(time.Millisecond))
	// Each replaced row has v + 1: the sum of 0 … n-1, plus one per key.
	for _, tbl := range []string{"it_rv_big", "it_rv_big2"} {
		h.expectRows("SELECT COUNT(*), SUM(v) FROM "+tbl, fmt.Sprintf("%d|%d", n, n*(n-1)/2+keys))
	}
	h.expectRows("SELECT COUNT(*) FROM it_rv_big WHERE (k1, k2) IN (SELECT k1, k2 FROM it_rv_big__dbt_tmp) AND v % 20 = 1",
		strconv.Itoa(keys))
	h.expectRows("SELECT COUNT(*) FROM it_rv_big3", strconv.Itoa(n-keys))
}
