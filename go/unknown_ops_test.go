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

// Unit tests for quoted string literals that are operands of arithmetic
// operators (unknown.go). The types and errors are those of Postgres 16, but
// for the driver's BIGINT integer literals and results, its type names
// (varchar, timestamp) and its own messages for text that isn't a value of
// the literal's type (only their SQLSTATEs and the text are checked).

import (
	"strings"
	"testing"
)

// opTestCols are the columns the expressions of these tests read.
var opTestCols = map[string]ColType{"i": typeInt32, "i2": {Kind: KindInt16}, "f": typeFloat64,
	"n": decimalType(10, 2), "s": {Kind: KindString, Length: 10}, "b": typeBool, "d": typeDate,
	"tm": {Kind: KindTime, Unit: typeTimestamp.Unit}, "tt": {Kind: KindTimeTZ, Unit: typeTimestamp.Unit},
	"ts": typeTimestamp, "tz": typeTimestampTZ, "iv": typeInterval}

// A quoted literal next to a value of type T is a T where the operator
// takes two, and otherwise the type of the one form the operator has for
// T, as in Postgres 16.
func TestUnknownOperandTypes(t *testing.T) {
	for _, c := range []struct{ expr, typ string }{
		{"'1' + 1", "INTEGER"},
		{"i + '1'", "INTEGER"},
		{"'1' * i", "INTEGER"},
		{"i2 - '1'", "SMALLINT"},
		{"f / '2'", "DOUBLE PRECISION"},
		// Contextual quoted numerics carry a conservative precision-38
		// bound. Reserve its possible integer/carry digits instead of
		// truncating arithmetic metadata back to Decimal128.
		{"'1.5' + 1.25", "NUMERIC(40,2)"},
		// A NUMERIC literal has its own digits (Postgres's has no scale).
		{"n + '0.005'", "NUMERIC(39,3)"},
		{"n * '2'", "NUMERIC(49,2)"},
		{"n - '1'", "NUMERIC(41,2)"},
		{"'1e2' + 1.5", "NUMERIC(40,1)"},
		{"n / '4'", "DOUBLE PRECISION"},
		{"d - '2024-01-01'", "BIGINT"}, // days
		{"'2024-03-01' - d", "BIGINT"},
		{"ts + '1 day'", "TIMESTAMP(6)"},
		{"'1 day' + ts", "TIMESTAMP(6)"},
		{"ts - '2024-01-01'", "INTERVAL"},
		{"tz + '1 hour'", typeTimestampTZ.SQLName()},
		{"tz - '2024-01-01 00:00'", "INTERVAL"},
		{"tm + '01:00'", "TIME(6)"},
		{"tm - '01:00'", "INTERVAL"},
		{"tt - '1 hour'", "TIME(6) WITH TIME ZONE"},
		{"iv + '1 hour'", "INTERVAL"},
		{"'3 days' - iv", "INTERVAL"},
		{"iv * '2'", "INTERVAL"},
		{"'2' * iv", "INTERVAL"},
		{"iv / '2'", "INTERVAL"},
		{"(i + '1') * '2'", "INTEGER"},
		{"'2' * (i + '1')", "INTEGER"},
		{"i + '1' + '2'", "INTEGER"},
		{"CASE WHEN i = 1 THEN '1' ELSE i END + '1'", "INTEGER"},
		{"COALESCE(n, '0') + '1'", "NUMERIC(41,2)"},
		{"i || '1'", "VARCHAR"},
		{"'1' || '1'", "VARCHAR"},
		// % is MOD, which keeps its arguments' type.
		{"'7' % 2", "INTEGER"},
		{"i % '2'", "INTEGER"},
		{"MOD('7', i2)", "SMALLINT"},
		{"n % '1'", "NUMERIC(10,2)"},
		{"f % '1.5'", "DOUBLE PRECISION"}, // the driver's MOD of doubles
	} {
		got, err := inferType(parseTestExpr(t, c.expr), opTestCols, nil)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if got.SQLName() != c.typ {
			t.Errorf("%s: %s, want %s", c.expr, got.SQLName(), c.typ)
		}
	}
}

// An operator with no form for the other operand's type, or more than one,
// is an error, as are two literals and a minus sign before one, and so is a
// literal that isn't a value of its type. All are reported when the
// expression is bound (checkUnknowns).
func TestUnknownOperandErrors(t *testing.T) {
	cols := func() map[string]ColType { return opTestCols }
	const exist, unique = stateUndefinedFunction, stateAmbiguousFunction
	for _, c := range []struct{ expr, state, err string }{
		{"'1' + '1'", unique, "operator is not unique: unknown + unknown"},
		{"NULL * '1'", unique, "operator is not unique: unknown * unknown"},
		{"'1' - NULL", unique, "operator is not unique: unknown - unknown"},
		{"-'1'", unique, "operator is not unique: - unknown"},
		{"d + '1 day'", unique, "operator is not unique: date + unknown"},
		{"'1' + d", unique, "operator is not unique: unknown + date"},
		{"tt + '1 hour'", unique, "operator is not unique: time with time zone + unknown"},
		{"b + '1'", exist, "operator does not exist: boolean + unknown"},
		{"'1' - b", exist, "operator does not exist: unknown - boolean"},
		{"s + '1'", exist, "operator does not exist: varchar + unknown"},
		{"'1' * s", exist, "operator does not exist: unknown * varchar"},
		{"d * '2'", exist, "operator does not exist: date * unknown"},
		{"ts / '2'", exist, "operator does not exist: timestamp / unknown"},
		{"'2' / iv", exist, "operator does not exist: unknown / interval"},
		{"'2' / tm", exist, "operator does not exist: unknown / time"},
		{"'1 hour' - tt", exist, "operator does not exist: unknown - time with time zone"},
		{"i + 1 + (b - '1')", exist, "operator does not exist: boolean - unknown"},
		{"'7' % '2'", unique, "operator is not unique: unknown % unknown"},
		{"MOD('7', '2')", unique, "function mod(unknown, unknown) is not unique"},
		{"d % '1'", exist, "operator does not exist: date % unknown"},
		{"MOD(d, '1')", exist, "function mod(date, unknown) does not exist"},
		{"MOD('1', b)", exist, "function mod(unknown, boolean) does not exist"},
		{"iv % '2'", exist, "operator does not exist: interval % unknown"},
		{"MOD(i, 'x')", stateInvalidText, `"x"`},
		// The literal's own errors: only the text is checked.
		{"'x' + 1", stateInvalidText, `"x"`},
		{"i + 'x'", stateInvalidText, `"x"`},
		{"i + ''", stateInvalidText, `""`},
		{"i2 + '40000'", stateNumericRange, `40000`},
		{"i + '9999999999'", stateNumericRange, `9999999999`},
		{"d - '1'", stateDatetimeFormat, `"1"`},
		{"ts - '1 day'", stateDatetimeFormat, `"1 day"`},
		{"n + 'x'", stateInvalidText, `"x"`},
		{"CASE WHEN i = 1 THEN i + 'x' END", stateInvalidText, `"x"`},
		{"COALESCE(i, i * 'x')", stateInvalidText, `"x"`},
	} {
		operator := c.state == exist || c.state == unique
		err := checkUnknowns(parseTestExpr(t, c.expr), cols, nil)
		if err == nil || (operator && err.Error() != c.err) || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: error %v, want %s", c.expr, err, c.err)
			continue
		}
		if got := sqlState(err); got != c.state {
			t.Errorf("%s: SQLSTATE %s, want %s", c.expr, got, c.state)
		}
		// inferType gives the operator's errors too.
		if _, err := inferType(parseTestExpr(t, c.expr), opTestCols, nil); operator && (err == nil || err.Error() != c.err) {
			t.Errorf("%s: inferType's error %v, want %s", c.expr, err, c.err)
		}
	}
	for _, expr := range []string{
		"i + '1'", "' 1 ' + i", "f + 'NaN'", "f - '1e2'", "ts + '1 day'", "iv * '1.5'",
		"d - '2024-01-01'", "'1' || '1'",
		// A parameter without a value: its type is taken when it has one.
		"? + '1'", "'1 day' + ?",
	} {
		if err := checkUnknowns(parseTestExpr(t, expr), cols, nil); err != nil {
			t.Errorf("%s: %v", expr, err)
		}
	}
	// With a value, the parameter's type is the literal's.
	if err := checkUnknowns(parseTestExpr(t, "? + 'x'"), cols, []ColType{typeInt32}); err == nil ||
		sqlState(err) != stateInvalidText {
		t.Errorf("? + 'x' of an INTEGER: error %v", err)
	}
}

// Evaluated, the literal is a value of its type: id + '1' adds 1.
func TestUnknownOperandValues(t *testing.T) {
	ids := `(SELECT 1 AS id UNION ALL SELECT 2) v`
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{`SELECT '1' + 1, 2 * '3', '10' / 4, '7' - 10`, []string{"2|6|2|-3"}},
		{`SELECT id + '1', '10' - id, id * '-1' FROM ` + ids, []string{"2|9|-1", "3|8|-2"}},
		{`SELECT ' 1 ' + 1, '-3' * 2`, []string{"2|-6"}},
		{`SELECT 1.25 + '1.5', CAST(2.5 AS NUMERIC(10,2)) * '2', 1.5 + '1e2'`, []string{"2.75|5.00|101.5"}},
		{`SELECT CAST(2.5 AS DOUBLE PRECISION) - '0.5', CAST(1 AS DOUBLE PRECISION) / '4'`, []string{"2|0.25"}},
		{`SELECT DATE '2024-01-31' - '2024-01-01', '2024-03-01' - DATE '2024-01-31'`, []string{"30|30"}},
		{`SELECT TIMESTAMP '2024-01-31 10:00:00' + '1 day', TIMESTAMP '2024-01-31 10:00:00' - '2024-01-01'`,
			[]string{"2024-02-01 10:00:00|30 days 10:00:00"}},
		{`SELECT TIME '10:00' + '01:30', TIME '10:00' - '01:00'`, []string{"11:30:00|09:00:00"}},
		{`SELECT INTERVAL '1 day' + '1 hour', INTERVAL '1 day' * '2', '2' * INTERVAL '1 day', INTERVAL '1 day' / '2', '3 days' - INTERVAL '1 day'`,
			[]string{"1 day 01:00:00|2 days|2 days|12:00:00|2 days"}},
		{`SELECT id + '1' AS k, COUNT(*) FROM ` + ids + ` GROUP BY id + '1' ORDER BY k`, []string{"2|1", "3|1"}},
		{`SELECT SUM(id) + '1', MAX(id * '10') FROM ` + ids, []string{"4|20"}},
		{`SELECT id FROM ` + ids + ` ORDER BY id * '-1'`, []string{"2", "1"}},
		{`SELECT id, SUM(id) OVER (ORDER BY id) + '10' FROM ` + ids, []string{"1|11", "2|13"}},
		{`SELECT id FROM ` + ids + ` WHERE id + '1' = 3`, []string{"2"}},
		{`SELECT CASE WHEN id = 1 THEN '1' ELSE id END + '1' FROM ` + ids, []string{"2", "3"}},
		{`SELECT x + '1' FROM (SELECT '1' AS x UNION ALL SELECT 3) s`, []string{"2", "4"}},
		{`SELECT '7' % 2, id % '2', MOD('7', id), 7.5 % '2' FROM ` + ids, []string{"1|1|0|1.5", "1|0|1|1.5"}},
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
				cells[i] = v.Text()
			}
			got = append(got, strings.Join(cells, "|"))
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s\n got: %q\nwant: %q", c.sql, got, c.want)
		}
	}
	// Without an executor (a constant folded where it is parsed) too.
	for expr, want := range map[string]string{"'1' + 1": "2", "INTERVAL '1 day' * '2'": "2 days",
		"DATE '2024-01-02' - '2024-01-01'": "1", "'7' % 4": "3"} {
		if got, _, err := evalTestExpr(t, expr); err != nil || got != want {
			t.Errorf("%s = %s, %v; want %s", expr, got, err, want)
		}
	}
}
