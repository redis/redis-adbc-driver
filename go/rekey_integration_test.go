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

// Integration tests for RENAME TO with adbc.redis.rename_rekey (rekey.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// newRekeyHarness is newSQLHarness with extra database options.
func newRekeyHarness(t *testing.T, opts map[string]string) *sqlHarness {
	t.Helper()
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run SQL integration tests")
	}
	ctx := context.Background()
	all := map[string]string{adbc.OptionKeyURI: uri}
	for k, v := range opts {
		all[k] = v
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return &sqlHarness{t: t, ctx: ctx, conn: conn}
}

func (h *sqlHarness) setConnOption(key, value string) error {
	return h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, key, value)
}

// tableNames returns a table's key prefix and index name.
func (h *sqlHarness) tableNames(raw goredis.UniversalClient, schema, table string) (string, string) {
	h.t.Helper()
	meta := h.inspectTable(raw, schema, table)
	return meta.prefix(), meta.index()
}

// Native storage identity is canonical and registered independently of SQL names.
func (h *sqlHarness) rekeyNativeLayout(raw goredis.UniversalClient, schema, table string) (string, string) {
	h.t.Helper()
	meta := h.inspectTable(raw, schema, table)
	for _, seg := range meta.snapshotSegments() {
		id := snapshotDataID(seg.Prefix)
		if id == "" || seg.Prefix != snapshotRows(id, schema) || snapshotDataID(seg.Index) != id {
			h.t.Fatal("noncanonical rekey storage")
		}
	}
	return meta.prefix(), meta.index()
}

// expectReleased checks that a prefix and index name are free in the
// registry, and that neither keys nor an index use them.
func (h *sqlHarness) expectReleased(raw goredis.UniversalClient, prefix, index string) {
	h.t.Helper()
	if ok, err := raw.SIsMember(h.ctx, prefixesKey, prefix).Result(); err != nil || ok {
		h.t.Errorf("prefix %q still reserved (%v)", prefix, err)
	}
	if ok, err := raw.SIsMember(h.ctx, indexesKey, index).Result(); err != nil || ok {
		h.t.Errorf("index name %q still reserved (%v)", index, err)
	}
	if n := h.prefixKeyCount(raw, prefix); n != 0 {
		h.t.Errorf("%d keys left under %q", n, prefix)
	}
	if h.indexExists(raw, index) {
		h.t.Errorf("index %q still exists", index)
	}
}

// rekeyPending reports whether a re-key of the old prefix is recorded.
func (h *sqlHarness) rekeyPending(raw goredis.UniversalClient, oldPrefix string) bool {
	h.t.Helper()
	ok, err := raw.HExists(h.ctx, rekeyKey, oldPrefix).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	return ok
}

// dbt's table materialization: build <name>__dbt_tmp, drop <name>, rename.
func TestSQLRekeyDbtPattern(t *testing.T) {
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables("it_rk_model", "it_rk_model__dbt_tmp")
	h.exec("DROP VIEW IF EXISTS it_rk_view")
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS it_rk_view") })
	raw := h.rawClient()

	h.exec(`CREATE TABLE it_rk_model (id INTEGER, label VARCHAR)`)
	h.exec(`INSERT INTO it_rk_model VALUES (100, 'old')`)
	// Physical UUIDs are never reused across builds or renames.
	seen := map[string]bool{}
	initial, _ := h.rekeyNativeLayout(raw, defaultSchema, "it_rk_model")
	seen[initial] = true
	for run := 1; run <= 2; run++ {
		h.exec(`CREATE TABLE it_rk_model__dbt_tmp (id INTEGER NOT NULL, label VARCHAR, amount NUMERIC(10,2), blob VARBINARY, note VARCHAR NOINDEX)`)
		h.exec(fmt.Sprintf(`INSERT INTO it_rk_model__dbt_tmp VALUES
			(1, 'a', 1.50, X'00ff', 'n1'), (2, 'b', NULL, NULL, NULL), (3, 'c', 3.25, X'01', 'n3'), (4, NULL, -4.00, NULL, 'run %d')`, run))
		tmpPrefix, tmpIndex := h.tableNames(raw, "public", "it_rk_model__dbt_tmp")
		h.rekeyNativeLayout(raw, defaultSchema, "it_rk_model__dbt_tmp")
		if seen[tmpPrefix] {
			t.Fatal("temporary build reused a physical prefix")
		}
		seen[tmpPrefix] = true
		st, oldCtx, oldPin := h.inspectionSnapshot(raw)
		t.Cleanup(oldPin.release)
		oldModel, err := st.getTable(oldCtx, defaultSchema, "it_rk_model")
		if err != nil {
			t.Fatal(err)
		}
		oldTmp, err := st.getTable(oldCtx, defaultSchema, "it_rk_model__dbt_tmp")
		if err != nil {
			t.Fatal(err)
		}
		beforeModel := snapshotExecuteTest(t, st, oldCtx, "SELECT id,label FROM it_rk_model ORDER BY id").rows
		beforeTmp := snapshotExecuteTest(t, st, oldCtx, "SELECT id,label,amount,blob,note FROM it_rk_model__dbt_tmp ORDER BY id").rows
		h.exec(`DROP TABLE it_rk_model`)
		h.exec(`ALTER TABLE it_rk_model__dbt_tmp RENAME TO it_rk_model`)

		// The rows and the index carry the new name, and nothing is left
		// under the old one, on every run.
		prefix, index := h.tableNames(raw, "public", "it_rk_model")
		h.rekeyNativeLayout(raw, defaultSchema, "it_rk_model")
		if seen[prefix] {
			t.Fatal("rekey reused a prior physical prefix")
		}
		seen[prefix] = true
		afterModel := snapshotExecuteTest(t, st, oldCtx, "SELECT id,label FROM it_rk_model ORDER BY id").rows
		afterTmp := snapshotExecuteTest(t, st, oldCtx, "SELECT id,label,amount,blob,note FROM it_rk_model__dbt_tmp ORDER BY id").rows
		if !reflect.DeepEqual(beforeModel, afterModel) || !reflect.DeepEqual(beforeTmp, afterTmp) {
			t.Fatal("dbt rename changed pinned model or build rows")
		}
		oldPin.release()
		for _, meta := range []*tableMeta{oldModel, oldTmp} {
			for _, seg := range meta.snapshotSegments() {
				h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
			}
		}
		if n := h.prefixKeyCount(raw, prefix); n != 4 {
			t.Errorf("run %d: %d keys under %q, want 4", run, n, prefix)
		}
		if !h.indexExists(raw, index) {
			t.Errorf("run %d: index %q missing", run, index)
		}
		h.expectSnapshotReleased(raw, tmpPrefix, tmpIndex)
		fields, err := raw.HGetAll(h.ctx, prefix+"1").Result()
		if err != nil {
			t.Fatal(err)
		}
		if fields["__rowid"] != "1" || fields["label"] != "a" || fields["blob"] != "\x00\xff" || fields["note"] != "n1" {
			t.Errorf("run %d: row 1 = %q", run, fields)
		}

		h.expectRows(`SELECT id, label, amount, note FROM it_rk_model ORDER BY id`,
			"1|a|1.50|n1", "2|b|NULL|NULL", "3|c|3.25|n3", fmt.Sprintf("4|NULL|-4.00|run %d", run))
		h.expectRows(`SELECT id FROM it_rk_model WHERE label = 'c'`, "3")
		h.expectRows(`SELECT id FROM it_rk_model WHERE amount > 0 ORDER BY amount DESC`, "3", "1")
		h.expectRows(`SELECT COUNT(*), COUNT(amount) FROM it_rk_model`, "4|3")
		h.expectRows(`SELECT id FROM it_rk_model WHERE __rowid = 2`, "2")
		h.expectError(`SELECT * FROM it_rk_model__dbt_tmp`, "does not exist")

		// Row IDs continue; new deltas leave the immutable renamed base unchanged.
		h.exec(`INSERT INTO it_rk_model (id, label) VALUES (5, 'e')`)
		h.exec(`UPDATE it_rk_model SET label = 'B' WHERE id = 2`)
		h.exec(`DELETE FROM it_rk_model WHERE id = 3`)
		h.expectRows(`SELECT __rowid, id, label FROM it_rk_model ORDER BY id`,
			"1|1|a", "2|2|B", "4|4|NULL", "5|5|e")
		if n := h.prefixKeyCount(raw, prefix); n != 4 {
			t.Errorf("run %d: %d keys under %q after writes, want 4", run, n, prefix)
		}
	}

	// Views are renamed in the metadata, as before.
	h.exec(`CREATE VIEW it_rk_view AS SELECT id FROM it_rk_model`)
	h.exec(`ALTER TABLE it_rk_view RENAME TO it_rk_view2`)
	h.exec(`ALTER VIEW it_rk_view2 RENAME TO it_rk_view`)
	h.expectRows(`SELECT COUNT(*) FROM it_rk_view`, "4")

	// The usual checks still apply.
	h.exec(`CREATE TABLE it_rk_model__dbt_tmp (x INTEGER)`)
	h.expectError(`ALTER TABLE it_rk_model RENAME TO it_rk_model__dbt_tmp`, "already exists")
	h.expectError(`ALTER TABLE it_rk_model RENAME TO it_rk_view`, "already exists as a view")
	h.expectError(`ALTER TABLE it_rk_model RENAME TO secondary.x`, "another schema")
	before, _ := h.tableNames(raw, "public", "it_rk_model")
	h.exec(`ALTER TABLE it_rk_model RENAME TO it_rk_model`)
	if prefix, _ := h.tableNames(raw, "public", "it_rk_model"); prefix != before {
		t.Errorf("renaming to the same name moved the rows from %q to %q", before, prefix)
	}
}

// A table larger than a cursor page is copied page by page.
func TestSQLRekeyManyPages(t *testing.T) {
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	const n = 2*cursorCount + 500
	h.dropTables("it_rk_pages", "it_rk_pages2")
	raw := h.rawClient()
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	if err := h.ingest("it_rk_pages", adbc.OptionValueIngestModeCreate, false, ids...); err != nil {
		t.Fatal(err)
	}
	oldPrefix, oldIndex := h.tableNames(raw, "public", "it_rk_pages")
	h.exec(`ALTER TABLE it_rk_pages RENAME TO it_rk_pages2`)
	h.rekeyNativeLayout(raw, defaultSchema, "it_rk_pages2")
	newPrefix, newIndex := h.tableNames(raw, defaultSchema, "it_rk_pages2")
	if newPrefix == oldPrefix || newIndex == oldIndex {
		t.Fatal("multipage rekey retained source storage")
	}
	if got := h.prefixKeyCount(raw, newPrefix); got != n {
		t.Errorf("%d keys under the new prefix, want %d", got, n)
	}
	h.expectSnapshotReleased(raw, oldPrefix, oldIndex)
	h.expectRows(`SELECT COUNT(*), SUM(id), MIN(id), MAX(id) FROM it_rk_pages2`,
		fmt.Sprintf("%d|%d|1|%d", n, n*(n+1)/2, n))
	h.expectRows(fmt.Sprintf(`SELECT id FROM it_rk_pages2 ORDER BY id LIMIT 2 OFFSET %d`, cursorCount-1),
		fmt.Sprint(cursorCount), fmt.Sprint(cursorCount+1))
	rows, _ := h.query(`SELECT __rowid, id FROM it_rk_pages2`)
	if len(rows) != n {
		t.Fatalf("full scan returned %d rows, want %d", len(rows), n)
	}
	for _, r := range rows {
		rowid, id, _ := strings.Cut(r, "|")
		if rowid != id {
			t.Fatalf("row %q: __rowid and id differ", r)
		}
	}
}

// Without the option, RENAME TO keeps the keys, as before.
func TestSQLRekeyDefaultOff(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_off", "it_rk_off2")
	raw := h.rawClient()
	if v, err := h.conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringRenameRekey); err != nil || v != "false" {
		t.Errorf("default %s = %q, %v", OptionStringRenameRekey, v, err)
	}
	h.exec(`CREATE TABLE it_rk_off (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_off VALUES (1), (2)`)
	oldPrefix, oldIndex := h.tableNames(raw, "public", "it_rk_off")
	h.exec(`ALTER TABLE it_rk_off RENAME TO it_rk_off2`)
	prefix, index := h.tableNames(raw, "public", "it_rk_off2")
	if prefix != oldPrefix || index != oldIndex {
		t.Errorf("names after a plain rename: %q, %q, want %q, %q", prefix, index, oldPrefix, oldIndex)
	}
	if got := h.prefixKeyCount(raw, prefix); got != 2 {
		t.Errorf("%d keys under %q, want 2", got, prefix)
	}
	h.expectRows(`SELECT id FROM it_rk_off2 ORDER BY id`, "1", "2")
}

// The connection option overrides the database's.
func TestSQLRekeyConnectionOption(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_conn", "it_rk_conn2", "it_rk_conn3")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_rk_conn (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_conn VALUES (1), (2), (3)`)

	if err := h.setConnOption(OptionStringRenameRekey, "yes"); err == nil || !strings.Contains(err.Error(), "want true or false") {
		t.Errorf("invalid value: %v", err)
	}
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	oldPrefix, oldIndex := h.tableNames(raw, defaultSchema, "it_rk_conn")
	h.exec(`ALTER TABLE it_rk_conn RENAME TO it_rk_conn2`)
	newPrefix, newIndex := h.rekeyNativeLayout(raw, defaultSchema, "it_rk_conn2")
	if newPrefix == oldPrefix || newIndex == oldIndex {
		t.Fatal("enabled rekey retained source storage")
	}
	movedPrefix, _ := h.tableNames(raw, "public", "it_rk_conn2")

	// Another connection of a database with the option on, turned off.
	other := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	if err := other.setConnOption(OptionStringRenameRekey, "false"); err != nil {
		t.Fatal(err)
	}
	other.exec(`ALTER TABLE it_rk_conn2 RENAME TO it_rk_conn3`)
	if prefix, _ := h.tableNames(raw, "public", "it_rk_conn3"); prefix != movedPrefix {
		t.Errorf("connection option off: prefix %q, want %q", prefix, movedPrefix)
	}
	other.expectRows(`SELECT COUNT(*) FROM it_rk_conn3`, "3")

	if _, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx,
		map[string]string{OptionStringRenameRekey: "1"}); err == nil {
		t.Error("invalid database option value accepted")
	}
}

// Temporary tables are re-keyed within the connection's temporary schema.
func TestSQLRekeyTempTable(t *testing.T) {
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	raw := h.rawClient()
	before := h.tempOwners(raw)
	h.exec(`CREATE TEMP TABLE it_rk_tmp (id INTEGER, label VARCHAR)`)
	h.exec(`INSERT INTO it_rk_tmp VALUES (1, 'a'), (2, 'b')`)
	schema := tempSchemaBase + h.newTempID(raw, before)
	oldPrefix, oldIndex := h.tableNames(raw, schema, "it_rk_tmp")
	h.exec(`ALTER TABLE it_rk_tmp RENAME TO it_rk_tmp2`)
	prefix, index := h.tableNames(raw, schema, "it_rk_tmp2")
	h.rekeyNativeLayout(raw, schema, "it_rk_tmp2")
	if prefix == oldPrefix || index == oldIndex {
		t.Fatal("temporary rekey retained source storage")
	}
	h.expectSnapshotReleased(raw, oldPrefix, oldIndex)
	h.expectRows(`SELECT id, label FROM it_rk_tmp2 WHERE label = 'b'`, "2|b")
	h.expectRows(`SELECT table_schema, table_type, key_prefix FROM information_schema.tables WHERE table_name = 'it_rk_tmp2'`,
		"pg_temp|LOCAL TEMPORARY|"+prefix)
	// Moving it back takes new names: the old ones are never reused.
	h.exec(`ALTER TABLE it_rk_tmp2 RENAME TO it_rk_tmp`)
	restored, restoredIndex := h.rekeyNativeLayout(raw, schema, "it_rk_tmp")
	if restored == oldPrefix || restored == prefix || restoredIndex == oldIndex || restoredIndex == index {
		t.Fatal("temporary reverse rekey reused physical storage")
	}
	h.expectSnapshotReleased(raw, prefix, index)
	h.expectRows(`SELECT id, label FROM it_rk_tmp ORDER BY id`, "1|a", "2|b")
}

// Public DROP removes fields through private layouts. Old pins keep the original
// images through both drop/rekey operations; retirement uses reference-aware GC.
func TestSQLRekeyFieldsAndCleanup(t *testing.T) {
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables("it_rk_fields", "it_rk_fields2", "it_rk_fields3")
	h.exec(`CREATE TABLE it_rk_fields (id INTEGER,v INTEGER,kept TEXT,ghost TEXT)`)
	h.exec(`INSERT INTO it_rk_fields VALUES (1,10,'k','boo'),(2,20,'k','boo'),(3,30,'k','boo')`)
	raw := h.rawClient()
	st, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	old, err := st.getTable(ctx, defaultSchema, "it_rk_fields")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{old.prefix() + "1", old.prefix() + "2", old.prefix() + "3"}
	before, err := st.fetchSnapshotVisible(ctx, old, keys, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_rk_fields DROP COLUMN kept`)
	h.exec(`ALTER TABLE it_rk_fields RENAME TO it_rk_fields2`)
	if f := h.rowFields(raw, defaultSchema, "it_rk_fields2"); f[rowIDField] != 3 || f["id"] != 3 || f["v"] != 3 || f["ghost"] != 3 || f["kept"] != 0 {
		t.Fatal("fields after first drop/rekey", f)
	}
	middle := h.inspectTable(raw, defaultSchema, "it_rk_fields2")
	h.exec(`ALTER TABLE it_rk_fields2 DROP COLUMN ghost`)
	h.exec(`ALTER TABLE it_rk_fields2 RENAME TO it_rk_fields3`)
	if f := h.rowFields(raw, defaultSchema, "it_rk_fields3"); f[rowIDField] != 3 || f["id"] != 3 || f["v"] != 3 || f["kept"] != 0 || f["ghost"] != 0 {
		t.Fatal("fields after final drop/rekey", f)
	}
	after, err := st.fetchSnapshotVisible(ctx, old, keys, nil)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("drop/rekey changed pinned fields", err)
	}
	for _, row := range after {
		if row["kept"] != "k" || row["ghost"] != "boo" {
			t.Fatal("pinned retired fields disappeared")
		}
	}
	h.expectRows(`SELECT id,v FROM it_rk_fields3 ORDER BY id`, "1|10", "2|20", "3|30")
	pin.release()
	for _, meta := range []*tableMeta{old, middle} {
		for _, seg := range meta.snapshotSegments() {
			h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
		}
	}
}

// A staged rekey uses the real private rewrite and publication pipeline.
// It does not install legacy mutable rekey records or change published input.
type stagedRekeySnapshot struct {
	store          *store
	base, ctx      context.Context
	pin            *snapshotPin
	id             string
	source, target *tableMeta
}

func stageRekeySnapshot(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, from, to string) *stagedRekeySnapshot {
	t.Helper()
	st := &store{client: raw, snapshots: true}
	base, pin, err := st.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	source, err := st.getTable(base, defaultSchema, from)
	if err != nil {
		pin.release()
		t.Fatal(err)
	}
	ctx, id, err := st.beginSnapshotWrite(base, pin)
	if err != nil {
		pin.release()
		t.Fatal(err)
	}
	t.Cleanup(func() { st.abortSnapshot(pin, id); pin.release() })
	parsed, err := ParseScript("ALTER TABLE " + from + " RENAME TO " + to)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: st, sess: newSession(defaultSchema), schema: defaultSchema, rekey: true, pushdown: PushdownExact}
	if _, err := e.execute(ctx, parsed[0], nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	target, err := st.getTable(ctx, defaultSchema, to)
	if err != nil {
		t.Fatal(err)
	}
	data := snapshotDataID(target.prefix())
	if data == "" || target.prefix() != snapshotRows(data, defaultSchema) || target.prefix() == source.prefix() {
		t.Fatal("private rekey did not allocate canonical fresh storage")
	}
	return &stagedRekeySnapshot{store: st, base: base, ctx: ctx, pin: pin, id: id, source: source, target: target}
}
func expectStagedRekeyReleased(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, r *stagedRekeySnapshot) {
	t.Helper()
	h.expectSnapshotReleased(raw, r.target.prefix(), r.target.index())
	indexes, err := raw.Do(h.ctx, "FT._LIST").StringSlice()
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if snapshotDataID(index) == snapshotDataID(r.target.prefix()) {
			t.Fatal("retired private rekey left a final index")
		}
	}
}

// Abandoned private output is rolled back; a committed generation survives its
// coordinator and old input stays pinned until readers release it. Public input
// resumption is separate from this ownership/recovery contract.
func TestSQLRekeyResumes(t *testing.T) {
	const from, to = "it_rk_res", "it_rk_res2"
	h := newSQLHarness(t)
	h.dropTables(from, to)
	h.exec("CREATE TABLE " + from + " (id INTEGER,label TEXT)")
	h.exec("INSERT INTO " + from + " VALUES (1,'a'),(2,'b'),(3,'c')")
	raw := h.rawClient()
	used := map[string]bool{}
	stage := func() *stagedRekeySnapshot {
		r := stageRekeySnapshot(t, h, raw, from, to)
		if used[r.target.prefix()] {
			t.Fatal("private output reused a retired identity")
		}
		used[r.target.prefix()] = true
		return r
	}
	// Live private work leaves the committed input readable and writable. Its
	// stale publication must refuse the intervening write without changing root.
	live := stage()
	before := snapshotExecuteTest(t, live.store, live.base, "SELECT id,label FROM "+from+" ORDER BY id").rows
	other := newSQLHarness(t)
	other.exec("INSERT INTO " + from + " VALUES (4,'d')")
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := live.store.publishSnapshot(live.ctx, live.pin, live.id); sqlState(err) != "40001" {
		t.Fatal("live rekey did not conflict", err)
	}
	if got := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); got != root {
		t.Fatal("stale rekey changed root")
	}
	after := snapshotExecuteTest(t, live.store, live.base, "SELECT id,label FROM "+from+" ORDER BY id").rows
	if !reflect.DeepEqual(before, after) {
		t.Fatal("live workspace changed pinned input")
	}
	live.store.abortSnapshot(live.pin, live.id)
	live.pin.release()
	expectStagedRekeyReleased(t, h, raw, live)
	// Stop the owner heartbeat without aborting its durable workspace, then
	// model lease expiry. A newly opened connection must recover private output.
	abandoned := stage()
	abandoned.pin.release()
	expireRekeyControl(t, h, raw, "w:"+abandoned.id)
	reopened := newSQLHarness(t)
	reopened.expectRows("SELECT id,label FROM "+from+" ORDER BY id", "1|a", "2|b", "3|c", "4|d")
	expectStagedRekeyReleased(t, h, raw, abandoned)
	// An already-open connection must also recover before its next statement.
	abandoned = stage()
	abandoned.pin.release()
	expireRekeyControl(t, h, raw, "w:"+abandoned.id)
	reopened.exec("INSERT INTO " + from + " VALUES (5,'e')")
	expectStagedRekeyReleased(t, h, raw, abandoned)
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("unpublished rename became public", err)
	}
	// Commit once, stop the owner, and recover without rerunning its SQL.
	source, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	old, err := source.getTable(oldCtx, defaultSchema, from)
	if err != nil {
		t.Fatal(err)
	}
	before = snapshotExecuteTest(t, source, oldCtx, "SELECT id,label FROM "+from+" ORDER BY id").rows
	committed := stage()
	if err := committed.store.publishSnapshot(committed.ctx, committed.pin, committed.id); err != nil {
		t.Fatal(err)
	}
	committed.pin.release()
	recovered := newSQLHarness(t)
	recovered.expectRows("SELECT id,label FROM "+to+" ORDER BY id", "1|a", "2|b", "3|c", "4|d", "5|e")
	if _, err := recovered.conn.GetTableSchema(h.ctx, nil, nil, from); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("old name survived committed rename", err)
	}
	after = snapshotExecuteTest(t, source, oldCtx, "SELECT id,label FROM "+from+" ORDER BY id").rows
	if !reflect.DeepEqual(before, after) {
		t.Fatal("coordinator recovery changed old generation")
	}
	for _, seg := range old.snapshotSegments() {
		if n, err := raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(seg.Prefix))).Result(); err != nil || n != seg.Rows {
			t.Fatal("recovery reclaimed pinned source", n, err)
		}
	}
	oldPin.release()
	final := newSQLHarness(t)
	for _, seg := range old.snapshotSegments() {
		final.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
	final.exec("INSERT INTO " + to + " VALUES (6,'f')")
	final.expectRows("SELECT id,label FROM "+to+" ORDER BY id", "1|a", "2|b", "3|c", "4|d", "5|e", "6|f")
}

// Old readers retain their binding while stale writes and destination-name
// races refuse publication. Name reuse never redirects their private output.
func TestSQLRekeyConcurrentStatements(t *testing.T) {
	const from, to, third = "it_rk_conc", "it_rk_conc2", "it_rk_conc3"
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables(from, to, third)
	h.exec("DROP VIEW IF EXISTS " + third)
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS " + third) })
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (1),(2)")
	raw := h.rawClient()
	st, oldCtx, oldPin := h.inspectionSnapshot(raw, true)
	defer oldPin.release()
	old, err := st.getTable(oldCtx, defaultSchema, from)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotExecuteTest(t, st, oldCtx, "SELECT id FROM "+from+" ORDER BY id").rows
	ctx, pin, work := snapshotStageSQL(t, st, h.ctx, from, "INSERT INTO "+from+" VALUES (3)")
	private, err := st.getTable(ctx, defaultSchema, from)
	if err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE " + from + " RENAME TO " + to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (100)")
	reused := h.inspectTable(raw, defaultSchema, from)
	if reused.prefix() == old.prefix() || reused.PrefixGen != 0 {
		t.Fatal("name reuse retained old physical identity")
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := st.publishSnapshot(ctx, pin, work); sqlState(err) != "40001" {
		t.Fatal("stale INSERT followed rename/name reuse", err)
	}
	if got := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); got != root {
		t.Fatal("stale INSERT changed root")
	}
	after := snapshotExecuteTest(t, st, oldCtx, "SELECT id FROM "+from+" ORDER BY id").rows
	if !reflect.DeepEqual(before, after) {
		t.Fatal("name reuse changed pinned read")
	}
	st.abortSnapshot(pin, work)
	pin.release()
	h.expectSnapshotReleased(raw, private.prefix(), private.index())
	h.expectRows("SELECT id FROM "+from, "100")
	h.expectRows("SELECT id FROM "+to+" ORDER BY id", "1", "2")
	// A view created after destination admission wins; no copied output leaks.
	race := stageRekeySnapshot(t, h, raw, to, third)
	h.exec("CREATE VIEW " + third + " AS SELECT 7 AS id")
	root = raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := race.store.publishSnapshot(race.ctx, race.pin, race.id); sqlState(err) != "40001" {
		t.Fatal("rekey overwrote destination view", err)
	}
	if got := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); got != root {
		t.Fatal("destination race changed root")
	}
	race.store.abortSnapshot(race.pin, race.id)
	race.pin.release()
	expectStagedRekeyReleased(t, h, raw, race)
	h.expectRows("SELECT id FROM "+third, "7")
	h.expectRows("SELECT id FROM "+to+" ORDER BY id", "1", "2")
	h.expectError("ALTER TABLE "+to+" RENAME TO "+third, "already exists as a view")
	h.exec("DROP VIEW " + third)
	h.exec("ALTER TABLE " + to + " RENAME TO " + third)
	h.expectRows("SELECT id FROM "+third+" ORDER BY id", "1", "2")
	// A fresh explicit write targets the new binding only.
	h.exec("INSERT INTO " + from + " VALUES (101)")
	h.expectRows("SELECT id FROM "+from+" ORDER BY id", "100", "101")
	oldPin.release()
	for _, seg := range old.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
}

// The native lease cannot be renewed after expiry. Both row and index/publication
// mutations must fail before changing private or committed state.
func TestSQLRekeyLease(t *testing.T) {
	const from, to = "it_rk_lease", "it_rk_lease2"
	h := newSQLHarness(t)
	h.dropTables(from, to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (1),(2),(3)")
	raw := h.rawClient()
	r := stageRekeySnapshot(t, h, raw, from, to)
	n, err := r.store.snapshotControl(h.ctx, "renew", r.pin.token, r.id, r.pin.root).Int()
	if err != nil || n != 1 {
		t.Fatal("live rekey lease could not renew", n, err)
	}
	image, err := raw.HGetAll(h.ctx, r.target.prefix()+"1").Result()
	if err != nil {
		t.Fatal(err)
	}
	expireRekeyControl(t, h, raw, "w:"+r.id)
	n, err = r.store.snapshotControl(h.ctx, "renew", r.pin.token, r.id, r.pin.root).Int()
	if err != nil || n != 0 {
		t.Fatal("expired rekey lease renewed", n, err)
	}
	err = r.store.redis(r.ctx).HSet(r.ctx, r.target.prefix()+"1", "id", 99).Err()
	if err == nil || !strings.Contains(err.Error(), "lease") {
		t.Fatal("expired writer changed private row", err)
	}
	if err := r.store.publishSnapshot(r.ctx, r.pin, r.id); err == nil {
		t.Fatal("expired rekey published")
	}
	if after, err := raw.HGetAll(h.ctx, r.target.prefix()+"1").Result(); err != nil || !reflect.DeepEqual(image, after) {
		t.Fatal("expired mutation changed row", err)
	}
	indexes, err := raw.Do(h.ctx, "FT._LIST").StringSlice()
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if snapshotDataID(index) == snapshotDataID(r.target.prefix()) {
			t.Fatal("expired writer built an index")
		}
	}
	r.pin.release()
	recovered := newSQLHarness(t)
	expectStagedRekeyReleased(t, recovered, raw, r)
	recovered.expectRows("SELECT id FROM "+from+" ORDER BY id", "1", "2", "3")
	if _, err := recovered.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("expired target became public", err)
	}
	recovered.exec("INSERT INTO " + from + " VALUES (4)")
	recovered.expectRows("SELECT id FROM "+from+" ORDER BY id", "1", "2", "3", "4")
}

func TestSQLRekeyInformationSchema(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_is", "it_rk_is2", "it_rk_is3")
	h.exec("DROP VIEW IF EXISTS it_rk_isv")
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS it_rk_isv") })
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_rk_is (id INTEGER)`)
	h.exec(`CREATE VIEW it_rk_isv AS SELECT id FROM it_rk_is`)
	q := `SELECT table_name, table_type, key_prefix, index_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name LIKE 'it_rk_is%' ORDER BY table_name`
	names := func(table string) string {
		prefix, index := h.rekeyNativeLayout(raw, defaultSchema, table)
		return prefix + "|" + index
	}
	original := names("it_rk_is")
	h.expectRows(q, "it_rk_is|BASE TABLE|"+names("it_rk_is"), "it_rk_isv|VIEW|NULL|NULL")

	// A plain rename keeps the names; a re-keying one changes them.
	h.exec(`ALTER TABLE it_rk_is RENAME TO it_rk_is2`)
	if names("it_rk_is2") != original {
		t.Fatal("plain rename changed physical identity")
	}
	h.expectRows(q, "it_rk_is2|BASE TABLE|"+names("it_rk_is2"), "it_rk_isv|VIEW|NULL|NULL")
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_rk_is2 RENAME TO it_rk_is3`)
	if names("it_rk_is3") == original {
		t.Fatal("forced rekey retained physical identity")
	}
	h.expectRows(q, "it_rk_is3|BASE TABLE|"+names("it_rk_is3"), "it_rk_isv|VIEW|NULL|NULL")
}

// tryExec runs a statement and returns the rows affected and its error.
func tryExec(ctx context.Context, conn adbc.ConnectionWithContext, sql string) (int64, error) {
	st, err := conn.NewStatement(ctx)
	if err != nil {
		return 0, err
	}
	defer st.Close(ctx)
	if err := st.SetSqlQuery(ctx, sql); err != nil {
		return 0, err
	}
	return st.ExecuteUpdate(ctx)
}

// Detect actual admission conditions rather than require unbounded writes past
// the segment limit or the compactor's short installation gate.
func rekeyWriterBackpressure(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, table string) bool {
	t.Helper()
	current := h.inspectTable(raw, defaultSchema, table)
	if len(current.snapshotSegments()) >= snapshotMaxSegments {
		return true
	}
	payload, err := raw.HGet(h.ctx, snapshotCatalogKey, "compaction").Bytes()
	if err == goredis.Nil {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	var job snapshotCompactionJob
	if err := json.Unmarshal(payload, &job); err != nil {
		t.Fatal(err)
	}
	if job.Phase != "install" {
		return false
	}
	payload, err = raw.HGet(h.ctx, snapshotCatalogKey, "w:"+job.ID).Bytes()
	if err == goredis.Nil {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	var work struct {
		State        string `json:"state"`
		Expires      int64  `json:"expires"`
		PublishUntil int64  `json:"publish_until"`
	}
	if err := json.Unmarshal(payload, &work); err != nil {
		t.Fatal(err)
	}
	now, err := raw.Time(h.ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	return work.State == "compacting" && work.Expires > now.UnixMilli() && work.PublishUntil > now.UnixMilli()
}

// Continuous writers overlap a real paused copy. Changed input must conflict;
// bounded admission may wait for that copy to finish. No acknowledged write may
// disappear, and no rejected INSERT may reach the renamed table.
func TestSQLRekeyConcurrentWriters(t *testing.T) {
	const from, to = "it_rk_cw", "it_rk_cw2"
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables(from, to)
	raw := h.rawClient()
	ids := make([]int64, 3000)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	if err := h.ingest(from, adbc.OptionValueIngestModeCreate, false, ids...); err != nil {
		t.Fatal(err)
	}
	old := h.inspectTable(raw, defaultSchema, from)
	writer := newSQLHarness(t)
	stop, stopped := make(chan struct{}), make(chan struct{})
	var stopOnce sync.Once
	stopWriter := func() {
		stopOnce.Do(func() { close(stop) })
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			_ = writer.conn.Close(h.ctx)
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Error("writer did not stop")
			}
		}
	}
	t.Cleanup(stopWriter)
	var progress, acknowledged atomic.Int64
	var pending atomic.Bool
	var inserted, rejected []int64
	var unexpected []error
	conflicts := 0
	moved := int64(1)
	knownRefusal := func(err error) bool {
		if sqlState(err) == "40001" {
			conflicts++
			return true
		}
		return isStatus(err, adbc.StatusNotFound)
	}
	go func() {
		defer close(stopped)
		for i := int64(1); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			value := 100000 + i
			pending.Store(true)
			_, err := tryExec(h.ctx, writer.conn, fmt.Sprintf("INSERT INTO %s VALUES (%d)", from, value))
			pending.Store(false)
			if err == nil {
				inserted = append(inserted, value)
				acknowledged.Add(1)
			} else if knownRefusal(err) {
				rejected = append(rejected, value)
			} else {
				unexpected = append(unexpected, err)
			}
			pending.Store(true)
			n, err := tryExec(h.ctx, writer.conn, fmt.Sprintf("UPDATE %s SET id=%d WHERE id=%d", from, -i, moved))
			pending.Store(false)
			if err == nil && n == 1 {
				moved = -i
				acknowledged.Add(1)
			} else if err == nil {
				unexpected = append(unexpected, fmt.Errorf("moving UPDATE affected %d rows", n))
			} else if !knownRefusal(err) {
				unexpected = append(unexpected, err)
			}
			progress.Add(1)
		}
	}()
	waitFor(t, "writer progress", func() bool { return progress.Load() >= 5 })
	// The source can span changing segment UUIDs; match its native schema prefix.
	prefix := strings.TrimSuffix(snapshotRows("fixture", defaultSchema), "fixture:")
	stmt, ctx, _, transport, barrier := snapshotPausedAlter(t, h, "ALTER TABLE "+from+" RENAME TO "+to, prefix)
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(barrier.resume) }) }
	t.Cleanup(resume)
	done := make(chan error, 1)
	go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
	scope := snapshotAwaitAlterBatch(t, h, raw, barrier, done)
	baseCtx := context.WithValue(h.ctx, snapshotContextKey{}, &snapshotScope{id: scope.pin.root, pin: scope.pin, store: scope.store})
	input, err := scope.store.redis(baseCtx).Get(baseCtx, metaKey(defaultSchema, from)).Result()
	if err != nil {
		t.Fatal(err)
	}
	sourceChanged, backpressure := false, false
	waitFor(t, "intervening source change or verified writer admission", func() bool {
		if h.storedTableJSON(raw, defaultSchema, from) != input {
			sourceChanged = true
			return true
		}
		if pending.Load() && rekeyWriterBackpressure(t, h, raw, from) {
			backpressure = true
			return true
		}
		return false
	})
	// Release the writer intent before waiting for a backpressured DML statement.
	resume()
	renameErr := snapshotAwaitAlterError(t, done)
	if sourceChanged && sqlState(renameErr) != "40001" {
		t.Fatal("changed-input rekey did not conflict", renameErr)
	}
	if renameErr != nil && sqlState(renameErr) != "40001" {
		t.Fatal("overlapping rekey", renameErr)
	}
	stopWriter()
	if renameErr != nil {
		snapshotExpectAlterLeaseReleased(t, h, raw, scope)
	} else {
		if exists, err := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+scope.pin.token).Result(); err != nil || exists {
			t.Fatal("committed rekey retained reader lease", err)
		}
		payload, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
		if err != goredis.Nil {
			var work struct {
				State string `json:"state"`
			}
			if err != nil || json.Unmarshal(payload, &work) != nil || work.State != "committed" {
				t.Fatal("committed rekey retained active writer state", err)
			}
		}
	}
	if len(unexpected) != 0 {
		t.Fatal("unexpected writer errors", unexpected)
	}
	if len(inserted) == 0 || moved == 1 {
		t.Fatal("writer made no committed progress")
	}
	if renameErr != nil {
		if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
			t.Fatal("conflicting rekey exposed target", err)
		}
	}
	// Each additional request follows a proven 40001 refusal. The driver never
	// resends SQL; background compaction may still publish a source rewrite.
	renamed := renameErr == nil
	freshRefusals := 0
	for attempt := 0; !renamed && attempt < 8; attempt++ {
		_, err := tryExec(h.ctx, h.conn, "ALTER TABLE "+from+" RENAME TO "+to)
		if err == nil {
			renamed = true
			break
		}
		if sqlState(err) != "40001" {
			t.Fatal("fresh rekey", err)
		}
		freshRefusals++
		if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
			t.Fatal("refused rekey exposed target", err)
		}
	}
	if !renamed {
		t.Fatal("fresh explicit rekey did not converge")
	}
	expected := map[int64]bool{moved: true}
	for i := int64(2); i <= 3000; i++ {
		expected[i] = true
	}
	for _, id := range inserted {
		expected[id] = true
	}
	got := map[int64]bool{}
	rows, _ := h.query("SELECT id FROM " + to)
	for _, row := range rows {
		var id int64
		if _, err := fmt.Sscan(row, &id); err != nil {
			t.Fatal(err)
		}
		if got[id] {
			t.Fatal("duplicate rekey row", id)
		}
		got[id] = true
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("renamed rows differ from acknowledged writes", len(got), len(expected))
	}
	for _, id := range rejected {
		if got[id] {
			t.Fatal("rejected INSERT reached target", id)
		}
	}
	target := h.inspectTable(raw, defaultSchema, to)
	if n := h.prefixKeyCount(raw, target.prefix()); n != len(rows) {
		t.Fatal("rekey physical output differs from complete result", n, len(rows))
	}
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, from); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("old binding survived rename", err)
	}
	for _, seg := range old.snapshotSegments() {
		waitFor(t, "retired source reclamation", func() bool {
			snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
			return h.prefixKeyCount(raw, seg.Prefix) == 0
		})
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
	t.Logf("acknowledged inserts=%d rejected inserts=%d writer conflicts=%d fresh rekey refusals=%d observed backpressure=%t", len(inserted), len(rejected), conflicts, freshRefusals, backpressure)
}
