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

// Unit tests (no Redis) for timestamp text with an offset or a zone. As in
// Postgres's timestamp_in, a TIMESTAMP (without time zone) checks the
// offset or zone and ignores it, so '2024-01-05 10:00:00-05' is 10:00; a
// TIMESTAMP WITH TIME ZONE is the instant written. Up to v0.0.14 a
// TIMESTAMP was that instant in UTC (15:00). Every value is Postgres 16's.

import "testing"

func TestTimestampTextOffset(t *testing.T) {
	runScalarCases(t, []scalarCase{
		// Casts, typed literals and ::, with every form of offset.
		{"CAST('2024-01-05 10:00:00-05' AS TIMESTAMP)", "2024-01-05 10:00:00", "TIMESTAMP(6)"},
		{"TIMESTAMP '2024-01-05 10:00:00-05'", "2024-01-05 10:00:00", "TIMESTAMP(6)"},
		{"'2024-01-05 10:00:00-05'::timestamp", "2024-01-05 10:00:00", "TIMESTAMP(6)"},
		{"CAST('2024-01-05T10:00:00-05' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00+05:30' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00+0530' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00+5' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00-05:30:15' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00+15:59' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00-05' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 -05' AS TIMESTAMP)", "2024-01-05 00:00:00", ""},
		{"CAST('2024-01-05 10:00:00Z' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00z' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00 UTC' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		// Zone names and abbreviations, also where DST skips or repeats the
		// time, and before the zone's standard time (local mean time).
		{"CAST('2024-01-05 10:00:00 America/New_York' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00 PST' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00 EST5EDT' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-03-10 02:30:00 America/New_York' AS TIMESTAMP)", "2024-03-10 02:30:00", ""},
		{"CAST('2024-11-03 01:30:00 America/New_York' AS TIMESTAMP)", "2024-11-03 01:30:00", ""},
		{"CAST('1850-01-05 10:00:00 America/New_York' AS TIMESTAMP)", "1850-01-05 10:00:00", ""},
		// The date stays the one written.
		{"CAST('2024-01-05 23:30:00-05' AS TIMESTAMP)", "2024-01-05 23:30:00", ""},
		{"CAST('2024-01-05 00:30:00+02' AS TIMESTAMP)", "2024-01-05 00:30:00", ""},
		{"CAST('2024-01-05 24:00:00-05' AS TIMESTAMP)", "2024-01-06 00:00:00", ""},
		{"CAST('0044-03-15 10:00:00-05 BC' AS TIMESTAMP)", "0044-03-15 10:00:00 BC", ""},
		{"CAST('0001-01-01 02:00:00+05 BC' AS TIMESTAMP)", "0001-01-01 02:00:00 BC", ""},
		{"CAST('4714-11-24 00:00:00+05 BC' AS TIMESTAMP)", "4714-11-24 00:00:00 BC", ""},
		// Fractions, rounded as without an offset.
		{"CAST('2024-01-05 10:00:00.123456789-05' AS TIMESTAMP)", "2024-01-05 10:00:00.123457", ""},
		{"CAST('2024-01-05 10:00:00.1234565-05' AS TIMESTAMP)", "2024-01-05 10:00:00.123456", ""},
		{"TIMESTAMP(3) '2024-01-05 10:00:00.1235-05'", "2024-01-05 10:00:00.124", "TIMESTAMP(3)"},
		{"CAST('2024-01-05 10:00:00.5-05' AS TIMESTAMP(0))", "2024-01-05 10:00:01", "TIMESTAMP(0)"},
		{"CAST('2262-04-11 23:47:16.854775807-05' AS TIMESTAMP)", "2262-04-11 23:47:16.854776", ""},

		// A timestamp with time zone is the instant written.
		{"CAST('2024-01-05 10:00:00-05' AS TIMESTAMPTZ)", "2024-01-05 15:00:00+00", ""},
		{"TIMESTAMPTZ '2024-01-05 10:00:00 America/New_York'", "2024-01-05 15:00:00+00", ""},
		{"CAST('2024-01-05 10:00:00-05:30:15' AS TIMESTAMPTZ)", "2024-01-05 15:30:15+00", ""},
		{"CAST(CAST('2024-01-05 10:00:00-05' AS TIMESTAMPTZ) AS TIMESTAMP)", "2024-01-05 15:00:00", ""},
		{"CAST(CAST('2024-01-05 10:00:00-05' AS TIMESTAMP) AS TIMESTAMPTZ)", "2024-01-05 10:00:00+00", ""},

		// A date is the date written.
		{"CAST('2024-01-05 23:00:00-05' AS DATE)", "2024-01-05", "DATE"},
		{"CAST('2024-01-05 01:00:00+05' AS DATE)", "2024-01-05", "DATE"},
		{"CAST('2024-01-05 23:00:00 America/New_York' AS DATE)", "2024-01-05", "DATE"},

		// Text compared with, or read as, a timestamp.
		{"TIMESTAMP '2024-01-05 10:00:00' = '2024-01-05 10:00:00-05'", "true", "BOOLEAN"},
		{"TIMESTAMP '2024-01-05 10:00:00' = TIMESTAMP '2024-01-05 10:00:00+09'", "true", "BOOLEAN"},
		{"TIMESTAMP '2024-01-05 10:00:00' < '2024-01-05 10:00:00+09'", "false", "BOOLEAN"},
		{"TIMESTAMP '2024-01-05 10:00:00' IN ('2024-01-05 15:00:00', '2024-01-05 10:00:00-05')", "true", "BOOLEAN"},
		{"TIMESTAMP '2024-01-05 10:00:00' BETWEEN '2024-01-05 09:00:00+08' AND '2024-01-05 11:00:00-08'", "true", "BOOLEAN"},
		{"EXTRACT(HOUR FROM TIMESTAMP '2024-01-05 10:00:00-05')", "10", ""},
		{"DATE_TRUNC('day', TIMESTAMP '2024-01-05 23:00:00-05')", "2024-01-05 00:00:00", ""},
		{"TIMESTAMP '2024-01-05 10:00:00-05' AT TIME ZONE 'UTC'", "2024-01-05 10:00:00+00", ""},
		{"TIMESTAMP '2024-01-05 10:00:00-05' + INTERVAL '1 hour'", "2024-01-05 11:00:00", ""},
		{"TIMESTAMP '2024-01-05 10:00:00-05' - TIMESTAMP '2024-01-05 10:00:00+05'", "00:00:00", "INTERVAL"},
		{"COALESCE(NULL, TIMESTAMP '2024-01-05 10:00:00-05')", "2024-01-05 10:00:00", ""},
		{"GREATEST(TIMESTAMP '2024-01-05 10:00:00', '2024-01-05 09:00:00-05')", "2024-01-05 10:00:00", ""},
		{"CASE WHEN 1 = 0 THEN TIMESTAMP '2024-01-05 10:00:00' ELSE '2024-01-05 09:00:00-05' END", "2024-01-05 09:00:00", ""},
		{"TO_CHAR(TIMESTAMP '2024-01-05 10:00:00-05', 'YYYY-MM-DD HH24:MI:SS')", "2024-01-05 10:00:00", "VARCHAR"},
		{"CAST(MAKE_TIMESTAMP(2024, 1, 5, 10, 0, 0) AS VARCHAR) = CAST(TIMESTAMP '2024-01-05 10:00:00+03' AS VARCHAR)", "true", "BOOLEAN"},
		// Text read as a timestamp with time zone keeps its instant.
		{"'2024-01-05 10:00:00-05' AT TIME ZONE 'UTC'", "2024-01-05 15:00:00", ""},
	})

	// In New York: a TIMESTAMP is still the time written, and becomes a
	// TIMESTAMP WITH TIME ZONE as a local time.
	runZoneCases(t, "America/New_York", []scalarCase{
		{"CAST('2024-01-05 10:00:00-05' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00+02' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00 Asia/Tokyo' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00' AS TIMESTAMP)", "2024-01-05 10:00:00", ""},
		{"CAST('2024-01-05 10:00:00+02' AS TIMESTAMPTZ)", "2024-01-05 03:00:00-05", ""},
		{"CAST('2024-01-05 10:00:00' AS TIMESTAMPTZ)", "2024-01-05 10:00:00-05", ""},
		{"CAST(CAST('2024-01-05 10:00:00+02' AS TIMESTAMP) AS TIMESTAMPTZ)", "2024-01-05 10:00:00-05", ""},
		{"CAST('2024-01-05 23:00:00+02' AS DATE)", "2024-01-05", ""},
		{"TIMESTAMPTZ '2024-01-05 10:00:00-05' = '2024-01-05 10:00:00+02'", "false", ""},
		{"TIMESTAMP '2024-01-05 10:00:00' = '2024-01-05 10:00:00+02'", "true", ""},
	})

	// The offset or zone is still checked, with Postgres's errors: hours up
	// to 15, minutes and seconds up to 59, and a zone name that exists.
	for _, c := range []struct{ expr, state, msg string }{
		{"CAST('2024-01-05 10:00:00+16' AS TIMESTAMP)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00+16"`},
		{"CAST('2024-01-05 10:00:00+05:60' AS TIMESTAMP)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00+05:60"`},
		{"CAST('2024-01-05 10:00:00+05:30:60' AS TIMESTAMP)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00+05:30:60"`},
		{"CAST('2024-01-05 10:00:00+16' AS TIMESTAMPTZ)", stateTZDisplacement, `time zone displacement out of range: "2024-01-05 10:00:00+16"`},
		{"CAST('2024-01-05 10:00:00+05x' AS TIMESTAMP)", stateDatetimeFormat, `invalid input syntax for type timestamp: "2024-01-05 10:00:00+05x"`},
		{"CAST('2024-01-05 10:00:00+05:3x' AS TIMESTAMP)", stateDatetimeFormat, `invalid input syntax for type timestamp: "2024-01-05 10:00:00+05:3x"`},
		{"CAST('2024-01-05 10:00:00 foo' AS TIMESTAMP)", stateDatetimeFormat, `invalid input syntax for type timestamp: "2024-01-05 10:00:00 foo"`},
		{"CAST('2024-01-05 10:00:00 Foo/Bar' AS TIMESTAMP)", stateInvalidParameter, `time zone "foo/bar" not recognized`},
		{"CAST('2024-01-05 25:00:00-05' AS TIMESTAMP)", stateDatetimeRange, `date/time field value out of range: "2024-01-05 25:00:00-05"`},
	} {
		_, _, err := evalTestExpr(t, c.expr)
		if err == nil || errText(err) != c.msg || sqlState(err) != c.state {
			t.Errorf("%s: error %v (%s), want %q (%s)", c.expr, err, sqlState(err), c.msg, c.state)
		}
	}
}

// parseTimestamp returns the date and time written and, apart, the offset
// of the offset or zone written.
func TestParseTimestampOffset(t *testing.T) {
	const day = 19727 * 86400 // 2024-01-05
	for _, c := range []struct {
		in            string
		local, offset int64
		hasTZ         bool
	}{
		{"2024-01-05 10:00:00", day + 36000, 0, false},
		{"2024-01-05 10:00:00Z", day + 36000, 0, true},
		{"2024-01-05 10:00:00 utc", day + 36000, 0, true},
		{"2024-01-05 10:00:00-05", day + 36000, -5 * 3600, true},
		{"2024-01-05 10:00:00+05:30", day + 36000, 19800, true},
		{"2024-01-05 10:00:00+0530", day + 36000, 19800, true},
		{"2024-01-05 10:00:00-05:30:15", day + 36000, -19815, true},
		{"2024-01-05 10:00:00 America/New_York", day + 36000, -5 * 3600, true},
		{"2024-07-05 10:00:00 America/New_York", (19727+182)*86400 + 36000, -4 * 3600, true},
		{"2024-01-05 -05", day, -5 * 3600, true},
	} {
		local, _, _, offset, hasTZ, err := parseTimestamp(c.in)
		if err != nil || local != c.local || offset != c.offset || hasTZ != c.hasTZ {
			t.Errorf("%q: %d, %d, %v, %v; want %d, %d, %v", c.in, local, offset, hasTZ, err, c.local, c.offset, c.hasTZ)
		}
	}
}
