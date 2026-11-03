// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSnapshotSchemaRowsCompactionAndReclamation(t *testing.T) {
	for _, schema := range []string{"private", "adbc", "tenant:{evil}*雪"} {
		t.Run(schema, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			quoted := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
			table := quoted + ".t"
			h.exec("CREATE SCHEMA " + quoted)
			h.exec("CREATE TABLE " + table + "(v INTEGER)")
			h.exec("INSERT INTO " + table + " VALUES(1)")
			h.exec("CREATE TABLE protected(v INTEGER)")
			h.exec("INSERT INTO protected VALUES(77)")
			s := snapshotTestStore(t, raw)
			oldCtx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			old, err := s.getTable(oldCtx, schema, "t")
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(old.prefix(), "adbc:") || old.prefix() != snapshotRows(snapshotDataID(old.prefix()), schema) {
				t.Fatal("rows overlap the metadata ACL grant")
			}
			for value := 2; value <= 4; value++ {
				h.exec(fmt.Sprintf("UPDATE %s SET v=%d", table, value))
			}
			snapshotDrainCompactionTest(t, s)
			h.expectRows("SELECT v FROM "+table, "4")
			currentCtx, currentPin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			current, err := s.getTable(currentCtx, schema, "t")
			if err != nil {
				t.Fatal(err)
			}
			currentPin.release()
			if len(current.snapshotSegments()) != 1 || current.prefix() != snapshotRows(snapshotDataID(current.prefix()), schema) {
				t.Fatal("compaction lost the schema namespace")
			}
			var data struct{ Prefix string }
			if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotMetadataGraph, "d:"+snapshotDataID(current.prefix())).Val()), &data); err != nil || data.Prefix != current.prefix() {
				t.Fatalf("durable physical prefix missing: %v", err)
			}
			h.exec("DROP SCHEMA " + quoted + " CASCADE")
			snapshotReapTest(s, h.ctx)
			rows, err := s.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1"}, nil)
			if err != nil || len(rows) != 1 || rows[0]["v"] != "1" {
				t.Fatalf("old schema reader lost its pinned data: %v %v", rows, err)
			}
			pin.release()
			snapshotReapTest(s, h.ctx)
			if raw.Exists(h.ctx, old.prefix()+"1", current.prefix()+"1").Val() != 0 {
				t.Fatal("schema-qualified rows survived zero-reference reclamation")
			}
			h.expectRows("SELECT v FROM protected", "77")
		})
	}
}

func TestSnapshotSchemaPrefixAliasRejected(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	s := snapshotTestStore(t, raw)
	wctx, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=2")
	defer s.abortSnapshot(pin, id)
	meta, err := s.getTable(wctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	wrong := snapshotRows(snapshotDataID(meta.prefix()), "other")
	if err := s.client.HSet(wctx, wrong+"1", rowIDField, "1", "v", "999").Err(); err == nil {
		t.Fatal("same UUID authorized a different schema prefix")
	}
	if raw.Exists(h.ctx, wrong+"1").Val() != 0 {
		t.Fatal("rejected prefix alias wrote a row")
	}
	changed := cloneMeta(meta)
	changed.KeyPrefix = wrong
	changed.IndexName = snapshotIndexPrefix(snapshotDataID(meta.prefix())) + "alias"
	if err := s.client.Do(wctx, snapshotScannedIndexArgs(changed)...).Err(); err == nil {
		t.Fatal("Search admission accepted another physical prefix")
	}
	if indexes, err := raw.Do(h.ctx, "FT._LIST").StringSlice(); err != nil {
		t.Fatal(err)
	} else {
		for _, index := range indexes {
			if index == changed.IndexName {
				t.Fatal("rejected Search admission created an index")
			}
		}
	}
	changed.IndexName = meta.IndexName
	encoded, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.redis(wctx).Set(wctx, metaKey("public", "t"), encoded, 0).Err(); err == nil {
		t.Fatal("descriptor registered an unowned physical prefix")
	}
	h.expectRows("SELECT v FROM t", "1")
}
