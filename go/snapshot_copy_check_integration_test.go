// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"maps"
	"os"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestSnapshotCopyChecklistPrivateNames(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(fmt.Sprintf("source_overlap_%t", overlap), func(t *testing.T) {
			schema, table, prefix := defaultSchema, "it_hc_private_names", "it:hc:private_names:"
			if overlap {
				schema, table = "it_hc_private_names", "copied"
				prefix = rowPrefix(schema, table) + "app:"
			}
			f := newHashFixture(t, prefix)
			if overlap {
				f.h.exec("CREATE SCHEMA IF NOT EXISTS " + schema)
				t.Cleanup(func() { f.h.exec("DROP SCHEMA " + schema + " CASCADE") })
			}
			qualified := schema + "." + table
			f.h.dropTables(qualified)
			legacy := namesFor(schema, table, 1)
			taken := legacy.prefix + "existing"
			f.hset(taken, "n", "123")
			t.Cleanup(func() { _ = f.raw.Del(f.h.ctx, taken).Err() })
			f.hset(prefix+"1", "n", "9007199254740993", "label", "one")
			f.hset(prefix+"2", "label", "two")
			const sourceIndex = "it_hc_private_names_source"
			f.appIndex(sourceIndex, "SCHEMA", "n", "NUMERIC")
			before := map[string]map[string]string{}
			for _, key := range []string{taken, prefix + "1", prefix + "2"} {
				fields, err := f.raw.HGetAll(f.h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				before[key] = fields
			}
			cp := checklist(t, f.inspect(HashInspectOptions{Schema: schema, Table: table}), ChecklistSQLCopy)
			if !cp.Ready || cp.Command == "" {
				t.Errorf("private copy incorrectly blocked: %+v", cp)
			}
			result, err := CopyHashes(f.h.ctx, f.opts, HashCopyOptions{Prefix: prefix, Schema: schema, Table: table, KeyColumn: "_key"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Rows != 2 {
				t.Fatal("copy row count", result.Rows)
			}
			f.h.expectRows("SELECT n,label,_key FROM "+qualified+" ORDER BY label", "9007199254740993|one|"+prefix+"1", "NULL|two|"+prefix+"2")
			meta := f.h.inspectTable(f.raw, schema, table)
			if snapshotRowPrefix(meta.prefix()) != meta.prefix() || meta.prefix() == legacy.prefix || prefixesOverlap(meta.prefix(), prefix) || meta.index() == sourceIndex {
				t.Fatal("copy claimed application or legacy storage")
			}
			f.h.exec("DROP TABLE " + qualified)
			f.h.expectSnapshotReleased(f.raw, meta.prefix(), meta.index())
			for key, want := range before {
				got, err := f.raw.HGetAll(f.h.ctx, key).Result()
				if err != nil || !maps.Equal(got, want) {
					t.Fatal("copy/drop changed an external HASH", err)
				}
			}
			if err := f.raw.Do(f.h.ctx, "FT.INFO", sourceIndex).Err(); err != nil {
				t.Fatal("copy/drop removed source index", err)
			}
		})
	}
}

// Explicitly disposable fixture: InspectHashes can be the first utility on a
// database, before any driver connection has created its snapshot catalog.
func TestSnapshotCopyChecklistBeforeCatalogCreation(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	if err := raw.FlushDB(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	const table = "it_hc_first_copy"
	prefix := rowPrefix(defaultSchema, table) + "app:"
	uri := os.Getenv("REDIS_SNAPSHOT_URI")
	opts := retainedTestDatabaseOptions(map[string]string{adbc.OptionKeyURI: uri})
	f := &hashFixture{h: h, raw: raw, opts: opts, prefix: prefix}
	f.hset(prefix+"1", "n", "9007199254740993")
	r, err := InspectHashes(h.ctx, opts, HashInspectOptions{Prefix: prefix, Table: table})
	if err != nil {
		t.Fatal(err)
	}
	cp := checklist(t, r, ChecklistSQLCopy)
	if !cp.Ready || cp.Command == "" {
		t.Fatal("first private copy incorrectly blocked", cp)
	}
	if n, err := raw.Exists(h.ctx, snapshotCatalogKey).Result(); err != nil || n != 0 {
		t.Fatal("inspection created a catalog", n, err)
	}
	result, err := CopyHashes(h.ctx, opts, HashCopyOptions{Prefix: prefix, Table: table})
	if err != nil || result.Rows != 1 {
		t.Fatal("first copy failed", result, err)
	}
	current := openWith(t, uri, opts)
	current.expectRows("SELECT n FROM "+table, "9007199254740993")
	current.exec("DROP TABLE " + table)
	if n, err := raw.Exists(h.ctx, snapshotCatalogKey).Result(); err != nil || n != 1 {
		t.Fatal("copy did not initialize snapshots", n, err)
	}
	fields, err := raw.HGetAll(h.ctx, prefix+"1").Result()
	if err != nil || len(fields) != 1 || fields["n"] != "9007199254740993" {
		t.Fatal("first copy changed its source", err)
	}
}
