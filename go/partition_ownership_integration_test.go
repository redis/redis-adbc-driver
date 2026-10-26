// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Rejected layouts must not become implicitly adopted/migrated merely because
// a coordinator plans a query or a worker presents a syntactically valid token.
func TestPartitionUnsupportedLayoutOwnership(t *testing.T) {
	for _, mode := range []string{"adopted", "application_index", "foreign_index"} {
		t.Run(mode, func(t *testing.T) {
			f := newHashFixture(t, "it:partition:ownership:")
			h := f.h
			const table = "it_partition_ownership"
			f.dropAdopted(table)
			t.Cleanup(func() { f.dropAdopted(table) })
			keys := []string{f.prefix + "alice", f.prefix + "00001", f.prefix + "byte\x00\xff"}
			for i, key := range keys {
				f.hset(key, "name", "exact\x00\xff", "n", i)
			}
			index, schema, name := "it_partition_app_index", defaultSchema, table
			if mode != "adopted" {
				f.appIndex(index, "SCHEMA", "name", "TAG", "n", "NUMERIC")
			}
			if mode == "foreign_index" {
				if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
				schema, name = foreignSchema, index
			} else {
				opts := HashAdoptOptions{Table: table, Apply: true}
				if mode == "application_index" {
					opts.Index = index
				}
				if !f.adopt(opts).Applied {
					t.Fatal("fixture adoption was not applied")
				}
			}
			st := &store{client: f.raw}
			if mode == "adopted" {
				meta, err := st.getTable(h.ctx, schema, name)
				if err != nil {
					t.Fatal(err)
				}
				index = meta.index()
			}
			before := make(map[string]map[string]string)
			for _, key := range keys {
				value, err := f.raw.HGetAll(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				before[key] = value
			}
			metaBefore, metaErr := f.raw.Get(h.ctx, metaKey(schema, name)).Result()
			infoBefore, err := st.searchDo(h.ctx, index, "FT.INFO", index).Result()
			if err != nil {
				t.Fatal(err)
			}
			definition := indexInfoOf(infoBefore)
			ownedBefore, err := f.raw.SIsMember(h.ctx, indexesKey, index).Result()
			if err != nil {
				t.Fatal(err)
			}
			query := "SELECT * FROM " + schema + "." + name
			for range 3 {
				statement, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := statement.SetSqlQuery(h.ctx, query); err != nil {
					t.Fatal(err)
				}
				_, parts, _, planErr := statement.ExecutePartitions(h.ctx)
				if err := statement.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
				if !isStatus(planErr, adbc.StatusNotImplemented) || parts.NumPartitions != 0 {
					t.Fatalf("unsupported layout published work: %v %+v", planErr, parts)
				}
				descriptor, _ := json.Marshal(partitionID{Version: 1, Query: query,
					Schema: schema, Table: name, Target: "0123456789012345678901234567890123456789012345678901234567890123",
					Zone: "UTC", Lo: 1, Hi: 1})
				reader, err := h.conn.ReadPartition(h.ctx, descriptor)
				if reader != nil {
					reader.Release()
					t.Fatal("unsupported layout returned a reader")
				}
				if !isStatus(err, adbc.StatusNotImplemented) {
					t.Fatal(err)
				}
			}
			for key, want := range before {
				got, err := f.raw.HGetAll(h.ctx, key).Result()
				if err != nil || !maps.Equal(got, want) {
					t.Fatalf("application HASH mutated: %q: %v", key, err)
				}
			}
			metaAfter, err := f.raw.Get(h.ctx, metaKey(schema, name)).Result()
			if err != metaErr || metaAfter != metaBefore {
				t.Fatal("partition operations rewrote or created layout metadata")
			}
			infoAfter, err := st.searchDo(h.ctx, index, "FT.INFO", index).Result()
			if err != nil {
				t.Fatal(err)
			}
			after := indexInfoOf(infoAfter)
			if definition.keyType != after.keyType || definition.filter != after.filter ||
				!reflect.DeepEqual(definition.prefixes, after.prefixes) || !reflect.DeepEqual(definition.attrs, after.attrs) {
				t.Fatal("partition operations changed the application index definition")
			}
			ownedAfter, err := f.raw.SIsMember(h.ctx, indexesKey, index).Result()
			if err != nil || ownedAfter != ownedBefore {
				t.Fatal("partition operations changed index ownership")
			}
		})
	}
}

func TestPartitionLegacyMetadataNotMigrated(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_legacy_guard"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	raw := h.rawClient()
	key := metaKey(defaultSchema, table)
	original, err := raw.Get(h.ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Set(h.ctx, key, original, 0).Err(); err != nil {
			t.Error(err)
		}
	})
	var meta tableMeta
	if err := json.Unmarshal([]byte(original), &meta); err != nil {
		t.Fatal(err)
	}
	meta.KeyPrefix = "" // Legacy metadata has no persistent unique physical prefix.
	legacy, _ := json.Marshal(meta)
	if err := raw.Set(h.ctx, key, legacy, 0).Err(); err != nil {
		t.Fatal(err)
	}
	statement, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetSqlQuery(h.ctx, "SELECT id FROM "+table); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := statement.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	if got, err := raw.Get(h.ctx, key).Result(); err != nil || got != string(legacy) {
		t.Fatal("partition planning implicitly migrated legacy metadata")
	}
}
