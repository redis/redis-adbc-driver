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

// TO_CHAR(x, format) for dates, timestamps, intervals and times, as
// Postgres 16's formatting.c does, and the format templates that TO_DATE
// and TO_TIMESTAMP read (fromchar.go).
//
// A format is read left to right. At each position, an optional prefix
// modifier (FM or fm: no padding; TM or tm: translated names, which are the
// English ones, so no padding) is followed by the first template pattern of
// dchKeywords that the text starts with, then by an optional suffix (TH or
// th: an ordinal suffix on a number; SP: spelled out, which Postgres
// accepts and ignores too). Patterns are case-sensitive: there are upper-
// and lower-case spellings, and for names (MONTH, Month, month) the case is
// the output's. A prefix not followed by a pattern is dropped. Anything
// else is copied: text in double quotes (where \ escapes the next
// character), \" outside quotes (a literal "), and every other character.

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
)

// dchKeyword is a template pattern: its spelling in a format and the
// pattern it is (lower-case spellings of numbers are the same pattern).
type dchKeyword struct{ name, id string }

// dchKeywords are Postgres's template patterns, in the order it matches
// them: the first one the text starts with is the pattern.
var dchKeywords = func() []dchKeyword {
	var out []dchKeyword
	add := func(ids ...string) {
		for _, id := range ids {
			out = append(out, dchKeyword{name: id, id: id})
		}
	}
	lower := func(names ...string) {
		for _, n := range names {
			out = append(out, dchKeyword{name: strings.ToLower(n), id: n})
		}
	}
	add("A.D.", "A.M.", "AD", "AM", "B.C.", "BC", "CC", "DAY", "DDD", "DD", "DY", "Day", "Dy", "D",
		"FF1", "FF2", "FF3", "FF4", "FF5", "FF6", "FX", "HH24", "HH12", "HH",
		"IDDD", "ID", "IW", "IYYY", "IYY", "IY", "I", "J", "MI", "MM", "MONTH", "MON", "MS", "Month", "Mon",
		"OF", "P.M.", "PM", "Q", "RM")
	out = append(out, dchKeyword{"SSSSS", "SSSS"})
	add("SSSS", "SS", "TZH", "TZM", "TZ", "US", "WW", "W", "Y,YYY", "YYYY", "YYY", "YY", "Y")
	// The lower-case spellings: names and meridiem / era indicators give
	// lower-case output, numbers are the same patterns.
	add("a.d.", "a.m.", "ad", "am", "b.c.", "bc")
	lower("CC")
	add("day")
	lower("DDD", "DD")
	add("dy")
	lower("D", "FF1", "FF2", "FF3", "FF4", "FF5", "FF6", "FX", "HH24", "HH12", "HH",
		"IDDD", "ID", "IW", "IYYY", "IYY", "IY", "I", "J", "MI", "MM")
	add("month", "mon")
	lower("MS", "OF")
	add("p.m.", "pm")
	lower("Q")
	add("rm")
	out = append(out, dchKeyword{"sssss", "SSSS"})
	lower("SSSS", "SS", "TZH", "TZM")
	add("tz")
	lower("US", "WW", "W", "Y,YYY", "YYYY", "YYY", "YY", "Y")
	return out
}()

// fmtToken is one element of a format: a pattern with its modifiers, or a
// literal character.
type fmtToken struct {
	key    *dchKeyword // nil for literal text
	text   string      // the literal
	quoted bool        // the literal is in double quotes
	fm, tm bool        // FM (no padding), TM (translated names)
	th     byte        // 'T' for TH, 't' for th, 0 for neither
}

func matchKeyword(s string) *dchKeyword {
	for i := range dchKeywords {
		if strings.HasPrefix(s, dchKeywords[i].name) {
			return &dchKeywords[i]
		}
	}
	return nil
}

// tokenizeFormat splits a format into patterns and literal characters, as
// Postgres's parse_format does (see the top of the file).
func tokenizeFormat(f string) []fmtToken {
	var out []fmtToken
	for i := 0; i < len(f); {
		var fm, tm bool
		switch {
		case strings.HasPrefix(f[i:], "FM"), strings.HasPrefix(f[i:], "fm"):
			fm = true
			i += 2
		case strings.HasPrefix(f[i:], "TM"), strings.HasPrefix(f[i:], "tm"):
			tm = true
			i += 2
		}
		if i >= len(f) {
			break
		}
		if k := matchKeyword(f[i:]); k != nil {
			t := fmtToken{key: k, fm: fm, tm: tm}
			i += len(k.name)
			switch {
			case strings.HasPrefix(f[i:], "TH"):
				t.th = 'T'
				i += 2
			case strings.HasPrefix(f[i:], "th"):
				t.th = 't'
				i += 2
			case strings.HasPrefix(f[i:], "SP"):
				i += 2
			}
			out = append(out, t)
			continue
		}
		if f[i] == '"' {
			// Quoted text, where a backslash escapes the next character.
			for i++; i < len(f); {
				if f[i] == '"' {
					i++
					break
				}
				if f[i] == '\\' && i+1 < len(f) {
					i++
				}
				_, n := utf8.DecodeRuneInString(f[i:])
				out = append(out, fmtToken{text: f[i : i+n], quoted: true})
				i += n
			}
			continue
		}
		if f[i] == '\\' && i+1 < len(f) && f[i+1] == '"' {
			i++ // \" is a literal "
		}
		_, n := utf8.DecodeRuneInString(f[i:])
		out = append(out, fmtToken{text: f[i : i+n]})
		i += n
	}
	return out
}

// ordinal appends the English ordinal suffix to the number s, in upper
// case for TH: 1st, 2nd, 3rd, 4th, 11th, 12th, 13th, 21st.
func ordinal(s string, th byte) string {
	suffix := "th"
	if n := len(s); n > 0 && (n < 2 || s[n-2] != '1') {
		switch s[n-1] {
		case '1':
			suffix = "st"
		case '2':
			suffix = "nd"
		case '3':
			suffix = "rd"
		}
	}
	if th == 'T' {
		suffix = strings.ToUpper(suffix)
	}
	return s + suffix
}

// romanMonths are the months in Roman numerals, for RM.
var romanMonths = []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}

// charFields are the fields TO_CHAR formats, as Postgres's fmt_tm: a
// timestamp's local date and time, or an interval's fields.
type charFields struct {
	interval bool
	// The astronomical year (0 is 1 BC), the month (1 to 12) and the day of
	// the month; for an interval its years, months (-11 to 11) and days.
	year, mon, mday int
	hour            int64 // an interval's hours, which can pass 23
	min, sec        int
	nanos           int // the fraction of a second, in nanoseconds
	wday            int // the day of the week, 0 for Sunday
	yday            int // the day of the year
	// The ISO 8601 year, week and day of that year, and the Julian day.
	isoYear, isoWeek, isoYday, julian int
	off                               int    // the UTC offset in seconds, for TZH, TZM and OF
	tzName                            string // the zone's abbreviation, for TZ
}

// timeFields are the fields of a local date and time.
func timeFields(tm time.Time) charFields {
	isoY, isoW := tm.ISOWeek()
	isoDow := (int(tm.Weekday())+6)%7 + 1
	return charFields{
		year: tm.Year(), mon: int(tm.Month()), mday: tm.Day(),
		hour: int64(tm.Hour()), min: tm.Minute(), sec: tm.Second(), nanos: tm.Nanosecond(),
		wday: int(tm.Weekday()), yday: tm.YearDay(),
		isoYear: isoY, isoWeek: isoW, isoYday: (isoW-1)*7 + isoDow,
		// Days since November 24, 4714 BC.
		julian: int(floorDiv(tm.Unix(), 86400) + 2440588),
	}
}

// intervalFields are an interval's fields, as Postgres's interval_to_char
// takes them: the years and the months left over (both with the sign of the
// interval's months), the days, and the hours, minutes, seconds and
// fraction of the time (all with its sign). The day of the year is the
// length in days, with a month as 30.
// The ISO fields and the Julian day are Postgres's date arithmetic on the
// years, months and days, which aren't a date (0000-00-00 for a time).
func intervalFields(v Value) charFields {
	y, m, d := v.Months/12, v.Months%12, v.Days
	isoY := pgDate2IsoYear(y, m, d)
	return charFields{
		interval: true,
		year:     int(y), mon: int(m), mday: int(d),
		hour:  v.I / nsPerHour,
		min:   int(v.I % nsPerHour / nsPerMinute),
		sec:   int(v.I % nsPerMinute / nsPerSecond),
		nanos: int(v.I % nsPerSecond),
		// In int32, which wraps, as in Postgres (built with -fwrapv).
		yday:    int(v.Months*30 + d),
		isoYear: int(isoY), isoWeek: int(pgDate2IsoWeek(y, m, d)),
		isoYday: int(pgDate2j(y, m, d) - pgIsoWeek2j(isoY, 1) + 1),
		julian:  int(pgDate2j(y, m, d)),
	}
}

// pgDate2j is Postgres's date2j, the Julian day of a date, in its int32
// arithmetic (Go's int32 wraps, as Postgres's int does with -fwrapv, and
// divides towards zero, as C does).
func pgDate2j(year, month, day int32) int32 {
	if month > 2 {
		month++
		year += 4800
	} else {
		month += 13
		year += 4799
	}
	century := year / 100
	julian := year*365 - 32167
	julian += year/4 - century + century/4
	return julian + 7834*month/256 + day
}

// pgJ2Day is Postgres's j2day: the day of the week of a Julian day, 0 for
// Sunday.
func pgJ2Day(j int32) int32 {
	j = (j + 1) % 7
	if j < 0 {
		j += 7
	}
	return j
}

// pgIsoWeek2j is Postgres's isoweek2j: the Julian day of the Monday of an
// ISO year's week.
func pgIsoWeek2j(year, week int32) int32 {
	day4 := pgDate2j(year, 1, 4)
	return (week-1)*7 + day4 - pgJ2Day(day4-1)
}

// pgDate2IsoWeek is Postgres's date2isoweek: the ISO week of a date.
func pgDate2IsoWeek(year, mon, mday int32) int32 {
	w, _ := pgIsoWeek(year, mon, mday)
	return w
}

// pgDate2IsoYear is Postgres's date2isoyear: the ISO year of a date.
func pgDate2IsoYear(year, mon, mday int32) int32 {
	_, y := pgIsoWeek(year, mon, mday)
	return y
}

// pgIsoWeek is the ISO week of a date, as Postgres's date2isoweek computes
// it, and its ISO year, as date2isoyear does.
func pgIsoWeek(year, mon, mday int32) (week, isoYear int32) {
	dayn := pgDate2j(year, mon, mday)
	isoYear = year
	week1 := pgIsoWeek2j(year, 1)
	if dayn < week1 {
		// In the previous year's last week.
		isoYear--
		week1 = pgIsoWeek2j(isoYear, 1)
	}
	week = (dayn-week1)/7 + 1
	if week >= 52 {
		// The last days of a year can be in the next year's first week.
		// date2isoweek looks at the year after the date's, date2isoyear at
		// the one after the ISO year, which differ only for an interval's
		// fields.
		if next := pgIsoWeek2j(year+1, 1); dayn >= next {
			week = (dayn-next)/7 + 1
		}
		if dayn >= pgIsoWeek2j(isoYear+1, 1) {
			isoYear++
		}
	}
	return week, isoYear
}

// toChar formats a date, time, timestamp or interval as Postgres's TO_CHAR
// does, in the session time zone z. A timestamp with time zone is formatted
// as its local time in z, a date as one at local midnight (Postgres
// converts it to one): TZ is z's abbreviation there (UTC, PST, IST), and
// TZH, TZM and OF its offset (+00, 00 and +00 in UTC). For a timestamp TZ is
// empty and the offset +00.
//
// An interval is formatted from its own fields (intervalFields): YYYY is
// its years, MM its months and DD its days, HH24 can pass 23, and the
// fields of a negative interval are negative. The patterns that need a
// calendar date (names, D, ID, eras and time zones) are an error. A time
// is formatted as the interval since midnight: Postgres has no TO_CHAR of a
// time, and casts it to an interval.
func toChar(v Value, format string, z tzZone) (string, error) {
	switch v.T.Kind {
	case KindInterval:
		return formatChar(format, intervalFields(v))
	case KindTime:
		return formatChar(format, intervalFields(Value{T: typeInterval, I: timeOfDay(v)}))
	case KindDate:
		// As the timestamp with time zone of its local midnight (01:00 if
		// midnight is in a DST gap).
		var err error
		if v, err = coerceIn(v, timestampType(arrow.Second, "UTC"), z); err != nil {
			return "", err
		}
	}
	tm, err := toTime(v)
	if err != nil {
		return "", err
	}
	tzName, off := "", 0
	if isTimestampTZ(v.T) {
		off, tzName = z.offsetAt(tm.Unix()), z.abbrevAt(tm.Unix())
		tm = tm.Add(time.Duration(off) * time.Second)
	}
	f := timeFields(tm)
	f.off, f.tzName = off, strings.ToUpper(tzName)
	return formatChar(format, f)
}

// formatChar writes the fields f with a format, as Postgres's DCH_to_char.
func formatChar(format string, f charFields) (string, error) {
	// The UTC offset, for TZH, TZM and OF.
	offSign, offAbs := '+', f.off
	if f.off < 0 {
		offSign, offAbs = '-', -f.off
	}
	// year is a year as YYYY writes it: a date's with its era (1 BC is 1),
	// an interval's as it is.
	year := func(y int) int {
		if !f.interval && y <= 0 {
			return 1 - y
		}
		return y
	}
	// wide is a width, one more for a negative number's sign.
	wide := func(width int, neg bool) int {
		if neg {
			return width + 1
		}
		return width
	}
	var month, day string
	if f.mon >= 1 && f.mon <= 12 {
		month = time.Month(f.mon).String()
	}
	if !f.interval {
		day = time.Weekday(f.wday).String()
	}

	var b strings.Builder
	for _, t := range tokenizeFormat(format) {
		if t.key == nil {
			b.WriteString(t.text)
			continue
		}
		// num writes a number zero-padded to width (no padding with FM),
		// with its ordinal suffix for TH / th.
		num := func(n int64, width int) {
			if t.fm {
				width = 0
			}
			s := fmt.Sprintf("%0*d", width, n)
			if t.th != 0 {
				s = ordinal(s, t.th)
			}
			b.WriteString(s)
		}
		// fixed writes a number always padded to width (fractions of a
		// second, which FM doesn't change).
		fixed := func(n, width int) {
			s := fmt.Sprintf("%0*d", width, n)
			if t.th != 0 {
				s = ordinal(s, t.th)
			}
			b.WriteString(s)
		}
		// name writes a name padded to 9 characters (no padding with FM or
		// TM).
		name := func(s string) {
			if t.fm || t.tm {
				b.WriteString(s)
			} else {
				fmt.Fprintf(&b, "%-9s", s)
			}
		}
		pick := func(cond bool, yes, no string) {
			if cond {
				b.WriteString(yes)
			} else {
				b.WriteString(no)
			}
		}
		switch t.key.id {
		case "TZ", "tz", "TZH", "TZM", "OF", "AD", "BC", "ad", "bc", "A.D.", "B.C.", "a.d.", "b.c.",
			"MONTH", "Month", "month", "MON", "Mon", "mon", "DAY", "Day", "day", "DY", "Dy", "dy", "D", "ID":
			if f.interval {
				return "", stateErr(stateDatetimeFormat, "invalid format specification for an interval value")
			}
		}
		pm := f.hour%24 >= 12
		y := year(f.year)
		switch t.key.id {
		case "AM", "PM":
			pick(pm, "PM", "AM")
		case "am", "pm":
			pick(pm, "pm", "am")
		case "A.M.", "P.M.":
			pick(pm, "P.M.", "A.M.")
		case "a.m.", "p.m.":
			pick(pm, "p.m.", "a.m.")
		case "HH", "HH12":
			// On a 12-hour clock, also for an interval.
			h := f.hour % 12
			if h == 0 {
				h = 12
			}
			num(h, wide(2, f.hour < 0))
		case "HH24":
			num(f.hour, wide(2, f.hour < 0))
		case "MI":
			num(int64(f.min), wide(2, f.min < 0))
		case "SS":
			num(int64(f.sec), wide(2, f.sec < 0))
		case "MS", "FF3":
			fixed(f.nanos/1_000_000, 3)
		case "US", "FF6":
			fixed(f.nanos/1_000, 6)
		case "FF1":
			fixed(f.nanos/100_000_000, 1)
		case "FF2":
			fixed(f.nanos/10_000_000, 2)
		case "FF4":
			fixed(f.nanos/100_000, 4)
		case "FF5":
			fixed(f.nanos/10_000, 5)
		case "SSSS":
			num(f.hour*3600+int64(f.min)*60+int64(f.sec), 0)
		case "TZ":
			b.WriteString(f.tzName)
		case "tz":
			b.WriteString(strings.ToLower(f.tzName))
		case "TZH":
			fmt.Fprintf(&b, "%c%02d", offSign, offAbs/3600)
		case "TZM":
			fmt.Fprintf(&b, "%02d", offAbs%3600/60)
		case "OF":
			width := 2
			if t.fm {
				width = 0
			}
			fmt.Fprintf(&b, "%c%0*d", offSign, width, offAbs/3600)
			if offAbs%3600 != 0 {
				fmt.Fprintf(&b, ":%02d", offAbs%3600/60)
			}
		case "AD", "BC":
			pick(f.year <= 0, "BC", "AD")
		case "ad", "bc":
			pick(f.year <= 0, "bc", "ad")
		case "A.D.", "B.C.":
			pick(f.year <= 0, "B.C.", "A.D.")
		case "a.d.", "b.c.":
			pick(f.year <= 0, "b.c.", "a.d.")
		case "MONTH":
			name(strings.ToUpper(month))
		case "Month":
			name(month)
		case "month":
			name(strings.ToLower(month))
		case "MON":
			b.WriteString(strings.ToUpper(month[:3]))
		case "Mon":
			b.WriteString(month[:3])
		case "mon":
			b.WriteString(strings.ToLower(month[:3]))
		case "MM":
			num(int64(f.mon), wide(2, f.mon < 0))
		case "DAY":
			name(strings.ToUpper(day))
		case "Day":
			name(day)
		case "day":
			name(strings.ToLower(day))
		case "DY":
			b.WriteString(strings.ToUpper(day[:3]))
		case "Dy":
			b.WriteString(day[:3])
		case "dy":
			b.WriteString(strings.ToLower(day[:3]))
		case "DDD":
			num(int64(f.yday), 3)
		case "IDDD":
			num(int64(f.isoYday), 3)
		case "DD":
			num(int64(f.mday), 2)
		case "D":
			num(int64(f.wday)+1, 0)
		case "ID":
			num(int64((f.wday+6)%7+1), 0)
		case "WW":
			// In int32, as in Postgres: an interval's -2147483648 days are
			// week 306783379.
			num(int64((int32(f.yday)-1)/7+1), 2)
		case "IW":
			num(int64(f.isoWeek), 2)
		case "W":
			num(int64((int32(f.mday)-1)/7+1), 0)
		case "Q":
			// Nothing for an interval without months.
			if f.mon != 0 {
				num(int64((f.mon-1)/3+1), 0)
			}
		case "CC":
			// A date's century starts on 2001-01-01, and the 1st century BC
			// is -01; an interval's is its years / 100.
			c := f.year / 100
			switch {
			case f.interval:
			case f.year > 0:
				c = (f.year-1)/100 + 1
			default:
				c = f.year/100 - 1
			}
			width := wide(2, c < 0)
			if c > 99 || c < -99 {
				width = 0
			}
			num(int64(c), width)
		case "Y,YYY":
			s := fmt.Sprintf("%d,%03d", y/1000, y%1000)
			if t.th != 0 {
				s = ordinal(s, t.th)
			}
			b.WriteString(s)
		case "YYYY":
			num(int64(y), wide(4, y < 0))
		case "IYYY":
			num(int64(year(f.isoYear)), wide(4, y < 0))
		case "YYY":
			num(int64(y%1000), wide(3, y < 0))
		case "IYY":
			num(int64(year(f.isoYear)%1000), wide(3, y < 0))
		case "YY":
			num(int64(y%100), wide(2, y < 0))
		case "IY":
			num(int64(year(f.isoYear)%100), wide(2, y < 0))
		case "Y":
			num(int64(y%10), 1)
		case "I":
			num(int64(year(f.isoYear)%10), 1)
		case "RM", "rm":
			// Padded to 4 characters, as in Postgres (no padding with FM).
			// An interval's negative months count back from December (-1 is
			// XII, -2 XI), and whole years are XII (I when negative); without
			// months or years it is nothing.
			m := f.mon
			switch {
			case m < 0:
				m += 13
			case m == 0 && f.year == 0:
				continue
			case m == 0 && f.year > 0:
				m = 12
			case m == 0:
				m = 1
			}
			s := romanMonths[m-1]
			if t.key.id == "rm" {
				s = strings.ToLower(s)
			}
			if !t.fm {
				s = fmt.Sprintf("%-4s", s)
			}
			b.WriteString(s)
		case "J":
			num(int64(f.julian), 0)
		case "FX":
			// Fixed format: only for TO_DATE and TO_TIMESTAMP.
		default:
			return "", fmt.Errorf("TO_CHAR pattern %q is not supported", t.key.name)
		}
	}
	return b.String(), nil
}
