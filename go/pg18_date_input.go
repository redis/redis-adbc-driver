// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var pgDateISO = regexp.MustCompile(`^(\d{4,7})-(\d{1,2})-(\d{1,2})(.*)$`)
var pgDateSlash = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{1,4})(.*)$`)
var pgDateCompact = regexp.MustCompile(`^(\d{8}|\d{6})([ T].*|$)`)
var pgDateOrdinal = regexp.MustCompile(`^(\d{4})\.(\d{3})(.*)$`)
var pgDateJulian = regexp.MustCompile(`^[Jj](\d+)(.*)$`)
var pgDateMonthFirst = regexp.MustCompile(`^([A-Za-z]+)[ -]+(\d{1,2}),?[ -]+(\d{1,7})(.*)$`)
var pgDateMonthMiddle = regexp.MustCompile(`^(\d{1,7})[ -]+([A-Za-z]+)[ -]+(\d{1,7})(.*)$`)
var pgDateMonthClock = regexp.MustCompile(`^([A-Za-z]+)\s+(\d{1,2})\s+(\d{1,2}:\S+)\s+(?:(\S+)\s+)?(\d{1,7})(.*)$`)
var pgMeridiem = regexp.MustCompile(`(?i)(\d{1,2}:\d{2}(?::\d{2}(?:\.\d+)?)?)\s*(AM|PM)\b`)

func normalizeMeridiem(s string) string {
	return pgMeridiem.ReplaceAllStringFunc(s, func(text string) string {
		m := pgMeridiem.FindStringSubmatch(text)
		colon := strings.IndexByte(m[1], ':')
		h, _ := strconv.Atoi(m[1][:colon])
		if h < 1 || h > 12 {
			return text
		}
		h %= 12
		if strings.EqualFold(m[2], "PM") {
			h += 12
		}
		return fmt.Sprintf("%02d%s", h, m[1][colon:])
	})
}

// parsePGDatePrefix extends the ISO fast path with PostgreSQL's English and
// compact input forms. Numeric slash dates use the driver's ISO/MDY input mode.
// It returns a separate clock/zone suffix, retaining the existing zone checks.
func parsePGDatePrefix(s string, bc bool) (days int64, rest, bad string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		switch strings.ToLower(s[:i]) {
		case "mon", "tue", "wed", "thu", "fri", "sat", "sun", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
			s = strings.TrimSpace(s[i+1:])
		}
	}
	y, m, d := 0, 0, 0
	yearText := ""
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	month := func(s string) int {
		names := []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
		s = strings.ToLower(s)
		for i, n := range names {
			if s == n || len(s) == 3 && s == n[:3] {
				return i + 1
			}
		}
		return 0
	}
	switch {
	case pgDateISO.MatchString(s):
		a := pgDateISO.FindStringSubmatch(s)
		yearText = a[1]
		y, m, d = atoi(a[1]), atoi(a[2]), atoi(a[3])
		rest = a[4]
	case pgDateSlash.MatchString(s):
		a := pgDateSlash.FindStringSubmatch(s)
		yearText = a[3]
		y, m, d = atoi(a[3]), atoi(a[1]), atoi(a[2])
		rest = a[4]
	case pgDateCompact.MatchString(s):
		a := pgDateCompact.FindStringSubmatch(s)
		n := len(a[1])
		yearText = a[1][:n-4]
		y, m, d = atoi(yearText), atoi(a[1][n-4:n-2]), atoi(a[1][n-2:])
		rest = a[2]
	case pgDateOrdinal.MatchString(s):
		a := pgDateOrdinal.FindStringSubmatch(s)
		y, n := atoi(a[1]), atoi(a[2])
		tm := time.Date(y, 1, n, 0, 0, 0, 0, time.UTC)
		if n < 1 || tm.Year() != y {
			return 0, "", stateDatetimeRange
		}
		return floorDiv(tm.Unix(), 86400), a[3], ""
	case pgDateJulian.MatchString(s):
		a := pgDateJulian.FindStringSubmatch(s)
		n, err := strconv.ParseInt(a[1], 10, 64)
		if err != nil || n < 0 || n > mathMaxJulian {
			return 0, "", stateDatetimeRange
		}
		return n - 2440588, a[2], ""
	case pgDateMonthClock.MatchString(s):
		a := pgDateMonthClock.FindStringSubmatch(s)
		yearText = a[5]
		y, m, d = atoi(a[5]), month(a[1]), atoi(a[2])
		rest = " " + a[3] + " " + a[4] + a[6]
	case pgDateMonthFirst.MatchString(s):
		a := pgDateMonthFirst.FindStringSubmatch(s)
		yearText = a[3]
		y, m, d = atoi(a[3]), month(a[1]), atoi(a[2])
		rest = a[4]
	case pgDateMonthMiddle.MatchString(s):
		a := pgDateMonthMiddle.FindStringSubmatch(s)
		m = month(a[2])
		rest = a[4]
		if len(a[1]) >= 3 {
			yearText = a[1]
			y, d = atoi(a[1]), atoi(a[3])
		} else {
			if !strings.Contains(s, "-") && atoi(a[1]) > 31 {
				return 0, "", stateDatetimeFormat
			}
			yearText = a[3]
			y, d = atoi(a[3]), atoi(a[1])
		}
	default:
		return 0, "", stateDatetimeFormat
	}
	if m == 0 {
		return 0, "", stateDatetimeFormat
	}
	if !bc && len(yearText) <= 2 {
		if y < 70 {
			y += 2000
		} else {
			y += 1900
		}
	}
	if y == 0 || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, "", stateDatetimeRange
	}
	if bc {
		y = 1 - y
	}
	tm := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if tm.Year() != y || int(tm.Month()) != m || tm.Day() != d {
		return 0, "", stateDatetimeRange
	}
	return floorDiv(tm.Unix(), 86400), rest, ""
}

const mathMaxJulian = int64(2147483494)
