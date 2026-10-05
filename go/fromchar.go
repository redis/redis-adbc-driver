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

// TO_DATE(text, format) and TO_TIMESTAMP(text, format) read text with a
// TO_CHAR format as Postgres 16 does (formatting.c's DCH_from_char and
// do_to_timestamp). The format is split into patterns and literals as for
// TO_CHAR (tokenizeFormat), and the text is read left to right with it:
//
//   - A pattern reads its field. A number reads exactly as many characters
//     as the pattern has (four for YYYY, two for MM) when a number pattern
//     or a digit comes next in the format, so 'YYYYMMDD' reads '20240315'.
//     Otherwise, or with FM or TH, it reads all the digits there are, after
//     an optional sign ('-44' for YYYY). TH skips the two characters after
//     the number. Names (MONTH, DAY, AM, BC, RM) match in any case.
//   - Outside FX mode, spaces before a field are skipped, and so are spaces
//     after one. A space or separator in the format (an ASCII character
//     other than a letter, a digit or a space) skips one space or separator
//     of the text, if there is one there; any other literal character, and
//     each character of quoted text, skips one character of the text,
//     unless spaces were skipped before it. In FX mode every literal skips
//     exactly one character.
//   - Reading stops at the end of the text or of the format, so the rest of
//     the other is ignored.
//
// The fields are then combined into a date and time (fromChar.result).
// A field that isn't given, or is given as 0, takes its default: year 0
// (1 BC), January, the 1st, midnight.

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
)

// The date conventions of patterns: a format can use Gregorian dates (YYYY,
// MM, DD, WW, …) or ISO week dates (IYYY, IW, ID, IDDD), not both.
const (
	fcGregorian = 1
	fcISOWeek   = 2
)

// fromCharModes are the conventions of the patterns that have one.
var fromCharModes = func() map[string]int {
	m := map[string]int{}
	for _, id := range []string{"DDD", "DD", "D", "MM", "MONTH", "MON", "Month", "Mon", "month", "mon",
		"RM", "rm", "WW", "W", "Y,YYY", "YYYY", "YYY", "YY", "Y"} {
		m[id] = fcGregorian
	}
	for _, id := range []string{"IDDD", "ID", "IW", "IYYY", "IYY", "IY", "I"} {
		m[id] = fcISOWeek
	}
	return m
}()

// fromCharNonDigit are the patterns whose text doesn't start with a digit
// (Postgres's is_digit is false for them): a number before one of them
// reads all its digits.
var fromCharNonDigit = func() map[string]bool {
	m := map[string]bool{}
	for _, id := range []string{"A.D.", "A.M.", "AD", "AM", "B.C.", "BC", "DAY", "DY", "Day", "Dy", "FX",
		"MONTH", "MON", "Month", "Mon", "OF", "P.M.", "PM", "RM", "TZH", "TZ",
		"a.d.", "a.m.", "ad", "am", "b.c.", "bc", "day", "dy", "month", "mon", "p.m.", "pm", "rm", "tz"} {
		m[id] = true
	}
	return m
}()

// The names the patterns read, in the order Postgres tries them (the first
// one the text starts with, in any case, is the value).
var (
	ampmWords, ampmDotWords                        = []string{"am", "pm"}, []string{"a.m.", "p.m."}
	eraWords, eraDotWords                          = []string{"ad", "bc"}, []string{"a.d.", "b.c."}
	monthWords, monthAbbrevs, dayWords, dayAbbrevs []string
	// romanMonthWords are XII down to I, so that XII is read before XI.
	romanMonthWords []string
)

func init() {
	for m := time.January; m <= time.December; m++ {
		monthWords = append(monthWords, m.String())
		monthAbbrevs = append(monthAbbrevs, m.String()[:3])
		romanMonthWords = append(romanMonthWords, romanMonths[12-m])
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		dayWords = append(dayWords, d.String())
		dayAbbrevs = append(dayAbbrevs, d.String()[:3])
	}
}

// fromChar holds the fields read from the text, as Postgres's TmFromChar:
// 0 is a field not given. d is the day of the week, 1 for Sunday.
type fromChar struct {
	mode                     int
	hh, pm, mi, ss, ssss     int
	d, dd, ddd, mm, ww, w, j int
	year, bc, cc, yysz       int
	ms, us, ff               int
	tzsign, tzh, tzm         int
	clock12                  bool
}

// The kinds of nodes of a format.
const (
	fcAction    = iota // a pattern
	fcSpace            // an unquoted space
	fcSeparator        // an unquoted ASCII character other than a letter, a digit or a space
	fcChar             // any other character, and quoted ones
)

type fcNode struct {
	kind int
	tok  fmtToken
}

// isCSpace is C's isspace.
func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func isSeparatorChar(c byte) bool {
	return c > ' ' && c < 0x7f && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9')
}

// fromCharNodes are a format's nodes, as Postgres's parse_format makes them.
func fromCharNodes(format string) []fcNode {
	toks := tokenizeFormat(format)
	nodes := make([]fcNode, len(toks))
	for i, t := range toks {
		kind := fcAction
		if t.key == nil {
			switch c := t.text[0]; {
			case t.quoted:
				kind = fcChar
			case isSeparatorChar(c):
				kind = fcSeparator
			case isCSpace(c):
				kind = fcSpace
			default:
				kind = fcChar
			}
		}
		nodes[i] = fcNode{kind, t}
	}
	return nodes
}

// fcParser reads the text s, at s[i:], with a format's nodes into f.
type fcParser struct {
	s     string
	i     int
	nodes []fcNode
	f     fromChar
}

// charLen is the length of the character at s[i:] (0 at the end).
func charLen(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	return n
}

// readFromChar reads the fields of s with format, as Postgres's
// DCH_from_char (see the top of the file).
func readFromChar(s, format string) (*fromChar, error) {
	p := &fcParser{s: s, nodes: fromCharNodes(format)}
	fx := false
	extra := 0 // spaces skipped beyond those the format has (Postgres's extra_skip)
	skipSpaces := func() {
		for p.i < len(s) && isCSpace(s[p.i]) {
			p.i++
			extra++
		}
	}
	for k := 0; k < len(p.nodes) && p.i < len(s); k++ {
		n := p.nodes[k]
		isFX := n.kind == fcAction && n.tok.key.id == "FX"
		if !fx && !isFX && (n.kind == fcAction || k == 0) {
			skipSpaces()
		}
		switch n.kind {
		case fcSpace, fcSeparator:
			if fx {
				p.i += charLen(s, p.i)
				continue
			}
			// One space or separator of the text, if there is one.
			extra--
			if p.i < len(s) && (isCSpace(s[p.i]) || isSeparatorChar(s[p.i])) {
				p.i++
				extra++
			}
			continue
		case fcChar:
			if !fx && extra > 0 {
				extra--
			} else {
				p.i += charLen(s, p.i)
			}
			continue
		}
		if isFX {
			fx = true
			continue
		}
		if err := p.field(k, extra); err != nil {
			return nil, err
		}
		if !fx {
			extra = 0
			skipSpaces()
		}
	}
	return &p.f, nil
}

// setField sets a field, which can be given twice only with the same value
// (Postgres's from_char_set_int).
func setField(dest *int, v int, name string) error {
	if *dest != 0 && *dest != v {
		return fmt.Errorf(`conflicting values for "%s" field in formatting string`, name)
	}
	*dest = v
	return nil
}

// field reads the field of pattern node k; extra is the count of spaces
// skipped before it.
func (p *fcParser) field(k, extra int) error {
	t := p.nodes[k].tok
	name, f := t.key.name, &p.f
	if m := fromCharModes[t.key.id]; m != 0 {
		if f.mode == 0 {
			f.mode = m
		} else if f.mode != m {
			return errors.New("invalid combination of date conventions")
		}
	}
	// num reads a number into dest, the pattern's width wide.
	num := func(dest *int) error {
		_, err := p.number(dest, len(name), k)
		return err
	}
	// word reads one of words into dest as its index plus add.
	word := func(dest *int, words []string, add int) error {
		v, err := p.word(words, name)
		if err != nil {
			return err
		}
		return setField(dest, v+add, name)
	}
	var err error
	th := true // a number skips the two characters of a TH suffix
	switch id := t.key.id; id {
	case "AM", "PM", "am", "pm":
		err, th = word(&f.pm, ampmWords, 0), false
		f.clock12 = true
	case "A.M.", "P.M.", "a.m.", "p.m.":
		err, th = word(&f.pm, ampmDotWords, 0), false
		f.clock12 = true
	case "HH", "HH12":
		_, err = p.number(&f.hh, 2, k)
		f.clock12 = true
	case "HH24":
		_, err = p.number(&f.hh, 2, k)
	case "MI":
		err = num(&f.mi)
	case "SS":
		err = num(&f.ss)
	case "SSSS":
		err = num(&f.ssss)
	case "MS":
		// The digits are a fraction: 5 and 500 are 500 ms, 005 is 5.
		var n int
		if n, err = p.number(&f.ms, 3, k); err == nil {
			f.ms *= fracScale(n, 3)
		}
	case "US", "FF1", "FF2", "FF3", "FF4", "FF5", "FF6":
		width := 6
		if id != "US" {
			f.ff = int(id[2] - '0')
			width = f.ff
		}
		var n int
		if n, err = p.number(&f.us, width, k); err == nil {
			f.us *= fracScale(n, 6)
		}
	case "TZ", "tz", "OF":
		err = fmt.Errorf(`formatting field "%s" is only supported in to_char`, name)
	case "TZH":
		// A sign, or a space for +. A '-' before the number that the
		// format read as a separator is the sign too, if spaces were
		// skipped after it.
		th = false
		switch {
		case p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-' || p.s[p.i] == ' '):
			f.tzsign = 1
			if p.s[p.i] == '-' {
				f.tzsign = -1
			}
			p.i++
		case extra > 0 && p.s[p.i-1] == '-':
			f.tzsign = -1
		default:
			f.tzsign = 1
		}
		_, err = p.number(&f.tzh, 2, k)
	case "TZM":
		th = false
		if f.tzsign == 0 {
			f.tzsign = 1
		}
		_, err = p.number(&f.tzm, 2, k)
	case "AD", "BC", "ad", "bc":
		err, th = word(&f.bc, eraWords, 0), false
	case "A.D.", "B.C.", "a.d.", "b.c.":
		err, th = word(&f.bc, eraDotWords, 0), false
	case "MONTH", "Month", "month":
		err, th = word(&f.mm, monthWords, 1), false
	case "MON", "Mon", "mon":
		err, th = word(&f.mm, monthAbbrevs, 1), false
	case "RM", "rm":
		var v int
		if v, err = p.word(romanMonthWords, name); err == nil {
			err = setField(&f.mm, 12-v, name)
		}
		th = false
	case "MM":
		err = num(&f.mm)
	case "DAY", "Day", "day", "DY", "Dy", "dy":
		words := dayWords
		if len(id) == 2 {
			words = dayAbbrevs
		}
		if err = word(&f.d, words, 0); err == nil {
			f.d++
		}
		th = false
	case "DDD":
		err = num(&f.ddd)
	case "IDDD":
		_, err = p.number(&f.ddd, 3, k)
	case "DD":
		err = num(&f.dd)
	case "D":
		err = num(&f.d)
	case "ID":
		// ISO days are 1 for Monday to 7 for Sunday; d is 1 for Sunday.
		if _, err = p.number(&f.d, 1, k); err == nil {
			if f.d++; f.d > 7 {
				f.d = 1
			}
		}
	case "WW", "IW":
		err = num(&f.ww)
	case "W":
		err = num(&f.w)
	case "Q":
		// Read, and ignored, as in Postgres.
		err = num(nil)
	case "J":
		err = num(&f.j)
	case "CC":
		err = num(&f.cc)
	case "Y,YYY":
		err = p.yCommaYYY()
	case "YYYY", "IYYY":
		err = num(&f.year)
		f.yysz = 4
	case "YYY", "IYY", "YY", "IY", "Y", "I":
		// Fewer than four digits are a year near 2020.
		var n int
		if n, err = p.number(&f.year, len(name), k); err == nil && n < 4 {
			f.year = adjustYearTo2020(f.year)
		}
		f.yysz = len(name)
	}
	if err == nil && th && t.th != 0 {
		p.i += charLen(p.s, p.i)
		p.i += charLen(p.s, p.i)
	}
	return err
}

// fracScale is what a fraction of n digits (of a field of digits) is
// multiplied by to have digits digits.
func fracScale(n, digits int) int {
	if n < digits {
		return int(intPow10[digits-n])
	}
	return 1
}

// adjustYearTo2020 is the year a number of fewer than four digits is, as
// Postgres's adjust_partial_year_to_2020: 0 to 69 are 2000 to 2069, 70 to
// 99 are 1970 to 1999, 100 to 519 are 2100 to 2519, 520 to 999 are 1520 to
// 1999.
func adjustYearTo2020(y int) int {
	switch {
	case y < 70:
		return y + 2000
	case y < 100:
		return y + 1900
	case y < 520:
		return y + 2000
	case y < 1000:
		return y + 1000
	}
	return y
}

// strtol reads an integer at s[i:] as C's strtol does: spaces, a sign, then
// digits. end is i if there are no digits. A value far outside the int32
// range stops growing, staying outside it.
func strtol(s string, i int) (v int64, end int) {
	j := i
	for j < len(s) && isCSpace(s[j]) {
		j++
	}
	neg := j < len(s) && s[j] == '-'
	if j < len(s) && (s[j] == '-' || s[j] == '+') {
		j++
	}
	start := j
	for ; j < len(s) && s[j] >= '0' && s[j] <= '9'; j++ {
		if v < 1<<40 {
			v = v*10 + int64(s[j]-'0')
		}
	}
	if j == start {
		return 0, i
	}
	if neg {
		v = -v
	}
	return v, j
}

// nextIsSeparator reports whether the text of the node after k can't start
// with a digit, so that a number at k reads all its digits (Postgres's
// is_next_separator). The end of the format counts as one, and so does TH.
func (p *fcParser) nextIsSeparator(k int) bool {
	if p.nodes[k].tok.th != 0 || k+1 >= len(p.nodes) {
		return true
	}
	n := p.nodes[k+1]
	if n.kind == fcAction {
		return fromCharNonDigit[n.tok.key.id]
	}
	return len(n.tok.text) != 1 || n.tok.text[0] < '0' || n.tok.text[0] > '9'
}

// number reads the number of pattern node k into dest (nil: read and drop
// it), width characters wide, as Postgres's from_char_parse_int_len (see the
// top of the file). It returns the count of characters read, spaces
// included.
func (p *fcParser) number(dest *int, width, k int) (int, error) {
	t := p.nodes[k].tok
	name := t.key.name
	start := p.i
	for p.i < len(p.s) && isCSpace(p.s[p.i]) {
		p.i++
	}
	rest := p.s[p.i:]
	shown := rest // the text the errors show
	if len(shown) > width {
		shown = strings.ToValidUTF8(shown[:width], "")
	}
	var v int64
	if t.fm || p.nextIsSeparator(k) {
		v, p.i = strtol(p.s, start)
	} else {
		if len(rest) < width {
			return 0, fmt.Errorf(`source string too short for "%s" formatting field`, name)
		}
		var used int
		v, used = strtol(rest[:width], 0)
		if used > 0 && used < width {
			return 0, fmt.Errorf(`invalid value "%s" for "%s"`, shown, name)
		}
		p.i += used
	}
	if p.i == start {
		return 0, fmt.Errorf(`invalid value "%s" for "%s"`, shown, name)
	}
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf(`value for "%s" in source string is out of range`, name)
	}
	if dest != nil {
		if err := setField(dest, int(v), name); err != nil {
			return 0, err
		}
	}
	return p.i - start, nil
}

// word reads the first of words that the text starts with, in any case,
// and returns its index.
func (p *fcParser) word(words []string, name string) (int, error) {
	rest := p.s[p.i:]
	for v, w := range words {
		if len(rest) >= len(w) && asciiEqualFold(rest[:len(w)], w) {
			p.i += len(w)
			return v, nil
		}
	}
	if j := strings.IndexAny(rest, " \t\n\r\f\v"); j >= 0 {
		rest = rest[:j]
	}
	return 0, fmt.Errorf(`invalid value "%s" for "%s"`, rest, name)
}

func asciiEqualFold(a, b string) bool {
	lower := func(c byte) byte {
		if c >= 'A' && c <= 'Z' {
			return c + 'a' - 'A'
		}
		return c
	}
	for i := 0; i < len(a); i++ {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

// yCommaYYY reads Y,YYY as Postgres does, with sscanf's "%d,%03d": a number,
// a comma, and up to three more characters of a number.
func (p *fcParser) yCommaYYY() error {
	// scan reads a number after spaces, of at most width characters with
	// its sign (0: any).
	scan := func(i, width int) (int64, int, bool) {
		for i < len(p.s) && isCSpace(p.s[i]) {
			i++
		}
		end := len(p.s)
		if width > 0 {
			end = min(end, i+width)
		}
		v, j := strtol(p.s[:end], i)
		return v, j, j > i
	}
	if thousands, i, ok := scan(p.i, 0); ok && i < len(p.s) && p.s[i] == ',' {
		if units, j, ok := scan(i+1, 3); ok {
			y := max(math.MinInt32, min(math.MaxInt32, thousands*1000+units))
			if err := setField(&p.f.year, int(y), "Y,YYY"); err != nil {
				return err
			}
			p.f.yysz = 4
			p.i = j
			return nil
		}
	}
	return errors.New(`invalid input string for "Y,YYY"`)
}

// fromCharTime is the date and time that TO_DATE and TO_TIMESTAMP read.
type fromCharTime struct {
	year, mon, mday      int // the year is astronomical: 0 is 1 BC
	hour, min, sec, usec int
	hasOffset            bool
	offset               int // TZH and TZM: seconds east of UTC
	prec                 int // FF1 to FF6: the digits to round to (0: none)
}

// The date fields given, for the checks of Postgres's ValidateDate.
const (
	fcYear = 1 << iota
	fcMonth
	fcDay
	fcDate = fcYear | fcMonth | fcDay
)

// clampYear keeps a year within what time.Date computes: one this far out
// is out of every range.
func clampYear(y int) int { return max(-1_000_000_000, min(1_000_000_000, y)) }

// civilDays is the days since the epoch of a date (the day overflowing
// into the next months, as Postgres's date2j does).
func civilDays(y, m, d int) int64 {
	return floorDiv(time.Date(clampYear(y), time.Month(m), d, 0, 0, 0, 0, time.UTC).Unix(), 86400)
}

func civilDate(days int64) (y, m, d int) {
	tm := time.Unix(days*86400, 0).UTC()
	return tm.Year(), int(tm.Month()), tm.Day()
}

// isoWeekStart is the days since the epoch of the Monday of ISO week w of
// year y (week 1 has January 4).
func isoWeekStart(y, w int) int64 {
	jan4 := civilDays(y, 1, 4)
	sinceMonday := jan4 + 3 - floorDiv(jan4+3, 7)*7 // day 0 was a Thursday
	return jan4 - sinceMonday + int64(w-1)*7
}

// daysBefore are the days of a year before each month, and in all of it,
// in a common and a leap year.
var daysBefore = [2][13]int{
	{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334, 365},
	{0, 31, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335, 366},
}

// daysBeforeIn is daysBefore for year y (astronomical).
func daysBeforeIn(y int) [13]int {
	if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		return daysBefore[1]
	}
	return daysBefore[0]
}

// result combines the fields read from the text s into a date and time, as
// Postgres's do_to_timestamp.
func (f *fromChar) result(s string) (fromCharTime, error) {
	r := fromCharTime{mon: 1, mday: 1}
	outOfRange := func() error { return fmt.Errorf(`date/time field value out of range: "%s"`, s) }
	if f.ssss != 0 {
		r.hour, r.min, r.sec = f.ssss/3600, f.ssss%3600/60, f.ssss%60
	}
	if f.ss != 0 {
		r.sec = f.ss
	}
	if f.mi != 0 {
		r.min = f.mi
	}
	if f.hh != 0 {
		r.hour = f.hh
	}
	if f.clock12 {
		if r.hour < 1 || r.hour > 12 {
			return r, fmt.Errorf(`hour "%d" is invalid for the 12-hour clock`, r.hour)
		}
		if f.pm != 0 && r.hour < 12 {
			r.hour += 12
		} else if f.pm == 0 && r.hour == 12 {
			r.hour = 0
		}
	}
	given := 0
	cc := f.cc
	if f.bc != 0 {
		cc = -cc
	}
	switch {
	case f.year != 0 && cc != 0 && f.yysz <= 2:
		// YY or Y is the year in century CC. The 21st century is 2001 to
		// 2100, the 6th century BC 600 BC to 501 BC.
		switch y := f.year % 100; {
		case y == 0 && cc >= 0:
			r.year = cc * 100
		case y == 0:
			r.year = cc*100 + 1
		case cc >= 0:
			r.year = (cc-1)*100 + y
		default:
			r.year = (cc+1)*100 - y + 1
		}
		given |= fcYear
	case f.year != 0:
		// A year without CC, or of three or more digits, which CC doesn't
		// change. A negative one is BC, and BC negates it.
		r.year = f.year
		if f.bc != 0 {
			r.year = -r.year
		}
		if r.year < 0 {
			r.year++
		}
		given |= fcYear
	case cc != 0:
		// The first year of the century.
		r.year = cc*100 + 1
		if cc >= 0 {
			r.year -= 100
		}
		given |= fcYear
	}
	if f.j != 0 {
		// The Julian day: day 0 is November 24, 4714 BC.
		r.year, r.mon, r.mday = civilDate(int64(f.j) - 2440588)
		given |= fcDate
	}
	ddd, dd := f.ddd, f.dd
	if f.ww != 0 {
		if f.mode == fcISOWeek {
			// The Monday of the ISO week, or its day d.
			day := isoWeekStart(r.year, f.ww)
			if f.d > 1 {
				day += int64(f.d) - 2
			} else if f.d != 0 {
				day += 6
			}
			r.year, r.mon, r.mday = civilDate(day)
			given |= fcDate
		} else {
			ddd = (f.ww-1)*7 + 1 // the week's first day
		}
	}
	if f.w != 0 {
		dd = (f.w-1)*7 + 1
	}
	if dd != 0 {
		r.mday = dd
		given |= fcDay
	}
	if f.mm != 0 {
		r.mon = f.mm
		given |= fcMonth
	}
	if ddd != 0 && (r.mon <= 1 || r.mday <= 1) {
		// The day of the year sets the month and the day not given.
		if r.year == 0 && f.bc == 0 {
			return r, errors.New("cannot calculate day of year without year information")
		}
		if f.mode == fcISOWeek {
			r.year, r.mon, r.mday = civilDate(isoWeekStart(r.year, 1) - 1 + int64(ddd))
			given |= fcDate
		} else {
			before := daysBeforeIn(r.year)
			m := 1
			for m <= 12 && ddd > before[m] {
				m++
			}
			if r.mon <= 1 {
				r.mon = m
			}
			if r.mday <= 1 {
				r.mday = ddd - before[m-1]
			}
			given |= fcMonth | fcDay
		}
	}
	usec := int64(f.ms)*1000 + int64(f.us)
	// The day is checked against the month only with a year, a month and
	// a day; otherwise it overflows into the next months.
	if given&fcMonth != 0 && (r.mon < 1 || r.mon > 12) || given&fcDay != 0 && (r.mday < 1 || r.mday > 31) {
		return r, outOfRange()
	}
	if before := daysBeforeIn(r.year); given == fcDate && r.mday > before[r.mon]-before[r.mon-1] {
		return r, outOfRange()
	}
	if r.hour < 0 || r.hour > 23 || r.min < 0 || r.min > 59 || r.sec < 0 || r.sec > 59 || usec < 0 || usec >= 1_000_000 {
		return r, outOfRange()
	}
	r.usec = int(usec)
	if f.tzsign != 0 {
		if f.tzh < 0 || f.tzh > 15 || f.tzm < 0 || f.tzm > 59 {
			return r, fmt.Errorf(`time zone displacement out of range: "%s"`, s)
		}
		r.hasOffset, r.offset = true, f.tzsign*(f.tzh*3600+f.tzm*60)
	}
	r.prec = f.ff
	return r, nil
}

// toDate is TO_DATE(s, format). A time and an offset in s are read, and
// checked, but not used.
func toDate(s, format string) (Value, error) {
	r, err := readDateTime(s, format)
	if err != nil {
		return Value{}, err
	}
	v, err := dateValue(civilDays(r.year, r.mon, r.mday))
	if err != nil {
		return Value{}, fmt.Errorf(`date out of range: "%s"`, s)
	}
	return v, nil
}

// toTimestamp is TO_TIMESTAMP(s, format), of type t: a local time in the
// session time zone z, unless TZH or TZM give the offset. FF1 to FF5 round
// it to that many digits, half away from Postgres's epoch, as Postgres
// rounds a timestamp to a lower precision.
func toTimestamp(s, format string, z tzZone, t ColType) (Value, error) {
	r, err := readDateTime(s, format)
	if err != nil {
		return Value{}, err
	}
	tm := time.Date(clampYear(r.year), time.Month(r.mon), r.mday, r.hour, r.min, r.sec, r.usec*1000, time.UTC)
	if r.hasOffset {
		tm = tm.Add(-time.Duration(r.offset) * time.Second)
	} else {
		tm = z.instant(tm)
	}
	us, err := secondsNanosToUnit(tm.Unix(), int64(tm.Nanosecond()), arrow.Microsecond)
	if err == nil && r.prec > 0 {
		us, err = roundUnit(us, arrow.Microsecond, arrow.Microsecond, r.prec, pgEpoch)
	}
	var v Value
	if err == nil {
		v, err = fromTime(time.UnixMicro(us).UTC(), t)
	}
	if err != nil {
		return Value{}, errors.New("timestamp out of range")
	}
	return v, nil
}

// readDateTime reads s with format and combines its fields.
func readDateTime(s, format string) (fromCharTime, error) {
	f, err := readFromChar(s, format)
	if err != nil {
		return fromCharTime{}, err
	}
	return f.result(s)
}
