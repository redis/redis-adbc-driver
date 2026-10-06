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

// Intervals and date/time arithmetic.
//
// An interval is (months, days, nanoseconds), like Postgres and Arrow's
// month-day-nano interval type: months and days are calendar units whose
// length depends on the date they are added to. Adding an interval applies
// months (clamping to the end of the month: Jan 31 + 1 month = Feb 29 in a
// leap year), then days, then the time part.
//
// Comparisons and equality (ORDER BY, GROUP BY, DISTINCT, joins) treat a
// month as 30 days and a day as 24 hours, as Postgres does, so
// INTERVAL '1 day' = INTERVAL '24 hours'.

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

const (
	nsPerSecond = int64(time.Second)
	nsPerMinute = 60 * nsPerSecond
	nsPerHour   = 60 * nsPerMinute
	nsPerDay    = 24 * nsPerHour
)

// intervalUnit says how one unit of a field maps onto the interval parts.
type intervalUnit struct {
	months float64
	days   float64
	nanos  float64
}

var intervalUnits = map[string]intervalUnit{}

func init() {
	add := func(u intervalUnit, names ...string) {
		for _, n := range names {
			intervalUnits[n] = u
		}
	}
	add(intervalUnit{months: 12000}, "millennium", "millennia", "millenniums")
	add(intervalUnit{months: 1200}, "century", "centuries")
	add(intervalUnit{months: 120}, "decade", "decades")
	add(intervalUnit{months: 12}, "year", "years", "y", "yr", "yrs")
	add(intervalUnit{months: 3}, "quarter", "quarters")
	add(intervalUnit{months: 1}, "month", "months", "mon", "mons")
	add(intervalUnit{days: 7}, "week", "weeks", "w")
	add(intervalUnit{days: 1}, "day", "days", "d")
	add(intervalUnit{nanos: float64(nsPerHour)}, "hour", "hours", "h", "hr", "hrs")
	add(intervalUnit{nanos: float64(nsPerMinute)}, "minute", "minutes", "m", "min", "mins")
	add(intervalUnit{nanos: float64(nsPerSecond)}, "second", "seconds", "s", "sec", "secs")
	add(intervalUnit{nanos: 1e6}, "millisecond", "milliseconds", "ms", "msec", "msecs")
	add(intervalUnit{nanos: 1e3}, "microsecond", "microseconds", "us", "usec", "usecs")
}

func intervalValue(months, days int64, nanos int64) (Value, error) {
	if months > math.MaxInt32 || months < math.MinInt32 || days > math.MaxInt32 || days < math.MinInt32 {
		return Value{}, stateErr(stateDatetimeRange, "interval out of range")
	}
	return Value{T: typeInterval, Months: int32(months), Days: int32(days), I: nanos}, nil
}

// An interval literal is read as Postgres 16 reads it (ParseDateTime,
// DecodeInterval and DecodeISO8601Interval in
// src/backend/utils/adt/datetime.c, and interval_in and
// AdjustIntervalForTypmod in timestamp.c), into an itmIn: each field's
// integer part is exact, and its fraction cascades into the smaller units
// as Postgres's does (a month is 30 days, a week 7 days and a day 24
// hours, and a fraction of a year is whole months), rounded to the
// microsecond. So a literal's time is whole microseconds, as Postgres's
// is, though an interval holds nanoseconds.

const (
	usPerSecond = int64(1_000_000)
	usPerMinute = 60 * usPerSecond
	usPerHour   = 60 * usPerMinute
	usPerDay    = 24 * usPerHour
)

// itmIn is an interval literal's fields as they are read (Postgres's
// struct pg_itm_in). Its adjust methods are Postgres's Adjust* functions:
// they are false if a field overflows.
type itmIn struct {
	usec      int64
	mday      int32
	mon, year int32
}

// literalErr is why a literal can't be read, as Postgres's DTERR codes: a
// field it can't read (DTERR_BAD_FORMAT), a field outside its range or
// one that overflows its part of the interval (DTERR_FIELD_OVERFLOW, such
// as 75 minutes or 2^31 days), or a count of INTERVAL n UNIT past the
// range of an interval.
type literalErr int

const (
	litBadFormat literalErr = iota + 1
	litFieldRange
	litOutOfRange
)

// error is e's error for literal s, with Postgres's text, which quotes s
// as it is.
func (e literalErr) error(s string) error {
	switch e {
	case litBadFormat:
		return stateErr(stateDatetimeFormat, `invalid input syntax for type interval: "%s"`, s)
	case litFieldRange:
		return stateErr(stateIntervalField, `interval field value out of range: "%s"`, s)
	}
	return stateErr(stateDatetimeRange, "interval out of range")
}

// mulAddInt64 adds val × m to *sum, and is false if that overflows
// (int64_multiply_add).
func mulAddInt64(val, m int64, sum *int64) bool {
	p := val * m
	if val != 0 && (p/val != m || (val == -1 && m == math.MinInt64)) {
		return false
	}
	var overflow bool
	*sum, overflow = addOrSub("+", *sum, p)
	return !overflow
}

// addScaled32 adds val × scale to a 32-bit field, and is false if val, the
// product or the sum doesn't fit in 32 bits (pg_mul_s32_overflow and
// pg_add_s32_overflow).
func addScaled32(field *int32, val, scale int64) bool {
	if val < math.MinInt32 || val > math.MaxInt32 {
		return false
	}
	p := val * scale
	s := int64(*field) + p
	if p < math.MinInt32 || p > math.MaxInt32 || s < math.MinInt32 || s > math.MaxInt32 {
		return false
	}
	*field = int32(s)
	return true
}

// addMicros adds us microseconds, and is false if that overflows.
func (it *itmIn) addMicros(us int64) bool { return mulAddInt64(us, 1, &it.usec) }

// adjustFractMicroseconds adds frac × scale microseconds, rounding off the
// fraction of a microsecond: half of one rounds toward zero.
func (it *itmIn) adjustFractMicroseconds(frac float64, scale int64) bool {
	if frac == 0 {
		return true
	}
	// The conversion keeps the product from being fused into an FMA.
	frac = float64(frac * float64(scale))
	us := int64(frac)
	frac -= float64(us)
	if frac > 0.5 {
		us++
	} else if frac < -0.5 {
		us--
	}
	return it.addMicros(us)
}

// adjustFractDays adds frac × scale days: the whole days, and the rest as
// microseconds.
func (it *itmIn) adjustFractDays(frac float64, scale int64) bool {
	if frac == 0 {
		return true
	}
	frac = float64(frac * float64(scale))
	extra := int64(frac)
	return addScaled32(&it.mday, extra, 1) && it.adjustFractMicroseconds(frac-float64(extra), usPerDay)
}

// adjustFractYears adds frac × scale years as whole months, rounded half to
// even.
func (it *itmIn) adjustFractYears(frac float64, scale int64) bool {
	return addScaled32(&it.mon, int64(math.RoundToEven(frac*float64(scale)*12)), 1)
}

// adjustMicroseconds adds (val + fval) × scale microseconds.
func (it *itmIn) adjustMicroseconds(val int64, fval float64, scale int64) bool {
	return mulAddInt64(val, scale, &it.usec) && it.adjustFractMicroseconds(fval, scale)
}

func (it *itmIn) adjustDays(val, scale int64) bool  { return addScaled32(&it.mday, val, scale) }
func (it *itmIn) adjustMonths(val int64) bool       { return addScaled32(&it.mon, val, 1) }
func (it *itmIn) adjustYears(val, scale int64) bool { return addScaled32(&it.year, val, scale) }

// addUnit adds val + fval of unit u, n of INTERVAL n UNIT, as
// DecodeInterval adds a number and its unit (addDecoded).
func (it *itmIn) addUnit(val int64, fval float64, u intervalUnit) bool {
	switch {
	case u.nanos != 0:
		return it.adjustMicroseconds(val, fval, int64(u.nanos)/1000)
	case u.days == 1:
		return it.adjustDays(val, 1) && it.adjustFractMicroseconds(fval, usPerDay)
	case u.days != 0:
		return it.adjustDays(val, int64(u.days)) && it.adjustFractDays(fval, int64(u.days))
	case u.months == 1:
		return it.adjustMonths(val) && it.adjustFractDays(fval, 30)
	case u.months == 3:
		// Postgres has no quarters. A quarter is 3 months, and a fraction
		// of one 3 times that fraction of a month.
		if val < math.MinInt32 || val > math.MaxInt32 {
			return false
		}
		m := fval * 3
		whole := math.Trunc(m)
		return it.adjustMonths(val*3+int64(whole)) && it.adjustFractDays(m-whole, 30)
	}
	years := int64(u.months) / 12
	return it.adjustYears(val, years) && it.adjustFractYears(fval, years)
}

// negate negates every field, as "ago" does, and is false if one can't be
// negated.
func (it *itmIn) negate() bool {
	if it.usec == math.MinInt64 || it.mday == math.MinInt32 || it.mon == math.MinInt32 || it.year == math.MinInt32 {
		return false
	}
	it.usec, it.mday, it.mon, it.year = -it.usec, -it.mday, -it.mon, -it.year
	return true
}

// value is the interval read. Its years and months together must fit in
// 32 bits, as in Postgres (itmin2interval), and its time in the driver's
// int64 nanoseconds.
func (it itmIn) value() (Value, error) { return it.typedValue(fullTypmod) }

// typedValue is the interval read, of typmod tm: without the fields tm
// doesn't have (intervalTypmod.adjust), which are dropped before its time
// must fit in nanoseconds, as Postgres drops them from its microseconds.
func (it itmIn) typedValue(tm intervalTypmod) (Value, error) {
	months := int64(it.year)*12 + int64(it.mon)
	if months > math.MaxInt32 || months < math.MinInt32 {
		return Value{}, stateErr(stateDatetimeRange, "interval out of range")
	}
	months, days, us := tm.adjust(months, it.mday, it.usec)
	if us > math.MaxInt64/1000 || us < math.MinInt64/1000 {
		return Value{}, stateErr(stateDatetimeRange, "interval out of range")
	}
	return intervalValue(months, int64(days), us*1000)
}

// intervalLiteral reads s as Postgres's interval_in reads an interval
// without a typmod: '1 year 2 months 3 days', '1.5 hours', '-2 weeks',
// '04:05:06.5', '3 04:05:06', '1-2' (years-months), '2 days ago', and ISO
// 8601 'P1Y2M3DT4H5M6S' and 'P0001-02-03T04:05:06'.
func intervalLiteral(s string) (Value, error) { return typedIntervalLiteral(s, fullTypmod) }

// typedIntervalLiteral reads s as interval_in reads an interval of typmod
// tm: as fields (parseIntervalFields and decodeIntervalFields), or if it
// can't read them, as ISO 8601 (isoInterval); then it drops the fields tm
// doesn't have (intervalTypmod.adjust).
func typedIntervalLiteral(s string, tm intervalTypmod) (Value, error) {
	it, e := itmIn{}, litBadFormat
	if fields, ok := parseIntervalFields(s); ok {
		it, e = decodeIntervalFields(fields, tm.fields)
	}
	if e == litBadFormat {
		it, e = isoInterval(s)
	}
	if e != 0 {
		return Value{}, e.error(s)
	}
	return it.typedValue(tm)
}

// The fields of an interval typmod (Postgres's INTERVAL_MASK bits).
const (
	ivYear = 1 << iota
	ivMonth
	ivDay
	ivHour
	ivMinute
	ivSecond
)

// typmodFields are the fields that an interval typmod names.
var typmodFields = map[string]int{
	"year": ivYear, "month": ivMonth, "day": ivDay, "hour": ivHour, "minute": ivMinute, "second": ivSecond,
}

// intervalTypmod is an interval's typmod: the fields of INTERVAL '…' DAY TO
// MINUTE (a mask of ivYear … ivSecond, or 0 for all of them), and the
// digits of a second of INTERVAL '…' SECOND(p) or INTERVAL(p) '…' (0 to 6,
// or -1 for all of them).
type intervalTypmod struct {
	fields    int
	precision int
}

var fullTypmod = intervalTypmod{precision: -1}

// adjust drops the fields of an interval of months, days and microseconds
// to the right of tm's last one, as Postgres's AdjustIntervalForTypmod
// does: the time is truncated toward zero, so INTERVAL '1 day 02:30' HOUR
// is 1 day 02:00:00, and INTERVAL '1 year 14 months' YEAR is 2 years. Then
// it rounds the time to tm's digits of a second, half away from zero.
func (tm intervalTypmod) adjust(months int64, days int32, us int64) (int64, int32, int64) {
	switch tm.fields {
	case ivYear:
		months, days, us = months/12*12, 0, 0
	case ivMonth, ivYear | ivMonth:
		days, us = 0, 0
	case ivDay:
		us = 0
	case ivHour, ivDay | ivHour:
		us = us / usPerHour * usPerHour
	case ivMinute, ivHour | ivMinute, ivDay | ivHour | ivMinute:
		us = us / usPerMinute * usPerMinute
	}
	// A time past the nanoseconds' range stays past it.
	if tm.precision >= 0 && tm.precision < 6 && us <= math.MaxInt64/1000 && us >= math.MinInt64/1000 {
		scale := int64(1)
		for p := tm.precision; p < 6; p++ {
			scale *= 10
		}
		if us >= 0 {
			us = (us + scale/2) / scale * scale
		} else {
			us = -((-us + scale/2) / scale * scale)
		}
	}
	return months, days, us
}

// dtField is a field of an interval literal as Postgres's ParseDateTime
// splits it: its text, lower-cased, and what it looks like.
type dtField struct {
	s   string
	typ dtFieldType
}

type dtFieldType int

const (
	dtNumber  dtFieldType = iota // digits, with a decimal point or not: "1", "1.5", ".5"
	dtDate                       // digits and delimiters, or text: "1-2", "1.seconds", "day2"
	dtTime                       // digits and colons: "04:05:06.5"
	dtString                     // letters: "days"
	dtSpecial                    // a sign and letters: "-days"
	dtTZ                         // a sign and digits: "-1", "+02:03", "-1-2"
)

const (
	maxDateFields = 25  // MAXDATEFIELDS
	dateWorkBuf   = 256 // interval_in's workbuf
	tokMaxLen     = 10  // TOKMAXLEN
)

// tokenKey is a word as Postgres looks it up in its tables, which compare
// its first 10 characters: '1 microseconds' is 1 microsecon.
func tokenKey(s string) string {
	if len(s) > tokMaxLen {
		return s[:tokMaxLen]
	}
	return s
}

// dateKeywords are the words of Postgres's datetktbl, the date and time
// keywords. Before a digit or "+", ParseDateTime ends a word there only if
// it is one of them, and otherwise reads on, as a date or a time zone
// name: '1d2h' is 1 day 2 hours, but '1day2hours' an error.
var dateKeywords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`ad allballs am apr april at aug august bc d dec december dow doy dst
		epoch feb february fri friday h infinity isodow isoyear j jan january jd jul julian july jun june
		m mar march may mm mon monday nov november now oct october on pm s sat saturday sep sept september
		sun sunday t thu thur thurs thursday today tomorrow tue tues tuesday wed wednesday weds y yesterday`) {
		m[w] = true
	}
	return m
}()

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// lowerASCII lower-cases the ASCII letters of s, as pg_tolower does.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// parseIntervalFields splits s into fields as ParseDateTime does, and is false
// where it fails (DTERR_BAD_FORMAT): at a character it doesn't read (one
// that isn't ASCII, or a sign before neither a digit nor a letter), past
// 25 fields, or past the 256 bytes of interval_in's buffer, which holds
// each field and a NUL. Spaces separate fields, and so does other
// punctuation, which is dropped ('@ 1 day, 2 hours').
func parseIntervalFields(s string) ([]dtField, bool) {
	var fields []dtField
	used := 0 // of the buffer
	for i := 0; i < len(s); {
		c := s[i]
		if isCSpace(c) {
			i++
			continue
		}
		if len(fields) == maxDateFields {
			return nil, false
		}
		start, typ := i, dtNumber
		var text string
		switch {
		case isDigit(c):
			i = digitsEnd(s, i)
			switch {
			case i == len(s):
			case s[i] == ':':
				typ = dtTime
				for i++; i < len(s) && (isDigit(s[i]) || s[i] == ':' || s[i] == '.'); i++ {
				}
			case s[i] == '-' || s[i] == '/' || s[i] == '.':
				delim := s[i]
				i++
				if i < len(s) && isDigit(s[i]) {
					if delim != '.' {
						typ = dtDate
					}
					i = digitsEnd(s, i)
					if i < len(s) && s[i] == delim {
						// Three fields, with the same delimiter.
						typ = dtDate
						for i++; i < len(s) && (isDigit(s[i]) || s[i] == delim); i++ {
						}
					}
				} else {
					// Text after the delimiter.
					typ = dtDate
					for ; i < len(s) && (isDigit(s[i]) || isAlpha(s[i]) || s[i] == delim); i++ {
					}
				}
			}
			text = s[start:i]
		case c == '.':
			i = digitsEnd(s, i+1)
			text = s[start:i]
		case isAlpha(c):
			typ = dtString
			for i++; i < len(s) && isAlpha(s[i]); i++ {
			}
			if i < len(s) && (s[i] == '-' || s[i] == '/' || s[i] == '.' ||
				(s[i] == '+' || isDigit(s[i])) && !dateKeywords[tokenKey(lowerASCII(s[start:i]))]) {
				// A date or a time zone name.
				typ = dtDate
				for ; i < len(s) && (isDigit(s[i]) || isAlpha(s[i]) || strings.IndexByte("+-/_.:", s[i]) >= 0); i++ {
				}
			}
			text = s[start:i]
		case c == '+' || c == '-':
			// The sign, and after any spaces, digits (a number, a clock or
			// years-months: a "time zone") or letters.
			for i++; i < len(s) && isCSpace(s[i]); i++ {
			}
			j := i
			switch {
			case i < len(s) && isDigit(s[i]):
				typ = dtTZ
				for i++; i < len(s) && (isDigit(s[i]) || s[i] == ':' || s[i] == '.' || s[i] == '-'); i++ {
				}
			case i < len(s) && isAlpha(s[i]):
				typ = dtSpecial
				for i++; i < len(s) && isAlpha(s[i]); i++ {
				}
			default:
				return nil, false
			}
			text = string(c) + s[j:i]
		case isSeparatorChar(c):
			i++
			continue
		default:
			return nil, false
		}
		if used+len(text) >= dateWorkBuf {
			return nil, false
		}
		used += len(text) + 1
		fields = append(fields, dtField{s: lowerASCII(text), typ: typ})
	}
	return fields, true
}

// dtUnit is the unit of a number field, as DecodeInterval reads it.
type dtUnit int

const (
	dtuNone dtUnit = iota // none yet: the typmod's last field, or seconds
	dtuBad                // none it reads: after "ago", or a quarter
	dtuMicrosecond
	dtuMillisecond
	dtuSecond
	dtuMinute
	dtuHour
	dtuDay
	dtuWeek
	dtuMonth
	dtuYear
	dtuDecade
	dtuCentury
	dtuMillennium
	dtuAgo // "ago", not a unit
)

// mask is the bit of the unit in the fields read so far (Postgres's
// fmask), so that each is given once.
func (u dtUnit) mask() int { return 1 << u }

const (
	// A second with a fraction is its milliseconds and microseconds too
	// (DTK_ALL_SECS_M), and a clock all of its units (DTK_TIME_M).
	allSecsMask = 1<<dtuSecond | 1<<dtuMillisecond | 1<<dtuMicrosecond
	clockMask   = 1<<dtuHour | 1<<dtuMinute | allSecsMask
)

// deltaUnits are the words of Postgres's deltatktbl, by tokenKey. Its
// quarters, "qtr" and "timezone" are words DecodeInterval doesn't read.
var deltaUnits = func() map[string]dtUnit {
	m := map[string]dtUnit{"ago": dtuAgo}
	for u, ws := range map[dtUnit]string{
		dtuBad:         "qtr quarter timezone",
		dtuMicrosecond: "microsecon us usec usecond useconds usecs",
		dtuMillisecond: "millisecon ms msec msecond mseconds msecs",
		dtuSecond:      "s sec second seconds secs",
		dtuMinute:      "m min mins minute minutes",
		dtuHour:        "h hour hours hr hrs",
		dtuDay:         "d day days",
		dtuWeek:        "w week weeks",
		dtuMonth:       "mon mons month months",
		dtuYear:        "y year years yr yrs",
		dtuDecade:      "dec decade decades decs",
		dtuCentury:     "c cent centuries century",
		dtuMillennium:  "mil millennia millennium mils",
	} {
		for _, w := range strings.Fields(ws) {
			m[w] = u
		}
	}
	return m
}()

// rangeUnit is the unit of the rightmost number without one, in a typmod
// of fields rng (0 for all of them): its last field.
func rangeUnit(rng int) dtUnit {
	switch rng {
	case ivYear:
		return dtuYear
	case ivMonth, ivYear | ivMonth:
		return dtuMonth
	case ivDay:
		return dtuDay
	case ivHour, ivDay | ivHour:
		return dtuHour
	case ivMinute, ivHour | ivMinute, ivDay | ivHour | ivMinute:
		return dtuMinute
	}
	return dtuSecond
}

// decodeIntervalFields reads the fields of an interval literal, in a
// typmod of fields rng, as Postgres's DecodeInterval does. It reads them
// from right to left, so that a unit comes before its number, and a number
// without one has the unit of the number to its right, but days after
// hours or a clock: '1 2 hours' is 1 day 02:00:00. The rightmost has rng's
// last field, or is seconds. A unit given twice is an error ('1 day 2
// days'), and so are seconds with a fraction and milliseconds or
// microseconds, and two clocks. "ago" anywhere negates them all.
func decodeIntervalFields(fields []dtField, rng int) (itmIn, literalErr) {
	var it itmIn
	fmask := 0
	unit := dtuNone
	ago := false
	for i := len(fields) - 1; i >= 0; i-- {
		f := fields[i]
		tmask := 0
		switch f.typ {
		case dtTime:
			us, e := clockMicros(f.s, rng)
			if e != 0 {
				return itmIn{}, e
			}
			// As in Postgres, the clock replaces the time read so far: that
			// of a fraction of a day, week or month to its right.
			it.usec = us
			tmask, unit = clockMask, dtuDay
		case dtTZ, dtDate, dtNumber:
			if f.typ == dtTZ && strings.Contains(f.s[1:], ":") {
				// A signed clock, as '1 -02:00'. If it isn't one, the
				// number fails below.
				if us, e := clockMicros(f.s[1:], rng); e == 0 {
					if f.s[0] == '-' {
						if us == math.MinInt64 {
							return itmIn{}, litFieldRange
						}
						us = -us
					}
					it.usec = us
					tmask, unit = clockMask, dtuDay
					break
				}
			}
			if unit == dtuNone {
				unit = rangeUnit(rng)
			}
			val, rest, ok := strtoi64(f.s)
			if !ok {
				return itmIn{}, litFieldRange
			}
			var fval float64
			switch {
			case rest == "":
			case rest[0] == '-':
				// Years-months: the months are 0 to 11, and a minus sign
				// applies to both.
				m, rest, ok := strtoi64(rest[1:])
				if !ok || m < 0 || m >= 12 {
					return itmIn{}, litFieldRange
				}
				if rest != "" {
					return itmIn{}, litBadFormat
				}
				unit = dtuMonth
				if f.s[0] == '-' {
					m = -m
				}
				if !mulAddInt64(val, 12, &m) {
					return itmIn{}, litFieldRange
				}
				val = m
			case rest[0] == '.':
				var e literalErr
				if fval, e = fraction(rest); e != 0 {
					return itmIn{}, e
				}
				if f.s[0] == '-' {
					fval = -fval
				}
			default:
				return itmIn{}, litBadFormat
			}
			if tmask, ok = it.addDecoded(unit, val, fval); tmask == 0 {
				return itmIn{}, litBadFormat
			} else if !ok {
				return itmIn{}, litFieldRange
			}
			if unit == dtuHour {
				unit = dtuDay
			}
		case dtString, dtSpecial:
			u, ok := deltaUnits[tokenKey(f.s)]
			switch {
			case !ok:
				return itmIn{}, litBadFormat
			case u == dtuAgo:
				ago, unit = true, dtuBad
			default:
				unit = u
			}
		}
		if tmask&fmask != 0 {
			return itmIn{}, litBadFormat
		}
		fmask |= tmask
	}
	if fmask == 0 {
		return itmIn{}, litBadFormat
	}
	if ago && !it.negate() {
		return itmIn{}, litFieldRange
	}
	return it, 0
}

// addDecoded adds val + fval of unit u, as DecodeInterval does. It returns
// the unit's bit in the fields read (0 if u isn't a unit), and is false if
// a field overflows.
func (it *itmIn) addDecoded(u dtUnit, val int64, fval float64) (int, bool) {
	var ok bool
	switch u {
	case dtuMicrosecond:
		ok = it.adjustMicroseconds(val, fval, 1)
	case dtuMillisecond:
		ok = it.adjustMicroseconds(val, fval, 1000)
	case dtuSecond:
		ok = it.adjustMicroseconds(val, fval, usPerSecond)
		if fval != 0 {
			return allSecsMask, ok
		}
	case dtuMinute:
		ok = it.adjustMicroseconds(val, fval, usPerMinute)
	case dtuHour:
		ok = it.adjustMicroseconds(val, fval, usPerHour)
	case dtuDay:
		ok = it.adjustDays(val, 1) && it.adjustFractMicroseconds(fval, usPerDay)
	case dtuWeek:
		ok = it.adjustDays(val, 7) && it.adjustFractDays(fval, 7)
	case dtuMonth:
		ok = it.adjustMonths(val) && it.adjustFractDays(fval, 30)
	case dtuYear:
		ok = it.adjustYears(val, 1) && it.adjustFractYears(fval, 1)
	case dtuDecade:
		ok = it.adjustYears(val, 10) && it.adjustFractYears(fval, 10)
	case dtuCentury:
		ok = it.adjustYears(val, 100) && it.adjustFractYears(fval, 100)
	case dtuMillennium:
		ok = it.adjustYears(val, 1000) && it.adjustFractYears(fval, 1000)
	default:
		return 0, false
	}
	return u.mask(), ok
}

// strtoi64 reads the integer at the start of s as C's strtoll does: a sign
// and digits, or 0 without digits (then rest is s). It is false past
// int64.
func strtoi64(s string) (val int64, rest string, ok bool) {
	sign := 0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign = 1
	}
	n := digitsEnd(s, sign)
	if n == sign {
		return 0, s, true
	}
	v, err := strconv.ParseInt(s[:n], 10, 64)
	return v, s[n:], err == nil
}

// strtoint is strtoi64, false past 32 bits (Postgres's strtoint).
func strtoint(s string) (int64, string, bool) {
	v, rest, ok := strtoi64(s)
	return v, rest, ok && v >= math.MinInt32 && v <= math.MaxInt32
}

// digitsEnd is the end of the digits in s from i.
func digitsEnd(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

// intervalNumber reads a number, [+-]digits[.digits], as DecodeInterval
// does: its integer part exactly, and its fraction as a double with the
// number's sign. Unlike Postgres, it reads a sign without an integer part
// ('-.5'): it reads n of INTERVAL n UNIT (unitCount).
func intervalNumber(s string) (int64, float64, literalErr) {
	sign := 0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign = 1
	}
	n := digitsEnd(s, sign)
	var val int64
	var fval float64
	if n > sign {
		var err error
		if val, err = strconv.ParseInt(s[:n], 10, 64); err != nil {
			return 0, 0, litOutOfRange
		}
	} else if len(s) < n+2 {
		return 0, 0, litBadFormat // no digits
	}
	if n < len(s) {
		var e literalErr
		if fval, e = fraction(s[n:]); e != 0 {
			return 0, 0, e
		}
		if s[0] == '-' {
			fval = -fval
		}
	}
	return val, fval, 0
}

// fraction reads a fraction, "." and digits, as a double (ParseFraction):
// "." alone is 0.
func fraction(f string) (float64, literalErr) {
	if f == "" || f[0] != '.' || digitsEnd(f, 1) != len(f) {
		return 0, litBadFormat
	}
	if len(f) == 1 {
		return 0, 0
	}
	v, err := strconv.ParseFloat(f, 64)
	if err != nil {
		return 0, litBadFormat
	}
	return v, 0
}

// clockMicros reads a clock field, h:m, h:m:s[.f] or m:s.f, in a typmod of
// fields rng, into microseconds as DecodeTimeForInterval does. In a MINUTE
// TO SECOND typmod, h:m is m:s too. A field's digits may be missing ('1:'
// is 1 hour). The minutes are 0 to 59 and the seconds 0 to 60, and the
// fraction of a second is rounded to the microsecond, half to even.
func clockMicros(f string, rng int) (int64, literalErr) {
	h, rest, ok := strtoi64(f)
	if !ok {
		return 0, litFieldRange
	}
	if rest == "" || rest[0] != ':' {
		return 0, litBadFormat
	}
	m, rest, ok := strtoint(rest[1:])
	if !ok {
		return 0, litFieldRange
	}
	var sec, fsec int64
	var e literalErr
	switch {
	case rest == "":
		if rng == ivMinute|ivSecond {
			if h > math.MaxInt32 {
				return 0, litFieldRange
			}
			h, m, sec = 0, h, m
		}
	case rest[0] == '.':
		// m:s.f
		if fsec, e = fractionalSecond(rest); e != 0 {
			return 0, e
		}
		if h > math.MaxInt32 {
			return 0, litFieldRange
		}
		h, m, sec = 0, h, m
	case rest[0] == ':':
		if sec, rest, ok = strtoint(rest[1:]); !ok {
			return 0, litFieldRange
		}
		if rest != "" && rest[0] == '.' {
			if fsec, e = fractionalSecond(rest); e != 0 {
				return 0, e
			}
		} else if rest != "" {
			return 0, litBadFormat
		}
	default:
		return 0, litBadFormat
	}
	if h < 0 || m < 0 || m > 59 || sec < 0 || sec > 60 || fsec < 0 || fsec > usPerSecond {
		return 0, litFieldRange
	}
	us := fsec
	if !mulAddInt64(h, usPerHour, &us) || !mulAddInt64(m, usPerMinute, &us) || !mulAddInt64(sec, usPerSecond, &us) {
		return 0, litFieldRange
	}
	return us, 0
}

// fractionalSecond reads a fraction of a second as microseconds, rounded
// half to even (ParseFractionalSecond).
func fractionalSecond(f string) (int64, literalErr) {
	frac, e := fraction(f)
	return int64(math.RoundToEven(frac * 1e6)), e
}

// isoInterval reads an ISO 8601 interval as Postgres's
// DecodeISO8601Interval does: with designators ('P1Y2M3DT4H5M6S', with
// weeks too, and fractions in any field), or in the alternative format
// ('P0001-02-03T04:05:06', 'P00010203T040506'). Its letters are upper
// case, and it has no spaces.
func isoInterval(s string) (itmIn, literalErr) {
	fail := func(e literalErr) (itmIn, literalErr) { return itmIn{}, e }
	if len(s) < 2 || s[0] != 'P' {
		return fail(litBadFormat)
	}
	str := s[1:]
	var it itmIn
	var val int64
	var fval float64
	// number reads the number at the start of str.
	number := func() literalErr {
		var e literalErr
		val, fval, str, e = iso8601Number(str)
		return e
	}
	datePart, haveField := true, false
	for str != "" {
		if str[0] == 'T' {
			// The time part.
			datePart, haveField = false, false
			str = str[1:]
			continue
		}
		fieldStart := str
		if e := number(); e != 0 {
			return fail(e)
		}
		var unit byte // 0 at the end
		if str != "" {
			unit, str = str[0], str[1:]
		}
		var ok bool
		if datePart {
			switch unit {
			case 'Y':
				ok = it.adjustYears(val, 1) && it.adjustFractYears(fval, 1)
			case 'M':
				ok = it.adjustMonths(val) && it.adjustFractDays(fval, 30)
			case 'W':
				ok = it.adjustDays(val, 7) && it.adjustFractDays(fval, 7)
			case 'D':
				ok = it.adjustDays(val, 1) && it.adjustFractMicroseconds(fval, usPerDay)
			case 'T', 0:
				if iso8601IntegerWidth(fieldStart) == 8 && !haveField {
					// The alternative format's basic yyyymmdd.
					if !it.adjustYears(val/10000, 1) || !it.adjustMonths(val/100%100) || !it.adjustDays(val%100, 1) ||
						!it.adjustFractMicroseconds(fval, usPerDay) {
						return fail(litFieldRange)
					}
					if unit == 0 {
						return it, 0
					}
					datePart, haveField = false, false
					continue
				}
				fallthrough
			case '-':
				// The alternative format's extended y-m-d.
				if haveField {
					return fail(litBadFormat)
				}
				if !it.adjustYears(val, 1) || !it.adjustFractYears(fval, 1) {
					return fail(litFieldRange)
				}
				if unit == 0 {
					return it, 0
				}
				if unit == 'T' {
					datePart, haveField = false, false
					continue
				}
				if e := number(); e != 0 {
					return fail(e)
				}
				if !it.adjustMonths(val) || !it.adjustFractDays(fval, 30) {
					return fail(litFieldRange)
				}
				if str == "" {
					return it, 0
				}
				if str[0] == 'T' {
					datePart, haveField = false, false
					continue
				}
				if str[0] != '-' {
					return fail(litBadFormat)
				}
				str = str[1:]
				if e := number(); e != 0 {
					return fail(e)
				}
				if !it.adjustDays(val, 1) || !it.adjustFractMicroseconds(fval, usPerDay) {
					return fail(litFieldRange)
				}
				if str == "" {
					return it, 0
				}
				if str[0] == 'T' {
					datePart, haveField = false, false
					continue
				}
				return fail(litBadFormat)
			default:
				return fail(litBadFormat)
			}
		} else {
			switch unit {
			case 'H':
				ok = it.adjustMicroseconds(val, fval, usPerHour)
			case 'M':
				ok = it.adjustMicroseconds(val, fval, usPerMinute)
			case 'S':
				ok = it.adjustMicroseconds(val, fval, usPerSecond)
			case 0:
				if iso8601IntegerWidth(fieldStart) == 6 && !haveField {
					// The alternative format's basic hhmmss.
					if !it.adjustMicroseconds(val/10000, 0, usPerHour) || !it.adjustMicroseconds(val/100%100, 0, usPerMinute) ||
						!it.adjustMicroseconds(val%100, 0, usPerSecond) || !it.adjustFractMicroseconds(fval, 1) {
						return fail(litFieldRange)
					}
					return it, 0
				}
				fallthrough
			case ':':
				// The alternative format's extended h:m:s.
				if haveField {
					return fail(litBadFormat)
				}
				if !it.adjustMicroseconds(val, fval, usPerHour) {
					return fail(litFieldRange)
				}
				if unit == 0 {
					return it, 0
				}
				if e := number(); e != 0 {
					return fail(e)
				}
				if !it.adjustMicroseconds(val, fval, usPerMinute) {
					return fail(litFieldRange)
				}
				if str == "" {
					return it, 0
				}
				if str[0] != ':' {
					return fail(litBadFormat)
				}
				str = str[1:]
				if e := number(); e != 0 {
					return fail(e)
				}
				if !it.adjustMicroseconds(val, fval, usPerSecond) {
					return fail(litFieldRange)
				}
				if str == "" {
					return it, 0
				}
				return fail(litBadFormat)
			default:
				return fail(litBadFormat)
			}
		}
		if !ok {
			return fail(litFieldRange)
		}
		haveField = true
	}
	return it, 0
}

// iso8601Number reads the number at the start of s as ParseISO8601Number
// does, with C's strtod: a double, also in e notation or hexadecimal
// ("0x1.8p1"), at most 1e15 in magnitude (an infinity or a NaN is
// "interval field value out of range"), as its integer part and fraction.
// It returns the rest of s.
func iso8601Number(s string) (int64, float64, string, literalErr) {
	if s == "" || !(s[0] >= '0' && s[0] <= '9' || s[0] == '-' || s[0] == '.') {
		return 0, 0, s, litBadFormat
	}
	// The longest prefix that strtod reads.
	i := 0
	if s[0] == '-' {
		i = 1
	}
	if w := strings.ToLower(s[i:min(len(s), i+3)]); w == "inf" || w == "nan" {
		return 0, 0, s, litFieldRange
	}
	end, hex := hexFloatEnd(s, i), true
	if end == 0 {
		hex = false
		end = digitsEnd(s, i)
		n := end - i
		if end < len(s) && s[end] == '.' {
			e := digitsEnd(s, end+1)
			n += e - end - 1
			end = e
		}
		if n == 0 {
			return 0, 0, s, litBadFormat
		}
		end = exponentEnd(s, end, 'e')
	}
	num := s[:end]
	if hex && !strings.ContainsAny(num, "pP") {
		num += "p0" // Go's hexadecimal floats have an exponent
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || strtodUnderflows(num, v, hex) {
		return 0, 0, s, litBadFormat // past the range of a double: ERANGE
	}
	if v < -1e15 || v > 1e15 {
		return 0, 0, s, litFieldRange
	}
	ip := math.Trunc(v)
	return int64(ip), v - ip, s[end:], 0
}

func isHexDigit(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// hexFloatEnd is the end of the hexadecimal number strtod reads at s[i:]
// (0x, hexadecimal digits with a point, and a binary exponent), or 0 if
// there is none.
func hexFloatEnd(s string, i int) int {
	if i+2 > len(s) || s[i] != '0' || s[i+1] != 'x' && s[i+1] != 'X' {
		return 0
	}
	j, digits := i+2, 0
	for ; j < len(s) && isHexDigit(s[j]); j++ {
		digits++
	}
	if j < len(s) && s[j] == '.' {
		for j++; j < len(s) && isHexDigit(s[j]); j++ {
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	return exponentEnd(s, j, 'p')
}

// exponentEnd is the end of the exponent at s[i:] that strtod reads, e or
// p (either case), a sign and digits, or i if there is none.
func exponentEnd(s string, i int, letter byte) int {
	if i >= len(s) || s[i]|0x20 != letter {
		return i
	}
	e := i + 1
	if e < len(s) && (s[e] == '+' || s[e] == '-') {
		e++
	}
	if d := digitsEnd(s, e); d > e {
		return d
	}
	return i
}

// strtodUnderflows is whether glibc's strtod sets ERANGE for v, read from
// num: if v is 0 or subnormal but num isn't exactly v. (A decimal number
// is never exactly subnormal.)
func strtodUnderflows(num string, v float64, hex bool) bool {
	if v != 0 && math.Abs(v) >= 0x1p-1022 {
		return false
	}
	mant := num
	if hex {
		mant = mant[strings.IndexAny(mant, "xX")+1 : strings.IndexAny(mant, "pP")]
	} else if e := strings.IndexAny(mant, "eE"); e >= 0 {
		mant = mant[:e]
	}
	if v == 0 {
		return strings.Trim(mant, "-.0") != ""
	}
	if !hex {
		return true
	}
	exact, _, err := big.ParseFloat(num, 0, uint(4*len(num)+64), big.ToNearestEven)
	return err != nil || exact.Cmp(big.NewFloat(v)) != 0
}

// iso8601IntegerWidth is the number of digits in the integer part of an
// ISO 8601 number.
func iso8601IntegerWidth(s string) int {
	s = strings.TrimPrefix(s, "-")
	return digitsEnd(s, 0)
}

// unitCount is n of INTERVAL n UNIT, as an integer part and a fraction: a
// NUMERIC, and text, as a literal's number is read (intervalNumber), else
// as a double.
func unitCount(v Value) (int64, float64, literalErr) {
	switch v.T.Kind {
	case KindInt16, KindInt32, KindInt64:
		return v.I, 0, 0
	case KindDecimal:
		return intervalNumber(v.Text())
	case KindString:
		if val, fval, e := intervalNumber(strings.TrimSpace(v.S)); e != litBadFormat {
			return val, fval, e
		}
	}
	n, err := Coerce(v, typeFloat64)
	if err != nil {
		return 0, 0, litBadFormat
	}
	if !(n.F > -(1<<63) && n.F < 1<<63) { // also NaN
		return 0, 0, litOutOfRange
	}
	ip := math.Trunc(n.F)
	return int64(ip), n.F - ip, 0
}

// formatInterval renders an interval like Postgres: "1 year 2 mons 3 days
// 04:05:06.5"; the zero interval is "00:00:00". As in Postgres, a positive
// field that follows a negative one has a "+": "-1 days +02:00:00",
// "-1 years +2 mons".
func formatInterval(v Value) string {
	var parts []string
	afterNegative := false
	unit := func(n int64, one, many string) {
		if n == 0 {
			return
		}
		name := many
		if n == 1 {
			name = one
		}
		sign := ""
		if afterNegative && n > 0 {
			sign = "+"
		}
		parts = append(parts, fmt.Sprintf("%s%d %s", sign, n, name))
		afterNegative = n < 0
	}
	years, months := int64(v.Months)/12, int64(v.Months)%12
	unit(years, "year", "years")
	unit(months, "mon", "mons")
	unit(int64(v.Days), "day", "days")
	if v.I != 0 || len(parts) == 0 {
		// The magnitude is a uint64 so that MinInt64's fits.
		ns := uint64(v.I)
		sign := ""
		if v.I < 0 {
			sign, ns = "-", -ns
		} else if afterNegative {
			sign = "+"
		}
		h, rem := ns/uint64(nsPerHour), ns%uint64(nsPerHour)
		m, rem := rem/uint64(nsPerMinute), rem%uint64(nsPerMinute)
		sec, frac := rem/uint64(nsPerSecond), rem%uint64(nsPerSecond)
		clock := fmt.Sprintf("%s%02d:%02d:%02d", sign, h, m, sec)
		if frac != 0 {
			clock += strings.TrimRight(fmt.Sprintf(".%09d", frac), "0")
		}
		parts = append(parts, clock)
	}
	return strings.Join(parts, " ")
}

// intervalTotal is the interval's length with a month as 30 days.
func intervalTotal(v Value) *big.Int {
	t := big.NewInt(int64(v.Months) * 30)
	t.Add(t, big.NewInt(int64(v.Days)))
	t.Mul(t, big.NewInt(nsPerDay))
	return t.Add(t, big.NewInt(v.I))
}

func encodeInterval(v Value) string {
	return fmt.Sprintf("%d:%d:%d", v.Months, v.Days, v.I)
}

func decodeInterval(s string) (Value, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return Value{}, fmt.Errorf("corrupt interval value %q", s)
	}
	var n [3]int64
	for i, p := range parts {
		x, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return Value{}, fmt.Errorf("corrupt interval value %q", s)
		}
		n[i] = x
	}
	return intervalValue(n[0], n[1], n[2])
}

// ---- arithmetic ----

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// addMonths adds months, clamping the day to the end of the target month.
func addMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	total := int(m) - 1 + n
	ny := y + int(floorDiv(int64(total), 12))
	nm := time.Month(total - int(floorDiv(int64(total), 12))*12 + 1)
	if last := daysIn(ny, nm); d > last {
		d = last
	}
	return time.Date(ny, nm, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// addInterval adds an interval to a time.
func addInterval(t time.Time, iv Value) time.Time {
	t = addMonths(t, int(iv.Months))
	t = t.AddDate(0, 0, int(iv.Days))
	return t.Add(time.Duration(iv.I))
}

// addOrSub is a + b, or a - b for op "-", and whether it overflowed int64.
func addOrSub(op string, a, b int64) (int64, bool) {
	if op == "-" {
		s := a - b
		return s, (s < a) != (b > 0)
	}
	s := a + b
	return s, (s > a) != (b > 0)
}

func negateInterval(v Value) (Value, error) {
	if v.Months == math.MinInt32 || v.Days == math.MinInt32 || v.I == math.MinInt64 {
		return Value{}, stateErr(stateDatetimeRange, "interval out of range")
	}
	return intervalValue(-int64(v.Months), -int64(v.Days), -v.I)
}

// intervalMicros is an interval's time part in microseconds, with a
// fraction for nanoseconds that aren't whole microseconds.
func intervalMicros(v Value) float64 { return float64(v.I/1000) + float64(v.I%1000)/1000 }

// multiplyInterval is v × f, as Postgres's interval * double precision
// (interval_mul, see scaleInterval).
func multiplyInterval(v Value, f float64) (Value, error) {
	return microsInterval(scaleInterval(v.Months, v.Days, intervalMicros(v), f, false))
}

// divideInterval divides an interval of months, days and microseconds by
// factor (non-zero), as Postgres's interval / double precision
// (interval_div, see scaleInterval): for interval / n, and for AVG, whose
// sum's time may be beyond the nanoseconds' range.
func divideInterval(months, days int32, us, factor float64) (Value, error) {
	return microsInterval(scaleInterval(months, days, us, factor, true))
}

// microsInterval is the interval of months, days and microseconds us, or
// "interval out of range" past the nanoseconds' range, or for err.
func microsInterval(months, days int32, us int64, err error) (Value, error) {
	if err == nil && (us > math.MaxInt64/1000 || us < math.MinInt64/1000) {
		err = stateErr(stateDatetimeRange, "interval out of range")
	}
	if err != nil {
		return Value{}, err
	}
	return Value{T: typeInterval, Months: months, Days: days, I: us * 1000}, nil
}

// scaleInterval multiplies an interval of months, days and microseconds by
// factor as Postgres's interval_mul does, or with div divides it by factor
// (non-zero) as interval_div does, in doubles as in Postgres. Each field is
// multiplied and truncated, and must fit in its 32 bits; the fraction of a
// month cascades into days (30 per month), rounded to 6 decimal places
// (TSROUND), the fraction of a day into seconds, also rounded so, and the
// time is rounded to the microsecond, half to even, and must fit in 64 bits.
// It returns the result's months, days and microseconds.
//
// The driver's intervals hold nanoseconds, Postgres's microseconds, so a
// time that isn't whole microseconds (a bound or ingested Arrow value)
// counts with its fraction, and the result is in whole microseconds, as
// Postgres's always is.
func scaleInterval(months, days int32, us, factor float64, div bool) (int32, int32, int64, error) {
	scale := func(x float64) float64 {
		if div {
			return x / factor
		}
		// The conversion keeps the product from being fused into an FMA.
		return float64(x * factor)
	}
	outOfRange := func() (int32, int32, int64, error) {
		return 0, 0, 0, stateErr(stateDatetimeRange, "interval out of range")
	}
	// FLOAT8_FITS_IN_INT32, false for NaN.
	fits := func(x float64) bool { return x >= math.MinInt32 && x < -math.MinInt32 }
	// pg_add_s32_overflow.
	add := func(a, b int32) (int32, bool) {
		s := int64(a) + int64(b)
		return int32(s), s == int64(int32(s))
	}
	tsRound := func(x float64) float64 { return math.RoundToEven(x*1e6) / 1e6 }
	mf, df := scale(float64(months)), scale(float64(days))
	if !fits(mf) || !fits(df) {
		return outOfRange()
	}
	m, d := int32(mf), int32(df)
	monthDays := tsRound((mf - float64(m)) * 30)
	sec := tsRound((df - float64(d) + monthDays - float64(int32(monthDays))) * 86400)
	var ok bool
	if math.Abs(sec) >= 86400 {
		// 24:00:00 from rounding, or more from the cascades.
		whole := int32(sec / 86400)
		if d, ok = add(d, whole); !ok {
			return outOfRange()
		}
		sec -= float64(whole * 86400)
	}
	if d, ok = add(d, int32(monthDays)); !ok {
		return outOfRange()
	}
	// The conversion keeps the product from being fused into an FMA.
	t := math.RoundToEven(scale(us) + float64(sec*1e6))
	if !(t >= -(1<<63) && t < 1<<63) {
		return outOfRange()
	}
	return m, d, int64(t), nil
}

// timeOfDay returns nanoseconds since midnight of a TIME value.
func timeOfDay(v Value) int64 {
	return v.I * (nsPerSecond / unitsPerSecond[v.T.Unit])
}

// temporalType is the result type of a date/time arithmetic operator, or ok
// = false if the operands are not a date/time combination. As in Postgres,
// a time or timestamp result doesn't keep its operand's declared precision
// (withoutTypmod): TIMESTAMP(2) + INTERVAL is a TIMESTAMP(3).
func temporalType(op string, a, b ColType) (ColType, bool, error) {
	ak, bk := a.Kind, b.Kind
	isInt := func(k Kind) bool { return k.isInteger() }
	iv := KindInterval
	bad := func() (ColType, bool, error) {
		return ColType{}, true, fmt.Errorf("operator %s is not supported for %s and %s", op, a.SQLName(), b.SQLName())
	}
	involved := ak == iv || bk == iv || ak == KindDate || bk == KindDate || ak == KindTimestamp || bk == KindTimestamp ||
		ak == KindTime || bk == KindTime || ak == KindTimeTZ || bk == KindTimeTZ
	if !involved {
		return ColType{}, false, nil
	}
	switch op {
	case "+":
		switch {
		case ak == KindTimestamp && bk == iv:
			return a.withoutTypmod(), true, nil
		case ak == iv && bk == KindTimestamp:
			return b.withoutTypmod(), true, nil
		case (ak == KindDate && bk == iv) || (ak == iv && bk == KindDate):
			return typeTimestamp, true, nil
		case ak == KindDate && isInt(bk), isInt(ak) && bk == KindDate:
			return typeDate, true, nil
		case ak == KindTime && bk == iv:
			return a.withoutTypmod(), true, nil
		case ak == iv && bk == KindTime:
			return b.withoutTypmod(), true, nil
		case ak == iv && bk == iv:
			return typeInterval, true, nil
		case ak == KindDate && bk == KindTime:
			return typeTimestamp, true, nil
		case ak == KindTimeTZ && bk == iv:
			return a.withoutTypmod(), true, nil
		case ak == iv && bk == KindTimeTZ:
			return b.withoutTypmod(), true, nil
		case ak == KindDate && bk == KindTimeTZ, ak == KindTimeTZ && bk == KindDate:
			return typeTimestampTZ, true, nil
		}
	case "-":
		switch {
		case ak == KindTimestamp && bk == iv:
			return a.withoutTypmod(), true, nil
		case ak == KindDate && bk == iv:
			return typeTimestamp, true, nil
		case ak == KindDate && isInt(bk):
			return typeDate, true, nil
		case ak == KindDate && bk == KindDate:
			return typeInt64, true, nil
		case (ak == KindTimestamp || ak == KindDate) && (bk == KindTimestamp || bk == KindDate):
			return typeInterval, true, nil
		case ak == KindTime && bk == iv:
			return a.withoutTypmod(), true, nil
		case ak == KindTime && bk == KindTime:
			return typeInterval, true, nil
		case ak == KindTimeTZ && bk == iv:
			return a.withoutTypmod(), true, nil
		case ak == iv && bk == iv:
			return typeInterval, true, nil
		}
	case "*":
		if (ak == iv && (bk.isNumeric() || bk == KindNull)) || ((ak.isNumeric() || ak == KindNull) && bk == iv) {
			return typeInterval, true, nil
		}
	case "/":
		if ak == iv && (bk.isNumeric() || bk == KindNull) {
			return typeInterval, true, nil
		}
	}
	if ak == KindNull || bk == KindNull {
		// NULL with a date/time operand: the type of the other side.
		if ak == KindNull {
			return b.withoutTypmod(), true, nil
		}
		return a.withoutTypmod(), true, nil
	}
	return bad()
}

// temporalOp evaluates date/time arithmetic on non-NULL operands, in the
// session time zone z: months and days are added to a timestamp with time
// zone on its local time (addIntervalIn), and a TIMESTAMP or DATE
// subtracted from or by one is a local time.
func temporalOp(op string, l, r Value, rt ColType, z tzZone) (Value, error) {
	lk, rk := l.T.Kind, r.T.Kind
	switch {
	case lk == KindInterval && rk == KindInterval:
		sign := int64(1)
		if op == "-" {
			sign = -1
		}
		ns, overflow := addOrSub(op, l.I, r.I)
		if overflow {
			return Value{}, stateErr(stateDatetimeRange, "interval out of range")
		}
		return intervalValue(int64(l.Months)+sign*int64(r.Months), int64(l.Days)+sign*int64(r.Days), ns)
	case op == "*" && lk == KindInterval:
		f, _ := r.asFloat()
		return multiplyInterval(l, f)
	case op == "*" && rk == KindInterval:
		f, _ := l.asFloat()
		return multiplyInterval(r, f)
	case op == "/" && lk == KindInterval:
		f, _ := r.asFloat()
		if f == 0 {
			return Value{}, stateErr(stateDivisionByZero, "division by zero")
		}
		return divideInterval(l.Months, l.Days, intervalMicros(l), f)
	case lk == KindDate && rk.isInteger(), lk.isInteger() && rk == KindDate:
		// date ± days, days + date
		d, n := l.I, r.I
		if lk != KindDate {
			d, n = n, d
		}
		days, overflow := addOrSub(op, d, n)
		if overflow {
			return Value{}, stateErr(stateDatetimeRange, "date out of range")
		}
		return dateValue(days)
	case lk == KindDate && rk == KindDate:
		// Dates are date32, so the difference fits.
		return intValue(typeInt64, l.I-r.I), nil
	case (lk == KindTime) && rk == KindTime:
		return intervalValue(0, 0, timeOfDay(l)-timeOfDay(r))
	case lk == KindTime && rk == KindInterval, lk == KindInterval && rk == KindTime:
		t, iv := l, r
		if lk == KindInterval {
			t, iv = r, l
		}
		sign := int64(1)
		if op == "-" {
			sign = -1
		}
		ns := timeOfDay(t) + sign*(iv.I%nsPerDay) // reduced first, so it can't overflow
		ns -= floorDiv(ns, nsPerDay) * nsPerDay   // wrap around midnight
		return intValue(rt, ns/(nsPerSecond/unitsPerSecond[rt.Unit])), nil
	case lk == KindDate && rk == KindTime:
		tm, err := toTime(l)
		if err != nil {
			return Value{}, err
		}
		return fromTime(tm.Add(time.Duration(timeOfDay(r))), rt)
	case lk == KindTimeTZ && rk == KindInterval, lk == KindInterval && rk == KindTimeTZ:
		t, iv := l, r
		if lk == KindInterval {
			t, iv = r, l
		}
		sign := int64(1)
		if op == "-" {
			sign = -1
		}
		return timeTZPlusInterval(t, iv, sign, rt), nil
	case lk == KindDate && rk == KindTimeTZ:
		return dateAtTimeTZ(l, r, rt)
	case lk == KindTimeTZ && rk == KindDate:
		return dateAtTimeTZ(r, l, rt)
	case rt.Kind == KindInterval:
		// timestamp/date - timestamp/date: days and time, no months. With
		// a timestamp with time zone, both are instants.
		if isTimestampTZ(l.T) != isTimestampTZ(r.T) {
			var err error
			if l, err = asInstant(l, z); err == nil {
				r, err = asInstant(r, z)
			}
			if err != nil {
				return Value{}, err
			}
		}
		a, err := toTime(l)
		if err != nil {
			return Value{}, err
		}
		b, err := toTime(r)
		if err != nil {
			return Value{}, err
		}
		diff := new(big.Int).Sub(big.NewInt(a.Unix()), big.NewInt(b.Unix()))
		diff.Mul(diff, big.NewInt(nsPerSecond))
		diff.Add(diff, big.NewInt(int64(a.Nanosecond()-b.Nanosecond())))
		days, rem := new(big.Int).QuoRem(diff, big.NewInt(nsPerDay), new(big.Int))
		return intervalValue(0, days.Int64(), rem.Int64())
	default:
		// timestamp/date ± interval
		t, iv := l, r
		if lk == KindInterval {
			t, iv = r, l
		}
		if op == "-" {
			// As in Postgres, an interval that can't be negated is out of range.
			var err error
			if iv, err = negateInterval(iv); err != nil {
				return Value{}, err
			}
		}
		tm, err := toTime(t)
		if err != nil {
			return Value{}, err
		}
		if isTimestampTZ(rt) {
			return fromTime(addIntervalIn(tm, iv, z), rt)
		}
		return fromTime(addInterval(tm, iv), rt)
	}
}

// isTimestampTZ reports whether t is TIMESTAMP WITH TIME ZONE.
func isTimestampTZ(t ColType) bool { return t.Kind == KindTimestamp && t.TZ != "" }

// asInstant converts a TIMESTAMP or DATE to a TIMESTAMP WITH TIME ZONE, as
// a local time in z, keeping a timestamp's unit.
func asInstant(v Value, z tzZone) (Value, error) {
	switch {
	case isTimestampTZ(v.T):
		return v, nil
	case v.T.Kind == KindTimestamp:
		return coerceIn(v, timestampType(v.T.Unit, "UTC"), z)
	}
	return coerceIn(v, timestampType(arrow.Second, "UTC"), z)
}

// age implements AGE(a, b): a - b in years, months, days and time, by
// subtracting field by field and borrowing (Postgres's algorithm).
func age(a, b time.Time) (Value, error) {
	if a.Before(b) {
		v, err := age(b, a)
		if err != nil {
			return Value{}, err
		}
		return negateInterval(v)
	}
	ns := a.Nanosecond() - b.Nanosecond()
	sec := a.Second() - b.Second()
	mi := a.Minute() - b.Minute()
	hr := a.Hour() - b.Hour()
	day := a.Day() - b.Day()
	mon := int(a.Month()) - int(b.Month())
	yr := a.Year() - b.Year()
	if ns < 0 {
		ns += 1_000_000_000
		sec--
	}
	if sec < 0 {
		sec += 60
		mi--
	}
	if mi < 0 {
		mi += 60
		hr--
	}
	if hr < 0 {
		hr += 24
		day--
	}
	for day < 0 {
		// Borrow the length of the earlier date's month.
		day += daysIn(b.Year(), b.Month())
		mon--
	}
	if mon < 0 {
		mon += 12
		yr--
	}
	nanos := ((int64(hr)*60+int64(mi))*60+int64(sec))*nsPerSecond + int64(ns)
	return intervalValue(int64(yr)*12+int64(mon), int64(day), nanos)
}

// intervalPart implements EXTRACT on intervals.
func intervalPart(field string, v Value) (Value, error) {
	i := func(n int64) (Value, error) { return intValue(typeInt64, n), nil }
	switch normalizeField(field) {
	case "millennium":
		return i(int64(v.Months) / 12000)
	case "century":
		return i(int64(v.Months) / 1200)
	case "decade":
		return i(int64(v.Months) / 120)
	case "year":
		return i(int64(v.Months) / 12)
	case "quarter":
		return i(int64(v.Months)%12/3 + 1)
	case "month":
		return i(int64(v.Months) % 12)
	case "day":
		return i(int64(v.Days))
	case "hour":
		return i(v.I / nsPerHour)
	case "minute":
		return i(v.I % nsPerHour / nsPerMinute)
	case "second":
		return floatValue(typeFloat64, float64(v.I%nsPerMinute)/1e9), nil
	case "milliseconds":
		return floatValue(typeFloat64, float64(v.I%nsPerMinute)/1e6), nil
	case "microseconds":
		return i(v.I % nsPerMinute / 1000)
	case "epoch":
		// Postgres: a year is 365.25 days, a remaining month 30 days.
		years := float64(v.Months / 12)
		months := float64(v.Months % 12)
		secs := years*365.25*86400 + months*30*86400 + float64(v.Days)*86400 + float64(v.I)/1e9
		return floatValue(typeFloat64, secs), nil
	}
	return Value{}, stateErr(stateFeatureNotSupported, "field %q is not valid for INTERVAL values", field)
}
