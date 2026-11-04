// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func partitionIndexDefinitionEqual(a, b indexInfo) bool {
	return a.keyType == b.keyType && a.filter == b.filter &&
		reflect.DeepEqual(a.prefixes, b.prefixes) && reflect.DeepEqual(a.attrs, b.attrs)
}

// Managed imports support live partitions without transferring ownership of
// application HASHes/indexes. Refresh publishes a new private copy explicitly.
func TestPartitionManagedCopyOwnership(t *testing.T) {
	for _, mode := range []string{"adopted", "application_index", "foreign_index"} {
		t.Run(mode, func(t *testing.T) {
			f := newHashFixture(t, "it:partition:ownership:")
			h := f.h
			const table, sourceIndex = "it_partition_ownership", "it_partition_app_index"
			f.dropAdopted(table)
			t.Cleanup(func() { f.dropAdopted(table) })
			keys := []string{f.prefix + "alice", f.prefix + "00001", f.prefix + "byte\x00\xff"}
			const exact = int64(9007199254740993)
			const rawName = "exact\x00\xff"
			for i, key := range keys {
				f.hset(key, "name", rawName, "n", i, "exact", exact+int64(i))
				if i == 0 {
					f.hset(key, "nullable", "present")
				}
			}
			schema, name := defaultSchema, table
			options := HashAdoptOptions{Table: table, Apply: true}
			if mode != "adopted" {
				f.appIndex(sourceIndex, "SCHEMA", "name", "TAG", "n", "NUMERIC", "exact", "NUMERIC")
			}
			if mode == "foreign_index" {
				if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
				schema, name = foreignSchema, sourceIndex
			} else {
				if mode == "application_index" {
					options.Index = sourceIndex
				}
				if !f.adopt(options).Applied {
					t.Fatal("managed import was not applied")
				}
			}
			qualified := schema + "." + name
			meta := h.inspectTable(f.raw, schema, name)
			if meta.Adopted || meta.AppIndex || meta.Foreign || meta.KeyRows || !meta.SnapshotSourceKey ||
				meta.SnapshotImportSource == "" || prefixesOverlap(meta.prefix(), f.prefix) || meta.index() == sourceIndex {
				t.Fatal("managed copy retained a live external storage layout")
			}
			owned, err := f.raw.HGet(h.ctx, snapshotMetadataGraph, "d:"+snapshotDataID(meta.prefix())).Bytes()
			var record struct {
				State  string `json:"state"`
				Prefix string `json:"prefix"`
			}
			if err != nil || json.Unmarshal(owned, &record) != nil || record.State != "live" || record.Prefix != meta.prefix() {
				t.Fatal("private copy has no registered data ownership", err)
			}
			worker := newSQLHarness(t)
			before := map[string]map[string]string{}
			for _, key := range keys {
				value, err := f.raw.HGetAll(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				before[key] = value
			}
			assertSources := func(want map[string]map[string]string) {
				t.Helper()
				for key, fields := range want {
					got, err := f.raw.HGetAll(h.ctx, key).Result()
					if err != nil || !maps.Equal(got, fields) {
						t.Fatalf("application HASH changed: %q: %v", key, err)
					}
				}
			}
			var definition indexInfo
			if mode != "adopted" {
				reply, err := f.raw.Do(h.ctx, "FT.INFO", sourceIndex).Result()
				if err != nil {
					t.Fatal(err)
				}
				definition = indexInfoOf(reply)
			}
			assertIndex := func() {
				t.Helper()
				if mode == "adopted" {
					return
				}
				reply, err := f.raw.Do(h.ctx, "FT.INFO", sourceIndex).Result()
				if err != nil || !partitionIndexDefinitionEqual(definition, indexInfoOf(reply)) {
					t.Fatal("partition/import changed the external index definition", err)
				}
			}
			query := "SELECT __key,name,n,exact,nullable FROM " + qualified
			var groups []string
			produce := func() (*arrow.Schema, adbc.Partitions) {
				t.Helper()
				statement, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer statement.Close(h.ctx)
				if err := statement.SetSqlQuery(h.ctx, query); err != nil {
					t.Fatal(err)
				}
				if err := statement.(adbc.GetSetOptionsWithContext).SetOptionInt(h.ctx, OptionIntPartitionCount, 3); err != nil {
					t.Fatal(err)
				}
				resultSchema, parts, _, err := statement.ExecutePartitions(h.ctx)
				if err != nil || parts.NumPartitions != 3 {
					t.Fatalf("managed copy did not produce three partitions: %d %v", parts.NumPartitions, err)
				}
				d, err := decodePartition(parts.PartitionIDs[0])
				if err != nil || d.Snapshot == nil {
					t.Fatal("partition lacks a shared generation", err)
				}
				for _, data := range parts.PartitionIDs {
					part, err := decodePartition(data)
					if err != nil || part.Snapshot == nil || *part.Snapshot != *d.Snapshot {
						t.Fatal("copy partitions use different generations", err)
					}
				}
				token := d.Snapshot.Token
				groups = append(groups, token)
				t.Cleanup(func() { h.exec("DROP PARTITION SNAPSHOT '" + token + "'") })
				return resultSchema, parts
			}
			read := func(schema *arrow.Schema, parts adbc.Partitions, refreshed bool) {
				t.Helper()
				seen := map[string]bool{}
				positions := map[string]int{}
				for i, key := range keys {
					positions[key] = i
				}
				for _, data := range parts.PartitionIDs {
					reader, err := worker.conn.ReadPartition(h.ctx, data)
					if err != nil {
						t.Fatal(err)
					}
					func() {
						defer reader.Release()
						if !reader.Schema().Equal(schema) {
							t.Fatal("copied partition schema changed")
						}
						for reader.Next() {
							batch := reader.RecordBatch()
							for row := 0; row < int(batch.NumRows()); row++ {
								values := make([]Value, 5)
								for col := range values {
									values[col], err = valueAt(batch.Column(col), row)
									if err != nil {
										t.Fatal(err)
									}
								}
								key := values[0].S
								i, ok := positions[key]
								n := int64(i)
								if refreshed && i == 0 {
									n = 99
								}
								if !ok || seen[key] || values[1].S != rawName || values[2].I != n || values[3].I != exact+n ||
									values[4].Null != (i != 0) || i == 0 && values[4].S != "present" {
									t.Fatalf("wrong/duplicate copied row for key %q: %v", key, values)
								}
								seen[key] = true
							}
						}
						if err := reader.Err(); err != nil {
							t.Fatal(err)
						}
					}()
				}
				if len(seen) != 3 {
					t.Fatal("partition coverage lost source keys", seen)
				}
			}
			metadataBefore := h.storedTableJSON(f.raw, schema, name)
			var oldSchema *arrow.Schema
			var oldParts adbc.Partitions
			for range 3 {
				oldSchema, oldParts = produce()
				read(oldSchema, oldParts, false)
				// A valid-looking version-1 token must never acquire the latest
				// copy implicitly or create/migrate metadata.
				legacy, err := json.Marshal(partitionID{Version: 1, Query: query, Schema: schema, Table: name,
					Target: partitionTarget(meta), Zone: "UTC", Lo: 1, Hi: 3})
				if err != nil {
					t.Fatal(err)
				}
				root := f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
				reader, err := worker.conn.ReadPartition(h.ctx, legacy)
				if reader != nil {
					reader.Release()
					t.Fatal("legacy descriptor returned a reader")
				}
				if !isStatus(err, adbc.StatusInvalidArgument) || !strings.Contains(err.Error(), "lacks a shared snapshot") {
					t.Fatal("legacy token failed for the wrong reason", err)
				}
				if f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
					t.Fatal("legacy token published metadata")
				}
			}
			assertSources(before)
			if mode == "foreign_index" {
				worker.expectError(query, "does not exist")
			}
			assertIndex()
			if h.storedTableJSON(f.raw, schema, name) != metadataBefore {
				t.Fatal("partition planning/reading rewrote copy metadata")
			}
			f.hset(keys[0], "n", 99, "exact", exact+99)
			read(oldSchema, oldParts, false)
			h.expectRows("SELECT COUNT(*) FROM "+qualified+" WHERE n=99", "0")
			if mode == "foreign_index" {
				if err := h.setConnOption(OptionStringForeignIndexesRefresh, "true"); err != nil {
					t.Fatal(err)
				}
			} else if !f.adopt(options).Applied {
				t.Fatal("explicit copy refresh failed")
			}
			fresh := h.inspectTable(f.raw, schema, name)
			if fresh.prefix() == meta.prefix() || fresh.index() == meta.index() {
				t.Fatal("refresh reused immutable copy storage")
			}
			newSchema, newParts := produce()
			read(newSchema, newParts, true)
			read(oldSchema, oldParts, false)
			assertIndex()
			expectedSource := maps.Clone(before)
			expectedSource[keys[0]] = maps.Clone(before[keys[0]])
			expectedSource[keys[0]]["n"], expectedSource[keys[0]]["exact"] = "99", fmt.Sprint(exact+99)
			if mode == "foreign_index" {
				// The fixture owns its external index. Removing that source and
				// explicitly refreshing retires the read-only foreign copy.
				if err := f.raw.Do(h.ctx, "FT.DROPINDEX", sourceIndex).Err(); err != nil {
					t.Fatal(err)
				}
				if err := h.setConnOption(OptionStringForeignIndexesRefresh, "true"); err != nil {
					t.Fatal(err)
				}
			} else {
				h.exec("DROP TABLE " + qualified)
			}
			snapshotReapTest(&store{client: f.raw, snapshots: true}, h.ctx)
			read(oldSchema, oldParts, false)
			read(newSchema, newParts, true)
			assertSources(expectedSource)
			if mode != "foreign_index" {
				assertIndex()
			}
			for _, group := range groups {
				h.exec("DROP PARTITION SNAPSHOT '" + group + "'")
			}
			for _, m := range []*tableMeta{meta, fresh} {
				for _, segment := range m.snapshotSegments() {
					h.expectSnapshotReleased(f.raw, segment.Prefix, segment.Index)
				}
			}
			assertSources(expectedSource)
		})
	}
}

// Storage rejection happens before partition planning: a pre-snapshot database
// cannot be opened. This explicitly disposable test never edits published data.
func TestSnapshotPartitionLegacyMetadataNotMigrated(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("source_index_%t", withIndex), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			if err := h.conn.Close(h.ctx); err != nil {
				t.Fatal(err)
			}
			if err := raw.FlushDB(h.ctx).Err(); err != nil {
				t.Fatal(err)
			}
			const table = "it_partition_legacy_guard"
			key, row := metaKey(defaultSchema, table), rowPrefix(defaultSchema, table)+"1"
			legacy, err := json.Marshal(&tableMeta{Schema: defaultSchema, Name: table,
				Columns: []columnMeta{{Name: "id", Type: typeInt64}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := raw.Set(h.ctx, key, legacy, 0).Err(); err != nil {
				t.Fatal(err)
			}
			if err := raw.HSet(h.ctx, row, rowIDField, "1", "id", "7").Err(); err != nil {
				t.Fatal(err)
			}
			index := indexName(defaultSchema, table)
			var before indexInfo
			if withIndex {
				if err := raw.Do(h.ctx, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, rowPrefix(defaultSchema, table),
					"SCHEMA", rowIDField, "NUMERIC", "id", "NUMERIC").Err(); err != nil {
					t.Fatal(err)
				}
				if err := (&store{client: raw}).awaitIndexReady(h.ctx, index); err != nil {
					t.Fatal(err)
				}
				reply, err := raw.Do(h.ctx, "FT.INFO", index).Result()
				if err != nil {
					t.Fatal(err)
				}
				before = indexInfoOf(reply)
			}
			db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx,
				retainedTestDatabaseOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(h.ctx)
			for range 3 {
				conn, err := db.Open(h.ctx)
				if conn != nil {
					_ = conn.Close(h.ctx)
					t.Fatal("legacy storage opened")
				}
				message := "require a fresh empty database"
				if withIndex {
					message = "requires a database without existing search indexes"
				}
				if err == nil || !strings.Contains(err.Error(), message) {
					t.Fatal("legacy open failed for the wrong reason", err)
				}
				if got, err := raw.Get(h.ctx, key).Bytes(); err != nil || string(got) != string(legacy) {
					t.Fatal("rejected open migrated legacy descriptor", err)
				}
				if fields, err := raw.HGetAll(h.ctx, row).Result(); err != nil || !maps.Equal(fields, map[string]string{rowIDField: "1", "id": "7"}) {
					t.Fatal("rejected open changed legacy row", err)
				}
				if n, err := raw.Exists(h.ctx, snapshotCatalogKey).Result(); err != nil || n != 0 {
					t.Fatal("rejected open initialized a snapshot catalog", n, err)
				}
				if withIndex {
					after, err := raw.Do(h.ctx, "FT.INFO", index).Result()
					if err != nil || !partitionIndexDefinitionEqual(before, indexInfoOf(after)) {
						t.Fatal("rejected open changed legacy index definition", err)
					}
				}
			}
		})
	}
}
