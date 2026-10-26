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
	"math"
	"strconv"
	"strings"
	"testing"
)

// Text read as a NUMERIC, with the values and errors of postgres:16's
// CAST(text AS NUMERIC(p, s)).
func TestParseNumericText(t *testing.T) {
	const (
		bad      = `invalid input syntax for type numeric: "%s"`
		format   = "value overflows numeric format"
		overflow = "numeric field overflow; a field with precision %d, scale %d must round to an absolute value less than %s"
	)
	n52, n200 := decimalType(5, 2), decimalType(38, 0)
	for _, c := range []struct {
		s    string
		t    ColType
		want string // the value, or the error (%s is s)
	}{
		{"1.5", n52, "1.50"},
		{" 1.5 ", n52, "1.50"},
		{"\t-1.5\n", n52, "-1.50"},
		{"\v+.5\f", n52, "0.50"},
		{"1.", n52, "1.00"},
		{"-.005", n52, "-0.01"},
		{"-.004", n52, "0.00"},
		{"999.994", n52, "999.99"},
		{"000000000000000000000000000000000000000000001.5", n52, "1.50"},
		{"1e2", n52, "100.00"},
		{"1.5E-1", n52, "0.15"},
		{"1e+2", n52, "100.00"},
		{"5e-3", n52, "0.01"},
		{"1e-16383", n52, "0.00"},
		{"0e1073741823", n52, "0.00"},
		{"0.0000000000000000000000000000000000000000001", n52, "0.00"},
		// Underscores between digits, and 0x, 0o and 0b.
		{"1_000", n200, "1000"},
		{"1_000.5_5", decimalType(10, 2), "1000.55"},
		{"1e1_0", n200, "10000000000"},
		{"0x1F", n200, "31"},
		{"-0X1f", n200, "-31"},
		{"0x_1F", n200, "31"},
		{"0o17", n200, "15"},
		{"0b101", n200, "5"},
		{"0xFFFFFFFFFFFFFFFFFFFF", n200, "1208925819614629174706175"},
		{"0x1_F", n200, "31"},
		// Not numbers.
		{"abc", n52, bad},
		{"", n52, bad},
		{" ", n52, bad},
		{".", n52, bad},
		{"-", n52, bad},
		{"- 1", n52, bad},
		{"+-1", n52, bad},
		{"1.5.", n52, bad},
		{"1 5", n52, bad},
		{"1,5", n52, bad},
		{"1.5x", n52, bad},
		{"1e", n52, bad},
		{"1e+", n52, bad},
		{"e3", n52, bad},
		{"1e3.5", n52, bad},
		{"_1", n52, bad},
		{"1_", n52, bad},
		{"1__0", n52, bad},
		{"1_.5", n52, bad},
		{"1._5", n52, bad},
		{"._5", n52, bad},
		{"1e_1", n52, bad},
		{"1e1_", n52, bad},
		{"0x", n200, bad},
		{"0x1.5", n200, bad},
		{"0x1_", n200, bad},
		{"0o8", n200, bad},
		{"0b2", n200, bad},
		{"-NaN", n52, bad},
		{"+NaN", n52, bad},
		{"infinityx", n52, bad},
		{"infin", n52, bad},
		{"١", n52, bad},
		{" 1", n52, bad}, // not a C space
		{`"a"`, n52, bad},
		// Past Postgres's limits, or the type's.
		{"1e1073741824", n52, format},
		{"0e1073741824", n52, format},
		{"1e-1073741824", n52, format},
		{"1e-16384", n52, format},
		{"0e-16384", n52, format},
		{"0." + strings.Repeat("0", 16384), n52, format},
		{"10e131071", n52, format},
		{"5e131071", n52, strings.ReplaceAll(overflow, "%s", "10^3")},
		{"1e1000000000", n52, format},
		{"123456", n52, strings.ReplaceAll(overflow, "%s", "10^3")},
		{"999.995", n52, strings.ReplaceAll(overflow, "%s", "10^3")},
		{"0.1", decimalType(1, 2), strings.ReplaceAll(overflow, "%s", "10^-1")},
		{"1.5", decimalType(1, 1), strings.ReplaceAll(overflow, "%s", "1")},
	} {
		name := strconv.Quote(c.s) + " as " + c.t.SQLName()
		v, err := Coerce(stringValue(c.s), c.t)
		want := c.want
		if strings.Contains(want, "%") {
			want = strings.Replace(want, "%s", c.s, 1)
			want = strings.Replace(want, "%d", strconv.Itoa(int(c.t.Precision)), 1)
			want = strings.Replace(want, "%d", strconv.Itoa(int(c.t.Scale)), 1)
		}
		if err != nil {
			state := stateNumericRange
			if strings.HasPrefix(want, "invalid input syntax") {
				state = stateInvalidText
			}
			if err.Error() != want || sqlState(err) != state {
				t.Errorf("%s: %v (%s), want %q (%s)", name, err, sqlState(err), want, state)
			}
			continue
		}
		if got := formatDecimal(v.D, v.T.Scale); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	// NaN and the infinities are read, and a NUMERIC can't hold them.
	for _, s := range []string{"NaN", "nan", " Infinity ", "-inf", "+INF"} {
		if _, err := Coerce(stringValue(s), n52); err == nil || !strings.Contains(err.Error(), "NUMERIC values can't be NaN or infinite") {
			t.Errorf("%q: %v", s, err)
		}
	}
}

// Text read as a DOUBLE PRECISION or a REAL, with the values and errors of
// postgres:16's float8in and float4in (glibc's strtod and strtof).
func TestParseFloatText(t *testing.T) {
	const bad, big = stateInvalidText, stateNumericRange
	inf, nan := math.Inf(1), math.NaN()
	for _, c := range []struct {
		s     string
		k     Kind
		want  float64
		state string // "" for a value
		num   string // the number in an out-of-range error
	}{
		{"1.5", KindFloat64, 1.5, "", ""},
		{" 1.5 ", KindFloat64, 1.5, "", ""},
		{"\t-1.5\n", KindFloat64, -1.5, "", ""},
		{"1.", KindFloat64, 1, "", ""},
		{".5", KindFloat64, 0.5, "", ""},
		{"+.5e1", KindFloat64, 5, "", ""},
		{"1E+2", KindFloat64, 100, "", ""},
		{"1e-320", KindFloat64, 1e-320, "", ""},
		{"4.9e-324", KindFloat64, 5e-324, "", ""},
		{"2.4703282292062328e-324", KindFloat64, 5e-324, "", ""},
		{"1.7976931348623157e308", KindFloat64, math.MaxFloat64, "", ""},
		{"1e-40", KindFloat32, float64(float32(1e-40)), "", ""},
		{"3.4028235e38", KindFloat32, math.MaxFloat32, "", ""},
		{"0.1", KindFloat32, float64(float32(0.1)), "", ""},
		// Hexadecimal, with or without a binary exponent.
		{"0x1F", KindFloat64, 31, "", ""},
		{"-0x1.8p1", KindFloat64, -3, "", ""},
		{"0x.8", KindFloat64, 0.5, "", ""},
		{"0X1P-2", KindFloat64, 0.25, "", ""},
		{"0x1e3", KindFloat64, 483, "", ""},
		// NaN and the infinities, in any case and with a sign.
		{"NaN", KindFloat64, nan, "", ""},
		{"-nan", KindFloat64, nan, "", ""},
		{"+NaN", KindFloat32, nan, "", ""},
		{"nan(123)", KindFloat64, nan, "", ""},
		{"nan(a_1)", KindFloat64, nan, "", ""},
		{"Infinity", KindFloat64, inf, "", ""},
		{"-Infinity", KindFloat64, -inf, "", ""},
		{"+inf", KindFloat64, inf, "", ""},
		{"INF ", KindFloat32, inf, "", ""},
		// Not numbers.
		{"x", KindFloat64, 0, bad, ""},
		{"x", KindFloat32, 0, bad, ""},
		{"", KindFloat64, 0, bad, ""},
		{" ", KindFloat64, 0, bad, ""},
		{".", KindFloat64, 0, bad, ""},
		{"-", KindFloat64, 0, bad, ""},
		{"+-1", KindFloat64, 0, bad, ""},
		{"1_000", KindFloat64, 0, bad, ""},
		{"1e", KindFloat64, 0, bad, ""},
		{"1e+", KindFloat64, 0, bad, ""},
		{"1.5x", KindFloat64, 0, bad, ""},
		{"1.2.3", KindFloat64, 0, bad, ""},
		{"1,5", KindFloat64, 0, bad, ""},
		{"0o17", KindFloat64, 0, bad, ""},
		{"0b1", KindFloat64, 0, bad, ""},
		{"0x", KindFloat64, 0, bad, ""},
		{"0x.", KindFloat64, 0, bad, ""},
		{"0x1p", KindFloat64, 0, bad, ""},
		{"0x_1p0", KindFloat64, 0, bad, ""},
		{"infin", KindFloat64, 0, bad, ""},
		{"nan(", KindFloat64, 0, bad, ""},
		{"nan(1", KindFloat64, 0, bad, ""},
		{" 1", KindFloat64, 0, bad, ""},
		// Out of range, naming the number read.
		{"1e400", KindFloat64, 0, big, "1e400"},
		{"-1e400", KindFloat64, 0, big, "-1e400"},
		{" 1e400 ", KindFloat64, 0, big, "1e400"},
		{"1e400x", KindFloat64, 0, big, "1e400"},
		{"1e-400", KindFloat64, 0, big, "1e-400"},
		{"2.4703282292062327e-324", KindFloat64, 0, big, "2.4703282292062327e-324"},
		{"1.7976931348623159e308", KindFloat64, 0, big, "1.7976931348623159e308"},
		{"0x1p99999", KindFloat64, 0, big, "0x1p99999"},
		{"1e39", KindFloat32, 0, big, "1e39"},
		{"3.4028236e38", KindFloat32, 0, big, "3.4028236e38"},
		{"1e-50", KindFloat32, 0, big, "1e-50"},
	} {
		typ := floatTypeName(c.k)
		name := strconv.Quote(c.s) + " as " + typ
		got, err := parseFloatText(c.s, c.k)
		if c.state == "" {
			if err != nil || got != c.want && !(math.IsNaN(got) && math.IsNaN(c.want)) {
				t.Errorf("%s = %v, %v; want %v", name, got, err, c.want)
			}
			continue
		}
		want := `invalid input syntax for type ` + typ + `: "` + c.s + `"`
		if c.state == big {
			want = `"` + c.num + `" is out of range for type ` + typ
		}
		if err == nil || err.Error() != want || sqlState(err) != c.state {
			t.Errorf("%s: got %v, %v (%s); want %q (%s)", name, got, err, sqlState(err), want, c.state)
		}
	}
}

// Text read as a BOOLEAN, as postgres:16's boolin reads it.
func TestParseBoolText(t *testing.T) {
	for s, want := range map[string]bool{
		"t": true, "tr": true, "TRUE": true, " true ": true, "\ttrue\n": true,
		"y": true, "Ye": true, "yes": true, "on": true, "ON ": true, "1": true,
		"f": false, "fa": false, "fals": false, "False": false, "n": false, "NO": false,
		"of": false, "off": false, "0": false,
	} {
		if got, err := parseBoolText(s); err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", s, got, err, want)
		}
	}
	for _, s := range []string{"maybe", "", " ", "o", "onn", "offf", "truex", "yess", "nO!", "01", "2", "t r", `"t"`, " t"} {
		want := `invalid input syntax for type boolean: "` + s + `"`
		if _, err := parseBoolText(s); err == nil || err.Error() != want || sqlState(err) != stateInvalidText {
			t.Errorf("%q: %v, want %q", s, err, want)
		}
	}
}

// Text converted to a number, a boolean, a date or a time the ways a
// statement converts it, with postgres:16's messages and SQLSTATEs.
func TestTextConversionErrors(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"CAST('1_000.5' AS NUMERIC(10, 2))", "1000.50", "NUMERIC(10,2)"},
		{"CAST('0x1F' AS DOUBLE PRECISION)", "31", "DOUBLE PRECISION"},
		{"CAST('-NaN' AS DOUBLE PRECISION)", "NaN", "DOUBLE PRECISION"},
		{"CAST('tr' AS BOOLEAN)", "true", "BOOLEAN"},
		{"'of'::BOOLEAN", "false", "BOOLEAN"},
		{"CAST(1.5 AS NUMERIC(5, 2)) = ' 1.5 '", "true", ""},
		{"TRY_CAST('1e400' AS DOUBLE PRECISION)", "NULL", "DOUBLE PRECISION"},
		{"TRY_CAST('maybe' AS BOOLEAN)", "NULL", "BOOLEAN"},
		{"TRY_CAST('2024-02-30' AS DATE)", "NULL", "DATE"},
		{"CAST('x' AS NUMERIC DEFAULT 0 ON CONVERSION ERROR)", "0.000000000", "NUMERIC(38,9)"},
		// A time and an offset or a time zone after a date are checked,
		// and ignored; one after a time too.
		{"CAST('2024-01-05 23:00-05' AS DATE)", "2024-01-05", "DATE"},
		{"CAST('2024-01-05 24:00' AS DATE)", "2024-01-05", "DATE"},
		{"DATE '2024-01-05 10:00'", "2024-01-05", "DATE"},
		{"CAST('10:00:00+05' AS TIME)", "10:00:00", "TIME(6)"},
		{"CAST('2024-01-05 10:00 UTC' AS TIME)", "10:00:00", "TIME(6)"},
		{"CAST('23:59:60' AS TIME) = TIME '24:00'", "true", ""},
		// Postgres's offsets: ±H, ±HHMM, ±HH:MM:SS.
		{"CAST(CAST('2024-01-05 10:00:00+530' AS TIMESTAMPTZ) AS VARCHAR)", "2024-01-05 04:30:00+00", "VARCHAR"},
		{"CAST(CAST('2024-01-05 10:00:00+05:30:15' AS TIMESTAMPTZ) AS VARCHAR)", "2024-01-05 04:29:45+00", "VARCHAR"},
		{"CAST(CAST('2024-01-05 10:00:00+15:59:59' AS TIMESTAMPTZ) AS VARCHAR)", "2024-01-04 18:00:01+00", "VARCHAR"},
		{"CAST('5881580-07-11' AS DATE)", "5881580-07-11", "DATE"},
	})
	for _, c := range []struct{ expr, state, msg string }{
		{"CAST('abc' AS NUMERIC)", stateInvalidText, `invalid input syntax for type numeric: "abc"`},
		{"'abc'::DECIMAL(10, 2)", stateInvalidText, `invalid input syntax for type numeric: "abc"`},
		{"CAST(' 1.5x ' AS NUMERIC)", stateInvalidText, `invalid input syntax for type numeric: " 1.5x "`},
		{"CAST('1e9999999999' AS NUMERIC)", stateNumericRange, "value overflows numeric format"},
		{"CAST('123456' AS NUMERIC(5, 2))", stateNumericRange,
			"numeric field overflow; a field with precision 5, scale 2 must round to an absolute value less than 10^3"},
		{"CAST('x' AS DOUBLE PRECISION)", stateInvalidText, `invalid input syntax for type double precision: "x"`},
		{"CAST('x' AS FLOAT)", stateInvalidText, `invalid input syntax for type double precision: "x"`},
		{"'x'::REAL", stateInvalidText, `invalid input syntax for type real: "x"`},
		{"CAST('1_000' AS DOUBLE PRECISION)", stateInvalidText, `invalid input syntax for type double precision: "1_000"`},
		{"CAST(' 1e400 ' AS DOUBLE PRECISION)", stateNumericRange, `"1e400" is out of range for type double precision`},
		{"CAST('1e39' AS REAL)", stateNumericRange, `"1e39" is out of range for type real`},
		{"'maybe'::BOOLEAN", stateInvalidText, `invalid input syntax for type boolean: "maybe"`},
		{"CAST('x' AS DATE)", stateDatetimeFormat, `invalid input syntax for type date: "x"`},
		{"CAST(' x ' AS DATE)", stateDatetimeFormat, `invalid input syntax for type date: " x "`},
		{"DATE 'x'", stateDatetimeFormat, `invalid input syntax for type date: "x"`},
		{"CAST('2024-01-05x' AS DATE)", stateDatetimeFormat, `invalid input syntax for type date: "2024-01-05x"`},
		{"CAST('2024-01-05 x' AS DATE)", stateDatetimeFormat, `invalid input syntax for type date: "2024-01-05 x"`},
		{"'2024-02-30'::DATE", stateDatetimeRange, `date/time field value out of range: "2024-02-30"`},
		{"DATE '2024-02-30'", stateDatetimeRange, `date/time field value out of range: "2024-02-30"`},
		{"CAST('2024-01-05 25:00' AS DATE)", stateDatetimeRange, `date/time field value out of range: "2024-01-05 25:00"`},
		{"CAST('5881580-07-12' AS DATE)", stateDatetimeRange, `date out of range: "5881580-07-12"`},
		{"CAST('x' AS TIME)", stateDatetimeFormat, `invalid input syntax for type time: "x"`},
		{"TIME 'x'", stateDatetimeFormat, `invalid input syntax for type time: "x"`},
		{"CAST('x' AS TIME(3))", stateDatetimeFormat, `invalid input syntax for type time: "x"`},
		{"CAST('25:00' AS TIME)", stateDatetimeRange, `date/time field value out of range: "25:00"`},
		{"CAST('24:00:01' AS TIME)", stateDatetimeRange, `date/time field value out of range: "24:00:01"`},
		{"TIME '24:00:00.5'", stateDatetimeRange, `date/time field value out of range: "24:00:00.5"`},
		{"CAST('23:59:60.5' AS TIME)", stateDatetimeRange, `date/time field value out of range: "23:59:60.5"`},
		{"CAST('10:00:00 x' AS TIME)", stateDatetimeFormat, `invalid input syntax for type time: "10:00:00 x"`},
		{"CAST('10:00+16' AS TIME)", stateTZDisplacement, `time zone displacement out of range: "10:00+16"`},
		{"CAST('10:00 America/New_York' AS TIME)", stateDatetimeFormat, `invalid input syntax for type time: "10:00 America/New_York"`},
		{"CAST('10:00 Mars/Base' AS TIME)", stateInvalidParameter, `time zone "mars/base" not recognized`},
		{"CAST('x' AS TIMESTAMP)", stateDatetimeFormat, `invalid input syntax for type timestamp: "x"`},
		{"TIMESTAMP 'x'", stateDatetimeFormat, `invalid input syntax for type timestamp: "x"`},
		{"CAST('x' AS TIMESTAMP WITH TIME ZONE)", stateDatetimeFormat, `invalid input syntax for type timestamp with time zone: "x"`},
		{"'x'::TIMESTAMPTZ", stateDatetimeFormat, `invalid input syntax for type timestamp with time zone: "x"`},
		{"TIMESTAMPTZ 'x'", stateDatetimeFormat, `invalid input syntax for type timestamp with time zone: "x"`},
		{"CAST('2024-02-30 10:00' AS TIMESTAMP)", stateDatetimeRange, `date/time field value out of range: "2024-02-30 10:00"`},
		{"CAST('2024-01-05 24:00:01' AS TIMESTAMP)", stateDatetimeRange, `date/time field value out of range: "2024-01-05 24:00:01"`},
		{"CAST('2024-01-05 10:00x' AS TIMESTAMP)", stateDatetimeFormat, `invalid input syntax for type timestamp: "2024-01-05 10:00x"`},
		{"CAST('2024-01-05 10:00:00+16' AS TIMESTAMPTZ)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00+16"`},
		{"CAST('2024-01-05 10:00:00-16:00' AS TIMESTAMP)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00-16:00"`},
		{"CAST('2024-01-05 10:00:00+1599' AS TIMESTAMPTZ)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00+1599"`},
		{"CAST('2024-01-05 10:00:00+xx' AS TIMESTAMPTZ)", stateDatetimeFormat, `invalid input syntax for type timestamp with time zone: "2024-01-05 10:00:00+xx"`},
		{"CAST('2024-01-05 10:00 Mars/Base' AS TIMESTAMPTZ)", stateInvalidParameter, `time zone "mars/base" not recognized`},
		{"CAST('2024-01-05 10:00 xyz' AS TIMESTAMP)", stateDatetimeFormat, `invalid input syntax for type timestamp: "2024-01-05 10:00 xyz"`},
		{"CAST('294277-01-01' AS TIMESTAMP)", stateDatetimeRange, `timestamp out of range: "294277-01-01"`},
		{"TIMESTAMP '294277-01-01'", stateDatetimeRange, `timestamp out of range: "294277-01-01"`},
		{"CAST('\"q\"' AS TIME WITH TIME ZONE)", stateDatetimeFormat, `invalid input syntax for type time with time zone: ""q""`},
		{"CAST('10:00 Mars/Base' AS TIMETZ)", stateInvalidParameter, `time zone "mars/base" not recognized`},
		// Text compared with a value is read as its type.
		{"CAST(1.5 AS NUMERIC) = 'x'", stateInvalidText, `invalid input syntax for type numeric: "x"`},
		{"CAST(1 AS DOUBLE PRECISION) < 'x'", stateInvalidText, `invalid input syntax for type double precision: "x"`},
		{"CAST(1 AS REAL) = '1e39'", stateNumericRange, `"1e39" is out of range for type real`},
		{"TRUE = 'maybe'", stateInvalidText, `invalid input syntax for type boolean: "maybe"`},
		{"DATE '2024-01-01' = 'x'", stateDatetimeFormat, `invalid input syntax for type date: "x"`},
		{"DATE '2024-01-01' = '2024-02-30'", stateDatetimeRange, `date/time field value out of range: "2024-02-30"`},
		{"TIME '10:00' = '25:00'", stateDatetimeRange, `date/time field value out of range: "25:00"`},
		{"TIMESTAMP '2024-01-05' IS DISTINCT FROM 'x'", stateDatetimeFormat, `invalid input syntax for type timestamp: "x"`},
		{"COS('abc')", stateInvalidText, `COS: invalid input syntax for type double precision: "abc"`},
	} {
		err := exprError(t, c.expr)
		if err == nil || !strings.HasSuffix(err.Error(), c.msg) || sqlState(err) != c.state {
			t.Errorf("%s: got %v (%s), want %q (%s)", c.expr, err, sqlState(err), c.msg, c.state)
		}
	}
}

// exprError is the error of a constant expression: parsing it (a literal
// such as DATE 'x'), typing it or evaluating it.
func exprError(t *testing.T, expr string) error {
	t.Helper()
	if _, err := ParseScript("SELECT " + expr); err != nil {
		return err
	}
	_, _, err := evalTestExpr(t, expr)
	return err
}

// Numbers converted to a type that can't hold them, with postgres:16's
// messages.
func TestNumberRangeErrors(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"CAST(CAST('NaN' AS DOUBLE PRECISION) AS REAL)", "NaN", "REAL"},
		{"CAST(CAST('-Infinity' AS DOUBLE PRECISION) AS REAL)", "-Infinity", "REAL"},
		{"CAST(CAST(1e-40 AS DOUBLE PRECISION) AS REAL) > 0", "true", ""},
		{"CAST(CAST(32767 AS INTEGER) AS SMALLINT)", "32767", "SMALLINT"},
	})
	for _, c := range []struct{ expr, msg string }{
		{"CAST(3000000000 AS INTEGER)", "integer out of range"},
		{"CAST(40000 AS SMALLINT)", "smallint out of range"},
		{"CAST(CAST(3e9 AS DOUBLE PRECISION) AS INTEGER)", "integer out of range"},
		{"CAST(3000000000.5 AS INTEGER)", "integer out of range"},
		{"CAST(CAST(1e300 AS DOUBLE PRECISION) AS BIGINT)", "bigint out of range"},
		{"CAST(CAST('NaN' AS DOUBLE PRECISION) AS INTEGER)", "integer out of range"},
		{"CAST(CAST('-Infinity' AS DOUBLE PRECISION) AS BIGINT)", "bigint out of range"},
		{"CAST(CAST(99999999999999999999 AS NUMERIC(20, 0)) AS BIGINT)", "bigint out of range"},
		{"CAST(CAST(1e39 AS DOUBLE PRECISION) AS REAL)", "value out of range: overflow"},
		{"CAST(CAST(1e-50 AS DOUBLE PRECISION) AS REAL)", "value out of range: underflow"},
		{"CAST(CAST('1e39' AS NUMERIC(40, 0)) AS REAL)", `"1000000000000000000000000000000000000000" is out of range for type real`},
		{"CAST(123456 AS NUMERIC(5, 2))", "numeric field overflow; a field with precision 5, scale 2 must round to an absolute value less than 10^3"},
		{"CAST(9.5 AS NUMERIC(1, 0))", "numeric field overflow; a field with precision 1, scale 0 must round to an absolute value less than 10^1"},
		{"CAST(CAST(1e10 AS DOUBLE PRECISION) AS NUMERIC(5, 2))", "numeric field overflow; a field with precision 5, scale 2 must round to an absolute value less than 10^3"},
	} {
		_, _, err := evalTestExpr(t, c.expr)
		if err == nil || err.Error() != c.msg || sqlState(err) != stateNumericRange {
			t.Errorf("%s: got %v (%s), want %q", c.expr, err, sqlState(err), c.msg)
		}
	}
}
