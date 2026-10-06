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

// Unit tests for reading interval literals, for rendering intervals as
// text, for the interval range, for interval * n and interval / n, and for
// the aggregates of intervals. They need no Redis server.

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Interval literals are read as Postgres 16 reads them: each field's integer
// part exactly, and its fraction cascaded into the smaller units and rounded
// to the microsecond. The values are Postgres 16's.
func TestIntervalLiteral(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// A fraction of a second, millisecond or microsecond is rounded to
		// the microsecond, and half of one toward zero.
		{"0.0000015 seconds", "00:00:00.000001"},
		{"0.0000025 seconds", "00:00:00.000002"},
		{"1.0000005 sec", "00:00:01"},
		{"-0.0000015 seconds", "-00:00:00.000001"},
		{"0.0000005001 seconds", "00:00:00.000001"},
		{"1.5 microseconds", "00:00:00.000001"},
		{"1.6 us", "00:00:00.000002"},
		{"-1.5 us", "-00:00:00.000001"},
		{"0.0013 ms", "00:00:00.000001"},
		{"1.5 ms", "00:00:00.0015"},
		{"1.5", "00:00:01.5"},
		// A fraction of a minute, hour, day or week too, and a fraction of a
		// month cascades into days (30 per month) and then microseconds.
		{"0.123456789 minutes", "00:00:07.407407"},
		{"1.5 hours", "01:30:00"},
		{"0.333333333 days", "07:59:59.999971"},
		{"1.5 days", "1 day 12:00:00"},
		{"1.5 weeks", "10 days 12:00:00"},
		{"0.123456789 weeks", "20:44:26.665987"},
		{"1.5 months", "1 mon 15 days"},
		{"0.1 mon", "3 days"},
		{"1.000001 months", "1 mon 00:00:02.592"},
		{"-1.5 months", "-1 mons -15 days"},
		{"1.5 months ago", "-1 mons -15 days"},
		{"-1.5 months ago", "1 mon 15 days"},
		{"1 day -0.0000015 seconds ago", "-1 days +00:00:00.000001"},
		// A fraction of a year, decade, century or millennium is whole
		// months, rounded half to even.
		{"1.5 years", "1 year 6 mons"},
		{"0.04 years", "00:00:00"},
		{"0.05 years", "1 mon"},
		{"0.125 years", "2 mons"},
		{"0.375 years", "4 mons"},
		{"-0.125 years", "-2 mons"},
		{"1.5 decades", "15 years"},
		{"0.01 decade", "1 mon"},
		{"0.5 century", "50 years"},
		{"0.001 millennium", "1 year"},
		// A clock's fraction of a second is rounded half to even. With a
		// fraction, two fields are minutes and seconds.
		{"1 year 2 months 3 days 04:05:06.5", "1 year 2 mons 3 days 04:05:06.5"},
		{"3 04:05:06", "3 days 04:05:06"},
		{"00:00:00.0000005", "00:00:00"},
		{"00:00:00.0000015", "00:00:00.000002"},
		{"00:00:00.0000025", "00:00:00.000002"},
		{"00:00:00.9999995", "00:00:01"},
		{"00:00:00.99999949", "00:00:00.999999"},
		{"1 day -00:00:00.0000015", "1 day -00:00:00.000002"},
		{"1:30.5", "00:01:30.5"},
		{"-1:30.5", "-00:01:30.5"},
		{"1:30", "01:30:00"},
		{"1:30:60", "01:31:00"},
		{"+1:30:00", "01:30:00"},
		// The time is exact past 2^53 nanoseconds.
		{"1000000:00:00.000001", "1000000:00:00.000001"},
		{"2562047:47:16.854775", "2562047:47:16.854775"},
		{"-2562047:47:16.854775", "-2562047:47:16.854775"},
		{"2836.854775808 seconds", "00:47:16.854776"},
		// Years-months.
		{"1-2", "1 year 2 mons"},
		{"-1-2", "-1 years -2 mons"},
		{"0-11", "11 mons"},
		{"178956970-7", "178956970 years 7 mons"},
		{"-178956970-8", "-178956970 years -8 mons"},
		// ISO 8601, with designators and in the alternative format.
		{"P1Y2M3DT4H5M6S", "1 year 2 mons 3 days 04:05:06"},
		{"P0.5Y", "6 mons"},
		{"P0.01Y", "00:00:00"},
		{"P0.05Y", "1 mon"},
		{"P-1.5Y", "-1 years -6 mons"},
		{"P1Y-2M", "10 mons"},
		{"P1.5M", "1 mon 15 days"},
		{"P1.5W", "10 days 12:00:00"},
		{"P1.5D", "1 day 12:00:00"},
		{"PT1.5H", "01:30:00"},
		{"PT0.0000015S", "00:00:00.000001"},
		{"PT0.0000025S", "00:00:00.000002"},
		{"PT1.0000005S", "00:00:01.000001"},
		{"PT2562047H47M16.854775S", "2562047:47:16.854775"},
		{"PT1H1H", "02:00:00"},
		{"P1e2D", "100 days"},
		{"PT1.5e3S", "00:25:00"},
		{"P0002-06-07T01:30:00", "2 years 6 mons 7 days 01:30:00"},
		{"P0002-06-07T01:30:00.0000015", "2 years 6 mons 7 days 01:30:00.000001"},
		{"P00020607T013000", "2 years 6 mons 7 days 01:30:00"},
		{"P0002-06", "2 years 6 mons"},
		{"P0002-06-07", "2 years 6 mons 7 days"},
		{"P1", "1 year"},
		{"PT01:30", "01:30:00"},
		{"PT013000.5", "01:30:00"},
	} {
		v, err := intervalLiteral(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
		} else if got := formatInterval(v); got != c.want {
			t.Errorf("%q = %q, want %q", c.in, got, c.want)
		}
	}

	for _, c := range []struct{ in, err string }{
		// A clock's minutes are 0 to 59 and its seconds 0 to 60, and the
		// months of years-months 0 to 11.
		{"1:75:00", `interval field value out of range: "1:75:00"`},
		{"1:30:61", `interval field value out of range: "1:30:61"`},
		{"75:30.5", `interval field value out of range: "75:30.5"`},
		{"1-12", `interval field value out of range: "1-12"`},
		// Postgres: invalid input syntax for type interval.
		{"1.5:00", `invalid interval "1.5:00"`},
		{"1:-30:00", `invalid interval "1:-30:00"`},
		{"P1Y2M3D4H", `invalid interval "P1Y2M3D4H"`},
		{"P1Y2", `invalid interval "P1Y2"`},
		{"PT1H2", `invalid interval "PT1H2"`},
	} {
		if _, err := intervalLiteral(c.in); err == nil || err.Error() != c.err {
			t.Errorf("%q: error %v, want %s", c.in, err, c.err)
		}
	}

	// INTERVAL 'n' UNIT and INTERVAL n UNIT read n as a literal's number,
	// and text cast to an interval is a literal.
	runScalarCases(t, []scalarCase{
		{"CAST(INTERVAL '0.0000015' SECOND AS VARCHAR)", "00:00:00.000001", "VARCHAR"},
		{"CAST(INTERVAL '1.0000005' SECOND AS VARCHAR)", "00:00:01", "VARCHAR"},
		{"CAST(INTERVAL 1.0000005 SECOND AS VARCHAR)", "00:00:01", "VARCHAR"},
		{"CAST(INTERVAL 1.5 HOUR AS VARCHAR)", "01:30:00", "VARCHAR"},
		{"CAST(INTERVAL '1e3' SECOND AS VARCHAR)", "00:16:40", "VARCHAR"},
		{"CAST(CAST('0.0000015 seconds' AS INTERVAL) AS VARCHAR)", "00:00:00.000001", "VARCHAR"},
	})
}

// formatInterval renders the time part's sign once, in front of the clock,
// including for the extremes of the nanosecond field: negating MinInt64
// overflows int64, so its magnitude must not be taken in int64.
func TestFormatInterval(t *testing.T) {
	iv := func(months, days int32, ns int64) Value {
		return Value{T: typeInterval, Months: months, Days: days, I: ns}
	}
	cases := []struct {
		v    Value
		want string
	}{
		{iv(0, 0, 0), "00:00:00"},
		{iv(0, 0, 1), "00:00:00.000000001"},
		{iv(0, 0, -1), "-00:00:00.000000001"},
		{iv(0, 0, -(4*nsPerHour + 5*nsPerMinute + 6*nsPerSecond + 500_000_000)), "-04:05:06.5"},
		{iv(0, 0, math.MaxInt64), "2562047:47:16.854775807"},
		{iv(0, 0, -math.MaxInt64), "-2562047:47:16.854775807"},
		{iv(0, 0, math.MinInt64), "-2562047:47:16.854775808"},
		{iv(14, 3, math.MinInt64), "1 year 2 mons 3 days -2562047:47:16.854775808"},
		{iv(-14, -3, 0), "-1 years -2 mons -3 days"},
		// A positive field after a negative one has a "+", as in Postgres.
		{iv(0, -1, 2*nsPerHour), "-1 days +02:00:00"},
		{iv(-12, 3, 0), "-1 years +3 days"},
		{iv(-11, 0, 0), "-11 mons"},
		{iv(-13, 0, 0), "-1 years -1 mons"},
		{iv(-10, 0, 0), "-10 mons"},
		{iv(-1, 2, -3*nsPerHour), "-1 mons +2 days -03:00:00"},
		{iv(14, -2, nsPerSecond/2), "1 year 2 mons -2 days +00:00:00.5"},
		{iv(0, 1, -nsPerHour), "1 day -01:00:00"},
	}
	for _, c := range cases {
		if got := formatInterval(c.v); got != c.want {
			t.Errorf("formatInterval(%d months, %d days, %d ns) = %q, want %q", c.v.Months, c.v.Days, c.v.I, got, c.want)
		}
	}

	runScalarCases(t, []scalarCase{
		{"CAST(" + minNanos + " AS VARCHAR)", "-2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-1 day' + " + minNanos + " AS VARCHAR)", "-1 days -2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-1 day 2 hours' AS VARCHAR)", "-1 days +02:00:00", "VARCHAR"},
		// The text reads back as the same interval.
		{"CAST(CAST(INTERVAL '-1 year 2 mons -3 days 4 hours' AS VARCHAR) AS INTERVAL) = INTERVAL '-1 year 2 mons -3 days 4 hours'", "true", "BOOLEAN"},
		{"CAST(CAST(INTERVAL '-1 year 2 mons -3 days 4 hours' AS VARCHAR) AS INTERVAL)", "-10 mons -3 days +04:00:00", "INTERVAL"},
	})
}

// minNanos is an interval of exactly math.MinInt64 nanoseconds, and
// oneNano one of 1 nanosecond. A literal's time is whole microseconds, so
// they are TIME(9) differences.
const (
	minNanos = "(INTERVAL '-2562047:47:16.854775' - (TIME '00:00:00.000000808' - TIME '00:00:00'))"
	oneNano  = "(TIME '00:00:00.000000001' - TIME '00:00:00')"
)

// The time part of an interval is int64 nanoseconds, about ±2562047 hours.
// Values at the ends of that range work; values beyond it are errors rather
// than saturating or wrapping around.
func TestIntervalRange(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"CAST(INTERVAL '2562047 hours' AS VARCHAR)", "2562047:00:00", "VARCHAR"},
		{"CAST(INTERVAL '-2562047 hours' AS VARCHAR)", "-2562047:00:00", "VARCHAR"},
		{"CAST(INTERVAL 'PT2562047H' AS VARCHAR)", "2562047:00:00", "VARCHAR"},
		{"CAST(INTERVAL '2562047:00:00' AS VARCHAR)", "2562047:00:00", "VARCHAR"},
		{"CAST(INTERVAL 2562047 HOUR AS VARCHAR)", "2562047:00:00", "VARCHAR"},
		// A literal's time is whole microseconds, as in Postgres: the
		// largest in the nanoseconds' range.
		{"CAST(INTERVAL '2562047:47:16.854775' AS VARCHAR)", "2562047:47:16.854775", "VARCHAR"},
		{"CAST(INTERVAL '-2562047:47:16.854775' AS VARCHAR)", "-2562047:47:16.854775", "VARCHAR"},
		{"CAST(INTERVAL '9223372036.854775 seconds' AS VARCHAR)", "2562047:47:16.854775", "VARCHAR"},
		{"CAST(INTERVAL '-9223372036854775 microseconds' AS VARCHAR)", "-2562047:47:16.854775", "VARCHAR"},
		{"CAST(INTERVAL 'PT2562047H47M16.854775S' AS VARCHAR)", "2562047:47:16.854775", "VARCHAR"},
		{"CAST(INTERVAL '2562047:47:16.8547754' AS VARCHAR)", "2562047:47:16.854775", "VARCHAR"},
		{"CAST(INTERVAL '9223372036854775.5 us' AS VARCHAR)", "2562047:47:16.854775", "VARCHAR"},
		// Exactly math.MinInt64 nanoseconds.
		{"CAST(" + minNanos + " AS VARCHAR)", "-2562047:47:16.854775808", "VARCHAR"},
		{"CAST(" + minNanos + " - INTERVAL '-1 second' AS VARCHAR)", "-2562047:47:15.854775808", "VARCHAR"},
		// * and / are in microseconds, as in Postgres. 9223372036854775 µs
		// is past 2^53, so the double of 9223372036854774 is the largest
		// that a product can be.
		{"CAST((INTERVAL '2562047 hours' + INTERVAL '2836.854774 seconds') * 1 AS VARCHAR)", "2562047:47:16.854774", "VARCHAR"},
		{"CAST((INTERVAL '-2562047 hours' - INTERVAL '2836.854774 seconds') / 1 AS VARCHAR)", "-2562047:47:16.854774", "VARCHAR"},
		{"CAST(INTERVAL '2562047 hours' + INTERVAL '-2562047 hours' AS VARCHAR)", "00:00:00", "VARCHAR"},
		{"CAST(INTERVAL '-2562047 hours' * 1 AS VARCHAR)", "-2562047:00:00", "VARCHAR"},
		// Days and months have their own fields.
		{"CAST(INTERVAL '2562047 hours' + INTERVAL '1000000 days' AS VARCHAR)", "1000000 days 2562047:00:00", "VARCHAR"},
		// time ± interval wraps around midnight, whatever the interval's size.
		{"CAST(TIME '23:00:00' + INTERVAL '2562047 hours' AS VARCHAR)", "22:00:00", "VARCHAR"},
		{"CAST(TIME '01:00:00' - " + minNanos + " AS VARCHAR)", "00:47:16.854775", "VARCHAR"},
		{"CAST(TIMESTAMP '2000-01-01 00:00:00' + " + minNanos + " AS VARCHAR)", "1707-09-22 00:12:43.145224", "VARCHAR"},
	})
	for _, expr := range []string{
		"INTERVAL '99999999999 seconds'",
		"INTERVAL '-99999999999 seconds'",
		"INTERVAL '2562048 hours'",
		"INTERVAL '-2562048 hours'",
		"INTERVAL '2562047 hours 0.5 days'", // the fraction of a day cascades into the time
		"INTERVAL 'PT2562048H'",
		"INTERVAL '2562048:00:00'",
		"INTERVAL 2562048 HOUR",
		"INTERVAL 'Infinity' SECOND",
		"INTERVAL 'NaN' SECOND",
		// Literals whose time rounds to a microsecond past the
		// nanoseconds' range (Postgres: 2562047:47:16.854776, and
		// -2562047:47:16.854776 for MinInt64 nanoseconds).
		"INTERVAL '2562047:47:16.854776'",
		"INTERVAL '2562047:47:16.8547755'",
		"INTERVAL '9223372036.854775808 seconds'",
		"INTERVAL '-9223372036.854775808 seconds'",
		"INTERVAL '9223372036854775.6 us'",
		"INTERVAL 'PT2562047H47M16.854776S'",
		"INTERVAL '2562047 hours 2836.854776 seconds'",
		"INTERVAL '2562047 hours' * 2",
		"INTERVAL '1 hour' * 2562048",
		"INTERVAL '2562047 hours' / 0.5",
		minNanos + " * -1",
		minNanos + " / -1",
		// In microseconds, -2^63 ns and 9223372036854775 µs round to
		// doubles past the nanoseconds' range (Postgres:
		// -2562047:47:16.854776 and 2562047:47:16.854776).
		minNanos + " * 1",
		"(INTERVAL '2562047 hours' + INTERVAL '2836.854775 seconds') * 1",
		"INTERVAL '2562047 hours' + INTERVAL '1 hour'",
		"INTERVAL '-2562047 hours' - INTERVAL '1 hour'",
		minNanos + " - " + oneNano,
		"INTERVAL '0 seconds' - " + minNanos,
		"-" + minNanos,
		// As in Postgres, subtracting an interval that can't be negated.
		"TIMESTAMP '2000-01-01 00:00:00' - " + minNanos,
	} {
		// An interval literal is parsed with the statement.
		_, err := ParseScript("SELECT " + expr)
		if err == nil {
			_, _, err = evalTestExpr(t, expr)
		}
		if err == nil || !strings.Contains(err.Error(), "interval out of range") {
			t.Errorf("%s: error %v, want interval out of range", expr, err)
		}
	}
}

// interval * n and interval / n work as Postgres's interval_mul and
// interval_div: each field is multiplied and truncated, a month's fraction
// cascades into days rounded to millionths of a day, a day's into seconds,
// and the time is rounded to microseconds, half to even. Each value is
// Postgres 16's on x86-64. Postgres on arm64 fuses multiply-adds, so three
// of its products differ there in the last microseconds (marked); the
// driver's are the same on both.
func TestIntervalMulDiv(t *testing.T) {
	var cases []scalarCase
	for _, c := range [][2]string{
		{"INTERVAL '1 sec' / 3", "00:00:00.333333"},
		{"INTERVAL '1 mon' / 7", "4 days 06:51:25.6896"}, // 30 / 7 days is 4.285714 days
		{"INTERVAL '1 mon 3 days' / 4", "8 days 06:00:00"},
		{"INTERVAL '1 day' / 3", "08:00:00"},
		{"INTERVAL '1 mon 1 day 1 sec' / 7", "4 days 10:17:08.6896"},
		{"INTERVAL '1 mon' * 0.1", "3 days"},
		{"INTERVAL '13 mons' * 0.1", "1 mon 9 days"},
		{"INTERVAL '1 mon' * (CAST(1 AS DOUBLE PRECISION) / 3)", "10 days"},
		{"INTERVAL '1 mon' / (CAST(1 AS DOUBLE PRECISION) / 3)", "3 mons"},
		{"INTERVAL '1 mon' * 0.034", "1 day 00:28:48"},
		{"INTERVAL '1 year 2 mons 3 days 04:05:06.789' * 0.7", "9 mons 26 days 05:15:34.7523"},
		{"INTERVAL '1 year 2 mons 3 days 04:05:06.789' / 7", "2 mons 10:52:09.541286"},
		{"INTERVAL '1 year 2 mons 3 days 04:05:06.789' * -2.71828", "-3 years -2 mons -9 days -31:04:59.986403"},
		{"INTERVAL '7 mons 13 days 01:02:03.456789' / 0.3", "1 year 11 mons 53 days 11:26:51.52263"},
		{"INTERVAL '5 mons 5 days 5 hours' / (CAST(1 AS DOUBLE PRECISION) / 7)", "2 years 11 mons 35 days 35:00:00"},
		{"INTERVAL '11 mons 29 days' * 1.1", "1 year 34 days 21:36:00"},
		{"3 * INTERVAL '1 mon 1 day 1 sec'", "3 mons 3 days 00:00:03"},
		{"0.3 * INTERVAL '1 year 2 mons 3 days 04:05:06.789'", "4 mons 6 days 22:49:32.0367"},
		// Negative fields and factors. Nothing carries into a larger unit but
		// the whole days of cascaded seconds; the time itself never does.
		{"INTERVAL '-1 mon -1 day' * 0.5", "-15 days -12:00:00"},
		{"INTERVAL '-1 mon 1 day 1 sec' / 3", "-10 days +08:00:00.333333"},
		{"INTERVAL '1 mon -1 day' * 1.5", "1 mon 14 days -12:00:00"},
		{"INTERVAL '29 days 23:59:59.999999' * 0.5", "14 days 24:00:00"},
		{"INTERVAL '1 mon 29 days 23:59:59.999999' / 2", "29 days 24:00:00"},
		// Microseconds, half to even.
		{"INTERVAL '0.000001 sec' / 3", "00:00:00"},
		{"INTERVAL '0.000001 sec' * 0.5", "00:00:00"},
		{"INTERVAL '00:00:00.000003' * 0.5", "00:00:00.000002"},
		{"INTERVAL '00:00:00.000003' / 2", "00:00:00.000002"},
		{"INTERVAL '12345 days 12:34:56.789012' * 0.000001", "00:17:46.653297"},
		{"INTERVAL '12345 days 12:34:56.789012' / 1000000", "00:17:46.653297"},
		{"INTERVAL '1 sec' * CAST('1e-300' AS DOUBLE PRECISION)", "00:00:00"},
		{"INTERVAL '1 day' / CAST('Infinity' AS DOUBLE PRECISION)", "00:00:00"},
		{"INTERVAL '1 day' / CAST('-Infinity' AS DOUBLE PRECISION)", "00:00:00"},
		// The ends of the ranges.
		{"INTERVAL '2147483647 mons' * 0.5", "89478485 years 3 mons 15 days"},
		{"INTERVAL '2147483647 mons' / 2", "89478485 years 3 mons 15 days"},
		{"INTERVAL '2147483647 mons' * 1.0000000001", "178956970 years 7 mons 6 days 10:37:07.7664"}, // the product truncates into range
		{"INTERVAL '-2147483648 mons' * 1", "-178956970 years -8 mons"},
		{"INTERVAL '-2147483648 days' / 2", "-1073741824 days"},
		{"INTERVAL '2147483647 mons 2147483647 days' / 3", "59652323 years 6 mons 715827892 days 08:00:00.089833"},
		{"INTERVAL '2562047 hours' * 1", "2562047:00:00"},
		{"INTERVAL '2562047 hours' / 1.0000000001", "2562046:59:59.077664"},
		{"INTERVAL '-2562047 hours' * 0.9999999999", "-2562046:59:59.077664"},
		{"(INTERVAL '2562047 hours' + INTERVAL '2836.854775 seconds') / 7", "366006:49:36.69354"},
		{"(INTERVAL '-2562047 hours' - INTERVAL '2836.854775 seconds') * -0.5", "1281023:53:38.427388"},
		// Postgres on arm64: …19.950196, 2 days 23:59:59.9136 and
		// 16:48:00.00103.
		{"INTERVAL '7 mons 13 days 01:02:03.456789' * 123456.789", "72016 years 5 mons 1604953 days 127713:17:19.95019"},
		{"INTERVAL '2147483647 mons' * 0.3", "53687091 years 2 mons 3 days 00:00:00.0864"},
		{"INTERVAL '2147483647 days' * 0.1", "214748364 days 16:48:00.001545"},
	} {
		cases = append(cases, scalarCase{"CAST(" + c[0] + " AS VARCHAR)", c[1], "VARCHAR"})
	}
	runScalarCases(t, cases)

	for expr, want := range map[string]string{
		// A field past its 32 bits, also when whole days of seconds or of a
		// month carry into the days.
		"INTERVAL '-2147483648 mons' * -1":                          "interval out of range",
		"INTERVAL '-2147483648 mons' / -1":                          "interval out of range",
		"INTERVAL '-2147483648 days' * -1":                          "interval out of range",
		"INTERVAL '2147483647 mons' / 0.5":                          "interval out of range",
		"INTERVAL '2147483647 days' * 1.5":                          "interval out of range",
		"INTERVAL '1 mon' / CAST('1e-10' AS DOUBLE PRECISION)":      "interval out of range",
		"INTERVAL '2147483647 mons 2147483647 days' * 0.9999999999": "interval out of range",
		"INTERVAL '2147483647 mons 2147483647 days' / 1.0000000001": "interval out of range",
		"INTERVAL '1 day' * CAST('NaN' AS DOUBLE PRECISION)":        "interval out of range",
		"INTERVAL '1 day' / CAST('NaN' AS DOUBLE PRECISION)":        "interval out of range",
		"INTERVAL '0' * CAST('Infinity' AS DOUBLE PRECISION)":       "interval out of range",
		"INTERVAL '1 day' * CAST('Infinity' AS DOUBLE PRECISION)":   "interval out of range",
		"INTERVAL '1 day' / 0":                                      "division by zero",
		"INTERVAL '1 day' / 0.0":                                    "division by zero",
		"INTERVAL '1 day' / CAST('-0' AS DOUBLE PRECISION)":         "division by zero",
		"INTERVAL '0' / 0":                                          "division by zero",
		// The time past the nanoseconds' range (Postgres: 5124094:00:00).
		"INTERVAL '2562047 hours' * 2":   "interval out of range",
		"INTERVAL '2562047 hours' / 0.5": "interval out of range",
	} {
		state := stateDatetimeRange
		if want == "division by zero" {
			state = stateDivisionByZero
		}
		if _, _, err := evalTestExpr(t, expr); err == nil || err.Error() != want || sqlState(err) != state {
			t.Errorf("%s: error %v (SQLSTATE %q), want %s (%s)", expr, err, sqlState(err), want, state)
		}
	}
}

// A time that isn't whole microseconds (a bound Arrow value) counts with
// its fraction in * and /, and the result is whole microseconds, half to
// even; Postgres has no such times.
func TestIntervalMulDivNanos(t *testing.T) {
	iv := func(months, days int32, ns int64) Value {
		return Value{T: typeInterval, Months: months, Days: days, I: ns}
	}
	for _, c := range []struct {
		v    Value
		f    float64
		div  bool
		want string
	}{
		{iv(0, 0, 1500), 1, false, "00:00:00.000002"},
		{iv(0, 0, 2500), 1, false, "00:00:00.000002"},
		{iv(0, 0, 2501), 1, false, "00:00:00.000003"},
		{iv(0, 0, -1500), 1, false, "-00:00:00.000002"},
		{iv(0, 0, 1), 1, false, "00:00:00"},
		{iv(0, 0, 7500), 3, true, "00:00:00.000002"}, // 2.5 µs
		{iv(0, 0, 7501), 3, true, "00:00:00.000003"},
		{iv(1, 1, 1_000_000_001), 7, true, "4 days 10:17:08.6896"},
		{iv(0, 0, math.MinInt64), 0.5, false, "-1281023:53:38.427388"},
		{iv(0, 0, math.MaxInt64), 2, true, "1281023:53:38.427388"},
		{iv(0, 0, math.MaxInt64), 1, false, "interval out of range"}, // 9223372036854775.807 µs
		{iv(0, 0, math.MinInt64), 1, true, "interval out of range"},
	} {
		var v Value
		var err error
		if c.div {
			v, err = divideInterval(c.v.Months, c.v.Days, intervalMicros(c.v), c.f)
		} else {
			v, err = multiplyInterval(c.v, c.f)
		}
		got := ""
		if err != nil {
			got = err.Error()
		} else {
			got = formatInterval(v)
		}
		if got != c.want {
			t.Errorf("%s (%d ns) × %v (div %v) = %s, want %s", formatInterval(c.v), c.v.I, c.f, c.div, got, c.want)
		}
	}
}

// An Arrow duration converts to an interval's nanoseconds, and one too long
// for them is an error.
func TestIntervalFromArrowDuration(t *testing.T) {
	b := array.NewDurationBuilder(memory.NewGoAllocator(), &arrow.DurationType{Unit: arrow.Second})
	defer b.Release()
	b.AppendValues([]arrow.Duration{-9223372036, 9223372036, 9223372037, -9223372037}, nil)
	arr := b.NewArray()
	defer arr.Release()
	for i, want := range []string{"-2562047:47:16", "2562047:47:16", "", ""} {
		v, err := valueAt(arr, i)
		switch {
		case want == "" && (err == nil || err.Error() != "interval out of range"):
			t.Errorf("duration %v: %s, error %v, want interval out of range", arr.(*array.Duration).Value(i), v.Text(), err)
		case want != "" && err != nil:
			t.Errorf("duration %v: %v", arr.(*array.Duration).Value(i), err)
		case want != "" && v.Text() != want:
			t.Errorf("duration %v = %s, want %s", arr.(*array.Duration).Value(i), v.Text(), want)
		}
	}
}

// mustInterval parses an interval literal.
func mustInterval(t *testing.T, s string) Value {
	t.Helper()
	v, err := intervalLiteral(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// intervalSum adds interval literals, and the zeros after them.
func intervalSum(t *testing.T, zeros int, lits ...string) *exactSum {
	t.Helper()
	s := &exactSum{}
	for _, l := range lits {
		s.add(mustInterval(t, l))
	}
	for range zeros {
		s.add(mustInterval(t, "0"))
	}
	return s
}

// SUM of intervals adds months, days and time on their own, as Postgres
// does, and fails only if the total is out of range, whatever the order.
func TestIntervalSum(t *testing.T) {
	sumText := func(s *exactSum) string {
		v, err := s.sumValue(typeInterval)
		if err != nil {
			return err.Error()
		}
		return formatInterval(v)
	}
	for _, c := range []struct {
		lits []string
		want string
	}{
		// Not justified: 30 days stay days, 25 hours stay hours.
		{[]string{"1 mon", "30 days", "1 day 02:00:00"}, "1 mon 31 days 02:00:00"},
		{[]string{"-1 year 3 days -00:00:01.5", "25 hours"}, "-1 years +3 days 24:59:58.5"},
		{[]string{"23:00:00", "02:00:00", "-1 day"}, "-1 days +25:00:00"},
		// Through running totals out of range, in any order (Postgres fails
		// when it adds them in an order that overflows).
		{[]string{"2147483647 mons", "1 mon", "-1 mon"}, "178956970 years 7 mons"},
		{[]string{"-1 mon", "2147483647 mons", "1 mon"}, "178956970 years 7 mons"},
		{[]string{"2147483647 days", "2147483647 days", "-2147483647 days"}, "2147483647 days"},
		{[]string{"2562047 hours", "2562047 hours", "-2562047 hours"}, "2562047:00:00"},
		// Totals out of range.
		{[]string{"2147483647 mons", "1 mon"}, "interval out of range"},
		{[]string{"-2147483647 days", "-2 days"}, "interval out of range"},
		{[]string{"2000000 hours", "2000000 hours"}, "interval out of range"},
	} {
		if got := sumText(intervalSum(t, 0, c.lits...)); got != c.want {
			t.Errorf("SUM(%q) = %s, want %s", c.lits, got, c.want)
		}
	}
	// Exactly MinInt64 nanoseconds, the most negative time part.
	s := &exactSum{}
	s.add(Value{T: typeInterval, I: -math.MaxInt64})
	s.add(Value{T: typeInterval, I: -1})
	if got := sumText(s); got != "-2562047:47:16.854775808" {
		t.Errorf("SUM to MinInt64 nanoseconds = %s", got)
	}
	s.add(Value{T: typeInterval, I: -1})
	if got := sumText(s); got != "interval out of range" {
		t.Errorf("SUM past MinInt64 nanoseconds = %s", got)
	}

	// A sliding frame removes values exactly.
	rng := rand.New(rand.NewPCG(9, 10))
	vals := make([]Value, 300)
	for i := range vals {
		ns := rng.Int64N(nsPerDay) - nsPerDay/2
		if rng.IntN(5) == 0 {
			ns = math.MaxInt64 - rng.Int64N(nsPerDay) // overflows when two are added
		}
		vals[i] = Value{T: typeInterval, Months: int32(rng.IntN(25) - 12), Days: int32(rng.IntN(61) - 30), I: ns}
	}
	for _, width := range []int{1, 3, 20} {
		s := &exactSum{}
		for i, v := range vals {
			s.add(v)
			if i >= width {
				s.remove(vals[i-width])
			}
			fresh := &exactSum{}
			for _, w := range vals[max(0, i-width+1) : i+1] {
				fresh.add(w)
			}
			a, aerr := s.sumValue(typeInterval)
			b, berr := fresh.sumValue(typeInterval)
			am, amerr := s.meanValue(typeInterval)
			bm, bmerr := fresh.meanValue(typeInterval)
			if a != b || (aerr == nil) != (berr == nil) || am != bm || (amerr == nil) != (bmerr == nil) {
				t.Fatalf("width %d, row %d: sliding sum %v (%v) / mean %v (%v), fresh %v (%v) / %v (%v)",
					width, i, a, aerr, am, amerr, b, berr, bm, bmerr)
			}
		}
	}
}

// wideInt adds and subtracts int64s in 128 bits.
func TestWideInt(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	var w wideInt
	ref := new(big.Int)
	for i := range 2000 {
		v := int64(rng.Uint64())
		switch i % 7 {
		case 0:
			v = math.MaxInt64
		case 1:
			v = math.MinInt64
		}
		sub := rng.IntN(3) == 0
		w.add(v, sub)
		if sub {
			ref.Sub(ref, big.NewInt(v))
		} else {
			ref.Add(ref, big.NewInt(v))
		}
		got := new(big.Int).Lsh(big.NewInt(w.hi), 64)
		got.Add(got, new(big.Int).SetUint64(w.lo))
		if got.Cmp(ref) != 0 {
			t.Fatalf("step %d: %s, want %s", i, got, ref)
		}
		n, ok := w.int64()
		if ok != ref.IsInt64() || (ok && n != ref.Int64()) {
			t.Fatalf("step %d: int64() = %d, %v for %s", i, n, ok, ref)
		}
		// The microseconds, within a unit in the last place (exactly when they
		// are whole, as Postgres's are).
		us, ok := w.micros()
		want, _ := new(big.Rat).SetFrac(ref, big.NewInt(1000)).Float64()
		q := new(big.Int).Quo(ref, big.NewInt(1000))
		if ok != q.IsInt64() || (ok && math.Abs(us-want) > math.Abs(want)*0x1p-52) {
			t.Fatalf("step %d: micros() = %v, %v for %s", i, us, ok, ref)
		}
	}
	// Whole microseconds are converted as Postgres converts its int64, also
	// beyond 64 bits of nanoseconds.
	for _, us := range []int64{1<<52 + 1, -(1<<52 + 1), math.MaxInt64 / 1000, 123456789} {
		var w wideInt
		w.add(us*1000, false)
		w.add(us*1000, false)
		if got, ok := w.micros(); !ok || got != float64(2*us) {
			t.Errorf("micros() of 2 × %d000 = %v, %v", us, got, ok)
		}
	}
}

// AVG of intervals is Postgres's: the sum divided by the count, with the
// fractions of months and days cascading down and the result rounded to
// microseconds. The expected values are Postgres 16's.
func TestIntervalMean(t *testing.T) {
	for _, c := range []struct {
		lits  []string
		zeros int
		want  string
	}{
		{[]string{"1 mon", "30 days", "1 day 02:00:00"}, 0, "20 days 08:40:00"},
		{[]string{"-1 year 3 days -00:00:01.5", "25 hours"}, 0, "-6 mons +1 day 24:29:59.25"},
		{[]string{"1 mon", "30 days", "1 day 02:00:00", "1 sec", "-1 year 3 days -00:00:01.5", "25 hours"}, 2,
			"-1 mons -7 days +03:22:29.9375"},
		{[]string{"1 sec"}, 2, "00:00:00.333333"},
		{[]string{"1 mon"}, 1, "15 days"},
		{[]string{"1 mon"}, 2, "10 days"},
		{[]string{"-1 mon"}, 1, "-15 days"},
		// The fraction of a month is rounded to microseconds of a day before
		// it cascades: 30 / 7 days is 4.285714 days.
		{[]string{"1 mon"}, 6, "4 days 06:51:25.6896"},
		{[]string{"1 mon 3 days"}, 3, "8 days 06:00:00"}, // 1.25 days of seconds cascade into a day
		{[]string{"5 mons"}, 2, "1 mon 20 days"},
		{[]string{"-5 mons -1 day"}, 2, "-1 mons -20 days -08:00:00"},
		{[]string{"1 year 1 mon 1 day 00:00:01"}, 2, "4 mons 10 days 08:00:00.333333"},
		{[]string{"1 mon 1 day 1 sec"}, 10, "2 days 19:38:11.023564"},
		{[]string{"7 mons 23 days 22:59:59.999999"}, 2, "2 mons 17 days 23:40:00"},
		{[]string{"7 mons 23 days 22:59:59.999999"}, 6, "1 mon 3 days 10:08:34.285714"},
		// Microseconds round half to even.
		{[]string{"3 microseconds"}, 1, "00:00:00.000002"},
		{[]string{"1 microsecond"}, 1, "00:00:00"},
		{[]string{"-3 microseconds"}, 1, "-00:00:00.000002"},
		{[]string{"1 day 00:00:00.000003"}, 1, "12:00:00.000002"},
		{[]string{"1 day", "1 day", "1 day 00:00:00.000001"}, 0, "1 day"},
		{[]string{"29 days 23:59:59.999999"}, 1, "14 days 24:00:00"},
		// The sum's time is beyond the nanoseconds of an interval, but not
		// beyond Postgres's microseconds.
		{[]string{"2000000 hours", "2000000 hours"}, 0, "2000000:00:00"},
		// As in Postgres, the sum must be an interval.
		{[]string{"2147483647 mons", "1 mon"}, 0, "interval out of range"},
		{[]string{"-2147483647 days", "-2 days"}, 0, "interval out of range"},
	} {
		got := ""
		if v, err := intervalSum(t, c.zeros, c.lits...).meanValue(typeInterval); err != nil {
			got = err.Error()
		} else {
			got = formatInterval(v)
		}
		if got != c.want {
			t.Errorf("AVG(%q and %d zeros) = %s, want %s", c.lits, c.zeros, got, c.want)
		}
	}
}

// Of intervals that compare equal, MIN and MAX return the last one, as
// Postgres's interval_smaller and interval_larger do.
func TestIntervalMinMaxTies(t *testing.T) {
	for _, name := range []string{"MIN", "MAX"} {
		for _, lits := range [][]string{{"1 mon", "30 days"}, {"30 days", "1 mon"}, {"1 day", "24 hours", "1 day"}} {
			a := newAccumulator(&Func{Name: name, Args: []Expr{&ColumnRef{Name: "iv"}}})
			for _, l := range lits {
				a.add(mustInterval(t, l))
			}
			v, err := a.result(nil, typeInterval)
			if want := mustInterval(t, lits[len(lits)-1]); err != nil || v != want {
				t.Errorf("%s(%q) = %s, %v; want %s", name, lits, formatInterval(v), err, formatInterval(want))
			}
		}
	}
}
