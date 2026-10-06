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

import "testing"

func testZone(t *testing.T, name string) tzZone {
	t.Helper()
	p, err := timeZoneParam(name)
	if err != nil {
		t.Fatal(err)
	}
	return p.zone
}

// A local time is written as the instant it is in the zone, with the
// zone's offset (Postgres's text for the folded constant), and reads back
// as that instant, with its unit, in any zone.
func TestFoldedLocalTime(t *testing.T) {
	for _, c := range []struct{ zone, in, want string }{
		{"America/New_York", "2024-01-01 00:00", "2024-01-01 00:00:00-05"},
		{"America/New_York", " 2024-01-01 ", "2024-01-01 00:00:00-05"},
		{"America/New_York", "2024-07-01T12:00:00.5", "2024-07-01 12:00:00.5-04"},
		// Skipped and repeated by DST: the offset before and after.
		{"America/Los_Angeles", "2024-03-10 02:30", "2024-03-10 03:30:00-07"},
		{"America/Los_Angeles", "2024-11-03 01:30", "2024-11-03 01:30:00-08"},
		{"Asia/Kolkata", "2024-01-10 02:00:00.25", "2024-01-10 02:00:00.25+05:30"},
		{"UTC+5", "2024-01-01 00:00", "2024-01-01 00:00:00-05"},
		{"UTC", "2024-01-01 00:00", "2024-01-01 00:00:00+00"},
		// Local mean time has seconds in its offset: written in UTC.
		{"America/New_York", "1800-01-01 00:00", "1800-01-01 04:56:02+00"},
		{"UTC", "0044-03-15 12:00 BC", "0044-03-15 12:00:00+00 BC"},
		// More than 6 digits: nanoseconds, also with trailing zeros.
		{"America/New_York", "2024-01-01 00:00:00.1234560", "2024-01-01 00:00:00.123456000-05"},
		// Kept as written.
		{"America/New_York", "2024-01-01 00:00+02", ""},
		{"America/New_York", "2024-01-01 00:00 Europe/Paris", ""},
		{"America/New_York", "2024-01-01 00:00Z", ""},
		{"America/New_York", "yesterday", ""},
		{"America/New_York", "", ""},
	} {
		z := testZone(t, c.zone)
		got, ok := foldedLocalTime(c.in, z)
		if !ok {
			got = ""
		}
		if got != c.want {
			t.Errorf("%s in %s: %q, want %q", c.in, c.zone, got, c.want)
			continue
		}
		if !ok {
			continue
		}
		want, _ := timestampLiteralIn(c.in, true, z)
		for _, other := range []string{"UTC", "Asia/Tokyo", "America/Los_Angeles"} {
			if v, err := timestampLiteralIn(got, true, testZone(t, other)); err != nil || v != want {
				t.Errorf("%q in %s reads %v, %v; want %v", got, other, v, err, want)
			}
		}
	}
}

// The literals a definition reads as timestamps with time zone are
// rewritten in its text, and the rest are kept as written.
func TestFoldLocalTimes(t *testing.T) {
	types := map[string]ColType{"tz": typeTimestampTZ, "ts": typeTimestamp, "s": typeString}
	z := testZone(t, "America/New_York")
	for _, c := range []struct{ in, want string }{
		{`tz >= '2024-01-01' AND s = '2024-01-01' AND ts < '2024-01-01'`,
			`tz >= '2024-01-01 00:00:00-05' AND s = '2024-01-01' AND ts < '2024-01-01'`},
		{`'2024-01-01 00:00' < tz OR tz IS DISTINCT FROM E'2024-01-02'`,
			`'2024-01-01 00:00:00-05' < tz OR tz IS DISTINCT FROM '2024-01-02 00:00:00-05'`},
		{`tz IN ('2024-01-01', '2024-01-02') AND tz BETWEEN '2024-01-01' AND $$2024-02-01$$`,
			`tz IN ('2024-01-01 00:00:00-05', '2024-01-02 00:00:00-05') AND tz BETWEEN '2024-01-01 00:00:00-05' AND '2024-02-01 00:00:00-05'`},
		{`TIMESTAMPTZ '2024-01-01' < '2024-01-02'::timestamptz`,
			`TIMESTAMPTZ '2024-01-01 00:00:00-05' < '2024-01-02 00:00:00-05'::timestamptz`},
		{`CAST('2024-01-01' AS TIMESTAMP(3) WITH TIME ZONE) = TIMESTAMP WITH TIME ZONE '2024-01-01'`,
			`CAST('2024-01-01 00:00:00-05' AS TIMESTAMP(3) WITH TIME ZONE) = TIMESTAMP WITH TIME ZONE '2024-01-01 00:00:00-05'`},
		{`TRY_CAST('2024-01-01' AS TIMESTAMPTZ) = tz`, `TRY_CAST('2024-01-01 00:00:00-05' AS TIMESTAMPTZ) = tz`},
		{`CASE tz WHEN '2024-01-01' THEN 1 END = 1`, `CASE tz WHEN '2024-01-01 00:00:00-05' THEN 1 END = 1`},
		{`COALESCE(tz, '2024-01-01') = GREATEST(tz, '2024-01-01') AND NULLIF(tz, '2024-01-01') IS NULL`,
			`COALESCE(tz, '2024-01-01 00:00:00-05') = GREATEST(tz, '2024-01-01 00:00:00-05') AND NULLIF(tz, '2024-01-01 00:00:00-05') IS NULL`},
		{`'2024-01-01' AT TIME ZONE 'UTC' = ts AND '2024-01-01' AT LOCAL = ts`,
			`'2024-01-01 00:00:00-05' AT TIME ZONE 'UTC' = ts AND '2024-01-01 00:00:00-05' AT LOCAL = ts`},
		{`tz AT TIME ZONE 'America/Chicago' = '2024-01-01'`, `tz AT TIME ZONE 'America/Chicago' = '2024-01-01'`},
		{`DATE_TRUNC('day', tz) = '2024-01-01'`, `DATE_TRUNC('day', tz) = '2024-01-01 00:00:00-05'`},
		// Read when the definition runs, as in Postgres.
		{`ts::timestamptz = tz AND DATE '2024-01-01'::timestamptz = tz AND TIMESTAMP '2024-01-01'::timestamptz = tz`,
			`ts::timestamptz = tz AND DATE '2024-01-01'::timestamptz = tz AND TIMESTAMP '2024-01-01'::timestamptz = tz`},
		{`'2024-01-01'::date = CURRENT_DATE AND ts = '2024-01-01' AND CAST(tz AS TEXT) = '2024-01-01'`,
			`'2024-01-01'::date = CURRENT_DATE AND ts = '2024-01-01' AND CAST(tz AS TEXT) = '2024-01-01'`},
		{`tz = '2024-01-01 00:00+00' AND tz > 'x'`, `tz = '2024-01-01 00:00+00' AND tz > 'x'`},
	} {
		x, lits, err := parseExprLits(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		untyped := map[*Literal]bool{}
		localTimeLiterals(x, func(x Expr) ColType {
			typ, _ := inferType(x, types, nil)
			return typ
		}, untyped)
		if got := foldLocalTimes(c.in, lits, untyped, z); got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.in, got, c.want)
		}
	}
}
