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

// Integration tests for text read as a NUMERIC, a DOUBLE PRECISION or
// REAL, a BOOLEAN, a DATE, a TIME or a TIMESTAMP (textin.go, value.go) by
// casts, INSERT and UPDATE, comparisons, bound parameters and bulk ingest.
// The values and errors of the statements are postgres:16's, but for the
// driver's `column "m": ` before an error writing a column.

import (
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

func TestSQLTextToTypes(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_textin")
	h.exec(`CREATE TABLE it_textin (id INTEGER, m NUMERIC(10, 2), u NUMERIC(10, 2) NOINDEX, f DOUBLE PRECISION, r REAL,
		b BOOLEAN, d DATE, tm TIME, tz TIMESTAMP WITH TIME ZONE, t VARCHAR)`)
	// Postgres's forms: underscores, 0x, hexadecimal doubles, prefixes of
	// true and false, a date's time and offset, a time's offset.
	h.exec(`INSERT INTO it_textin VALUES (1, '1_000.5', ' 0x1F ', '0x1p3', '1e-40', 'ye', '2024-01-05 23:00-05', '10:00+05',
		'2024-01-05 10:00+05:30:15', 'abc'),
		(2, '-.5', '1e2', '-NaN', '+Infinity', 'of', '2024-01-05 24:00', '23:59:60', '2024-01-05 10:00+530', '2024-02-30')`)
	all := []string{
		"1|1000.50|31.00|8|1e-40|true|2024-01-05|10:00:00|2024-01-05 04:29:45+00|abc",
		"2|-0.50|100.00|NaN|Infinity|false|2024-01-05|24:00:00|2024-01-05 04:30:00+00|2024-02-30",
	}
	const sel = `SELECT id, m::text, u::text, f::text, r::text, b, d::text, tm::text, tz::text, t FROM it_textin ORDER BY id`
	h.expectRows(sel, all...)

	// Text compared with the columns, indexed or not.
	h.expectRows(`SELECT id FROM it_textin WHERE m = '1_000.50'`, "1")
	h.expectRows(`SELECT id FROM it_textin WHERE u = '0b11111'`, "1")
	h.expectRows(`SELECT id FROM it_textin WHERE f < '0x10' ORDER BY id`, "1")
	h.expectRows(`SELECT id FROM it_textin WHERE b = 'Y'`, "1")
	h.expectRows(`SELECT id FROM it_textin WHERE d = '2024-01-05 10:00 UTC' ORDER BY id`, "1", "2")
	h.expectRows(`SELECT id FROM it_textin WHERE tm = '24:00'`, "2")

	const data = adbc.StatusInvalidData
	for _, c := range []struct{ sql, state, msg string }{
		// Written to a column.
		{`INSERT INTO it_textin (id, m) VALUES (3, 'x')`, "22P02", `column "m": invalid input syntax for type numeric: "x"`},
		{`INSERT INTO it_textin (id, m) VALUES (3, '123456789.5')`, "22003",
			`column "m": numeric field overflow; a field with precision 10, scale 2 must round to an absolute value less than 10^8`},
		{`INSERT INTO it_textin (id, u) VALUES (3, '1__0')`, "22P02", `column "u": invalid input syntax for type numeric: "1__0"`},
		{`INSERT INTO it_textin (id, f) VALUES (3, '1_000')`, "22P02", `column "f": invalid input syntax for type double precision: "1_000"`},
		{`INSERT INTO it_textin (id, f) VALUES (3, '1e400')`, "22003", `column "f": "1e400" is out of range for type double precision`},
		{`INSERT INTO it_textin (id, r) VALUES (3, '1e39')`, "22003", `column "r": "1e39" is out of range for type real`},
		{`INSERT INTO it_textin (id, b) VALUES (3, 'o')`, "22P02", `column "b": invalid input syntax for type boolean: "o"`},
		{`INSERT INTO it_textin (id, d) VALUES (3, '2024-02-30')`, "22008", `column "d": date/time field value out of range: "2024-02-30"`},
		{`INSERT INTO it_textin (id, tm) VALUES (3, '24:00:01')`, "22008", `column "tm": date/time field value out of range: "24:00:01"`},
		{`INSERT INTO it_textin (id, tz) VALUES (3, '2024-01-05 10:00-16')`, "22009",
			`column "tz": time zone displacement out of range: "2024-01-05 10:00-16"`},
		{`UPDATE it_textin SET m = '1e' WHERE id = 1`, "22P02", `column "m": invalid input syntax for type numeric: "1e"`},
		{`UPDATE it_textin SET d = 'x'`, "22007", `column "d": invalid input syntax for type date: "x"`},
		// Read from the rows.
		{`SELECT CAST(t AS NUMERIC) FROM it_textin`, "22P02", `invalid input syntax for type numeric: "abc"`},
		{`SELECT CAST(t AS DOUBLE PRECISION) FROM it_textin`, "22P02", `invalid input syntax for type double precision: "abc"`},
		{`SELECT CAST(t AS DATE) FROM it_textin WHERE id = 2`, "22008", `date/time field value out of range: "2024-02-30"`},
		// Compared with a column.
		{`SELECT id FROM it_textin WHERE m = 'x'`, "22P02", `invalid input syntax for type numeric: "x"`},
		{`SELECT id FROM it_textin WHERE u > '1.5.'`, "22P02", `invalid input syntax for type numeric: "1.5."`},
		{`SELECT id FROM it_textin WHERE f = '1_0'`, "22P02", `invalid input syntax for type double precision: "1_0"`},
		{`SELECT id FROM it_textin WHERE r = '1e39'`, "22003", `"1e39" is out of range for type real`},
		{`SELECT id FROM it_textin WHERE b = 'maybe'`, "22P02", `invalid input syntax for type boolean: "maybe"`},
		{`SELECT id FROM it_textin WHERE d >= 'x'`, "22007", `invalid input syntax for type date: "x"`},
		{`SELECT id FROM it_textin WHERE tm = '25:00'`, "22008", `date/time field value out of range: "25:00"`},
		{`SELECT id FROM it_textin WHERE tz < '2024-01-05 10:00 Mars/Base'`, "22023", `time zone "mars/base" not recognized`},
	} {
		checkState(t, c.sql, h.stmtErr(c.sql, nil), data, c.state, c.msg)
	}
	h.expectRows(sel, all...)

	// Bound strings.
	rec := textBatch("p", "1_0x")
	defer rec.Release()
	checkState(t, "bound '1_0x' inserted into NUMERIC", h.stmtErr(`INSERT INTO it_textin (id, m) VALUES (3, $1)`, rec),
		data, "22P02", `column "m": invalid input syntax for type numeric: "1_0x"`)
	checkState(t, "bound '1_0x' compared with DOUBLE PRECISION", h.stmtErr(`SELECT id FROM it_textin WHERE f = $1`, rec),
		data, "22P02", `invalid input syntax for type double precision: "1_0x"`)
	checkState(t, "bound '1_0x' cast to BOOLEAN", h.stmtErr(`SELECT CAST($1 AS BOOLEAN)`, rec),
		data, "22P02", `invalid input syntax for type boolean: "1_0x"`)
	ok := textBatch("p", " 1_000.5 ")
	defer ok.Release()
	if _, rows, _ := h.queryWith(`SELECT id FROM it_textin WHERE m = $1`, ok); len(rows) != 1 || rows[0] != "1" {
		t.Errorf("bound ' 1_000.5 ' compared with m: %q, want [1]", rows)
	}

	// Bulk ingest of strings into the columns: a batch with one that
	// doesn't convert writes nothing.
	ingest := func(rec arrow.RecordBatch) error {
		t.Helper()
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		for k, v := range map[string]string{
			adbc.OptionKeyIngestTargetTable: "it_textin",
			adbc.OptionKeyIngestMode:        adbc.OptionValueIngestModeAppend,
		} {
			if err := st.SetOption(h.ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.Bind(h.ctx, rec); err != nil {
			t.Fatal(err)
		}
		_, err = st.ExecuteUpdate(h.ctx)
		return err
	}
	for _, c := range []struct {
		rec        arrow.RecordBatch
		state, msg string
	}{
		{textBatch("m", "5", "abc"), "22P02", `column "m": invalid input syntax for type numeric: "abc"`},
		{textBatch("b", "t", "maybe"), "22P02", `column "b": invalid input syntax for type boolean: "maybe"`},
		{textBatch("d", "2024-01-05", "2024-02-30"), "22008", `column "d": date/time field value out of range: "2024-02-30"`},
	} {
		checkState(t, "ingest", ingest(c.rec), data, c.state, c.msg)
		c.rec.Release()
	}
	h.expectRows(sel, all...)
	good := textBatch("m", " 0x10 ", "2_5.5")
	defer good.Release()
	if err := ingest(good); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT m::text FROM it_textin WHERE id IS NULL ORDER BY m`, "16.00", "25.50")
}

// redis-adbc copy with -type reads the HASHes' text as a cast does, with
// Postgres's errors.
func TestHashScanTextNumbers(t *testing.T) {
	f := newHashFixture(t, "it:textin:")
	f.hset("it:textin:1", "m", "1_000.5", "b", "ye")
	f.hset("it:textin:2", "m", "abc", "b", "t")
	opts := HashCopyOptions{Prefix: f.prefix, KeyColumn: "_key", Types: map[string]string{"m": "NUMERIC(10,2)", "b": "BOOLEAN"}}
	s, err := scanHashes(f.h.ctx, f.opts, opts)
	if err != nil {
		t.Fatal(err)
	}
	for s.Next() {
	}
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), `invalid input syntax for type numeric: "abc"`) {
		t.Errorf("got %v", err)
	}
	s.Release()
	opts.NullOnError = true
	if s, err = scanHashes(f.h.ctx, f.opts, opts); err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	rows := scanAll(t, s)
	if rows["it:textin:1"] != "m=1000.5 b=true" || rows["it:textin:2"] != "m=NULL b=true" {
		t.Errorf("got %v", rows)
	}
}
