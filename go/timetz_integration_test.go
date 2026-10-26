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

// Integration tests for TIME WITH TIME ZONE columns (timetz.go). The
// expected rows are Postgres 16's for the same tables and queries; where
// they depend on the current date (a time without an offset read in New
// York), the offset is today's.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// flightTypeName is the Arrow field metadata key that names a time with
// time zone column's type, Flight SQL's.
const flightTypeName = "ARROW:FLIGHT:SQL:TYPE_NAME"

// setupTimeTZ creates it_ttz, whose times compare as Postgres orders
// them: 10:00+02, 09:00+01 and 08:00+00 are the same time in UTC, in that
// order, and 23:00-05 (28:00 UTC) is after 24:00+00.
func (h *sqlHarness) setupTimeTZ() {
	h.t.Helper()
	h.dropTables("it_ttz", "it_ttz2", "it_ttz3")
	h.exec(`CREATE TABLE it_ttz (id INTEGER, t TIMETZ, t3 TIME(3) WITH TIME ZONE, n INTEGER)`)
	h.exec(`INSERT INTO it_ttz VALUES (1, '10:00+02', '10:00:00.1234+02', 1), (2, '09:00+01', '09:00:00.5+01', 2),
		(3, '08:00+00', '08:00+00', 3), (4, '23:00-05', '23:00-05', 4), (5, '04:00+00', '04:00+00', 5),
		(6, '10:00:00.000+02', '24:00+00', 6), (7, NULL, NULL, 7)`)
	h.exec(`CREATE TABLE it_ttz2 (k INTEGER, t TIME WITH TIME ZONE)`)
	h.exec(`INSERT INTO it_ttz2 VALUES (1, '10:00+02'), (2, '08:00+00'), (3, '08:00+01')`)
	h.exec(`CREATE TABLE it_ttz3 (k INTEGER, tm TIME)`)
	h.exec(`INSERT INTO it_ttz3 VALUES (1, '08:00'), (2, '04:00'), (3, '10:00')`)
}

// Stored rows: text, ordering, filters, grouping, aggregates, joins,
// subqueries, set operations and window frames.
func TestTimeTZColumns(t *testing.T) {
	h := newSQLHarness(t)
	h.setupTimeTZ()
	schema := h.expectRows(`SELECT id, t, t3 FROM it_ttz ORDER BY id`,
		"1|10:00:00+02|10:00:00.123+02", "2|09:00:00+01|09:00:00.5+01", "3|08:00:00+00|08:00:00+00",
		"4|23:00:00-05|23:00:00-05", "5|04:00:00+00|04:00:00+00", "6|10:00:00+02|24:00:00+00", "7|NULL|NULL")
	// A string column, whose metadata names the type.
	for i, want := range map[int]string{1: "TIME(6) WITH TIME ZONE", 2: "TIME(3) WITH TIME ZONE"} {
		f := schema.Field(i)
		if f.Type.ID() != arrow.STRING {
			t.Errorf("%s: Arrow type %s", f.Name, f.Type)
		}
		if v, ok := f.Metadata.GetValue(flightTypeName); !ok || v != want {
			t.Errorf("%s: metadata %v, want %s = %s", f.Name, f.Metadata, flightTypeName, want)
		}
	}

	// ORDER BY: by the time in UTC, then larger offsets first.
	h.expectRows(`SELECT id FROM it_ttz ORDER BY t, id`, "5", "1", "6", "2", "3", "4", "7")
	h.expectRows(`SELECT id FROM it_ttz ORDER BY t DESC NULLS FIRST, id`, "7", "4", "3", "2", "1", "6", "5")
	h.expectRows(`SELECT id, t FROM it_ttz WHERE t IS NOT NULL ORDER BY t, id LIMIT 3`, "5|04:00:00+00", "1|10:00:00+02", "6|10:00:00+02")
	h.expectRows(`SELECT id, t FROM it_ttz ORDER BY t NULLS FIRST LIMIT 2`, "7|NULL", "5|04:00:00+00")

	// Filters: = needs the same time and offset.
	h.expectRows(`SELECT id FROM it_ttz WHERE t = '10:00+02' ORDER BY id`, "1", "6")
	h.expectRows(`SELECT id FROM it_ttz WHERE t = TIMETZ '09:00+01' ORDER BY id`, "2")
	h.expectRows(`SELECT id FROM it_ttz WHERE t > '08:00+00' ORDER BY id`, "4")
	h.expectRows(`SELECT id FROM it_ttz WHERE t >= '08:00+00' ORDER BY id`, "3", "4")
	h.expectRows(`SELECT id FROM it_ttz WHERE t < '09:00+01' ORDER BY id`, "1", "5", "6")
	h.expectRows(`SELECT id FROM it_ttz WHERE t <> '10:00+02' ORDER BY id`, "2", "3", "4", "5")
	h.expectRows(`SELECT id FROM it_ttz WHERE t IN ('10:00+02', '04:00+00') ORDER BY id`, "1", "5", "6")
	h.expectRows(`SELECT id FROM it_ttz WHERE t BETWEEN '04:00+00' AND '08:00+00' ORDER BY id`, "1", "2", "3", "5", "6")
	// The index doesn't hold the column: its comparisons are checked on the
	// rows the rest of the WHERE finds.
	h.expectRows(`SELECT id FROM it_ttz WHERE n < 6 AND t = '10:00+02' ORDER BY id`, "1")
	if q, residual, _ := h.planOf(`SELECT id FROM it_ttz WHERE n < 6 AND t = '10:00+02'`); !residual || strings.Contains(q, "@t") {
		t.Errorf("plan: query %q, residual %v", q, residual)
	}
	raw := h.rawClient()
	if attrs := h.indexAttrs(raw, "public", "it_ttz"); strings.Contains(" "+attrs+" ", " t ") || strings.Contains(attrs, "t3") {
		t.Errorf("index attributes: %s", attrs)
	}
	// Rows store the text.
	st := &store{client: raw}
	meta, err := st.getTable(h.ctx, "public", "it_ttz")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.aggregate(h.ctx, &aggRequest{index: meta.index(), query: "*", load: []string{"__key"}})
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]bool{}
	for _, r := range rows {
		if v, err := raw.HGet(h.ctx, r["__key"], "t3").Result(); err == nil {
			stored[v] = true
		}
	}
	for _, want := range []string{"10:00:00.123+02", "09:00:00.5+01", "24:00:00+00"} {
		if !stored[want] {
			t.Errorf("stored values %v have no %q", stored, want)
		}
	}

	// GROUP BY, DISTINCT and aggregates.
	h.expectRows(`SELECT t, COUNT(*) FROM it_ttz GROUP BY t ORDER BY t`,
		"04:00:00+00|1", "10:00:00+02|2", "09:00:00+01|1", "08:00:00+00|1", "23:00:00-05|1", "NULL|1")
	h.expectRows(`SELECT COUNT(DISTINCT t), COUNT(t), COUNT(*) FROM it_ttz`, "5|6|7")
	h.expectRows(`SELECT DISTINCT t FROM it_ttz ORDER BY t`,
		"04:00:00+00", "10:00:00+02", "09:00:00+01", "08:00:00+00", "23:00:00-05", "NULL")
	h.expectRows(`SELECT MIN(t), MAX(t), MIN(t3), MAX(t3) FROM it_ttz`, "04:00:00+00|23:00:00-05|04:00:00+00|23:00:00-05")
	h.expectRows(`SELECT STRING_AGG(CAST(t AS TEXT), ',' ORDER BY t) FROM it_ttz`,
		"04:00:00+00,10:00:00+02,10:00:00+02,09:00:00+01,08:00:00+00,23:00:00-05")

	// Joins, subqueries and set operations.
	h.expectRows(`SELECT a.id, b.k FROM it_ttz a JOIN it_ttz2 b ON a.t = b.t ORDER BY a.id, b.k`, "1|1", "3|2", "6|1")
	h.expectRows(`SELECT id FROM it_ttz WHERE t IN (SELECT t FROM it_ttz2) ORDER BY id`, "1", "3", "6")
	// A TIME is a time with time zone in the session time zone (UTC).
	h.expectRows(`SELECT a.id, c.k FROM it_ttz a JOIN it_ttz3 c ON a.t = c.tm ORDER BY a.id`, "3|1", "5|2")
	h.expectRows(`SELECT id FROM it_ttz WHERE t IN (SELECT tm FROM it_ttz3) ORDER BY id`, "3", "5")
	h.expectRows(`SELECT id FROM it_ttz WHERE t NOT IN (SELECT t FROM it_ttz2 WHERE t IS NOT NULL) ORDER BY id`, "2", "4", "5")
	h.expectRows(`SELECT id FROM it_ttz a WHERE EXISTS (SELECT 1 FROM it_ttz2 b WHERE b.t = a.t) ORDER BY id`, "1", "3", "6")
	h.expectRows(`SELECT t FROM it_ttz WHERE id <= 2 UNION SELECT t FROM it_ttz2 ORDER BY 1`,
		"08:00:00+01", "10:00:00+02", "09:00:00+01", "08:00:00+00")

	// RANGE frames: the time ± the interval, with the row's offset.
	h.expectRows(`SELECT id, COUNT(*) OVER (ORDER BY t RANGE BETWEEN INTERVAL '1 hour' PRECEDING AND CURRENT ROW)
		FROM it_ttz WHERE t IS NOT NULL ORDER BY id`, "1|2", "2|3", "3|4", "4|1", "5|1", "6|2")
	h.expectRows(`SELECT id, COUNT(*) OVER (ORDER BY t RANGE BETWEEN CURRENT ROW AND INTERVAL '30 minutes' FOLLOWING)
		FROM it_ttz WHERE t IS NOT NULL ORDER BY id`, "1|4", "2|2", "3|1", "4|1", "5|1", "6|4")

	// Functions of the column.
	h.expectRows(`SELECT id, t AT TIME ZONE 'UTC', CAST(CAST(t AS TIME) AS TEXT), EXTRACT(timezone FROM t), t + INTERVAL '1 hour',
			CAST(t AS TEXT)
		FROM it_ttz WHERE id IN (1, 4) ORDER BY id`,
		"1|08:00:00+00|10:00:00|7200|11:00:00+02|10:00:00+02", "4|04:00:00+00|23:00:00|-18000|00:00:00-05|23:00:00-05")
	h.expectRows(`SELECT id, t3 AT TIME ZONE 'Asia/Kolkata', CAST(t3 AS TIMETZ(0)) FROM it_ttz WHERE id IN (1, 2, 6) ORDER BY id`,
		"1|13:30:00.123+05:30|10:00:00+02", "2|13:30:00.5+05:30|09:00:01+01", "6|05:30:00+05:30|24:00:00+00")
	h.expectRows(`SELECT id, CAST(DATE '2024-01-01' + t AS TEXT) FROM it_ttz WHERE id IN (1, 4) ORDER BY id`,
		"1|2024-01-01 08:00:00+00", "4|2024-01-02 04:00:00+00")

	// Writes: UPDATE, CREATE TABLE AS, ADD COLUMN … DEFAULT.
	h.exec(`UPDATE it_ttz SET t = t + INTERVAL '1 hour' WHERE id = 3`)
	h.expectRows(`SELECT t FROM it_ttz WHERE id = 3`, "09:00:00+00")
	h.dropTables("it_ttz_ctas")
	h.exec(`CREATE TABLE it_ttz_ctas AS SELECT id, t, t3 FROM it_ttz`)
	h.expectRows(`SELECT id, t, t3 FROM it_ttz_ctas WHERE id IN (1, 6) ORDER BY id`, "1|10:00:00+02|10:00:00.123+02", "6|10:00:00+02|24:00:00+00")
	h.exec(`ALTER TABLE it_ttz ADD COLUMN d TIMETZ(0) DEFAULT '12:00:00.6+05'`)
	h.expectRows(`SELECT COUNT(*), MIN(d), MAX(d) FROM it_ttz`, "7|12:00:01+05|12:00:01+05")

	// The column's type in the catalog.
	h.expectRows(`SELECT column_name, driver_data_type, datetime_precision, is_indexed FROM information_schema.columns
		WHERE table_name = 'it_ttz' AND column_name LIKE 't%' ORDER BY ordinal_position`,
		"t|TIME(6) WITH TIME ZONE|6|NO", "t3|TIME(3) WITH TIME ZONE|3|NO")
	h.expectRows(`SELECT driver_data_type FROM information_schema.columns WHERE table_name = 'it_ttz_ctas' AND column_name = 't3'`,
		"TIME(3) WITH TIME ZONE")
	if got := strings.Join(h.columnDigits("it_ttz"), ", "); !strings.Contains(got, "t TIME(6) WITH TIME ZONE 6") ||
		!strings.Contains(got, "t3 TIME(3) WITH TIME ZONE 3") {
		t.Errorf("GetObjects: %s", got)
	}
	ts, err := h.conn.GetTableSchema(h.ctx, nil, nil, "it_ttz")
	if err != nil {
		t.Fatal(err)
	}
	if f := ts.Field(1); f.Type.ID() != arrow.STRING || f.Metadata.Values()[0] != "TIME(6) WITH TIME ZONE" {
		t.Errorf("GetTableSchema: %v", f)
	}
}

// The session time zone: a time without an offset takes its offset today;
// AT TIME ZONE and CURRENT_TIME; views, defaults and CHECKs read their
// literals in the zone they are created in.
func TestTimeTZSessionZoneColumns(t *testing.T) {
	h := newSQLHarness(t)
	h.setupTimeTZ()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(ny)
	_, todayOff := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, ny).Zone()
	_, nowOff := now.Zone()
	local := formatOffset(todayOff, false)
	h.exec(`SET TIME ZONE 'America/New_York'`)
	h.exec(`INSERT INTO it_ttz (id, t) VALUES (8, '10:00'), (9, TIME '11:00'), (10, CAST(TIMESTAMPTZ '2024-01-10 15:00:00+00' AS TIMETZ))`)
	h.expectRows(`SELECT id, t FROM it_ttz WHERE id >= 8 ORDER BY id`,
		"8|10:00:00"+local, "9|11:00:00"+local, "10|10:00:00-05")
	h.expectRows(`SELECT id, t AT TIME ZONE 'America/New_York', t AT LOCAL FROM it_ttz WHERE id = 1`,
		"1|"+formatTime(int64((8*3600+nowOff+86400)%86400), arrow.Second)+formatOffset(nowOff, false)+"|"+
			formatTime(int64((8*3600+nowOff+86400)%86400), arrow.Second)+formatOffset(nowOff, false))
	h.expectRows(`SELECT CAST('10:00' AS TIME) AT TIME ZONE 'UTC'`,
		formatTime(int64((10*3600-todayOff)%86400), arrow.Second)+"+00")
	h.expectRows(`SELECT id FROM it_ttz WHERE t = '10:00' ORDER BY id`, "8")
	h.expectRows(`SELECT CURRENT_TIME = LOCALTIME, CAST(CURRENT_TIME AS TIME) = LOCALTIME, EXTRACT(timezone FROM CURRENT_TIME)`,
		"true|true|"+strconv.Itoa(nowOff))
	if _, schema := h.query(`SELECT CURRENT_TIME, CURRENT_TIME(0), LOCALTIME`); schema.Field(0).Type.ID() != arrow.STRING ||
		schema.Field(1).Type.ID() != arrow.STRING || schema.Field(2).Type.String() != "time64[us]" {
		t.Errorf("current time types: %s", schema)
	} else if v, _ := schema.Field(1).Metadata.GetValue(flightTypeName); v != "TIME(0) WITH TIME ZONE" {
		t.Errorf("CURRENT_TIME(0) metadata: %v", schema.Field(1).Metadata)
	}

	// Created in New York: the literals keep its offset in every session.
	h.dropViews("it_ttz_v")
	h.dropTables("it_ttz_def")
	h.exec(`CREATE VIEW it_ttz_v AS SELECT id, CAST('10:00' AS TIMETZ) AS lit, TIMETZ '11:00' AS typed FROM it_ttz WHERE t = '10:00'`)
	h.exec(`CREATE TABLE it_ttz_def (id INTEGER, t TIMETZ DEFAULT '10:00', u TIMETZ CHECK (u > '09:00'))`)
	h.expectRows(`SELECT view_definition LIKE '%''10:00:00`+local+`''%', view_definition LIKE '%''11:00:00`+local+`''%'
		FROM information_schema.views WHERE table_name = 'it_ttz_v'`, "true|true")
	h.expectRows(`SELECT column_default FROM information_schema.columns WHERE table_name = 'it_ttz_def' AND column_name = 't'`,
		"'10:00:00"+local+"'")
	utc := newSQLHarness(t)
	utc.expectRows(`SELECT id, lit, typed FROM it_ttz_v`, "8|10:00:00"+local+"|11:00:00"+local)
	utc.exec(`INSERT INTO it_ttz_def (id, u) VALUES (1, '09:30` + local + `')`)
	utc.expectRows(`SELECT id, t, u FROM it_ttz_def`, "1|10:00:00"+local+"|09:30:00"+local)
	utc.expectError(`INSERT INTO it_ttz_def (id, u) VALUES (2, '08:30`+local+`')`, "violates check constraint")
}

// Bulk ingest and bound parameters: text, with or without an offset, into
// a time with time zone column, and a string field whose metadata names the
// type, which a new table takes.
func TestTimeTZIngestAndBind(t *testing.T) {
	h := newSQLHarness(t)
	h.setupTimeTZ()
	h.dropTables("it_ttz_ing", "it_ttz_new")
	h.exec(`CREATE TABLE it_ttz_ing (id BIGINT, t TIMETZ)`)
	h.exec(`SET TIME ZONE 'Asia/Kolkata'`)
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "t", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil))
	defer b.Release()
	b.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"10:00+02", "10:00", ""}, []bool{true, true, false})
	rec := b.NewRecordBatch()
	defer rec.Release()
	ingest := func(table, mode string, rec arrow.RecordBatch) {
		t.Helper()
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
			if err := st.SetOption(h.ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.Bind(h.ctx, rec); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ExecuteUpdate(h.ctx); err != nil {
			t.Fatalf("ingest into %s: %v", table, err)
		}
	}
	ingest("it_ttz_ing", adbc.OptionValueIngestModeAppend, rec)
	// Kolkata has had +05:30 since 1945.
	h.expectRows(`SELECT id, t FROM it_ttz_ing ORDER BY id`, "1|10:00:00+02", "2|10:00:00+05:30", "3|NULL")

	// A result's field, ingested into a new table, makes a time with time
	// zone column.
	rdr := h.stmtWith(`SELECT id, t3 FROM it_ttz WHERE id <= 2 ORDER BY id`, nil)
	res, _, err := rdr.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Release()
	if !res.Next() {
		t.Fatal("no batch")
	}
	ingest("it_ttz_new", adbc.OptionValueIngestModeCreate, res.RecordBatch())
	h.expectRows(`SELECT driver_data_type FROM information_schema.columns WHERE table_name = 'it_ttz_new' AND column_name = 't3'`,
		"TIME(3) WITH TIME ZONE")
	h.expectRows(`SELECT id, t3 FROM it_ttz_new ORDER BY id`, "1|10:00:00.123+02", "2|09:00:00.5+01")

	// Parameters: text read as a time with time zone where one is wanted.
	pb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.BinaryTypes.String}, {Name: "b", Type: arrow.BinaryTypes.String}}, nil))
	defer pb.Release()
	pb.Field(0).(*array.StringBuilder).Append("10:00+02")
	pb.Field(1).(*array.StringBuilder).Append("11:00")
	params := pb.NewRecordBatch()
	defer params.Release()
	sel := h.stmtWith(`SELECT id FROM it_ttz WHERE t = $1 OR t = CAST($2 AS TIMETZ) ORDER BY id`, params)
	got, _, err := sel.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for got.Next() {
		col := got.RecordBatch().Column(0)
		for i := 0; i < col.Len(); i++ {
			ids = append(ids, col.ValueStr(i))
		}
	}
	got.Release()
	if strings.Join(ids, ",") != "1,6" {
		t.Errorf("WHERE t = $1: %v", ids)
	}
	ins := h.stmtWith(`INSERT INTO it_ttz_ing VALUES (4, $1), (5, $2)`, params)
	if _, err := ins.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT id, t FROM it_ttz_ing WHERE id >= 4 ORDER BY id`, "4|10:00:00+02", "5|11:00:00+05:30")
}
