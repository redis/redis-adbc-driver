// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Named live schema suites use the disposable snapshot database: they create
// catalog entries, indexes and source HASHes, and FLUSHDB precedes each test.
func liveSchemaHarness(t *testing.T) (*sqlHarness, *goredis.Client) {
	t.Helper()
	h, raw, _ := newSnapshotHarness(t)
	setConnOption(t, h, OptionStringLiveTables, "true")
	setConnOption(t, h, OptionStringSchemaOwnership, "live")
	return h, raw
}

func setConnOption(t *testing.T, h *sqlHarness, key, value string) {
	t.Helper()
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, key, value); err != nil {
		t.Fatalf("set %s=%s: %v", key, value, err)
	}
}

func liveSchemaStore(t *testing.T, h *sqlHarness) *store {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	return st.(*statementImpl).conn.store
}

func expectIndexGone(t *testing.T, raw *goredis.Client, index string) {
	t.Helper()
	if err := raw.Do(context.Background(), "FT.INFO", index).Err(); err == nil || !isUnknownIndex(err) {
		t.Fatalf("driver-owned live index %s was not removed: %v", index, err)
	}
}

func TestLiveSchemaDDLLifecycle(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx, opts := h.ctx, unmanagedArrowDatabaseOptions()

	h.exec("CREATE SCHEMA IF NOT EXISTS redis_live")
	h.expectError("CREATE SCHEMA redis_live", "already exists")
	h.expectError("DROP SCHEMA redis_live", "read-only")
	h.expectError("ALTER SCHEMA redis_live RENAME TO other", "read-only")

	h.exec("CREATE SCHEMA app")
	h.expectError("CREATE SCHEMA app", "already exists")
	h.exec("CREATE SCHEMA IF NOT EXISTS app")
	h.expectRows("SELECT schema_name FROM information_schema.schemata WHERE schema_name='app'", "app")
	// CREATE TABLE in a live schema creates a live table (TestLiveSchemaCreateTable);
	// other managed objects are refused.
	h.expectError("CREATE VIEW app.v AS SELECT 1 AS a", "holds live tables")
	h.expectError("CREATE SEQUENCE app.s", "holds live tables")

	binary := "app:users:" + string([]byte{'b', 0, 'c'})
	if !strings.ContainsRune(binary, 0) {
		t.Fatal("binary fixture has no zero byte")
	}
	for key, fields := range map[string][]any{
		"app:users:a":        {"name", "Ada", "age", "37", "extra", "keep"},
		binary:               {"name", "Bin", "age", "5"},
		"app:users:a:nested": {"name", "Nested", "age", "1"},
		"app:pets:p1":        {"kind", "cat"},
	} {
		if err := raw.HSet(ctx, key, fields...).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Expire(ctx, "app:users:a", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	users, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "app:users:", Schema: "app", Table: "users", Types: map[string]string{"age": "BIGINT"}, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if !users.Applied || users.Table != "app.users" {
		t.Fatalf("registration %+v", users)
	}
	pets, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "app:pets:", Schema: "app", Table: "pets", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "app:other:", Schema: "missing", Table: "t", Apply: true}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("registration into a missing live schema: %v", err)
	}

	h.expectRows("SELECT name, age FROM app.users ORDER BY age", "Bin|5", "Ada|37")
	h.expectRows("SELECT CAST(__key AS BYTEA) = CAST('"+binary+"' AS BYTEA) FROM app.users WHERE age = 5", "true")
	h.expectRows("SELECT table_name FROM information_schema.tables WHERE table_schema='app' ORDER BY table_name", "pets", "users")
	h.exec("INSERT INTO app.users (__key, name, age) VALUES ('app:users:c', 'Cy', 41)")
	if raw.HGet(ctx, "app:users:c", "name").Val() != "Cy" {
		t.Fatal("named live INSERT did not write the source HASH")
	}
	h.exec("UPDATE app.users SET age = 38 WHERE __key = 'app:users:a'")
	if raw.HGet(ctx, "app:users:a", "age").Val() != "38" || raw.HGet(ctx, "app:users:a", "extra").Val() != "keep" || raw.TTL(ctx, "app:users:a").Val() <= 0 {
		t.Fatal("named live UPDATE changed unknown fields or TTL")
	}
	h.exec("DELETE FROM app.users WHERE __key = 'app:users:c'")
	if raw.Exists(ctx, "app:users:c").Val() != 0 {
		t.Fatal("named live DELETE kept the source HASH")
	}
	h.expectRows("SELECT u.name, p.kind FROM app.users u CROSS JOIN app.pets p WHERE u.age = 38", "Ada|cat")

	setConnOption(t, h, OptionStringSchemaOwnership, "managed")
	h.exec("CREATE TABLE public.managed_side (a INTEGER)")
	setConnOption(t, h, OptionStringSchemaOwnership, "live")
	h.expectError("DROP TABLE app.pets, public.managed_side", "cannot combine")
	h.expectError("INSERT INTO public.managed_side SELECT age FROM app.users", "managed writes cannot access live tables")

	// Table rename moves only the SQL registration.
	h.expectError("ALTER TABLE app.users ADD COLUMN z INTEGER", "only ALTER TABLE")
	h.expectError("ALTER TABLE app.users RENAME COLUMN name TO n", "only ALTER TABLE")
	h.expectError("ALTER TABLE app.users RENAME TO public.people", "another schema")
	h.expectError("ALTER VIEW app.users RENAME TO people", "not a view")
	h.expectError("ALTER TABLE app.pets RENAME TO users", "already exists")
	st := liveSchemaStore(t, h)
	ref, ok, err := st.liveSchemaRef(ctx, "app")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	before, err := st.liveTable(ctx, ref, "users")
	if err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE app.users RENAME TO people")
	h.expectError("SELECT name FROM app.users", "does not exist")
	h.expectRows("SELECT name FROM app.people WHERE age = 38", "Ada")
	if err := st.checkLiveIdentity(ctx, before); err == nil {
		t.Fatal("a statement using the old table name passed its identity check")
	}
	after, err := st.liveTable(ctx, ref, "people")
	if err != nil {
		t.Fatal(err)
	}
	if after.index() != users.Index || after.prefix() != "app:users:" || after.LiveVersion != before.LiveVersion {
		t.Fatal("table rename changed the index, prefix or live version")
	}
	h.exec("ALTER TABLE IF EXISTS app.users RENAME TO again")
	h.expectError("ALTER TABLE app.users RENAME TO again", "does not exist")

	// Schema rename keeps table records and in-flight identities.
	h.expectError("ALTER SCHEMA app RENAME TO redis_live", "reserved")
	h.expectError("ALTER SCHEMA app RENAME TO public", "reserved")
	h.expectError("ALTER SCHEMA public RENAME TO p2", "only for live schemas")
	h.exec("ALTER SCHEMA app RENAME TO app2")
	h.expectError("SELECT name FROM app.people", "does not exist")
	h.expectRows("SELECT name FROM app2.people WHERE age = 38", "Ada")
	h.expectRows("SELECT table_name FROM information_schema.tables WHERE table_schema='app2' ORDER BY table_name", "people", "pets")
	h.expectRows("SELECT count(*) FROM information_schema.schemata WHERE schema_name='app'", "0")
	if err := st.checkLiveIdentity(ctx, after); err != nil {
		t.Fatal("schema rename broke a live identity:", err)
	}
	if err := ResumeLiveHashesInSchema(ctx, opts, "app2", "people"); err != nil {
		t.Fatal(err)
	}
	if err := ResumeLiveHashesInSchema(ctx, opts, "app", "people"); err == nil {
		t.Fatal("API accepted the old schema name")
	}
	h.exec("CREATE SCHEMA app")
	h.expectRows("SELECT count(*) FROM information_schema.tables WHERE table_schema='app'", "0")
	h.exec("DROP SCHEMA app")

	// DROP SCHEMA unregisters every table and keeps every source HASH.
	h.expectError("DROP SCHEMA app2", "not empty")
	h.exec("DROP SCHEMA app2 CASCADE")
	expectIndexGone(t, raw, users.Index)
	expectIndexGone(t, raw, pets.Index)
	if raw.HGet(ctx, "app:users:a", "extra").Val() != "keep" || raw.TTL(ctx, "app:users:a").Val() <= 0 || raw.HGet(ctx, binary, "name").Val() != "Bin" ||
		raw.Exists(ctx, "app:users:a:nested", "app:pets:p1").Val() != 2 {
		t.Fatal("DROP SCHEMA CASCADE changed source HASHes or TTLs")
	}
	if raw.Exists(ctx, ref.registry(), ref.stateKey()).Val() != 0 {
		t.Fatal("DROP SCHEMA left live control keys")
	}
	h.expectError("SELECT name FROM app2.people", "does not exist")
	h.expectRows("SELECT count(*) FROM information_schema.schemata WHERE schema_name IN ('app', 'app2')", "0")
	h.exec("DROP SCHEMA IF EXISTS app2")
	h.expectError("DROP SCHEMA app2", "does not exist")
}

func TestLiveSchemaDropTableKeepsSources(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx, opts := h.ctx, unmanagedArrowDatabaseOptions()
	h.exec("CREATE SCHEMA sales")
	for _, key := range []string{"sales:a:1", "sales:b:1"} {
		if err := raw.HSet(ctx, key, "v", "1", "unknown", "keep").Err(); err != nil {
			t.Fatal(err)
		}
		if err := raw.Expire(ctx, key, time.Hour).Err(); err != nil {
			t.Fatal(err)
		}
	}
	a, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "sales:a:", Schema: "sales", Table: "a", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "sales:b:", Schema: "sales", Table: "b", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	h.expectError("DROP TABLE sales.a, sales.missing", "does not exist")
	h.expectRows("SELECT v FROM sales.a", "1") // preflight refused before any removal
	h.exec("DROP TABLE IF EXISTS sales.a, sales.missing, sales.b")
	expectIndexGone(t, raw, a.Index)
	expectIndexGone(t, raw, b.Index)
	for _, key := range []string{"sales:a:1", "sales:b:1"} {
		if raw.HGet(ctx, key, "unknown").Val() != "keep" || raw.TTL(ctx, key).Val() <= 0 {
			t.Fatal("DROP TABLE changed a source HASH or TTL")
		}
	}
	h.expectRows("SELECT count(*) FROM information_schema.tables WHERE table_schema='sales'", "0")
	h.exec("DROP SCHEMA sales")
}

func TestLiveSchemaManagedNameCollisions(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx := h.ctx
	setConnOption(t, h, OptionStringSchemaOwnership, "managed")
	h.exec("CREATE SCHEMA mgd")
	h.exec("CREATE TABLE mgd.t (a INTEGER)")
	setConnOption(t, h, OptionStringSchemaOwnership, "live")
	h.expectError("CREATE SCHEMA mgd", "already exists")
	h.exec("CREATE SCHEMA IF NOT EXISTS mgd") // keeps its managed ownership
	h.exec("INSERT INTO mgd.t VALUES (1)")
	h.expectRows("SELECT a FROM mgd.t", "1")
	h.expectError("ALTER SCHEMA mgd RENAME TO m2", "only for live schemas")
	h.exec("CREATE SCHEMA lv")
	h.expectError("ALTER SCHEMA lv RENAME TO mgd", "already exists")
	setConnOption(t, h, OptionStringSchemaOwnership, "managed")
	h.expectError("CREATE SCHEMA lv", "already exists")
	h.exec("CREATE SCHEMA IF NOT EXISTS lv")

	if err := raw.HSet(ctx, "lv:src:1", "v", "1").Err(); err != nil {
		t.Fatal(err)
	}
	adoption, err := AdoptHashes(ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Prefix: "lv:src:", Schema: "lv", Table: "copy", Apply: true})
	if err == nil {
		t.Fatal("managed adoption into a live schema was accepted")
	}
	blocked := false
	if adoption != nil && adoption.Report != nil {
		for _, list := range adoption.Report.Checks {
			for _, item := range list.Items {
				blocked = blocked || item.Status == CheckBlocker && strings.Contains(item.Text, "holds live tables")
			}
		}
	}
	if !blocked {
		t.Fatalf("adoption into a live schema lacks its blocker: %v", err)
	}
	h.expectRows("SELECT count(*) FROM information_schema.tables WHERE table_schema='lv'", "0")

	// Without live_tables a live schema is hidden and cannot be dropped as managed.
	setConnOption(t, h, OptionStringLiveTables, "false")
	h.expectRows("SELECT count(*) FROM information_schema.schemata WHERE schema_name='lv'", "0")
	h.expectError("DROP SCHEMA lv", "holds live tables")
	h.expectError("ALTER SCHEMA lv RENAME TO lv2", "holds live tables")
	// The managed path cannot create a table in a live schema.
	h.expectError("CREATE TABLE lv.t (a INTEGER)", "holds live tables")
	setConnOption(t, h, OptionStringSchemaOwnership, "live")
	h.expectError("CREATE SCHEMA other", "live_tables")
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(ctx, OptionStringSchemaOwnership, "both"); err == nil {
		t.Fatal("accepted an invalid schema ownership")
	}
	setConnOption(t, h, OptionStringLiveTables, "true")
	h.exec("DROP SCHEMA lv")
	h.exec("DROP TABLE mgd.t")
	h.exec("DROP SCHEMA mgd")
}

func TestLiveSchemaDropFencesRegistration(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx, opts := h.ctx, unmanagedArrowDatabaseOptions()
	h.exec("CREATE SCHEMA fence")
	if err := raw.HSet(ctx, "fence:a:1", "v", "1").Err(); err != nil {
		t.Fatal(err)
	}
	st := liveSchemaStore(t, h)
	ref, ok, err := st.liveSchemaRef(ctx, "fence")
	if err != nil || !ok || !validLiveSchemaID(ref.id) {
		t.Fatal(ref, ok, err)
	}
	// An interrupted DROP SCHEMA leaves its tombstone: registration and
	// rename are refused until the drop is rerun.
	if err := raw.Set(ctx, ref.stateKey(), "dropping", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "fence:a:", Schema: "fence", Table: "a", Apply: true}); err == nil || !strings.Contains(err.Error(), "being dropped") {
		t.Fatalf("registration into a dropping schema: %v", err)
	}
	h.expectError("ALTER SCHEMA fence RENAME TO f2", "being dropped")
	if n := raw.HLen(ctx, ref.registry()).Val(); n != 0 {
		t.Fatal("refused registration left a record", n)
	}
	h.exec("DROP SCHEMA fence")
	h.expectError("DROP SCHEMA fence", "does not exist")
	if raw.Exists(ctx, ref.stateKey()).Val() != 0 {
		t.Fatal("completed drop kept its tombstone")
	}
	// A registrar that resolved the schema before its drop withdraws its
	// reservation: the schema ID is no longer mapped.
	meta := &tableMeta{Name: "late", Schema: "fence", LiveSchemaID: ref.id, Live: true, Adopted: true, KeyRows: true, KeyPrefix: "fence:a:", IndexName: liveIndexPrefix + snapshotID()}
	if _, err := st.reserveLive(ctx, meta); err == nil || !strings.Contains(err.Error(), "dropped during registration") {
		t.Fatalf("stale reservation: %v", err)
	}
	if raw.HExists(ctx, ref.registry(), "late").Val() {
		t.Fatal("stale reservation was not withdrawn")
	}
	if raw.Exists(ctx, "fence:a:1").Val() != 1 {
		t.Fatal("registration fencing changed a source HASH")
	}
}

func TestLiveSchemaArrowStagedSwap(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx := h.ctx
	h.exec("CREATE SCHEMA IF NOT EXISTS lake")
	create, _ := unmanagedArrowInput(t, h, "lake", adbc.OptionValueIngestModeCreate, []string{"lake:v1:one"}, []int64{1})
	setLiveSource(t, h, create, unmanagedIngestSource{Prefix: "lake:v1:"})
	if n, err := create.ExecuteUpdate(ctx); n != 1 || err != nil {
		t.Fatalf("create %d %v", n, err)
	}
	appendRows, _ := unmanagedArrowInput(t, h, "lake", adbc.OptionValueIngestModeCreateAppend, []string{"lake:v1:two"}, []int64{2})
	if n, err := appendRows.ExecuteUpdate(ctx); n != 1 || err != nil {
		t.Fatalf("append %d %v", n, err)
	}
	h.expectRows("SELECT n FROM lake.live ORDER BY n", "1", "2")
	st := liveSchemaStore(t, h)
	ref, ok, err := st.liveSchemaRef(ctx, "lake")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, record, err := st.readLiveRecord(ctx, ref, "live")
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.LiveSchemaID != ref.id || record.Meta.Schema != "lake" {
		t.Fatal("named live record has the wrong schema identity", record.Meta.Schema, record.Meta.LiveSchemaID)
	}
	stored := raw.HGet(ctx, ref.registry(), "live").Val()
	if !strings.Contains(stored, `"live_schema_id":"`+ref.id+`"`) || strings.Contains(stored, `"schema":"lake"`) {
		t.Fatal("named live record stored its schema name instead of its schema ID")
	}

	// Dagster-style swap: stage under a fresh prefix, clear and unregister the
	// old target, then rename the stage. DROP TABLE alone keeps source HASHes.
	stage, _ := unmanagedArrowInput(t, h, "lake", adbc.OptionValueIngestModeCreate, []string{"lake:v2:one"}, []int64{10})
	if err := stage.SetOption(ctx, adbc.OptionKeyIngestTargetTable, "stage"); err != nil {
		t.Fatal(err)
	}
	setLiveSource(t, h, stage, unmanagedIngestSource{Prefix: "lake:v2:"})
	if n, err := stage.ExecuteUpdate(ctx); n != 1 || err != nil {
		t.Fatalf("stage %d %v", n, err)
	}
	h.exec("DELETE FROM lake.live")
	h.exec("DROP TABLE IF EXISTS lake.live")
	h.exec("ALTER TABLE lake.stage RENAME TO live")
	h.expectRows("SELECT __key, n FROM lake.live", "lake:v2:one|10")
	if raw.Exists(ctx, "lake:v1:one", "lake:v1:two").Val() != 0 || raw.Exists(ctx, "lake:v2:one").Val() != 1 {
		t.Fatal("staged swap left the wrong source HASHes")
	}
	again, _ := unmanagedArrowInput(t, h, "lake", adbc.OptionValueIngestModeAppend, []string{"lake:v2:two"}, []int64{11})
	if n, err := again.ExecuteUpdate(ctx); n != 1 || err != nil {
		t.Fatalf("append after rename %d %v", n, err)
	}
	h.expectRows("SELECT n FROM lake.live ORDER BY n", "10", "11")

	// A managed Arrow target may not be created in a live schema.
	setConnOption(t, h, OptionStringLiveTables, "false")
	managed, _ := unmanagedArrowInput(t, h, "lake", adbc.OptionValueIngestModeCreate, []string{"x"}, []int64{1})
	if err := managed.SetOption(ctx, adbc.OptionKeyIngestTargetTable, "managed_copy"); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.ExecuteUpdate(ctx); err == nil || !strings.Contains(err.Error(), "holds live tables") {
		t.Fatalf("managed Arrow creation in a live schema: %v", err)
	}
	setConnOption(t, h, OptionStringLiveTables, "true")
	h.exec("DROP SCHEMA lake CASCADE")
	if raw.Exists(ctx, "lake:v2:one", "lake:v2:two").Val() != 2 {
		t.Fatal("DROP SCHEMA CASCADE removed source HASHes")
	}
}

func TestLiveSchemaPartitions(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx := h.ctx
	h.exec("CREATE SCHEMA part")
	want := []string{}
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		key := "part:items:" + n
		if err := raw.HSet(ctx, key, "n", n, "v", "row-"+n).Err(); err != nil {
			t.Fatal(err)
		}
		want = append(want, key+"|"+n+"|row-"+n)
	}
	if _, err := RegisterLiveHashes(ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Prefix: "part:items:", Schema: "part", Table: "items", Types: map[string]string{"n": "BIGINT", "v": "VARCHAR"}, Apply: true}); err != nil {
		t.Fatal(err)
	}
	f := &hashFixture{h: h, raw: raw, opts: unmanagedArrowDatabaseOptions(), prefix: "part:items:"}
	parts := planLiveHashPartitions(t, f, "SELECT CAST(__key AS BYTEA),n,v FROM part.items", 3, "")
	d, err := decodeLiveHashPartition(parts.PartitionIDs[0])
	if err != nil || d.Schema != "part" {
		t.Fatal(d.Schema, err)
	}
	worker := liveHashWorker(t, f)
	var got []string
	for _, id := range parts.PartitionIDs {
		rows, err := readLiveHashPart(ctx, worker, id)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, rows...)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("partition rows %v, want %v", got, want)
	}
	// Descriptors resolve their schema under the retained producer catalog pin
	// and identify the registration by schema ID, so a schema rename (like
	// other in-flight live statements) keeps them readable. Unregistering does not.
	h.exec("ALTER SCHEMA part RENAME TO part2")
	if rows, err := readLiveHashPart(ctx, worker, parts.PartitionIDs[0]); err != nil {
		t.Fatal("schema rename broke a retained descriptor:", err)
	} else if len(rows) > len(want) {
		t.Fatal(rows)
	}
	h.exec("DROP SCHEMA part2 CASCADE")
	if _, err := readLiveHashPart(ctx, worker, parts.PartitionIDs[0]); err == nil {
		t.Fatal("a descriptor read an unregistered live table")
	}
	if raw.Exists(ctx, "part:items:1").Val() != 1 {
		t.Fatal("DROP SCHEMA CASCADE removed source HASHes")
	}
}

func TestLiveSchemaEvolution(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx, opts := h.ctx, unmanagedArrowDatabaseOptions()
	h.exec("CREATE SCHEMA evo")
	if err := raw.HSet(ctx, "evo:items:1", "n", "1", "label", "one", "extra", "keep").Err(); err != nil {
		t.Fatal(err)
	}
	reg, err := RegisterLiveHashes(ctx, opts, HashAdoptOptions{Prefix: "evo:items:", Schema: "evo", Table: "items", Types: map[string]string{"n": "BIGINT"}, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER SCHEMA evo RENAME TO evo2")
	o := LiveHashEvolutionOptions{HashAdoptOptions: HashAdoptOptions{Prefix: "evo:items:", Schema: "evo2", Table: "items", IndexColumns: []string{"label"}}, Columns: []string{"n", "label"}, ExpectedIndex: reg.Index, ExclusiveQuiescent: true}
	o.Apply = true
	next, err := EvolveLiveHashes(ctx, opts, o)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Applied || next.Table != "evo2.items" || next.Index == reg.Index {
		t.Fatalf("evolution %+v", next)
	}
	expectIndexGone(t, raw, reg.Index)
	h.expectRows("SELECT n, label FROM evo2.items", "1|one")
	st := liveSchemaStore(t, h)
	ref, ok, err := st.liveSchemaRef(ctx, "evo2")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, record, err := st.readLiveRecord(ctx, ref, "items")
	if err != nil || record.State != "ready" || record.Pending != nil || record.Meta.LiveSchemaID != ref.id || record.Meta.index() != next.Index {
		t.Fatal("evolved record lost its schema identity", err)
	}
	if raw.HExists(ctx, liveRegistry, "items").Val() {
		t.Fatal("named evolution wrote the redis_live registry")
	}
	if raw.HGet(ctx, "evo:items:1", "extra").Val() != "keep" {
		t.Fatal("evolution changed the source HASH")
	}
	h.exec("DROP SCHEMA evo2 CASCADE")
	expectIndexGone(t, raw, next.Index)
}

func TestLiveSchemaCreateTable(t *testing.T) {
	h, raw := liveSchemaHarness(t)
	ctx := h.ctx
	h.exec("CREATE SCHEMA app")
	h.exec("CREATE TABLE app.users (name TEXT, age BIGINT, note TEXT NOINDEX COMMENT 'free text', ok BOOLEAN) COMMENT 'people'")
	h.exec("CREATE TABLE IF NOT EXISTS app.users (x INTEGER)")
	h.expectError("CREATE TABLE app.users (x INTEGER)", "already exists")
	h.expectRows("SELECT column_name FROM information_schema.columns WHERE table_schema='app' AND table_name='users' AND column_name <> '__key' ORDER BY ordinal_position", "name", "age", "note", "ok")

	rows, _ := h.query("INSERT INTO app.users (name, age, note) VALUES ('Ada', 37, 'n') RETURNING __key")
	if len(rows) != 1 || !strings.HasPrefix(rows[0], "app:users:") {
		t.Fatal(rows)
	}
	if got := raw.HGetAll(ctx, rows[0]).Val(); len(got) != 3 || got["name"] != "Ada" || got["age"] != "37" || got["note"] != "n" {
		t.Fatalf("SQL-created live row %v", got)
	}
	if err := raw.HSet(ctx, "app:users:direct", "name", "Bo", "age", "5").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT name FROM app.users WHERE age = 5", "Bo")
	st := liveSchemaStore(t, h)
	ref, ok, err := st.liveSchemaRef(ctx, "app")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	meta, err := st.liveTable(ctx, ref, "users")
	if err != nil {
		t.Fatal(err)
	}
	if meta.prefix() != "app:users:" || meta.Comment != "people" || meta.Columns[2].Indexed || !meta.Columns[1].Indexed {
		t.Fatalf("SQL-created live descriptor %+v", meta)
	}
	info := indexInfoOf(raw.Do(ctx, "FT.INFO", meta.index()).Val())
	for _, a := range info.attrs {
		if a.identifier == "note" {
			t.Fatal("NOINDEX column was indexed")
		}
	}

	for sql, want := range map[string]string{
		"CREATE TABLE app.c1 (a INTEGER NOT NULL)":                "cannot be enforced",
		"CREATE TABLE app.c2 (a INTEGER DEFAULT 1)":               "cannot be enforced",
		"CREATE TABLE app.c3 (a INTEGER PRIMARY KEY)":             "cannot be enforced",
		"CREATE TABLE app.c4 (a INTEGER, CHECK (a > 0))":          "cannot be enforced",
		"CREATE TABLE app.c5 (id SERIAL)":                         "cannot be enforced",
		"CREATE TABLE app.c6 (__x INTEGER)":                       "reserved",
		"CREATE TABLE app.c7 (a INTEGER) WITH (fillfactor=7)":     "unsupported live table storage parameter",
		"CREATE TABLE app.c8 (a INTEGER) WITH (prefix='adbc:x:')": "outside driver",
	} {
		h.expectError(sql, want)
	}
	h.expectRows("SELECT count(*) FROM information_schema.tables WHERE table_schema='app'", "1")

	// Explicit, occupied and covered prefixes.
	h.exec("CREATE TABLE app.events (kind TEXT) WITH (prefix = 'ev:')")
	if events, _ := h.query("INSERT INTO app.events (kind) VALUES ('click') RETURNING __key"); len(events) != 1 || !strings.HasPrefix(events[0], "ev:") {
		t.Fatal(events)
	}
	if err := raw.HSet(ctx, "busy:1", "v", "1").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError("CREATE TABLE app.busy (v TEXT) WITH (prefix = 'busy:')", "already holds keys")
	h.exec("CREATE TABLE app.empty (v TEXT)")
	h.exec("ALTER TABLE app.empty RENAME TO empty_old")
	h.expectError("CREATE TABLE app.empty (v TEXT)", "already covers live prefix")
	if err := raw.Do(ctx, "FT.CREATE", "app_owned_idx", "ON", "HASH", "PREFIX", 1, "appidx:", "SCHEMA", "v", "TEXT").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError("CREATE TABLE app.covered (v TEXT) WITH (prefix = 'appidx:sub:')", "already covers live prefix")

	// Managed tables keep refusing storage parameters.
	setConnOption(t, h, OptionStringSchemaOwnership, "managed")
	h.exec("CREATE TABLE public.m (a INTEGER)")
	h.exec("INSERT INTO public.m VALUES (1)")
	h.expectError("CREATE TABLE public.m2 (a INTEGER) WITH (prefix = 'x:')", "supported only for live tables")
	h.exec("CREATE TEMP TABLE tmp (a INTEGER)")
	h.exec("DROP TABLE tmp")

	// CREATE TABLE AS reads only live tables and runs its query first.
	if n := h.exec("CREATE TABLE app.adults AS SELECT name, age FROM app.users WHERE age >= 18"); n != 1 {
		t.Fatal("CREATE TABLE AS inserted", n)
	}
	h.expectRows("SELECT name, age FROM app.adults", "Ada|37")
	var keys []string
	iter := raw.Scan(ctx, 0, "app:adults:*", 1000).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil || len(keys) != 1 {
		t.Fatal(keys, err)
	}
	h.expectError("CREATE TABLE app.frommanaged AS SELECT a FROM public.m", "live writes may read only live tables")
	h.expectError("SELECT * FROM app.frommanaged", "does not exist")
	h.expectError("CREATE TABLE app.nulls AS SELECT NULL AS a", "only NULL values")
	h.expectError("SELECT * FROM app.nulls", "does not exist")
	if n := h.exec("CREATE TABLE app.consts WITH (prefix = 'consts:') AS SELECT 1 AS one, 'x' AS s"); n != 1 {
		t.Fatal("constant CREATE TABLE AS inserted", n)
	}
	h.expectRows("SELECT one, s FROM app.consts", "1|x")
	h.expectRows("SELECT count(*) FROM (SELECT 1 FROM app.consts) q", "1")

	// Arrow appends to a SQL-created table; DROP keeps its HASHes, so the
	// default prefix cannot be reused while they remain.
	h.exec("CREATE TABLE app.nums (n BIGINT)")
	appendRows, _ := unmanagedArrowInput(t, h, "app", adbc.OptionValueIngestModeAppend, []string{"app:nums:a"}, []int64{7})
	if err := appendRows.SetOption(ctx, adbc.OptionKeyIngestTargetTable, "nums"); err != nil {
		t.Fatal(err)
	}
	if n, err := appendRows.ExecuteUpdate(ctx); n != 1 || err != nil {
		t.Fatalf("append %d %v", n, err)
	}
	h.expectRows("SELECT n FROM app.nums", "7")
	h.exec("DROP TABLE app.nums")
	if raw.HGet(ctx, "app:nums:a", "n").Val() != "7" {
		t.Fatal("DROP TABLE removed the SQL-created table's HASH")
	}
	h.expectError("CREATE TABLE app.nums (n BIGINT)", "already holds keys")

	h.exec("CREATE TABLE redis_live.sql_made (v TEXT)")
	if made, _ := h.query("INSERT INTO redis_live.sql_made (v) VALUES ('a') RETURNING __key"); len(made) != 1 || !strings.HasPrefix(made[0], "redis_live:sql_made:") {
		t.Fatal(made)
	}
	h.exec("DROP TABLE redis_live.sql_made")
	h.exec("DROP SCHEMA app CASCADE")
	if raw.Exists(ctx, rows[0], "app:users:direct", "app:nums:a").Val() != 3 {
		t.Fatal("DROP SCHEMA CASCADE removed source HASHes")
	}
}
