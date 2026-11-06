// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Independent accounting includes native objects waiting for value GC, not just
// objects reachable from the latest root. Staging/job references are additional
// owners; committed manifests must never retain catalog-wide reference arrays.
func assertSnapshotDataReferences(t *testing.T, h *sqlHarness, s *store) {
	t.Helper()
	graph, err := s.client.HGetAll(h.ctx, snapshotMetadataGraph).Result()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := s.client.HGetAll(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int64{}
	add := func(value string) {
		var refs []string
		if err := json.Unmarshal([]byte(value), &refs); err != nil {
			t.Fatal(err)
		}
		for _, id := range refs {
			expected[id]++
		}
	}
	for key, value := range graph {
		if strings.HasPrefix(key, "t:") {
			add(value)
		}
	}
	for key, value := range catalog {
		if strings.HasPrefix(key, "d:") {
			t.Fatal("data registry leaked into bounded control catalog")
		}
		if strings.HasPrefix(key, "r:") {
			add(value)
		}
	}
	for key, value := range graph {
		if !strings.HasPrefix(key, "r:") {
			continue
		}
		id := strings.TrimPrefix(key, "r:")
		count, err := strconv.ParseInt(value, 10, 64)
		if err != nil || count != expected[id] {
			t.Fatalf("reference count for %s: actual %s, expected %d", id, value, expected[id])
		}
		delete(expected, id)
	}
	if len(expected) != 0 {
		t.Fatal("missing data reference counters", expected)
	}
}

func TestSnapshotDataReferencesPrivateCopySurvivesBaseRetirement(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(7)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	source, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(metaKey("public", "t"), metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	private, err := resolveSnapshotMetadata(wctx, strings.TrimPrefix(metaKey("public", "t"), metaPrefix), true)
	if err != nil {
		t.Fatal(err)
	}
	if private == source {
		t.Fatal("test requires a private native copy")
	}
	if refs := raw.HGet(h.ctx, snapshotCatalogKey, "r:"+id).Val(); refs != "[]" {
		t.Fatal("reservation copied base data references", refs)
	}
	// A stopped/expired base pin must not revoke a slightly later staging lease.
	pin.release()
	h.exec("DROP TABLE t")
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, source).Val() != 0 {
		t.Fatal("source native object was not reclaimed")
	}
	if raw.Exists(h.ctx, meta.prefix()+"1").Val() != 1 {
		t.Fatal("private descriptor lost its inherited source data")
	}
	if count := raw.HGet(h.ctx, snapshotMetadataGraph, "r:"+snapshotDataID(meta.prefix())).Val(); count != "1" {
		t.Fatal("private descriptor should be the only data owner", count)
	}
	assertSnapshotDataReferences(t, h, s)
	s.abortSnapshot(pin, id)
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, private, meta.prefix()+"1").Val() != 0 {
		t.Fatal("aborted private descriptor or its data survived reclamation")
	}
	if raw.HExists(h.ctx, snapshotMetadataGraph, "d:"+snapshotDataID(meta.prefix())).Val() {
		t.Fatal("aborted copy leaked its data registration")
	}
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotDataReferencesDescriptorReplacementIsAtomic(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v BIGINT)")
	h.exec("INSERT INTO t VALUES(9007199254740993)")
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
	key := metaKey("public", "t")
	physical, err := resolveSnapshotMetadata(wctx, strings.TrimPrefix(key, metaPrefix), true)
	if err != nil {
		t.Fatal(err)
	}
	before, err := raw.Get(h.ctx, physical).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.redis(wctx).Set(wctx, key, before, 0).Err(); err != nil {
			t.Fatal(err)
		}
		assertSnapshotDataReferences(t, h, s)
	}
	// A failed SET NX must leave the existing object's references intact.
	if changed, err := s.redis(wctx).SetNX(wctx, key, before, 0).Result(); err != nil || changed {
		t.Fatal("SET NX changed an existing descriptor", changed, err)
	}
	var broken tableMeta
	if err := json.Unmarshal(before, &broken); err != nil {
		t.Fatal(err)
	}
	missing := snapshotID()
	broken.KeyPrefix, broken.IndexName = snapshotRows(missing), snapshotIndexPrefix(missing)+"working"
	value, err := json.Marshal(broken)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.redis(wctx).Set(wctx, key, value, 0).Err(); err == nil {
		t.Fatal("descriptor accepted unregistered data")
	}
	if got := raw.Get(h.ctx, physical).Val(); got != string(before) {
		t.Fatal("rejected reference change modified descriptor")
	}
	assertSnapshotDataReferences(t, h, s)
	h.expectRows("SELECT v FROM t", "9007199254740993")
}

func TestSnapshotDataReferencesFollowMetadataAndCompaction(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("INSERT INTO a VALUES(1)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO b VALUES(2)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	for i := 0; i < 4; i++ {
		h.exec(fmt.Sprintf("UPDATE a SET v=%d", 10+i))
		h.exec(fmt.Sprintf("COMMENT ON TABLE b IS 'version %d'", i))
		assertSnapshotDataReferences(t, h, s)
	}
	snapshotDrainCompactionTest(t, s)
	assertSnapshotDataReferences(t, h, s)
	h.exec("TRUNCATE b")
	h.exec("DROP TABLE a")
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
	got := snapshotExecuteTest(t, s, ctx, "SELECT a.v,b.v FROM a CROSS JOIN b")
	if len(got.rows) != 1 || got.rows[0][0].I != 1 || got.rows[0][1].I != 2 {
		t.Fatal("old pin lost compacted/truncated sources", got.rows)
	}
	pin.release()
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotDataReferencesMissingCopyEdgesFailClosed(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(7)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	suffix := strings.TrimPrefix(metaKey("public", "t"), metaPrefix)
	source, err := resolveSnapshotMetadata(ctx, suffix, false)
	if err != nil {
		t.Fatal(err)
	}
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	before := raw.Get(h.ctx, source).Val()
	if err := raw.HDel(h.ctx, snapshotMetadataGraph, "t:"+source).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSnapshotMetadata(wctx, suffix, true); err == nil || !strings.Contains(err.Error(), "descriptor references missing") {
		t.Fatal("copy accepted missing row ownership", err)
	}
	if raw.Exists(h.ctx, snapshotWorkspace(id)+suffix).Val() != 0 {
		t.Fatal("failed copy created an unprotected descriptor")
	}
	if got := raw.Get(h.ctx, source).Val(); got != before {
		t.Fatal("failed copy modified source")
	}
}
