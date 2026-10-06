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

// TIME WITH TIME ZONE (TIMETZ), as Postgres 16 has it.
//
// A value is a time of day, 00:00:00 to 24:00:00 in microseconds (TIME(p)
// WITH TIME ZONE holds p digits, up to 6), and a UTC offset of up to
// ±15:59:59, in seconds east of Greenwich (Value.Off). It isn't an instant:
// the offset is kept as written, and no date goes with it.
//
//   - Text in: a time, then an offset (+02, -05:30, +0530, Z) or a zone
//     abbreviation (EST, CEST, MSK), as timetz_in reads them. Without one,
//     the offset is the session time zone's on the current date (the
//     statement's), as in Postgres: '10:00' is 10:00:00-04 in New York in
//     July and 10:00:00-05 in January. A zone name (America/New_York) needs
//     a date before the time ('2024-07-01 10:00 America/New_York', whose
//     offset is the one on that date), unless the zone has only ever had one
//     offset (Etc/GMT+5).
//   - Text out: the time, then the offset (10:00:00+02, 10:00:00.5-05:30,
//     10:00:00-04:56:02).
//   - Comparison: by the time in UTC, unwrapped (23:00-05 is 28:00 UTC, so
//     it sorts after 04:00+00), then by the offset, larger offsets first, as
//     Postgres's timetz_cmp. So '10:00+02' and '09:00+01' are different
//     values (Postgres's = is false), and '10:00+02' < '09:00+01'. Equal
//     values have the same time and offset, which the hash keys of DISTINCT,
//     GROUP BY, IN sets and hash joins use (timeTZKey).
//   - Conversions: a TIME takes the session time zone's offset on the
//     current date; a timestamp with time zone becomes its local time and
//     offset in the session time zone; a time with time zone becomes a TIME
//     by dropping its offset. A TIMESTAMP, a DATE and other types don't
//     convert, as in Postgres.
//   - x AT TIME ZONE z of a time or a time with time zone gives the same
//     instant's time in z, with z's offset at the statement's time (a TIME
//     is read as a time with time zone first), wrapped into a day.
//   - Arithmetic: ± an interval moves the time by the interval's time part,
//     wrapping around midnight (months and days are ignored), and keeps the
//     offset; a DATE plus a time with time zone is a timestamp with time
//     zone.
//   - CURRENT_TIME is one: the local time and offset in the session time
//     zone. LOCALTIME is a TIME.
//
// Arrow has no such type, so a column of one is a string column (utf8) of
// its text, with the field metadata ARROW:FLIGHT:SQL:TYPE_NAME naming the
// type (arrowField). Postgres's own ADBC driver has no mapping either (it
// returns Postgres's binary form, as an opaque extension type). Rows store
// the text, and the index doesn't hold such a column, so comparisons with
// it are checked in the driver.

import (
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

const (
	// maxTimeTZDigits is the fractional digits a time with time zone holds:
	// microseconds, as in Postgres.
	maxTimeTZDigits = 6
	// maxTZDisplacement is the largest UTC offset of a time with time zone,
	// 15:59:59, Postgres's TZDISP_LIMIT less a second.
	maxTZDisplacement = 15*3600 + 59*60 + 59
	// typeNameKey is the Arrow field metadata key that names a column's SQL
	// type, the one Flight SQL uses (see arrowField).
	typeNameKey = "ARROW:FLIGHT:SQL:TYPE_NAME"
)

// typeTimeTZ is TIME WITH TIME ZONE, TIME(6) WITH TIME ZONE.
var typeTimeTZ = fracType(KindTimeTZ, maxTimeTZDigits, "")

// timeTZText is the text of a time with time zone: 10:00:00+02.
func timeTZText(v Value) string {
	return formatTime(v.I, v.T.Unit) + formatOffset(int(v.Off), false)
}

// errTimeTZSyntax is Postgres's error for text that isn't a time with time
// zone.
func errTimeTZSyntax(s string) error {
	return stateErr(stateDatetimeFormat, "invalid input syntax for type time with time zone: %q", s)
}

// parseTimeTZ reads text as a time with time zone (see the top of the file),
// in microseconds, in the session time zone z: [date [T]] time [zone]. The
// date, if any, decides a zone's offset; the time is HH:MM[:SS[.fraction]],
// up to 24:00:00, with a fraction of more than 6 digits rounded as Postgres
// reads it (pgMicros).
func parseTimeTZ(in string, z tzZone) (Value, error) {
	days, hasDate, clock, name := timeTZParts(in)
	sec, nanos, _, err := parseClock(clock)
	if err != nil {
		if clockShaped(clock) {
			// 25:00, 10:60: a field out of range.
			return Value{}, stateErr(stateDatetimeRange, "date/time field value out of range: %q", in)
		}
		return Value{}, errTimeTZSyntax(in)
	}
	us := sec*1_000_000 + pgMicros(nanos)
	if us > 86400*1_000_000 {
		return Value{}, stateErr(stateDatetimeRange, "date/time field value out of range: %q", in)
	}
	// offsetOn is zone's offset at the local time sec on the date, or on the
	// current date in the session time zone.
	offsetOn := func(zone tzZone) int {
		d := days
		if !hasDate {
			d = z.today()
		}
		local := d*86400 + sec
		return int(local - zone.localToUTC(local))
	}
	var off int
	switch {
	case name == "":
		off = offsetOn(z)
	case name[0] == '+' || name[0] == '-':
		if off, err = parseTZDisplacement(name, in); err != nil {
			return Value{}, err
		}
	default:
		// Postgres splits the text at a comma, so a POSIX zone's rules can't
		// be written here.
		zone, err := resolveZone(name)
		if err != nil || strings.ContainsRune(name, ',') {
			return Value{}, errTimeTZSyntax(in)
		}
		switch single, ok := zone.singleOffset(); {
		case ok:
			off = single
		case zone.dyn != nil || hasDate:
			// An abbreviation tied to a zone (MSK) has its offset on the
			// current date without a date, as in Postgres; a zone name needs
			// one.
			off = offsetOn(zone)
		default:
			return Value{}, errTimeTZSyntax(in)
		}
	}
	if off < -maxTZDisplacement || off > maxTZDisplacement {
		return Value{}, stateErr(stateTZDisplacement, "time zone displacement out of range: %q", in)
	}
	return Value{T: typeTimeTZ, I: us, Off: int32(off)}, nil
}

// timeTZParts splits time with time zone text into its date (days since
// the epoch), if it has one, its clock and its zone, "" if it has none.
func timeTZParts(in string) (days int64, hasDate bool, clock, zone string) {
	s := strings.TrimSpace(in)
	if d, rest, bad := parseDatePrefix(s, false); bad == "" {
		days, hasDate, s = d, true, strings.TrimLeft(rest, " ")
	}
	if len(s) > 1 && (s[0] == 'T' || s[0] == 't') && s[1] >= '0' && s[1] <= '9' {
		s = s[1:] // ISO 8601's T before the time
	}
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == ':' || s[end] == '.') {
		end++
	}
	return days, hasDate, s[:end], strings.TrimSpace(s[end:])
}

// clockShaped reports whether s is written as a clock, HH:MM[:SS[.f]],
// whatever its fields' values.
func clockShaped(s string) bool {
	clock, frac, hasFrac := strings.Cut(s, ".")
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 || hasFrac && (len(parts) != 3 || strings.Trim(frac, "0123456789") != "") {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// timeTZLocal reports whether time with time zone text has no zone, so that
// its offset is the session time zone's.
func timeTZLocal(s string) bool {
	_, _, _, zone := timeTZParts(s)
	return zone == ""
}

// readsLocalTime reports whether text without an offset read as a value of
// type t is a local time in the session time zone: t is a timestamp with
// time zone or a time with time zone.
func readsLocalTime(t ColType) bool { return isTimestampTZ(t) || t.Kind == KindTimeTZ }

// parseTZDisplacement reads a UTC offset written as numbers, as Postgres's
// DecodeTimezone does: ±H, ±HH, ±HHMM (more than two digits are hours and
// minutes), ±HH:MM or ±HH:MM:SS, with hours up to 15. in is the whole text,
// for errors.
func parseTZDisplacement(s, in string) (int, error) {
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	num := func(t string) (int, string, bool) {
		n := 0
		for n < len(t) && t[n] >= '0' && t[n] <= '9' {
			n++
		}
		if n == 0 || n > 9 {
			return 0, t, false
		}
		v, _ := strconv.Atoi(t[:n])
		return v, t[n:], true
	}
	digits := s[1:]
	h, rest, ok := num(digits)
	if !ok {
		return 0, errTimeTZSyntax(in)
	}
	m, sec := 0, 0
	switch {
	case strings.HasPrefix(rest, ":"):
		if m, rest, ok = num(rest[1:]); !ok {
			return 0, errTimeTZSyntax(in)
		}
		if strings.HasPrefix(rest, ":") {
			if sec, rest, ok = num(rest[1:]); !ok {
				return 0, errTimeTZSyntax(in)
			}
		}
	case rest == "" && len(digits) > 2:
		h, m = h/100, h%100
	}
	if rest != "" {
		return 0, errTimeTZSyntax(in)
	}
	if h > 15 || m > 59 || sec > 59 {
		return 0, stateErr(stateTZDisplacement, "time zone displacement out of range: %q", in)
	}
	return sign * (h*3600 + m*60 + sec), nil
}

// currentTime is the statement's current time in z (seconds since the
// epoch), or the clock's.
func (z tzZone) currentTime() int64 {
	if z.hasNow {
		return z.now
	}
	return time.Now().Unix()
}

// today is the current date in z, in days since the epoch.
func (z tzZone) today() int64 {
	now := z.currentTime()
	return floorDiv(now+int64(z.offsetAt(now)), 86400)
}

// singleOffset is the zone's offset if it has only ever had one, as
// Postgres's pg_get_timezone_offset finds it (a zone whose abbreviation
// changed but not its offset has one).
func (z tzZone) singleOffset() (int, bool) {
	switch {
	case z.fixed():
		return z.off, true
	case z.rule != nil:
		return z.rule.dstOff, z.rule.always
	case z.dyn != nil:
		return 0, false
	}
	tm := time.Unix(-1<<50, 0).In(z.loc)
	_, off := tm.Zone()
	for range 10000 {
		_, end := tm.ZoneBounds()
		if end.IsZero() {
			return off, true
		}
		tm = end.In(z.loc)
		if _, o := tm.Zone(); o != off {
			return 0, false
		}
	}
	return 0, false
}

// timeTZFromTime is the time with time zone of type t of a TIME v: the
// session time zone's offset at that local time on the current date, as
// Postgres's time_timetz (rounded to t's digits).
func timeTZFromTime(v Value, t ColType, z tzZone) (Value, error) {
	i, err := roundUnit(v.I, v.T.Unit, t.Unit, t.fracDigits(), 0)
	if err != nil {
		return Value{}, err
	}
	local := z.today()*86400 + floorDiv(v.I, unitsPerSecond[v.T.Unit])
	return Value{T: t, I: i, Off: int32(local - z.localToUTC(local))}, nil
}

// timeTZFromTimestamp is the time with time zone of type t of a timestamp
// with time zone v: its local time and offset in the session time zone z,
// as Postgres's timestamptz_timetz (rounded to t's digits).
func timeTZFromTimestamp(v Value, t ColType, z tzZone) (Value, error) {
	local, err := v.wallClock(z)
	if err != nil {
		return Value{}, err
	}
	per := unitsPerSecond[v.T.Unit] * 86400
	i, err := roundUnit(local-floorDiv(local, per)*per, v.T.Unit, t.Unit, t.fracDigits(), 0)
	if err != nil {
		return Value{}, err
	}
	return Value{T: t, I: i, Off: int32(v.offsetIn(z))}, nil
}

// asTimeTZ converts a TIME compared with a time with time zone to one, in
// the session time zone z, without rounding it.
func asTimeTZ(v Value, z tzZone) Value {
	if v.T.Kind != KindTime {
		return v
	}
	t := v.T
	t.Kind, t.Precision = KindTimeTZ, 0
	local := z.today()*86400 + floorDiv(v.I, unitsPerSecond[v.T.Unit])
	return Value{T: t, I: v.I, Off: int32(local - z.localToUTC(local))}
}

// compareTimeTZ compares two times with time zone as Postgres's timetz_cmp:
// by their times in UTC, then the one with the larger offset first.
func compareTimeTZ(a, b Value) int {
	ua := timeOfDay(a) - int64(a.Off)*nsPerSecond
	ub := timeOfDay(b) - int64(b.Off)*nsPerSecond
	if c := cmpInt(ua, ub); c != 0 {
		return c
	}
	return cmpInt(int64(b.Off), int64(a.Off))
}

// timeTZKey is the key of a time with time zone in hash tables: its time in
// nanoseconds and its offset, which compare equal exactly when these are.
func timeTZKey(v Value) string {
	return "tz" + strconv.FormatInt(timeOfDay(v), 10) + "|" + strconv.FormatInt(int64(v.Off), 10)
}

// timeTZAtZone is TIMEZONE(z, x) of a time with time zone x: the time of
// the same instant with z's offset at the statement's time (now), as
// Postgres's timetz_zone, wrapped into a day. t is the result type.
func timeTZAtZone(x Value, zone tzZone, now int64, t ColType) Value {
	off := zone.offsetAt(now)
	per := unitsPerSecond[x.T.Unit]
	day := per * 86400
	i := x.I + int64(off-int(x.Off))*per
	i -= floorDiv(i, day) * day
	return Value{T: t, I: i, Off: int32(off)}
}

// timeTZPart is EXTRACT(field FROM x) of a time with time zone, as
// Postgres's timetz_part: the time's fields, epoch (the seconds of the time
// in UTC), and timezone, timezone_hour and timezone_minute (the offset,
// east of Greenwich).
func timeTZPart(field string, v Value) (Value, error) {
	sec, nanos := splitUnits(v.I, v.T.Unit)
	i := func(n int64) (Value, error) { return intValue(typeInt64, n), nil }
	off := int64(v.Off)
	switch normalizeField(field) {
	case "hour":
		return i(sec / 3600)
	case "minute":
		return i(sec % 3600 / 60)
	case "second":
		return floatValue(typeFloat64, float64(sec%60)+float64(nanos)/1e9), nil
	case "milliseconds":
		return floatValue(typeFloat64, float64(sec%60*1000)+float64(nanos)/1e6), nil
	case "microseconds":
		return i(sec%60*1_000_000 + nanos/1000)
	case "epoch":
		return floatValue(typeFloat64, float64(sec-off)+float64(nanos)/1e9), nil
	case "timezone":
		return i(off)
	case "timezone_hour":
		return i(off / 3600)
	case "timezone_minute":
		return i(off / 60 % 60)
	case "year", "month", "day", "dow", "doy", "isodow", "isoyear", "week", "quarter", "decade", "century",
		"millennium", "julian":
		return Value{}, stateErr(stateFeatureNotSupported, "unit %q not supported for type time with time zone", strings.ToLower(field))
	}
	return Value{}, stateErr(stateInvalidParameter, "unit %q not recognized for type time with time zone", strings.ToLower(field))
}

// timeTZPlusInterval is x ± the time part of an interval (sign 1 or -1),
// wrapped into a day, with x's offset: Postgres's timetz_pl_interval and
// timetz_mi_interval. t is the result type.
func timeTZPlusInterval(x, iv Value, sign int64, t ColType) Value {
	ns := timeOfDay(x) + sign*(iv.I%nsPerDay) // reduced first, so it can't overflow
	ns -= floorDiv(ns, nsPerDay) * nsPerDay
	return Value{T: t, I: ns / (nsPerSecond / unitsPerSecond[t.Unit]), Off: x.Off}
}

// dateAtTimeTZ is DATE d + time with time zone x: the instant of x's time on
// d at x's offset, a timestamp with time zone of type t (Postgres's
// datetimetz_timestamptz).
func dateAtTimeTZ(d, x Value, t ColType) (Value, error) {
	tm := time.Unix(d.I*86400-int64(x.Off), 0).UTC().Add(time.Duration(timeOfDay(x)))
	return fromTime(tm, t)
}

// arrowField is the Arrow field of a column of type t: a time with time
// zone, which Arrow has no type for, is its text with typeNameKey naming
// its type (TIME(6) WITH TIME ZONE).
func arrowField(name string, t ColType, nullable bool) arrow.Field {
	f := arrow.Field{Name: name, Type: t.ArrowType(), Nullable: nullable}
	if t.Kind == KindTimeTZ {
		f.Metadata = arrow.NewMetadata([]string{typeNameKey}, []string{t.SQLName()})
	}
	return f
}

// colTypeFromField is colTypeFromArrow for a field, which may be a time
// with time zone's text (a string field whose typeNameKey names one, as
// arrowField makes it).
func colTypeFromField(f arrow.Field) (ColType, error) {
	switch f.Type.ID() {
	case arrow.STRING, arrow.LARGE_STRING, arrow.STRING_VIEW:
		if i := f.Metadata.FindKey(typeNameKey); i >= 0 {
			if t, ok := timeTZTypeName(f.Metadata.Values()[i]); ok {
				return t, nil
			}
		}
	}
	return colTypeFromArrow(f.Type)
}

// timeTZTypeName reads a type name that names a time with time zone.
func timeTZTypeName(name string) (ColType, bool) {
	toks, err := lex(name)
	if err != nil {
		return ColType{}, false
	}
	p := &parser{src: name, toks: toks}
	spec, err := p.parseTypeSpec()
	if err != nil || p.peek().kind != tokEOF {
		return ColType{}, false
	}
	t, err := colTypeFromSQL(spec)
	return t, err == nil && t.Kind == KindTimeTZ
}
