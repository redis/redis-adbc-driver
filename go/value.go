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
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

// Value is a single typed SQL value.
//
//   - Integers, booleans (0/1), dates (days since epoch), times (units since
//     midnight) and timestamps (units since epoch) live in I.
//   - A time with time zone is its time in I and its UTC offset in Off.
//   - Floating-point values live in F.
//   - Strings and binary values live in S.
//   - Decimals live in D as an unscaled integer at scale T.Scale.
//   - Records hold their fields in rec (see record.go), and have type
//     typeRecord: the fields have their own types.
type Value struct {
	T    ColType
	Null bool
	// Off is a time with time zone's UTC offset, in seconds east of
	// Greenwich (timetz.go).
	Off int32
	I   int64
	F   float64
	S   string
	D   *big.Int
	// Intervals: Months and Days, with nanoseconds in I.
	Months int32
	Days   int32
	rec    *[]Value
}

func nullValue(t ColType) Value { return Value{T: t, Null: true} }
func intValue(t ColType, v int64) Value {
	return Value{T: t, I: v}
}
func boolValue(b bool) Value {
	if b {
		return Value{T: typeBool, I: 1}
	}
	return Value{T: typeBool}
}
func floatValue(t ColType, f float64) Value { return Value{T: t, F: f} }
func stringValue(s string) Value            { return Value{T: typeString, S: s} }
func binaryValue(s string) Value            { return Value{T: typeBinary, S: s} }
func decimalValue(unscaled *big.Int, precision, scale int32) Value {
	return Value{T: decimalType(precision, scale), D: unscaled}
}

var bigTen = big.NewInt(10)

func pow10(n int32) *big.Int {
	return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil)
}

// parseDecimal parses a decimal literal like "-123.45" or "1.5e3" into an
// unscaled integer and a scale.
func parseDecimal(s string) (*big.Int, int32, error) {
	s = strings.TrimSpace(s)
	exp := int64(0)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.ParseInt(s[i+1:], 10, 32)
		if err != nil {
			return nil, 0, stateErr(stateInvalidText, "invalid decimal %q", s)
		}
		exp = e
		s = s[:i]
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	digits := intPart + frac
	if digits == "" {
		return nil, 0, stateErr(stateInvalidText, "invalid decimal %q", s)
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return nil, 0, stateErr(stateInvalidText, "invalid decimal %q", s)
		}
	}
	v, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, 0, stateErr(stateInvalidText, "invalid decimal %q", s)
	}
	if neg {
		v.Neg(v)
	}
	scale := int64(len(frac)) - exp
	return v, int32(scale), nil
}

// rescaleDecimal converts an unscaled value from one scale to another,
// rounding half away from zero when digits are dropped.
func rescaleDecimal(v *big.Int, from, to int32) *big.Int {
	if from == to {
		return new(big.Int).Set(v)
	}
	if to > from {
		return new(big.Int).Mul(v, pow10(to-from))
	}
	div := pow10(from - to)
	q, r := new(big.Int).QuoRem(v, div, new(big.Int))
	twice := new(big.Int).Abs(r)
	twice.Mul(twice, big.NewInt(2))
	if twice.Cmp(div) >= 0 {
		if v.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

// formatDecimal renders an unscaled value at the given scale.
func formatDecimal(v *big.Int, scale int32) string {
	if scale <= 0 {
		return new(big.Int).Mul(v, pow10(-scale)).String()
	}
	neg := v.Sign() < 0
	digits := new(big.Int).Abs(v).String()
	for int32(len(digits)) <= scale {
		digits = "0" + digits
	}
	cut := int32(len(digits)) - scale
	out := digits[:cut] + "." + digits[cut:]
	if neg {
		out = "-" + out
	}
	return out
}

func decimalDigits(v *big.Int) int32 {
	return int32(len(new(big.Int).Abs(v).String()))
}

func decimalToFloat(v *big.Int, scale int32) float64 {
	f, _ := strconv.ParseFloat(formatDecimal(v, scale), 64)
	return f
}

// ---- temporal helpers ----

var unitsPerSecond = map[arrow.TimeUnit]int64{
	arrow.Second:      1,
	arrow.Millisecond: 1_000,
	arrow.Microsecond: 1_000_000,
	arrow.Nanosecond:  1_000_000_000,
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// convertUnit converts a count of `from` units into `to` units (truncating
// toward negative infinity), reporting overflow.
func convertUnit(v int64, from, to arrow.TimeUnit) (int64, error) {
	f, t := unitsPerSecond[from], unitsPerSecond[to]
	if f == t {
		return v, nil
	}
	if t > f {
		mult := t / f
		r := v * mult
		if v != 0 && (r/mult != v) {
			return 0, stateErr(stateDatetimeRange, "value %d out of range for unit %s", v, unitNames[to])
		}
		return r, nil
	}
	return floorDiv(v, f/t), nil
}

// intPow10 are the powers of ten up to 10⁹.
var intPow10 = [10]int64{1, 10, 100, 1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000, 1_000_000_000}

// roundUnit converts v from unit `from` to unit `to`, with digits
// fractional-second digits (at most to's: a TIMESTAMP(2) is in
// milliseconds, rounded to 2 digits). To fewer digits than from's it
// rounds, in one step from v, half away from epoch (seconds since the Unix
// epoch), as Postgres rounds a value to a lower precision
// (AdjustTimestampForTypmod and AdjustTimeForTypmod round half away from
// zero, and Postgres's timestamps count from 2000-01-01, pgEpoch; times
// from midnight, 0). A carry moves into the next second, minute, day or
// year.
func roundUnit(v int64, from, to arrow.TimeUnit, digits int, epoch int64) (int64, error) {
	fd, digits := precisionForUnit(from), min(digits, precisionForUnit(to))
	if digits >= fd {
		return convertUnit(v, from, to)
	}
	k := intPow10[fd-digits]
	q, r := v/k, v%k
	if r < 0 {
		q, r = q-1, r+k
	}
	if 2*r > k || (2*r == k && v > epoch*unitsPerSecond[from]) {
		q++
	}
	// q counts units of 10^-digits seconds, which `to` holds.
	m := intPow10[precisionForUnit(to)-digits]
	if q*m/m != q {
		return 0, stateErr(stateDatetimeRange, "value %d out of range for unit %s", v, unitNames[to])
	}
	return q * m, nil
}

// secondsNanosToUnit combines seconds and nanoseconds into a count of units
// without intermediate overflow.
func secondsNanosToUnit(sec, nanos int64, unit arrow.TimeUnit) (int64, error) {
	per := unitsPerSecond[unit]
	total := new(big.Int).Mul(big.NewInt(sec), big.NewInt(per))
	total.Add(total, big.NewInt(floorDiv(nanos, 1_000_000_000/per)))
	if !total.IsInt64() {
		return 0, stateErr(stateDatetimeRange, "timestamp out of range for unit %s", unitNames[unit])
	}
	return total.Int64(), nil
}

// parseFraction parses up to 9 fractional digits into nanoseconds and returns
// the number of digits.
func parseFraction(frac string) (int64, int, error) {
	if frac == "" {
		return 0, 0, nil
	}
	if len(frac) > 9 {
		frac = frac[:9]
	}
	n, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	digits := len(frac)
	for i := digits; i < 9; i++ {
		n *= 10
	}
	return n, digits, nil
}

// parseClock reads HH:MM[:SS[.fraction]] as seconds, nanoseconds and the
// fraction's digits. As in Postgres (time_overflows), a field past its
// range (25:00, 10:60) is datetime_field_overflow, and so is a time past
// 24:00:00 once its fraction is read in microseconds (24:00:01,
// 23:59:60.5; 23:59:60 is 24:00:00).
func parseClock(s string) (sec int64, nanos int64, digits int, err error) {
	bad := func() (int64, int64, int, error) {
		return 0, 0, 0, dateTimeTextError(stateDatetimeFormat, "time", s)
	}
	clock, frac, _ := strings.Cut(s, ".")
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return bad()
	}
	var hms [3]int64
	for i, p := range parts {
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return bad()
		}
		hms[i] = v
	}
	if hms[0] > 24 || hms[1] > 59 || hms[2] > 60 {
		return 0, 0, 0, dateTimeTextError(stateDatetimeRange, "time", s)
	}
	nanos, digits, err = parseFraction(frac)
	if err != nil {
		return bad()
	}
	sec = hms[0]*3600 + hms[1]*60 + hms[2]
	if sec*1_000_000+pgMicros(nanos) > 86400*1_000_000 {
		return 0, 0, 0, dateTimeTextError(stateDatetimeRange, "time", s)
	}
	return sec, nanos, digits, nil
}

// splitEra removes a trailing era, " BC" or " AD" in any case, from date or
// timestamp text, as Postgres writes years before 1 AD (0044-03-15 BC).
func splitEra(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if n := len(s); n > 3 && (s[n-3] == ' ' || s[n-3] == '\t') {
		switch strings.ToUpper(s[n-2:]) {
		case "BC":
			return strings.TrimSpace(s[:n-2]), true
		case "AD":
			return strings.TrimSpace(s[:n-2]), false
		}
	}
	return s, false
}

// parseDatePrefix parses the YYYY-MM-DD that s starts with (four to seven
// year digits; bc: a BC year, so 0001 is astronomical year 0) and returns
// its days since the epoch and the text after it. As in Postgres there is no
// year 0. If s doesn't start with a date, bad is invalid_datetime_format
// (22007); if the date's fields are out of range (February 30, year 0),
// datetime_field_overflow (22008), as Postgres reports them.
func parseDatePrefix(s string, bc bool) (days int64, rest, bad string) {
	isDigit := func(i int) bool { return i < len(s) && s[i] >= '0' && s[i] <= '9' }
	n := 0
	for isDigit(n) {
		n++
	}
	if n < 4 || n > 7 || len(s) < n+6 || s[n] != '-' || !isDigit(n+1) || !isDigit(n+2) || s[n+3] != '-' ||
		!isDigit(n+4) || !isDigit(n+5) {
		return 0, "", stateDatetimeFormat
	}
	y, _ := strconv.Atoi(s[:n])
	m, _ := strconv.Atoi(s[n+1 : n+3])
	d, _ := strconv.Atoi(s[n+4 : n+6])
	if y == 0 {
		return 0, "", stateDatetimeRange
	}
	if bc {
		y = 1 - y
	}
	tm := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if tm.Year() != y || int(tm.Month()) != m || tm.Day() != d {
		return 0, "", stateDatetimeRange
	}
	return floorDiv(tm.Unix(), 86400), s[n+6:], ""
}

// dateTimeErr is err, from reading text in as a value of type typ (its
// name in Postgres: "date", "timestamp with time zone"), with Postgres's
// message for what is wrong (dateTimeTextError). A time zone that doesn't
// exist keeps its error.
func dateTimeErr(err error, typ, in string) error {
	state := sqlState(err)
	if state == stateInvalidParameter {
		return err
	}
	return dateTimeTextError(state, typ, in)
}

// parseTimestamp parses "YYYY-MM-DD[ T]HH:MM:SS[.fffffffff][Z|±HH[:MM]|zone]
// [BC]". It returns the date and time written, as seconds and nanoseconds
// since 1970-01-01 00:00:00, the number of fractional digits, and the
// offset (seconds east of UTC) if an explicit offset or zone was present
// (hasTZ). An offset is read as Postgres's DecodeTimezone reads it
// (parseTZDisplacement), and a zone name as by AT TIME ZONE (timezone.go),
// with its offset at that local time.
func parseTimestamp(s string) (local, nanos int64, digits int, offset int64, hasTZ bool, err error) {
	s = strings.TrimSpace(s)
	t, bc := splitEra(s)
	days, rest, bad := parseDatePrefix(t, bc)
	if bad != "" {
		return 0, 0, 0, 0, false, dateTimeTextError(bad, "timestamp", s)
	}
	rest = strings.TrimLeft(rest, " T")
	if rest != "" {
		clockEnd := len(rest)
		for i, c := range rest {
			if c == '+' || c == '-' || c == 'Z' || c == 'z' || c == ' ' {
				clockEnd = i
				break
			}
		}
		tz := strings.TrimSpace(rest[clockEnd:])
		rest = rest[:clockEnd]
		switch {
		case tz == "":
		case tz == "Z" || tz == "z" || strings.EqualFold(tz, "UTC"):
			hasTZ = true
		case tz[0] == '+' || tz[0] == '-':
			hasTZ = true
			off, bad := parseTZDisplacement(tz) // timetz.go
			if bad != "" {
				return 0, 0, 0, 0, false, dateTimeTextError(bad, "timestamp", s)
			}
			offset = int64(off)
		default:
			// Postgres splits the text at a comma, so a POSIX zone's rules
			// can't be written here.
			z, zerr := resolveZone(tz)
			if zerr != nil || strings.ContainsRune(tz, ',') {
				return 0, 0, 0, 0, false, unknownZoneText(tz, "timestamp", s)
			}
			hasTZ = true
			// The zone's offset at that local time, with DST gaps and
			// overlaps resolved as for AT TIME ZONE.
			var clockSec int64
			if rest != "" {
				var cerr error
				if clockSec, _, _, cerr = parseClock(rest); cerr != nil {
					return 0, 0, 0, 0, false, dateTimeErr(cerr, "timestamp", s)
				}
			}
			at := days*86400 + clockSec
			offset = at - z.localToUTC(at)
		}
	}
	var clockSec int64
	if rest != "" {
		clockSec, nanos, digits, err = parseClock(rest)
		if err != nil {
			return 0, 0, 0, 0, false, dateTimeErr(err, "timestamp", s)
		}
	}
	return days*86400 + clockSec, nanos, digits, offset, hasTZ, nil
}

func unitForDigits(digits int) arrow.TimeUnit {
	if digits > 6 {
		return arrow.Nanosecond
	}
	return arrow.Microsecond
}

// pgMicros is a fraction of a second (nanoseconds) in microseconds as
// Postgres reads one from text: rint(fraction × 10⁶), with the fraction as
// a double, so that an exact half rounds to even (.0000005 is 0, .0000015
// is 2, .1234565 is .123456). It is 1,000,000 for .9999995.
func pgMicros(nanos int64) int64 {
	return int64(math.RoundToEven(float64(nanos) / 1e9 * 1e6))
}

// inMicros is a time or timestamp read from text with more than 6 digits
// (in nanoseconds) as Postgres reads it, in microseconds (pgMicros).
func inMicros(v Value) Value {
	if v.T.Unit != arrow.Nanosecond {
		return v
	}
	sec, nanos := splitUnits(v.I, arrow.Nanosecond)
	v.T.Unit, v.I = arrow.Microsecond, sec*1_000_000+pgMicros(nanos)
	return v
}

// timestampLiteral builds a timestamp value from a SQL literal string.
func timestampLiteral(s string, withTZ bool) (Value, error) {
	return timestampLiteralIn(s, withTZ, utcZone)
}

// timestampLiteralIn is timestampLiteral in the session time zone z. As in
// Postgres, read as a timestamp with time zone, text with an offset or a
// zone is that instant, and text without one a local time in z; read as a
// timestamp, the text's offset or zone is checked and ignored, so
// '2024-01-05 10:00:00-05' is 10:00. The unit has the fraction's digits
// (microseconds, or nanoseconds for more than 6).
func timestampLiteralIn(s string, withTZ bool, z tzZone) (Value, error) {
	typ := "timestamp"
	if withTZ {
		typ = "timestamp with time zone"
	}
	sec, nanos, digits, offset, hasTZ, err := parseTimestamp(s)
	if err != nil {
		return Value{}, dateTimeErr(err, typ, s)
	}
	switch {
	case withTZ && hasTZ:
		sec -= offset
	case withTZ:
		sec = z.localToUTC(sec)
	}
	unit := unitForDigits(digits)
	v, err := secondsNanosToUnit(sec, nanos, unit)
	if err != nil && unit == arrow.Nanosecond {
		// Outside the nanoseconds' range (1677 to 2262): microseconds, as
		// Postgres reads them.
		unit = arrow.Microsecond
		v, err = secondsNanosToUnit(sec, pgMicros(nanos)*1000, unit)
	}
	if err != nil {
		// Past the microseconds' range, about ±292,000 years from 1970.
		return Value{}, stateErr(stateDatetimeRange, `timestamp out of range: "%s"`, s)
	}
	tz := ""
	if withTZ {
		tz = "UTC"
	}
	return intValue(timestampType(unit, tz), v), nil
}

// timeLiteral reads text as a TIME, as Postgres's time_in does: the time,
// which may follow a date and be followed by an offset or a time zone,
// checked as for a time with time zone (parseTimeTZ) and otherwise ignored
// ('2024-01-05 10:00:00+05' is 10:00:00).
func timeLiteral(s string) (Value, error) {
	_, _, clock, zone := timeTZParts(s)
	if zone != "" {
		if _, err := parseTimeTZ(s, utcZone); err != nil {
			return Value{}, dateTimeErr(err, "time", s)
		}
	}
	sec, nanos, digits, err := parseClock(clock)
	if err != nil {
		return Value{}, dateTimeErr(err, "time", s)
	}
	unit := unitForDigits(digits)
	v, err := secondsNanosToUnit(sec, nanos, unit)
	if err != nil {
		return Value{}, err
	}
	return intValue(timeType(unit), v), nil
}

// dateLiteral reads text as a DATE, as Postgres's date_in does: a date,
// which may be followed by a time, and an offset or a time zone, which are
// checked as a timestamp's and otherwise ignored ('2024-01-05 23:00-05' and
// '2024-01-05 24:00' are 2024-01-05).
func dateLiteral(s string) (Value, error) {
	t, bc := splitEra(s)
	days, rest, bad := parseDatePrefix(t, bc)
	if bad != "" {
		return Value{}, dateTimeTextError(bad, "date", s)
	}
	if strings.TrimSpace(rest) != "" {
		if _, _, _, _, _, err := parseTimestamp(s); err != nil {
			return Value{}, dateTimeErr(err, "date", s)
		}
	}
	if days < math.MinInt32 || days > math.MaxInt32 {
		// Past an Arrow date32's range (see dateValue).
		return Value{}, stateErr(stateDatetimeRange, `date out of range: "%s"`, s)
	}
	return intValue(typeDate, days), nil
}

// unknownZoneText is the error for timestamp text in, read as type typ,
// whose time zone zone doesn't exist. As in Postgres, a name with a slash
// is a zone name, `time zone "mars/base" not recognized` (22023, in lower
// case, as Postgres reads the text), and other text invalid syntax.
func unknownZoneText(zone, typ, in string) error {
	if strings.ContainsRune(zone, '/') && !strings.ContainsRune(zone, ',') {
		return stateErr(stateInvalidParameter, `time zone "%s" not recognized`, strings.ToLower(zone))
	}
	return dateTimeTextError(stateDatetimeFormat, typ, in)
}

// numberLiteral types a numeric literal: integers become BIGINT, literals
// with a decimal point become NUMERIC, literals with an exponent DOUBLE.
func numberLiteral(text string) (Value, error) {
	if strings.ContainsAny(text, "eE") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return Value{}, stateErr(stateInvalidText, "invalid number %q", text)
		}
		return floatValue(typeFloat64, f), nil
	}
	if !strings.Contains(text, ".") {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			t := typeInt64
			if i >= math.MinInt32 && i <= math.MaxInt32 {
				t = typeInt32
			}
			return intValue(t, i), nil
		}
	}
	d, scale, err := parseDecimal(text)
	if err != nil {
		return Value{}, err
	}
	if scale < 0 {
		d = rescaleDecimal(d, scale, 0)
		scale = 0
	}
	prec := max(decimalDigits(d), scale+1, 1)
	return decimalValue(d, prec, scale), nil
}

// ---- formatting ----
//
// Dates, times and timestamps are written as Postgres writes them with
// DateStyle ISO, whatever the declared precision: seconds always, then the
// fraction without trailing zeros (and no "." when it is zero), so
// 2024-01-10 10:00:00 and 2024-01-10 10:00:00.5. A timestamp with time zone
// is its local time in the session time zone, with that zone's offset
// (2024-01-10 02:00:00-08; +00 in UTC). A year before 1 AD is written as
// its BC year (astronomical year 0 is 0001-01-01 BC), and a year past 9999
// with all its digits.

// splitUnits splits v units since the epoch (or midnight) into whole
// seconds and nanoseconds.
func splitUnits(v int64, unit arrow.TimeUnit) (int64, int64) {
	per := unitsPerSecond[unit]
	sec := floorDiv(v, per)
	return sec, (v - sec*per) * (1_000_000_000 / per)
}

// fractionText is the fractional seconds of nanos without trailing zeros
// (".5", ".05", ".123456"), or "" when they are zero.
func fractionText(nanos int64) string {
	if nanos == 0 {
		return ""
	}
	return strings.TrimRight(fmt.Sprintf(".%09d", nanos), "0")
}

// dateText is tm's date as YYYY-MM-DD, with at least four year digits; bc
// reports a year before 1 AD, which is written as its BC year.
func dateText(tm time.Time) (string, bool) {
	y, m, d := tm.Date()
	bc := y <= 0
	if bc {
		y = 1 - y
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, int(m), d), bc
}

// formatOffset writes a UTC offset in seconds east as Postgres does: +00,
// +05:30, -08, +05:30:15. xsd is the ISO 8601 form of JSON, which always
// has the minutes (+00:00).
func formatOffset(secs int, xsd bool) string {
	sign := byte('+')
	if secs < 0 {
		sign, secs = '-', -secs
	}
	h, m, s := secs/3600, secs/60%60, secs%60
	switch {
	case s != 0:
		return fmt.Sprintf("%c%02d:%02d:%02d", sign, h, m, s)
	case m != 0 || xsd:
		return fmt.Sprintf("%c%02d:%02d", sign, h, m)
	}
	return fmt.Sprintf("%c%02d", sign, h)
}

// formatDate renders days since the epoch: 2024-01-10, 0044-03-15 BC.
func formatDate(days int64) string {
	s, bc := dateText(time.Unix(days*86400, 0).UTC())
	if bc {
		s += " BC"
	}
	return s
}

// formatTimestamp renders v units since the epoch, 2024-01-10 10:00:00.5,
// and with withTZ the offset off (seconds east of UTC) after the time:
// 2024-01-10 10:00:00+00. xsd is the ISO 8601 form of JSON, with a T
// between the date and the time and the offset as +00:00. A BC year ends
// with " BC", after the offset, in both forms.
func formatTimestamp(v int64, unit arrow.TimeUnit, withTZ bool, off int, xsd bool) string {
	sec, nanos := splitUnits(v, unit)
	tm := time.Unix(sec+int64(off), nanos).UTC()
	date, bc := dateText(tm)
	sep := " "
	if xsd {
		sep = "T"
	}
	out := date + sep + fmt.Sprintf("%02d:%02d:%02d", tm.Hour(), tm.Minute(), tm.Second()) + fractionText(nanos)
	if withTZ {
		out += formatOffset(off, xsd)
	}
	if bc {
		out += " BC"
	}
	return out
}

// formatTime renders v units since midnight: 10:00:00, 10:00:00.5.
func formatTime(v int64, unit arrow.TimeUnit) string {
	sec, nanos := splitUnits(v, unit)
	return fmt.Sprintf("%02d:%02d:%02d", sec/3600, (sec%3600)/60, sec%60) + fractionText(nanos)
}

// Text renders a value the way CAST(x AS VARCHAR) would in the time zone
// UTC.
func (v Value) Text() string { return v.textIn(utcZone) }

// textIn renders a value the way CAST(x AS VARCHAR) does in the session
// time zone z: a timestamp with time zone is its local time in z, with
// z's offset at that instant.
func (v Value) textIn(z tzZone) string {
	switch v.T.Kind {
	case KindBool:
		if v.I != 0 {
			return "true"
		}
		return "false"
	case KindInt16, KindInt32, KindInt64:
		return strconv.FormatInt(v.I, 10)
	case KindFloat32:
		return floatText(v.F, 32)
	case KindFloat64:
		return floatText(v.F, 64)
	case KindDecimal:
		return formatDecimal(v.D, v.T.Scale)
	case KindString:
		if v.T.Fixed {
			return trimPadding(v.S) // CHAR to text drops the padding
		}
		return v.S
	case KindBinary:
		return v.S
	case KindDate:
		return formatDate(v.I)
	case KindTime:
		return formatTime(v.I, v.T.Unit)
	case KindTimeTZ:
		return timeTZText(v)
	case KindTimestamp:
		return formatTimestamp(v.I, v.T.Unit, v.T.TZ != "", v.offsetIn(z), false)
	case KindInterval:
		return formatInterval(v)
	case KindRecord:
		return recordText(v, z)
	}
	return ""
}

// floatText renders a REAL (bits 32) or DOUBLE PRECISION (bits 64) value as
// Postgres's float4out / float8out do: NaN, Infinity and -Infinity, and
// otherwise the shortest form that reads back as the value.
func floatText(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	// Choose notation from the exponent of the shortest round-trip form.
	s := strconv.FormatFloat(f, 'e', -1, bits)
	i := strings.IndexByte(s, 'e')
	exponent, _ := strconv.Atoi(s[i+1:])
	upper := 15
	if bits == 32 {
		upper = 6
	}
	if exponent >= -4 && exponent < upper {
		return strconv.FormatFloat(f, 'f', -1, bits)
	}
	return s
}

// offsetIn is the offset (seconds east of UTC) a timestamp with time zone
// is shown with in the session time zone z, and 0 for other values.
func (v Value) offsetIn(z tzZone) int {
	if v.T.Kind != KindTimestamp || v.T.TZ == "" || z.isUTC() {
		return 0
	}
	return z.offsetAt(floorDiv(v.I, unitsPerSecond[v.T.Unit]))
}

// ---- coercion ----

func intRange(k Kind) (int64, int64) {
	switch k {
	case KindInt16:
		return math.MinInt16, math.MaxInt16
	case KindInt32:
		return math.MinInt32, math.MaxInt32
	default:
		return math.MinInt64, math.MaxInt64
	}
}

// intOutOfRange is Postgres's error for a number past the range of integer
// kind k: "integer out of range", or "smallint" or "bigint".
func intOutOfRange(k Kind) error {
	return stateErr(stateNumericRange, "%s out of range", intTypeName(k)) // intin.go
}

// realRangeError is Postgres's error for v, f as a double, that a REAL
// can't hold, or nil: a double past its range is "value out of range:
// overflow", and one that isn't 0 but rounds to it "value out of range:
// underflow" (float84 in float.c); a NUMERIC is read as its text is
// (numeric_float4), `"1000…0" is out of range for type real`.
func realRangeError(v Value, f float64) error {
	r := float64(float32(f))
	over, under := math.IsInf(r, 0) && !math.IsInf(f, 0), r == 0 && f != 0
	switch {
	case !over && !under:
		return nil
	case v.T.Kind == KindDecimal:
		return stateErr(stateNumericRange, `"%s" is out of range for type real`, formatDecimal(v.D, v.T.Scale))
	case over:
		return stateErr(stateNumericRange, "value out of range: overflow")
	}
	return stateErr(stateNumericRange, "value out of range: underflow")
}

func (v Value) asFloat() (float64, bool) {
	switch v.T.Kind {
	case KindInt16, KindInt32, KindInt64, KindBool:
		return float64(v.I), true
	case KindFloat32, KindFloat64:
		return v.F, true
	case KindDecimal:
		return decimalToFloat(v.D, v.T.Scale), true
	}
	return 0, false
}

// Coerce converts a value to the given type, as done on INSERT and CAST,
// in the time zone UTC (see coerceIn).
func Coerce(v Value, t ColType) (Value, error) { return coerceIn(v, t, utcZone) }

// coerceIn converts a value to the given type, as done on INSERT and CAST,
// in the session time zone z, as Postgres converts in its session time
// zone: a TIMESTAMP becomes a TIMESTAMP WITH TIME ZONE as a local time in
// z, a TIMESTAMP WITH TIME ZONE becomes its local time (also as a DATE or a
// TIME, and with z's offset as a TIME WITH TIME ZONE), a DATE is local
// midnight, a TIME becomes a TIME WITH TIME ZONE with z's offset today,
// text without an offset is read as a local time, and a timestamp with time
// zone as text has z's offset. A time or timestamp is rounded to t's
// fractional digits (roundUnit), so a TIMESTAMP(2) holds milliseconds
// rounded to 2 digits.
func coerceIn(v Value, t ColType, z tzZone) (Value, error) {
	if v.Null || t.Kind == KindNull {
		return nullValue(t), nil
	}
	fail := func() (Value, error) {
		// A value of a type that converts that doesn't fit (a DOUBLE past
		// BIGINT's range, or NaN) is numeric_value_out_of_range, and one of a
		// type that never converts (DATE to BOOLEAN) cannot_coerce, as in
		// Postgres.
		state := stateNumericRange
		if !castable(v.T, t) {
			state = stateCannotCoerce
		}
		return Value{}, stateErr(state, "cannot convert %s value %q to %s", v.T.Kind, v.textIn(z), t.SQLName())
	}
	if t.Kind == KindRecord && v.T.Kind == KindRecord {
		return coerceRecord(v, t, z)
	}
	// Parse strings into the target type first.
	if v.T.Kind == KindString && t.Kind != KindString && t.Kind != KindBinary {
		parsed, err := parseStringIn(v.S, t, z)
		if err != nil {
			return Value{}, err
		}
		return coerceIn(parsed, t, z)
	}
	switch t.Kind {
	case KindBool:
		switch {
		case v.T.Kind == KindBool:
			return v, nil
		case v.T.Kind.isInteger():
			return boolValue(v.I != 0), nil
		}
	case KindInt16, KindInt32, KindInt64:
		var i int64
		switch {
		case v.T.Kind.isInteger() || v.T.Kind == KindBool:
			i = v.I
		case v.T.Kind.isFloat():
			if math.IsNaN(v.F) || math.IsInf(v.F, 0) || v.F >= 9.223372036854775807e18 || v.F < -9.223372036854775808e18 {
				return Value{}, intOutOfRange(t.Kind)
			}
			i = int64(math.RoundToEven(v.F))
		case v.T.Kind == KindDecimal:
			r := rescaleDecimal(v.D, v.T.Scale, 0)
			if !r.IsInt64() {
				return Value{}, intOutOfRange(t.Kind)
			}
			i = r.Int64()
		default:
			return fail()
		}
		lo, hi := intRange(t.Kind)
		if i < lo || i > hi {
			return Value{}, intOutOfRange(t.Kind)
		}
		return intValue(t, i), nil
	case KindFloat32, KindFloat64:
		f, ok := v.asFloat()
		if !ok {
			return fail()
		}
		if t.Kind == KindFloat32 {
			if err := realRangeError(v, f); err != nil {
				return Value{}, err
			}
			f = float64(float32(f))
		}
		return floatValue(t, f), nil
	case KindDecimal:
		if t.Precision == 0 {
			if v.T.Kind == KindDecimal {
				return v, nil
			}
			if v.T.Kind.isInteger() {
				return decimalValue(big.NewInt(v.I), decimalDigits(big.NewInt(v.I)), 0), nil
			}
		}
		var d *big.Int
		var scale int32
		switch {
		case v.T.Kind.isInteger() || v.T.Kind == KindBool:
			d, scale = big.NewInt(v.I), 0
		case v.T.Kind == KindDecimal:
			d, scale = v.D, v.T.Scale
		case v.T.Kind.isFloat():
			if math.IsNaN(v.F) || math.IsInf(v.F, 0) {
				return Value{}, numericSpecialError(v.F, t)
			}
			var err error
			d, scale, err = parseDecimal(strconv.FormatFloat(v.F, 'f', -1, 64))
			if err != nil {
				return fail()
			}
		default:
			return fail()
		}
		r := rescaleDecimal(d, scale, t.Scale)
		if decimalDigits(r) > t.Precision && r.Sign() != 0 {
			return Value{}, numericFieldOverflow(t) // textin.go
		}
		return decimalValue(r, t.Precision, t.Scale), nil
	case KindString:
		// The length isn't checked here: casts cut (castValue) and writes
		// check it (fitLength). CHAR values are padded, and lose their
		// padding as other strings.
		switch {
		case t.Fixed && v.T.isChar():
			return charValue(v.S, t), nil
		case t.Fixed:
			return charValue(v.textIn(z), t), nil
		case v.T.Kind == KindString && !v.T.Fixed:
			return v, nil
		}
		return stringValue(v.textIn(z)), nil
	case KindBinary:
		if v.T.Kind == KindBinary || v.T.Kind == KindString {
			return binaryValue(v.S), nil
		}
	case KindDate:
		switch v.T.Kind {
		case KindDate:
			return v, nil
		case KindTimestamp:
			local, err := v.wallClock(z)
			if err != nil {
				return Value{}, err
			}
			return dateValue(floorDiv(local, unitsPerSecond[v.T.Unit]*86400))
		}
	case KindTimeTZ:
		switch {
		case v.T.Kind == KindTimeTZ:
			i, err := roundUnit(v.I, v.T.Unit, t.Unit, t.fracDigits(), 0)
			if err != nil {
				return Value{}, err
			}
			return Value{T: t, I: i, Off: v.Off}, nil
		case v.T.Kind == KindTime:
			return timeTZFromTime(v, t, z)
		case isTimestampTZ(v.T):
			return timeTZFromTimestamp(v, t, z)
		}
	case KindTime:
		if v.T.Kind == KindTime || v.T.Kind == KindTimeTZ {
			// A time with time zone's time, without its offset.
			i, err := roundUnit(v.I, v.T.Unit, t.Unit, t.fracDigits(), 0)
			if err != nil {
				return Value{}, err
			}
			return intValue(t, i), nil
		}
		if v.T.Kind == KindTimestamp {
			// The time of day, rounded: 23:59:59.9 is 24:00:00 in TIME(0),
			// as in Postgres.
			local, err := v.wallClock(z)
			if err != nil {
				return Value{}, err
			}
			per := unitsPerSecond[v.T.Unit] * 86400
			i, err := roundUnit(local-floorDiv(local, per)*per, v.T.Unit, t.Unit, t.fracDigits(), 0)
			if err != nil {
				return Value{}, err
			}
			return intValue(t, i), nil
		}
	case KindInterval:
		if v.T.Kind == KindInterval {
			return v, nil
		}
	case KindTimestamp:
		switch v.T.Kind {
		case KindTimestamp:
			i, err := v.I, error(nil)
			switch {
			case v.T.TZ == "" && t.TZ != "":
				i, err = z.utcUnits(i, v.T.Unit)
			case v.T.TZ != "" && t.TZ == "":
				i, err = z.localUnits(i, v.T.Unit)
			}
			if err == nil {
				i, err = roundUnit(i, v.T.Unit, t.Unit, t.fracDigits(), pgEpoch)
			}
			if err != nil {
				return Value{}, err
			}
			return intValue(t, i), nil
		case KindDate:
			i, err := secondsNanosToUnit(v.I*86400, 0, t.Unit)
			if err == nil && t.TZ != "" {
				i, err = z.utcUnits(i, t.Unit) // local midnight
			}
			if err != nil {
				return Value{}, err
			}
			return intValue(t, i), nil
		}
	}
	return fail()
}

// wallClock is a timestamp's wall-clock time in units since the epoch: its
// own for a TIMESTAMP, and for a TIMESTAMP WITH TIME ZONE the local time in
// the session time zone z.
func (v Value) wallClock(z tzZone) (int64, error) {
	if v.T.TZ == "" {
		return v.I, nil
	}
	return z.localUnits(v.I, v.T.Unit)
}

// castable reports whether Coerce converts some values of type from to type
// to; for the other combinations (such as DATE to BOOLEAN) it always fails.
func castable(from, to ColType) bool {
	f := from.Kind
	switch {
	case to.Kind == KindRecord:
		return f == KindRecord || f == KindNull // text can't be read as a record
	case f == KindRecord:
		return to.Kind == KindString || to.Kind == KindNull
	case f == KindNull || f == KindString || to.Kind == KindNull || to.Kind == KindString:
		return true
	case to.Kind == KindBool:
		return f == KindBool || f.isInteger()
	case to.Kind.isNumeric():
		return f.isNumeric() || f == KindBool
	case to.Kind == KindDate:
		return f == to.Kind || f == KindTimestamp
	case to.Kind == KindTime:
		return f == KindTime || f == KindTimeTZ || f == KindTimestamp
	case to.Kind == KindTimeTZ:
		return f == KindTimeTZ || f == KindTime || isTimestampTZ(from)
	case to.Kind == KindTimestamp:
		return f == KindTimestamp || f == KindDate
	}
	return f == to.Kind // BINARY, INTERVAL
}

// numericSpecialError is the error for a NaN or an infinity (f) converted to
// NUMERIC type t. NUMERIC values are fixed-point decimals here, which
// Postgres's numeric NaN and ±Infinity don't fit.
func numericSpecialError(f float64, t ColType) error {
	return stateErr(stateNumericRange, "cannot convert %s to %s: NUMERIC values can't be NaN or infinite", floatText(f, 64), t.SQLName())
}

// parseString converts text into a value of the given type.
func parseString(s string, t ColType) (Value, error) { return parseStringIn(s, t, utcZone) }

// parseStringIn converts text into a value of the given type, reading a
// timestamp with time zone without an offset as a local time in the
// session time zone z. Times and timestamps have the precision written
// (timestampLiteral), not t's; for a t of microseconds or fewer, text with
// more digits is read as Postgres reads it, in microseconds (pgMicros),
// before it is rounded to t (so '….4999995' is 10:00:01 in TIMESTAMP(0)).
func parseStringIn(s string, t ColType, z tzZone) (Value, error) {
	trimmed := strings.TrimSpace(s)
	switch t.Kind {
	case KindBool:
		b, err := parseBoolText(s) // textin.go
		if err != nil {
			return Value{}, err
		}
		return boolValue(b), nil
	case KindInt16, KindInt32, KindInt64:
		i, err := parseIntText(s, t.Kind) // intin.go
		if err != nil {
			return Value{}, err
		}
		return intValue(typeInt64, i), nil
	case KindFloat32, KindFloat64:
		f, err := parseFloatText(s, t.Kind) // textin.go
		if err != nil {
			return Value{}, err
		}
		return floatValue(t, f), nil
	case KindDecimal:
		return parseNumericText(s, t) // textin.go
	case KindDate:
		return dateLiteral(s)
	case KindTime, KindTimestamp:
		var v Value
		var err error
		if t.Kind == KindTime {
			v, err = timeLiteral(s)
		} else {
			v, err = timestampLiteralIn(s, t.TZ != "", z)
		}
		if err == nil && t.Unit != arrow.Nanosecond {
			v = inMicros(v)
		}
		return v, err
	case KindInterval:
		// Not trimmed: as in Postgres, ' P1D' isn't ISO 8601.
		return intervalLiteral(s)
	case KindTimeTZ:
		return parseTimeTZ(trimmed, z)
	case KindRecord:
		// As in Postgres, which can read text only as a named composite type.
		return Value{}, stateErr(stateFeatureNotSupported, "input of anonymous composite types is not implemented")
	}
	return Value{}, stateErr(stateInvalidText, "cannot convert %q to %s", s, t.SQLName())
}

// ---- storage encoding ----

// encodeStored renders a value (already coerced to its column type) as the
// string stored in the row HASH. A NaN is nanStored, which the index holds
// as +Infinity (see nan.go).
func encodeStored(v Value) string {
	switch v.T.Kind {
	case KindBool, KindInt16, KindInt32, KindInt64, KindDate, KindTime, KindTimestamp:
		return strconv.FormatInt(v.I, 10)
	case KindFloat32, KindFloat64:
		if math.IsNaN(v.F) {
			return nanStored
		}
		bits := 64
		if v.T.Kind == KindFloat32 {
			bits = 32
		}
		return strconv.FormatFloat(v.F, 'g', -1, bits)
	case KindDecimal:
		return formatDecimal(v.D, v.T.Scale)
	case KindInterval:
		return encodeInterval(v)
	case KindTimeTZ:
		return timeTZText(v) // its text, with its offset
	default:
		return v.S
	}
}

// decodeStored parses a string loaded from a row HASH, or a value RediSearch
// computed. A float's nanStored is NaN, and so are the "NaN" of earlier
// versions and RediSearch's -nan (see parseFloat).
func decodeStored(s string, t ColType) (Value, error) {
	if s == nanStored && t.Kind.isFloat() {
		return floatValue(t, math.NaN()), nil
	}
	switch t.Kind {
	case KindBool, KindInt16, KindInt32, KindInt64, KindDate, KindTime, KindTimestamp:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			// Values computed by RediSearch (e.g. reducers) may be floats.
			f, ferr := strconv.ParseFloat(s, 64)
			if ferr != nil {
				return Value{}, fmt.Errorf("corrupt %s value %q", t.Kind, s)
			}
			i = int64(f)
		}
		return intValue(t, i), nil
	case KindFloat32:
		f, err := parseFloat(s, 32)
		if err != nil {
			return Value{}, fmt.Errorf("corrupt float value %q", s)
		}
		return floatValue(t, f), nil
	case KindFloat64:
		f, err := parseFloat(s, 64)
		if err != nil {
			return Value{}, fmt.Errorf("corrupt double value %q", s)
		}
		return floatValue(t, f), nil
	case KindDecimal:
		d, scale, err := parseDecimal(s)
		if err != nil {
			return Value{}, err
		}
		return decimalValue(rescaleDecimal(d, scale, t.Scale), t.Precision, t.Scale), nil
	case KindBinary:
		return binaryValue(s), nil
	case KindInterval:
		return decodeInterval(s)
	case KindTimeTZ:
		v, err := parseTimeTZ(s, utcZone) // the text has an offset
		if err != nil {
			return Value{}, fmt.Errorf("corrupt %s value %q", t.Kind, s)
		}
		return Coerce(v, t)
	case KindString:
		if t.Fixed {
			return Value{T: t, S: s}, nil
		}
		return stringValue(s), nil
	default:
		return stringValue(s), nil
	}
}

// ---- comparison ----

// compareValues compares two non-null values in the time zone UTC (see
// compareIn); ok is false when the values are not comparable.
func compareValues(a, b Value) (int, bool) { return compareIn(a, b, utcZone) }

// textOperand converts text compared with a value of type t to t's type,
// in the session time zone z. A time or timestamp keeps the precision
// written, as Postgres compares at full precision: '10:00:00.5' is not
// equal to a TIMESTAMP(0) of 10:00:01 or 10:00:00.
func textOperand(s Value, t ColType, z tzZone) (Value, error) {
	if t.isTimeOfDay() || t.Kind == KindTimestamp {
		return parseStringIn(s.S, t, z)
	}
	return coerceIn(s, t, z)
}

// textOperandError is the error of text that doesn't convert to the type of
// the value it is compared with (i = '1.5' for an INTEGER i), or nil.
func textOperandError(a, b Value, z tzZone) error {
	isText := func(v Value) bool { return v.T.Kind == KindString || v.T.Kind == KindBinary }
	switch {
	case a.T.Kind == KindString && !isText(b):
		_, err := textOperand(a, b.T, z)
		return err
	case b.T.Kind == KindString && !isText(a):
		_, err := textOperand(b, a.T, z)
		return err
	}
	return nil
}

// compareIn compares two non-null values in the session time zone z, which
// reads text compared with a timestamp with time zone, and a TIMESTAMP or
// DATE compared with one, as local times; ok is false when the values are
// not comparable.
func compareIn(a, b Value, z tzZone) (int, bool) {
	ak, bk := a.T.Kind, b.T.Kind
	switch {
	case ak == KindRecord && bk == KindRecord:
		return compareRecords(a, b, z)
	case ak == KindRecord || bk == KindRecord:
		return 0, false
	case ak == KindInterval && bk == KindInterval:
		return intervalTotal(a).Cmp(intervalTotal(b)), true
	case (ak == KindString || ak == KindBinary) && (bk == KindString || bk == KindBinary):
		as, bs := a.S, b.S
		if a.T.isChar() || b.T.isChar() {
			// Trailing spaces don't count next to a CHAR value (lengths.go).
			if ak == KindString {
				as = trimPadding(as)
			}
			if bk == KindString {
				bs = trimPadding(bs)
			}
		}
		return bytes.Compare([]byte(as), []byte(bs)), true
	case ak == KindString && bk != KindString:
		pa, err := textOperand(a, b.T, z)
		if err != nil {
			return 0, false
		}
		return compareIn(pa, b, z)
	case bk == KindString && ak != KindString:
		pb, err := textOperand(b, a.T, z)
		if err != nil {
			return 0, false
		}
		return compareIn(a, pb, z)
	case (ak.isInteger() || ak == KindBool) && (bk.isInteger() || bk == KindBool):
		return cmpInt(a.I, b.I), true
	case ak == KindDecimal || bk == KindDecimal:
		if ak.isFloat() || bk.isFloat() {
			af, _ := a.asFloat()
			bf, _ := b.asFloat()
			return cmpFloat(af, bf), true
		}
		ad, as, ok1 := asDecimal(a)
		bd, bs, ok2 := asDecimal(b)
		if !ok1 || !ok2 {
			return 0, false
		}
		s := max(as, bs)
		return rescaleDecimal(ad, as, s).Cmp(rescaleDecimal(bd, bs, s)), true
	case ak.isNumeric() && bk.isNumeric():
		af, _ := a.asFloat()
		bf, _ := b.asFloat()
		return cmpFloat(af, bf), true
	case ak == bk && (ak == KindDate):
		return cmpInt(a.I, b.I), true
	case ak == KindTimeTZ && bk == KindTimeTZ:
		return compareTimeTZ(a, b), true
	case ak == KindTimeTZ && bk == KindTime, ak == KindTime && bk == KindTimeTZ:
		// A TIME is read as a time with time zone, with the session time
		// zone's offset today.
		return compareTimeTZ(asTimeTZ(a, z), asTimeTZ(b, z)), true
	case (ak == KindTime && bk == KindTime) || (ak == KindTimestamp && bk == KindTimestamp):
		if ak == KindTimestamp && (a.T.TZ == "") != (b.T.TZ == "") {
			// A TIMESTAMP compared with a TIMESTAMP WITH TIME ZONE is a
			// local time in z.
			var err error
			if a.T.TZ == "" {
				a, err = coerceIn(a, timestampType(a.T.Unit, "UTC"), z)
			} else {
				b, err = coerceIn(b, timestampType(b.T.Unit, "UTC"), z)
			}
			if err != nil {
				return 0, false
			}
		}
		// Seconds, then nanoseconds: exact for any units and range (a BC
		// timestamp has no nanoseconds' count).
		as, an := splitUnits(a.I, a.T.Unit)
		bs, bn := splitUnits(b.I, b.T.Unit)
		if c := cmpInt(as, bs); c != 0 {
			return c, true
		}
		return cmpInt(an, bn), true
	case ak == KindTimestamp && bk == KindDate:
		pb, err := coerceIn(b, a.T, z)
		if err != nil {
			return 0, false
		}
		return cmpInt(a.I, pb.I), true
	case ak == KindDate && bk == KindTimestamp:
		pa, err := coerceIn(a, b.T, z)
		if err != nil {
			return 0, false
		}
		return cmpInt(pa.I, b.I), true
	}
	return 0, false
}

func asDecimal(v Value) (*big.Int, int32, bool) {
	switch {
	case v.T.Kind == KindDecimal:
		return v.D, v.T.Scale, true
	case v.T.Kind.isInteger() || v.T.Kind == KindBool:
		return big.NewInt(v.I), 0, true
	}
	return nil, 0, false
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	case math.IsNaN(a) && !math.IsNaN(b):
		return 1
	case !math.IsNaN(a) && math.IsNaN(b):
		return -1
	}
	return 0
}
