// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func retainedCaptureFixture(t *testing.T) *retainedCapture {
	t.Helper()
	_, _, cfg, timeouts, cluster, _ := retainedSourceFixture(t)
	client := newRetainedSourceClient(cfg, cluster, timeouts, newRetainedBudget(64<<20))
	t.Cleanup(func() { _ = client.Close() })
	return &retainedCapture{store: &store{client: client, foreignIndexes: true}, budget: newRetainedBudget(64 << 20), maxRows: 4096, maxKeys: 10000}
}

func TestRetainedSourceCaptureExactLargeIDsAndMissingValues(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_retained_capture_large_ids"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT, label TEXT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (9007199254740993,'exact'),(9223372036854775807,NULL)")
	h.exec("ALTER TABLE " + table + " ADD COLUMN added BIGINT DEFAULT 17")
	raw := h.rawClient()
	meta, err := (&store{client: raw}).getTable(h.ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{1<<53 + 1, 1<<63 - 1}
	for index, id := range ids {
		key := meta.prefix() + strconv.FormatInt(id, 10)
		oldKey := meta.prefix() + strconv.Itoa(index+1)
		payload, err := raw.Dump(h.ctx, oldKey).Result()
		if err != nil {
			t.Fatal(err)
		}
		if err := raw.Restore(h.ctx, key, 0, payload).Err(); err != nil {
			t.Fatal(err)
		}
		if err := raw.Del(h.ctx, oldKey).Err(); err != nil {
			t.Fatal(err)
		}
		if err := raw.HSet(h.ctx, key, rowIDField, strconv.FormatInt(id, 10)).Err(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := raw.Del(h.ctx, key).Err(); err != nil {
				t.Error(err)
			}
		})
	}
	if err := raw.Set(h.ctx, seqKey(defaultSchema, table), strconv.FormatInt(ids[1], 10), 0).Err(); err != nil {
		t.Fatal(err)
	}
	// Adjust only this fixture's missing-value watermark to cover its moved IDs;
	// this is source setup, not a capture-side schema migration.
	for i := range meta.Columns {
		if meta.Columns[i].Name == "added" {
			meta.Columns[i].MissingThrough = ids[1]
		}
	}
	before, _ := json.Marshal(meta)
	if err := raw.Set(h.ctx, metaKey(defaultSchema, table), before, 0).Err(); err != nil {
		t.Fatal(err)
	}
	capture := retainedCaptureFixture(t)
	snapshot, err := capture.table(h.ctx, meta)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.isMem || len(snapshot.mem) != 2 {
		t.Fatal("incomplete capture")
	}
	var got []int64
	for _, row := range snapshot.mem {
		got = append(got, row[rowIDField].I)
		if row["id"].I != row[rowIDField].I || row["added"].I != 17 {
			t.Fatal("exact ID or missing default changed")
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, ids) {
		t.Fatal(got)
	}
	if current, err := raw.Get(h.ctx, metaKey(defaultSchema, table)).Bytes(); err != nil || string(current) != string(before) {
		t.Fatal("capture rewrote source metadata", err)
	}
	// Persistent DDL must invalidate unpublished coordinator state.
	h.exec("ALTER TABLE " + table + " RENAME COLUMN label TO renamed")
	if err := capture.checkAll(h.ctx); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("source DDL was ignored", err)
	}
}

func TestRetainedSourceCaptureApplicationOwnership(t *testing.T) {
	for _, mode := range []string{"adopted", "application_index", "foreign_index"} {
		t.Run(mode, func(t *testing.T) {
			f := newHashFixture(t, "it:retained:capture:")
			h := f.h
			const table = "it_retained_capture_app"
			const index = "it_retained_capture_index"
			f.dropAdopted(table)
			t.Cleanup(func() { f.dropAdopted(table) })
			keys := []string{f.prefix + "alice", f.prefix + "00001", f.prefix + "byte\x00\xff"}
			before := map[string]map[string]string{}
			for i, key := range keys {
				f.hset(key, "name", "exact\x00\xff", "n", i)
				row, err := f.raw.HGetAll(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				before[key] = row
			}
			if mode != "adopted" {
				f.appIndex(index, "SCHEMA", "name", "TAG", "n", "NUMERIC")
			}
			schema, name := defaultSchema, table
			if mode == "foreign_index" {
				schema, name = foreignSchema, index
			} else {
				opts := HashAdoptOptions{Table: table, Apply: true}
				if mode == "application_index" {
					opts.Index = index
				}
				if !f.adopt(opts).Applied {
					t.Fatal("fixture adoption failed")
				}
			}
			capture := retainedCaptureFixture(t)
			meta, err := capture.store.getTable(h.ctx, schema, name)
			if err != nil {
				t.Fatal(err)
			}
			if meta.AppIndex {
				if err := capture.store.loadAppIndex(h.ctx, meta); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := capture.table(h.ctx, meta)
			if err != nil {
				t.Fatal(err)
			}
			var found []string
			for _, row := range snapshot.mem {
				found = append(found, row[keyField].S)
			}
			slices.Sort(found)
			slices.Sort(keys)
			if !slices.Equal(found, keys) {
				t.Fatalf("application key identities changed: %q", found)
			}
			storage := retainedFixture(t, f.raw, defaultRetainedLimits())
			publisher := retainedAPIConnection(t, storage.namespace, false)
			if mode == "foreign_index" {
				if err := publisher.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
			}
			arrowSchema, parts, count := retainedAPIPlan(t, publisher, "SELECT CAST(__key AS BYTEA) AS key,n FROM "+schema+"."+name+" ORDER BY n")
			if count != 3 {
				t.Fatal("public layout capture omitted rows", count)
			}
			readerConn := retainedAPIConnection(t, storage.namespace, true)
			var exact []string
			for _, part := range parts.PartitionIDs {
				reader, err := readerConn.ReadPartition(h.ctx, part)
				if err != nil {
					t.Fatal(err)
				}
				if !reader.Schema().Equal(arrowSchema) {
					t.Fatal("public layout schema changed")
				}
				for reader.Next() {
					col := reader.RecordBatch().Column(0).(*array.Binary)
					for i := 0; i < col.Len(); i++ {
						exact = append(exact, string(col.Value(i)))
					}
				}
				err = reader.Err()
				reader.Release()
				if err != nil {
					t.Fatal(err)
				}
			}
			slices.Sort(exact)
			if !slices.Equal(exact, keys) {
				t.Fatal("public retained byte-key identities changed")
			}
			for key, expected := range before {
				row, err := f.raw.HGetAll(h.ctx, key).Result()
				if err != nil || !maps.Equal(row, expected) {
					t.Fatal("capture modified application data", err)
				}
			}
		})
	}
}

func TestRetainedSourceCaptureLimitsDoNotTruncate(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_retained_capture_limits"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (1),(2),(3)")
	for _, kind := range []string{"rows", "keys", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			c := retainedCaptureFixture(t)
			meta, err := c.store.getTable(h.ctx, defaultSchema, table)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "rows":
				c.maxRows = 2
			case "keys":
				c.maxKeys = 2
			case "bytes":
				c.budget = newRetainedBudget(1)
			}
			snapshot, err := c.table(h.ctx, meta)
			if snapshot != nil || !isStatus(err, adbc.StatusInvalidState) {
				t.Fatal("capture silently truncated or exceeded limits", err)
			}
		})
	}
	if rows, _ := h.query("SELECT id FROM " + table + " ORDER BY id"); !slices.Equal(rows, []string{"1", "2", "3"}) {
		t.Fatal("budget failure changed source rows", rows)
	}
}

func TestRetainedSourceCaptureLegacyWithoutMigration(t *testing.T) {
	id, err := retainedRandomID()
	if err != nil {
		t.Fatal(err)
	}
	table := "it_retained_capture_legacy_" + id
	for _, name := range []string{"fresh", "reused"} {
		t.Run(name, func(t *testing.T) { checkRetainedLegacyCapture(t, table) })
	}
}

func checkRetainedLegacyCapture(t *testing.T, table string) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (9007199254740993),(9223372036854775807)")
	raw := h.rawClient()
	st := &store{client: raw}
	meta, err := st.getTable(h.ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	original, err := raw.Get(h.ctx, metaKey(defaultSchema, table)).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Set(h.ctx, metaKey(defaultSchema, table), original, 0).Err(); err != nil {
			t.Error(err)
		}
	})
	legacy := *meta
	legacy.KeyPrefix = ""
	legacy.IndexName = ""
	for id := 1; id <= 2; id++ {
		key := legacy.prefix() + strconv.Itoa(id)
		oldKey := meta.prefix() + strconv.Itoa(id)
		t.Cleanup(func() {
			if err := raw.Del(h.ctx, key).Err(); err != nil {
				t.Error(err)
			}
		})
		// A never-used table name already has the legacy physical prefix.
		// Reused names receive a generation suffix and need the fixture copy.
		if oldKey == key {
			continue
		}
		payload, err := raw.Dump(h.ctx, oldKey).Result()
		if err != nil {
			t.Fatal(err)
		}
		if err := raw.Restore(h.ctx, key, 0, payload).Err(); err != nil {
			t.Fatal(err)
		}
		if err := raw.Del(h.ctx, oldKey).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.searchDo(h.ctx, meta.index(), "FT.DROPINDEX", meta.index()).Err(); err != nil {
		t.Fatal(err)
	}
	args := indexCreateArgs(&legacy)
	filtered := make([]any, 0, len(args))
	for _, arg := range args {
		if value, ok := arg.(string); ok && value == "SKIPINITIALSCAN" {
			continue
		}
		filtered = append(filtered, arg)
	}
	if err := st.searchDo(h.ctx, legacy.index(), filtered...).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.searchDo(h.ctx, legacy.index(), "FT.DROPINDEX", legacy.index()).Err(); err != nil {
			t.Error(err)
		}
	})
	if err := st.awaitIndexReady(h.ctx, legacy.index()); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(&legacy)
	if err := raw.Set(h.ctx, metaKey(defaultSchema, table), before, 0).Err(); err != nil {
		t.Fatal(err)
	}
	capture := retainedCaptureFixture(t)
	snapshot, err := capture.table(h.ctx, &legacy)
	if err != nil {
		t.Fatal(err)
	}
	var values []int64
	for _, row := range snapshot.mem {
		values = append(values, row["id"].I)
	}
	slices.Sort(values)
	if !slices.Equal(values, []int64{1<<53 + 1, 1<<63 - 1}) {
		t.Fatal("legacy values changed", values)
	}
	if got, err := raw.Get(h.ctx, metaKey(defaultSchema, table)).Result(); err != nil || got != string(before) {
		t.Fatal("capture migrated legacy metadata", err)
	}
}

func TestRetainedSourceCaptureForeignPrefixesAndFilter(t *testing.T) {
	h := newSQLHarness(t)
	raw := h.rawClient()
	st := &store{client: raw}
	const index = "it_retained_capture_multi"
	prefixes := []string{"it:retained:multi:a:", "it:retained:multi:b:\xff:"}
	keys := []string{prefixes[0] + "alice", prefixes[0] + "excluded", prefixes[1] + "00001", prefixes[1] + "excluded"}
	before := map[string]map[string]string{}
	for i, key := range keys {
		keep := 1
		if i%2 == 1 {
			keep = 0
		}
		if err := raw.HSet(h.ctx, key, "n", i, "keep", keep).Err(); err != nil {
			t.Fatal(err)
		}
		row, err := raw.HGetAll(h.ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		before[key] = row
		t.Cleanup(func() {
			if err := raw.Del(h.ctx, key).Err(); err != nil {
				t.Error(err)
			}
		})
	}
	_ = st.searchDo(h.ctx, index, "FT.DROPINDEX", index).Err()
	if err := st.searchDo(h.ctx, index, "FT.CREATE", index, "ON", "HASH", "PREFIX", 2, prefixes[0], prefixes[1], "FILTER", "@keep == 1", "SCHEMA", "n", "NUMERIC", "keep", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.searchDo(h.ctx, index, "FT.DROPINDEX", index).Err(); err != nil {
			t.Error(err)
		}
	})
	if err := st.awaitIndexReady(h.ctx, index); err != nil {
		t.Fatal(err)
	}
	capture := retainedCaptureFixture(t)
	meta, err := capture.store.getTable(h.ctx, foreignSchema, index)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.store.loadAppIndex(h.ctx, meta); err != nil {
		t.Fatal(err)
	}
	snapshot, err := capture.table(h.ctx, meta)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range snapshot.mem {
		got = append(got, row[keyField].S)
	}
	slices.Sort(got)
	want := []string{keys[0], keys[2]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("foreign index scope/filter changed: %q != %q", got, want)
	}
	for key, expected := range before {
		row, err := raw.HGetAll(h.ctx, key).Result()
		if err != nil || !maps.Equal(row, expected) {
			t.Fatal("foreign source was modified", err)
		}
	}
}
