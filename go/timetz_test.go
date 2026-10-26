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
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

// TIME WITH TIME ZONE, against Postgres 16's output for each expression
// (psql, with the session time zone and date the cases name).

// July and January, for the offsets of zones with daylight saving time.
var (
	julyNow    = time.Date(2024, 7, 10, 16, 0, 0, 0, time.UTC)
	januaryNow = time.Date(2024, 1, 10, 17, 0, 0, 0, time.UTC)
)

// Text in and out, and the types.
func TestTimeTZText(t *testing.T) {
	runZoneCasesAt(t, "UTC", julyNow, []scalarCase{
		{"CAST('10:00+02' AS TIMETZ)", "10:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"CAST('10:00:00.123456789+02' AS TIMETZ)", "10:00:00.123457+02", "TIME(6) WITH TIME ZONE"},
		{"CAST('10:00:00.5-05:30' AS TIME WITH TIME ZONE)", "10:00:00.5-05:30", "TIME(6) WITH TIME ZONE"},
		{"CAST('10:00-04:56:02' AS TIMETZ)", "10:00:00-04:56:02", ""},
		{"CAST('10:00+05:30:15' AS TIMETZ)", "10:00:00+05:30:15", ""},
		{"CAST('24:00:00+02' AS TIMETZ)", "24:00:00+02", ""},
		{"CAST('10:00+15:59:59' AS TIMETZ)", "10:00:00+15:59:59", ""},
		{"CAST('23:59:59.9999995+02' AS TIMETZ)", "24:00:00+02", ""},
		{"CAST('23:59:59.9999994+02' AS TIMETZ)", "23:59:59.999999+02", ""},
		// Offsets as Postgres's DecodeTimezone reads them.
		{"CAST('10:00 +2' AS TIMETZ)", "10:00:00+02", ""},
		{"CAST('10:00+5' AS TIMETZ)", "10:00:00+05", ""},
		{"CAST('10:00+0230' AS TIMETZ)", "10:00:00+02:30", ""},
		{"CAST('10:00+530' AS TIMETZ)", "10:00:00+05:30", ""},
		{"CAST('10:00:00+02:00' AS TIMETZ)", "10:00:00+02", ""},
		{"CAST('10:00:00 -0530' AS TIMETZ)", "10:00:00-05:30", ""},
		{"CAST('10:00Z' AS TIMETZ)", "10:00:00+00", ""},
		{"CAST('10:00 z' AS TIMETZ)", "10:00:00+00", ""},
		{"CAST('10:00 UTC' AS TIMETZ)", "10:00:00+00", ""},
		{"CAST('T10:00+02' AS TIMETZ)", "10:00:00+02", ""},
		// Abbreviations, and zone names with a single offset or a date.
		{"CAST('10:00 EST' AS TIMETZ)", "10:00:00-05", ""},
		{"CAST('10:00 PST' AS TIMETZ)", "10:00:00-08", ""},
		{"CAST('10:00 PDT' AS TIMETZ)", "10:00:00-07", ""},
		{"CAST('10:00 CEST' AS TIMETZ)", "10:00:00+02", ""},
		{"CAST('10:00 MSK' AS TIMETZ)", "10:00:00+03", ""},
		{"CAST('10:00 Etc/GMT+5' AS TIMETZ)", "10:00:00-05", ""},
		{"CAST('10:00 Etc/UTC' AS TIMETZ)", "10:00:00+00", ""},
		{"CAST('2024-01-15 10:00 Europe/Paris' AS TIMETZ)", "10:00:00+01", ""},
		{"CAST('2024-07-15 10:00 europe/paris' AS TIMETZ)", "10:00:00+02", ""},
		{"CAST('2024-07-01 10:00+03' AS TIMETZ)", "10:00:00+03", ""},
		// Without an offset: the session time zone's.
		{"CAST('10:00' AS TIMETZ)", "10:00:00+00", ""},
		// Typed literals, precisions and synonyms.
		{"TIME WITH TIME ZONE '10:00+02'", "10:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ '10:00+02'", "10:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ(2) '10:00:00.555+02'", "10:00:00.56+02", "TIME(2) WITH TIME ZONE"},
		{"TIME(3) WITH TIME ZONE '10:00:00.12345+02'", "10:00:00.123+02", "TIME(3) WITH TIME ZONE"},
		{"CAST('10:00:00.123456+02' AS TIMETZ(2))", "10:00:00.12+02", "TIME(2) WITH TIME ZONE"},
		{"CAST('10:00:00.999+02' AS TIMETZ(0))", "10:00:01+02", "TIME(0) WITH TIME ZONE"},
		{"CAST('23:59:59.9+02' AS TIMETZ(0))", "24:00:00+02", "TIME(0) WITH TIME ZONE"},
		{"CAST('10:00:00.5+02' AS TIME(0) WITH TIME ZONE)", "10:00:01+02", "TIME(0) WITH TIME ZONE"},
		{"CAST('10:00:00.1234567+02' AS TIMETZ(7))", "10:00:00.123457+02", "TIME(6) WITH TIME ZONE"},
		{"CAST(TIMETZ '10:00:00.5+02' AS TEXT)", "10:00:00.5+02", "VARCHAR"},
		{"CAST(TIMETZ '10:00+02' AS VARCHAR(5))", "10:00", "VARCHAR(5)"},
		{"TIMETZ '10:00+02' || 'x'", "10:00:00+02x", "VARCHAR"},
		{"LENGTH(CAST(TIMETZ '10:00+02' AS TEXT))", "11", "BIGINT"},
		{"TO_JSON(TIMETZ '10:00:00.5-05:30')", `"10:00:00.5-05:30"`, ""},
		{"JSON_BUILD_ARRAY(TIMETZ '10:00+00')", `["10:00:00+00"]`, ""},
		// Errors.
		{"CAST('10:00 America/New_York' AS TIMETZ)",
			`error: invalid input syntax for type time with time zone: "10:00 America/New_York"`, ""},
		{"CAST('10:00 Asia/Kolkata' AS TIMETZ)", `error: invalid input syntax for type time with time zone: "10:00 Asia/Kolkata"`, ""},
		{"CAST('10:00 EST5EDT' AS TIMETZ)", `error: invalid input syntax for type time with time zone: "10:00 EST5EDT"`, ""},
		{"CAST('10:00 xyz' AS TIMETZ)", `error: invalid input syntax for type time with time zone: "10:00 xyz"`, ""},
		{"CAST('abc' AS TIMETZ)", `error: invalid input syntax for type time with time zone: "abc"`, ""},
		{"CAST('10:00+16' AS TIMETZ)", `error: time zone displacement out of range: "10:00+16"`, ""},
		{"CAST('10:00+15:59:60' AS TIMETZ)", `error: time zone displacement out of range: "10:00+15:59:60"`, ""},
		{"CAST('10:00+053015' AS TIMETZ)", `error: time zone displacement out of range: "10:00+053015"`, ""},
		{"CAST('24:00:01+00' AS TIMETZ)", `error: date/time field value out of range: "24:00:01+00"`, ""},
		{"CAST('25:00+00' AS TIMETZ)", `error: date/time field value out of range: "25:00+00"`, ""},
		{"CAST('10:60+00' AS TIMETZ)", `error: date/time field value out of range: "10:60+00"`, ""},
		{"CAST(TIMETZ '10:00+02' AS TIMESTAMPTZ)", `error: cannot convert timetz value "10:00:00+02" to TIMESTAMP(6) WITH TIME ZONE`, ""},
		{"CAST(DATE '2024-01-01' AS TIMETZ)", `error: cannot convert date value "2024-01-01" to TIME(6) WITH TIME ZONE`, ""},
		{"CAST(TIMESTAMP '2024-01-01 10:00:00' AS TIMETZ)",
			`error: cannot convert timestamp value "2024-01-01 10:00:00" to TIME(6) WITH TIME ZONE`, ""},
	})
}

// Comparison and equality: by the time in UTC, then larger offsets first.
func TestTimeTZCompare(t *testing.T) {
	runZoneCasesAt(t, "UTC", julyNow, []scalarCase{
		{"TIMETZ '10:00+02' = TIMETZ '09:00+01'", "false", "BOOLEAN"},
		{"TIMETZ '10:00+02' < TIMETZ '09:00+01'", "true", ""},
		{"TIMETZ '10:00+02' > TIMETZ '09:00+01'", "false", ""},
		{"TIMETZ '10:00+02' <= TIMETZ '09:00+01'", "true", ""},
		{"TIMETZ '10:00+02' IS DISTINCT FROM TIMETZ '09:00+01'", "true", ""},
		{"TIMETZ '10:00:00.000+02' = TIMETZ '10:00+02'", "true", ""},
		{"TIMETZ '23:00-05' < TIMETZ '04:00+00'", "false", ""},
		{"TIMETZ '01:00+05' < TIMETZ '00:00+00'", "true", ""},
		{"TIMETZ '10:00+02' = '10:00+02'", "true", ""},
		{"TIMETZ '10:00+02' = '10:00'", "false", ""},
		{"TIMETZ '10:00+02' < TIME '10:00'", "true", ""},
		{"TIMETZ '10:00+02' IN ('09:00+01', '08:00+00')", "false", ""},
		{"TIMETZ '10:00+02' IN (TIMETZ '09:00+01', TIMETZ '10:00+02')", "true", ""},
		{"TIMETZ '10:00+02' BETWEEN '09:00+02' AND '11:00+02'", "true", ""},
		{"GREATEST(TIMETZ '10:00+02', TIMETZ '09:00+01')", "09:00:00+01", "TIME(6) WITH TIME ZONE"},
		{"LEAST(TIMETZ '10:00+02', TIMETZ '09:00+01')", "10:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"GREATEST(TIME '10:00', TIMETZ '10:00+02')", "10:00:00+00", "TIME(6) WITH TIME ZONE"},
		{"LEAST(TIME '10:00', TIMETZ '10:00+02')", "10:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"NULLIF(TIMETZ '10:00+02', TIMETZ '09:00+01')", "10:00:00+02", ""},
		{"NULLIF(TIMETZ '10:00+02', '10:00+02')", "NULL", ""},
		{"COALESCE(NULL, TIME '10:00', TIMETZ '10:00+02')", "10:00:00+00", "TIME(6) WITH TIME ZONE"},
		{"CASE WHEN true THEN TIME '10:00' ELSE TIMETZ '10:00+02' END", "10:00:00+00", "TIME(6) WITH TIME ZONE"},
	})
	// The ORDER BY of Postgres: '24:00+00' (24:00 UTC) before '23:00-05'
	// (28:00 UTC).
	in := []string{"10:00+02", "09:00+01", "08:00+00", "23:00-05", "04:00+00", "00:00+00", "24:00+00", "00:00-15:59"}
	want := []string{"00:00:00+00", "04:00:00+00", "10:00:00+02", "09:00:00+01", "08:00:00+00", "00:00:00-15:59",
		"24:00:00+00", "23:00:00-05"}
	vals := make([]Value, len(in))
	for i, s := range in {
		v, err := parseTimeTZ(s, utcZone)
		if err != nil {
			t.Fatal(err)
		}
		vals[i] = v
	}
	slices.SortStableFunc(vals, func(a, b Value) int {
		c, _ := compareValues(a, b)
		return c
	})
	for i, v := range vals {
		if v.Text() != want[i] {
			t.Errorf("sorted[%d] = %s, want %s", i, v.Text(), want[i])
		}
	}
	// Equal values have equal keys, different values different ones.
	a, _ := parseTimeTZ("10:00+02", utcZone)
	b, _ := Coerce(a, fracType(KindTimeTZ, 0, ""))
	c, _ := parseTimeTZ("09:00+01", utcZone)
	if ka, kb, kc := timeTZKey(a), timeTZKey(b), timeTZKey(c); ka != kb || ka == kc {
		t.Errorf("keys %q, %q, %q", ka, kb, kc)
	}
}

// Conversions, AT TIME ZONE, arithmetic and fields.
func TestTimeTZFunctions(t *testing.T) {
	runZoneCasesAt(t, "UTC", julyNow, []scalarCase{
		{"CAST(TIMETZ '10:00+02' AS TIME)", "10:00:00", "TIME(6)"},
		{"CAST(TIMETZ '10:00:00.7+02' AS TIME(0))", "10:00:01", "TIME(0)"},
		{"CAST(TIMESTAMPTZ '2024-01-01 10:00:00.7+00' AS TIMETZ(0))", "10:00:01+00", "TIME(0) WITH TIME ZONE"},
		{"CAST(TIMESTAMPTZ '2024-01-01 23:59:59.7+00' AS TIMETZ(0))", "24:00:00+00", ""},
		{"CAST(TIME '10:00' AS TIMETZ)", "10:00:00+00", "TIME(6) WITH TIME ZONE"},
		{"CAST(TIME '10:00:00.5' AS TIME(0) WITH TIME ZONE)", "10:00:01+00", "TIME(0) WITH TIME ZONE"},
		// AT TIME ZONE: the same instant's time with the zone's offset (POSIX
		// '+05' is five hours west).
		{"TIME '10:00' AT TIME ZONE 'UTC'", "10:00:00+00", "TIME(6) WITH TIME ZONE"},
		{"TIME '10:00' AT TIME ZONE '+05'", "05:00:00-05", ""},
		{"TIME '10:00:00.5' AT TIME ZONE 'UTC'", "10:00:00.5+00", ""},
		{"TIME '24:00' AT TIME ZONE 'UTC'", "00:00:00+00", ""},
		{"TIMETZ '10:00+02' AT TIME ZONE 'UTC'", "08:00:00+00", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ '10:00+02' AT TIME ZONE 'EST'", "03:00:00-05", ""},
		{"TIMETZ '10:00+02' AT TIME ZONE 'MSK'", "11:00:00+03", ""},
		{"TIMETZ '10:00+02' AT TIME ZONE 'Asia/Kolkata'", "13:30:00+05:30", ""},
		{"TIMETZ '10:00+02' AT TIME ZONE INTERVAL '+05:30'", "13:30:00+05:30", ""},
		{"TIMETZ '24:00+02' AT TIME ZONE '+02'", "20:00:00-02", ""},
		{"TIMEZONE('UTC', TIMETZ '10:00+02')", "08:00:00+00", ""},
		{"TIMETZ '10:00+02' AT LOCAL", "08:00:00+00", ""},
		{"CAST(TIMETZ '10:00:00.5+02' AS TIMETZ(3)) AT TIME ZONE 'UTC'", "08:00:00.5+00", "TIME(3) WITH TIME ZONE"},
		// Arithmetic: the interval's time part, wrapped into a day.
		{"TIMETZ '10:00+02' + INTERVAL '1 day 2 hours'", "12:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ '23:00+02' + INTERVAL '3 hours'", "02:00:00+02", ""},
		{"INTERVAL '3 hours' + TIMETZ '23:00+02'", "02:00:00+02", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ '24:00+02' + INTERVAL '0'", "00:00:00+02", ""},
		{"TIMETZ '01:00+02' - INTERVAL '3 hours'", "22:00:00+02", ""},
		{"TIMETZ '10:00+02' - INTERVAL '11 hours'", "23:00:00+02", ""},
		{"TIMETZ '10:00+02' + INTERVAL '-1 hour'", "09:00:00+02", ""},
		{"TIMETZ '10:00+02' - INTERVAL '1 month 1 day'", "10:00:00+02", ""},
		{"DATE '2024-01-01' + TIMETZ '10:00+02'", "2024-01-01 08:00:00+00", "TIMESTAMP(6) WITH TIME ZONE"},
		{"TIMETZ '10:00+02' + DATE '2024-01-01'", "2024-01-01 08:00:00+00", "TIMESTAMP(6) WITH TIME ZONE"},
		{"DATE '2024-01-01' + TIMETZ '23:00-05'", "2024-01-02 04:00:00+00", ""},
		{"TIMETZ '10:00+02' - TIMETZ '09:00+01'",
			"error: operator - is not supported for TIME(6) WITH TIME ZONE and TIME(6) WITH TIME ZONE", ""},
		{"INTERVAL '3 hours' - TIMETZ '23:00+02'", "error: operator - is not supported for INTERVAL and TIME(6) WITH TIME ZONE", ""},
		// EXTRACT and DATE_PART.
		{"EXTRACT(timezone FROM TIMETZ '10:00-05:30')", "-19800", "BIGINT"},
		{"EXTRACT(timezone_hour FROM TIMETZ '10:00-05:30')", "-5", "BIGINT"},
		{"EXTRACT(timezone_minute FROM TIMETZ '10:00-05:30')", "-30", "BIGINT"},
		{"EXTRACT(timezone_minute FROM TIMETZ '10:00+05:30')", "30", "BIGINT"},
		{"EXTRACT(timezone FROM TIMETZ '10:00+15:59:59')", "57599", "BIGINT"},
		{"EXTRACT(hour FROM TIMETZ '10:00:05.5-05:30')", "10", "BIGINT"},
		{"EXTRACT(minute FROM TIMETZ '10:30+02')", "30", "BIGINT"},
		{"EXTRACT(second FROM TIMETZ '10:00:05.5-05:30')", "5.5", "DOUBLE PRECISION"},
		{"EXTRACT(milliseconds FROM TIMETZ '10:00:05.5+02')", "5500", "DOUBLE PRECISION"},
		{"EXTRACT(microseconds FROM TIMETZ '10:00:05.5+02')", "5500000", "BIGINT"},
		{"EXTRACT(epoch FROM TIMETZ '10:00:05.5-05:30')", "55805.5", "DOUBLE PRECISION"},
		{"EXTRACT(epoch FROM TIMETZ '24:00:00+02')", "79200", "DOUBLE PRECISION"},
		{"EXTRACT(hour FROM TIMETZ '24:00+02')", "24", "BIGINT"},
		{"DATE_PART('timezone', TIMETZ '10:00-05:30')", "-19800", ""},
		{"DATE_PART('epoch', TIMETZ '10:00+02')", "28800", ""},
		{"HOUR(TIMETZ '10:30+02')", "10", "BIGINT"},
		{"EXTRACT(day FROM TIMETZ '10:00+02')", `error: unit "day" not supported for type time with time zone`, ""},
		{"EXTRACT(year FROM TIMETZ '10:00+02')", `error: unit "year" not supported for type time with time zone`, ""},
		{"EXTRACT(foo FROM TIMETZ '10:00+02')", `error: unit "foo" not recognized for type time with time zone`, ""},
		{"TO_CHAR(TIMETZ '10:00+02', 'HH24')", "error: function to_char(time with time zone, varchar) does not exist", ""},
		{"DATE_TRUNC('hour', TIMETZ '10:30+02')", "error: expected a date, time or timestamp, got timetz", ""},
	})
}

// The session time zone: a time without an offset, a TIME, CURRENT_TIME
// and a timestamp with time zone take its offset, on the statement's date.
func TestTimeTZSessionZone(t *testing.T) {
	runZoneCasesAt(t, "America/New_York", julyNow, []scalarCase{
		{"CAST('10:00' AS TIMETZ)", "10:00:00-04", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ '10:00'", "10:00:00-04", ""},
		{"TIME WITH TIME ZONE '10:00:00.5'", "10:00:00.5-04", ""},
		{"CAST(TIME '10:00' AS TIMETZ)", "10:00:00-04", ""},
		{"CAST('2024-01-01 10:00' AS TIMETZ)", "10:00:00-05", ""},
		{"CAST('2024-07-01 10:00' AS TIMETZ)", "10:00:00-04", ""},
		{"CAST('10:00 EST' AS TIMETZ)", "10:00:00-05", ""},
		{"CAST('10:00 EDT' AS TIMETZ)", "10:00:00-04", ""},
		{"CAST('10:00 MSK' AS TIMETZ)", "10:00:00+03", ""},
		{"CAST('10:00 Europe/Paris' AS TIMETZ)", `error: invalid input syntax for type time with time zone: "10:00 Europe/Paris"`, ""},
		{"TIME '10:00' AT TIME ZONE 'UTC'", "14:00:00+00", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ '10:00+02' AT TIME ZONE 'America/New_York'", "04:00:00-04", ""},
		{"TIMETZ '10:00+02' AT LOCAL", "04:00:00-04", ""},
		{"CAST(TIMESTAMPTZ '2024-01-01 10:00:00+00' AS TIMETZ)", "05:00:00-05", ""},
		{"CAST(TIMESTAMPTZ '2024-07-01 10:00:00+00' AS TIMETZ)", "06:00:00-04", ""},
		{"CAST('10:00' AS TIMETZ) = '10:00-04'", "true", ""},
		{"TIME '10:00' = TIMETZ '10:00-04'", "true", ""},
		{"TIME '10:00' < TIMETZ '10:00-05'", "true", ""},
		{"COALESCE(NULL, TIME '10:00', TIMETZ '10:00+02')", "10:00:00-04", ""},
		{"CASE WHEN true THEN TIME '10:00' ELSE TIMETZ '10:00+02' END", "10:00:00-04", ""},
		// CURRENT_TIME is a time with time zone, LOCALTIME a TIME (16:00 UTC
		// is 12:00 EDT).
		{"CURRENT_TIME", "12:00:00-04", "TIME(6) WITH TIME ZONE"},
		{"CURRENT_TIME(0)", "12:00:00-04", "TIME(0) WITH TIME ZONE"},
		{"LOCALTIME", "12:00:00", "TIME(6)"},
		{"CURRENT_TIME = LOCALTIME", "true", ""},
		{"CAST(CURRENT_TIME AS TIME) = LOCALTIME", "true", ""},
		{"EXTRACT(timezone FROM CURRENT_TIME)", "-14400", ""},
		{"CURRENT_TIME + INTERVAL '1 hour'", "13:00:00-04", "TIME(6) WITH TIME ZONE"},
	})
	runZoneCasesAt(t, "America/New_York", januaryNow, []scalarCase{
		{"CAST('10:00' AS TIMETZ)", "10:00:00-05", ""},
		{"TIME '10:00' AT TIME ZONE 'UTC'", "15:00:00+00", ""},
		{"TIMETZ '10:00+02' AT TIME ZONE 'America/New_York'", "03:00:00-05", ""},
		{"CURRENT_TIME", "12:00:00-05", ""},
	})
	runZoneCasesAt(t, "Asia/Kolkata", julyNow, []scalarCase{
		{"CAST('10:00' AS TIMETZ)", "10:00:00+05:30", ""},
		{"TIME '10:00' AT TIME ZONE 'UTC'", "04:30:00+00", ""},
		{"CAST(TIMESTAMPTZ '2024-01-01 10:00:00+00' AS TIMETZ)", "15:30:00+05:30", ""},
	})
	runZoneCasesAt(t, "UTC+5", julyNow, []scalarCase{
		{"CAST('10:00' AS TIMETZ)", "10:00:00-05", ""},
		{"TIMETZ '10:00+02' AT TIME ZONE 'UTC+5'", "03:00:00-05", ""},
	})
}

// The column type: its names, metadata, Arrow field and storage.
func TestTimeTZColumnType(t *testing.T) {
	for _, c := range []struct{ sql, name string }{
		{"TIME WITH TIME ZONE", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ", "TIME(6) WITH TIME ZONE"},
		{"TIMETZ(3)", "TIME(3) WITH TIME ZONE"},
		{"TIME(2) WITH TIME ZONE", "TIME(2) WITH TIME ZONE"},
		{"TIME(0) WITH TIME ZONE", "TIME(0) WITH TIME ZONE"},
		{"TIMETZ(9)", "TIME(6) WITH TIME ZONE"},
		{"TIME WITHOUT TIME ZONE", "TIME(6)"},
	} {
		typ, ok := timeTZTypeName(c.sql)
		if c.name == "TIME(6)" {
			if ok {
				t.Errorf("%s: read as %s", c.sql, typ.SQLName())
			}
			continue
		}
		if !ok || typ.SQLName() != c.name {
			t.Errorf("%s: %s (%v), want %s", c.sql, typ.SQLName(), ok, c.name)
			continue
		}
		// The table metadata keeps the type.
		b, err := json.Marshal(typ)
		if err != nil {
			t.Fatal(err)
		}
		var back ColType
		if err := json.Unmarshal(b, &back); err != nil || back != typ {
			t.Errorf("%s: %s reads back as %+v (%v)", c.sql, b, back, err)
		}
		// Its Arrow field is a string with the type's name, which ingest reads
		// back.
		f := arrowField("t", typ, true)
		if f.Type.ID() != arrow.STRING || !f.HasMetadata() {
			t.Errorf("%s: field %v", c.sql, f)
		}
		if got, err := colTypeFromField(f); err != nil || got != typ {
			t.Errorf("%s: colTypeFromField = %s (%v)", c.sql, got.SQLName(), err)
		}
	}
	if _, err := colTypeFromSQL(sqlTypeSpec{Name: "TIMETZ", Params: []int{-1}}); err == nil ||
		err.Error() != "TIME(-1) WITH TIME ZONE precision must not be negative" {
		t.Errorf("TIMETZ(-1): %v", err)
	}
	// Stored as its text, read back exactly.
	typ := fracType(KindTimeTZ, 3, "")
	v, _ := Coerce(stringValue("10:00:00.125-05:30"), typ)
	enc := encodeStored(v)
	if enc != "10:00:00.125-05:30" {
		t.Errorf("stored as %q", enc)
	}
	if back, err := decodeStored(enc, typ); err != nil || back != v {
		t.Errorf("decoded %+v (%v), want %+v", back, err, v)
	}
	if err := storedFits("10:00:00+02", typ); err != nil {
		t.Errorf("storedFits: %v", err)
	}
	if err := storedFits("10:00:00", typ); err == nil {
		t.Errorf("storedFits accepts a time without an offset")
	}
}

// RANGE frames over a time with time zone: the bound is the current row's
// time ± the interval's time part, with its offset, unwrapped.
func TestTimeTZRangeFrame(t *testing.T) {
	cur, _ := parseTimeTZ("09:30+01", utcZone)
	iv, _ := intervalLiteral("1 hour")
	target, err := timeOffset(cur, iv, -1)
	if err != nil {
		t.Fatal(err)
	}
	// 08:30+01 (07:30 UTC): 10:00+02 (08:00 UTC) is after it.
	first, _ := parseTimeTZ("10:00+02", utcZone)
	if c, _ := compareValues(first, target); c <= 0 {
		t.Errorf("10:00+02 compared with %s: %d", target.Text(), c)
	}
}
