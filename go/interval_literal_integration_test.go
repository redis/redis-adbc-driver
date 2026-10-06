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

// Integration tests for interval literals, which are read as Postgres 16
// reads them: a literal's time is whole microseconds. Bound and ingested
// Arrow month_day_nano values keep their nanoseconds.

import (
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSQLIntervalLiterals(t *testing.T) {
	h := newSQLHarness(t)
	drop := func() {
		h.exec("DROP TABLE IF EXISTS it_ivlit")
		h.exec("DROP TABLE IF EXISTS it_ivlit_ing")
	}
	drop()
	t.Cleanup(drop)
	str := func(e string) string { return "CAST(" + e + " AS VARCHAR)" }

	// Each field's integer part is exact, and its fraction is rounded to
	// the microsecond. The values are Postgres 16's.
	h.expectRows(`SELECT `+strings.Join([]string{
		str(`INTERVAL '0.0000015 seconds'`), str(`INTERVAL '1.0000005 sec'`), str(`INTERVAL '00:00:00.0000015'`),
		str(`INTERVAL '1000000:00:00.000001'`), str(`INTERVAL '2562047:47:16.854775'`), str(`INTERVAL '1:30.5'`),
		str(`INTERVAL '0.1 mon'`), str(`INTERVAL '0.125 years'`), str(`INTERVAL 'PT0.0000025S'`),
		str(`INTERVAL 'P0002-06-07T01:30:00'`), str(`INTERVAL '0.0000015' SECOND`)}, ", "),
		"00:00:00.000001|00:00:01|00:00:00.000002|1000000:00:00.000001|2562047:47:16.854775|00:01:30.5|"+
			"3 days|2 mons|00:00:00.000002|2 years 6 mons 7 days 01:30:00|00:00:00.000001")

	// Text from a column, read as an interval.
	h.exec(`CREATE TABLE it_ivlit (id INTEGER, s VARCHAR)`)
	h.exec(`INSERT INTO it_ivlit VALUES (1, '0.0000015 seconds'), (2, '-2562047:47:16.854775'), (3, '1.5 weeks ago'),
		(4, 'P1.5M'), (5, NULL)`)
	h.expectRows(`SELECT id, `+str(`CAST(s AS INTERVAL)`)+` FROM it_ivlit ORDER BY id`,
		"1|00:00:00.000001", "2|-2562047:47:16.854775", "3|-10 days -12:00:00", "4|1 mon 15 days", "5|NULL")
	// Text is read as it is: with a space before it, ISO 8601 is an error.
	h.exec(`INSERT INTO it_ivlit VALUES (6, ' P1D')`)
	h.expectError(`SELECT CAST(s AS INTERVAL) FROM it_ivlit WHERE id = 6`, `invalid input syntax for type interval: " P1D"`)

	h.expectError(`SELECT INTERVAL '1:75:00'`, `interval field value out of range: "1:75:00"`)
	h.expectError(`SELECT INTERVAL '1-12'`, `interval field value out of range: "1-12"`)
	// Postgres: 2562047:47:16.854776, past the nanoseconds' range.
	h.expectError(`SELECT INTERVAL '2562047:47:16.8547755'`, "interval out of range")

	// The fields are read from right to left, each unit once, and with
	// fields or SECOND(p), the fields after the last are dropped. The values
	// are Postgres 16's.
	h.expectRows(`SELECT `+strings.Join([]string{
		str(`INTERVAL '1 2 hours'`), str(`INTERVAL '1-2 years'`), str(`INTERVAL '1 day ago 2 hours'`),
		str(`INTERVAL '1.5' HOUR`), str(`INTERVAL '1 day 02:30' HOUR`), str(`INTERVAL '1:30' MINUTE TO SECOND`),
		str(`INTERVAL '3 4:05:06.789' DAY TO MINUTE`), str(`INTERVAL '1.23456' SECOND(2)`), str(`INTERVAL(0) '2.5'`)}, ", "),
		"1 day 02:00:00|1 year 2 mons|-1 days -02:00:00|01:00:00|1 day 02:00:00|00:01:30|3 days 04:05:00|00:00:01.23|00:00:03")
	h.expectError(`SELECT INTERVAL '1 day 2 days'`, `invalid input syntax for type interval: "1 day 2 days"`)
	h.expectError(`SELECT INTERVAL '1.5 seconds 3 microseconds'`, `invalid input syntax for type interval: "1.5 seconds 3 microseconds"`)
	h.expectError(`SELECT INTERVAL '1 quarter'`, `invalid input syntax for type interval: "1 quarter"`)
	h.expectError(`SELECT INTERVAL '2147483648 days'`, `interval field value out of range: "2147483648 days"`)
	h.expectError(`SELECT INTERVAL '1' YEAR TO DAY`, "syntax error")

	// A bound month_day_nano keeps its nanoseconds, and bound text is read
	// as a literal.
	mem := memory.DefaultAllocator
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, `SELECT CAST(? AS VARCHAR), CAST(CAST(? AS INTERVAL) AS VARCHAR)`); err != nil {
		t.Fatal(err)
	}
	ib := array.NewMonthDayNanoIntervalBuilder(mem)
	defer ib.Release()
	ib.Append(arrow.MonthDayNanoInterval{Months: 1, Days: 2, Nanoseconds: 1500})
	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.Append("0.0000015 seconds")
	cols := []arrow.Array{ib.NewArray(), sb.NewArray()}
	for _, c := range cols {
		defer c.Release()
	}
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "iv", Type: arrow.FixedWidthTypes.MonthDayNanoInterval},
		{Name: "s", Type: arrow.BinaryTypes.String},
	}, nil), cols, 1)
	defer rec.Release()
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rdr.Next()
	r := rdr.RecordBatch()
	if got, want := r.Column(0).ValueStr(0)+"|"+r.Column(1).ValueStr(0), "1 mon 2 days 00:00:00.0000015|00:00:00.000001"; got != want {
		t.Errorf("bound: got %q, want %q", got, want)
	}
	rdr.Release()

	// INTERVAL ? HOUR reads bound text for the typmod, and drops the fields
	// after the hours of a bound number too.
	hs, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close(h.ctx)
	if err := hs.SetSqlQuery(h.ctx, `SELECT CAST(INTERVAL ? HOUR AS VARCHAR), CAST(INTERVAL ? HOUR AS VARCHAR)`); err != nil {
		t.Fatal(err)
	}
	tb := array.NewStringBuilder(mem)
	defer tb.Release()
	tb.Append("1 day 02:30")
	fb := array.NewFloat64Builder(mem)
	defer fb.Release()
	fb.Append(1.5)
	hcols := []arrow.Array{tb.NewArray(), fb.NewArray()}
	for _, c := range hcols {
		defer c.Release()
	}
	hrec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "s", Type: arrow.BinaryTypes.String},
		{Name: "f", Type: arrow.PrimitiveTypes.Float64},
	}, nil), hcols, 1)
	defer hrec.Release()
	if err := hs.Bind(h.ctx, hrec); err != nil {
		t.Fatal(err)
	}
	hrdr, _, err := hs.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	hrdr.Next()
	hr := hrdr.RecordBatch()
	if got, want := hr.Column(0).ValueStr(0)+"|"+hr.Column(1).ValueStr(0), "1 day 02:00:00|01:00:00"; got != want {
		t.Errorf("INTERVAL ? HOUR: got %q, want %q", got, want)
	}
	hrdr.Release()

	// So does an ingested one.
	h.exec(`CREATE TABLE it_ivlit_ing (id BIGINT, iv INTERVAL)`)
	idb := array.NewInt64Builder(mem)
	defer idb.Release()
	idb.Append(1)
	ib.Append(arrow.MonthDayNanoInterval{Nanoseconds: -2500})
	cols = []arrow.Array{idb.NewArray(), ib.NewArray()}
	for _, c := range cols {
		defer c.Release()
	}
	rec = array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "iv", Type: arrow.FixedWidthTypes.MonthDayNanoInterval},
	}, nil), cols, 1)
	defer rec.Release()
	ing, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ing.Close(h.ctx)
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: "it_ivlit_ing", adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeAppend} {
		if err := ing.SetOption(h.ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := ing.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	if n, err := ing.ExecuteUpdate(h.ctx); err != nil || n != 1 {
		t.Fatalf("ingest: %d, %v", n, err)
	}
	h.expectRows(`SELECT id, `+str(`iv`)+` FROM it_ivlit_ing`, "1|-00:00:00.0000025")
}
