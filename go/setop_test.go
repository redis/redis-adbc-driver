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

// Unit tests (no Redis) for the keys of rows and values (rowKey, writeKey)
// that set operations, SELECT DISTINCT, GROUP BY, DISTINCT aggregates and
// window partitions find equal values by.

import (
	"strings"
	"testing"
)

// Values of one kind have the same key exactly when they compare equal, as
// in Postgres: '1 mon' = '30 days', '1 day' = '24 hours', 0.0 = -0.0, 1.5 =
// 1.50, NaN = NaN, 'a' = 'a  ' as CHAR(3), and one instant in any unit.
func TestRowKeyMatchesCompareValues(t *testing.T) {
	char3 := func(s string) Value { return Value{T: ColType{Kind: KindString, Length: 3, Fixed: true}, S: s} }
	iv := func(months, days int32, ns int64) Value {
		return Value{T: typeInterval, Months: months, Days: days, I: ns}
	}
	values := append(equalityValues(), char3("a  "), char3("a"), char3("ab "), char3(" a "),
		iv(1, -30, 0), iv(0, 0, 0), iv(-1, 0, 0), iv(0, -30, 0), iv(0, 1, 2*3600e9), iv(0, 0, 26*3600e9),
		iv(12, 0, 0), iv(0, 360, 0), iv(0, 0, -1))
	for _, a := range values {
		for _, b := range values {
			if a.T.Kind != b.T.Kind || a.T.isChar() != b.T.isChar() {
				continue // one expression's values have one type
			}
			c, ok := compareValues(a, b)
			if !ok {
				t.Errorf("%s (%s) and %s (%s) fail to compare", a.Text(), a.T.Kind, b.Text(), b.T.Kind)
				continue
			}
			ka, kb := rowKey([]Value{a}), rowKey([]Value{b})
			if (c == 0) != (ka == kb) {
				t.Errorf("%s (%s) vs %s (%s): compare %d, keys %q %q", a.Text(), a.T.Kind, b.Text(), b.T.Kind, c, ka, kb)
			}
		}
	}
}

// Rows have the same key only when each of their values is equal to the
// other row's, however the values' bytes look: NULLs, empty strings, and
// strings holding NULs or what looks like another value's key.
func TestRowKeyParts(t *testing.T) {
	values := []Value{nullValue(typeString), stringValue(""), stringValue("a"), stringValue("N"),
		stringValue("a\x00"), stringValue("\x00a"), stringValue("a\x00Vsb"), stringValue("b\x00Vsc"),
		stringValue("b"), stringValue("c"), stringValue("Vsc"), stringValue("1:sa"), stringValue("a2:sb"),
		binaryValue("a\x00"), binaryValue("\x00")}
	same := func(a, b Value) bool {
		if a.Null || b.Null {
			return a.Null == b.Null
		}
		return a.S == b.S
	}
	for _, a1 := range values {
		for _, a2 := range values {
			for _, b1 := range values {
				for _, b2 := range values {
					ka, kb := rowKey([]Value{a1, a2}), rowKey([]Value{b1, b2})
					if want := same(a1, b1) && same(a2, b2); (ka == kb) != want {
						t.Errorf("(%q, %q) vs (%q, %q): keys %q %q", a1.S, a2.S, b1.S, b2.S, ka, kb)
					}
				}
			}
		}
	}
	// GROUP BY and DISTINCT aggregates write their keys a value at a time.
	var b strings.Builder
	writeKey(&b, Value{T: typeInterval, Months: 1})
	writeKey(&b, nullValue(typeInterval))
	if k := rowKey([]Value{{T: typeInterval, Days: 30}, nullValue(typeString)}); b.String() != k {
		t.Errorf("writeKey %q, rowKey %q", b.String(), k)
	}
}

// GROUP BY, SELECT DISTINCT (which runs as GROUP BY), grouping sets, DISTINCT
// aggregates and window partitions find values that compare equal to be the
// same, and the first of them represents them. The expected rows are
// Postgres 16's. (The casts have aliases: Postgres sorts ORDER BY iv by an
// output column named iv, which CAST(iv AS VARCHAR) is.)
func TestEqualValuesGroupTogether(t *testing.T) {
	ivs := `(SELECT INTERVAL '1 mon' AS iv UNION ALL SELECT INTERVAL '1 day' UNION ALL SELECT INTERVAL '30 days'
		UNION ALL SELECT INTERVAL '26 hours' UNION ALL SELECT INTERVAL '24 hours' UNION ALL SELECT NULL) v`
	pairs := `(SELECT 1 AS g, INTERVAL '1 mon' AS iv UNION ALL SELECT 1, INTERVAL '30 days'
		UNION ALL SELECT 2, INTERVAL '30 days' UNION ALL SELECT 2, INTERVAL '720 hours') v`
	zeros := `(SELECT CAST('-0' AS DOUBLE PRECISION) AS x UNION ALL SELECT CAST('0' AS DOUBLE PRECISION)) v`
	ints, mixed := `(SELECT 1 AS id, 1 AS g UNION ALL SELECT 2, 1) v`, `COALESCE(CASE WHEN id = 2 THEN g END, '1')`
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{`SELECT CAST(iv AS VARCHAR) AS s, COUNT(*) FROM ` + ivs + ` GROUP BY iv ORDER BY iv`,
			[]string{"1 day|2", "26:00:00|1", "1 mon|2", "NULL|1"}},
		{`SELECT COUNT(DISTINCT iv), CAST(SUM(DISTINCT iv) AS VARCHAR), CAST(AVG(DISTINCT iv) AS VARCHAR), COUNT(iv) FROM ` + ivs,
			[]string{"3|1 mon 1 day 26:00:00|10 days 16:40:00|5"}},
		{`SELECT CAST(iv AS VARCHAR) AS s FROM (SELECT DISTINCT iv FROM ` + ivs + `) s ORDER BY iv`,
			[]string{"1 day", "26:00:00", "1 mon", "NULL"}},
		{`SELECT g, CAST(iv AS VARCHAR), COUNT(*) FROM ` + pairs + ` GROUP BY ROLLUP (g, iv) ORDER BY g, iv`,
			[]string{"1|1 mon|2", "1|NULL|2", "2|30 days|2", "2|NULL|2", "NULL|NULL|4"}},
		{`SELECT g, COUNT(*) OVER (PARTITION BY iv), COUNT(*) OVER (PARTITION BY g, iv) FROM ` + pairs + ` ORDER BY g`,
			[]string{"1|4|2", "1|4|2", "2|4|2", "2|4|2"}},
		{`SELECT CAST(x AS VARCHAR), COUNT(*), COUNT(DISTINCT x), COUNT(DISTINCT -x) FROM ` + zeros + ` GROUP BY x`,
			[]string{"-0|2|1|1"}},
		{`SELECT COUNT(*) FROM (SELECT DISTINCT x FROM ` + zeros + `) s`, []string{"1"}},
		// The '1' of an integer expression is the integer 1.
		{`SELECT ` + mixed + `, COUNT(*), COUNT(DISTINCT ` + mixed + `) FROM ` + ints + ` GROUP BY 1`,
			[]string{"1|2|1"}},
		{`SELECT g, COUNT(*) OVER (PARTITION BY ` + mixed + `) FROM ` + ints, []string{"1|2", "1|2"}},
	} {
		stmts, err := ParseScript(c.sql)
		if err != nil {
			t.Fatalf("%s: %v", c.sql, err)
		}
		rows, err := runInMemory(stmts[0].Stmt.(*SelectStmt))
		if err != nil {
			t.Errorf("%s: %v", c.sql, err)
			continue
		}
		var got []string
		for _, r := range rows {
			cells := make([]string, len(r))
			for i, v := range r {
				if cells[i] = v.Text(); v.Null {
					cells[i] = "NULL"
				}
			}
			got = append(got, strings.Join(cells, "|"))
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s\n got: %q\nwant: %q", c.sql, got, c.want)
		}
	}
}
