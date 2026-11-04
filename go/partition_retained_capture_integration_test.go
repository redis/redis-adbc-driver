// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func retainedCaptureFixture(t *testing.T, h *sqlHarness) (*retainedCapture, context.Context) {
	t.Helper()
	_, _, cfg, timeouts, cluster, _ := retainedSourceFixture(t)
	client := newRetainedSourceClient(cfg, cluster, timeouts, newRetainedBudget(64<<20))
	addSnapshotHooks(client)
	t.Cleanup(func() { _ = client.Close() })
	st := &store{client: client, foreignIndexes: true, snapshots: true}
	ctx, pin, err := st.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pin.release)
	return &retainedCapture{store: st, budget: newRetainedBudget(64 << 20), maxRows: 4096, maxKeys: 10000}, ctx
}

func TestRetainedSourceCaptureExactLargeIDsAndMissingValues(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_retained_capture_large_ids"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT, label TEXT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (9007199254740993,'exact'),(9223372036854775807,NULL)")
	ids := []int64{1<<53 + 1, 1<<63 - 1}
	// Publish sparse IDs and tombstones through the native ownership protocol.
	// SQL ADD DEFAULT materializes values at both moved IDs.
	h.sparseSnapshotTable(table, ids[1], map[int64]int64{1: ids[0], 2: ids[1]})
	h.exec("ALTER TABLE " + table + " ADD COLUMN added BIGINT DEFAULT 17")
	raw := h.rawClient()
	capture, ctx := retainedCaptureFixture(t, h)
	meta, err := capture.store.getTable(ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the historical missing-value decoder check on a read-only metadata
	// copy. Current SQL materializes ADD DEFAULT, so published metadata stays native.
	virtualMissing := func(source *tableMeta) *tableMeta {
		m := cloneMeta(source)
		m.Columns = append(m.Columns, columnMeta{Name: "missing", Type: typeInt64, Nullable: true, Default: "23", Missing: "23", MissingThrough: ids[1]})
		return m
	}
	keys := []string{meta.prefix() + fmt.Sprint(ids[0]), meta.prefix() + fmt.Sprint(ids[1])}
	images, err := capture.store.fetchSnapshotVisible(ctx, meta, keys, nil)
	if err != nil || len(images) != 2 {
		t.Fatal("exact fixture rows unavailable", err)
	}
	for _, image := range images {
		if image == nil || image["added"] != "17" {
			t.Fatal("SQL default was not materialized")
		}
		if _, exists := image["missing"]; exists {
			t.Fatal("virtual missing-value fixture field exists")
		}
	}
	before, err := capture.store.redis(ctx).Get(ctx, metaKey(defaultSchema, table)).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := capture.table(ctx, virtualMissing(meta))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.isMem || len(snapshot.mem) != 2 {
		t.Fatal("incomplete capture")
	}
	var got []int64
	for _, row := range snapshot.mem {
		got = append(got, row[rowIDField].I)
		if row["id"].Null || row[rowIDField].Null || row["added"].Null || row["missing"].Null || row["missing"].I != 23 || row["id"].I != row[rowIDField].I || row["added"].I != 17 ||
			row["label"].Null != (row["id"].I == ids[1]) || row["id"].I == ids[0] && row["label"].S != "exact" {
			t.Fatal("exact ID or missing default changed")
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, ids) {
		t.Fatal(got)
	}
	if current, err := capture.store.redis(ctx).Get(ctx, metaKey(defaultSchema, table)).Bytes(); err != nil || string(current) != string(before) {
		t.Fatal("capture rewrote pinned metadata", err)
	}
	// Later DDL publishes a new generation; the captured source remains valid.
	h.exec("ALTER TABLE " + table + " RENAME COLUMN label TO renamed")
	if err := capture.checkAll(ctx); err != nil {
		t.Fatal("DDL invalidated immutable input", err)
	}
	old, err := capture.store.getTable(ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	_, oldLabel := old.column("label")
	_, oldRenamed := old.column("renamed")
	if !oldLabel || oldRenamed {
		t.Fatal("pinned definition changed", err)
	}
	current := h.inspectTable(raw, defaultSchema, table)
	_, newLabel := current.column("label")
	_, newRenamed := current.column("renamed")
	if !newRenamed || newLabel {
		t.Fatal("fresh definition did not change")
	}
	again, err := capture.table(ctx, virtualMissing(old))
	if err != nil || len(again.mem) != 2 {
		t.Fatal("pinned recapture failed", err)
	}
	for _, row := range again.mem {
		if row["id"].Null || row[rowIDField].Null || row["added"].Null || row["missing"].Null || row["missing"].I != 23 || row["id"].I != row[rowIDField].I || row["added"].I != 17 ||
			row["label"].Null != (row["id"].I == ids[1]) || row["id"].I == ids[0] && row["label"].S != "exact" {
			t.Fatal("recapture changed exact values")
		}
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
				if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
			} else {
				opts := HashAdoptOptions{Table: table, Apply: true}
				if mode == "application_index" {
					opts.Index = index
				}
				if !f.adopt(opts).Applied {
					t.Fatal("fixture adoption failed")
				}
			}
			var definition indexInfo
			if mode != "adopted" {
				reply, err := f.raw.Do(h.ctx, "FT.INFO", index).Result()
				if err != nil {
					t.Fatal(err)
				}
				definition = indexInfoOf(reply)
			}
			capture, ctx := retainedCaptureFixture(t, h)
			meta, err := capture.store.getTable(ctx, schema, name)
			if err != nil {
				t.Fatal(err)
			}
			if meta.Adopted || meta.AppIndex || meta.Foreign || meta.KeyRows || !meta.SnapshotSourceKey || prefixesOverlap(meta.prefix(), f.prefix) {
				t.Fatal("capture did not use an owned managed copy")
			}

			snapshot, err := capture.table(ctx, meta)
			if err != nil {
				t.Fatal(err)
			}
			var found []string
			for _, row := range snapshot.mem {
				key := row[keyField].S
				fields, ok := before[key]
				if !ok || row[keyField].Null || row["name"].Null || row["name"].S != fields["name"] || row["n"].Null || fmt.Sprint(row["n"].I) != fields["n"] {
					t.Fatal("copied source values changed", key)
				}
				found = append(found, key)
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
			arrowSchema, parts, count := retainedAPIPlan(t, publisher, "SELECT CAST(__key AS BYTEA) AS key,CAST(name AS BYTEA) AS name,n FROM "+schema+"."+name+" ORDER BY n")
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
					batch := reader.RecordBatch()
					col := batch.Column(0).(*array.Binary)
					names := batch.Column(1).(*array.Binary)
					nums := batch.Column(2).(*array.Int64)
					for i := 0; i < col.Len(); i++ {
						key := string(col.Value(i))
						fields, ok := before[key]
						if !ok || col.IsNull(i) || names.IsNull(i) || nums.IsNull(i) || string(names.Value(i)) != fields["name"] || fmt.Sprint(nums.Value(i)) != fields["n"] {
							t.Fatal("public retained exact source values changed", key)
						}
						exact = append(exact, key)
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
			if mode != "adopted" {
				reply, err := f.raw.Do(h.ctx, "FT.INFO", index).Result()
				if err != nil || !partitionIndexDefinitionEqual(definition, indexInfoOf(reply)) {
					t.Fatal("capture changed external index definition", err)
				}
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
			c, ctx := retainedCaptureFixture(t, h)
			meta, err := c.store.getTable(ctx, defaultSchema, table)
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
			snapshot, err := c.table(ctx, meta)
			if snapshot != nil || !isStatus(err, adbc.StatusInvalidState) {
				t.Fatal("capture silently truncated or exceeded limits", err)
			}
		})
	}
	if rows, _ := h.query("SELECT id FROM " + table + " ORDER BY id"); !slices.Equal(rows, []string{"1", "2", "3"}) {
		t.Fatal("budget failure changed source rows", rows)
	}
}

func TestSnapshotRetainedSourceCaptureLegacyWithoutMigration(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("source_index_%t", withIndex), func(t *testing.T) { checkSnapshotLegacyCatalogRefusal(t, withIndex, true) })
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
	if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	capture, ctx := retainedCaptureFixture(t, h)
	meta, err := capture.store.getTable(ctx, foreignSchema, index)
	if err != nil {
		t.Fatal(err)
	}
	if meta.AppIndex || meta.Foreign || meta.Adopted || meta.KeyRows || !meta.SnapshotSourceKey {
		t.Fatal("foreign source was not converted to a private managed copy")
	}
	snapshot, err := capture.table(ctx, meta)
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
