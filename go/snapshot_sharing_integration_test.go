// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at https://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Inspect commands after the namespace/fence hook, including individual pipeline
// commands. This catches copying or rebuilding B even if its final values agree.
type snapshotSharedGuard struct {
	snapshotHook
	prefix, index string
	touches       int
}

func (g *snapshotSharedGuard) check(cmd goredis.Cmder) {
	if c, ok := cmd.(snapshotFencedCommand); ok {
		cmd = c.Cmder
	}
	args := cmd.Args()
	if len(args) < 2 {
		return
	}
	name, key := strings.ToLower(fmt.Sprint(args[0])), fmt.Sprint(args[1])
	if name == "scan" {
		for i := 2; i+1 < len(args); i++ {
			if strings.EqualFold(fmt.Sprint(args[i]), "match") && strings.HasPrefix(fmt.Sprint(args[i+1]), globEscape(g.prefix)) {
				g.touches++
			}
		}
	}
	switch name {
	case "hgetall", "dump", "hset", "hdel", "del", "unlink", "restore", "ft.create", "ft.dropindex", "ft.alter":
		if strings.HasPrefix(key, g.prefix) || key == g.index {
			g.touches++
		}
	}
}
func (g *snapshotSharedGuard) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { g.check(cmd); return next(ctx, cmd) }
}
func (g *snapshotSharedGuard) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			g.check(cmd)
		}
		return next(ctx, cmds)
	}
}
func snapshotExecuteTest(t *testing.T, s *store, ctx context.Context, sql string) execResult {
	t.Helper()
	parsed, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	res, err := e.execute(ctx, parsed[0], nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func snapshotTableTest(t *testing.T, s *store, ctx context.Context, table string) *tableMeta {
	t.Helper()
	meta, err := s.getTable(ctx, "public", table)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}
func snapshotReapTest(s *store, ctx context.Context) {
	for i := 0; i < 64; i++ {
		s.reapSnapshot(ctx)
	}
}

func TestSnapshotSharesUnchangedTablesAndReclaimsReferences(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE a(id INTEGER,v INTEGER)")
	h.exec("CREATE TABLE b(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO a VALUES(1,10)")
	h.exec("INSERT INTO b SELECT i,9007199254740993 FROM generate_series(1,2001) AS g(i)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	a, b := snapshotTableTest(t, s, old, "a"), snapshotTableTest(t, s, old, "b")
	bRow := b.prefix() + "1"
	before, err := raw.Dump(h.ctx, bRow).Result()
	if err != nil {
		t.Fatal(err)
	}
	guard := &snapshotSharedGuard{prefix: b.prefix(), index: b.index()}
	s.client.AddHook(guard)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE a SET v=11")
	if guard.touches != 0 {
		t.Fatalf("unmodified table copied or rebuilt: %d physical commands", guard.touches)
	}
	current, currentPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	nextA, nextB := snapshotTableTest(t, s, current, "a"), snapshotTableTest(t, s, current, "b")
	currentPin.release()
	if nextA.prefix() == a.prefix() || nextA.index() == a.index() {
		t.Fatal("changed table reused published storage")
	}
	if nextB.prefix() != b.prefix() || nextB.index() != b.index() {
		t.Fatal("unmodified table did not share its generation")
	}
	if after := raw.Dump(h.ctx, bRow).Val(); after != before {
		t.Fatal("unmodified row HASH changed")
	}
	// The table's creator root can disappear while newer manifests retain B.
	// Repeated publications must not exhaust the manifest bound or reclaim B.
	for i := 0; i < 20; i++ {
		h.exec(fmt.Sprintf("UPDATE a SET v=%d", 100+i))
	}
	snapshotReapTest(s, h.ctx)
	if !raw.HExists(h.ctx, snapshotCatalogKey, "d:"+snapshotDataID(b.prefix())).Val() {
		t.Fatal("shared data record reclaimed")
	}
	if raw.Exists(h.ctx, a.prefix()+"1", bRow).Val() != 2 {
		t.Fatal("current or pinned table data reclaimed")
	}
	res := snapshotExecuteTest(t, s, old, "SELECT a.v,b.v FROM a JOIN b ON a.id=b.id")
	if len(res.rows) != 1 || res.rows[0][0].I != 10 || res.rows[0][1].I != 9007199254740993 {
		t.Fatalf("old manifest changed: %v", res.rows)
	}
	pin.release()
	snapshotReapTest(s, h.ctx)
	if raw.HExists(h.ctx, snapshotCatalogKey, "w:"+pin.root).Val() {
		t.Fatal("creator manifest was not reclaimed")
	}
	if raw.Exists(h.ctx, a.prefix()+"1").Val() != 0 {
		t.Fatal("unreferenced old A rows leaked")
	}
	if raw.Exists(h.ctx, bRow).Val() != 1 {
		t.Fatal("current shared B rows reclaimed")
	}
	if err := raw.Do(h.ctx, "FT.INFO", a.index()).Err(); err == nil {
		t.Fatal("unreferenced old A index leaked")
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM b", "2001|9007199254740993|9007199254740993")
	// DROP only removes the descriptor. A pinned manifest owns the physical
	// table independently of the current manifest and its original creator.
	held, heldPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer heldPin.release()
	h.exec("DROP TABLE b")
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, bRow).Val() != 1 {
		t.Fatal("DROP deleted pinned shared rows")
	}
	if res := snapshotExecuteTest(t, s, held, "SELECT COUNT(*) FROM b"); len(res.rows) != 1 || res.rows[0][0].I != 2001 {
		t.Fatalf("pinned dropped table unreadable: %v", res.rows)
	}
	heldPin.release()
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, bRow).Val() != 0 {
		t.Fatal("dropped shared rows leaked after final reference")
	}
	if err := raw.Do(h.ctx, "FT.INFO", b.index()).Err(); err == nil {
		t.Fatal("dropped shared index leaked")
	}
}

func TestSnapshotMetadataAndTruncateReuseUnchangedData(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO a VALUES(10)")
	h.exec("INSERT INTO b VALUES(20)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	a, b := snapshotTableTest(t, s, old, "a"), snapshotTableTest(t, s, old, "b")
	guard := &snapshotSharedGuard{prefix: b.prefix(), index: b.index()}
	s.client.AddHook(guard)
	snapshotExecuteTest(t, s, h.ctx, "COMMENT ON TABLE b IS 'shared metadata only'")
	snapshotExecuteTest(t, s, h.ctx, "TRUNCATE a")
	if guard.touches != 0 {
		t.Fatalf("metadata/TRUNCATE copied or rebuilt unrelated B: %d", guard.touches)
	}
	current, cp, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.release()
	if next := snapshotTableTest(t, s, current, "b"); next.prefix() != b.prefix() || next.index() != b.index() {
		t.Fatal("metadata changed physical generation")
	}
	if next := snapshotTableTest(t, s, current, "a"); next.prefix() == a.prefix() {
		t.Fatal("TRUNCATE retained old storage")
	}
	snapshotReapTest(s, h.ctx)
	if res := snapshotExecuteTest(t, s, old, "SELECT v FROM a"); len(res.rows) != 1 || res.rows[0][0].I != 10 {
		t.Fatalf("TRUNCATE damaged pinned table: %v", res.rows)
	}
	h.expectRows("SELECT COUNT(*) FROM a", "0")
	h.expectRows("SELECT v FROM b", "20")
}

func TestSnapshotStagingReferencesAndServerDataFence(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(7)")
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	shared := snapshotTableTest(t, s, base, "t")
	wctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.client.HSet(wctx, shared.prefix()+"1", "v", "999").Err(); err == nil {
		t.Fatal("writer mutated inherited generation")
	}
	// Even bypassing the Go scope validator cannot mutate another owner's data.
	forged := fenceSnapshotCommand(wctx, goredis.NewCmd(wctx, "HSET", shared.prefix()+"1", "v", "999"))
	if err := raw.Do(h.ctx, forged.Args()...).Err(); err == nil {
		t.Fatal("server fence permitted mutation of shared data")
	}
	if err := s.prepareSnapshotTable(wctx, "public", "t"); err != nil {
		t.Fatal(err)
	}
	private := snapshotTableTest(t, s, wctx, "t")
	if err := s.client.Del(wctx, private.prefix()+"1", shared.prefix()+"1").Err(); err == nil {
		t.Fatal("mixed-owner DEL passed the Go scope")
	}
	mixed := fenceSnapshotCommand(wctx, goredis.NewCmd(wctx, "DEL", private.prefix()+"1", shared.prefix()+"1"))
	if err := raw.Do(h.ctx, mixed.Args()...).Err(); err == nil {
		t.Fatal("mixed-owner DEL passed the server fence")
	}
	if raw.Exists(h.ctx, private.prefix()+"1", shared.prefix()+"1").Val() != 2 {
		t.Fatal("rejected mixed-owner DEL partially deleted rows")
	}
	h.exec("UPDATE t SET v=8")
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, private.prefix()+"1", shared.prefix()+"1").Val() != 2 {
		t.Fatal("active writer's referenced data reclaimed")
	}
	s.abortSnapshot(pin, id)
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, private.prefix()+"1").Val() != 0 {
		t.Fatal("aborted private table leaked")
	}
	if raw.Exists(h.ctx, shared.prefix()+"1").Val() != 1 {
		t.Fatal("abort deleted pinned base data")
	}
	h.expectRows("SELECT v FROM t", "8")
}
