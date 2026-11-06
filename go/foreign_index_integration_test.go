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
	"slices"
	"strings"
	"testing"
)

// Application indexes, including legacy table-like names and prefixes, survive
// CREATE, TRUNCATE, rekey, DROP and GC. Managed tables use private generations.
func TestSQLForeignIndexName(t *testing.T) {
	h := newSQLHarness(t)
	raw := h.rawClient()
	st := &store{client: raw}
	h.exec(`DROP TABLE IF EXISTS it_fi_t`)
	h.exec(`DROP TABLE IF EXISTS it_fi_u`)
	// Legacy-looking names belong to the application, regardless of whether
	// their indexes are empty. Managed generations use fresh UUID identities.
	t3 := namesFor("public", "it_fi_t", 3)
	u1 := namesFor("public", "it_fi_u", 1)
	apps := []struct{ index, prefix, key string }{
		{namesFor("public", "it_fi_t", 1).index, "it_fi_app:", "it_fi_app:1"},
		{t3.index, t3.prefix, t3.prefix + "1"},
		{u1.index, u1.prefix, ""},
	}
	t.Cleanup(func() {
		h.exec(`DROP TABLE IF EXISTS it_fi_t`)
		h.exec(`DROP TABLE IF EXISTS it_fi_u`)
		for _, a := range apps {
			_ = st.searchDo(h.ctx, a.index, "FT.DROPINDEX", a.index).Err()
			if a.key != "" {
				_ = raw.Del(h.ctx, a.key).Err()
			}
		}
	})
	for _, a := range apps {
		args := []any{"FT.CREATE", a.index, "ON", "HASH", "PREFIX", 1, a.prefix}
		if a.key == "" {
			args = append(args, "FILTER", "@n > 0")
		}
		args = append(args, "SCHEMA", "n", "NUMERIC")
		if err := st.searchDo(h.ctx, a.index, args...).Err(); err != nil {
			t.Fatal(err)
		}
		if a.key != "" {
			if err := raw.HSet(h.ctx, a.key, "n", 1).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Each still exists, and so does its HASH.
	expectApps := func(step string) {
		t.Helper()
		for _, a := range apps {
			if err := st.searchDo(h.ctx, a.index, "FT.INFO", a.index).Err(); err != nil {
				t.Errorf("%s: the application's index %s: %v", step, a.index, err)
			}
			if a.key == "" {
				continue
			}
			fields, err := raw.HGetAll(h.ctx, a.key).Result()
			if err != nil || len(fields) != 1 || fields["n"] != "1" {
				t.Errorf("%s: application's HASH changed: %v %v", step, fields, err)
			}
		}
	}
	usedPrefixes, usedIndexes := map[string]bool{}, map[string]bool{}
	expectNames := func(step, table string) {
		t.Helper()
		rows, _ := h.query(`SELECT key_prefix || '|' || index_name FROM information_schema.tables WHERE table_name = '` + table + `'`)
		if len(rows) != 1 {
			t.Fatalf("%s: expected one managed table, got %q", step, rows)
		}
		prefix, index, ok := strings.Cut(rows[0], "|")
		if !ok || snapshotRowPrefix(prefix) != prefix || prefix == "" || !strings.HasPrefix(index, snapshotIndexPrefix(snapshotDataID(prefix))) {
			t.Fatalf("%s: invalid private generation names %q", step, rows)
		}
		if usedPrefixes[prefix] || usedIndexes[index] {
			t.Errorf("%s reused a prior generation: %q", step, rows)
		}
		usedPrefixes[prefix], usedIndexes[index] = true, true
		for _, a := range apps {
			if prefix == a.prefix || index == a.index {
				t.Errorf("%s claimed application storage: %q", step, rows)
			}
		}
	}

	h.exec(`CREATE TABLE it_fi_t (a INTEGER)`)
	expectApps("CREATE TABLE")
	expectNames("CREATE TABLE", "it_fi_t")
	h.exec(`INSERT INTO it_fi_t VALUES (1), (2)`)
	h.expectRows(`SELECT COUNT(*) FROM it_fi_t`, "2")

	h.exec(`TRUNCATE it_fi_t`)
	expectApps("TRUNCATE")
	expectNames("TRUNCATE", "it_fi_t")
	h.exec(`INSERT INTO it_fi_t VALUES (3)`)

	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_fi_t RENAME TO it_fi_u`)
	expectApps("RENAME")
	expectNames("RENAME", "it_fi_u")
	h.expectRows(`SELECT a FROM it_fi_u`, "3")
	h.exec(`DROP TABLE it_fi_u`)
	snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
	expectApps("DROP and GC")
	info, err := st.searchDo(h.ctx, u1.index, "FT.INFO", u1.index).Result()
	if err != nil || !slices.Equal(indexInfoOf(info).prefixes, []string{u1.prefix}) || indexInfoOf(info).filter != "@n > 0" {
		t.Fatal("application index policy changed", err)
	}
}

// Empty unowned indexes survive CREATE and GC. Only a registered private
// output of an interrupted CREATE is reclaimed; its identity is never reused.
func TestSQLLeftoverIndexOwnership(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_fi_left")
	raw := h.rawClient()
	st := &store{client: raw, snapshots: true}
	nm := namesFor("public", "it_fi_left", 1)
	app := &tableMeta{Schema: "public", Name: "it_fi_left", Columns: []columnMeta{{Name: "a", Type: typeInt32, Indexed: true}}, KeyPrefix: nm.prefix, IndexName: nm.index}
	if err := st.searchDo(h.ctx, app.index(), indexCreateArgs(app)...).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.searchDo(h.ctx, app.index(), "FT.DROPINDEX", app.index()).Err() })
	hook := &managedImportIndexFailure{}
	privateRaw := h.rawClient(hook)
	privateStore := &store{client: privateRaw, snapshots: true}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	wctx, pin, work := snapshotStageSQL(t, privateStore, h.ctx, "", "CREATE TABLE it_fi_left (a INTEGER)")
	private, err := privateStore.getTable(wctx, defaultSchema, "it_fi_left")
	if err != nil {
		t.Fatal(err)
	}
	err = privateStore.rebuildSnapshotIndexes(wctx)
	if err == nil || !strings.Contains(err.Error(), "injected late private index failure") || !hook.fired.Load() || hook.creates.Load() != 1 || !strings.HasPrefix(hook.index, snapshotIndexPrefix(snapshotDataID(private.prefix()))) {
		t.Fatalf("late CREATE index failure was hidden or replayed: %v", err)
	}
	info, err := privateStore.searchDo(wctx, hook.index, "FT.INFO", hook.index).Result()
	if err != nil || !slices.Equal(indexInfoOf(info).prefixes, []string{private.prefix()}) {
		t.Fatal("failure did not leave a real index on the registered private prefix", err)
	}
	if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("failed CREATE published")
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='it_fi_left'", "0")
	privateStore.abortSnapshot(pin, work)
	pin.release()
	h.expectSnapshotReleased(raw, private.prefix(), hook.index)
	h.exec(`CREATE TABLE it_fi_left (a INTEGER)`)
	current := h.inspectTable(raw, defaultSchema, "it_fi_left")
	if current.prefix() == private.prefix() || current.index() == hook.index || current.prefix() == app.prefix() || current.index() == app.index() {
		t.Fatal("fresh CREATE reused retired or unowned storage")
	}
	h.exec(`INSERT INTO it_fi_left VALUES (7)`)
	h.expectRows(`SELECT a FROM it_fi_left`, "7")
	written := h.inspectTable(raw, defaultSchema, "it_fi_left")
	h.exec(`DROP TABLE it_fi_left`)
	h.expectSnapshotReleased(raw, current.prefix(), current.index())
	h.expectSnapshotReleased(raw, written.prefix(), written.index())
	info, err = st.searchDo(h.ctx, app.index(), "FT.INFO", app.index()).Result()
	if err != nil || !slices.Equal(indexInfoOf(info).prefixes, []string{app.prefix()}) {
		t.Fatal("unowned empty index was reclaimed", err)
	}
}

// A driver user cannot inspect the application's HASH prefix. CREATE must
// use a private generation without adopting or reclaiming that external index.
func TestACLForeignIndexName(t *testing.T) {
	a := newACLHarness(t)
	st := &store{client: a.raw}
	a.exec(`DROP TABLE IF EXISTS it_fi_acl`)
	app := namesFor("public", "it_fi_acl", 1)
	if err := st.searchDo(a.ctx, app.index, "FT.CREATE", app.index, "ON", "HASH", "PREFIX", 1, "it_fi_acl_app:",
		"SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.exec(`DROP TABLE IF EXISTS it_fi_acl`)
		_ = st.searchDo(a.ctx, app.index, "FT.DROPINDEX", app.index).Err()
	})
	a.user("it_fi_acl", append(a.driverCommands(), "~adbc:*", "~public:*")...)
	u := a.connect("it_fi_acl", nil)
	u.exec(`CREATE TABLE it_fi_acl (a INTEGER)`)
	u.exec(`INSERT INTO it_fi_acl VALUES (1)`)
	prefix, index := a.tableNames(a.raw, defaultSchema, "it_fi_acl")
	if prefix == app.prefix || index == app.index || snapshotRowPrefix(prefix) != prefix || !strings.HasPrefix(index, snapshotIndexPrefix(snapshotDataID(prefix))) {
		t.Fatal("driver user claimed application storage")
	}
	if err := st.searchDo(a.ctx, app.index, "FT.INFO", app.index).Err(); err != nil {
		t.Errorf("the application's index: %v", err)
	}
}
