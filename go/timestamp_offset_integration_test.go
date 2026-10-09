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
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Timestamp text with an offset or a zone written to, compared with and
// read as a TIMESTAMP column: as in Postgres, the offset or zone is checked
// and ignored ('2024-01-05 10:00:00-05' is 10:00), also in the index
// query. A TIMESTAMP WITH TIME ZONE column keeps the instant. Up to v0.0.14
// a TIMESTAMP was the instant in UTC. Every result is Postgres 16's.
func TestTimestampTextOffsetSQL(t *testing.T) {
	h := newSQLHarness(t)
	h.dropViews("it_tso_v")
	h.dropTables("it_tso", "it_tso_def", "it_tso_alt", "it_tso_copy")
	h.exec(`CREATE TABLE it_tso (id INTEGER, ts TIMESTAMP, tz TIMESTAMPTZ, d DATE, s VARCHAR(40))`)
	for _, v := range []string{
		`(1, '2024-01-05 10:00:00-05', '2024-01-05 10:00:00-05', '2024-01-05 23:00:00-05', '2024-01-05 10:00:00-05')`,
		`(2, '2024-01-05 10:00:00+09', '2024-01-05 10:00:00+09', '2024-01-05 01:00:00+09', '2024-01-05 10:00:00+09')`,
		`(3, '2024-01-05 10:00:00 America/New_York', '2024-01-05 10:00:00 America/New_York',
			'2024-01-05 23:00:00 America/New_York', '2024-01-05 10:00:00 America/New_York')`,
		`(4, '2024-01-05 15:00:00', '2024-01-05 15:00:00', '2024-01-06', '2024-01-05 15:00:00')`,
		`(5, '2024-01-05 10:00:00Z', '2024-01-05 10:00:00Z', '2024-01-05', '2024-01-05 10:00:00Z')`,
	} {
		h.exec(`INSERT INTO it_tso VALUES ` + v)
	}
	h.expectError(`INSERT INTO it_tso (id, ts) VALUES (6, '2024-01-05 10:00:00+16')`,
		`time zone displacement out of range: "2024-01-05 10:00:00+16"`)
	h.expectRows(`SELECT id, CAST(ts AS VARCHAR), CAST(tz AS VARCHAR), CAST(d AS VARCHAR) FROM it_tso ORDER BY id`,
		"1|2024-01-05 10:00:00|2024-01-05 15:00:00+00|2024-01-05",
		"2|2024-01-05 10:00:00|2024-01-05 01:00:00+00|2024-01-05",
		"3|2024-01-05 10:00:00|2024-01-05 15:00:00+00|2024-01-05",
		"4|2024-01-05 15:00:00|2024-01-05 15:00:00+00|2024-01-06",
		"5|2024-01-05 10:00:00|2024-01-05 10:00:00+00|2024-01-05")
	// The column's text read as each type.
	h.expectRows(`SELECT id, CAST(CAST(s AS TIMESTAMP) AS VARCHAR), CAST(CAST(s AS TIMESTAMPTZ) AS VARCHAR),
		CAST(CAST(s AS DATE) AS VARCHAR), CAST(ts - CAST(s AS TIMESTAMP) AS VARCHAR) FROM it_tso ORDER BY id`,
		"1|2024-01-05 10:00:00|2024-01-05 15:00:00+00|2024-01-05|00:00:00",
		"2|2024-01-05 10:00:00|2024-01-05 01:00:00+00|2024-01-05|00:00:00",
		"3|2024-01-05 10:00:00|2024-01-05 15:00:00+00|2024-01-05|00:00:00",
		"4|2024-01-05 15:00:00|2024-01-05 15:00:00+00|2024-01-05|00:00:00",
		"5|2024-01-05 10:00:00|2024-01-05 10:00:00+00|2024-01-05|00:00:00")

	// Comparisons, run in the index and in the driver.
	for _, c := range []struct {
		where string
		want  []string
	}{
		{`ts = '2024-01-05 10:00:00-05'`, []string{"1", "2", "3", "5"}},
		{`ts = '2024-01-05 10:00:00+02'`, []string{"1", "2", "3", "5"}},
		{`ts = '2024-01-05 15:00:00-05'`, []string{"4"}},
		{`ts > '2024-01-05 10:00:00+09'`, []string{"4"}},
		{`ts >= '2024-01-05 10:00:00+09'`, []string{"1", "2", "3", "4", "5"}},
		{`ts < '2024-01-05 15:00:00 Asia/Tokyo'`, []string{"1", "2", "3", "5"}},
		{`ts <= '2024-01-05 10:00:00 America/Los_Angeles'`, []string{"1", "2", "3", "5"}},
		{`ts <> '2024-01-05 10:00:00-01'`, []string{"4"}},
		{`ts IN ('2024-01-05 10:00:00-05', '2024-01-05 15:00:00+05')`, []string{"1", "2", "3", "4", "5"}},
		{`ts BETWEEN '2024-01-05 10:00:00+12' AND '2024-01-05 12:00:00-12'`, []string{"1", "2", "3", "5"}},
		{`ts = TIMESTAMP '2024-01-05 15:00:00+03'`, []string{"4"}},
		{`ts = CAST('2024-01-05 15:00:00+03' AS TIMESTAMP)`, []string{"4"}},
		{`ts = CAST(s AS TIMESTAMP)`, []string{"1", "2", "3", "4", "5"}},
		{`tz = '2024-01-05 10:00:00-05'`, []string{"1", "3", "4"}},
		{`tz = TIMESTAMP '2024-01-05 15:00:00-05'`, []string{"1", "3", "4"}},
		{`d = '2024-01-05 23:59:00-12'`, []string{"1", "2", "3", "5"}},
	} {
		h.expectRows(`SELECT id FROM it_tso WHERE `+c.where+` ORDER BY id`, c.want...)
	}
	// The index query has the local time: 2024-01-05 10:00 is
	// 1704448800000000 microseconds, 15:00 1704466800000000.
	for where, want := range map[string]string{
		`ts = '2024-01-05 10:00:00-05'`:           "@ts:[1704448800000000 1704448800000000]",
		`ts > '2024-01-05 10:00:00+09'`:           "@ts:[(1704448800000000 +inf]",
		`ts = TIMESTAMP '2024-01-05 15:00:00+03'`: "@ts:[1704466800000000 1704466800000000]",
		`tz = '2024-01-05 10:00:00-05'`:           "@tz:[1704466800000000 1704466800000000]",
	} {
		if q, residual, _ := h.planOf(`SELECT id FROM it_tso WHERE ` + where); residual || !strings.Contains(q, want) {
			t.Errorf("%s: query %q (residual %v), want %s", where, q, residual, want)
		}
	}
	h.expectRows(`SELECT COUNT(*), CAST(MIN(ts) AS VARCHAR), CAST(MAX(ts) AS VARCHAR) FROM it_tso WHERE ts < '2024-01-06 00:00:00-12'`,
		"5|2024-01-05 10:00:00|2024-01-05 15:00:00")

	h.exec(`UPDATE it_tso SET ts = '2024-02-01 08:00:00+08:00:30' WHERE id = 5`)
	h.expectRows(`SELECT CAST(ts AS VARCHAR), COUNT(*) FROM it_tso GROUP BY ts ORDER BY ts`,
		"2024-01-05 10:00:00|3", "2024-01-05 15:00:00|1", "2024-02-01 08:00:00|1")

	// DEFAULTs, CHECKs and views read the text as the column's type.
	h.exec(`CREATE TABLE it_tso_def (id INTEGER, ts TIMESTAMP DEFAULT '2024-01-05 10:00:00-05',
		tz TIMESTAMPTZ DEFAULT '2024-01-05 10:00:00-05', CHECK (ts >= '2024-01-05 10:00:00+09'))`)
	h.exec(`INSERT INTO it_tso_def (id) VALUES (1)`)
	h.expectError(`INSERT INTO it_tso_def (id, ts) VALUES (2, '2024-01-05 09:59:59-05')`, "violates check constraint")
	h.exec(`INSERT INTO it_tso_def (id, ts) VALUES (3, '2024-01-05 10:00:00-05')`)
	h.expectRows(`SELECT id, CAST(ts AS VARCHAR), CAST(tz AS VARCHAR) FROM it_tso_def ORDER BY id`,
		"1|2024-01-05 10:00:00|2024-01-05 15:00:00+00", "3|2024-01-05 10:00:00|2024-01-05 15:00:00+00")
	h.exec(`CREATE VIEW it_tso_v AS SELECT id, CAST(ts AS VARCHAR) AS t FROM it_tso WHERE ts = '2024-01-05 10:00:00-05'`)
	h.expectRows(`SELECT * FROM it_tso_v ORDER BY id`, "1|2024-01-05 10:00:00", "2|2024-01-05 10:00:00", "3|2024-01-05 10:00:00")

	// A text column changed to TIMESTAMP, and rows written from a query.
	h.exec(`CREATE TABLE it_tso_alt (id INTEGER, s VARCHAR(40))`)
	h.exec(`INSERT INTO it_tso_alt VALUES (1, '2024-01-05 10:00:00-05'), (2, '2024-01-05 10:00:00 Europe/Paris')`)
	h.exec(`ALTER TABLE it_tso_alt ALTER COLUMN s TYPE TIMESTAMP USING CAST(s AS TIMESTAMP)`)
	h.expectRows(`SELECT id, CAST(s AS VARCHAR) FROM it_tso_alt ORDER BY id`, "1|2024-01-05 10:00:00", "2|2024-01-05 10:00:00")
	h.exec(`CREATE TABLE it_tso_copy AS SELECT id, CAST(s AS TIMESTAMP) AS ts FROM it_tso WHERE id <= 3`)
	h.exec(`INSERT INTO it_tso_copy SELECT 9, '2024-03-01 12:00:00-03'`)
	h.expectRows(`INSERT INTO it_tso_copy (id, ts) VALUES (10, '2024-03-01 12:00:00-03') RETURNING id, CAST(ts AS VARCHAR)`,
		"10|2024-03-01 12:00:00")
	h.expectRows(`SELECT id, CAST(ts AS VARCHAR) FROM it_tso_copy ORDER BY id`,
		"1|2024-01-05 10:00:00", "2|2024-01-05 10:00:00", "3|2024-01-05 10:00:00", "9|2024-03-01 12:00:00", "10|2024-03-01 12:00:00")

	// In New York: the TIMESTAMP is the time written, and the TIMESTAMP
	// WITH TIME ZONE the instant, shown in New York.
	h.exec(`SET TIME ZONE 'America/New_York'`)
	h.exec(`INSERT INTO it_tso (id, ts, tz) VALUES (7, '2024-01-05 10:00:00+02', '2024-01-05 10:00:00+02')`)
	h.expectRows(`SELECT CAST(ts AS VARCHAR), CAST(tz AS VARCHAR) FROM it_tso WHERE id = 7`, "2024-01-05 10:00:00|2024-01-05 03:00:00-05")
	h.expectRows(`SELECT id FROM it_tso WHERE ts = '2024-01-05 10:00:00+05:30' ORDER BY id`, "1", "2", "3", "7")
	h.expectRows(`SELECT id FROM it_tso WHERE tz = TIMESTAMP '2024-01-05 03:00:00+05:30' ORDER BY id`, "7")
	h.expectRows(`SELECT id FROM it_tso WHERE ts = TIMESTAMPTZ '2024-01-05 10:00:00-05' ORDER BY id`, "1", "2", "3", "7")
	h.exec(`SET TIME ZONE 'UTC'`)
}

// Bulk ingest and bound parameters read text as INSERT does: an offset or a
// zone is ignored for a TIMESTAMP or a DATE column and kept for a TIMESTAMP
// WITH TIME ZONE column. In a New York session, as Postgres 16 reads the
// same INSERT.
func TestTimestampTextOffsetIngest(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_tso_ing", "it_tso_ins")
	for _, table := range []string{"it_tso_ing", "it_tso_ins"} {
		h.exec(`CREATE TABLE ` + table + ` (id BIGINT, ts TIMESTAMP, d DATE, tz TIMESTAMPTZ)`)
	}
	h.exec(`SET TIME ZONE 'America/New_York'`)
	texts := []string{"2024-01-05 10:00:00-05", "2024-01-05 23:30:00+09", "2024-01-05 10:00:00 America/New_York", "2024-01-05 00:30:00-08:00"}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "ts", Type: arrow.BinaryTypes.String},
		{Name: "d", Type: arrow.BinaryTypes.String},
		{Name: "tz", Type: arrow.BinaryTypes.String},
	}, nil))
	defer b.Release()
	b.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
	for f := 1; f <= 3; f++ {
		b.Field(f).(*array.StringBuilder).AppendValues(texts, nil)
	}
	rec := b.NewRecordBatch()
	defer rec.Release()

	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: "it_tso_ing", adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeAppend} {
		if err := st.SetOption(h.ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 4 {
		t.Fatalf("ingest: %d, %v", n, err)
	}
	if n, err := h.stmtWith(`INSERT INTO it_tso_ins VALUES ($1, $2, $3, $4)`, rec).ExecuteUpdate(h.ctx); err != nil || n != 4 {
		t.Fatalf("insert: %d, %v", n, err)
	}
	for _, table := range []string{"it_tso_ing", "it_tso_ins"} {
		h.expectRows(`SELECT id, CAST(ts AS VARCHAR), CAST(d AS VARCHAR), CAST(tz AS VARCHAR) FROM `+table+` ORDER BY id`,
			"1|2024-01-05 10:00:00|2024-01-05|2024-01-05 10:00:00-05",
			"2|2024-01-05 23:30:00|2024-01-05|2024-01-05 09:30:00-05",
			"3|2024-01-05 10:00:00|2024-01-05|2024-01-05 10:00:00-05",
			"4|2024-01-05 00:30:00|2024-01-05|2024-01-05 03:30:00-05")
	}

	// A bound string compared with the column.
	pb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "p", Type: arrow.BinaryTypes.String},
	}, nil))
	defer pb.Release()
	pb.Field(0).(*array.StringBuilder).Append("2024-01-05 10:00:00+02")
	prec := pb.NewRecordBatch()
	defer prec.Release()
	rdr, _, err := h.stmtWith(`SELECT id FROM it_tso_ing WHERE ts = $1 ORDER BY id`, prec).ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Release()
	var got []string
	for rdr.Next() {
		ids := rdr.RecordBatch().Column(0).(*array.Int64)
		for i := 0; i < ids.Len(); i++ {
			got = append(got, ids.ValueStr(i))
		}
	}
	if strings.Join(got, ",") != "1,3" {
		t.Errorf("ts = $1: %v, want 1 and 3", got)
	}
	h.exec(`SET TIME ZONE 'UTC'`)
}
