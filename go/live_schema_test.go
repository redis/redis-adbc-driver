// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func parseOneStmt(src string) (ParsedStmt, error) {
	stmts, err := ParseScript(src)
	if err != nil || len(stmts) != 1 {
		return ParsedStmt{}, err
	}
	return stmts[0], nil
}

func TestParseAlterSchemaRename(t *testing.T) {
	ps, err := parseOneStmt(`ALTER SCHEMA "a b" RENAME TO c`)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := ps.Stmt.(*AlterSchemaStmt)
	if !ok || st.Name != "a b" || st.NewName != "c" {
		t.Fatalf("%#v", ps.Stmt)
	}
	if _, err := parseOneStmt(`ALTER SCHEMA a OWNER TO someone`); err == nil || !strings.Contains(err.Error(), "only RENAME TO") {
		t.Fatalf("ALTER SCHEMA OWNER TO: %v", err)
	}
	if got := usageKind(st); got != "ddl" {
		t.Fatalf("usage kind %q", got)
	}
}

func TestValidLiveSchemaName(t *testing.T) {
	for _, name := range []string{"app", "Ünïcode", strings.Repeat("x", 1024)} {
		if err := validLiveSchemaName(name); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
	nul := string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(nul, 0) {
		t.Fatal("fixture has no zero byte")
	}
	for _, name := range []string{"", nul, string([]byte{0xff}), strings.Repeat("x", 1025), defaultSchema, liveSchema, foreignSchema, pgCatalog, infoSchema, "pg_temp", tempSchemaBase + "x", catalogSchema("other", "s")} {
		if err := validLiveSchemaName(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestLiveRecordSchemaIdentity(t *testing.T) {
	id := snapshotID()
	if !validLiveSchemaID(id) || validLiveSchemaID("ABC") || validLiveSchemaID(strings.Repeat("g", 32)) {
		t.Fatal("schema ID validation")
	}
	ref := liveRef{schema: "app", id: id}
	if ref.registry() == liveRegistry || !strings.HasPrefix(ref.registry(), livePrefix) || !strings.HasPrefix(ref.stateKey(), livePrefix) {
		t.Fatal("named registries must stay in the live control namespace", ref.registry(), ref.stateKey())
	}
	if (liveRef{schema: liveSchema}).registry() != liveRegistry {
		t.Fatal("redis_live must keep its original registry")
	}
	meta := &tableMeta{Name: "t", Schema: "app", LiveSchemaID: id, Live: true}
	pending := &tableMeta{Name: "t", Schema: "app", LiveSchemaID: id, Live: true}
	raw, err := marshalLiveRecord(liveRecord{State: "evolving", Meta: meta, Pending: pending})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "app" || pending.Schema != "app" {
		t.Fatal("encoding mutated in-memory descriptors")
	}
	var stored liveRecord
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Meta.Schema != "" || stored.Pending.Schema != "" || stored.Meta.LiveSchemaID != id {
		t.Fatalf("named record stored schema %q / %q", stored.Meta.Schema, stored.Pending.Schema)
	}
	renamed := stored
	if !renamed.resolveSchema(liveRef{schema: "renamed", id: id}) || renamed.Meta.Schema != "renamed" || renamed.Pending.Schema != "renamed" {
		t.Fatal("a schema rename must resolve stored records by ID")
	}
	var other liveRecord
	_ = json.Unmarshal(raw, &other)
	if other.resolveSchema(liveRef{schema: "app", id: snapshotID()}) || other.resolveSchema(liveRef{schema: liveSchema}) {
		t.Fatal("a record was accepted under another schema ID")
	}
	legacy := &tableMeta{Name: "t", Schema: liveSchema, Live: true}
	raw, err = marshalLiveRecord(liveRecord{State: "ready", Meta: legacy})
	if err != nil {
		t.Fatal(err)
	}
	var old liveRecord
	_ = json.Unmarshal(raw, &old)
	if old.Meta.Schema != liveSchema || strings.Contains(string(raw), "live_schema_id") || !old.resolveSchema(liveRef{schema: liveSchema}) {
		t.Fatal("redis_live records must keep their original format", string(raw))
	}
	if old.resolveSchema(ref) {
		t.Fatal("a redis_live record was accepted in a named schema")
	}
}

func TestSchemaOwnershipOption(t *testing.T) {
	for value, live := range map[string]bool{"managed": false, "live": true} {
		got, err := parseSchemaOwnership(value)
		if err != nil || got != live || schemaOwnershipValue(got) != value {
			t.Fatal(value, got, err)
		}
	}
	for _, value := range []string{"", "LIVE", "true"} {
		if _, err := parseSchemaOwnership(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	ctx := context.Background()
	d := &databaseImpl{}
	if v, err := d.GetOption(ctx, OptionStringSchemaOwnership); err != nil || v != "managed" {
		t.Fatal(v, err)
	}
	if err := d.SetOption(ctx, OptionStringSchemaOwnership, "live"); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.GetOption(ctx, OptionStringSchemaOwnership); v != "live" {
		t.Fatal(v)
	}
	if err := d.SetOption(ctx, OptionStringSchemaOwnership, "both"); err == nil {
		t.Fatal("accepted an invalid ownership")
	}
}

func TestParseCreateTableStorageOptions(t *testing.T) {
	ps, err := parseOneStmt(`CREATE TABLE s.t (a INTEGER, b TEXT NOINDEX) WITH (prefix = 'app:t:', Other) COMMENT 'c'`)
	if err != nil {
		t.Fatal(err)
	}
	st := ps.Stmt.(*CreateTableStmt)
	if len(st.Options) != 2 || st.Options[0] != (StorageOption{Name: "prefix", Value: "app:t:"}) || st.Options[1] != (StorageOption{Name: "other"}) || st.Comment != "c" || !st.Columns[1].NoIndex {
		t.Fatalf("%+v", st)
	}
	ps, err = parseOneStmt(`CREATE TABLE s.t WITH (prefix = 'x:') AS SELECT 1 AS a`)
	if err != nil {
		t.Fatal(err)
	}
	if st := ps.Stmt.(*CreateTableStmt); st.AsSelect == nil || len(st.Options) != 1 || st.Options[0].Value != "x:" {
		t.Fatalf("%+v", st)
	}
	// A CTE after AS is not a storage parameter list.
	ps, err = parseOneStmt(`CREATE TABLE s.t AS WITH q AS (SELECT 1 AS a) SELECT a FROM q`)
	if err != nil {
		t.Fatal(err)
	}
	if st := ps.Stmt.(*CreateTableStmt); st.AsSelect == nil || len(st.Options) != 0 {
		t.Fatalf("%+v", st)
	}
	for _, bad := range []string{`CREATE TABLE s.t WITH (prefix = 'x:')`, `CREATE TABLE s.t (a INTEGER) WITH (prefix = )`, `CREATE TABLE s.t (a INTEGER) WITH prefix`} {
		if _, err := parseOneStmt(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestLiveCreatePrefix(t *testing.T) {
	if got := defaultLivePrefix("app", "users"); got != "app:users:" {
		t.Fatal(got)
	}
	// Escaped parts never add another ':' and keep adbc outside driver storage.
	if got := defaultLivePrefix("a:b", "c*d"); got != "a%3Ab:c%2Ad:" {
		t.Fatal(got)
	}
	if got := defaultLivePrefix("adbc", "t"); got != "%61dbc:t:" || validLivePrefix(got) != nil {
		t.Fatal(got)
	}
	if got, err := liveCreatePrefix("app", "t", []StorageOption{{Name: "prefix", Value: "ev:"}}); err != nil || got != "ev:" {
		t.Fatal(got, err)
	}
	for _, opts := range [][]StorageOption{
		{{Name: "fillfactor", Value: "70"}},
		{{Name: "prefix", Value: "a:"}, {Name: "prefix", Value: "b:"}},
		{{Name: "prefix", Value: "adbc:x:"}},
		{{Name: "prefix", Value: "x{meta}:"}},
		{{Name: "prefix"}},
	} {
		if _, err := liveCreatePrefix("app", "t", opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
}

func TestNewLiveTableMeta(t *testing.T) {
	ref := liveRef{schema: "app", id: snapshotID()}
	meta, err := newLiveTableMeta(ref, "t", "app:t:", "c", []columnMeta{{Name: "n", Type: typeInt64}, {Name: "s", Type: typeString}, {Name: "f", Type: typeFloat64}}, map[string]bool{"s": true})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Live || !meta.Adopted || !meta.KeyRows || meta.LiveSchemaID != ref.id || meta.Schema != "app" || !strings.HasPrefix(meta.IndexName, liveIndexPrefix) || meta.LiveVersion == "" {
		t.Fatalf("%+v", meta)
	}
	for _, c := range meta.Columns {
		if !c.Nullable {
			t.Fatal("live columns must be nullable", c.Name)
		}
	}
	if !meta.Columns[0].Indexed || meta.Columns[1].Indexed || !meta.Columns[2].NaNs {
		t.Fatalf("index policy %+v", meta.Columns)
	}
	for _, cols := range [][]columnMeta{nil, {{Name: "__x", Type: typeInt64}}, {{Name: "a", Type: typeInt64}, {Name: "a", Type: typeInt64}}} {
		if _, err := newLiveTableMeta(ref, "t", "app:t:", "", cols, nil); err == nil {
			t.Fatalf("accepted %+v", cols)
		}
	}
}
