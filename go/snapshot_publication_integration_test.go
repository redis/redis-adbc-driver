// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func snapshotStageSQL(t *testing.T, s *store, ctx context.Context, target, sql string) (context.Context, *snapshotPin, string) {
	t.Helper()
	base, pin, err := s.acquireSnapshot(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	wctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		snapshotAdmissionDiagnostics(t, s, ctx)
		pin.release()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.abortSnapshot(pin, id); pin.release() })
	if target != "" {
		if err := s.prepareSnapshotTable(wctx, "public", target); err != nil {
			t.Fatal(err)
		}
	}
	snapshotExecuteTest(t, s, wctx, sql)
	return wctx, pin, id
}

func TestSnapshotIndependentPublicationMergesCollidingBuckets(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	first := "independent_a"
	slot := snapshotMetadataSlot(metaKey("public", first)[len(metaPrefix):])
	second := ""
	for i := 0; i < 10000; i++ {
		candidate := fmt.Sprintf("independent_%d", i)
		if snapshotMetadataSlot(metaKey("public", candidate)[len(metaPrefix):]) == slot {
			second = candidate
			break
		}
	}
	if second == "" {
		t.Fatal("no colliding table descriptor found")
	}
	h.exec("CREATE TABLE " + first + "(v INTEGER)")
	h.exec("CREATE TABLE " + second + "(v INTEGER)")
	h.exec("INSERT INTO " + first + " VALUES(1)")
	h.exec("INSERT INTO " + second + " VALUES(10)")
	s := snapshotTestStore(t, raw)
	old, oldPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer oldPin.release()
	w1, p1, id1 := snapshotStageSQL(t, s, h.ctx, first, "UPDATE "+first+" SET v=v+1")
	w2, p2, id2 := snapshotStageSQL(t, s, h.ctx, second, "UPDATE "+second+" SET v=v+1")
	if err := s.publishSnapshot(w1, p1, id1); err != nil {
		t.Fatal(err)
	}
	if err := s.publishSnapshot(w2, p2, id2); err != nil {
		t.Fatalf("unrelated write conflicted: %v", err)
	}
	h.expectRows("SELECT v FROM "+first, "2")
	h.expectRows("SELECT v FROM "+second, "11")
	p1.release()
	p2.release()
	snapshotReapTest(s, h.ctx)
	got := snapshotExecuteTest(t, s, old, "SELECT a.v,b.v FROM "+first+" a CROSS JOIN "+second+" b")
	if len(got.rows) != 1 || got.rows[0][0].I != 1 || got.rows[0][1].I != 10 {
		t.Fatal("old generation changed", got.rows)
	}
	assertSnapshotDataReferences(t, h, s)
	oldPin.release()
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotIndependentPublicationReadSourceConflicts(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO b VALUES(10)")
	s := snapshotTestStore(t, raw)
	w1, p1, id1 := snapshotStageSQL(t, s, h.ctx, "a", "INSERT INTO a SELECT v FROM b")
	w2, p2, id2 := snapshotStageSQL(t, s, h.ctx, "b", "UPDATE b SET v=20")
	if err := s.publishSnapshot(w2, p2, id2); err != nil {
		t.Fatal(err)
	}
	var ae adbc.Error
	if err := s.publishSnapshot(w1, p1, id1); !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" {
		t.Fatal("changed read source did not conflict", err)
	}
	h.expectRows("SELECT COUNT(*) FROM a", "0")
	h.expectRows("SELECT v FROM b", "20")
}

func TestSnapshotIndependentPublicationPreservesPreWorkspaceDependencies(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO b VALUES(10)")
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	if _, err := s.getTable(base, "public", "b"); err != nil {
		t.Fatal(err)
	}
	wctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.prepareSnapshotTable(wctx, "public", "a"); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, wctx, "INSERT INTO a VALUES(10)")
	h.exec("UPDATE b SET v=20")
	var ae adbc.Error
	if err := s.publishSnapshot(wctx, pin, id); !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" {
		t.Fatal("pre-workspace dependency was forgotten", err)
	}
	h.expectRows("SELECT COUNT(*) FROM a", "0")
}

func TestSnapshotIndependentPublicationStaleBaseCanStageOnce(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO a VALUES(1)")
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	h.exec("INSERT INTO b VALUES(10)")
	wctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal("unrelated publication prevented staging", err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.prepareSnapshotTable(wctx, "public", "a"); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, wctx, "UPDATE a SET v=v+1")
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM a", "2")
	h.expectRows("SELECT v FROM b", "10")
}

func TestSnapshotIndependentPublicationSealedOutputsRejectMutation(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("INSERT INTO a VALUES(1)")
	s := snapshotTestStore(t, raw)
	wctx, pin, id := snapshotStageSQL(t, s, h.ctx, "a", "UPDATE a SET v=2")
	meta, err := s.getTable(wctx, "public", "a")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.snapshotControl(wctx, "seal", pin.token, id, pin.root).Int(); err != nil || n != 1 {
		t.Fatal("seal failed", n, err)
	}
	if err := s.redis(wctx).HSet(wctx, meta.prefix()+"1", "v", "999").Err(); err == nil {
		t.Fatal("sealed row was mutable")
	}
	// A sealed workspace remains renewable, survives GC, and can be aborted.
	if n, err := s.snapshotControl(wctx, "renew", pin.token, id, pin.root).Int(); err != nil || n != 1 {
		t.Fatal("sealed lease could not renew", n, err)
	}
	snapshotReapTest(s, h.ctx)
	if raw.HGet(h.ctx, snapshotCatalogKey, "w:"+id).Val() == "" {
		t.Fatal("live sealed workspace was reclaimed")
	}
	s.abortSnapshot(pin, id)
	snapshotReapTest(s, h.ctx)
	if raw.HExists(h.ctx, snapshotCatalogKey, "w:"+id).Val() {
		t.Fatal("sealed abort leaked workspace")
	}
	h.expectRows("SELECT v FROM a", "1")
}

// Inject only at merge publication, after ordinary publication has returned its
// known CAS refusal. No SQL or input is replayed by the driver.
type snapshotMergePublicationHook struct {
	snapshotHook
	merges      atomic.Int64
	before      func()
	beforeMerge func(snapshotMergeBase)
	lose        bool
	lost        atomic.Bool
}

func (h *snapshotMergePublicationHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := snapshotObservedControlArgs(cmd)
		merge := false
		if len(args) > 2 && args[1] == snapshotControl {
			count, err := strconv.Atoi(fmt.Sprint(args[2]))
			op := 3 + count
			merge = err == nil && op+5 < len(args) && args[op] == "publish" && fmt.Sprint(args[op+5]) != ""
		}
		if merge && h.merges.Add(1) == 1 {
			if h.before != nil {
				h.before()
			}
			if h.beforeMerge != nil {
				count, _ := strconv.Atoi(fmt.Sprint(args[2]))
				var base snapshotMergeBase
				if err := json.Unmarshal([]byte(fmt.Sprint(args[3+count+5])), &base); err != nil {
					return err
				}
				h.beforeMerge(base)
			}
		}
		err := next(ctx, cmd)
		if merge && h.lose && err == nil && h.lost.CompareAndSwap(false, true) {
			cmd.SetErr(io.ErrUnexpectedEOF)
			return io.ErrUnexpectedEOF
		}
		return err
	}
}
func TestSnapshotIndependentPublicationRetriesOnlyMetadataCAS(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	for _, name := range []string{"a", "b", "c"} {
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
	}
	h.exec("INSERT INTO a VALUES(1)")
	h.exec("INSERT INTO b VALUES(10)")
	h.exec("INSERT INTO c VALUES(100)")
	s := snapshotTestStore(t, raw)
	w1, p1, id1 := snapshotStageSQL(t, s, h.ctx, "a", "UPDATE a SET v=v+1")
	w2, p2, id2 := snapshotStageSQL(t, s, h.ctx, "b", "UPDATE b SET v=v+1")
	if err := s.publishSnapshot(w1, p1, id1); err != nil {
		t.Fatal(err)
	}
	observer := &snapshotMergePublicationHook{before: func() { h.exec("UPDATE c SET v=v+1") }}
	s.client.AddHook(observer)
	if err := s.publishSnapshot(w2, p2, id2); err != nil {
		t.Fatal(err)
	}
	if observer.merges.Load() < 2 {
		t.Fatal("test did not force a second metadata CAS")
	}
	h.expectRows("SELECT a.v,b.v,c.v FROM a CROSS JOIN b CROSS JOIN c", "2|11|101")
	p1.release()
	p2.release()
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}
func TestSnapshotIndependentPublicationResolvesLostMergeReply(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO a VALUES(1)")
	h.exec("INSERT INTO b VALUES(10)")
	s := snapshotTestStore(t, raw)
	w1, p1, id1 := snapshotStageSQL(t, s, h.ctx, "a", "UPDATE a SET v=v+1")
	w2, p2, id2 := snapshotStageSQL(t, s, h.ctx, "b", "UPDATE b SET v=v+1")
	if err := s.publishSnapshot(w1, p1, id1); err != nil {
		t.Fatal(err)
	}
	observer := &snapshotMergePublicationHook{lose: true}
	s.client.AddHook(observer)
	if err := s.publishSnapshot(w2, p2, id2); err != nil {
		t.Fatal(err)
	}
	if !observer.lost.Load() || observer.merges.Load() != 1 {
		t.Fatal("merge publication was not lost exactly once")
	}
	h.expectRows("SELECT a.v,b.v FROM a CROSS JOIN b", "2|11")
	p1.release()
	p2.release()
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotIndependentPublicationConcurrentWriters(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	for _, name := range []string{"a", "b", "c", "d"} {
		h.exec("CREATE TABLE " + name + "(v INTEGER)")
		h.exec("INSERT INTO " + name + " VALUES(0)")
	}
	s := snapshotTestStore(t, raw)
	type staged struct {
		ctx context.Context
		pin *snapshotPin
		id  string
	}
	var writes []staged
	for _, name := range []string{"a", "b", "c", "d"} {
		ctx, pin, id := snapshotStageSQL(t, s, h.ctx, name, "UPDATE "+name+" SET v=v+1")
		writes = append(writes, staged{ctx, pin, id})
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	failures := make(chan error, len(writes))
	for _, w := range writes {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; failures <- s.publishSnapshot(w.ctx, w.pin, w.id) }()
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Error("unrelated concurrent writer failed", err)
		}
	}
	h.expectRows("SELECT a.v,b.v,c.v,d.v FROM a CROSS JOIN b CROSS JOIN c CROSS JOIN d", "1|1|1|1")
	for _, w := range writes {
		w.pin.release()
	}
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotIndependentPublicationMergesDescriptorDeletion(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE a(v INTEGER)")
	h.exec("CREATE TABLE b(v INTEGER)")
	h.exec("INSERT INTO a VALUES(1)")
	h.exec("INSERT INTO b VALUES(10)")
	s := snapshotTestStore(t, raw)
	drop, p1, id1 := snapshotStageSQL(t, s, h.ctx, "", "DROP TABLE a")
	update, p2, id2 := snapshotStageSQL(t, s, h.ctx, "b", "UPDATE b SET v=v+1")
	if err := s.publishSnapshot(update, p2, id2); err != nil {
		t.Fatal(err)
	}
	if err := s.publishSnapshot(drop, p1, id1); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='a'", "0")
	h.expectRows("SELECT v FROM b", "11")
	p1.release()
	p2.release()
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotIndependentPublicationRequiresBothPins(t *testing.T) {
	for _, which := range []string{"original", "latest"} {
		t.Run(which, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE a(v INTEGER)")
			h.exec("CREATE TABLE b(v INTEGER)")
			h.exec("INSERT INTO a VALUES(1)")
			h.exec("INSERT INTO b VALUES(10)")
			s := snapshotTestStore(t, raw)
			w1, p1, id1 := snapshotStageSQL(t, s, h.ctx, "a", "UPDATE a SET v=v+1")
			w2, p2, id2 := snapshotStageSQL(t, s, h.ctx, "b", "UPDATE b SET v=v+1")
			if err := s.publishSnapshot(w1, p1, id1); err != nil {
				t.Fatal(err)
			}
			observer := &snapshotMergePublicationHook{beforeMerge: func(base snapshotMergeBase) {
				if which == "original" {
					p2.release()
				} else if err := raw.HDel(h.ctx, snapshotCatalogKey, "p:"+base.Token).Err(); err != nil {
					t.Fatal(err)
				}
			}}
			s.client.AddHook(observer)
			if err := s.publishSnapshot(w2, p2, id2); err == nil {
				t.Fatal("merge committed without " + which + " pin")
			}
			if observer.merges.Load() != 1 {
				t.Fatal("pin was not invalidated at merge publication")
			}
			if root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); root != id1 {
				t.Fatal("failed merge changed root")
			}
			h.expectRows("SELECT a.v,b.v FROM a CROSS JOIN b", "2|10")
			s.abortSnapshot(p2, id2)
			p1.release()
			p2.release()
			snapshotReapTest(s, h.ctx)
			assertSnapshotDataReferences(t, h, s)
		})
	}
}
