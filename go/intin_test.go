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
	"strconv"
	"strings"
	"testing"
)

// Text read as an integer, with the values and errors of postgres:16's
// CAST(text AS SMALLINT / INTEGER / BIGINT).
func TestParseIntText(t *testing.T) {
	const (
		bad = stateInvalidText
		big = stateNumericRange
	)
	for _, c := range []struct {
		s     string
		k     Kind
		want  int64
		state string // "" for a value
	}{
		{"1", KindInt32, 1, ""},
		{" 1 ", KindInt32, 1, ""},
		{"\t7\t", KindInt32, 7, ""},
		{"\n-12\r", KindInt16, -12, ""},
		{"\v7\f", KindInt32, 7, ""},
		{"+5", KindInt32, 5, ""},
		{"-5", KindInt32, -5, ""},
		{"-0", KindInt32, 0, ""},
		{"0", KindInt32, 0, ""},
		{"00012", KindInt32, 12, ""},
		{"00000000000000000000000001", KindInt32, 1, ""},
		// Underscores between digits, and 0x, 0o and 0b.
		{"1_000", KindInt32, 1000, ""},
		{"0_1", KindInt32, 1, ""},
		{"1_000_000_000_000", KindInt64, 1000000000000, ""},
		{"0x1F", KindInt32, 31, ""},
		{"0X1f", KindInt32, 31, ""},
		{"0o17", KindInt32, 15, ""},
		{"0b101", KindInt32, 5, ""},
		{"-0b11", KindInt16, -3, ""},
		{"0x_1", KindInt32, 1, ""},
		{"+0x_F", KindInt32, 15, ""},
		{"0x1_F", KindInt32, 31, ""},
		{"0x00000000000000000000001", KindInt16, 1, ""},
		// Each type's range.
		{"32767", KindInt16, 32767, ""},
		{"-32768", KindInt16, -32768, ""},
		{"-0x8000", KindInt16, -32768, ""},
		{"2147483647", KindInt32, 2147483647, ""},
		{"-2147483648", KindInt32, -2147483648, ""},
		{"-0x80000000", KindInt32, -2147483648, ""},
		{"0o17777777777", KindInt32, 2147483647, ""},
		{"-0o20000000000", KindInt32, -2147483648, ""},
		{"0b1111111111111111111111111111111", KindInt32, 2147483647, ""},
		{"9223372036854775807", KindInt64, 9223372036854775807, ""},
		{"-9223372036854775808", KindInt64, -9223372036854775808, ""},
		{"0x7FFFFFFFFFFFFFFF", KindInt64, 9223372036854775807, ""},
		{"-0x8000000000000000", KindInt64, -9223372036854775808, ""},
		// Not integers.
		{"1.5", KindInt32, 0, bad},
		{" 1.5 ", KindInt32, 0, bad},
		{"1.0", KindInt32, 0, bad},
		{".5", KindInt32, 0, bad},
		{"1e3", KindInt32, 0, bad},
		{"x", KindInt32, 0, bad},
		{"x", KindInt64, 0, bad},
		{"x", KindInt16, 0, bad},
		{"", KindInt32, 0, bad},
		{" ", KindInt32, 0, bad},
		{"- 5", KindInt32, 0, bad},
		{"++1", KindInt32, 0, bad},
		{"+-1", KindInt32, 0, bad},
		{"1-", KindInt32, 0, bad},
		{"1 2", KindInt32, 0, bad},
		{"1 ", KindInt32, 0, bad},
		{"_1000", KindInt32, 0, bad},
		{"-_1", KindInt32, 0, bad},
		{"1000_", KindInt32, 0, bad},
		{"1__000", KindInt32, 0, bad},
		{"0x", KindInt32, 0, bad},
		{"0b", KindInt32, 0, bad},
		{"0x1_", KindInt32, 0, bad},
		{"0x1__2", KindInt32, 0, bad},
		{"0o8", KindInt32, 0, bad},
		{"0b2", KindInt32, 0, bad},
		{"0xG", KindInt32, 0, bad},
		{"Infinity", KindInt32, 0, bad},
		{"NaN", KindInt64, 0, bad},
		{"١", KindInt32, 0, bad},
		{"1\x00", KindInt32, 0, bad},
		// Out of range, also when what follows the digits that overflowed
		// isn't a digit; text that ends badly before the value overflows is
		// invalid.
		{"32768", KindInt16, 0, big},
		{"-32769", KindInt16, 0, big},
		{"0x8000", KindInt16, 0, big},
		{"2147483648", KindInt32, 0, big},
		{"-2147483649", KindInt32, 0, big},
		{"0x80000000", KindInt32, 0, big},
		{"0o20000000000", KindInt32, 0, big},
		{"0b10000000000000000000000000000000", KindInt32, 0, big},
		{"2_147_483_648", KindInt32, 0, big},
		{"99999999999x", KindInt32, 0, big},
		{"21474836480x", KindInt32, 0, big},
		{"9_999_999_999_", KindInt32, 0, big},
		{"2147483648x", KindInt32, 0, bad},
		{"2147483648_", KindInt32, 0, bad},
		{"9223372036854775808", KindInt64, 0, big},
		{"-9223372036854775809", KindInt64, 0, big},
		{"99999999999999999999", KindInt64, 0, big},
		{"0x8000000000000000", KindInt64, 0, big},
	} {
		name := strconv.Quote(c.s) + " as " + intTypeName(c.k)
		got, err := parseIntText(c.s, c.k)
		if c.state == "" {
			if err != nil || got != c.want {
				t.Errorf("%s = %d, %v; want %d", name, got, err, c.want)
			}
			continue
		}
		want := `invalid input syntax for type ` + intTypeName(c.k) + `: "` + c.s + `"`
		if c.state == big {
			want = `value "` + c.s + `" is out of range for type ` + intTypeName(c.k)
		}
		if err == nil || err.Error() != want || sqlState(err) != c.state {
			t.Errorf("%s: got %d, %v (%s); want %q (%s)", name, got, err, sqlState(err), want, c.state)
		}
	}
}

// Text converted to an integer type the ways a statement converts it, each
// as Postgres does.
func TestTextToInteger(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"CAST(' 1 ' AS INTEGER)", "1", "INTEGER"},
		{"CAST('0x1F' AS SMALLINT)", "31", "SMALLINT"},
		{"'1_000'::BIGINT", "1000", "BIGINT"},
		{"CAST('12' AS INTEGER) + 1", "13", ""},
		{"1 = ' 1 '", "true", ""},
		{"ROUND(1.2345, '2') = 1.23", "true", ""},
		{"LPAD('a', '3', '*')", "**a", ""},
		// TRY_CAST and DEFAULT … ON CONVERSION ERROR take the fallback.
		{"TRY_CAST('1.5' AS INTEGER)", "NULL", "INTEGER"},
		{"CAST('1e3' AS INTEGER DEFAULT -1 ON CONVERSION ERROR)", "-1", "INTEGER"},
		{"TRY_CAST('3000000000' AS INTEGER)", "NULL", "INTEGER"},
	})
	for _, c := range []struct{ expr, state, msg string }{
		{"CAST('1.5' AS INTEGER)", stateInvalidText, `invalid input syntax for type integer: "1.5"`},
		{"CAST('1e3' AS INTEGER)", stateInvalidText, `invalid input syntax for type integer: "1e3"`},
		{"'1.5'::INTEGER", stateInvalidText, `invalid input syntax for type integer: "1.5"`},
		{"CAST('x' AS BIGINT)", stateInvalidText, `invalid input syntax for type bigint: "x"`},
		{"CAST(' x ' AS SMALLINT)", stateInvalidText, `invalid input syntax for type smallint: " x "`},
		{"CAST('3000000000' AS INTEGER)", stateNumericRange, `value "3000000000" is out of range for type integer`},
		{"CAST('99999999999999999999' AS BIGINT)", stateNumericRange, `value "99999999999999999999" is out of range for type bigint`},
		// Text compared with an integer is read as one (of the type of
		// the other operand: an integer literal is a BIGINT).
		{"CAST(1 AS INTEGER) = '1.5'", stateInvalidText, `invalid input syntax for type integer: "1.5"`},
		{"1 = '1.5'", stateInvalidText, `invalid input syntax for type integer: "1.5"`},
		{"CAST(1 AS INTEGER) < 'x'", stateInvalidText, `invalid input syntax for type integer: "x"`},
		{"CAST(1 AS INTEGER) IS DISTINCT FROM '1.0'", stateInvalidText, `invalid input syntax for type integer: "1.0"`},
		{"CAST(1 AS INTEGER) = '99999999999'", stateNumericRange, `value "99999999999" is out of range for type integer`},
		{"NULLIF(CAST(1 AS INTEGER), '1.5')", stateInvalidText, `invalid input syntax for type integer: "1.5"`},
		// So is text for a function's integer parameter, as an INTEGER.
		{"ROUND(1.2345, '2.5')", stateInvalidText,
			`ROUND: the number of decimal places must be an integer: invalid input syntax for type integer: "2.5"`},
		{"LPAD('a', '3.5', '*')", stateInvalidText, `LPAD: the length must be an integer: invalid input syntax for type integer: "3.5"`},
		{"REPEAT('a', '3000000000')", stateNumericRange, `REPEAT: the count must be an integer: value "3000000000" is out of range for type integer`},
		{"CAST('x' AS INTEGER DEFAULT 'y' ON CONVERSION ERROR)", stateInvalidText, `invalid input syntax for type integer: "y"`},
	} {
		_, _, err := evalTestExpr(t, c.expr)
		if err == nil || !strings.HasSuffix(err.Error(), c.msg) || sqlState(err) != c.state {
			t.Errorf("%s: got %v (%s), want %q (%s)", c.expr, err, sqlState(err), c.msg, c.state)
		}
	}
}
