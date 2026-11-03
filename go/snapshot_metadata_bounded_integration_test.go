// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotBucketAccess struct {
	snapshotHook
	raw      *goredis.Client
	whole    atomic.Int64
	calls    atomic.Int64
	maxAdded atomic.Int64
}

func (h *snapshotBucketAccess) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if len(a) > 1 && strings.EqualFold(fmt.Sprint(a[0]), "HGETALL") && strings.HasPrefix(fmt.Sprint(a[1]), snapshotPrefix+"metadata:bucket:") {
			h.whole.Add(1)
		}
		copying := len(a) > 6 && a[1] == snapshotMetadataCloneBucket
		var before int64
		if copying {
			before = h.raw.HLen(ctx, fmt.Sprint(a[6])).Val()
		}
		err := next(ctx, cmd)
		if copying && err == nil {
			h.calls.Add(1)
			added := h.raw.HLen(ctx, fmt.Sprint(a[6])).Val() - before
			for current := h.maxAdded.Load(); added > current; current = h.maxAdded.Load() {
				if h.maxAdded.CompareAndSwap(current, added) {
					break
				}
			}
		}
		return err
	}
}
func TestSnapshotMetadataLookupDoesNotReadWholeBuckets(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(7)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotBucketAccess{raw: raw}
	s.client.AddHook(guard)
	got := snapshotExecuteTest(t, s, h.ctx, "SELECT v FROM t")
	if len(got.rows) != 1 || got.rows[0][0].I != 7 {
		t.Fatal(got.rows)
	}
	if guard.whole.Load() != 0 {
		t.Fatalf("point metadata lookup read %d entire buckets", guard.whole.Load())
	}
}
func seedSnapshotMetadataBucket(t *testing.T, h *sqlHarness, s *store, count int) (string, []string) {
	t.Helper()
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
	slot := snapshotMetadataSlot("test:bounded:0")
	var keys []string
	for i := 0; len(keys) < count; i++ {
		suffix := fmt.Sprintf("test:bounded:%d", i)
		if snapshotMetadataSlot(suffix) != slot {
			continue
		}
		keys = append(keys, suffix)
		if err := s.redis(wctx).Set(wctx, metaPrefix+suffix, fmt.Sprintf("value-%d", len(keys)), 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	return slot, keys
}
func TestSnapshotMetadataBucketCopyHasBoundedSteps(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	_, keys := seedSnapshotMetadataBucket(t, h, s, 193)
	guard := &snapshotBucketAccess{raw: raw}
	s.client.AddHook(guard)
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
	if err := s.redis(wctx).Set(wctx, metaPrefix+keys[0], "changed", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if guard.maxAdded.Load() > 64 || guard.calls.Load() < 4 {
		t.Fatalf("unbounded copy: %d steps, maximum %d fields in one step", guard.calls.Load(), guard.maxAdded.Load())
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	check, checkPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer checkPin.release()
	for i, key := range keys {
		want := fmt.Sprintf("value-%d", i+1)
		if i == 0 {
			want = "changed"
		}
		if got, err := s.redis(check).Get(check, metaPrefix+key).Result(); err != nil || got != want {
			t.Fatalf("lost copied metadata %s: %q %v", key, got, err)
		}
	}
}

func TestSnapshotMetadataBucketConstructionResumesAfterScopeLoss(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	slot, keys := seedSnapshotMetadataBucket(t, h, s, 193)
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
	source := raw.HGet(h.ctx, snapshotMetadataMap(id), slot).Val()
	if n, err := s.snapshotMetadataBucketStep(wctx, slot, source); err != nil || n != 0 {
		t.Fatal("expected partial first step", n, err)
	}
	if got := raw.HLen(h.ctx, snapshotMetadataBucket(id, slot)).Val(); got != 64 {
		t.Fatal("unexpected first batch size", got)
	}
	checkpoint := raw.HGet(h.ctx, snapshotMetadataGraph, "c:"+id+":"+slot).Val()
	if checkpoint == "" {
		t.Fatal("construction checkpoint missing")
	}
	// Incomplete metadata cannot be exposed, even through direct publication.
	if err := s.snapshotControl(wctx, "publish", pin.token, id, pin.root).Err(); err == nil {
		t.Fatal("published an incomplete metadata bucket")
	}
	// A replacement scope models losing local construction/cache progress. SQL
	// execution is not resumed; only this private metadata construction is resumed.
	resumed := context.WithValue(wctx, snapshotContextKey{}, &snapshotScope{id: id, pin: pin, store: s, write: true, owned: map[string]bool{}})
	got, err := s.redis(resumed).Get(resumed, metaPrefix+keys[len(keys)-1]).Result()
	if err != nil || got != "value-193" {
		t.Fatal("resume lost an uncopied field", got, err)
	}
	if raw.HLen(h.ctx, snapshotMetadataBucket(id, slot)).Val() != 193 || raw.ZCard(h.ctx, snapshotMetadataDirectory(id, slot)).Val() != 193 {
		t.Fatal("resumed bucket incomplete")
	}
	if raw.HExists(h.ctx, snapshotMetadataGraph, "c:"+id+":"+slot).Val() {
		t.Fatal("completed construction retained checkpoint")
	}
	var work struct {
		Builds int `json:"metadata_builds"`
	}
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+id).Val()), &work); err != nil || work.Builds != 0 {
		t.Fatal("construction count did not drain", work, err)
	}
	for _, key := range keys {
		physical := snapshotWorkspace(source) + key
		if count := raw.HGet(h.ctx, snapshotMetadataGraph, "v:"+physical).Val(); count != "2" {
			t.Fatal("resume duplicated or lost an object reference", count)
		}
	}
	s.abortSnapshot(pin, id)
	pin.release()
	snapshotReapTest(s, h.ctx)
	for _, key := range keys {
		if count := raw.HGet(h.ctx, snapshotMetadataGraph, "v:"+snapshotWorkspace(source)+key).Val(); count != "1" {
			t.Fatal("aborted resumed bucket leaked object reference", count)
		}
	}
}

func TestSnapshotMetadataPartialBucketRetainsSourceAfterBaseRetires(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	slot, keys := seedSnapshotMetadataBucket(t, h, s, 193)
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
	source := raw.HGet(h.ctx, snapshotMetadataMap(id), slot).Val()
	if n, err := s.snapshotMetadataBucketStep(wctx, slot, source); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	pin.release()
	// Replace this source bucket in the committed catalog, then retire its root.
	next, nextPin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	changed, changedID, err := s.beginSnapshotWrite(next, nextPin)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.redis(changed).Set(changed, metaPrefix+keys[0], "new-current", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.publishSnapshot(changed, nextPin, changedID); err != nil {
		t.Fatal(err)
	}
	nextPin.release()
	snapshotReapTest(s, h.ctx)
	if raw.HExists(h.ctx, snapshotCatalogKey, "w:"+source).Val() {
		t.Fatal("original source manifest did not retire")
	}
	if raw.HLen(h.ctx, snapshotMetadataBucket(source, slot)).Val() != 193 {
		t.Fatal("construction lost its source bucket")
	}
	if count := raw.HGet(h.ctx, snapshotMetadataGraph, "b:"+source+":"+slot).Val(); count != "1" {
		t.Fatal("source should have only the construction edge", count)
	}
	// The work lease remains live slightly beyond the stopped base pin. Its
	// source edge protects all remaining native values until abort or completion.
	if n, err := s.snapshotMetadataBucketStep(wctx, slot, source); err != nil || n != 0 {
		t.Fatal("retired source could not supply next batch", n, err)
	}
	s.abortSnapshot(pin, id)
	snapshotReapTest(s, h.ctx)
	if raw.Exists(h.ctx, snapshotMetadataBucket(id, slot), snapshotMetadataDirectory(id, slot), snapshotMetadataBucket(source, slot), snapshotMetadataDirectory(source, slot)).Val() != 0 {
		t.Fatal("partial target/source metadata leaked after abort")
	}
	if raw.HExists(h.ctx, snapshotMetadataGraph, "c:"+id+":"+slot).Val() {
		t.Fatal("aborted construction edge leaked")
	}
}

func TestSnapshotMetadataPartialBucketExpiredWriterIsFenced(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	slot, _ := seedSnapshotMetadataBucket(t, h, s, 129)
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	source := raw.HGet(h.ctx, snapshotMetadataMap(id), slot).Val()
	if n, err := s.snapshotMetadataBucketStep(wctx, slot, source); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	var work map[string]any
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+id).Val()), &work); err != nil {
		t.Fatal(err)
	}
	work["expires"] = time.Now().Add(-time.Hour).UnixMilli()
	value, _ := json.Marshal(work)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+id, value).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.snapshotMetadataBucketStep(wctx, slot, source); err == nil {
		t.Fatal("expired construction advanced")
	}
	pin.release()
	snapshotReapTest(s, h.ctx)
	if _, err := s.snapshotMetadataBucketStep(wctx, slot, source); err == nil {
		t.Fatal("reclaimed construction resurrected")
	}
	if raw.Exists(h.ctx, snapshotMetadataBucket(id, slot), snapshotMetadataDirectory(id, slot)).Val() != 0 {
		t.Fatal("expired partial bucket survived")
	}
	if raw.HExists(h.ctx, snapshotMetadataGraph, "c:"+id+":"+slot).Val() {
		t.Fatal("expired construction retained source edge")
	}
}

func TestSnapshotMetadataBucketCopyBoundsFieldBytes(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "test:wide:" + strings.Repeat("x", 16*1024)
	slot := snapshotMetadataSlot(prefix + "0")
	var keys []string
	for i := 0; len(keys) < 65; i++ {
		suffix := prefix + fmt.Sprint(i)
		if snapshotMetadataSlot(suffix) != slot {
			continue
		}
		keys = append(keys, suffix)
		if err := s.redis(wctx).Set(wctx, metaPrefix+suffix, "value", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	pin.release()
	guard := &snapshotBucketAccess{raw: raw}
	s.client.AddHook(guard)
	next, nextPin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer nextPin.release()
	changed, changedID, err := s.beginSnapshotWrite(next, nextPin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(nextPin, changedID)
	if err := s.redis(changed).Set(changed, metaPrefix+keys[0], "changed", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if guard.calls.Load() < 2 || guard.maxAdded.Load() > 63 {
		t.Fatalf("byte budget ignored: %d calls, maximum %d wide fields", guard.calls.Load(), guard.maxAdded.Load())
	}
	if err := s.publishSnapshot(changed, nextPin, changedID); err != nil {
		t.Fatal(err)
	}
}

type snapshotBucketLostStep struct {
	snapshotHook
	lost atomic.Bool
}

func (h *snapshotBucketLostStep) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		a := cmd.Args()
		if err == nil && len(a) > 1 && a[1] == snapshotMetadataCloneBucket && h.lost.CompareAndSwap(false, true) {
			cmd.SetErr(io.ErrUnexpectedEOF)
			return io.ErrUnexpectedEOF
		}
		return err
	}
}
func TestSnapshotMetadataBucketLostStepReplyDoesNotDuplicateReferences(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	slot, keys := seedSnapshotMetadataBucket(t, h, s, 193)
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
	source := raw.HGet(h.ctx, snapshotMetadataMap(id), slot).Val()
	hook := &snapshotBucketLostStep{}
	s.client.AddHook(hook)
	if err := s.redis(wctx).Set(wctx, metaPrefix+keys[0], "changed", 0).Err(); err == nil {
		t.Fatal("test did not lose the first construction reply")
	}
	if raw.HLen(h.ctx, snapshotMetadataBucket(id, slot)).Val() != 64 {
		t.Fatal("uncertain step was automatically replayed")
	}
	// Explicitly resume only private metadata construction; no SQL/Arrow is run.
	if _, err := resolveSnapshotMetadata(wctx, keys[0], false); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if count := raw.HGet(h.ctx, snapshotMetadataGraph, "v:"+snapshotWorkspace(source)+key).Val(); count != "2" {
			t.Fatal("lost reply duplicated object references", count)
		}
	}
	s.abortSnapshot(pin, id)
	pin.release()
	snapshotReapTest(s, h.ctx)
	for _, key := range keys {
		if count := raw.HGet(h.ctx, snapshotMetadataGraph, "v:"+snapshotWorkspace(source)+key).Val(); count != "1" {
			t.Fatal("uncertain construction leaked object references", count)
		}
	}
}
