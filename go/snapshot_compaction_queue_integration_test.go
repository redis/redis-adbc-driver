// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSnapshotCompactionIdleDoesNotReadTableDescriptors(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	guard := &snapshotUnrelatedMetadataCopies{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("idle_%d", i)
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
		guard.suffixes = append(guard.suffixes, strings.TrimPrefix(metaKey("public", name), metaPrefix))
	}
	s := snapshotTestStore(t, raw)
	s.client.AddHook(guard)
	if started, err := s.startSnapshotCompaction(h.ctx, snapshotID()); err != nil || started {
		t.Fatal("idle worker unexpectedly started", started, err)
	}
	if reads := guard.reads.Load(); reads != 0 {
		t.Fatalf("idle compaction read %d unrelated descriptors", reads)
	}
}

func snapshotQueuedTable(t *testing.T, h *sqlHarness, name string) string {
	t.Helper()
	h.exec("CREATE TABLE " + name + "(v INTEGER)")
	h.exec("INSERT INTO " + name + " VALUES(1)")
	h.exec("UPDATE " + name + " SET v=2")
	h.exec("UPDATE " + name + " SET v=3")
	h.exec("UPDATE " + name + " SET v=4")
	return strings.TrimPrefix(metaKey("public", name), metaPrefix)
}
func TestSnapshotCompactionQueuePublishesAtomicallyAndPreservesAge(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	h.exec("UPDATE t SET v=2")
	h.exec("UPDATE t SET v=3")
	key := strings.TrimPrefix(metaKey("public", "t"), metaPrefix)
	s := snapshotTestStore(t, raw)
	_, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=4")
	if raw.ZCard(h.ctx, snapshotCompactionQueue).Val() != 0 {
		t.Fatal("private staging enqueued work")
	}
	s.abortSnapshot(pin, id)
	pin.release()
	if raw.HExists(h.ctx, snapshotCompactionCandidates, key).Val() {
		t.Fatal("abort published a candidate")
	}
	h.exec("UPDATE t SET v=4")
	prior, err := raw.HGet(h.ctx, snapshotCompactionCandidates, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	age, err := raw.ZScore(h.ctx, snapshotCompactionQueue, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	h.exec("COMMENT ON TABLE t IS 'new descriptor'")
	current := raw.HGet(h.ctx, snapshotCompactionCandidates, key).Val()
	if current == prior {
		t.Fatal("eligible publication did not advance version")
	}
	if got := raw.ZScore(h.ctx, snapshotCompactionQueue, key).Val(); got != age {
		t.Fatal("hot publication reset queue age")
	}
	if n, err := s.snapshotControl(h.ctx, "compact_discard", "", key, "", prior).Int(); err != nil || n != 0 {
		t.Fatal("stale discard changed newer work", n, err)
	}
	if raw.HGet(h.ctx, snapshotCompactionCandidates, key).Val() != current {
		t.Fatal("stale discard removed current candidate")
	}
	h.exec("TRUNCATE t")
	if raw.ZCard(h.ctx, snapshotCompactionQueue).Val() != 0 || raw.HExists(h.ctx, snapshotCompactionCandidates, key).Val() {
		t.Fatal("truncate left an obsolete candidate")
	}
	// Direct malformed maintenance input must fail before the root changes.
	base, p, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.release()
	bad, badID, err := s.beginSnapshotWrite(base, p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(p, badID)
	before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := s.snapshotControl(bad, "publish", p.token, badID, p.root, "", "", "", "{").Err(); err == nil {
		t.Fatal("malformed queue update accepted")
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
		t.Fatal("invalid queue input changed root")
	}
}
func TestSnapshotCompactionQueueClaimsOnlyTargetAndRecoversLease(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	guard := &snapshotUnrelatedMetadataCopies{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("unrelated_%d", i)
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
		guard.suffixes = append(guard.suffixes, strings.TrimPrefix(metaKey("public", name), metaPrefix))
	}
	key := snapshotQueuedTable(t, h, "target")
	s := snapshotTestStore(t, raw)
	s.client.AddHook(guard)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("queued job did not start", started, err)
	}
	if guard.reads.Load() != 0 {
		t.Fatal("queued discovery read unrelated descriptors", guard.reads.Load())
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.QueueKey != key {
		t.Fatal("wrong durable job", job, err)
	}
	if raw.HExists(h.ctx, snapshotCompactionCandidates, key).Val() || raw.ZCard(h.ctx, snapshotCompactionQueue).Val() != 0 {
		t.Fatal("claim did not consume candidate")
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	recovered := snapshotTestStore(t, raw)
	snapshotDrainCompactionTest(t, recovered)
	h.expectRows("SELECT v FROM target", "4")
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}
func TestSnapshotCompactionQueueDefersBrokenSourceWithoutBlockingOthers(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	badKey := snapshotQueuedTable(t, h, "bad")
	snapshotQueuedTable(t, h, "healthy")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	bad := snapshotTableTest(t, s, ctx, "bad")
	pin.release()
	if err := raw.Do(h.ctx, "FT.DROPINDEX", bad.index()).Err(); err != nil {
		t.Fatal(err)
	}
	if started, err := s.startSnapshotCompaction(h.ctx, snapshotID()); err != nil || !started {
		t.Fatal("broken source blocked healthy candidate", started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.Source.Name != "healthy" {
		t.Fatal("wrong job after broken source", job, err)
	}
	if !raw.HExists(h.ctx, snapshotCompactionCandidates, badKey).Val() {
		t.Fatal("failed source was silently dropped")
	}
	for i := 0; i < 100; i++ {
		job, err = s.readSnapshotCompaction(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if _, err := s.snapshotCompactionStep(h.ctx, job.Token); err != nil {
			t.Fatal(err)
		}
	}
	h.expectRows("SELECT v FROM healthy", "4")
}

type snapshotQueuePeekRace struct {
	op string
	snapshotHook
	once      atomic.Bool
	beforePin func()
}

func (h *snapshotQueuePeekRace) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		if len(a) > 2 && a[1] == snapshotControl {
			count, err := strconv.Atoi(fmt.Sprint(a[2]))
			op := 3 + count
			expected := h.op
			if expected == "" {
				expected = "acquire"
			}
			if err == nil && op < len(a) && a[op] == expected && h.once.CompareAndSwap(false, true) {
				h.beforePin()
			}
		}
		return next(ctx, cmd)
	}
}
func TestSnapshotCompactionQueueStalePeekCannotRemoveRecreatedTable(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	key := snapshotQueuedTable(t, h, "t")
	previous := raw.HGet(h.ctx, snapshotCompactionCandidates, key).Val()
	s := snapshotTestStore(t, raw)
	race := &snapshotQueuePeekRace{beforePin: func() { h.exec("DROP TABLE t"); snapshotQueuedTable(t, h, "t") }}
	s.client.AddHook(race)
	if started, err := s.startSnapshotCompaction(h.ctx, snapshotID()); err != nil || started {
		t.Fatal("stale peek started a job", started, err)
	}
	current := raw.HGet(h.ctx, snapshotCompactionCandidates, key).Val()
	if current == "" || current == previous {
		t.Fatal("recreated candidate was removed or unchanged")
	}
	var candidate snapshotCompactionCandidate
	if err := json.Unmarshal([]byte(current), &candidate); err != nil {
		t.Fatal(err)
	}
	if started, err := s.startSnapshotCompaction(h.ctx, snapshotID()); err != nil || !started {
		t.Fatal("recreated candidate could not start", started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.QueueVersion != candidate.Version {
		t.Fatal("job used a stale descriptor", job, err)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, job.Token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT v FROM t", "4")
}

func TestSnapshotCompactionQueueSustainsTwoTablesWithoutCallerRetries(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	for _, name := range []string{"a", "b"} {
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
	}
	h.exec("INSERT INTO a VALUES(0)")
	h.exec("INSERT INTO b VALUES(10)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	for i := 0; i < 64; i++ {
		name := "a"
		if i%2 != 0 {
			name = "b"
		}
		h.exec("UPDATE " + name + " SET v=v+1")
	}
	h.expectRows("SELECT a.v,b.v FROM a CROSS JOIN b", "32|42")
	got := snapshotExecuteTest(t, s, old, "SELECT a.v,b.v FROM a CROSS JOIN b")
	if len(got.rows) != 1 || got.rows[0][0].I != 0 || got.rows[0][1].I != 10 {
		t.Fatal("pinned generation changed", got.rows)
	}
}

func TestSnapshotCompactionQueueClaimsPrefixAcrossConcurrentAppend(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	snapshotQueuedTable(t, h, "t")
	s := snapshotTestStore(t, raw)
	race := &snapshotQueuePeekRace{op: "compact_start", beforePin: func() { h.exec("UPDATE t SET v=5") }}
	s.client.AddHook(race)
	if started, err := s.startSnapshotCompaction(h.ctx, snapshotID()); err != nil || !started {
		t.Fatal("new tail postponed an eligible source prefix", started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || len(job.References) != 4 {
		t.Fatal("worker did not retain the original four-source prefix", job, err)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, job.Token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT v FROM t", "5")
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	current := snapshotTableTest(t, s, ctx, "t")
	if len(current.snapshotSegments()) != 2 {
		t.Fatal("compaction lost or recopied the newer tail", len(current.snapshotSegments()))
	}
	if raw.ZCard(h.ctx, snapshotCompactionQueue).Val() != 0 {
		t.Fatal("short installed lineage remained queued")
	}
}
