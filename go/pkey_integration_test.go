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

// Integration tests for PRIMARY KEY (pkey.go): the key is kept, its columns
// are NOT NULL, and GetObjects and information_schema list it, as in
// Postgres 16. Its uniqueness isn't enforced.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// expectKey checks a table's primary key, "name (columns)" or "" without
// one, as GetObjects' table_constraints, information_schema's
// table_constraints and its key_column_usage list it.
func (h *sqlHarness) expectKey(schema, table, want string) {
	h.t.Helper()
	where := fmt.Sprintf(`table_schema = '%s' AND table_name = '%s'`, schema, table)
	tc, _ := h.query(`SELECT constraint_catalog, constraint_schema, constraint_name, table_catalog,
		is_deferrable, initially_deferred, enforced, nulls_distinct
		FROM information_schema.table_constraints WHERE ` + where + ` AND constraint_type = 'PRIMARY KEY'`)
	kcu, _ := h.query(`SELECT constraint_name, column_name, ordinal_position, position_in_unique_constraint
		FROM information_schema.key_column_usage WHERE ` + where + ` ORDER BY ordinal_position`)
	info := ""
	if len(tc) > 0 {
		f := strings.Split(tc[0], "|")
		name := f[2]
		if len(tc) > 1 || strings.Join(append(f[:2:2], f[3:]...), "|") != "redis|"+schema+"|redis|NO|NO|NO|NULL" {
			h.t.Errorf("%s: information_schema.table_constraints %q", table, tc)
		}
		var cols []string
		for i, r := range kcu {
			k := strings.Split(r, "|")
			if k[0] != name || k[2] != strconv.Itoa(i+1) || k[3] != "NULL" {
				h.t.Errorf("%s: information_schema.key_column_usage %q", table, kcu)
			}
			cols = append(cols, k[1])
		}
		info = name + " (" + strings.Join(cols, ", ") + ")"
	} else if len(kcu) > 0 {
		h.t.Errorf("%s: information_schema.key_column_usage %q without a key", table, kcu)
	}
	objects := ""
	for _, c := range h.tableConstraints(schema, table) {
		if name, cols, ok := strings.Cut(c, " PRIMARY KEY "); ok {
			if objects != "" {
				h.t.Errorf("%s: GetObjects lists two keys", table)
			}
			objects = name + " " + cols
		}
	}
	if info != want || objects != want {
		h.t.Errorf("%s: key %q in information_schema, %q in GetObjects, want %q", table, info, objects, want)
	}
}

// The forms of PRIMARY KEY in CREATE TABLE, their names, and the errors
// Postgres gives. The key's columns are NOT NULL on every write; rows with
// the same key are written.
func TestSQLPrimaryKeyCreate(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_pk", "it_pk_multi", "it_pk_named", "it_pk_col", "it_pk_chk", "it_pk_chk2", "it_pk_opts", "it_pk_bad")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_pk (id integer primary key, v varchar)`)
	h.expectNullable("it_pk", "id=NO", "v=YES")
	h.expectKey("public", "it_pk", "it_pk_pkey (id)")
	h.expectRows(`SELECT constraint_name, constraint_type FROM information_schema.table_constraints WHERE table_name = 'it_pk'`,
		"it_pk_pkey|PRIMARY KEY")

	notNull := `null value in column "id" of relation "it_pk" violates not-null constraint`
	for _, sql := range []string{
		`INSERT INTO it_pk VALUES (NULL, 'a')`,
		`INSERT INTO it_pk (v) VALUES ('a')`,
		`INSERT INTO it_pk SELECT NULL, 'a'`,
		`INSERT INTO it_pk DEFAULT VALUES`,
		`MERGE INTO it_pk t USING (SELECT CAST(NULL AS integer) AS id) s ON t.id = s.id WHEN NOT MATCHED THEN INSERT (id) VALUES (s.id)`,
	} {
		h.expectQueryError(sql, adbc.StatusIntegrity, notNull)
	}
	h.exec(`INSERT INTO it_pk VALUES (1, 'a')`)
	h.expectQueryError(`UPDATE it_pk SET id = NULL`, adbc.StatusIntegrity, notNull)
	h.expectQueryError(`MERGE INTO it_pk t USING (SELECT 1 AS id) s ON t.id = s.id WHEN MATCHED THEN UPDATE SET id = NULL`,
		adbc.StatusIntegrity, notNull)
	// Bulk ingest.
	ib, vb := array.NewInt32Builder(memory.DefaultAllocator), array.NewStringBuilder(memory.DefaultAllocator)
	defer ib.Release()
	defer vb.Release()
	ib.AppendValues([]int32{5, 0}, []bool{true, false})
	vb.AppendValues([]string{"x", "y"}, nil)
	ids, vs := ib.NewArray(), vb.NewArray()
	defer ids.Release()
	defer vs.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "v", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil), []arrow.Array{ids, vs}, 2)
	defer rec.Release()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, "it_pk"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestMode, adbc.OptionValueIngestModeAppend); err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), notNull) {
		t.Errorf("ingest of a NULL key: %v", err)
	}
	// Not enforced: rows with the same key are written, by INSERT and by
	// UPDATE.
	h.exec(`INSERT INTO it_pk VALUES (1, 'b'), (2, 'c')`)
	h.exec(`UPDATE it_pk SET id = 1 WHERE id = 2`)
	h.expectRows(`SELECT id, v FROM it_pk ORDER BY v`, "1|a", "1|b", "1|c")
	if got := h.rawMeta(raw, defaultSchema, "it_pk"); !strings.Contains(got, `"primary_key":{"name":"it_pk_pkey","fields":["id"]}`) {
		t.Errorf("metadata %s", got)
	}

	// A table key, in the order written; named ones, also on a column
	// declared NULL (which it makes NOT NULL, as Postgres 16 does).
	h.exec(`CREATE TABLE it_pk_multi (a integer, b varchar, c integer, primary key (b, a))`)
	h.expectNullable("it_pk_multi", "a=NO", "b=NO", "c=YES")
	h.expectKey("public", "it_pk_multi", "it_pk_multi_pkey (b, a)")
	h.exec(`CREATE TABLE it_pk_named (a integer, constraint "My PK" primary key (A))`)
	h.expectKey("public", "it_pk_named", "My PK (a)")
	h.exec(`CREATE TABLE it_pk_col (a integer null constraint a_key primary key, b integer)`)
	h.expectNullable("it_pk_col", "a=NO", "b=YES")
	h.expectKey("public", "it_pk_col", "a_key (a)")

	// The CHECKs are named first, and the key avoids their names.
	h.exec(`CREATE TABLE it_pk_chk (a integer constraint it_pk_chk_pkey check (a > 0) primary key)`)
	h.expectKey("public", "it_pk_chk", "it_pk_chk_pkey1 (a)")
	h.expectChecks(raw, "it_pk_chk", "it_pk_chk_pkey: a > 0")
	h.exec(`CREATE TABLE it_pk_chk2 (a integer, b integer, primary key (b), check (b > 0), check (a > 0 and b > 0))`)
	h.expectKey("public", "it_pk_chk2", "it_pk_chk2_pkey (b)")
	h.expectChecks(raw, "it_pk_chk2", "it_pk_chk2_b_check: b > 0", "it_pk_chk2_check: a > 0 and b > 0")
	if got := strings.Join(h.tableConstraints("public", "it_pk_chk2"), "\n"); got != strings.Join([]string{"table",
		"it_pk_chk2_pkey PRIMARY KEY (b)", "it_pk_chk2_b_check CHECK (b)", "it_pk_chk2_check CHECK (a, b)"}, "\n") {
		t.Errorf("GetObjects constraints %q", got)
	}
	h.expectRows(`SELECT constraint_name, constraint_type, enforced FROM information_schema.table_constraints
		WHERE table_name = 'it_pk_chk2' ORDER BY constraint_name`,
		"it_pk_chk2_b_check|CHECK|YES", "it_pk_chk2_check|CHECK|YES", "it_pk_chk2_pkey|PRIMARY KEY|NO")

	// Index parameters and DEFERRABLE are accepted, and mean nothing.
	h.exec(`CREATE TABLE it_pk_opts (a integer, b integer,
		primary key (a, b) include (b) with (fillfactor = 70) using index tablespace pg_default deferrable initially deferred)`)
	h.expectKey("public", "it_pk_opts", "it_pk_opts_pkey (a, b)")

	multiple := `multiple primary keys for table "it_pk_bad" are not allowed`
	for _, c := range []struct {
		sql  string
		code adbc.Status
		msg  string
	}{
		{`CREATE TABLE it_pk_bad (a integer primary key, b integer primary key)`, adbc.StatusInvalidArgument, multiple},
		{`CREATE TABLE it_pk_bad (a integer primary key, primary key (a))`, adbc.StatusInvalidArgument, multiple},
		{`CREATE TABLE it_pk_bad (a integer, b integer, primary key (a), primary key (b))`, adbc.StatusInvalidArgument, multiple},
		{`CREATE TABLE it_pk_bad (a integer primary key primary key)`, adbc.StatusInvalidArgument, multiple},
		{`CREATE TABLE it_pk_bad (a integer, primary key (a, a))`, adbc.StatusAlreadyExists, `column "a" appears twice in primary key constraint`},
		{`CREATE TABLE it_pk_bad (a integer, primary key (nope))`, adbc.StatusNotFound, `column "nope" named in key does not exist`},
		{`CREATE TABLE it_pk_bad (a integer, primary key (a, nope, a))`, adbc.StatusNotFound, `column "nope" named in key does not exist`},
		{`CREATE TABLE it_pk_bad (a integer, primary key (nope), primary key (a))`, adbc.StatusNotFound, `column "nope" named in key does not exist`},
		{`CREATE TABLE it_pk_bad (a integer check (a > 0), constraint it_pk_bad_a_check primary key (a))`, adbc.StatusAlreadyExists,
			`constraint "it_pk_bad_a_check" for relation "it_pk_bad" already exists`},
		{`CREATE TABLE it_pk_bad (a integer constraint c check (a > 0), constraint c primary key (a))`, adbc.StatusAlreadyExists,
			`constraint "c" for relation "it_pk_bad" already exists`},
		{`CREATE TABLE it_pk_bad (a integer, primary key ())`, adbc.StatusInvalidArgument, `syntax error at or near ")"`},
		{`CREATE TABLE it_pk_bad (a integer, primary key a)`, adbc.StatusInvalidArgument, `syntax error: expected "(" near "a"`},
	} {
		h.expectQueryError(c.sql, c.code, c.msg)
	}
	if ok, err := (&store{client: raw}).tableExists(h.ctx, defaultSchema, "it_pk_bad"); err != nil || ok {
		t.Errorf("it_pk_bad exists (%v)", err)
	}
}

// ADD PRIMARY KEY checks the rows for NULLs, one key per table, DROP NOT
// NULL refuses a key column, and the actions run in Postgres's passes.
func TestSQLPrimaryKeyAlter(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_pk_alt", "it_pk_alt2", "it_pk_alt3")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_pk_alt (id integer, v integer, w integer)`)
	h.exec(`INSERT INTO it_pk_alt VALUES (1, 10, NULL), (NULL, 20, 2)`)
	meta := h.rawMeta(raw, defaultSchema, "it_pk_alt")
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (id)`, adbc.StatusIntegrity,
		`column "id" of relation "it_pk_alt" contains null values`)
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD CONSTRAINT k PRIMARY KEY (v, w)`, adbc.StatusIntegrity,
		`column "w" of relation "it_pk_alt" contains null values`)
	if got := h.rawMeta(raw, defaultSchema, "it_pk_alt"); got != meta {
		t.Errorf("metadata changed\n got: %s\nwant: %s", got, meta)
	}
	h.exec(`UPDATE it_pk_alt SET id = 2 WHERE id IS NULL`)
	h.exec(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (id)`)
	h.expectNullable("it_pk_alt", "id=NO", "v=YES", "w=YES")
	h.expectKey("public", "it_pk_alt", "it_pk_alt_pkey (id)")
	h.expectQueryError(`INSERT INTO it_pk_alt (v) VALUES (30)`, adbc.StatusIntegrity,
		`null value in column "id" of relation "it_pk_alt" violates not-null constraint`)

	// One key per table.
	multiple := `multiple primary keys for table "it_pk_alt" are not allowed`
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (v)`, adbc.StatusInvalidArgument, multiple)
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD COLUMN k integer DEFAULT 0 PRIMARY KEY`, adbc.StatusInvalidArgument, multiple)
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD COLUMN k integer DEFAULT 0 PRIMARY KEY PRIMARY KEY`, adbc.StatusInvalidArgument, multiple)
	// ADD COLUMN IF NOT EXISTS skips a column that exists with its key.
	h.exec(`ALTER TABLE it_pk_alt ADD COLUMN IF NOT EXISTS v integer PRIMARY KEY`)
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (nope)`, adbc.StatusNotFound,
		`column "nope" of relation "it_pk_alt" does not exist`)
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (v, nope, v)`, adbc.StatusAlreadyExists,
		`column "v" appears twice in primary key constraint`)

	// DROP NOT NULL refuses a column of the key, unless the key is dropped
	// first: both are drops, which run in the order written.
	inKey := `column "id" is in a primary key`
	h.expectQueryError(`ALTER TABLE it_pk_alt ALTER COLUMN id DROP NOT NULL`, adbc.StatusInvalidArgument, inKey)
	h.expectQueryError(`ALTER TABLE it_pk_alt ALTER COLUMN id DROP NOT NULL, DROP CONSTRAINT it_pk_alt_pkey`, adbc.StatusInvalidArgument, inKey)
	h.expectKey("public", "it_pk_alt", "it_pk_alt_pkey (id)")
	// A new key in place of the old one (the drop comes first, written
	// either way); the old key's column stays NOT NULL.
	h.exec(`UPDATE it_pk_alt SET w = 1 WHERE w IS NULL`)
	h.exec(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (w, v), DROP CONSTRAINT it_pk_alt_pkey`)
	h.expectKey("public", "it_pk_alt", "it_pk_alt_pkey (w, v)")
	h.expectNullable("it_pk_alt", "id=NO", "v=NO", "w=NO")
	h.exec(`ALTER TABLE it_pk_alt DROP CONSTRAINT IT_PK_ALT_PKEY, ALTER COLUMN w DROP NOT NULL`)
	h.expectKey("public", "it_pk_alt", "")
	h.expectNullable("it_pk_alt", "id=NO", "v=NO", "w=YES")
	h.expectQueryError(`ALTER TABLE it_pk_alt DROP CONSTRAINT it_pk_alt_pkey`, adbc.StatusNotFound,
		`constraint "it_pk_alt_pkey" of relation "it_pk_alt" does not exist`)
	h.exec(`ALTER TABLE it_pk_alt DROP CONSTRAINT IF EXISTS it_pk_alt_pkey`)

	// A key on a column that the statement adds: NOT NULL, so the existing
	// rows need a default, which they read.
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD COLUMN k integer PRIMARY KEY`, adbc.StatusIntegrity,
		`column "k" of relation "it_pk_alt" contains null values`)
	h.expectQueryError(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (k), ADD COLUMN k integer`, adbc.StatusIntegrity,
		`column "k" of relation "it_pk_alt" contains null values`)
	h.expectKey("public", "it_pk_alt", "")
	h.exec(`ALTER TABLE it_pk_alt ADD PRIMARY KEY (k), ADD COLUMN k integer DEFAULT 7`)
	h.expectKey("public", "it_pk_alt", "it_pk_alt_pkey (k)")
	h.expectNullable("it_pk_alt", "id=NO", "v=NO", "w=YES", "k=NO")
	h.expectRows(`SELECT id, k FROM it_pk_alt ORDER BY id`, "1|7", "2|7")
	// On a table without rows, a key column needs no default.
	h.exec(`CREATE TABLE it_pk_alt3 (a integer)`)
	h.exec(`ALTER TABLE it_pk_alt3 ADD COLUMN id integer CHECK (id > 0) PRIMARY KEY`)
	h.expectKey("public", "it_pk_alt3", "it_pk_alt3_pkey (id)")
	h.expectNullable("it_pk_alt3", "a=YES", "id=NO")

	// Names: the key avoids the CHECKs' names, a CHECK added later the key's,
	// and the key is named before the CHECKs of the same statement.
	h.exec(`CREATE TABLE it_pk_alt2 (a integer, b integer, constraint it_pk_alt2_pkey check (a > 0))`)
	h.exec(`ALTER TABLE it_pk_alt2 ADD PRIMARY KEY (b)`)
	h.expectKey("public", "it_pk_alt2", "it_pk_alt2_pkey1 (b)")
	h.expectQueryError(`ALTER TABLE it_pk_alt2 ADD CONSTRAINT it_pk_alt2_pkey1 CHECK (b > 0)`, adbc.StatusAlreadyExists,
		`constraint "it_pk_alt2_pkey1" for relation "it_pk_alt2" already exists`)
	h.expectQueryError(`ALTER TABLE it_pk_alt2 DROP CONSTRAINT it_pk_alt2_pkey1, ADD CONSTRAINT it_pk_alt2_pkey PRIMARY KEY (b)`,
		adbc.StatusAlreadyExists, `constraint "it_pk_alt2_pkey" for relation "it_pk_alt2" already exists`)
	h.exec(`ALTER TABLE it_pk_alt2 ADD CHECK (b > 0), DROP CONSTRAINT it_pk_alt2_pkey1, ADD CONSTRAINT it_pk_alt2_b_check PRIMARY KEY (b)`)
	h.expectKey("public", "it_pk_alt2", "it_pk_alt2_b_check (b)")
	h.expectChecks(raw, "it_pk_alt2", "it_pk_alt2_pkey: a > 0", "it_pk_alt2_b_check1: b > 0")
}

// The key follows its columns through RENAME COLUMN, keeps its name
// through RENAME TO (also re-keying) and TRUNCATE, and goes with DROP
// COLUMN of one of them. CREATE TABLE … AS has none, temporary tables have
// theirs, and metadata without one reads as a table without a key.
func TestSQLPrimaryKeyLifetime(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_pk_life", "it_pk_life2", "it_pk_life3", "it_pk_ctas", "it_pk_old")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_pk_life (a integer, b integer, c integer, primary key (c, a))`)
	h.exec(`INSERT INTO it_pk_life VALUES (1, 2, 3)`)
	h.exec(`ALTER TABLE it_pk_life RENAME COLUMN a TO aa`)
	h.expectKey("public", "it_pk_life", "it_pk_life_pkey (c, aa)")
	h.expectQueryError(`ALTER TABLE it_pk_life ALTER COLUMN aa DROP NOT NULL`, adbc.StatusInvalidArgument, `column "aa" is in a primary key`)
	h.exec(`ALTER TABLE it_pk_life RENAME TO it_pk_life2`)
	h.expectKey("public", "it_pk_life2", "it_pk_life_pkey (c, aa)")
	h.exec(`TRUNCATE it_pk_life2`)
	h.expectKey("public", "it_pk_life2", "it_pk_life_pkey (c, aa)")
	h.exec(`INSERT INTO it_pk_life2 VALUES (1, 2, 3)`)
	rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	rk.exec(`ALTER TABLE it_pk_life2 RENAME TO it_pk_life3`)
	h.expectKey("public", "it_pk_life3", "it_pk_life_pkey (c, aa)")
	h.expectQueryError(`INSERT INTO it_pk_life3 (aa, b) VALUES (1, 2)`, adbc.StatusIntegrity,
		`null value in column "c" of relation "it_pk_life3" violates not-null constraint`)
	h.exec(`ALTER TABLE it_pk_life3 DROP COLUMN aa`)
	h.expectKey("public", "it_pk_life3", "")
	h.expectNullable("it_pk_life3", "b=YES", "c=NO")
	h.expectRows(`SELECT b, c FROM it_pk_life3`, "2|3")

	h.exec(`CREATE TABLE it_pk_ctas AS SELECT * FROM it_pk_life3`)
	h.expectKey("public", "it_pk_ctas", "")
	h.expectNullable("it_pk_ctas", "b=YES", "c=YES")

	h.exec(`CREATE TEMP TABLE it_pk_tmp (id integer primary key, v integer)`)
	t.Cleanup(func() { h.exec(`DROP TABLE IF EXISTS pg_temp.it_pk_tmp`) })
	h.expectKey("pg_temp", "it_pk_tmp", "it_pk_tmp_pkey (id)")
	h.expectQueryError(`INSERT INTO it_pk_tmp (v) VALUES (1)`, adbc.StatusIntegrity,
		`null value in column "id" of relation "it_pk_tmp" violates not-null constraint`)

	// Metadata without the primary_key field, as older versions wrote it:
	// no key, and the column stays as its metadata says.
	h.exec(`CREATE TABLE it_pk_old (id integer primary key)`)
	key := metaKey(defaultSchema, "it_pk_old")
	var m map[string]any
	if err := json.Unmarshal([]byte(h.rawMeta(raw, defaultSchema, "it_pk_old")), &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "primary_key")
	out, _ := json.Marshal(m)
	if err := raw.Set(h.ctx, key, out, 0).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectKey("public", "it_pk_old", "")
	h.exec(`ALTER TABLE it_pk_old ALTER COLUMN id DROP NOT NULL`)
	h.exec(`INSERT INTO it_pk_old VALUES (NULL)`)
	h.expectQueryError(`ALTER TABLE it_pk_old ADD PRIMARY KEY (id)`, adbc.StatusIntegrity,
		`column "id" of relation "it_pk_old" contains null values`)
}

// Without locks, other connections write while ADD PRIMARY KEY checks the
// rows: a NULL key inserted before it commits makes it fail, a key that
// another connection adds first makes it a second key, and an insert with
// metadata read before the key was added is checked against its NOT NULL.
func TestSQLPrimaryKeyConcurrent(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_pk_race", "it_pk_race2")
	raw := h.rawClient()
	other := newSQLHarness(t)
	h.exec(`CREATE TABLE it_pk_race (id integer, v integer)`)
	h.exec(`INSERT INTO it_pk_race VALUES (1, 1)`)
	t.Cleanup(func() { testHookAltered = nil })
	beforeCommit := func(sql string) {
		testHookAltered = func(*tableMeta) {
			testHookAltered = nil
			other.exec(sql)
		}
	}
	beforeCommit(`INSERT INTO it_pk_race VALUES (NULL, 2)`)
	h.expectQueryError(`ALTER TABLE it_pk_race ADD PRIMARY KEY (id)`, adbc.StatusIntegrity,
		`column "id" of relation "it_pk_race" contains null values`)
	h.expectKey("public", "it_pk_race", "")
	h.exec(`DELETE FROM it_pk_race WHERE id IS NULL`)
	beforeCommit(`ALTER TABLE it_pk_race ADD PRIMARY KEY (v)`)
	h.expectQueryError(`ALTER TABLE it_pk_race ADD PRIMARY KEY (id)`, adbc.StatusInvalidArgument,
		`multiple primary keys for table "it_pk_race" are not allowed`)
	h.expectKey("public", "it_pk_race", "it_pk_race_pkey (v)")
	h.expectNullable("it_pk_race", "id=YES", "v=NO")

	h.exec(`CREATE TABLE it_pk_race2 (id integer, v integer)`)
	st := &store{client: raw}
	old, err := st.getTable(h.ctx, defaultSchema, "it_pk_race2")
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_pk_race2 ADD PRIMARY KEY (id)`)
	_, err = st.insertRows(h.ctx, old, [][]Value{{nullValue(typeInt32), intValue(typeInt32, 1)}})
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusIntegrity ||
		ae.Msg != `[redis] null value in column "id" of relation "it_pk_race2" violates not-null constraint` {
		t.Errorf("insert with the metadata read before ADD PRIMARY KEY: %v", err)
	}
	h.expectRows(`SELECT COUNT(*) FROM it_pk_race2`, "0")
}
