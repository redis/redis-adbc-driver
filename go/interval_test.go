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

// Unit tests for rendering intervals as text, for the interval range and
// for the aggregates of intervals. They need no Redis server.

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
		{"CAST(INTERVAL '-9223372036.854775808 seconds' AS VARCHAR)", "-2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-1 day -9223372036.854775808 seconds' AS VARCHAR)", "-1 days -2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-1 day 2 hours' AS VARCHAR)", "-1 days +02:00:00", "VARCHAR"},
		// The text reads back as the same interval.
		{"CAST(CAST(INTERVAL '-1 year 2 mons -3 days 4 hours' AS VARCHAR) AS INTERVAL) = INTERVAL '-1 year 2 mons -3 days 4 hours'", "true", "BOOLEAN"},
		{"CAST(CAST(INTERVAL '-1 year 2 mons -3 days 4 hours' AS VARCHAR) AS INTERVAL)", "-10 mons -3 days +04:00:00", "INTERVAL"},
	})
}

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
		// Exactly math.MinInt64 nanoseconds.
		{"CAST(INTERVAL '-9223372036.854775808 seconds' AS VARCHAR)", "-2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-9223372036.854775808 seconds' * 1 AS VARCHAR)", "-2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-9223372036.854775808 seconds' - INTERVAL '-1 second' AS VARCHAR)", "-2562047:47:15.854775808", "VARCHAR"},
		{"CAST(INTERVAL '-2562047 hours' - INTERVAL '2836.854775808 seconds' AS VARCHAR)", "-2562047:47:16.854775808", "VARCHAR"},
		{"CAST(INTERVAL '2562047 hours' + INTERVAL '-2562047 hours' AS VARCHAR)", "00:00:00", "VARCHAR"},
		{"CAST(INTERVAL '-2562047 hours' * 1 AS VARCHAR)", "-2562047:00:00", "VARCHAR"},
		// Days and months have their own fields.
		{"CAST(INTERVAL '2562047 hours' + INTERVAL '1000000 days' AS VARCHAR)", "1000000 days 2562047:00:00", "VARCHAR"},
		// time ± interval wraps around midnight, whatever the interval's size.
		{"CAST(TIME '23:00:00' + INTERVAL '2562047 hours' AS VARCHAR)", "22:00:00", "VARCHAR"},
		{"CAST(TIME '01:00:00' - INTERVAL '-9223372036.854775808 seconds' AS VARCHAR)", "00:47:16.854775", "VARCHAR"},
		{"CAST(TIMESTAMP '2000-01-01 00:00:00' + INTERVAL '-9223372036.854775808 seconds' AS VARCHAR)", "1707-09-22 00:12:43.145224", "VARCHAR"},
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
		// 2^63 nanoseconds: float64(math.MaxInt64) rounds up to this.
		"INTERVAL '9223372036.854775808 seconds'",
		"INTERVAL '2562047 hours' * 2",
		"INTERVAL '1 hour' * 2562048",
		"INTERVAL '2562047 hours' / 0.5",
		"INTERVAL '-9223372036.854775808 seconds' * -1",
		"INTERVAL '-9223372036.854775808 seconds' / -1",
		"INTERVAL '2562047 hours' + INTERVAL '1 hour'",
		"INTERVAL '-2562047 hours' - INTERVAL '1 hour'",
		"INTERVAL '-9223372036.854775808 seconds' + INTERVAL '-0.000000001 seconds'",
		"INTERVAL '0 seconds' - INTERVAL '-9223372036.854775808 seconds'",
		"-INTERVAL '-9223372036.854775808 seconds'",
		// As in Postgres, subtracting an interval that can't be negated.
		"TIMESTAMP '2000-01-01 00:00:00' - INTERVAL '-9223372036.854775808 seconds'",
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
	s.add(mustInterval(t, "-0.000000001 seconds"))
	if got := sumText(s); got != "-2562047:47:16.854775808" {
		t.Errorf("SUM to MinInt64 nanoseconds = %s", got)
	}
	s.add(mustInterval(t, "-0.000000001 seconds"))
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
