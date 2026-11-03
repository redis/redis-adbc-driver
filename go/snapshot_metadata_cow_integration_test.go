// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotUnrelatedMetadataCopies struct {
	snapshotHook
	suffixes []string
	copies   atomic.Int64
}

func (h *snapshotUnrelatedMetadataCopies) record(cmd goredis.Cmder) {
	args := cmd.Args()
	if fenced, ok := cmd.(snapshotFencedCommand); ok {
		args = fenced.Cmder.Args()
	}
	if len(args) < 2 {
		return
	}
	positions := []int{1}
	switch strings.ToLower(fmt.Sprint(args[0])) {
	case "dump", "restore":
	case "copy":
		positions = append(positions, 2)
	default:
		return
	}
	for _, position := range positions {
		if position >= len(args) {
			continue
		}
		key := fmt.Sprint(args[position])
		for _, suffix := range h.suffixes {
			if strings.HasSuffix(key, suffix) {
				h.copies.Add(1)
				return
			}
		}
	}
}
func (h *snapshotUnrelatedMetadataCopies) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { h.record(cmd); return next(ctx, cmd) }
}
func (h *snapshotUnrelatedMetadataCopies) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			h.record(cmd)
		}
		return next(ctx, cmds)
	}
}
func TestSnapshotDMLDoesNotCopyUnrelatedMetadata(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	guard := &snapshotUnrelatedMetadataCopies{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("t%d", i)
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
		if i != 0 {
			guard.suffixes = append(guard.suffixes, strings.TrimPrefix(metaKey("public", name), metaPrefix))
		}
	}
	h.exec("INSERT INTO t0 VALUES(1)")
	s := snapshotTestStore(t, raw)
	s.client.AddHook(guard)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t0 SET v=2")
	if n := guard.copies.Load(); n != 0 {
		t.Fatalf("one-row DML copied unrelated table metadata %d times", n)
	}
	h.expectRows("SELECT v FROM t0", "2")
	for i := 1; i < 8; i++ {
		h.expectRows(fmt.Sprintf("SELECT COUNT(*) FROM t%d", i), "0")
	}
}

func TestSnapshotMetadataValuesSurviveOriginalManifest(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO a VALUES(1)")
	h.exec("INSERT INTO b VALUES(2)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	root := snapshotOf(ctx).id
	suffix := strings.TrimPrefix(metaKey("public", "b"), metaPrefix)
	key, err := resolveSnapshotMetadata(ctx, suffix, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, snapshotWorkspace(root)) {
		t.Fatal("test requires a value owned by the retiring manifest")
	}
	pin.release()
	for i := 0; i < 20; i++ {
		h.exec(fmt.Sprintf("COMMENT ON TABLE a IS 'version %d'", i))
	}
	snapshotReapTest(s, h.ctx)
	if raw.HExists(h.ctx, snapshotCatalogKey, "w:"+root).Val() {
		t.Fatal("original manifest was kept alive for an unrelated metadata value")
	}
	if raw.Exists(h.ctx, snapshotMetadataMap(root)).Val() != 0 {
		t.Fatal("retired root map survived")
	}
	if raw.Exists(h.ctx, key).Val() != 1 {
		t.Fatal("sharing lost the original metadata value")
	}
	current, currentPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	now, err := resolveSnapshotMetadata(current, suffix, false)
	currentPin.release()
	if err != nil || now != key {
		t.Fatalf("unmodified metadata was copied instead of shared: %v", err)
	}
	h.expectRows("SELECT v FROM b", "2")
	h.exec("DROP TABLE b")
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, key).Val() != 0 {
		t.Fatal("unreferenced native metadata value was not reclaimed")
	}
}

func TestSnapshotMetadataOldPinProtectsValuesAcrossDrop(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(7)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	key, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(metaKey("public", "t"), metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	h.exec("DROP TABLE t")
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, key).Val() != 1 {
		t.Fatal("old pin lost its metadata")
	}
	got := snapshotExecuteTest(t, s, ctx, "SELECT v FROM t")
	if len(got.rows) != 1 || got.rows[0][0].I != 7 {
		t.Fatal("old metadata/row visibility changed", got.rows)
	}
	pin.release()
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, key).Val() != 0 {
		t.Fatal("released old metadata was not reclaimed")
	}
}

func TestSnapshotMetadataSharingRejectsIncompleteObjects(t *testing.T) {
	for _, kind := range []string{"root", "bucket", "value"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(v INTEGER)")
			h.exec("INSERT INTO t VALUES(7)")
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			root := snapshotOf(ctx).id
			suffix := strings.TrimPrefix(metaKey("public", "t"), metaPrefix)
			slot := snapshotMetadataSlot(suffix)
			key, err := resolveSnapshotMetadata(ctx, suffix, false)
			if err != nil {
				t.Fatal(err)
			}
			owner := raw.HGet(h.ctx, snapshotMetadataMap(root), slot).Val()
			pin.release()
			var message string
			switch kind {
			case "root":
				err = raw.HDel(h.ctx, snapshotMetadataMap(root), slot).Err()
				message = "metadata map is incomplete"
			case "bucket":
				err = raw.HDel(h.ctx, snapshotMetadataBucket(owner, slot), suffix).Err()
				message = "metadata bucket is incomplete"
			case "value":
				err = raw.Del(h.ctx, key).Err()
				message = "metadata object is missing or corrupt"
			}
			if err != nil {
				t.Fatal(err)
			}
			h.expectError("SELECT v FROM t", message)
		})
	}
}

func TestSnapshotMetadataFenceRejectsSharedPhysicalMutation(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	key, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(metaKey("public", "t"), metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	before := raw.Get(h.ctx, key).Val()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	forged := fenceSnapshotCommand(wctx, goredis.NewCmd(wctx, "SET", key, "corrupt"))
	if err := s.client.Process(wctx, forged); err == nil {
		t.Fatal("writer mutated shared physical metadata")
	}
	if raw.Get(h.ctx, key).Val() != before {
		t.Fatal("failed fence modified shared metadata")
	}
	orphan := snapshotWorkspace(id) + "unregistered"
	if err := s.client.Set(wctx, orphan, "value", 0).Err(); err == nil {
		t.Fatal("writer bypassed metadata reference registration")
	}
	if raw.Exists(h.ctx, orphan).Val() != 0 {
		t.Fatal("unregistered metadata was created")
	}
}

func TestSnapshotMetadataDeletedNamesDoNotAccumulate(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("gone_%d", i)
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
		h.exec("DROP TABLE " + name)
	}
	s := snapshotTestStore(t, raw)
	snapshotReapTest(s, h.ctx)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	roots, err := raw.HGetAll(h.ctx, snapshotMetadataMap(snapshotOf(ctx).id)).Result()
	if err != nil {
		t.Fatal(err)
	}
	for slot, owner := range roots {
		fields, err := raw.HKeys(h.ctx, snapshotMetadataBucket(owner, slot)).Result()
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range fields {
			if strings.Contains(field, ":gone_") {
				t.Fatal("deleted table or sequence mapping remained in current metadata", field)
			}
		}
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name LIKE 'gone_%'", "0")
}
