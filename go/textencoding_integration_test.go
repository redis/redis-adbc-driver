// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import "testing"

func TestAdoptTextEncoding(t *testing.T) {
	for _, app := range []bool{false, true} {
		t.Run(map[bool]string{false: "own_index", true: "app_index"}[app], func(t *testing.T) {
			f := newHashFixture(t, "it:enc:")
			h := f.h
			f.dropAdopted("it_enc")
			t.Cleanup(func() { f.dropAdopted("it_enc") })
			f.hset(f.prefix+"a", "active", "YES", "created", "2024-01-01T12:00:00Z", "day", "2024-01-01", "clock", "12:00:00")
			f.hset(f.prefix+"b", "active", "false", "created", "2024-01-02 14:00:00+02", "day", "2024-01-02", "clock", "13:00:00")
			o := HashAdoptOptions{Table: "it_enc", Apply: true}
			if app {
				f.appIndex("it_enc_app", "SCHEMA", "active", "TAG", "created", "TAG")
				o.Index = "it_enc_app"
			}
			if !f.adopt(o).Applied {
				t.Fatal("not adopted")
			}
			h.expectRows(`SELECT active, day, clock FROM it_enc ORDER BY created`, "true|2024-01-01|12:00:00.000000", "false|2024-01-02|13:00:00.000000")
			h.expectRows(`SELECT COUNT(*), MIN(created) FROM it_enc WHERE active`, "1|2024-01-01T12:00:00Z")
			h.exec(`UPDATE it_enc SET active=NOT active, created=created+INTERVAL '1 day'`)
			for key, want := range map[string]string{"a": "NO", "b": "true"} {
				if got, _ := f.raw.HGet(h.ctx, f.prefix+key, "active").Result(); got != want {
					t.Errorf("%s active = %s", key, got)
				}
			}
			for key, want := range map[string]string{"a": "2024-01-02T12:00:00Z", "b": "2024-01-03 14:00:00+02"} {
				if got, _ := f.raw.HGet(h.ctx, f.prefix+key, "created").Result(); got != want {
					t.Errorf("%s created = %s, want %s", key, got, want)
				}
			}
			f.hset(f.prefix+"c", "active", "t", "created", "2024-01-04T00:00:00Z")
			h.expectRows(`SELECT COUNT(*) FROM it_enc WHERE active`, "2")
		})
	}
}

func TestSQLForeignIndexDiscovery(t *testing.T) {
	f := newHashFixture(t, "it:foreign:")
	h := f.h
	f.hset(f.prefix+"alice", "name", "Alice", "active", "yes", "n", 2)
	f.hset(f.prefix+"bob", "name", "Bob", "active", "no", "n", 1)
	f.hset(f.prefix+"bob:extra", "name", "Nested", "active", "yes", "n", 3)
	f.appIndex("it_foreign_idx", "FILTER", "@n > 1", "SCHEMA", "name", "TEXT", "active", "TAG", "n", "NUMERIC")
	f.appIndex("it_foreign_idx2", "SCHEMA", "name", "TEXT", "n", "NUMERIC")
	h.expectError(`SELECT * FROM redis_foreign.it_foreign_idx`, "does not exist")
	if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT name,active FROM redis_foreign.it_foreign_idx ORDER BY n`, "Alice|true", "Nested|true")
	h.expectRows(`SELECT COUNT(*) FROM redis_foreign.it_foreign_idx WHERE __key='it:foreign:bob'`, "0")
	h.expectRows(`SELECT table_name FROM information_schema.tables WHERE table_schema='redis_foreign' AND table_name='it_foreign_idx'`, "it_foreign_idx")
	h.expectRows(`SELECT COUNT(DISTINCT c.oid) FROM pg_class c JOIN pg_namespace n ON c.relnamespace=n.oid WHERE n.nspname='redis_foreign' AND c.relkind='r' AND c.relname IN ('it_foreign_idx','it_foreign_idx2')`, "2")
	for _, q := range []string{`UPDATE redis_foreign.it_foreign_idx SET n=0`, `DELETE FROM redis_foreign.it_foreign_idx`, `DROP TABLE redis_foreign.it_foreign_idx`, `CREATE TABLE redis_foreign.x (n integer)`} {
		h.expectError(q, "read-only")
	}
	if n, _ := f.raw.Exists(h.ctx, metaKey(foreignSchema, "it_foreign_idx")).Result(); n != 0 {
		t.Fatal("foreign discovery persisted metadata")
	}
	f.hset(f.prefix+"new", "name", "New", "active", "true", "n", 4)
	h.expectRows(`SELECT COUNT(*) FROM redis_foreign.it_foreign_idx`, "3")
	if err := h.setConnOption(OptionStringForeignIndexes, "false"); err != nil {
		t.Fatal(err)
	}
	h.expectError(`SELECT * FROM redis_foreign.it_foreign_idx`, "does not exist")
}
