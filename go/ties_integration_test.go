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

// Integration tests for ORDER BY on values the index rounds to the same
// double (see ties.go). They run against the Redis server at REDIS_URI,
// standalone or a cluster, like sql_integration_test.go. The expected rows
// are Postgres 16's (with NULLS LAST for DESC, the driver's default). Each
// query runs on an indexed column, which the index sorts, and on a NOINDEX
// copy, which the driver sorts itself.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// tieColumns are it_ties' indexed columns whose values the index rounds,
// each with a NOINDEX copy named <c>n.
var tieColumns = []string{"b", "ts", "n", "m"}

// setupTieTable creates it_ties. Its values tie in the index in pairs, of
// rows with the same g, and each pair is inserted larger first, so the
// index's order (insertion order within a tie) is the wrong one ascending
// (and descending for the negative pairs).
//
//   - b BIGINT: 2^53 + 1 and 2^53, -2^53 and -2^53 - 1, 2^63 - 1 and
//     2^63 - 2 (2^63 as doubles); 2^53 + 2 is a double of its own.
//   - ts TIMESTAMP (microseconds): 9999-12-31 23:59:59.999999 and .999998
//     (doubles 32 µs apart there), 1000-01-01 00:00:00.000001 and 00:00:00
//     (4 µs apart), and 2^53 + 1 and 2^53 µs, in 2255.
//   - n NUMERIC (38, 9 digits): 10000000.000000002 and .000000001; 1e6 +
//     1e-9 and 999999.999999999 don't tie (doubles are 1.2e-10 apart).
//   - m NUMERIC(20,17): 0.10000000000000001 and 0.1, -0.1 and
//     -0.10000000000000001.
func (h *sqlHarness) setupTieTable() {
	h.t.Helper()
	h.dropTables("it_ties")
	h.exec(`CREATE TABLE it_ties (id INTEGER, g VARCHAR, b BIGINT, bn BIGINT NOINDEX, ts TIMESTAMP, tsn TIMESTAMP NOINDEX,
		n NUMERIC, nn NUMERIC NOINDEX, m NUMERIC(20,17), mn NUMERIC(20,17) NOINDEX)`)
	rows := [][]string{
		{"1", "'a'", "9007199254740993", "TIMESTAMP '9999-12-31 23:59:59.999999'", "10000000.000000002", "0.10000000000000001"},
		{"2", "'a'", "9007199254740992", "TIMESTAMP '9999-12-31 23:59:59.999998'", "10000000.000000001", "0.10000000000000000"},
		{"3", "'b'", "-9007199254740992", "TIMESTAMP '1000-01-01 00:00:00.000001'", "-10000000.000000001", "-0.10000000000000000"},
		{"4", "'b'", "-9007199254740993", "TIMESTAMP '1000-01-01 00:00:00'", "-10000000.000000002", "-0.10000000000000001"},
		{"5", "'a'", "5", "TIMESTAMP '2024-06-01 00:00:00'", "5", "0.5"},
		{"6", "'b'", "NULL", "NULL", "NULL", "NULL"},
		{"7", "'a'", "9223372036854775807", "TIMESTAMP '2255-06-05 23:47:34.740993'", "1000000.000000001", "0.00999999999999999"},
		{"8", "'a'", "9223372036854775806", "TIMESTAMP '2255-06-05 23:47:34.740992'", "999999.999999999", "0.01"},
		{"9", "'b'", "9007199254740994", "TIMESTAMP '2255-06-05 23:47:34.740994'", "10000000.000000004", "0.10000000000000003"},
	}
	var values []string
	for _, r := range rows {
		vals := []string{r[0], r[1]}
		for _, v := range r[2:] {
			vals = append(vals, v, v)
		}
		values = append(values, "("+strings.Join(vals, ", ")+")")
	}
	h.exec("INSERT INTO it_ties VALUES " + strings.Join(values, ", "))
}

// tieAgree runs sql with {c} replaced by c and by its NOINDEX copy, and
// checks that both give the ids in want (comma-separated).
func (h *sqlHarness) tieAgree(c, sql, want string) {
	h.t.Helper()
	var rows []string
	if want != "" {
		rows = strings.Split(want, ",")
	}
	for _, col := range []string{c, c + "n"} {
		h.expectRows(strings.ReplaceAll(sql, "{c}", col), rows...)
	}
}

// tieWork is the work of scans with a tieSort that f does: runs sorted,
// LIMIT and OFFSET read with more rows, and runs read by value.
type tieWork struct{ sorted, widened, runsRead int64 }

func tieWorkOf(f func()) tieWork {
	s, w, r := tieStats.sorted.Load(), tieStats.widened.Load(), tieStats.runsRead.Load()
	f()
	return tieWork{tieStats.sorted.Load() - s, tieStats.widened.Load() - w, tieStats.runsRead.Load() - r}
}

func TestSQLOrderByRoundedValues(t *testing.T) {
	h := newSQLHarness(t)
	h.setupTieTable()

	// By column: ORDER BY c, ORDER BY c DESC, WHERE g = 'a' (an index
	// query) ORDER BY c, ORDER BY g, c and ORDER BY g DESC, c DESC.
	want := map[string][5]string{
		"b":  {"4,3,5,2,1,9,8,7,6", "7,8,9,1,2,5,3,4,6", "5,2,1,8,7", "5,2,1,8,7,4,3,9,6", "9,3,4,6,7,8,1,2,5"},
		"ts": {"4,3,5,8,7,9,2,1,6", "1,2,9,7,8,5,3,4,6", "5,8,7,2,1", "5,8,7,2,1,4,3,9,6", "9,3,4,6,1,2,7,8,5"},
		"n":  {"4,3,5,8,7,2,1,9,6", "9,1,2,7,8,5,3,4,6", "5,8,7,2,1", "5,8,7,2,1,4,3,9,6", "9,3,4,6,1,2,7,8,5"},
		"m":  {"4,3,7,8,2,1,9,5,6", "5,9,1,2,8,7,3,4,6", "7,8,2,1,5", "7,8,2,1,5,4,3,9,6", "9,3,4,6,5,1,2,8,7"},
	}
	for _, c := range tieColumns {
		w := want[c]
		h.tieAgree(c, "SELECT id FROM it_ties ORDER BY {c}", w[0])
		h.tieAgree(c, "SELECT id FROM it_ties ORDER BY {c} DESC", w[1])
		h.tieAgree(c, "SELECT id FROM it_ties WHERE g = 'a' ORDER BY {c}", w[2])
		h.tieAgree(c, "SELECT id FROM it_ties ORDER BY g, {c}", w[3])
		h.tieAgree(c, "SELECT id FROM it_ties ORDER BY g DESC, {c} DESC", w[4])
		// The column read for the WHERE clause, rather than by HMGET for the
		// sort.
		h.tieAgree(c, "SELECT id FROM it_ties WHERE {c} IS NOT NULL OR id = 6 ORDER BY {c}", w[0])
		h.tieAgree(c, "SELECT id FROM it_ties ORDER BY {c}, id", w[0])

		// Every LIMIT and OFFSET: the rows of the full order.
		order := strings.Split(w[0], ",")
		for i, by := range []string{"{c}", "{c} DESC", "g, {c}", "g DESC, {c} DESC"} {
			all := strings.Split(w[[]int{0, 1, 3, 4}[i]], ",")
			for off := 0; off <= len(all); off++ {
				for lim := 1; lim <= 3; lim++ {
					end := min(off+lim, len(all))
					h.tieAgree(c, fmt.Sprintf("SELECT id FROM it_ties ORDER BY %s LIMIT %d OFFSET %d", by, lim, off),
						strings.Join(all[min(off, end):end], ","))
				}
			}
		}
		// FT.AGGREGATE takes a LIMIT of up to 2^31 (on a cluster, offset and
		// count together), the extra rows included.
		h.tieAgree(c, "SELECT id FROM it_ties ORDER BY {c} LIMIT 2147483647 OFFSET 2", strings.Join(order[2:], ","))
		// LIMIT with part of the WHERE clause re-checked: the driver limits.
		var rest []string
		for _, id := range order {
			if id != "1" {
				rest = append(rest, id)
			}
		}
		h.tieAgree(c, "SELECT id FROM it_ties WHERE id + 0 > 1 ORDER BY {c} LIMIT 3", strings.Join(rest[:3], ","))
		h.tieAgree(c, "SELECT id FROM it_ties WHERE id + 0 > 1 ORDER BY {c} LIMIT 3 OFFSET 2", strings.Join(rest[2:5], ","))
	}

	// The driver sorts the three runs that tie, and reads more rows only
	// when one goes on past LIMIT and OFFSET.
	for _, c := range []struct {
		sql  string
		want string
		work tieWork
	}{
		{"SELECT id FROM it_ties ORDER BY b", want["b"][0], tieWork{3, 0, 0}},
		// LIMIT 1 would pick -2^53, the first of -2^53 and -2^53 - 1 in the
		// index's order (OFFSET 3 would pick 2^53 + 1, and DESC OFFSET 7
		// -2^53), so the scan reads that run whole, by value, and sorts it.
		{"SELECT id FROM it_ties ORDER BY b LIMIT 1", "4", tieWork{1, 0, 1}},
		{"SELECT id FROM it_ties ORDER BY b LIMIT 1 OFFSET 3", "2", tieWork{1, 0, 1}},
		{"SELECT id FROM it_ties ORDER BY b DESC LIMIT 1 OFFSET 7", "4", tieWork{1, 0, 1}},
		// NUMERIC values, and keys after one that doesn't round, are read
		// with more rows on that side instead.
		{"SELECT id FROM it_ties ORDER BY n LIMIT 1", "4", tieWork{1, 1, 0}},
		{"SELECT id FROM it_ties ORDER BY g, b LIMIT 1 OFFSET 1", "2", tieWork{1, 1, 0}},
		// Runs that end within LIMIT and OFFSET are sorted there.
		{"SELECT id FROM it_ties ORDER BY b LIMIT 3", "4,3,5", tieWork{1, 0, 0}},
		{"SELECT id FROM it_ties ORDER BY b LIMIT 2 OFFSET 3", "2,1", tieWork{1, 0, 0}},
		{"SELECT id FROM it_ties ORDER BY b LIMIT 1 OFFSET 2", "5", tieWork{}},
		// Without values from 2^53 on, nothing is sorted again.
		{"SELECT id FROM it_ties WHERE b BETWEEN -9007199254740991 AND 9007199254740991 ORDER BY b LIMIT 1", "5", tieWork{}},
	} {
		if work := tieWorkOf(func() { h.expectRows(c.sql, strings.Split(c.want, ",")...) }); work != c.work {
			t.Errorf("%s: %+v, want %+v", c.sql, work, c.work)
		}
	}

	// Nanosecond timestamps are all from 2^53 on, but those 256 ns or more
	// apart don't tie: they are sorted by the index alone.
	h.dropTables("it_ties_ns")
	h.exec("CREATE TABLE it_ties_ns (id INTEGER, ts TIMESTAMP(9))")
	var values []string
	for i := 1; i <= 20; i++ {
		values = append(values, fmt.Sprintf("(%d, TIMESTAMP '2026-10-05 12:00:00' - INTERVAL '%d microseconds')", i, i))
	}
	h.exec("INSERT INTO it_ties_ns VALUES " + strings.Join(values, ", "))
	if work := tieWorkOf(func() {
		h.expectRows("SELECT id FROM it_ties_ns ORDER BY ts LIMIT 3 OFFSET 5", "15", "14", "13")
		h.expectRows("SELECT id FROM it_ties_ns ORDER BY ts DESC LIMIT 2", "1", "2")
	}); work != (tieWork{}) {
		t.Errorf("TIMESTAMP(9) that don't tie: %+v, want none", work)
	}
	// 1 ns apart, they do.
	h.exec("INSERT INTO it_ties_ns VALUES (21, TIMESTAMP '2026-10-05 12:00:00.000000002'), (22, TIMESTAMP '2026-10-05 12:00:00.000000001')")
	h.expectRows("SELECT id FROM it_ties_ns ORDER BY ts DESC LIMIT 2", "21", "22")
	h.expectRows("SELECT id FROM it_ties_ns WHERE ts > TIMESTAMP '2026-10-05 12:00:00' ORDER BY ts", "22", "21")
}

// A run of rows that tie in the index can span cursor pages and stream
// batches.
func TestSQLOrderByRoundedRuns(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_ties_runs")
	h.exec("CREATE TABLE it_ties_runs (k BIGINT, b BIGINT, s VARCHAR)")
	// b is 2^62 + k for k from 0 to 1499, in a shuffled order. At 2^62 the
	// doubles are 1024 apart, so k up to 512 are one run of 2^62 and the
	// others another, of 2^62 + 1024: longer than a cursor page.
	const n = 1500
	var values []string
	for i := range n {
		k := i * 7919 % n
		values = append(values, fmt.Sprintf("(%d, %d, 'r%d')", k, 1<<62+k, k))
		if len(values) == 500 {
			h.exec("INSERT INTO it_ties_runs VALUES " + strings.Join(values, ", "))
			values = nil
		}
	}
	ks := func(from, to int) []string {
		var out []string
		for k := from; k != to; {
			out = append(out, strconv.Itoa(k))
			if from < to {
				k++
			} else {
				k--
			}
		}
		return out
	}

	if work := tieWorkOf(func() {
		h.expectRows("SELECT k FROM it_ties_runs ORDER BY b", ks(0, n)...)
		h.expectRows("SELECT k, b - 4611686018427387904 FROM it_ties_runs ORDER BY b DESC", func() []string {
			var out []string
			for _, k := range ks(n-1, -1) {
				out = append(out, k+"|"+k)
			}
			return out
		}()...)
	}); work != (tieWork{4, 0, 0}) {
		t.Errorf("ORDER BY b: %+v, want 4 runs sorted", work)
	}
	// LIMIT and OFFSET within the second run, and across both.
	if work := tieWorkOf(func() {
		h.expectRows("SELECT k FROM it_ties_runs ORDER BY b LIMIT 5 OFFSET 600", ks(600, 605)...)
		h.expectRows("SELECT k FROM it_ties_runs ORDER BY b DESC LIMIT 3 OFFSET 986", "513", "512", "511")
	}); work != (tieWork{3, 0, 3}) {
		t.Errorf("LIMIT within runs: %+v, want 3 runs read and sorted", work)
	}
	// After a key that doesn't round, the scan asks for more rows on each
	// side (twice as many, and at least 256 more) until the run ends within
	// them: three times here.
	h.dropTables("it_ties_runs_g")
	h.exec("CREATE TABLE it_ties_runs_g AS SELECT 'x' AS g, k, b FROM it_ties_runs")
	if work := tieWorkOf(func() {
		h.expectRows("SELECT k FROM it_ties_runs_g ORDER BY g, b LIMIT 5 OFFSET 600", ks(600, 605)...)
	}); work != (tieWork{1, 3, 0}) {
		t.Errorf("ORDER BY g, b LIMIT 5 OFFSET 600: %+v, want 1 run sorted after reading wider 3 times", work)
	}
	h.expectRows("SELECT k FROM it_ties_runs WHERE s <> 'x' ORDER BY b LIMIT 4 OFFSET 1100", ks(1100, 1104)...)

	// Streamed, 100 rows a batch. With LIMIT, the second run goes on to
	// the last row, so all of them are read before the first is returned.
	for _, c := range []struct {
		sql      string
		from, to int
		streamed bool
	}{
		{"SELECT k FROM it_ties_runs ORDER BY b", 0, n, true},
		{"SELECT k FROM it_ties_runs ORDER BY b DESC", n - 1, -1, true},
		{"SELECT k FROM it_ties_runs ORDER BY b LIMIT 300 OFFSET 450", 450, 750, false},
		{"SELECT k FROM it_ties_runs WHERE s <> 'x' ORDER BY b LIMIT 300 OFFSET 450", 450, 750, false},
	} {
		rdr, count := h.openStream(h.ctx, h.conn, c.sql, nil, map[string]string{OptionIntStreamBatchRows: "100"})
		got, batches, err := readInts(rdr)
		rdr.Release()
		if err != nil {
			t.Fatalf("%s: %v", c.sql, err)
		}
		var gotS []string
		for _, k := range got {
			gotS = append(gotS, strconv.FormatInt(k, 10))
		}
		if want := ks(c.from, c.to); !slices.Equal(gotS, want) {
			t.Errorf("%s: %d rows, %v…, want %d from %d", c.sql, len(got), gotS[:min(5, len(gotS))], len(want), c.from)
		}
		if c.streamed && (count != -1 || len(batches) != n/100) {
			t.Errorf("%s: row count %d, %d batches; want a stream of %d", c.sql, count, len(batches), n/100)
		}
	}
}

// An adopted table, whose HASHes the application writes: its rows are
// identified by their keys and read whole (see keyrows.go).
func TestSQLOrderByRoundedAdopted(t *testing.T) {
	f := newHashFixture(t, "it:ties:ev:")
	f.dropAdopted("it_ties_ev")
	t.Cleanup(func() { f.dropAdopted("it_ties_ev") })
	// Snowflake-like ids 2^60 + 7k, about 36 to a double, written in a
	// shuffled order.
	const n = 300
	for i := range n {
		k := i * 7 % n
		f.hset(fmt.Sprintf("it:ties:ev:%d", k), "id", strconv.Itoa(1<<60+7*k), "name", fmt.Sprintf("e%d", k))
	}
	if res := f.adopt(HashAdoptOptions{Table: "it_ties_ev", Apply: true}); !res.Applied {
		t.Fatalf("adopt: %+v", res)
	}
	h := f.h
	h.expectRows("SELECT data_type FROM information_schema.columns WHERE table_name = 'it_ties_ev' AND column_name = 'id'", "BIGINT")
	names := func(from, to int) []string {
		var out []string
		for k := from; k != to; {
			out = append(out, fmt.Sprintf("e%d", k))
			if from < to {
				k++
			} else {
				k--
			}
		}
		return out
	}
	h.expectRows("SELECT name FROM it_ties_ev ORDER BY id", names(0, n)...)
	h.expectRows("SELECT name FROM it_ties_ev ORDER BY id DESC", names(n-1, -1)...)
	if work := tieWorkOf(func() {
		h.expectRows("SELECT name FROM it_ties_ev ORDER BY id LIMIT 3 OFFSET 100", names(100, 103)...)
		h.expectRows("SELECT id - 1152921504606846976, name FROM it_ties_ev ORDER BY id DESC LIMIT 2", "2093|e299", "2086|e298")
	}); work.runsRead == 0 || work.widened != 0 {
		t.Errorf("%+v, want runs read by value", work)
	}
	h.expectRows("SELECT name FROM it_ties_ev WHERE name <> 'x' ORDER BY id LIMIT 3 OFFSET 200", names(200, 203)...)
}
