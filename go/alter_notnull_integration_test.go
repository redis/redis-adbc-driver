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

// Integration tests for ALTER TABLE … ALTER COLUMN … SET / DROP NOT NULL,
// and ADD COLUMN … NOT NULL without a default (#114).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// nullability lists whether a table's columns are nullable, as "column=YES"
// or "column=NO", from information_schema.columns. GetObjects (both of its
// fields) and GetTableSchema must say the same.
func (h *sqlHarness) nullability(table string) []string {
	h.t.Helper()
	got, _ := h.query(fmt.Sprintf(`SELECT column_name || '=' || is_nullable FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = '%s' ORDER BY ordinal_position`, table))
	var objects, fields []string
	schema := defaultSchema
	rdr, err := h.conn.GetObjects(h.ctx, adbc.ObjectDepthColumns, nil, &schema, &table, nil, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rdr.Release()
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
							Name       string `json:"column_name"`
							Nullable   int16  `json:"xdbc_nullable"`
							IsNullable string `json:"xdbc_is_nullable"`
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
							objects = append(objects, fmt.Sprintf("%s=%s", c.Name, c.IsNullable))
							if (c.Nullable == 1) != (c.IsNullable == "YES") {
								h.t.Errorf("GetObjects %s.%s: xdbc_nullable %d, xdbc_is_nullable %s", table, c.Name, c.Nullable, c.IsNullable)
							}
						}
					}
				}
			}
		}
	}
	s, err := h.conn.GetTableSchema(h.ctx, nil, nil, table)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, f := range s.Fields() {
		yes := "NO"
		if f.Nullable {
			yes = "YES"
		}
		fields = append(fields, f.Name+"="+yes)
	}
	if strings.Join(objects, " ") != strings.Join(got, " ") || strings.Join(fields, " ") != strings.Join(got, " ") {
		h.t.Errorf("%s: information_schema %q, GetObjects %q, GetTableSchema %q", table, got, objects, fields)
	}
	return got
}

func (h *sqlHarness) expectNullable(table string, want ...string) {
	h.t.Helper()
	if got := h.nullability(table); strings.Join(got, " ") != strings.Join(want, " ") {
		h.t.Errorf("%s: nullable %q, want %q", table, got, want)
	}
}

// SET NOT NULL checks the rows and then marks the column; DROP NOT NULL
// unmarks it. information_schema, GetObjects and GetTableSchema show it,
// and writes are checked against it.
func TestSQLAlterNotNull(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_nn", "it_nn_empty", "it_nn_order")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_nn (id integer NOT NULL, v integer, s varchar)`)
	h.exec(`INSERT INTO it_nn VALUES (1, 10, 'a'), (2, NULL, 'b')`)
	h.expectNullable("it_nn", "id=NO", "v=YES", "s=YES")

	// A column that some row has NULL in: Postgres's error, and the table
	// stays as it was.
	meta := h.rawMeta(raw, defaultSchema, "it_nn")
	h.expectQueryError(`ALTER TABLE it_nn ALTER COLUMN v SET NOT NULL`, adbc.StatusIntegrity,
		`column "v" of relation "it_nn" contains null values`)
	h.expectQueryError(`ALTER TABLE it_nn ALTER s SET NOT NULL, ALTER v SET NOT NULL`, adbc.StatusIntegrity,
		`column "v" of relation "it_nn" contains null values`)
	if got := h.rawMeta(raw, defaultSchema, "it_nn"); got != meta {
		t.Errorf("metadata changed\n got: %s\nwant: %s", got, meta)
	}
	h.exec(`INSERT INTO it_nn VALUES (3, NULL, NULL)`)

	// Once no row has NULL, it works, and writes are checked.
	h.exec(`UPDATE it_nn SET v = 0 WHERE v IS NULL`)
	h.exec(`ALTER TABLE it_nn ALTER COLUMN v SET NOT NULL`)
	h.expectNullable("it_nn", "id=NO", "v=NO", "s=YES")
	h.expectQueryError(`INSERT INTO it_nn (id) VALUES (4)`, adbc.StatusIntegrity, `null value in column "v" of relation "it_nn" violates not-null constraint`)
	h.expectQueryError(`UPDATE it_nn SET v = NULL WHERE id = 1`, adbc.StatusIntegrity, `null value in column "v" of relation "it_nn" violates not-null constraint`)
	// SET NOT NULL again, and DROP NOT NULL of a nullable column, change
	// nothing, as in Postgres.
	h.exec(`ALTER TABLE it_nn ALTER COLUMN v SET NOT NULL`)
	h.exec(`ALTER TABLE it_nn ALTER COLUMN s DROP NOT NULL`)
	h.expectNullable("it_nn", "id=NO", "v=NO", "s=YES")

	// DROP NOT NULL, also of a column created NOT NULL.
	h.exec(`ALTER TABLE it_nn ALTER COLUMN v DROP NOT NULL, ALTER id DROP NOT NULL`)
	h.expectNullable("it_nn", "id=YES", "v=YES", "s=YES")
	h.exec(`INSERT INTO it_nn (s) VALUES ('c')`)
	h.expectRows(`SELECT id, v FROM it_nn WHERE s = 'c'`, "NULL|NULL")
	h.exec(`UPDATE it_nn SET v = NULL WHERE id = 1`)

	// DROP NOT NULL comes before SET NOT NULL, as written either way.
	h.expectQueryError(`ALTER TABLE it_nn ALTER COLUMN s SET NOT NULL, ALTER COLUMN s DROP NOT NULL`, adbc.StatusIntegrity,
		`column "s" of relation "it_nn" contains null values`)
	h.exec(`DELETE FROM it_nn WHERE s IS NULL`)
	h.exec(`ALTER TABLE it_nn ALTER COLUMN s SET NOT NULL, ALTER COLUMN s DROP NOT NULL`)
	h.expectNullable("it_nn", "id=YES", "v=YES", "s=NO")
	h.expectQueryError(`ALTER TABLE it_nn ALTER COLUMN nope SET NOT NULL`, adbc.StatusNotFound,
		`column "nope" of relation "it_nn" does not exist`)
	h.expectQueryError(`ALTER TABLE it_nn ALTER COLUMN nope DROP NOT NULL`, adbc.StatusNotFound,
		`column "nope" of relation "it_nn" does not exist`)
	h.expectRows(`SELECT id, v, s FROM it_nn ORDER BY id`, "1|NULL|a", "2|0|b", "NULL|NULL|c")

	// A table without rows takes any NOT NULL, also ADD COLUMN … NOT NULL
	// without a default.
	h.exec(`CREATE TABLE it_nn_empty (id integer, v integer)`)
	h.exec(`ALTER TABLE it_nn_empty ALTER COLUMN v SET NOT NULL, ADD COLUMN w integer NOT NULL,
		ADD COLUMN x integer, ALTER COLUMN x SET NOT NULL`)
	h.expectNullable("it_nn_empty", "id=YES", "v=NO", "w=NO", "x=NO")
	h.expectQueryError(`INSERT INTO it_nn_empty (id, v, x) VALUES (1, 1, 1)`, adbc.StatusIntegrity,
		`null value in column "w" of relation "it_nn_empty" violates not-null constraint`)
	h.exec(`INSERT INTO it_nn_empty VALUES (1, 1, 1, 1)`)
	// With a row, the new column needs a non-NULL default, which the row
	// reads.
	h.expectQueryError(`ALTER TABLE it_nn_empty ADD COLUMN y integer NOT NULL`, adbc.StatusIntegrity,
		`column "y" of relation "it_nn_empty" contains null values`)
	h.expectQueryError(`ALTER TABLE it_nn_empty ADD COLUMN y integer, ALTER COLUMN y SET NOT NULL`, adbc.StatusIntegrity,
		`column "y" of relation "it_nn_empty" contains null values`)
	h.exec(`ALTER TABLE it_nn_empty ADD COLUMN y integer NOT NULL DEFAULT 5, ADD COLUMN z integer DEFAULT 6, ALTER COLUMN z SET NOT NULL`)
	h.expectRows(`SELECT id, y, z FROM it_nn_empty`, "1|5|6")
	h.expectNullable("it_nn_empty", "id=YES", "v=NO", "w=NO", "x=NO", "y=NO", "z=NO")

	// Each row is checked against NOT NULL before the CHECKs, as in
	// Postgres.
	h.exec(`CREATE TABLE it_nn_order (id integer, a integer)`)
	h.exec(`INSERT INTO it_nn_order VALUES (1, NULL)`)
	h.expectQueryError(`ALTER TABLE it_nn_order ADD CONSTRAINT big CHECK (id > 5), ALTER COLUMN a SET NOT NULL`, adbc.StatusIntegrity,
		`column "a" of relation "it_nn_order" contains null values`)
	h.expectQueryError(`ALTER TABLE it_nn_order ADD COLUMN b integer CHECK (b > 0) NOT NULL`, adbc.StatusIntegrity,
		`column "b" of relation "it_nn_order" contains null values`)
	h.expectQueryError(`ALTER TABLE it_nn_order ADD CONSTRAINT big CHECK (id > 5), ALTER COLUMN id SET NOT NULL`, adbc.StatusIntegrity,
		`check constraint "big" of relation "it_nn_order" is violated by some row`)
	h.expectNullable("it_nn_order", "id=YES", "a=YES")
	h.expectChecks(raw, "it_nn_order")
}

// The rows read a column added with a DEFAULT as queries do: the rows that
// existed then read its missing value, unless set to NULL since, and rows
// inserted later without a value read NULL.
func TestSQLAlterNotNullMissing(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_nn_miss")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_nn_miss (id integer)`)
	h.exec(`INSERT INTO it_nn_miss VALUES (1), (2)`)
	h.exec(`ALTER TABLE it_nn_miss ADD COLUMN d integer DEFAULT 5`)
	h.exec(`ALTER TABLE it_nn_miss ALTER COLUMN d SET NOT NULL`)
	h.expectNullable("it_nn_miss", "id=YES", "d=NO")
	if f := h.rowFields(raw, defaultSchema, "it_nn_miss"); f["d"] != 0 {
		t.Errorf("row fields %v: the rows were written", f)
	}

	h.exec(`ALTER TABLE it_nn_miss ALTER COLUMN d DROP NOT NULL`)
	h.exec(`UPDATE it_nn_miss SET d = NULL WHERE id = 2`)
	h.expectQueryError(`ALTER TABLE it_nn_miss ALTER COLUMN d SET NOT NULL`, adbc.StatusIntegrity,
		`column "d" of relation "it_nn_miss" contains null values`)
	h.exec(`UPDATE it_nn_miss SET d = 6 WHERE id = 2`)
	h.exec(`ALTER TABLE it_nn_miss ALTER COLUMN d SET NOT NULL`)

	h.exec(`ALTER TABLE it_nn_miss ALTER COLUMN d DROP NOT NULL, ALTER COLUMN d DROP DEFAULT`)
	h.exec(`INSERT INTO it_nn_miss (id) VALUES (3)`)
	h.expectRows(`SELECT id, d FROM it_nn_miss ORDER BY id`, "1|5", "2|6", "3|NULL")
	h.expectQueryError(`ALTER TABLE it_nn_miss ALTER COLUMN d SET NOT NULL`, adbc.StatusIntegrity,
		`column "d" of relation "it_nn_miss" contains null values`)
	h.exec(`DELETE FROM it_nn_miss WHERE id = 3`)
	h.exec(`ALTER TABLE it_nn_miss ALTER COLUMN d SET NOT NULL`)
	h.expectNullable("it_nn_miss", "id=YES", "d=NO")
	h.expectRows(`SELECT id, d FROM it_nn_miss ORDER BY id`, "1|5", "2|6")
}

// Without locks, other connections write while SET NOT NULL or ADD
// CONSTRAINT … CHECK runs. A row inserted after the rows were checked and
// before the change commits is checked when it commits, and an insert by a
// statement that read the table before the change, once it has committed,
// is checked against the new NOT NULL.
func TestSQLAlterNotNullConcurrent(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_nn_race", "it_nn_race2")
	raw := h.rawClient()
	other := newSQLHarness(t)
	h.exec(`CREATE TABLE it_nn_race (id integer, v integer)`)
	h.exec(`INSERT INTO it_nn_race VALUES (1, 1)`)
	t.Cleanup(func() { testHookAltered = nil })
	insertBeforeCommit := func(sql string) {
		testHookAltered = func(*tableMeta) {
			testHookAltered = nil
			other.exec(sql)
		}
	}

	insertBeforeCommit(`INSERT INTO it_nn_race VALUES (2, NULL)`)
	h.expectQueryError(`ALTER TABLE it_nn_race ALTER COLUMN v SET NOT NULL`, adbc.StatusIntegrity,
		`column "v" of relation "it_nn_race" contains null values`)
	h.expectNullable("it_nn_race", "id=YES", "v=YES")
	h.exec(`DELETE FROM it_nn_race WHERE v IS NULL`)
	insertBeforeCommit(`INSERT INTO it_nn_race VALUES (3, 3)`)
	h.exec(`ALTER TABLE it_nn_race ALTER COLUMN v SET NOT NULL`)
	h.expectNullable("it_nn_race", "id=YES", "v=NO")

	insertBeforeCommit(`INSERT INTO it_nn_race VALUES (4, -4)`)
	h.expectQueryError(`ALTER TABLE it_nn_race ADD CONSTRAINT v_pos CHECK (v > 0)`, adbc.StatusIntegrity,
		`check constraint "v_pos" of relation "it_nn_race" is violated by some row`)
	h.expectChecks(raw, "it_nn_race")
	h.exec(`DELETE FROM it_nn_race WHERE id = 4`)

	// SET NOT NULL on a NOT NULL column checks no rows; if another
	// connection drops the NOT NULL before it commits, it fails.
	insertBeforeCommit(`ALTER TABLE it_nn_race ALTER COLUMN v DROP NOT NULL`)
	h.expectQueryError(`ALTER TABLE it_nn_race ALTER COLUMN v SET NOT NULL`, adbc.StatusIO,
		`table "it_nn_race" changed concurrently; try again`)
	h.expectNullable("it_nn_race", "id=YES", "v=YES")

	// Rows written with metadata read before SET NOT NULL committed.
	h.exec(`ALTER TABLE it_nn_race ALTER COLUMN v DROP NOT NULL`)
	st := &store{client: raw}
	old, err := st.getTable(h.ctx, defaultSchema, "it_nn_race")
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_nn_race ALTER COLUMN v SET NOT NULL`)
	row := func(meta *tableMeta, vals ...int64) []Value {
		out := make([]Value, len(meta.Columns))
		for i, c := range meta.Columns {
			out[i] = nullValue(c.Type)
			if i < len(vals) {
				if out[i], err = Coerce(intValue(typeInt64, vals[i]), c.Type); err != nil {
					t.Fatal(err)
				}
			}
		}
		return out
	}
	expectErr := func(err error, code adbc.Status, msg string) {
		t.Helper()
		var ae adbc.Error
		if !errors.As(err, &ae) || ae.Code != code || ae.Msg != "[redis] "+msg {
			t.Errorf("got %v, want %q (%v)", err, msg, code)
		}
	}
	_, err = st.insertRows(h.ctx, old, [][]Value{row(old, 5)})
	expectErr(err, adbc.StatusIntegrity, `null value in column "v" of relation "it_nn_race" violates not-null constraint`)
	if _, err := st.insertRows(h.ctx, old, [][]Value{row(old, 6, 6)}); err != nil {
		t.Fatal(err)
	}
	// A NOT NULL column added since gives such rows its missing value, or
	// without one (the table had no rows), they are refused.
	h.exec(`ALTER TABLE it_nn_race ADD COLUMN w integer NOT NULL DEFAULT 7`)
	if _, err := st.insertRows(h.ctx, old, [][]Value{row(old, 8, 8)}); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT id, v, w FROM it_nn_race ORDER BY id`, "1|1|7", "3|3|7", "6|6|7", "8|8|7")
	h.exec(`CREATE TABLE it_nn_race2 (id integer)`)
	old2, err := st.getTable(h.ctx, defaultSchema, "it_nn_race2")
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_nn_race2 ADD COLUMN w integer NOT NULL`)
	_, err = st.insertRows(h.ctx, old2, [][]Value{row(old2, 1)})
	expectErr(err, adbc.StatusIO, `table "it_nn_race2" changed concurrently; try again`)
	h.expectRows(`SELECT COUNT(*) FROM it_nn_race2`, "0")
}
