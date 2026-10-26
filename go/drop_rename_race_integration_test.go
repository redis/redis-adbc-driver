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

// DROP TABLE and ALTER TABLE … RENAME TO of the same table, overlapping. In
// Postgres one of them waits for the other's lock: the DROP drops the table,
// and the rename then fails because the table is gone, or the rename comes
// first and the DROP fails because the name is gone. Here a plain rename
// (which keeps the table's keys and index) that commits while a DROP drops
// the table's index can't be undone, since the rows are gone: the DROP
// drops the table under its new name.

import (
	"fmt"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// expectNoTable checks that a table name is free: no metadata, no row id
// counter, and a table created with it starts again at row id 1.
func (h *sqlHarness) expectNoTable(raw goredis.UniversalClient, table string) {
	h.t.Helper()
	h.expectError(fmt.Sprintf(`SELECT * FROM %s`, table), "does not exist")
	if ok, err := raw.SIsMember(h.ctx, tablesKey("public"), table).Result(); err != nil || ok {
		h.t.Errorf("table %q still listed (%v)", table, err)
	}
	if n, err := raw.Exists(h.ctx, seqKey("public", table)).Result(); err != nil || n != 0 {
		h.t.Errorf("table %q: row id counter left (%v)", table, err)
	}
}

// A plain RENAME TO that commits while DROP TABLE runs, before or after the
// DROP has dropped the table's index: the DROP drops the table under its
// new name, rows, index, names and all. A table created under the old name
// meanwhile is another table, and stays.
func TestSQLRenameDuringDrop(t *testing.T) {
	for _, c := range []struct {
		name string
		hook *func()
	}{
		{"before the index is dropped", &testHookDropRead},
		{"after the index is dropped", &testHookDropping},
	} {
		for _, v := range []struct{ name, drop, after string }{
			{"plain", `DROP TABLE it_dr_t`, ``},
			{"if exists", `DROP TABLE IF EXISTS it_dr_t`, ``},
			{"new table of the old name", `DROP TABLE it_dr_t`, `CREATE TABLE it_dr_t (x INTEGER)`},
		} {
			t.Run(c.name+"/"+v.name, func(t *testing.T) {
				h := newSQLHarness(t)
				h.dropTables("it_dr_t", "it_dr_u")
				raw := h.rawClient()
				other := newSQLHarness(t)
				h.exec(`CREATE TABLE it_dr_t (id BIGINT, note VARCHAR)`)
				h.exec(`INSERT INTO it_dr_t VALUES (1, 'a'), (2, 'b')`)
				prefix, index := h.tableNames(raw, "public", "it_dr_t")
				*c.hook = func() {
					*c.hook = nil
					other.exec(`ALTER TABLE it_dr_t RENAME TO it_dr_u`)
					if v.after != "" {
						other.exec(v.after)
						other.exec(`INSERT INTO it_dr_t VALUES (7)`)
					}
				}
				t.Cleanup(func() { *c.hook = nil })
				h.exec(v.drop)
				*c.hook = nil
				h.expectReleased(raw, prefix, index)
				h.expectNoTable(raw, "it_dr_u")
				listed := `SELECT table_name FROM information_schema.tables WHERE table_name IN ('it_dr_t', 'it_dr_u')`
				if v.after != "" {
					h.expectRows(`SELECT x, __rowid FROM it_dr_t`, "7|1")
					h.expectRows(listed, "it_dr_t")
				} else {
					h.expectNoTable(raw, "it_dr_t")
					h.expectRows(listed)
				}
				// Both names are free for new tables.
				h.exec(`CREATE TABLE it_dr_u (id BIGINT)`)
				h.exec(`INSERT INTO it_dr_u VALUES (3)`)
				h.expectRows(`SELECT id, __rowid FROM it_dr_u`, "3|1")
			})
		}
	}
}

// A DROP TABLE that commits while a plain RENAME TO runs: the rename fails
// because the table is gone, as in Postgres.
func TestSQLDropDuringRename(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_dr_t", "it_dr_u")
	raw := h.rawClient()
	other := newSQLHarness(t)
	h.exec(`CREATE TABLE it_dr_t (id BIGINT)`)
	h.exec(`INSERT INTO it_dr_t VALUES (1), (2)`)
	prefix, index := h.tableNames(raw, "public", "it_dr_t")
	testHookRenaming = func() {
		testHookRenaming = nil
		other.exec(`DROP TABLE it_dr_t`)
	}
	t.Cleanup(func() { testHookRenaming = nil })
	h.expectError(`ALTER TABLE it_dr_t RENAME TO it_dr_u`, `table "public"."it_dr_t" does not exist`)
	testHookRenaming = nil
	h.expectReleased(raw, prefix, index)
	h.expectNoTable(raw, "it_dr_t")
	h.expectNoTable(raw, "it_dr_u")
}

// DROP TABLE racing a re-keying rename (adbc.redis.rename_rekey), which
// moves the rows to new keys. One that starts after the DROP has dropped
// the index can't copy the rows: it fails ("No such index") and is rolled
// back, and the DROP drops the table. One that runs to the end before the
// DROP drops the index has moved the rows and released the old keys: the
// DROP fails, and the renamed table keeps its rows. Neither is followed.
func TestSQLRekeyDuringDrop(t *testing.T) {
	t.Run("after the index is dropped", func(t *testing.T) {
		h := newSQLHarness(t)
		h.dropTables("it_dr_rk", "it_dr_rk2")
		raw := h.rawClient()
		rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
		h.exec(`CREATE TABLE it_dr_rk (id BIGINT)`)
		h.exec(`INSERT INTO it_dr_rk VALUES (1), (2)`)
		prefix, index := h.tableNames(raw, "public", "it_dr_rk")
		var renameErr error
		testHookDropping = func() {
			testHookDropping = nil
			_, renameErr = tryExec(rk.ctx, rk.conn, `ALTER TABLE it_dr_rk RENAME TO it_dr_rk2`)
		}
		t.Cleanup(func() { testHookDropping = nil })
		h.exec(`DROP TABLE it_dr_rk`)
		testHookDropping = nil
		if renameErr == nil || !isUnknownIndex(renameErr) {
			t.Errorf("rename: got %v, want the old index's absence", renameErr)
		}
		h.expectReleased(raw, prefix, index)
		h.expectNoTable(raw, "it_dr_rk")
		h.expectNoTable(raw, "it_dr_rk2")
		if h.rekeyPending(raw, prefix) {
			t.Error("the rename is still recorded")
		}
		// The names the rename reserved are given back.
		n, err := raw.HGet(h.ctx, namesNextKey, rowPrefix("public", "it_dr_rk2")).Int64()
		if err != nil {
			t.Fatal(err)
		}
		reserved := namesFor("public", "it_dr_rk2", n)
		h.expectReleased(raw, reserved.prefix, reserved.index)
	})

	t.Run("before the index is dropped", func(t *testing.T) {
		h := newSQLHarness(t)
		h.dropTables("it_dr_rk", "it_dr_rk2")
		raw := h.rawClient()
		rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
		h.exec(`CREATE TABLE it_dr_rk (id BIGINT)`)
		h.exec(`INSERT INTO it_dr_rk VALUES (1), (2)`)
		prefix, index := h.tableNames(raw, "public", "it_dr_rk")
		testHookDropRead = func() {
			testHookDropRead = nil
			rk.exec(`ALTER TABLE it_dr_rk RENAME TO it_dr_rk2`)
		}
		t.Cleanup(func() { testHookDropRead = nil })
		h.expectError(`DROP TABLE it_dr_rk`, `table "public"."it_dr_rk" does not exist`)
		testHookDropRead = nil
		h.expectReleased(raw, prefix, index)
		h.expectRows(`SELECT id FROM it_dr_rk2 ORDER BY id`, "1", "2")
		if p, _ := h.tableNames(raw, "public", "it_dr_rk2"); p == prefix {
			t.Errorf("the re-keyed table kept the prefix %q", p)
		}
		h.expectNoTable(raw, "it_dr_rk")
	})

	// Between the switch and the end, the old prefix is still reserved, but
	// no table has it: the DROP isn't fooled into following the rename, and
	// its DD only does what the end does.
	t.Run("switched, not ended", func(t *testing.T) {
		h := newSQLHarness(t)
		h.dropTables("it_dr_rk", "it_dr_rk2")
		raw := h.rawClient()
		st := &store{client: raw}
		h.exec(`CREATE TABLE it_dr_rk (id BIGINT)`)
		h.exec(`INSERT INTO it_dr_rk VALUES (1), (2)`)
		prefix, index := h.tableNames(raw, "public", "it_dr_rk")
		var job *rekeyJob
		testHookDropRead = func() {
			testHookDropRead = nil
			j, start, err := st.beginRekey(h.ctx, "public", "it_dr_rk", "it_dr_rk2")
			if err != nil {
				t.Fatal(err)
			}
			job = j
			if err := st.moveRows(h.ctx, job, start, time.Now()); err != nil {
				t.Fatal(err)
			}
			if _, err := st.switchRekey(h.ctx, job, start); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() { testHookDropRead = nil })
		h.expectError(`DROP TABLE it_dr_rk`, `table "public"."it_dr_rk" does not exist`)
		testHookDropRead = nil
		if job == nil {
			t.Fatal("the rename did not run")
		}
		defer job.lease.release()
		if err := st.finishRekey(h.ctx, job); err != nil {
			t.Fatal(err)
		}
		h.expectReleased(raw, prefix, index)
		h.expectRows(`SELECT id FROM it_dr_rk2 ORDER BY id`, "1", "2")
		h.expectNoTable(raw, "it_dr_rk")
	})
}

// A temporary table, renamed by its own connection (the only one that can
// name it) while a DROP of that connection runs.
func TestSQLRenameDuringDropTemp(t *testing.T) {
	h := newSQLHarness(t)
	raw := h.rawClient()
	before := h.tempOwners(raw)
	h.exec(`CREATE TEMP TABLE it_dr_tmp (id BIGINT)`)
	h.exec(`INSERT INTO it_dr_tmp VALUES (1), (2)`)
	schema := tempSchemaBase + h.newTempID(raw, before)
	prefix, index := h.tableNames(raw, schema, "it_dr_tmp")
	testHookDropping = func() {
		testHookDropping = nil
		if _, err := tryExec(h.ctx, h.conn, `ALTER TABLE pg_temp.it_dr_tmp RENAME TO it_dr_tmp2`); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { testHookDropping = nil })
	h.exec(`DROP TABLE pg_temp.it_dr_tmp`)
	testHookDropping = nil
	h.expectReleased(raw, prefix, index)
	for _, name := range []string{"it_dr_tmp", "it_dr_tmp2"} {
		h.expectError(`SELECT * FROM pg_temp.`+name, "does not exist")
		if n, err := raw.Exists(h.ctx, metaKey(schema, name)).Result(); err != nil || n != 0 {
			t.Errorf("%s: metadata left (%v)", name, err)
		}
	}
	h.expectRows(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'pg_temp'`, "0")
}
