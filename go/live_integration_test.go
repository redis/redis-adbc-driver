// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/google/uuid"
)

func TestLiveHashesSQLAndRedis(t *testing.T) {
	f := newHashFixture(t, "it:live:users:")
	h := f.h
	f.hset(f.prefix+"one", "name", "Alice", "age", "30", "extra", "keep")
	f.hset(f.prefix+"one:prefs", "theme", "dark")
	r, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_users", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Applied {
		t.Fatal("not applied")
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_users"); err != nil {
			t.Error(err)
		}
	})
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT __key,name,age FROM redis_live.it_live_users", f.prefix+"one|Alice|30")
	f.hset(f.prefix+"one", "name", "Alicia")
	h.expectRows("SELECT name FROM redis_live.it_live_users WHERE __key='"+f.prefix+"one'", "Alicia")
	h.exec("INSERT INTO redis_live.it_live_users(__key,name,age) VALUES ('" + f.prefix + "two','Bob',40)")
	if v := f.raw.HGet(h.ctx, f.prefix+"two", "name").Val(); v != "Bob" {
		t.Fatalf("Redis sees %q", v)
	}
	h.expectError("INSERT INTO redis_live.it_live_users(__key,name) VALUES ('"+f.prefix+"two','collision')", "already exists")
	h.expectError("INSERT INTO redis_live.it_live_users(__key,name) VALUES (NULL,'null key')", "__key")
	if err := f.raw.Expire(h.ctx, f.prefix+"one", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE redis_live.it_live_users SET name='SQL' WHERE __key='" + f.prefix + "one'")
	if v := f.raw.HGet(h.ctx, f.prefix+"one", "name").Val(); v != "SQL" {
		t.Fatalf("Redis sees %q", v)
	}
	if ttl := f.raw.TTL(h.ctx, f.prefix+"one").Val(); ttl <= 0 {
		t.Fatal("SQL removed TTL")
	}
	f.hset(f.prefix+"one", "unmapped", "preserve")
	h.exec("UPDATE redis_live.it_live_users SET age=31 WHERE __key='" + f.prefix + "one'")
	if f.raw.HGet(h.ctx, f.prefix+"one", "unmapped").Val() != "preserve" {
		t.Fatal("SQL removed unmapped field")
	}
	h.exec("DELETE FROM redis_live.it_live_users WHERE __key='" + f.prefix + "two'")
	if f.raw.Exists(h.ctx, f.prefix+"two").Val() != 0 {
		t.Fatal("SQL DELETE did not delete source")
	}
	f.hset(f.prefix+"three", "name", "Carol", "age", "50")
	h.expectRows("SELECT name FROM redis_live.it_live_users WHERE age >= 40", "Carol")
	f.hset(f.prefix+"nul\x00key", "name", "Binary", "age", "60")
	h.expectRows("SELECT __key,name FROM redis_live.it_live_users WHERE age=60", f.prefix+"nul\x00key|Binary")
	h.exec("UPDATE redis_live.it_live_users SET age=61 WHERE __key='" + f.prefix + "nul\x00key'")
	h.expectRows("SELECT name,age FROM redis_live.it_live_users WHERE age>=60", "Binary|61")
	h.expectRows("SELECT table_name FROM information_schema.tables WHERE table_schema='redis_live'", "it_live_users")
	h.expectError("TRUNCATE redis_live.it_live_users", "live")
	h.expectError("DROP TABLE redis_live.it_live_users", "read-only")
}

func TestLiveGeneratedInsertKeys(t *testing.T) {
	f := newHashFixture(t, "it:live:gen'\\key:")
	h := f.h
	f.hset(f.prefix+"seed", "v", "0")
	if _, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_generated", Apply: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_generated"); err != nil {
			t.Error(err)
		}
	})
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	check := func(query string, values ...string) {
		t.Helper()
		rows, _ := h.query(query)
		if len(rows) != len(values) {
			t.Fatalf("got %d generated rows, want %d", len(rows), len(values))
		}
		for i, row := range rows {
			key, value, ok := strings.Cut(row, "|")
			if !ok || value != values[i] {
				t.Fatalf("unexpected RETURNING row %q", row)
			}
			suffix, ok := strings.CutPrefix(key, f.prefix)
			id, err := uuid.Parse(suffix)
			if !ok || err != nil || id.Version() != 4 || suffix != id.String() {
				t.Fatalf("invalid generated key %q", key)
			}
			if seen[key] {
				t.Fatalf("reused generated key %q", key)
			}
			seen[key] = true
			if got := f.raw.HGet(h.ctx, key, "v").Val(); got != value {
				t.Fatalf("Redis value %q, want %q", got, value)
			}
			for _, field := range []string{keyField, rowIDField} {
				if f.raw.HExists(h.ctx, key, field).Val() {
					t.Fatalf("stored internal field %s", field)
				}
			}
		}
	}
	check("INSERT INTO redis_live.it_live_generated(v) VALUES(1),(2) RETURNING __key,v", "1", "2")
	check("INSERT INTO redis_live.it_live_generated VALUES(3) RETURNING __key,v", "3")
	check("INSERT INTO redis_live.it_live_generated(__key,v) VALUES(DEFAULT,4) RETURNING __key,v", "4")
	check("INSERT INTO redis_live.it_live_generated(v) SELECT v+10 FROM redis_live.it_live_generated WHERE v=1 RETURNING __key,v", "11")
	key := f.prefix + "manual"
	quoted := "'" + strings.ReplaceAll(key, "'", "''") + "'"
	h.expectRows("INSERT INTO redis_live.it_live_generated(__key,v) VALUES("+quoted+",7) RETURNING __key,v", key+"|7")
	h.expectError("INSERT INTO redis_live.it_live_generated(__key,v) VALUES(NULL,8)", "__key")
	h.expectError("INSERT INTO redis_live.it_live_generated(v) VALUES(NULL)", "without fields")
	h.expectRows("SELECT COUNT(*) FROM redis_live.it_live_generated", "7")
}

func TestLiveExactValuesAndSchema(t *testing.T) {
	f := newHashFixture(t, "it:live:exact:")
	h := f.h
	f.hset(f.prefix+"one", "n", "9007199254740993", "v", "  mixed\x1ftext\x00tail")
	if _, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_exact", Apply: true, Types: map[string]string{"n": "BIGINT", "v": "VARCHAR"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_exact"); err != nil {
			t.Error(err)
		}
	})
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT n,v FROM redis_live.it_live_exact WHERE n=9007199254740993", "9007199254740993|  mixed\x1ftext\x00tail")
	h.expectRows("SELECT COUNT(*),SUM(n) FROM redis_live.it_live_exact", "1|9007199254740993")
	h.exec("UPDATE redis_live.it_live_exact SET n=n+1 WHERE __key='" + f.prefix + "one'")
	if got := f.raw.HGet(h.ctx, f.prefix+"one", "n").Val(); got != "9007199254740994" {
		t.Fatalf("rounded SQL write: %s", got)
	}
	h.expectRows("SELECT n FROM redis_live.it_live_exact WHERE n>9007199254740993", "9007199254740994")
}

func TestLiveIsolationAndManagedTables(t *testing.T) {
	f := newHashFixture(t, "it:live:isolation:")
	h := f.h
	f.hset(f.prefix+"one", "n", "10")
	r, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_isolation", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_isolation"); err != nil {
			t.Error(err)
		}
	})
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec("DROP TABLE IF EXISTS it_live_managed")
	h.exec("CREATE TABLE it_live_managed(n INTEGER)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_live_managed") })
	h.exec("INSERT INTO it_live_managed VALUES(10)")
	h.expectRows("SELECT l.n,m.n FROM redis_live.it_live_isolation l JOIN it_live_managed m ON l.n=m.n", "10|10")
	h.expectError("UPDATE redis_live.it_live_isolation SET n=(SELECT n FROM it_live_managed)", "live writes may read only live tables")
	h.expectError("CREATE TABLE it_live_bad AS SELECT * FROM redis_live.it_live_isolation", "managed writes cannot access live tables")
	h.expectError("ALTER TABLE redis_live.it_live_isolation ADD COLUMN bad INTEGER", "live")
	opts := h.conn.(adbc.GetSetOptionsWithContext)
	if err := opts.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
		t.Fatal(err)
	}
	h.expectError("SELECT * FROM redis_live.it_live_isolation", "shared read snapshots")
	if err := opts.SetOption(h.ctx, OptionStringReadSnapshotAction, "release"); err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE it_live_managed SET n=20")
	h.expectRows("SELECT * FROM it_live_managed", "20")
	if _, err := SweepReleased(h.ctx, f.opts, SweepOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	if f.raw.HGet(h.ctx, f.prefix+"one", "n").Val() != "10" {
		t.Fatal("managed sweep changed live HASH")
	}
	if f.raw.Do(h.ctx, "FT.INFO", r.Index).Err() != nil {
		t.Fatal("managed sweep removed live index")
	}
	schema := liveSchema
	if _, err := h.conn.GetTableSchema(h.ctx, nil, &schema, "it_live_isolation"); err != nil {
		t.Fatal(err)
	}
}

func TestLiveIndexFailuresAndRecovery(t *testing.T) {
	f := newHashFixture(t, "it:live:failure:")
	h := f.h
	f.hset(f.prefix+"one", "n", "10")
	r, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_failure", Apply: true, Types: map[string]string{"n": "BIGINT"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_failure"); err != nil {
			t.Error(err)
		}
	})
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT n FROM redis_live.it_live_failure WHERE n=10", "10")
	f.hset(f.prefix+"one", "n", "not-a-number")
	h.expectError("SELECT * FROM redis_live.it_live_failure", "indexing failures")
	// Simulate a create acknowledged by Redis but publication interrupted. A
	// resume observes the existing index rather than issuing another FT.CREATE.
	f.hset(f.prefix+"one", "n", "10")
	if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_failure"); err != nil {
		t.Fatal(err)
	}
	r, err = RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_failure", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	raw := f.raw.HGet(h.ctx, liveRegistry, "it_live_failure").Val()
	var record liveRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatal(err)
	}
	record.State = "building"
	pending, _ := json.Marshal(record)
	if err := f.raw.HSet(h.ctx, liveRegistry, "it_live_failure", pending).Err(); err != nil {
		t.Fatal(err)
	}
	if err := ResumeLiveHashes(h.ctx, f.opts, "it_live_failure"); err != nil {
		t.Fatal(err)
	}
	if f.raw.Do(h.ctx, "FT.INFO", r.Index).Err() != nil {
		t.Fatal("resume replaced its index")
	}
	h.expectRows("SELECT n FROM redis_live.it_live_failure", "10")
}

func TestLiveUpdateMissingRowRefusesResurrection(t *testing.T) {
	f := newHashFixture(t, "it:live:missing:")
	h := f.h
	f.hset(f.prefix+"one", "v", "10")
	if _, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_missing", Apply: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_missing"); err != nil {
			t.Error(err)
		}
	})
	s := &store{client: f.raw, liveTables: true, liveExecution: true}
	meta, err := s.liveTable(context.Background(), "it_live_missing")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.raw.Del(h.ctx, f.prefix+"one").Err(); err != nil {
		t.Fatal(err)
	}
	changes := []rowChange{{key: f.prefix + "one", set: []any{"v", "11"}}}
	if err := keyRowUpdateScript.Load(h.ctx, f.raw).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.updateKeyRows(h.ctx, meta, changes); err == nil {
		t.Fatal("missing UPDATE succeeded")
	}
	if f.raw.Exists(h.ctx, f.prefix+"one").Val() != 0 {
		t.Fatal("UPDATE resurrected deleted HASH")
	}
	if err := f.raw.Set(h.ctx, f.prefix+"one", "replacement string", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteLiveKeys(h.ctx, meta, []string{f.prefix + "one"}); err == nil {
		t.Fatal("DELETE accepted changed key type")
	}
	if f.raw.Get(h.ctx, f.prefix+"one").Val() != "replacement string" {
		t.Fatal("DELETE removed replacement non-HASH")
	}
}

func TestLiveUnregisterPreservesSource(t *testing.T) {
	f := newHashFixture(t, "it:live:remove:")
	f.hset(f.prefix+"one", "v", "1")
	r, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_remove", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := UnregisterLiveHashes(f.h.ctx, f.opts, "it_live_remove"); err != nil {
		t.Fatal(err)
	}
	if f.raw.HGet(f.h.ctx, f.prefix+"one", "v").Val() != "1" {
		t.Fatal("unregister changed source")
	}
	if err := f.raw.Do(f.h.ctx, "FT.INFO", r.Index).Err(); !isUnknownIndex(err) {
		t.Fatalf("index remains: %v", err)
	}
}
