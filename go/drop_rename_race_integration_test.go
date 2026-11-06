// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// DROP and RENAME publish catalog changes atomically. A stale statement must
// conflict instead of following a rename or deleting a replacement relation.

import (
	"fmt"
	"reflect"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Absence belongs to the current manifest; pinned older generations may still
// retain their physical metadata, counters, rows and indexes.
func (h *sqlHarness) expectNoTable(raw goredis.UniversalClient, table string) {
	h.t.Helper()
	h.expectError(fmt.Sprintf("SELECT * FROM %s", table), "does not exist")
	s, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	if ok, err := s.tableExists(ctx, "public", table); err != nil || ok {
		h.t.Errorf("table %q still present in current manifest: %v", table, err)
	}
	if n, err := s.redis(ctx).Exists(ctx, seqKey("public", table)).Result(); err != nil || n != 0 {
		h.t.Errorf("table %q retained a current sequence: %v", table, err)
	}
}

func expectDropRenameConflict(t *testing.T, err error) {
	t.Helper()
	if sqlState(err) != "40001" {
		t.Fatalf("stale DROP/RENAME: %v, want serialization refusal", err)
	}
}

func TestSQLRenameDuringDrop(t *testing.T) {
	for _, phase := range []struct {
		name string
		hook *func()
	}{
		{"after descriptor read", &testHookDropRead},
		{"before private metadata removal", &testHookDropping},
	} {
		for _, variant := range []struct {
			name, drop string
			reuse      bool
		}{
			{"plain", "DROP TABLE it_dr_t", false},
			{"if exists", "DROP TABLE IF EXISTS it_dr_t", false},
			{"replacement under old name", "DROP TABLE it_dr_t", true},
		} {
			t.Run(phase.name+"/"+variant.name, func(t *testing.T) {
				h := newSQLHarness(t)
				h.dropTables("it_dr_t", "it_dr_u")
				raw := h.rawClient()
				other := newSQLHarness(t)
				h.exec("CREATE TABLE it_dr_t (id BIGINT,note VARCHAR)")
				h.exec("INSERT INTO it_dr_t VALUES (1,'a'),(2,'b')")
				prefix, index := h.tableNames(raw, "public", "it_dr_t")
				fired := false
				*phase.hook = func() {
					*phase.hook = nil
					fired = true
					if err := raw.Do(h.ctx, "FT.INFO", index).Err(); err != nil {
						t.Fatal("DROP removed its published source index", err)
					}
					other.exec("ALTER TABLE it_dr_t RENAME TO it_dr_u")
					if variant.reuse {
						other.exec("CREATE TABLE it_dr_t (x INTEGER)")
						other.exec("INSERT INTO it_dr_t VALUES (7)")
					}
					snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
					if h.prefixKeyCount(raw, prefix) != 2 {
						t.Fatal("in-flight DROP lost protected rows")
					}
				}
				t.Cleanup(func() { *phase.hook = nil })
				_, err := tryExec(h.ctx, h.conn, variant.drop)
				*phase.hook = nil
				if !fired {
					t.Fatal("DROP boundary was not exercised")
				}
				expectDropRenameConflict(t, err)
				h.expectRows("SELECT id,note FROM it_dr_u ORDER BY id", "1|a", "2|b")
				if p, i := h.tableNames(raw, "public", "it_dr_u"); p != prefix || i != index {
					t.Fatal("plain rename changed physical identity")
				}
				if variant.reuse {
					h.expectRows("SELECT x,__rowid FROM it_dr_t", "7|1")
				} else {
					h.expectNoTable(raw, "it_dr_t")
				}
				// A fresh, explicitly named DROP retires the renamed relation;
				// the stale original statement must never follow it.
				h.exec("DROP TABLE it_dr_u")
				h.expectNoTable(raw, "it_dr_u")
				h.expectSnapshotReleased(raw, prefix, index)
				if variant.reuse {
					h.expectRows("SELECT x,__rowid FROM it_dr_t", "7|1")
				}
				h.exec("CREATE TABLE it_dr_u (id BIGINT)")
				h.exec("INSERT INTO it_dr_u VALUES (3)")
				h.expectRows("SELECT id,__rowid FROM it_dr_u", "3|1")
			})
		}
	}
}

func TestSQLDropDuringRename(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_dr_t", "it_dr_u")
	raw := h.rawClient()
	other := newSQLHarness(t)
	h.exec("CREATE TABLE it_dr_t (id BIGINT)")
	h.exec("INSERT INTO it_dr_t VALUES (1),(2)")
	prefix, index := h.tableNames(raw, "public", "it_dr_t")
	fired := false
	testHookRenaming = func() {
		testHookRenaming = nil
		fired = true
		other.exec("DROP TABLE it_dr_t")
		snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
		if err := raw.Do(h.ctx, "FT.INFO", index).Err(); err != nil {
			t.Fatal("DROP reclaimed an in-flight rename's source", err)
		}
	}
	t.Cleanup(func() { testHookRenaming = nil })
	_, err := tryExec(h.ctx, h.conn, "ALTER TABLE it_dr_t RENAME TO it_dr_u")
	testHookRenaming = nil
	if !fired {
		t.Fatal("rename boundary was not exercised")
	}
	expectDropRenameConflict(t, err)
	h.expectSnapshotReleased(raw, prefix, index)
	h.expectNoTable(raw, "it_dr_t")
	h.expectNoTable(raw, "it_dr_u")
	h.exec("CREATE TABLE it_dr_u (id BIGINT)")
	h.exec("INSERT INTO it_dr_u VALUES (3)")
	h.expectRows("SELECT id,__rowid FROM it_dr_u", "3|1")
}

func TestSQLRekeyDuringDrop(t *testing.T) {
	for _, phase := range []struct {
		name string
		hook *func()
	}{
		{"after descriptor read", &testHookDropRead},
		{"before private metadata removal", &testHookDropping},
	} {
		t.Run(phase.name, func(t *testing.T) {
			h := newSQLHarness(t)
			h.dropTables("it_dr_rk", "it_dr_rk2")
			raw := h.rawClient()
			rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
			h.exec("CREATE TABLE it_dr_rk (id BIGINT)")
			h.exec("INSERT INTO it_dr_rk VALUES (1),(2)")
			prefix, index := h.tableNames(raw, "public", "it_dr_rk")
			fired := false
			*phase.hook = func() {
				*phase.hook = nil
				fired = true
				rk.exec("ALTER TABLE it_dr_rk RENAME TO it_dr_rk2")
				snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
				if err := raw.Do(h.ctx, "FT.INFO", index).Err(); err != nil {
					t.Fatal("rekey/GC reclaimed the in-flight DROP source", err)
				}
				if h.prefixKeyCount(raw, prefix) != 2 {
					t.Fatal("DROP's pinned rekey input disappeared")
				}
			}
			t.Cleanup(func() { *phase.hook = nil })
			_, err := tryExec(h.ctx, h.conn, "DROP TABLE it_dr_rk")
			*phase.hook = nil
			if !fired {
				t.Fatal("rekey boundary was not exercised")
			}
			expectDropRenameConflict(t, err)
			h.expectNoTable(raw, "it_dr_rk")
			h.expectRows("SELECT id FROM it_dr_rk2 ORDER BY id", "1", "2")
			p, i := h.tableNames(raw, "public", "it_dr_rk2")
			if p == prefix || i == index || snapshotDataID(p) == "" {
				t.Fatal("rekey did not allocate fresh registered storage")
			}
			h.expectSnapshotReleased(raw, prefix, index)
			h.exec("DROP TABLE it_dr_rk2")
			h.expectSnapshotReleased(raw, p, i)
			h.expectNoTable(raw, "it_dr_rk2")
		})
	}
	t.Run("published with old input pinned", func(t *testing.T) {
		h := newSQLHarness(t)
		h.dropTables("it_dr_rk", "it_dr_rk2")
		raw := h.rawClient()
		h.exec("CREATE TABLE it_dr_rk (id BIGINT)")
		h.exec("INSERT INTO it_dr_rk VALUES (1),(2)")
		s, ctx, pin := h.inspectionSnapshot(raw)
		defer pin.release()
		old, err := s.getTable(ctx, "public", "it_dr_rk")
		if err != nil {
			t.Fatal(err)
		}
		keys, before, err := newSnapshotDeltaScan(s, old, nil).next(ctx)
		if err != nil || len(before) != 2 {
			t.Fatal("missing old rekey input", err)
		}
		rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
		rk.exec("ALTER TABLE it_dr_rk RENAME TO it_dr_rk2")
		h.expectError("DROP TABLE it_dr_rk", "does not exist")
		h.exec("DROP TABLE IF EXISTS it_dr_rk")
		h.expectNoTable(raw, "it_dr_rk")
		snapshotReapTest(s, h.ctx)
		after, err := s.fetchSnapshotVisible(ctx, old, keys, nil)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("published rekey invalidated pinned input", err)
		}
		if last, err := s.redis(ctx).Get(ctx, seqKey("public", "it_dr_rk")).Int64(); err != nil || last != 2 {
			t.Fatal("current absence reclaimed the pinned old sequence", last, err)
		}
		h.expectRows("SELECT id FROM it_dr_rk2 ORDER BY id", "1", "2")
		pin.release()
		h.expectSnapshotReleased(raw, old.prefix(), old.index())
	})
}

func TestSQLRenameDuringDropTemp(t *testing.T) {
	h := newSQLHarness(t)
	raw := h.rawClient()
	before := h.tempOwners(raw)
	h.exec("CREATE TEMP TABLE it_dr_tmp (id BIGINT)")
	h.exec("INSERT INTO it_dr_tmp VALUES (1),(2)")
	schema := tempSchemaBase + h.newTempID(raw, before)
	prefix, index := h.tableNames(raw, schema, "it_dr_tmp")
	fired := false
	testHookDropping = func() {
		testHookDropping = nil
		fired = true
		h.exec("ALTER TABLE pg_temp.it_dr_tmp RENAME TO it_dr_tmp2")
	}
	t.Cleanup(func() { testHookDropping = nil })
	_, err := tryExec(h.ctx, h.conn, "DROP TABLE pg_temp.it_dr_tmp")
	testHookDropping = nil
	if !fired {
		t.Fatal("temporary DROP boundary was not exercised")
	}
	expectDropRenameConflict(t, err)
	h.expectError("SELECT * FROM pg_temp.it_dr_tmp", "does not exist")
	h.expectRows("SELECT id FROM pg_temp.it_dr_tmp2 ORDER BY id", "1", "2")
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='pg_temp'", "1")
	h.exec("DROP TABLE pg_temp.it_dr_tmp2")
	h.expectSnapshotReleased(raw, prefix, index)
	s, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	for _, name := range []string{"it_dr_tmp", "it_dr_tmp2"} {
		h.expectError("SELECT * FROM pg_temp."+name, "does not exist")
		if ok, err := s.tableExists(ctx, schema, name); err != nil || ok {
			t.Fatal("temporary descriptor remained", name, err)
		}
		if n, err := s.redis(ctx).Exists(ctx, seqKey(schema, name)).Result(); err != nil || n != 0 {
			t.Fatal("temporary sequence remained", name, err)
		}
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='pg_temp'", "0")
}
