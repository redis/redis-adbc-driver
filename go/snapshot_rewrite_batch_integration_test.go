// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"os"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotLargeRewriteBatches(t *testing.T) {
	for _, mode := range []string{"update", "delete", "alter"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT,v BIGINT,payload VARCHAR)")
			h.exec("INSERT INTO t SELECT i,9007199254740993,'old' FROM generate_series(1,100001) AS g(i)")
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			old := snapshotTableTest(t, s, ctx, "t")
			before := raw.Dump(h.ctx, old.prefix()+"1").Val()
			switch mode {
			case "update":
				h.exec("UPDATE t SET v=NULL,payload='new'")
				h.expectRows("SELECT COUNT(*),COUNT(v),MIN(payload),MAX(payload) FROM t", "100001|0|new|new")
			case "delete":
				h.exec("DELETE FROM t")
				h.expectRows("SELECT COUNT(*) FROM t", "0")
			case "alter":
				h.exec("ALTER TABLE t ALTER COLUMN v TYPE VARCHAR")
				h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "100001|9007199254740993|9007199254740993")
			}
			if raw.Dump(h.ctx, old.prefix()+"1").Val() != before || raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(old.prefix()))).Val() != 100001 {
				t.Fatal("rewrite changed pinned source")
			}
		})
	}
}

func TestSnapshotReplaceBatchPermissionPreflight(t *testing.T) {
	uri := os.Getenv("REDIS_SNAPSHOT_REPLACE_ACL_URI")
	if uri == "" {
		t.Skip("requires owned Software principal with HSET/ZADD allowed and DEL denied")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	meta := &tableMeta{Schema: "public", Name: "replace_acl", Columns: []columnMeta{{Name: "v", Type: typeString, Nullable: true}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	first := snapshotInsertRow{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1), fields: []any{rowIDField, "1", "v", "old"}}
	if err := s.insertSnapshotBatch(wctx, meta.prefix(), []snapshotInsertRow{first}); err != nil {
		t.Fatal(err)
	}
	credentials, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal("invalid private principal configuration")
	}
	opts := *raw.Options()
	opts.Username, opts.Password = credentials.Username, credentials.Password
	limited := goredis.NewClient(&opts)
	defer limited.Close()
	directory := snapshotMembers(snapshotDataID(meta.prefix()))
	if err := limited.ZAdd(h.ctx, directory, goredis.Z{Member: first.member}).Err(); err != nil {
		t.Fatal("ZADD positive control failed")
	}
	probe := meta.prefix() + "acl-probe"
	if err := limited.HSet(h.ctx, probe, "v", "old").Err(); err != nil {
		t.Fatal("HSET positive control failed")
	}
	if err := limited.Del(h.ctx, probe).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("DEL negative control failed")
	}
	if err := raw.Del(h.ctx, probe).Err(); err != nil {
		t.Fatal(err)
	}
	guarded := snapshotTestStore(t, limited)
	first.fields = []any{rowIDField, "1"} // replacement would remove the old value
	second := snapshotInsertRow{key: meta.prefix() + "2", member: fmt.Sprintf("%020d", 2), fields: []any{rowIDField, "2", "v", "new"}}
	err = guarded.snapshotRowBatch(wctx, meta.prefix(), []snapshotInsertRow{first, second}, true)
	if err == nil || !strings.Contains(err.Error(), "snapshot row replacement requires DEL") {
		t.Fatalf("replacement preflight not exercised: %v", err)
	}
	if raw.HGet(h.ctx, first.key, "v").Val() != "old" || raw.Exists(h.ctx, second.key).Val() != 0 || raw.ZCard(h.ctx, directory).Val() != 1 {
		t.Fatal("permission denial partially replaced/registered rows")
	}
}

func TestSnapshotRewriteBatchOutcomes(t *testing.T) {
	for _, mode := range []string{"update", "delete", "alter"} {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after_%t", mode, after), func(t *testing.T) {
				h, raw, _ := newSnapshotHarnessMode(t, false, false)
				h.exec("CREATE TABLE t(v INTEGER)")
				h.exec("INSERT INTO t SELECT i FROM generate_series(1,600) AS g(i)")
				s := snapshotTestStore(t, raw)
				observer := &snapshotInsertBatchInterruption{after: after}
				s.client.AddHook(observer)
				query := map[string]string{"update": "UPDATE t SET v=v+1", "delete": "DELETE FROM t", "alter": "ALTER TABLE t ALTER COLUMN v TYPE BIGINT USING v+1"}[mode]
				parsed, err := ParseScript(query)
				if err != nil {
					t.Fatal(err)
				}
				e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
				root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
				_, err = e.execute(h.ctx, parsed[0], nil, nil, nil)
				if !after {
					if err == nil || observer.calls != 2 || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
						t.Fatalf("failed rewrite published/replayed: %v calls=%d", err, observer.calls)
					}
					h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "600|1|600")
					return
				}
				if err != nil || observer.calls != 3 || observer.rows != 600 {
					t.Fatalf("lost reply did not resolve once: %v calls=%d rows=%d", err, observer.calls, observer.rows)
				}
				if mode == "delete" {
					h.expectRows("SELECT COUNT(*) FROM t", "0")
				} else {
					h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "600|2|601")
				}
			})
		}
	}
}

func TestSnapshotReplacementRequiresRegisteredPrivateImages(t *testing.T) {
	for _, mode := range []string{"hash", "string", "missing", "score", "alias"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			wctx, id, err := s.beginSnapshotWrite(ctx, pin)
			if err != nil {
				t.Fatal(err)
			}
			defer s.abortSnapshot(pin, id)
			meta := &tableMeta{Schema: "public", Name: "replace_ownership", Columns: []columnMeta{{Name: "v", Type: typeString, Nullable: true}}}
			if _, err := s.createTable(wctx, meta, false); err != nil {
				t.Fatal(err)
			}
			first := snapshotInsertRow{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1), fields: []any{rowIDField, "1", "v", "old"}}
			if err := s.insertSnapshotBatch(wctx, meta.prefix(), []snapshotInsertRow{first}); err != nil {
				t.Fatal(err)
			}
			second := snapshotInsertRow{key: meta.prefix() + "2", member: fmt.Sprintf("%020d", 2), fields: []any{rowIDField, "2", "v", "new"}}
			directory := snapshotMembers(snapshotDataID(meta.prefix()))
			switch mode {
			case "hash", "score":
				if err := raw.HSet(h.ctx, second.key, rowIDField, "2", "v", "unregistered").Err(); err != nil {
					t.Fatal(err)
				}
			case "string":
				if err := raw.Set(h.ctx, second.key, "unregistered", 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "alias":
				second.key = snapshotRows(snapshotDataID(meta.prefix()), "another_schema", meta.SnapshotAccessID) + "2"
			}
			if mode == "missing" || mode == "score" {
				score := float64(0)
				if mode == "score" {
					score = 1
				}
				if err := raw.ZAdd(h.ctx, directory, goredis.Z{Score: score, Member: second.member}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			beforeFirst, beforeSecond, beforeDirectory := raw.Dump(h.ctx, first.key).Val(), raw.Dump(h.ctx, second.key).Val(), raw.Dump(h.ctx, directory).Val()
			first.fields = []any{rowIDField, "1"}
			if err := s.snapshotRowBatch(wctx, meta.prefix(), []snapshotInsertRow{first, second}, true); err == nil {
				t.Fatal("unsafe replacement admitted")
			}
			if raw.Dump(h.ctx, first.key).Val() != beforeFirst || raw.Dump(h.ctx, second.key).Val() != beforeSecond || raw.Dump(h.ctx, directory).Val() != beforeDirectory {
				t.Fatal("late validation partially changed owned/unregistered objects")
			}
		})
	}
}
