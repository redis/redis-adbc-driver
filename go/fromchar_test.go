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

// Unit tests (no Redis) for TO_DATE and TO_TIMESTAMP reading every pattern
// TO_CHAR writes, and for MAKE_DATE and MAKE_TIMESTAMP reading negative
// years as BC (#114). Every result and error is Postgres 16's.

import (
	"strings"
	"testing"
)

const tsTZ = "TIMESTAMP(6) WITH TIME ZONE"

func TestToDatePatterns(t *testing.T) {
	runScalarCases(t, []scalarCase{
		// The patterns #97 added to TO_CHAR.
		{"TO_DATE('48 2020', 'WW YYYY')", "2020-11-25", "DATE"},
		{"TO_DATE('2020 11 5', 'YYYY MM W')", "2020-11-29", "DATE"},
		{"TO_DATE('2459183', 'J')", "2020-11-29", "DATE"},
		{"TO_DATE('21', 'CC')", "2001-01-01", "DATE"},
		{"TO_DATE('2020 XI', 'YYYY RM')", "2020-11-01", "DATE"},
		{"TO_DATE('2,020', 'Y,YYY')", "2020-01-01", "DATE"},
		{"TO_DATE('2020 334', 'YYYY DDD')", "2020-11-29", "DATE"},
		{"TO_DATE('2020 53 5', 'IYYY IW ID')", "2021-01-01", "DATE"},
		{"TO_DATE('2020 368', 'IYYY IDDD')", "2020-12-31", "DATE"},
		{"TO_DATE('29th Nov 2020', 'DDth Mon YYYY')", "2020-11-29", "DATE"},
		{"TO_DATE('2,020-11-29', 'Y,YYY-MM-DD')", "2020-11-29", "DATE"},

		// Years of fewer than four digits are near 2020.
		{"TO_DATE('020', 'YYY')", "2020-01-01", "DATE"},
		{"TO_DATE('519', 'YYY')", "2519-01-01", "DATE"},
		{"TO_DATE('520', 'YYY')", "1520-01-01", "DATE"},
		{"TO_DATE('69', 'YY')", "2069-01-01", "DATE"},
		{"TO_DATE('70', 'YY')", "1970-01-01", "DATE"},
		{"TO_DATE('5', 'Y')", "2005-01-01", "DATE"},
		// A negative year is BC, and so is one with BC; BC negates a
		// negative year.
		{"TO_DATE('-44-03-15', 'YYYY-MM-DD')", "0044-03-15 BC", "DATE"},
		{"TO_DATE('0044-03-15 BC', 'YYYY-MM-DD BC')", "0044-03-15 BC", "DATE"},
		{"TO_DATE('0044-03-15 b.c.', 'YYYY-MM-DD B.C.')", "0044-03-15 BC", "DATE"},
		{"TO_DATE('-44-03-15 BC', 'YYYY-MM-DD BC')", "0044-03-15", "DATE"},
		{"TO_DATE('2020 AD', 'YYYY BC')", "2020-01-01", "DATE"},
		{"TO_DATE('44 BC', 'YY BC')", "2044-01-01 BC", "DATE"},
		// CC: the first year of the century, or YY / Y in it. A year of
		// three or four digits doesn't take CC.
		{"TO_DATE('21 05', 'CC YY')", "2005-01-01", "DATE"},
		{"TO_DATE('21 00', 'CC YY')", "2100-01-01", "DATE"},
		{"TO_DATE('20 2020', 'CC YYYY')", "2020-01-01", "DATE"},
		{"TO_DATE('-6 05', 'CC YY')", "0505-01-01 BC", "DATE"},
		{"TO_DATE('6 05 BC', 'CC YY BC')", "0505-01-01 BC", "DATE"},
		{"TO_DATE('1 BC', 'CC BC')", "0100-01-01 BC", "DATE"},

		// J sets the date; DD and MM then replace its day and month.
		{"TO_DATE('1', 'J')", "4714-11-25 BC", "DATE"},
		{"TO_DATE('2459183 2000-05-05', 'J YYYY-MM-DD')", "2020-05-05", "DATE"},
		// WW and W are the first day of the week; DDD the day of the year,
		// for the month or day not given. D, DAY, DY and Q are read and
		// ignored.
		{"TO_DATE('2020 48 3', 'YYYY WW D')", "2020-11-25", "DATE"},
		{"TO_DATE('2020 2', 'YYYY WW')", "2020-01-08", "DATE"},
		{"TO_DATE('2020 366', 'YYYY DDD')", "2020-12-31", "DATE"},
		{"TO_DATE('2020 03 01 334', 'YYYY MM DD DDD')", "2020-03-29", "DATE"},
		{"TO_DATE('2020 9 Monday 2', 'YYYY MM Day Q')", "2020-09-01", "DATE"},
		// ISO week dates: the Monday of the week, or the day ID (or DAY).
		{"TO_DATE('2020 53', 'IYYY IW')", "2020-12-28", "DATE"},
		{"TO_DATE('2021 01 1', 'IYYY IW ID')", "2021-01-04", "DATE"},
		{"TO_DATE('20 53 5', 'IY IW ID')", "2021-01-01", "DATE"},
		{"TO_DATE('020 53 5', 'IYY IW ID')", "2021-01-01", "DATE"},
		{"TO_DATE('0 53 5', 'I IW ID')", "2001-01-05", "DATE"},
		{"TO_DATE('2020 53 Friday', 'IYYY IW Day')", "2021-01-01", "DATE"},

		// Names, in any case. RM reads the longest numeral first.
		{"TO_DATE('2020 xi', 'YYYY RM')", "2020-11-01", "DATE"},
		{"TO_DATE('2020 iv 15', 'YYYY rm DD')", "2020-04-15", "DATE"},
		{"TO_DATE('2020 XIII', 'YYYY RM')", "2020-12-01", "DATE"},
		{"TO_DATE('15 MAR 2024', 'DD mon YYYY')", "2024-03-15", "DATE"},
		{"TO_DATE('March 5, 2024', 'Month DD, YYYY')", "2024-03-05", "DATE"},
		{"TO_DATE('MARCH 2024', 'month YYYY')", "2024-03-01", "DATE"},
		{"TO_DATE('Fri, 15 Mar 2024', 'Dy, DD Mon YYYY')", "2024-03-15", "DATE"},

		// A number is as wide as its pattern before another number, and
		// otherwise (or with FM) reads all its digits.
		{"TO_DATE('20200315', 'YYYYMMDD')", "2020-03-15", "DATE"},
		{"TO_DATE('2020-3-5', 'YYYY-MM-DD')", "2020-03-05", "DATE"},
		{"TO_DATE('2024 1 5', 'FMYYYY FMMM FMDD')", "2024-01-05", "DATE"},
		// Separators and spaces match each other, or nothing; text in
		// quotes skips as many characters; the rest of the text, or of the
		// format, is ignored.
		{"TO_DATE('2024/02', 'YYYY-MM-DD')", "2024-02-01", "DATE"},
		{"TO_DATE('2020 / 03 / 15', 'YYYY-MM-DD')", "2020-03-15", "DATE"},
		{"TO_DATE('2020-03-15', 'YYYY MM DD')", "2020-03-15", "DATE"},
		{"TO_DATE('2020-01-01 garbage', 'YYYY-MM-DD')", "2020-01-01", "DATE"},
		{`TO_DATE('2024 "x" 03', 'YYYY "\"x\"" MM')`, "2024-03-01", "DATE"},
		{`TO_DATE('2024x03', 'YYYY"-"MM')`, "2024-03-01", "DATE"},
		{"TO_DATE('2024-03-15', 'yyyy-mm-dd')", "2024-03-15", "DATE"},

		// Fields not given, or given as 0: year 0 (1 BC), January, the 1st.
		// Without a year the day isn't checked against the month.
		{"TO_DATE('03-15', 'MM-DD')", "0001-03-15 BC", "DATE"},
		{"TO_DATE('02-30', 'MM-DD')", "0001-03-01 BC", "DATE"},
		{"TO_DATE('2020-00-00', 'YYYY-MM-DD')", "2020-01-01", "DATE"},
		{"TO_DATE('', 'YYYY')", "0001-01-01 BC", "DATE"},
		// TO_DATE ignores the time and the offset.
		{"TO_DATE('2020-01-01 23:00 -05', 'YYYY-MM-DD HH24:MI TZH')", "2020-01-01", "DATE"},
		// The driver's DATE range is wider than Postgres's (which ends
		// at 4714-11-24 BC).
		{"TO_DATE('5000-01-01 BC', 'YYYY-MM-DD BC')", "5000-01-01 BC", "DATE"},
	})
}

func TestToTimestampPatterns(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"TO_TIMESTAMP('12:30', 'HH24:MI')", "0001-01-01 12:30:00+00 BC", tsTZ},
		{"TO_TIMESTAMP('', '')", "0001-01-01 00:00:00+00 BC", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 14:30', 'FXYYYY-MM-DD HH24:MI')", "2024-03-15 14:30:00+00", tsTZ},
		{"TO_TIMESTAMP('0044-03-15 12:00 BC', 'YYYY-MM-DD HH24:MI BC')", "0044-03-15 12:00:00+00 BC", tsTZ},
		// Hours, minutes and seconds.
		{"TO_TIMESTAMP('13:05:09', 'HH24:MI:SS')", "0001-01-01 13:05:09+00 BC", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 1:05 PM', 'YYYY-MM-DD HH:MI PM')", "2024-03-15 13:05:00+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 12:05 a.m.', 'YYYY-MM-DD HH12:MI A.M.')", "2024-03-15 00:05:00+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 12:05 pm', 'YYYY-MM-DD HH12:MI am')", "2024-03-15 12:05:00+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 47109', 'YYYY-MM-DD SSSS')", "2024-03-15 13:05:09+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 47109', 'YYYY-MM-DD SSSSS')", "2024-03-15 13:05:09+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 130509', 'YYYY-MM-DD HH24MISS')", "2024-03-15 13:05:09+00", tsTZ},
		// Fractions: MS, US and FF1 to FF6 read digits after the point, so
		// .5 is 500 ms.
		{"TO_TIMESTAMP('2024-03-15 09.5', 'YYYY-MM-DD SS.MS')", "2024-03-15 00:00:09.5+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.005', 'YYYY-MM-DD SS.MS')", "2024-03-15 00:00:09.005+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.5', 'YYYY-MM-DD SS.US')", "2024-03-15 00:00:09.5+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.123 456', 'YYYY-MM-DD SS.MS US')", "2024-03-15 00:00:09.579+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.12', 'YYYY-MM-DD SS.FF2')", "2024-03-15 00:00:09.12+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.1234', 'YYYY-MM-DD SS.FF4')", "2024-03-15 00:00:09.1234+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.12345', 'YYYY-MM-DD SS.FF5')", "2024-03-15 00:00:09.12345+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.123456', 'YYYY-MM-DD SS.FF6')", "2024-03-15 00:00:09.123456+00", tsTZ},
		// FFn rounds to n digits, half away from 2000-01-01.
		{"TO_TIMESTAMP('2024-03-15 09.123456', 'YYYY-MM-DD SS.FF3')", "2024-03-15 00:00:09.123+00", tsTZ},
		{"TO_TIMESTAMP('2024-03-15 09.15', 'YYYY-MM-DD SS.FF1')", "2024-03-15 00:00:09.2+00", tsTZ},
		{"TO_TIMESTAMP('1999-03-15 09.15', 'YYYY-MM-DD SS.FF1')", "1999-03-15 00:00:09.1+00", tsTZ},
		{"TO_TIMESTAMP('2020-12-31 23:59:59.9999', 'YYYY-MM-DD HH24:MI:SS.FF3')", "2021-01-01 00:00:00+00", tsTZ},
		// TZH and TZM give the offset. A '-' read as a separator isn't
		// TZH's sign unless spaces follow it.
		{"TO_TIMESTAMP('2020-01-01 12:00 +05', 'YYYY-MM-DD HH24:MI TZH')", "2020-01-01 07:00:00+00", tsTZ},
		{"TO_TIMESTAMP('2020-01-01 12:00 -05', 'YYYY-MM-DD HH24:MI TZH')", "2020-01-01 17:00:00+00", tsTZ},
		{"TO_TIMESTAMP('2020-01-01 12:00:00-05', 'YYYY-MM-DD HH24:MI:SS-TZH')", "2020-01-01 07:00:00+00", tsTZ},
		{"TO_TIMESTAMP('2020-01-01 12:00:00 -05:30', 'YYYY-MM-DD HH24:MI:SS TZH:TZM')", "2020-01-01 17:30:00+00", tsTZ},
		{"TO_TIMESTAMP('2020-01-01 12:00:00 +0530', 'YYYY-MM-DD HH24:MI:SS TZHTZM')", "2020-01-01 06:30:00+00", tsTZ},
		{"TO_TIMESTAMP('2020-01-01 12:00 30', 'YYYY-MM-DD HH24:MI TZM')", "2020-01-01 11:30:00+00", tsTZ},
	})
}

func TestToDateErrors(t *testing.T) {
	expectExprErrors(t, map[string]string{
		// Text a pattern can't read.
		"TO_DATE('2020', 'Y,YYY')":                          `invalid input string for "Y,YYY"`,
		"TO_DATE('Mar 2020', 'MONTH YYYY')":                 `invalid value "Mar" for "MONTH"`,
		"TO_DATE('March 2020', 'MON YYYY')":                 `invalid value "ch 2" for "YYYY"`,
		"TO_DATE('2020 Funday', 'YYYY Day')":                `invalid value "Funday" for "Day"`,
		"TO_DATE('2020 xx', 'YYYY BC')":                     `invalid value "xx" for "BC"`,
		"TO_DATE('2024-xx-01', 'YYYY-MM-DD')":               `invalid value "xx" for "MM"`,
		"TO_DATE('x2020-03-15', 'YYYY-MM-DD')":              `invalid value "x202" for "YYYY"`,
		"TO_DATE('2020 3 15', 'YYYYMMDD')":                  `invalid value "3 " for "MM"`,
		"TO_DATE('2024 1', 'YYYYMMDD')":                     `source string too short for "MM" formatting field`,
		"TO_DATE('99999999999', 'YYYY')":                    `value for "YYYY" in source string is out of range`,
		"TO_DATE('2020-03-15', 'IYYY-MM-DD')":               "invalid combination of date conventions",
		"TO_DATE('2020 53 5', 'YYYY IW ID')":                "invalid combination of date conventions",
		"TO_DATE('334', 'DDD')":                             "cannot calculate day of year without year information",
		"TO_TIMESTAMP('2020 12:00 PST', 'YYYY HH24:MI TZ')": `formatting field "TZ" is only supported in to_char`,
		"TO_TIMESTAMP('2020 12:00 pst', 'YYYY HH24:MI tz')": `formatting field "tz" is only supported in to_char`,
		"TO_TIMESTAMP('2020 12:00 +05', 'YYYY HH24:MI OF')": `formatting field "OF" is only supported in to_char`,
		// A field given twice with different values.
		"TO_DATE('2020 XI 10', 'YYYY RM MM')":          `conflicting values for "MM" field in formatting string`,
		"TO_DATE('2020 November 10', 'YYYY Month MM')": `conflicting values for "MM" field in formatting string`,
		"TO_DATE('BC 2020 AD', 'BC YYYY AD')":          `conflicting values for "AD" field in formatting string`,
		"TO_DATE('2020 sunday 3', 'YYYY day D')":       `conflicting values for "D" field in formatting string`,
		"TO_TIMESTAMP('5:05 PM AM', 'HH:MI AM PM')":    `conflicting values for "PM" field in formatting string`,
		"TO_TIMESTAMP('09.1 9', 'SS.FF1 FF3')":         `conflicting values for "FF3" field in formatting string`,
		// Values out of range.
		"TO_TIMESTAMP('13:05 PM', 'HH12:MI PM')":                                `hour "13" is invalid for the 12-hour clock`,
		"TO_TIMESTAMP('PM 17:05', 'PM HH24:MI')":                                `hour "17" is invalid for the 12-hour clock`,
		"TO_TIMESTAMP('PM', 'PM')":                                              `hour "0" is invalid for the 12-hour clock`,
		"TO_DATE('2024-02-30', 'YYYY-MM-DD')":                                   `date/time field value out of range: "2024-02-30"`,
		"TO_DATE('2021 366', 'YYYY DDD')":                                       `date/time field value out of range: "2021 366"`,
		"TO_DATE('2020 03 32', 'YYYY MM DD')":                                   `date/time field value out of range: "2020 03 32"`,
		"TO_DATE('13', 'MM')":                                                   `date/time field value out of range: "13"`,
		"TO_TIMESTAMP('24:00', 'HH24:MI')":                                      `date/time field value out of range: "24:00"`,
		"TO_TIMESTAMP('09.1234', 'SS.MS')":                                      `date/time field value out of range: "09.1234"`,
		"TO_TIMESTAMP('2020-01-01 12:00 16', 'YYYY-MM-DD HH24:MI TZH')":         `time zone displacement out of range: "2020-01-01 12:00 16"`,
		"TO_TIMESTAMP('2020-01-01 12:00 +05:60', 'YYYY-MM-DD HH24:MI TZH:TZM')": `time zone displacement out of range: "2020-01-01 12:00 +05:60"`,
		"TO_DATE('6000000', 'YYYY')":                                            `date out of range: "6000000"`,
		"TO_DATE('5881581-01-01', 'YYYY-MM-DD')":                                `date out of range: "5881581-01-01"`,
		"TO_TIMESTAMP('300000', 'YYYY')":                                        "timestamp out of range",
	})
}

// TO_DATE and TO_TIMESTAMP read back what TO_CHAR writes.
func TestToCharRoundTrip(t *testing.T) {
	var cases []scalarCase
	for _, d := range []string{"2020-11-29", "2024-02-29", "2021-01-03", "2020-12-31", "2000-01-01", "2100-12-31",
		"0044-03-15 BC", "0001-12-31 BC", "0001-01-01", "10000-01-01"} {
		formats := []string{"YYYY-MM-DD BC", "Y,YYY MM DD BC", "J", "YYYY DDD BC", "FMDD FMMonth YYYY BC", "DD RM YYYY BC",
			"Day, DD Mon YYYY B.C.", "CC YY MM DD", "FMMM/FMDD/YYYY bc"}
		if !strings.HasSuffix(d, "BC") && d != "0001-01-01" {
			// The ISO year of a date near 1 AD can be another era's.
			formats = append(formats, "IYYY IW ID", "IYYY-IDDD", "IYYY IW Dy")
		}
		for _, f := range formats {
			cases = append(cases, scalarCase{"TO_DATE(TO_CHAR(DATE '" + d + "', '" + f + "'), '" + f + "') = DATE '" + d + "'", "true", "BOOLEAN"})
		}
	}
	for _, ts := range []string{"2024-01-10 13:05:09.123456", "2024-07-01 00:00:00", "2024-03-10 03:30:00", "0044-03-15 12:00:00.5 BC"} {
		for _, f := range []string{"YYYY-MM-DD HH24:MI:SS.US BC", "YYYY-MM-DD HH12:MI:SS.US AM BC", "J SSSSS.US", "FMJ SSSS.FF6",
			"YYYYMMDDHH24MISSUS BC", "Y,YYY DDD HH:MI:SS.US P.M. BC"} {
			cases = append(cases, scalarCase{"TO_TIMESTAMP(TO_CHAR(TIMESTAMPTZ '" + ts + "', '" + f + "'), '" + f + "') = TIMESTAMPTZ '" + ts + "'", "true", "BOOLEAN"})
		}
	}
	runScalarCases(t, cases)
	// In another zone, also with the offset written and read (but for 44
	// BC, in local mean time, whose offset has seconds, which TZH and TZM
	// don't write).
	for i := range cases {
		if !strings.Contains(cases[i].expr, "0044") {
			cases[i].expr = strings.ReplaceAll(cases[i].expr, "SS.US BC'", `SS.US TZH:TZM BC'`)
		}
	}
	runZoneCases(t, "America/New_York", cases)
}

// TO_TIMESTAMP reads a local time in the session time zone, unless TZH or
// TZM give the offset.
func TestToTimestampZone(t *testing.T) {
	runZoneCases(t, "America/Los_Angeles", []scalarCase{
		{"TO_TIMESTAMP('2024-01-10 02:00', 'YYYY-MM-DD HH24:MI')", "2024-01-10 02:00:00-08", tsTZ},
		// The spring-forward gap and the fall-back overlap, as in Postgres.
		{"TO_TIMESTAMP('2024-03-10 02:30', 'YYYY-MM-DD HH24:MI')", "2024-03-10 03:30:00-07", tsTZ},
		{"TO_TIMESTAMP('2024-11-03 01:30', 'YYYY-MM-DD HH24:MI')", "2024-11-03 01:30:00-08", tsTZ},
		{"TO_TIMESTAMP('2024-11-03 01:30 -07', 'YYYY-MM-DD HH24:MI TZH')", "2024-11-03 01:30:00-07", tsTZ},
		{"TO_TIMESTAMP('2024-01-10 02:00 +05:30', 'YYYY-MM-DD HH24:MI TZH:TZM')", "2024-01-09 12:30:00-08", tsTZ},
		// FFn rounds the instant: before 2000-01-01 UTC a half rounds down.
		{"TO_TIMESTAMP('1999-12-31 16:00:00.15', 'YYYY-MM-DD HH24:MI:SS.FF1')", "1999-12-31 16:00:00.2-08", tsTZ},
		{"TO_TIMESTAMP('1999-12-31 15:59:59.15', 'YYYY-MM-DD HH24:MI:SS.FF1')", "1999-12-31 15:59:59.1-08", tsTZ},
		// Local mean time before the zone's first transition.
		{"TO_TIMESTAMP('12:30', 'HH24:MI')", "0001-01-01 12:30:00-07:52:58 BC", tsTZ},
		{"MAKE_TIMESTAMPTZ(-44, 3, 15, 1, 2, 3)", "0044-03-15 01:02:03-07:52:58 BC", tsTZ},
		{"TO_DATE('2024-01-10 23:00 -05', 'YYYY-MM-DD HH24:MI TZH')", "2024-01-10", "DATE"},
	})
}

// MAKE_DATE and MAKE_TIMESTAMP read a negative year as BC, and year 0 is
// an error. Postgres's MAKE_DATE messages show a BC year as the
// astronomical one (-43 for 44 BC); MAKE_TIMESTAMP's show it as given.
func TestMakeDateBC(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"MAKE_DATE(-44, 3, 15)", "0044-03-15 BC", "DATE"},
		{"MAKE_DATE(-1, 12, 31)", "0001-12-31 BC", "DATE"},
		{"MAKE_DATE(-1, 12, 31) + 1", "0001-01-01", "DATE"},
		{"MAKE_DATE(-45, 2, 29)", "0045-02-29 BC", "DATE"},
		{"MAKE_DATE(-44, 3, 15) = DATE '0044-03-15 BC'", "true", "BOOLEAN"},
		{"MAKE_DATE(-5877642, 6, 23)", "5877642-06-23 BC", "DATE"},
		{"MAKE_TIMESTAMP(-44, 3, 15, 1, 2, 3)", "0044-03-15 01:02:03 BC", "TIMESTAMP(6)"},
		{"MAKE_TIMESTAMPTZ(-44, 3, 15, 1, 2, 3)", "0044-03-15 01:02:03+00 BC", tsTZ},
	})
	expectExprErrors(t, map[string]string{
		"MAKE_DATE(0, 3, 15)":                        "date field value out of range: 0-03-15",
		"MAKE_DATE(-44, 2, 29)":                      "date field value out of range: -43-02-29",
		"MAKE_DATE(-44, 13, 1)":                      "date field value out of range: -43-13-01",
		"MAKE_DATE(-2147483648, 1, 1)":               "date field value out of range: -2147483648-01-01",
		"MAKE_DATE(-2147483647, 1, 1)":               "date out of range: -2147483646-01-01",
		"MAKE_DATE(-5877642, 6, 22)":                 "date out of range: -5877641-06-22",
		"MAKE_TIMESTAMP(0, 3, 15, 1, 2, 3)":          "date field value out of range: 0-03-15",
		"MAKE_TIMESTAMP(-44, 2, 29, 1, 2, 3)":        "date field value out of range: -44-02-29",
		"MAKE_TIMESTAMP(-2147483648, 1, 1, 0, 0, 0)": "date field value out of range: -2147483648-01-01",
		"MAKE_TIMESTAMPTZ(0, 3, 15, 1, 2, 3)":        "date field value out of range: 0-03-15",
	})
}
