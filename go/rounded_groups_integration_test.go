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

// Integration tests for GROUP BY, DISTINCT, MIN, MAX and SUM on values the
// index holds as one double, or replies with the same 12 digits (see
// indexValueExact). They run against the Redis server at REDIS_URI,
// standalone or a cluster, like sql_integration_test.go, in every
// aggregate_pushdown mode. The expected rows are Postgres 16's. Each query
// runs on an indexed column, which the all mode groups in the index, and on
// a NOINDEX copy, which the driver groups.

import (
	"strings"
	"testing"
)

// roundedColumns are it_rgroups' indexed columns, each with a NOINDEX copy
// named <c>n.
var roundedColumns = []string{"b", "ts", "n", "m", "q", "d"}

// setupRoundedGroups creates it_rgroups. Rows 1 and 2, 3 and 4, and 7 and
// 8 hold pairs of values that the index can't tell apart in its replies:
//
//   - b BIGINT: 2^53 + 1 and 2^53, -2^53 and -2^53 - 1, 2^63 - 1 and
//     2^63 - 2, each pair one double.
//   - ts TIMESTAMP (microseconds): 9999-12-31 23:59:59.999999 and .999998,
//     one double replied as 10000-01-01; 1000-01-01 00:00:00.000001 and
//     00:00:00; 2^53 + 1 and 2^53 µs, in 2255.
//   - n NUMERIC (38, 9 digits): 10000000.000000002 and .000000001, one
//     double; 1000000.000000001 and 999999.999999999, two doubles both
//     replied as 1000000; 999.999999999 and .999999998, which have 12
//     digits and are replied exactly.
//   - m NUMERIC(20,17): 0.10000000000000001 and 0.1, and their negatives,
//     one double each; 0.00999999999999999 and 0.01, both replied as 0.01.
//   - q NUMERIC(14,2): 123456789012.35 and .34, their negatives, and
//     99999999999.99 and .98, replied the same in pairs.
//   - d DOUBLE PRECISION: 0.30000000000000004 and 0.3, and 5.0000000000001
//     and 5, replied as 0.3 and 5.
//
// Rows 5 and 9 hold the same small values, and row 6 NULLs.
func (h *sqlHarness) setupRoundedGroups() {
	h.t.Helper()
	h.dropTables("it_rgroups")
	h.exec(`CREATE TABLE it_rgroups (id INTEGER, g VARCHAR, b BIGINT, bn BIGINT NOINDEX, ts TIMESTAMP, tsn TIMESTAMP NOINDEX,
		n NUMERIC, nn NUMERIC NOINDEX, m NUMERIC(20,17), mn NUMERIC(20,17) NOINDEX, q NUMERIC(14,2), qn NUMERIC(14,2) NOINDEX,
		d DOUBLE PRECISION, dn DOUBLE PRECISION NOINDEX)`)
	rows := [][]string{
		{"1", "'a'", "9007199254740993", "TIMESTAMP '9999-12-31 23:59:59.999999'", "10000000.000000002", "0.10000000000000001", "123456789012.35", "0.30000000000000004"},
		{"2", "'a'", "9007199254740992", "TIMESTAMP '9999-12-31 23:59:59.999998'", "10000000.000000001", "0.1", "123456789012.34", "0.3"},
		{"3", "'b'", "-9007199254740992", "TIMESTAMP '1000-01-01 00:00:00.000001'", "1000000.000000001", "-0.1", "-123456789012.34", "5"},
		{"4", "'b'", "-9007199254740993", "TIMESTAMP '1000-01-01 00:00:00'", "999999.999999999", "-0.10000000000000001", "-123456789012.35", "5.0000000000001"},
		{"5", "'a'", "5", "TIMESTAMP '2024-06-01 00:00:00'", "5", "0.5", "5", "1.5"},
		{"6", "'b'", "NULL", "NULL", "NULL", "NULL", "NULL", "NULL"},
		{"7", "'a'", "9223372036854775807", "TIMESTAMP '2255-06-05 23:47:34.740993'", "999.999999999", "0.00999999999999999", "99999999999.99", "1e300"},
		{"8", "'a'", "9223372036854775806", "TIMESTAMP '2255-06-05 23:47:34.740992'", "999.999999998", "0.01", "99999999999.98", "1e300"},
		{"9", "'b'", "5", "TIMESTAMP '2024-06-01 00:00:00'", "5", "0.5", "5", "1.5"},
	}
	var values []string
	for _, r := range rows {
		vals := []string{r[0], r[1]}
		for _, v := range r[2:] {
			vals = append(vals, v, v)
		}
		values = append(values, "("+strings.Join(vals, ", ")+")")
	}
	h.exec("INSERT INTO it_rgroups VALUES " + strings.Join(values, ", "))
}

// roundedAgree runs sql with {c} replaced by c and by its NOINDEX copy, and
// checks that both give want.
func (h *sqlHarness) roundedAgree(c, sql string, want ...string) {
	h.t.Helper()
	for _, col := range []string{c, c + "n"} {
		h.expectRows(strings.ReplaceAll(sql, "{c}", col), want...)
	}
}

// roundedWant is what Postgres 16 returns for a column of it_rgroups: its
// groups (with COUNT(*)), its groups within g, and the MIN, MAX and COUNT
// of each g.
type roundedWant struct {
	groups, byG, minMax []string
}

var roundedWants = map[string]roundedWant{
	"b": {
		groups: []string{"-9007199254740993|1", "-9007199254740992|1", "5|2", "9007199254740992|1", "9007199254740993|1",
			"9223372036854775806|1", "9223372036854775807|1", "NULL|1"},
		byG: []string{"a|5|1", "a|9007199254740992|1", "a|9007199254740993|1", "a|9223372036854775806|1", "a|9223372036854775807|1",
			"b|-9007199254740993|1", "b|-9007199254740992|1", "b|5|1", "b|NULL|1"},
		minMax: []string{"a|5|9223372036854775807|5", "b|-9007199254740993|5|3"},
	},
	"ts": {
		groups: []string{"1000-01-01T00:00:00|1", "1000-01-01T00:00:00.000001|1", "2024-06-01T00:00:00|2",
			"2255-06-05T23:47:34.740992|1", "2255-06-05T23:47:34.740993|1", "9999-12-31T23:59:59.999998|1",
			"9999-12-31T23:59:59.999999|1", "NULL|1"},
		byG: []string{"a|2024-06-01T00:00:00|1", "a|2255-06-05T23:47:34.740992|1", "a|2255-06-05T23:47:34.740993|1",
			"a|9999-12-31T23:59:59.999998|1", "a|9999-12-31T23:59:59.999999|1", "b|1000-01-01T00:00:00|1",
			"b|1000-01-01T00:00:00.000001|1", "b|2024-06-01T00:00:00|1", "b|NULL|1"},
		minMax: []string{"a|2024-06-01T00:00:00|9999-12-31T23:59:59.999999|5", "b|1000-01-01T00:00:00|2024-06-01T00:00:00|3"},
	},
	"n": {
		groups: []string{"5.000000000|2", "999.999999998|1", "999.999999999|1", "999999.999999999|1", "1000000.000000001|1",
			"10000000.000000001|1", "10000000.000000002|1", "NULL|1"},
		byG: []string{"a|5.000000000|1", "a|999.999999998|1", "a|999.999999999|1", "a|10000000.000000001|1",
			"a|10000000.000000002|1", "b|5.000000000|1", "b|999999.999999999|1", "b|1000000.000000001|1", "b|NULL|1"},
		minMax: []string{"a|5.000000000|10000000.000000002|5", "b|5.000000000|1000000.000000001|3"},
	},
	"m": {
		groups: []string{"-0.10000000000000001|1", "-0.10000000000000000|1", "0.00999999999999999|1", "0.01000000000000000|1",
			"0.10000000000000000|1", "0.10000000000000001|1", "0.50000000000000000|2", "NULL|1"},
		byG: []string{"a|0.00999999999999999|1", "a|0.01000000000000000|1", "a|0.10000000000000000|1", "a|0.10000000000000001|1",
			"a|0.50000000000000000|1", "b|-0.10000000000000001|1", "b|-0.10000000000000000|1", "b|0.50000000000000000|1", "b|NULL|1"},
		minMax: []string{"a|0.00999999999999999|0.50000000000000000|5", "b|-0.10000000000000001|0.50000000000000000|3"},
	},
	"q": {
		groups: []string{"-123456789012.35|1", "-123456789012.34|1", "5.00|2", "99999999999.98|1", "99999999999.99|1",
			"123456789012.34|1", "123456789012.35|1", "NULL|1"},
		byG: []string{"a|5.00|1", "a|99999999999.98|1", "a|99999999999.99|1", "a|123456789012.34|1", "a|123456789012.35|1",
			"b|-123456789012.35|1", "b|-123456789012.34|1", "b|5.00|1", "b|NULL|1"},
		minMax: []string{"a|5.00|123456789012.35|5", "b|-123456789012.35|5.00|3"},
	},
	"d": {
		groups: []string{"0.3|1", "0.30000000000000004|1", "1.5|2", "5|1", "5.0000000000001|1", "1e+300|2", "NULL|1"},
		byG: []string{"a|0.3|1", "a|0.30000000000000004|1", "a|1.5|1", "a|1e+300|2", "b|1.5|1", "b|5|1", "b|5.0000000000001|1",
			"b|NULL|1"},
		minMax: []string{"a|0.3|1e+300|5", "b|1.5|5.0000000000001|3"},
	},
}

// TestSQLGroupByRoundedValues checks GROUP BY, DISTINCT, ROLLUP, HAVING,
// MIN, MAX and SUM on it_rgroups in every mode, and which run in the
// index in the all mode.
func TestSQLGroupByRoundedValues(t *testing.T) {
	hs := pushdownHarnesses(t)
	h := hs["default"]
	h.setupRoundedGroups()
	forModes(t, hs, func(mode string, mh *sqlHarness) {
		for _, c := range roundedColumns {
			w := roundedWants[c]
			mh.roundedAgree(c, "SELECT {c}, COUNT(*) FROM it_rgroups GROUP BY {c} ORDER BY {c}", w.groups...)
			mh.roundedAgree(c, "SELECT g, {c}, COUNT(*) FROM it_rgroups GROUP BY g, {c} ORDER BY g, {c}", w.byG...)
			var distinct, rollup, having []string
			for _, r := range w.groups {
				key, count, _ := strings.Cut(r, "|")
				distinct = append(distinct, key)
				rollup = append(rollup, r+"|0")
				if count != "1" {
					having = append(having, r)
				}
			}
			mh.roundedAgree(c, "SELECT DISTINCT {c} FROM it_rgroups ORDER BY {c}", distinct...)
			mh.roundedAgree(c, "SELECT {c}, COUNT(*), GROUPING({c}) FROM it_rgroups GROUP BY ROLLUP ({c}) ORDER BY GROUPING({c}), {c}",
				append(rollup, "NULL|9|1")...)
			mh.roundedAgree(c, "SELECT {c}, COUNT(*) FROM it_rgroups GROUP BY {c} HAVING COUNT(*) > 1 ORDER BY {c}", having...)
			// The index's MIN and MAX of a decimal or a double have 12
			// digits in the all mode.
			if mode != PushdownAll || c == "b" || c == "ts" {
				mh.roundedAgree(c, "SELECT g, MIN({c}), MAX({c}), COUNT({c}) FROM it_rgroups GROUP BY g ORDER BY g", w.minMax...)
			}
		}
		mh.roundedAgree("b", "SELECT g, SUM({c}) FROM it_rgroups WHERE id BETWEEN 1 AND 5 GROUP BY g ORDER BY g",
			"a|18014398509481990", "b|-18014398509481985")
		mh.roundedAgree("b", "SELECT g, MIN({c}), MAX({c}), SUM({c}) FROM it_rgroups WHERE b BETWEEN 0 AND 1000 GROUP BY g ORDER BY g",
			"a|5|5|5", "b|5|5|5")
		// Below 2^53, but its doubles add up to -9007199254740987.
		mh.roundedAgree("b", "SELECT SUM({c}) FROM it_rgroups WHERE id BETWEEN 2 AND 5", "-9007199254740988")
	})

	// In the all mode, the index groups and reduces values it replies
	// exactly; the driver does again when a reply isn't. (IN lists are
	// re-checked, so these WHERE clauses are ranges, which aren't.)
	pushed := func(want bool, sqls ...string) {
		t.Helper()
		for _, sql := range sqls {
			if got := h.indexAggIn(PushdownAll, sql); got != want {
				t.Errorf("%s: in the index %v, want %v", sql, got, want)
			}
		}
	}
	pushed(true,
		"SELECT b, ts, COUNT(*) FROM it_rgroups WHERE b BETWEEN 0 AND 1000 GROUP BY b, ts",
		"SELECT n, COUNT(*) FROM it_rgroups WHERE id BETWEEN 7 AND 9 GROUP BY n",
		"SELECT q, COUNT(*) FROM it_rgroups WHERE b BETWEEN 0 AND 1000 GROUP BY q",
		"SELECT g, MIN(b), MAX(b), SUM(b), MIN(ts), MAX(ts) FROM it_rgroups WHERE b BETWEEN 0 AND 1000 GROUP BY g",
		// Approximate, as documented.
		"SELECT MIN(n), MAX(n), MIN(m), MAX(m), MIN(d), MAX(d), SUM(d) FROM it_rgroups",
		"SELECT COUNT(b), COUNT(d) FROM it_rgroups")
	for _, c := range roundedColumns {
		pushed(false, "SELECT "+c+", COUNT(*) FROM it_rgroups GROUP BY "+c)
	}
	pushed(false,
		"SELECT b, COUNT(*) FROM it_rgroups WHERE id BETWEEN 2 AND 5 GROUP BY b",
		"SELECT MIN(b) FROM it_rgroups", "SELECT MAX(b) FROM it_rgroups WHERE id >= 5",
		"SELECT MIN(ts) FROM it_rgroups WHERE id BETWEEN 4 AND 5", "SELECT MAX(ts) FROM it_rgroups",
		"SELECT SUM(b) FROM it_rgroups WHERE id BETWEEN 1 AND 2", "SELECT SUM(b) FROM it_rgroups WHERE id BETWEEN 2 AND 5",
		// 0.5 may be 0.50000000000000001 in a NUMERIC(20,17).
		"SELECT m, COUNT(*) FROM it_rgroups WHERE b BETWEEN 0 AND 1000 GROUP BY m",
		// Never for a double.
		"SELECT d, COUNT(*) FROM it_rgroups WHERE b BETWEEN 0 AND 1000 GROUP BY d")
}

// TestSQLGroupByNanoseconds groups TIMESTAMP(9) values 1 ns apart, which
// are one double in 2024. The all mode doesn't group or reduce them in the
// index at all. Postgres has no TIMESTAMP(9).
func TestSQLGroupByNanoseconds(t *testing.T) {
	hs := pushdownHarnesses(t)
	h := hs["default"]
	h.dropTables("it_rgroups_ns")
	h.exec("CREATE TABLE it_rgroups_ns (id INTEGER, t TIMESTAMP(9), tn TIMESTAMP(9) NOINDEX)")
	h.exec(`INSERT INTO it_rgroups_ns VALUES (1, '2024-01-01 00:00:00.000000002', '2024-01-01 00:00:00.000000002'),
		(2, '2024-01-01 00:00:00.000000001', '2024-01-01 00:00:00.000000001'), (3, '2024-01-01 00:00:00.000000003', '2024-01-01 00:00:00.000000003'),
		(4, '2024-01-01 00:00:00.000000001', '2024-01-01 00:00:00.000000001'), (5, NULL, NULL)`)
	forModes(t, hs, func(_ string, mh *sqlHarness) {
		mh.roundedAgree("t", "SELECT {c}, COUNT(*) FROM it_rgroups_ns GROUP BY {c} ORDER BY {c}",
			"2024-01-01T00:00:00.000000001|2", "2024-01-01T00:00:00.000000002|1", "2024-01-01T00:00:00.000000003|1", "NULL|1")
		mh.roundedAgree("t", "SELECT DISTINCT {c} FROM it_rgroups_ns WHERE id < 4 ORDER BY {c} DESC",
			"2024-01-01T00:00:00.000000003", "2024-01-01T00:00:00.000000002", "2024-01-01T00:00:00.000000001")
		mh.roundedAgree("t", "SELECT MIN({c}), MAX({c}), COUNT({c}) FROM it_rgroups_ns",
			"2024-01-01T00:00:00.000000001|2024-01-01T00:00:00.000000003|4")
	})
	for _, sql := range []string{"SELECT t, COUNT(*) FROM it_rgroups_ns GROUP BY t", "SELECT MIN(t), MAX(t) FROM it_rgroups_ns"} {
		if h.indexAggIn(PushdownAll, sql) {
			t.Errorf("%s: in the index", sql)
		}
	}
	if !h.indexAggIn(PushdownAll, "SELECT COUNT(t) FROM it_rgroups_ns") {
		t.Error("COUNT(t): not in the index")
	}
}
