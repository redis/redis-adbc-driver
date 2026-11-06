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

// Integration tests for ALTER TABLE … ALTER COLUMN … [SET DATA] TYPE (#114).
// The expected results and errors are postgres:16's.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// columnTypes lists a table's columns as "column type" from
// information_schema.columns.driver_data_type. GetObjects' xdbc_type_name must say
// the same, and GetTableSchema must have the type's Arrow type.
func (h *sqlHarness) columnTypes(table string) []string {
	h.t.Helper()
	got, _ := h.query(fmt.Sprintf(`SELECT column_name || ' ' || driver_data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = '%s' ORDER BY ordinal_position`, table))
	schema := defaultSchema
	rdr, err := h.conn.GetObjects(h.ctx, adbc.ObjectDepthColumns, nil, &schema, &table, nil, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rdr.Release()
	var objects []string
	for rdr.Next() {
		var buf bytes.Buffer
		if err := array.RecordToJSON(rdr.RecordBatch(), &buf); err != nil {
			h.t.Fatal(err)
		}
		for dec := json.NewDecoder(&buf); dec.More(); {
			var cat struct {
				Schemas []struct {
					Tables []struct {
						Name    string `json:"table_name"`
						Columns []struct {
							Name string `json:"column_name"`
							Type string `json:"xdbc_type_name"`
						} `json:"table_columns"`
					} `json:"db_schema_tables"`
				} `json:"catalog_db_schemas"`
			}
			if err := dec.Decode(&cat); err != nil {
				h.t.Fatal(err)
			}
			for _, s := range cat.Schemas {
				for _, t := range s.Tables {
					for _, c := range t.Columns {
						if t.Name == table {
							objects = append(objects, c.Name+" "+c.Type)
						}
					}
				}
			}
		}
	}
	if strings.Join(objects, ", ") != strings.Join(got, ", ") {
		h.t.Errorf("%s: GetObjects %q, information_schema %q", table, objects, got)
	}
	s, err := h.conn.GetTableSchema(h.ctx, nil, nil, table)
	if err != nil {
		h.t.Fatal(err)
	}
	for i, f := range s.Fields() {
		if i >= len(got) {
			break
		}
		name, typ, _ := strings.Cut(got[i], " ")
		ct, err := parseTypeText(typ)
		if err != nil {
			h.t.Fatalf("%s: %v", typ, err)
		}
		if f.Name != name || !arrowSame(f.Type.String(), ct) {
			h.t.Errorf("%s: GetTableSchema has %s %s, information_schema %s", table, f.Name, f.Type, got[i])
		}
	}
	return got
}

// arrowSame reports whether an Arrow type (as a string) is t's.
func arrowSame(arrowType string, t ColType) bool { return t.ArrowType().String() == arrowType }

func (h *sqlHarness) expectTypes(table string, want ...string) {
	h.t.Helper()
	if got := h.columnTypes(table); strings.Join(got, ", ") != strings.Join(want, ", ") {
		h.t.Errorf("%s: types %q, want %q", table, got, want)
	}
}

// attrTypes lists a table's index attributes as "attribute TYPE", in order.
func (h *sqlHarness) attrTypes(raw goredis.UniversalClient, table string) string {
	h.t.Helper()
	st, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	meta, err := st.getTable(ctx, defaultSchema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	reply, err := st.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	var out []string
	for _, a := range indexInfoOf(reply).attrs {
		out = append(out, a.identifier+" "+a.typ)
	}
	return strings.Join(out, ", ")
}

// lastNames returns the names a table's name was last given (by CREATE,
// TRUNCATE, a re-key or a rewrite).
func (h *sqlHarness) lastNames(raw goredis.UniversalClient, table string) tableNames {
	h.t.Helper()
	n, err := (&store{client: raw}).lastNames(h.ctx, raw, defaultSchema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	return namesFor(defaultSchema, table, n)
}

// expectRolledBack checks that a failed ALTER left a table as it was: its
// metadata (meta, as stored before), and consistent row ownership after
// bounded reclamation of its private rewrite.
func (h *sqlHarness) expectRolledBack(raw goredis.UniversalClient, table, meta string) {
	h.t.Helper()
	if got := h.rawMeta(raw, defaultSchema, table); got != meta {
		h.t.Errorf("%s: metadata changed\n got: %s\nwant: %s", table, got, meta)
	}
	// Aborted private generations retire through the snapshot reference graph;
	// the old mutable rename/name registries do not own these objects.
	st := &store{client: raw, snapshots: true}
	snapshotReapTest(st, h.ctx)
	assertSnapshotDataReferences(h.t, h, st)
	graph, err := raw.HGetAll(h.ctx, snapshotMetadataGraph).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	for field, count := range graph {
		if strings.HasPrefix(field, "r:") && count == "0" {
			h.t.Error("unreferenced rewrite output survived cleanup")
		}
	}
	indexes, err := raw.Do(h.ctx, "FT._LIST").StringSlice()
	if err != nil {
		h.t.Fatal(err)
	}
	for _, index := range indexes {
		if data := snapshotDataID(index); data != "" && graph["d:"+data] == "" {
			h.t.Error("rewrite index survived without registered ownership")
		}
	}
}

// The values ALTER COLUMN … TYPE converts, without and with USING, in the
// session time zone, as postgres:16 converts them. Each case alters a new
// copy of the table.
func TestSQLAlterTypeValues(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_at_val")
	raw := h.rawClient()
	h.exec(`SET TIME ZONE 'America/New_York'`)
	setup := func() {
		h.dropTables("it_at_val")
		h.exec(`CREATE TABLE it_at_val (id int, f double precision, r real, n numeric(10,3), s text, b bool, d date,
			ts timestamp(6), tz timestamptz, tm time, iv interval, by bytea)`)
		h.exec(`INSERT INTO it_at_val VALUES
			(1, 2.25, 1.5, 2.5, 'ab   ', true, '2024-02-29', '2024-03-10 01:30:00.123456', '2024-03-10 06:30:00+00', '23:59:59.999', '1 day 02:03:04', '\x0102'),
			(2, 3.75, -2.5, -2.5, 'abcd', false, '1999-12-31', '1999-12-31 23:59:59.999999', '1999-12-31 23:59:59.5+00', '00:00:00.5', '-3 mons', 'abc'),
			(3, 'NaN', 'Infinity', 1234567.125, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
	}
	for _, c := range []struct {
		alter, sel string
		want       []string // or err
		err        string
		code       adbc.Status
		// field is the altered column's value as row 1 stores it.
		field string
	}{
		// Numbers.
		{alter: `ALTER TABLE it_at_val ALTER f TYPE int`, err: `integer out of range`, code: adbc.StatusInvalidData},
		{alter: `ALTER TABLE it_at_val ALTER f TYPE int USING CASE WHEN f = 'NaN' THEN 0 ELSE f END`, sel: `f`, want: []string{"2", "4", "0"}},
		{alter: `ALTER TABLE it_at_val ALTER r TYPE bigint USING CASE WHEN r = 'Infinity' THEN 7 ELSE r * 2 END`, sel: `r`, want: []string{"3", "-5", "7"}},
		{alter: `ALTER TABLE it_at_val ALTER n TYPE int`, sel: `n`, want: []string{"3", "-3", "1234567"}},
		{alter: `ALTER TABLE it_at_val ALTER n TYPE numeric(10,1)`, sel: `n::text`, want: []string{"2.5", "-2.5", "1234567.1"}},
		{alter: `ALTER TABLE it_at_val ALTER n TYPE numeric(5,1)`, err: `numeric field overflow; a field with precision 5, scale 1 must round to an absolute value less than 10^4`, code: adbc.StatusInvalidData},
		{alter: `ALTER TABLE it_at_val ALTER f TYPE real`, sel: `f::text`, want: []string{"2.25", "3.75", "NaN"}},
		{alter: `ALTER TABLE it_at_val ALTER r TYPE double precision`, sel: `r::text`, want: []string{"1.5", "-2.5", "Infinity"}},
		{alter: `ALTER TABLE it_at_val ALTER n TYPE double precision`, sel: `n::numeric(10,3)::text`, want: []string{"2.500", "-2.500", "1234567.125"}},
		{alter: `ALTER TABLE it_at_val ALTER f TYPE text`, sel: `f`, want: []string{"2.25", "3.75", "NaN"}},
		{alter: `ALTER TABLE it_at_val ALTER n TYPE text`, sel: `n`, want: []string{"2.500", "-2.500", "1234567.125"}},
		{alter: `ALTER TABLE it_at_val ALTER id TYPE smallint`, sel: `id`, want: []string{"1", "2", "3"}},
		{alter: `ALTER TABLE it_at_val ALTER id TYPE smallint USING CASE WHEN id = 1 THEN 100000 ELSE id END`, err: `smallint out of range`, code: adbc.StatusInvalidData},
		// Strings: an assignment, so a value that is too long is an error,
		// unless the rest is spaces (cut), and CHAR pads.
		{alter: `ALTER TABLE it_at_val ALTER s TYPE varchar(2)`, err: `value too long for type character varying(2)`, code: adbc.StatusInvalidData},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE varchar(4)`, sel: `'[' || s || ']', length(s)`, want: []string{"[ab  ]|4", "[abcd]|4", "NULL|NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE char(6)`, sel: `'[' || s::text || ']', length(s)`, want: []string{"[ab]|2", "[abcd]|4", "NULL|NULL"}, field: "ab    "},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE char(3)`, err: `value too long for type character(3)`, code: adbc.StatusInvalidData},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE varchar(3) USING s || 'xxxx'`, err: `value too long for type character varying(3)`, code: adbc.StatusInvalidData},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE varchar(3) USING CAST(s AS varchar(3))`, sel: `'[' || s || ']'`, want: []string{"[ab ]", "[abc]", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE int USING length(s)`, sel: `s`, want: []string{"5", "4", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER s TYPE text USING upper(s) || id`, sel: `s`, want: []string{"AB   1", "ABCD2", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER b TYPE text`, sel: `b`, want: []string{"true", "false", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER b TYPE char(3)`, err: `value too long for type character(3)`, code: adbc.StatusInvalidData},
		{alter: `ALTER TABLE it_at_val ALTER b TYPE int USING b::int`, sel: `b`, want: []string{"1", "0", "NULL"}},
		// As CAST(by AS text) converts it (Postgres reads 'abc' as three bytes,
		// \x616263; the driver keeps a BYTEA as written).
		{alter: `ALTER TABLE it_at_val ALTER by TYPE text`, sel: `by`, want: []string{`\x0102`, `abc`, "NULL"}},
		// Dates and times, in the session time zone.
		{alter: `ALTER TABLE it_at_val ALTER d TYPE timestamptz`, sel: `d::text`, want: []string{"2024-02-29 00:00:00-05", "1999-12-31 00:00:00-05", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER d TYPE text`, sel: `d`, want: []string{"2024-02-29", "1999-12-31", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER ts TYPE timestamptz`, sel: `ts::text`, want: []string{"2024-03-10 01:30:00.123456-05", "1999-12-31 23:59:59.999999-05", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER ts TYPE timestamp(2)`, sel: `ts::text`, want: []string{"2024-03-10 01:30:00.12", "2000-01-01 00:00:00", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER ts TYPE date`, sel: `ts::text`, want: []string{"2024-03-10", "1999-12-31", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER ts TYPE time(0)`, sel: `ts::text`, want: []string{"01:30:00", "24:00:00", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER tz TYPE timestamp`, sel: `tz::text`, want: []string{"2024-03-10 01:30:00", "1999-12-31 18:59:59.5", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER tz TYPE date`, sel: `tz::text`, want: []string{"2024-03-10", "1999-12-31", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER tz TYPE text`, sel: `tz`, want: []string{"2024-03-10 01:30:00-05", "1999-12-31 18:59:59.5-05", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER tm TYPE time(0)`, sel: `tm::text`, want: []string{"24:00:00", "00:00:01", "NULL"}},
		{alter: `ALTER TABLE it_at_val ALTER iv TYPE text`, sel: `iv`, want: []string{"1 day 02:03:04", "-3 mons", "NULL"}},
	} {
		setup()
		before := h.rawMeta(raw, defaultSchema, "it_at_val")
		if c.err != "" {
			h.expectQueryError(c.alter, c.code, c.err)
			h.expectRolledBack(raw, "it_at_val", before)
			h.expectRows(`SELECT count(*) FROM it_at_val`, "3")
			continue
		}
		h.exec(c.alter)
		h.expectRows(`SELECT `+c.sel+` FROM it_at_val ORDER BY id`, c.want...)
		if c.field != "" {
			if got := h.storedField(raw, defaultSchema, "it_at_val", "1", "s"); got != c.field {
				t.Errorf("%s: stored %q, want %q", c.alter, got, c.field)
			}
		}
	}

	// TIME WITH TIME ZONE, in a zone without daylight saving time (a TIME
	// takes the zone's offset on the current date).
	h.exec(`SET TIME ZONE 'Etc/GMT+5'`)
	h.dropTables("it_at_tz")
	h.exec(`CREATE TABLE it_at_tz (id int, tm time, tt timetz, tz timestamptz, ts timestamp)`)
	h.exec(`INSERT INTO it_at_tz VALUES (1, '10:30:00', '10:30:00+02', '2024-03-10 06:30:00+00', '2024-03-10 06:30:00')`)
	h.expectQueryError(`ALTER TABLE it_at_tz ALTER ts TYPE timetz`, adbc.StatusInvalidArgument,
		`column "ts" cannot be cast automatically to type time with time zone; you might need to specify "USING ts::time with time zone"`)
	h.exec(`ALTER TABLE it_at_tz ALTER tm TYPE timetz, ALTER tt TYPE time, ALTER tz TYPE timetz`)
	h.expectRows(`SELECT tm::text, tt::text, tz::text FROM it_at_tz`, "10:30:00-05|10:30:00|01:30:00-05")
	h.expectTypes("it_at_tz", "id INTEGER", "tm TIME(6) WITH TIME ZONE", "tt TIME(6)", "tz TIME(6) WITH TIME ZONE", "ts TIMESTAMP(6)")
}

// The converted rows are checked against NOT NULL and the CHECKs that read
// the column (bound again for its new type), and against the statement's
// new constraints, as in Postgres.
func TestSQLAlterTypeConstraints(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_atc", "it_atc2", "it_atc3")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_atc (id int, c text CHECK (length(c) < 3), n int NOT NULL, v numeric(6,2) CHECK (v > 0))`)
	h.exec(`INSERT INTO it_atc VALUES (1, '10', 1, 1.25), (2, '20', 2, 2.5)`)
	h.exec(`ALTER TABLE it_atc ALTER COLUMN v TYPE int`)
	h.expectRows(`SELECT * FROM it_atc ORDER BY id`, "1|10|1|1", "2|20|2|3")
	h.expectChecks(raw, "it_atc", "it_atc_c_check: length(c) < 3", "it_atc_v_check: v > 0")
	h.expectNullable("it_atc", "id=YES", "c=YES", "n=NO", "v=YES")
	before := h.rawMeta(raw, defaultSchema, "it_atc")
	for _, c := range []struct {
		sql  string
		code adbc.Status
		msg  string
	}{
		{`ALTER TABLE it_atc ALTER COLUMN v TYPE numeric(6,2) USING v - 3`, adbc.StatusIntegrity,
			`check constraint "it_atc_v_check" of relation "it_atc" is violated by some row`},
		{`ALTER TABLE it_atc ALTER COLUMN c TYPE text USING c || 'xx'`, adbc.StatusIntegrity,
			`check constraint "it_atc_c_check" of relation "it_atc" is violated by some row`},
		{`ALTER TABLE it_atc ALTER COLUMN n TYPE int USING NULLIF(n, 2)`, adbc.StatusIntegrity,
			`column "n" of relation "it_atc" contains null values`},
		{`ALTER TABLE it_atc ALTER COLUMN n TYPE text USING CASE WHEN n = 2 THEN NULL ELSE n::text END`, adbc.StatusIntegrity,
			`column "n" of relation "it_atc" contains null values`},
		{`ALTER TABLE it_atc ALTER COLUMN n TYPE smallint USING CASE WHEN n = 1 THEN 100000 ELSE n END`, adbc.StatusInvalidData, `smallint out of range`},
	} {
		h.expectQueryError(c.sql, c.code, c.msg)
		h.expectRolledBack(raw, "it_atc", before)
	}
	h.expectNullable("it_atc", "id=YES", "c=YES", "n=NO", "v=YES")
	h.expectQueryError(`INSERT INTO it_atc VALUES (3, 'abc', 3, 1)`, adbc.StatusIntegrity,
		`new row for relation "it_atc" violates check constraint "it_atc_c_check"`)

	// A CHECK that no longer works for the new type.
	h.exec(`CREATE TABLE it_atc2 (f boolean CHECK (f), g int)`)
	h.exec(`INSERT INTO it_atc2 VALUES (true, 1)`)
	h.expectQueryError(`ALTER TABLE it_atc2 ALTER COLUMN f TYPE int USING f::int`, adbc.StatusInvalidArgument,
		`argument of CHECK must be type boolean, not type integer`)

	// With other actions: the DEFAULT goes first, the new DEFAULT, NOT NULL
	// and CHECK come after the conversion.
	h.exec(`CREATE TABLE it_atc3 (id int, c text NOT NULL DEFAULT '1')`)
	h.exec(`INSERT INTO it_atc3 VALUES (1, '1'), (2, '2')`)
	before = h.rawMeta(raw, defaultSchema, "it_atc3")
	for _, c := range []struct {
		sql  string
		code adbc.Status
		msg  string
	}{
		{`ALTER TABLE it_atc3 ALTER COLUMN c TYPE int USING c::int`, adbc.StatusInvalidArgument,
			`default for column "c" cannot be cast automatically to type integer`},
		{`ALTER TABLE it_atc3 ALTER COLUMN c DROP DEFAULT, ALTER COLUMN c TYPE int USING c::int, ALTER COLUMN c SET DEFAULT 'x'`,
			adbc.StatusInvalidData, `column "c": invalid input syntax for type integer: "x"`},
		{`ALTER TABLE it_atc3 ALTER COLUMN c DROP DEFAULT, ALTER COLUMN c TYPE int USING NULLIF(c, '2')::int, ALTER COLUMN c SET NOT NULL`,
			adbc.StatusIntegrity, `column "c" of relation "it_atc3" contains null values`},
	} {
		h.expectQueryError(c.sql, c.code, c.msg)
		h.expectRolledBack(raw, "it_atc3", before)
	}
	h.exec(`ALTER TABLE it_atc3 ALTER COLUMN c DROP DEFAULT, ALTER COLUMN c DROP NOT NULL, ALTER COLUMN c TYPE int USING NULLIF(c, '2')::int`)
	h.expectRows(`SELECT id, c FROM it_atc3 ORDER BY id`, "1|1", "2|NULL")
	before = h.rawMeta(raw, defaultSchema, "it_atc3")
	h.expectQueryError(`ALTER TABLE it_atc3 ALTER COLUMN c TYPE bigint USING coalesce(c, 0) * 10, ADD CONSTRAINT big CHECK (c > 15)`,
		adbc.StatusIntegrity, `check constraint "big" of relation "it_atc3" is violated by some row`)
	h.expectRolledBack(raw, "it_atc3", before)
	h.exec(`ALTER TABLE it_atc3 ALTER COLUMN c TYPE bigint USING coalesce(c, 0) * 10, ADD CONSTRAINT big CHECK (c >= 0), ADD COLUMN z int DEFAULT 7`)
	h.expectRows(`SELECT * FROM it_atc3 ORDER BY id`, "1|10|7", "2|0|7")
	h.expectChecks(raw, "it_atc3", "big: c >= 0")
	h.exec(`ALTER TABLE it_atc3 ALTER COLUMN c TYPE numeric(4,1) USING c / 3.0`)
	h.expectRows(`SELECT c::text FROM it_atc3 ORDER BY id`, "3.3", "0.0")
	h.expectQueryError(`INSERT INTO it_atc3 VALUES (3, -1, 0)`, adbc.StatusIntegrity, `new row for relation "it_atc3" violates check constraint "big"`)
}

// Several actions with ALTER COLUMN … TYPE, in Postgres's order: USING reads
// the row as it was, also a column the statement drops; the new rows have
// only the table's columns; and missing values are written into them.
func TestSQLAlterTypeMulti(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_atm", "it_atmv")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_atm (id int, c text, n int, d text, gone int)`)
	h.exec(`INSERT INTO it_atm VALUES (1, '10', 1, 'a', 9), (2, '20', 2, 'b', 9)`)
	// A column dropped earlier, whose field may still be in the rows.
	h.exec(`ALTER TABLE it_atm DROP COLUMN gone`)

	h.exec(`ALTER TABLE it_atm ALTER COLUMN id TYPE text USING n::text, ALTER COLUMN n TYPE int USING id::int * 10`)
	h.expectRows(`SELECT id, n FROM it_atm ORDER BY id`, "1|10", "2|20")
	h.exec(`ALTER TABLE it_atm DROP COLUMN d, ALTER COLUMN c TYPE text USING d || c`)
	h.expectColumns(`SELECT * FROM it_atm ORDER BY id`, "id|c|n", "1|a10|10", "2|b20|20")
	if f := h.rowFields(raw, defaultSchema, "it_atm"); len(f) != 4 || f["id"] != 2 || f["c"] != 2 || f["n"] != 2 {
		t.Errorf("row fields: %v (the dropped columns' fields should be gone)", f)
	}
	if meta := h.inspectTable(raw, defaultSchema, "it_atm"); len(meta.PendingCleanup) != 0 {
		t.Errorf("replacement retained pending cleanup: %v", meta.PendingCleanup)
	}

	before := h.rawMeta(raw, defaultSchema, "it_atm")
	for _, c := range []struct{ sql, msg string }{
		{`ALTER TABLE it_atm ADD COLUMN z int, ALTER COLUMN z TYPE text`, `column "z" of relation "it_atm" does not exist`},
		{`ALTER TABLE it_atm DROP COLUMN n, ALTER COLUMN n TYPE text`, `column "n" of relation "it_atm" does not exist`},
		{`ALTER TABLE it_atm ALTER COLUMN n TYPE int USING z, ADD COLUMN z int`, `column "z" does not exist in table "it_atm"`},
		{`ALTER TABLE it_atm ALTER COLUMN n TYPE bigint USING length(c), ALTER COLUMN n SET DATA TYPE text`, `cannot alter type of column "n" twice`},
	} {
		h.expectErrorText(c.sql, c.msg)
		h.expectRolledBack(raw, "it_atm", before)
	}

	h.exec(`ALTER TABLE it_atm ALTER n TYPE text USING n || 'x', ALTER COLUMN n SET DEFAULT 5, ADD COLUMN m int DEFAULT 7`)
	h.exec(`INSERT INTO it_atm (id) VALUES ('3')`)
	h.expectRows(`SELECT * FROM it_atm ORDER BY id`, "1|a10|10x|7", "2|b20|20x|7", "3|NULL|5|7")
	h.expectRows(`SELECT id FROM it_atm WHERE m = 7 ORDER BY id`, "1", "2", "3")
	if c := h.tableColumn(raw, "it_atm", "m"); c.Missing != "" || c.MissingThrough != 0 {
		t.Errorf("m: missing value %q through %d, want it written into the rows", c.Missing, c.MissingThrough)
	}

	// Missing values: the altered column's are converted and written; the
	// others' stay missing values, and a row set to NULL stays NULL.
	h.exec(`CREATE TABLE it_atmv (id int)`)
	h.exec(`INSERT INTO it_atmv VALUES (1), (2)`)
	h.exec(`ALTER TABLE it_atmv ADD COLUMN m int DEFAULT 7, ADD COLUMN k text DEFAULT 'kk'`)
	h.exec(`UPDATE it_atmv SET m = NULL, k = NULL WHERE id = 2`)
	h.exec(`INSERT INTO it_atmv (id) VALUES (3)`)
	h.exec(`ALTER TABLE it_atmv ALTER COLUMN m TYPE bigint USING m * 2`)
	h.expectRows(`SELECT * FROM it_atmv ORDER BY id`, "1|14|kk", "2|NULL|NULL", "3|14|kk")
	h.exec(`ALTER TABLE it_atmv ALTER COLUMN id TYPE bigint`)
	h.expectRows(`SELECT * FROM it_atmv ORDER BY id`, "1|14|kk", "2|NULL|NULL", "3|14|kk")
	h.expectRows(`SELECT id FROM it_atmv WHERE k = 'kk' ORDER BY id`, "1", "3")
	h.exec(`INSERT INTO it_atmv (id) VALUES (4)`)
	h.expectRows(`SELECT * FROM it_atmv WHERE id = 4`, "4|7|kk")

	// A primary key column keeps its key (and so its NOT NULL), as in
	// Postgres.
	h.dropTables("it_atpk")
	h.exec(`CREATE TABLE it_atpk (id int PRIMARY KEY, v text)`)
	h.exec(`INSERT INTO it_atpk VALUES (1, 'a'), (2, 'b')`)
	h.expectQueryError(`ALTER TABLE it_atpk ALTER COLUMN id TYPE bigint USING NULLIF(id, 2)`, adbc.StatusIntegrity,
		`column "id" of relation "it_atpk" contains null values`)
	h.exec(`ALTER TABLE it_atpk ALTER COLUMN id TYPE bigint USING id * 10`)
	h.expectRows(`SELECT * FROM it_atpk ORDER BY id`, "10|a", "20|b")
	if got := strings.Join(h.tableConstraints("public", "it_atpk"), "\n"); got != "table\nit_atpk_pkey PRIMARY KEY (id)" {
		t.Errorf("constraints: %q", got)
	}
	h.expectNullable("it_atpk", "id=NO", "v=YES")
}

// A column that a view reads can't change its type, as in Postgres; the
// detail names the view that reads it itself.
func TestSQLAlterTypeViews(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`CREATE SCHEMA IF NOT EXISTS it_atv_s`)
	h.dropViews("it_atv_v2", "it_atv_v", "it_atv_w", "it_atv_s.vs", "it_atv_ov")
	h.dropTables("it_atv", "it_atv_o")
	h.exec(`CREATE TABLE it_atv (a int, b int, c int)`)
	h.exec(`INSERT INTO it_atv VALUES (1, 2, 3)`)
	h.exec(`CREATE VIEW it_atv_v AS SELECT a FROM it_atv`)
	h.exec(`CREATE VIEW it_atv_v2 AS SELECT * FROM it_atv_v`)
	const msg = `cannot alter type of a column used by a view or rule; rule _RETURN on view %s depends on column %q`
	h.expectQueryError(`ALTER TABLE it_atv ALTER COLUMN a TYPE bigint`, adbc.StatusNotImplemented, fmt.Sprintf(msg, "it_atv_v", "a"))
	h.expectQueryError(`ALTER TABLE it_atv ALTER COLUMN a TYPE int`, adbc.StatusNotImplemented, fmt.Sprintf(msg, "it_atv_v", "a"))
	h.exec(`ALTER TABLE it_atv ALTER COLUMN b TYPE bigint`)
	h.exec(`CREATE VIEW it_atv_w AS SELECT 1 AS one FROM it_atv WHERE c > 0`)
	h.expectQueryError(`ALTER TABLE it_atv ALTER COLUMN c TYPE bigint USING c + 1`, adbc.StatusNotImplemented, fmt.Sprintf(msg, "it_atv_w", "c"))
	h.exec(`CREATE VIEW it_atv_s.vs AS SELECT * FROM public.it_atv`)
	h.expectQueryError(`ALTER TABLE it_atv ALTER COLUMN b TYPE int`, adbc.StatusNotImplemented, fmt.Sprintf(msg, "it_atv_s.vs", "b"))
	h.exec(`DROP VIEW it_atv_s.vs`)
	// A view that reads another table's column of that name doesn't count.
	h.exec(`CREATE TABLE it_atv_o (b int)`)
	h.exec(`CREATE VIEW it_atv_ov AS SELECT o.b FROM it_atv_o o JOIN it_atv t ON t.a = o.b`)
	h.exec(`ALTER TABLE it_atv ALTER COLUMN b TYPE int`)
	h.expectQueryError(`ALTER TABLE it_atv ALTER COLUMN a TYPE bigint`, adbc.StatusNotImplemented, fmt.Sprintf(msg, "it_atv_ov", "a"))
	h.exec(`CREATE TEMP VIEW it_atv_t AS SELECT b FROM it_atv`)
	h.expectQueryError(`ALTER TABLE it_atv ALTER COLUMN b TYPE bigint`, adbc.StatusNotImplemented, fmt.Sprintf(msg, "it_atv_t", "b"))
	h.exec(`DROP VIEW it_atv_t`)
	h.exec(`ALTER TABLE it_atv ALTER COLUMN b TYPE bigint`)
	h.expectRows(`SELECT * FROM it_atv_v2`, "1")
	h.expectRows(`SELECT * FROM it_atv`, "1|2|3")
}

// Private rewrites do not block a concurrent writer or invalidate an old
// reader. Changed source/schema dependencies conflict at publication instead;
// stale writes cannot enter the replacement generation and no SQL is replayed.
func TestSQLAlterTypeConcurrent(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_atx")
	h.exec("CREATE TABLE it_atx(id INTEGER,c TEXT)")
	h.exec("INSERT INTO it_atx VALUES(1,'1'),(2,'2')")
	raw := h.rawClient()
	st, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	old, err := st.getTable(oldCtx, defaultSchema, "it_atx")
	if err != nil {
		t.Fatal(err)
	}
	conflict := func(ctx context.Context, pin *snapshotPin, work string) {
		t.Helper()
		err := st.publishSnapshot(ctx, pin, work)
		var ae adbc.Error
		if !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" {
			t.Fatalf("stale rewrite/write did not conflict: %v", err)
		}
		st.abortSnapshot(pin, work)
		pin.release()
	}
	staleCtx, stalePin, staleWork := snapshotStageSQL(t, st, h.ctx, "it_atx", "INSERT INTO it_atx VALUES(4,'4')")
	ctx, pin, work := snapshotStageSQL(t, st, h.ctx, "", "ALTER TABLE it_atx ALTER COLUMN c TYPE INTEGER USING c::INTEGER")
	h.exec("INSERT INTO it_atx VALUES(3,'3')")
	conflict(ctx, pin, work)
	conflict(staleCtx, stalePin, staleWork)
	h.expectTypes("it_atx", "id INTEGER", "c VARCHAR")
	h.expectRows("SELECT id,c FROM it_atx ORDER BY id", "1|1", "2|2", "3|3")
	h.exec("ALTER TABLE it_atx ALTER COLUMN c TYPE INTEGER USING c::INTEGER")

	staleCtx, stalePin, staleWork = snapshotStageSQL(t, st, h.ctx, "it_atx", "INSERT INTO it_atx VALUES(5,5)")
	h.exec("ALTER TABLE it_atx ALTER COLUMN c TYPE BIGINT USING c+10")
	conflict(staleCtx, stalePin, staleWork)
	h.expectRows("SELECT id,c FROM it_atx ORDER BY id", "1|11", "2|12", "3|13")
	// Reads retain the original values and type across both successful rewrites.
	rows, err := st.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1", old.prefix() + "2"}, nil)
	if err != nil || len(rows) != 2 || rows[0]["c"] != "1" || rows[1]["c"] != "2" || old.Columns[1].Type.Kind != KindString {
		t.Fatalf("rewrites invalidated pinned source: %v %v", rows, err)
	}

	ctx, pin, work = snapshotStageSQL(t, st, h.ctx, "", "ALTER TABLE it_atx ALTER COLUMN c TYPE TEXT")
	h.exec("COMMENT ON TABLE it_atx IS 'meanwhile'")
	conflict(ctx, pin, work)
	meta := h.inspectTable(raw, defaultSchema, "it_atx")
	if meta.Comment != "meanwhile" || meta.RekeyTo != "" || meta.Rewriting || meta.Columns[1].Type != typeInt64 {
		t.Fatalf("failed rewrite lost intervening metadata: %+v", meta)
	}
	h.exec("INSERT INTO it_atx VALUES(6,16)")

	ctx, pin, work = snapshotStageSQL(t, st, h.ctx, "", "ALTER TABLE it_atx ALTER COLUMN c TYPE TEXT")
	private, err := st.getTable(ctx, defaultSchema, "it_atx")
	if err != nil {
		t.Fatal(err)
	}
	h.exec("DROP TABLE it_atx")
	conflict(ctx, pin, work)
	h.expectError("SELECT * FROM it_atx", "does not exist")
	h.expectSnapshotReleased(raw, private.prefix(), private.index())
	rows, err = st.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1", old.prefix() + "2"}, nil)
	if err != nil || len(rows) != 2 || rows[0]["c"] != "1" || rows[1]["c"] != "2" {
		t.Fatal("DROP invalidated pinned pre-rewrite source", err)
	}
	oldPin.release()
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
	assertSnapshotDataReferences(t, h, st)
}

// Both caller-context cancellation and AdbcStatementCancel stop a real rewrite
// after one acknowledged private batch, preserving published rows and metadata.
func TestSQLAlterTypeCancel(t *testing.T) {
	for _, mode := range []string{"context", "statement"} {
		t.Run(mode, func(t *testing.T) {
			h := newSQLHarness(t)
			h.dropTables("it_atk")
			h.exec("CREATE TABLE it_atk(id INTEGER,c TEXT)")
			h.exec("INSERT INTO it_atk SELECT i,i::TEXT FROM generate_series(1,600) AS g(i)")
			raw := h.rawClient()
			prefix, _ := h.tableNames(raw, defaultSchema, "it_atk")
			before := h.rawMeta(raw, defaultSchema, "it_atk")
			stmt, ctx, cancel, transport, barrier := snapshotPausedAlter(t, h, "ALTER TABLE it_atk ALTER COLUMN c TYPE INTEGER USING c::INTEGER", prefix)
			done := make(chan error, 1)
			go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
			scope := snapshotAwaitAlterBatch(t, h, raw, barrier, done)
			start := time.Now()
			if mode == "context" {
				cancel()
			} else if err := stmt.(*statementImpl).Cancel(h.ctx); err != nil {
				t.Fatal(err)
			}
			err := snapshotAwaitAlterError(t, done)
			expectCancelled(t, time.Since(start), err)
			snapshotExpectAlterLeaseReleased(t, h, raw, scope)
			h.expectRolledBack(raw, "it_atk", before)
			h.expectRows("SELECT COUNT(*),SUM(c::BIGINT),MIN(id),MAX(id) FROM it_atk", "600|180300|1|600")
			h.exec("INSERT INTO it_atk VALUES(601,'601')")
			h.expectRows("SELECT COUNT(*),SUM(c::BIGINT) FROM it_atk", "601|180901")
		})
	}
}

// Close must stop active execution while its cleanup client is still usable.
// The next writer must not wait for a private workspace or pin to expire.
func TestSQLAlterTypeClose(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_atq")
	h.exec("CREATE TABLE it_atq(id INTEGER,c TEXT)")
	h.exec("INSERT INTO it_atq SELECT i,i::TEXT FROM generate_series(1,600) AS g(i)")
	raw := h.rawClient()
	prefix, _ := h.tableNames(raw, defaultSchema, "it_atq")
	before := h.rawMeta(raw, defaultSchema, "it_atq")
	closing := newSQLHarness(t)
	stmt, ctx, _, transport, barrier := snapshotPausedAlter(t, closing, "ALTER TABLE it_atq ALTER COLUMN c TYPE INTEGER USING c::INTEGER", prefix)
	done := make(chan error, 1)
	go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
	scope := snapshotAwaitAlterBatch(t, h, raw, barrier, done)
	closed := make(chan error, 1)
	go func() { closed <- closing.conn.Close(context.Background()) }()
	err := snapshotAwaitAlterError(t, done)
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusInvalidState {
		t.Errorf("ALTER after Close: %v, want INVALID_STATE", err)
	}
	if err := snapshotAwaitAlterError(t, closed); err != nil {
		t.Errorf("Close: %v", err)
	}
	snapshotExpectAlterLeaseReleased(t, h, raw, scope)
	h.expectRolledBack(raw, "it_atq", before)
	h.expectRows("SELECT COUNT(*),SUM(c::BIGINT) FROM it_atq", "600|180300")
	h.exec("INSERT INTO it_atq VALUES(601,'601')")
	h.expectRows("SELECT COUNT(*),SUM(c::BIGINT) FROM it_atq", "601|180901")
}

// Lease expiry fences an acknowledged partial rewrite. Another collector may
// retire its private rows before the old execution resumes; it cannot recreate
// them or publish stale output, and fresh SQL remains usable.
func TestSQLAlterTypeLostLease(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_atl")
	h.exec("CREATE TABLE it_atl(id INTEGER,c TEXT)")
	h.exec("INSERT INTO it_atl SELECT i,i::TEXT FROM generate_series(1,600) AS g(i)")
	raw := h.rawClient()
	prefix, _ := h.tableNames(raw, defaultSchema, "it_atl")
	before := h.rawMeta(raw, defaultSchema, "it_atl")
	stmt, ctx, _, transport, barrier := snapshotPausedAlter(t, h, "ALTER TABLE it_atl ALTER COLUMN c TYPE INTEGER USING c::INTEGER", prefix)
	done := make(chan error, 1)
	go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
	scope := snapshotAwaitAlterBatch(t, h, raw, barrier, done)
	scope.mu.Lock()
	var dataID string
	for data := range scope.owned {
		if dataID != "" {
			t.Error("unexpected multiple rewrite outputs")
		}
		dataID = data
	}
	scope.mu.Unlock()
	if dataID == "" {
		t.Fatal("rewrite has no owned private output")
	}
	target := snapshotRows(dataID, defaultSchema, snapshotAccessID(prefix))
	if count, err := raw.ZCard(h.ctx, snapshotMembers(dataID)).Result(); err != nil || count != snapshotDeltaPage {
		t.Fatal("partial private output missing", count, err)
	}
	payload, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
	var work map[string]any
	if err != nil || json.Unmarshal(payload, &work) != nil {
		t.Fatal("private work missing", err)
	}
	work["expires"] = 0
	payload, err = json.Marshal(work)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+scope.id, payload).Err(); err != nil {
		t.Fatal(err)
	}
	collector := &store{client: raw, snapshots: true}
	snapshotReapTest(collector, h.ctx)
	if raw.Exists(h.ctx, target+"1", target+"256", snapshotMembers(dataID)).Val() != 0 || raw.HExists(h.ctx, snapshotMetadataGraph, "d:"+dataID).Val() {
		t.Fatal("expired partial rewrite was not reclaimed")
	}
	close(barrier.resume)
	err = snapshotAwaitAlterError(t, done)
	if !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "reclaimed") {
		t.Fatalf("expired execution was not server-fenced: %v", err)
	}
	if barrier.pages.Load() != 2 || raw.Exists(h.ctx, target+"1", target+"257").Val() != 0 {
		t.Fatal("expired writer replayed or resurrected private rows")
	}
	snapshotExpectAlterLeaseReleased(t, h, raw, scope)
	h.expectRolledBack(raw, "it_atl", before)
	h.expectRows("SELECT COUNT(*),SUM(c::BIGINT) FROM it_atl", "600|180300")
	h.exec("INSERT INTO it_atl VALUES(601,'601')")
	h.exec("ALTER TABLE it_atl ALTER COLUMN c TYPE INTEGER USING c::INTEGER")
	h.expectRows("SELECT COUNT(*),SUM(c) FROM it_atl", "601|180901")
}

// Several immutable directory pages are converted exactly once. Managed rows
// share the catalog slot; a multi-primary fixture does not distribute these rows.
func TestSQLAlterTypeBig(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_atb")
	raw := h.rawClient()
	const n = 20000
	h.exec(fmt.Sprintf("CREATE TABLE it_atb AS SELECT g AS id,CAST(g AS TEXT) AS c,g %% 7 AS k FROM generate_series(1,%d) AS s(g)", n))
	h.exec("ALTER TABLE it_atb DROP COLUMN k")
	oldPrefix, oldIndex := h.tableNames(raw, defaultSchema, "it_atb")
	pages, rows := snapshotCountedAlter(t, h, "ALTER TABLE it_atb ALTER COLUMN c TYPE BIGINT USING c::BIGINT*2,ALTER COLUMN id TYPE TEXT", oldPrefix)
	if pages != int64((n+snapshotDeltaPage-1)/snapshotDeltaPage) || rows != n {
		t.Fatalf("rewrite source paging duplicated/skipped rows: pages=%d rows=%d", pages, rows)
	}
	h.expectTypes("it_atb", "id VARCHAR", "c BIGINT")
	h.expectRows("SELECT COUNT(*),SUM(c),MIN(c),MAX(c) FROM it_atb", fmt.Sprintf("%d|%d|2|%d", n, n*(n+1), 2*n))
	h.expectRows("SELECT c FROM it_atb WHERE id='12345'", "24690")
	h.expectRows("SELECT COUNT(*) FROM it_atb WHERE c>39990", "5")
	h.expectRows("SELECT id FROM it_atb ORDER BY c DESC LIMIT 2", "20000", "19999")
	if f := h.rowFields(raw, defaultSchema, "it_atb"); len(f) != 3 || f["c"] != n || f["id"] != n || f[rowIDField] != n {
		t.Errorf("row fields: %v", f)
	}
	h.expectSnapshotReleased(raw, oldPrefix, oldIndex)
	if meta := h.inspectTable(raw, defaultSchema, "it_atb"); len(meta.PendingCleanup) != 0 {
		t.Errorf("replacement retained pending cleanup: %v", meta.PendingCleanup)
	}
}

// Snapshot cleanup never HDELs a published field between pages. Exercise the
// corresponding overlap through an intervening SQL update and reference GC:
// the stale copy conflicts, and the original pinned directory stays exact even
// after a fresh DROP COLUMN/type rewrite publishes and retires its old roots.
func TestSQLAlterTypeCleanupDuringCursor(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_at_cleanup")
	raw := h.rawClient()
	const n = 20000
	h.exec(fmt.Sprintf("CREATE TABLE it_at_cleanup AS SELECT g AS id,CAST(g AS TEXT) AS c,g %% 7 AS k FROM generate_series(1,%d) AS s(g)", n))
	source, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	old, err := source.getTable(oldCtx, defaultSchema, "it_at_cleanup")
	if err != nil {
		t.Fatal(err)
	}
	stmt, ctx, _, transport, barrier := snapshotPausedAlter(t, h, "ALTER TABLE it_at_cleanup DROP COLUMN k,ALTER COLUMN c TYPE BIGINT USING c::BIGINT*2", old.prefix(), time.Minute)
	done := make(chan error, 1)
	go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
	snapshotAwaitAlterBatch(t, h, raw, barrier, done)
	h.exec("UPDATE it_at_cleanup SET k=-1 WHERE id<=100")
	snapshotReapTest(source, h.ctx)
	if value := raw.HGet(h.ctx, old.prefix()+"1", "k").Val(); value != "1" {
		t.Fatal("concurrent cleanup mutated pinned source", value)
	}
	close(barrier.resume)
	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("stale paged rewrite did not finish")
	}
	var ae adbc.Error
	if !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" {
		t.Fatalf("stale paged rewrite did not conflict: %v", err)
	}
	h.expectRows("SELECT COUNT(*) FROM it_at_cleanup WHERE k=-1", "100")
	h.exec("ALTER TABLE it_at_cleanup DROP COLUMN k,ALTER COLUMN c TYPE BIGINT USING c::BIGINT*2")
	h.expectRows("SELECT COUNT(*),SUM(c),MIN(c),MAX(c) FROM it_at_cleanup", fmt.Sprintf("%d|%d|2|%d", n, n*(n+1), 2*n))
	if fields := h.rowFields(raw, defaultSchema, "it_at_cleanup"); len(fields) != 3 || fields["id"] != n || fields["c"] != n || fields["k"] != 0 {
		t.Fatal("replacement kept dropped fields or lost rows", fields)
	}
	snapshotReapTest(source, h.ctx)
	scan := newSnapshotDeltaScan(source, old, nil)
	seen := 0
	for {
		_, rows, err := scan.next(oldCtx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			seen++
			if row["id"] != fmt.Sprint(seen) || row["c"] != fmt.Sprint(seen) || row["k"] != fmt.Sprint(seen%7) {
				t.Fatalf("pinned directory skipped/changed source row %d: %v", seen, row)
			}
		}
	}
	if seen != n {
		t.Fatalf("pinned source returned %d rows, want %d", seen, n)
	}
	oldPin.release()
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
	assertSnapshotDataReferences(t, h, source)
}

func (h *sqlHarness) expectAttrs(raw goredis.UniversalClient, table, want string) {
	h.t.Helper()
	if got := h.attrTypes(raw, table); got != want {
		h.t.Errorf("%s: index attributes %q, want %q", table, got, want)
	}
}

// A column's type changes, with its index attribute (TAG and NUMERIC), and
// the indexed queries on it read the new values; information_schema,
// GetObjects and GetTableSchema show the new type. A conversion that fails
// leaves the table as it was.
func TestSQLAlterType(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_at")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_at (id integer, c text, n integer DEFAULT 5, v varchar(5), f double precision NOINDEX)`)
	h.exec(`INSERT INTO it_at VALUES (1, '10', 1, 'a', 1.5), (2, '9', 2, 'bb', 2.5), (3, 'abc', 3, NULL, NULL)`)
	h.expectTypes("it_at", "id INTEGER", "c VARCHAR", "n INTEGER", "v VARCHAR(5)", "f DOUBLE PRECISION")
	h.expectAttrs(raw, "it_at", "__rowid NUMERIC, "+snapshotDeletedField+" NUMERIC, id NUMERIC, c TAG, n NUMERIC, v TAG")

	// Postgres's errors, and the table stays as it was.
	before := h.rawMeta(raw, defaultSchema, "it_at")
	oldPrefix, oldIndex := h.tableNames(raw, defaultSchema, "it_at")
	for _, c := range []struct {
		sql  string
		code adbc.Status
		msg  string
	}{
		{`ALTER TABLE it_at ALTER COLUMN c TYPE integer`, adbc.StatusInvalidArgument,
			`column "c" cannot be cast automatically to type integer; you might need to specify "USING c::integer"`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE integer USING c`, adbc.StatusInvalidArgument,
			`result of USING clause for column "c" cannot be cast automatically to type integer; you might need to add an explicit cast`},
		{`ALTER TABLE it_at ALTER COLUMN n TYPE boolean USING n <> 0`, adbc.StatusInvalidArgument,
			`default for column "n" cannot be cast automatically to type boolean`},
		{`ALTER TABLE it_at ALTER COLUMN nope TYPE integer`, adbc.StatusNotFound, `column "nope" of relation "it_at" does not exist`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE integer USING nope`, adbc.StatusNotFound, `column "nope" does not exist in table "it_at"`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE integer USING (SELECT 1)`, adbc.StatusNotImplemented, `cannot use subquery in transform expression`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE integer USING max(id)`, adbc.StatusInvalidArgument, `aggregate functions are not allowed in transform expressions`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE text COLLATE "x"`, adbc.StatusNotFound,
			`collation "x" for encoding UTF8 does not exist`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE nosuchtype`, adbc.StatusInvalidArgument, `unsupported SQL type NOSUCHTYPE`},
		{`ALTER TABLE it_at ALTER COLUMN c TYPE bigint USING c::bigint, ALTER COLUMN c TYPE integer USING c::integer`, adbc.StatusNotImplemented,
			`cannot alter type of column "c" twice`},
		// Row 3's 'abc' doesn't convert: nothing changes.
		{`ALTER TABLE it_at ALTER COLUMN c TYPE integer USING c::integer`, adbc.StatusInvalidData, `invalid input syntax for type integer: "abc"`},
		{`ALTER TABLE it_at ALTER COLUMN v TYPE varchar(1)`, adbc.StatusInvalidData, `value too long for type character varying(1)`},
	} {
		h.expectQueryError(c.sql, c.code, c.msg)
		h.expectRolledBack(raw, "it_at", before)
	}
	if p, i := h.tableNames(raw, defaultSchema, "it_at"); p != oldPrefix || i != oldIndex {
		t.Errorf("names %s %s, want %s %s", p, i, oldPrefix, oldIndex)
	}
	h.expectRows(`SELECT * FROM it_at ORDER BY id`, "1|10|1|a|1.5", "2|9|2|bb|2.5", "3|abc|3|NULL|NULL")

	// Text to integer, with USING: the rows move to new keys and a new
	// index, where c is NUMERIC. Queries on it are pushed down and order
	// numbers as numbers.
	h.exec(`UPDATE it_at SET c = '11' WHERE id = 3`)
	h.exec(`ALTER TABLE it_at ALTER COLUMN c TYPE integer USING c::integer`)
	h.expectTypes("it_at", "id INTEGER", "c INTEGER", "n INTEGER", "v VARCHAR(5)", "f DOUBLE PRECISION")
	h.expectAttrs(raw, "it_at", "__rowid NUMERIC, "+snapshotDeletedField+" NUMERIC, id NUMERIC, c NUMERIC, n NUMERIC, v TAG")
	newPrefix, newIndex := h.tableNames(raw, defaultSchema, "it_at")
	if newPrefix == oldPrefix || newIndex == oldIndex {
		t.Errorf("names %s %s: the rows weren't rewritten", newPrefix, newIndex)
	}
	h.expectSnapshotReleased(raw, oldPrefix, oldIndex)
	assertSnapshotDataReferences(t, h, &store{client: raw, snapshots: true})
	h.expectRows(`SELECT id, c FROM it_at ORDER BY c`, "2|9", "1|10", "3|11")
	h.expectRows(`SELECT id FROM it_at WHERE c BETWEEN 9 AND 10 ORDER BY id`, "1", "2")
	h.expectRows(`SELECT sum(c), max(c) FROM it_at`, "30|11")
	if q, residual := h.pushedWhere(raw, `SELECT id FROM it_at WHERE c = 10`); !strings.Contains(q, "@c:[10 10]") || residual {
		t.Errorf("WHERE c = 10: index query %q, residual %v", q, residual)
	}
	h.expectQueryError(`INSERT INTO it_at (id, c) VALUES (4, 'x')`, adbc.StatusInvalidData, `column "c": invalid input syntax for type integer: "x"`)
	h.exec(`INSERT INTO it_at (id, c) VALUES (4, 12)`)
	h.expectRows(`SELECT id, n FROM it_at WHERE c = 12`, "4|5")

	// And back to text, without USING: c is a TAG again.
	h.exec(`ALTER TABLE it_at ALTER COLUMN c TYPE text`)
	h.expectAttrs(raw, "it_at", "__rowid NUMERIC, "+snapshotDeletedField+" NUMERIC, id NUMERIC, c TAG, n NUMERIC, v TAG")
	h.expectRows(`SELECT id FROM it_at WHERE c = '10'`, "1")
	h.expectRows(`SELECT c FROM it_at ORDER BY c`, "10", "11", "12", "9")
	if q, _ := h.pushedWhere(raw, `SELECT id FROM it_at WHERE c = '10'`); !strings.Contains(q, "@c:{10}") {
		t.Errorf("WHERE c = '10': index query %q", q)
	}

	// A longer VARCHAR, TEXT and the same type only change the metadata,
	// as in Postgres: the rows keep their keys.
	prefix, index := h.tableNames(raw, defaultSchema, "it_at")
	h.exec(`ALTER TABLE it_at ALTER COLUMN v TYPE varchar(10)`)
	h.exec(`ALTER TABLE it_at ALTER COLUMN n SET DATA TYPE integer`)
	h.expectTypes("it_at", "id INTEGER", "c VARCHAR", "n INTEGER", "v VARCHAR(10)", "f DOUBLE PRECISION")
	h.exec(`ALTER TABLE it_at ALTER COLUMN v TYPE text`)
	h.expectTypes("it_at", "id INTEGER", "c VARCHAR", "n INTEGER", "v VARCHAR", "f DOUBLE PRECISION")
	if p, i := h.tableNames(raw, defaultSchema, "it_at"); p != prefix || i != index {
		t.Errorf("names %s %s, want %s %s (no rewrite)", p, i, prefix, index)
	}
	h.exec(`INSERT INTO it_at (id, v) VALUES (5, 'longer than five')`)

	// The DEFAULT converts: INTEGER's 5 to BIGINT, then to text. A
	// NOINDEX column stays out of the index.
	h.exec(`ALTER TABLE it_at ALTER COLUMN n SET DATA TYPE bigint, ALTER COLUMN f TYPE numeric(4,1)`)
	h.expectTypes("it_at", "id INTEGER", "c VARCHAR", "n BIGINT", "v VARCHAR", "f NUMERIC(4,1)")
	h.expectAttrs(raw, "it_at", "__rowid NUMERIC, "+snapshotDeletedField+" NUMERIC, id NUMERIC, c TAG, n NUMERIC, v TAG")
	h.exec(`INSERT INTO it_at (id) VALUES (6)`)
	h.exec(`ALTER TABLE it_at ALTER COLUMN n TYPE text`)
	h.exec(`INSERT INTO it_at (id) VALUES (7)`)
	h.expectRows(`SELECT id, c, n, v, f FROM it_at ORDER BY id`, "1|10|1|a|1.5", "2|9|2|bb|2.5", "3|11|3|NULL|NULL", "4|12|5|NULL|NULL",
		"5|NULL|5|longer than five|NULL", "6|NULL|5|NULL|NULL", "7|NULL|5|NULL|NULL")
	h.expectRows(`SELECT column_name, column_default FROM information_schema.columns WHERE table_name = 'it_at' AND column_name = 'n'`, "n|5")
	h.expectRows(`SELECT id FROM it_at WHERE n = '5' ORDER BY id`, "4", "5", "6", "7")
}
