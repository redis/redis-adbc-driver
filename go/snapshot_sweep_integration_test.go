// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Build an actual private generation and retire only its metadata, leaving its
// zero-reference data/index for the public sweep API to preview and reclaim.
func snapshotSweepGarbage(t *testing.T, h *sqlHarness, s *store, ready bool) (*tableMeta, string) {
	return snapshotSweepGarbageRows(t, h, s, ready, 3)
}

func snapshotSweepGarbageRows(t *testing.T, h *sqlHarness, s *store, ready bool, count int) (*tableMeta, string) {
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
	meta := &tableMeta{Schema: "public", Name: "sweep_orphan", Columns: []columnMeta{{Name: "v", Type: typeInt32, Nullable: true, Indexed: true}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	rows := make([][]Value, count)
	for i := range rows {
		rows[i] = []Value{intValue(typeInt32, int64(i+1))}
	}
	if _, err := s.insertRows(wctx, meta, rows); err != nil {
		t.Fatal(err)
	}
	if err := s.rebuildSnapshotIndexes(wctx); err != nil {
		t.Fatal(err)
	}
	meta, err = s.getTable(wctx, "public", "sweep_orphan")
	if err != nil {
		t.Fatal(err)
	}
	s.abortSnapshot(pin, id)
	pin.release()
	if ready {
		claimed, err := s.snapshotControl(h.ctx, "claim", "", "", "").Text()
		if err != nil || claimed != id {
			t.Fatalf("retire private metadata: %q %v", claimed, err)
		}
		if err := s.retireSnapshotMetadata(h.ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := s.snapshotControl(h.ctx, "reclaimed", "", id, "").Err(); err != nil {
			t.Fatal(err)
		}
		for range 32 {
			if err := s.collectSnapshotMetadata(h.ctx); err != nil {
				t.Fatal(err)
			}
			refs, err := s.client.HGet(h.ctx, snapshotMetadataGraph, "r:"+snapshotDataID(meta.prefix())).Int64()
			if err != nil {
				t.Fatal(err)
			}
			if refs == 0 {
				return meta, id
			}
		}
		t.Fatal("test data did not become unreferenced")
	}
	return meta, id
}

func TestSnapshotSweepReadyDataReport(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprintf("apply_%t", apply), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			meta, _ := snapshotSweepGarbage(t, h, s, true)
			// Unregistered objects are never inferred to be owned from a prefix scan.
			if err := raw.HSet(h.ctx, meta.prefix()+"999", "app", "keep").Err(); err != nil {
				t.Fatal(err)
			}
			if err := raw.Set(h.ctx, meta.prefix()+"1000", "keep", 0).Err(); err != nil {
				t.Fatal(err)
			}
			if err := raw.HSet(h.ctx, "application:1", "v", "keep").Err(); err != nil {
				t.Fatal(err)
			}
			if err := raw.Do(h.ctx, "FT.CREATE", "application_sweep_source", "ON", "HASH", "PREFIX", 1, "application:", "SCHEMA", "v", "TAG").Err(); err != nil {
				t.Fatal(err)
			}
			res, err := SweepReleased(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, SweepOptions{Apply: apply})
			if err != nil {
				t.Fatal(err)
			}
			if !apply && raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Val() != 3 {
				t.Fatal("dry-run connection deleted snapshot rows")
			}
			wantDeleted := int64(0)
			if apply {
				wantDeleted = 3
			}
			got := swept(res, meta.prefix())
			if res.Found != 3 || res.Deleted != wantDeleted || got.Found != 3 || got.Deleted != wantDeleted {
				t.Fatalf("snapshot sweep lost actual row counts: %+v prefix=%+v", res, got)
			}
			if res.Applied != apply || !res.ManagedSnapshots || res.More || res.MetadataPending {
				t.Fatalf("incorrect managed maintenance report: %+v", res)
			}
			if apply {
				if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3", snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 0 {
					t.Fatal("eligible row generation not reclaimed")
				}
				if err := raw.Do(h.ctx, "FT.INFO", meta.index()).Err(); err == nil || !isUnknownIndex(err) {
					t.Fatal("eligible private index not reclaimed", err)
				}
			} else if err := raw.Do(h.ctx, "FT.INFO", meta.index()).Err(); err != nil {
				t.Fatal("dry run dropped the private index", err)
			}
			if raw.HGet(h.ctx, meta.prefix()+"999", "app").Val() != "keep" || raw.Get(h.ctx, meta.prefix()+"1000").Val() != "keep" || raw.HGet(h.ctx, "application:1", "v").Val() != "keep" {
				t.Fatal("sweep changed unregistered/application objects")
			}
			if err := raw.Do(h.ctx, "FT.INFO", "application_sweep_source").Err(); err != nil {
				t.Fatal("sweep changed the application index", err)
			}
		})
	}
}

type snapshotSweepLateDrop struct {
	snapshotHook
	index string
	calls atomic.Int64
}

func (h *snapshotSweepLateDrop) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		err := next(ctx, cmd)
		if err == nil && len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "FT.DROPINDEX") && fmt.Sprint(args[1]) == h.index {
			h.calls.Add(1)
			return errors.New("injected late sweep index reply")
		}
		return err
	}
}

func TestSnapshotSweepLateFailureAndExplicitResume(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbage(t, h, s, true)
	c, err := dialHashesMode(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(h.ctx)
	hook := &snapshotSweepLateDrop{index: meta.index()}
	c.client.AddHook(hook)
	res, err := c.sweepSnapshots(c.context(h.ctx), SweepOptions{Apply: true}, &Sweep{Applied: true}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "injected late sweep index reply") || hook.calls.Load() != 1 || res == nil || !res.More || res.Deleted != 0 {
		t.Fatalf("late GC failure hidden/replayed: %+v %v calls=%d", res, err, hook.calls.Load())
	}
	if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Val() != 3 {
		t.Fatal("rows deleted after uncertain index reply")
	}
	resumed, err := SweepReleased(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, SweepOptions{Apply: true})
	if err != nil || resumed.Deleted != 3 || resumed.More {
		t.Fatalf("explicit GC resume failed: %+v %v", resumed, err)
	}
}

func TestSnapshotSweepPendingMetadata(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, id := snapshotSweepGarbage(t, h, s, false)
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	dry, err := SweepReleased(h.ctx, options, SweepOptions{})
	if err != nil || !dry.MetadataPending || !dry.More || dry.Deleted != 0 {
		t.Fatalf("pending metadata not reported: %+v %v", dry, err)
	}
	if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3", snapshotMetadataMap(id)).Val() != 4 {
		t.Fatal("preview retired data or metadata")
	}
	applied, err := SweepReleased(h.ctx, options, SweepOptions{Apply: true})
	if err != nil || applied.Deleted != 3 || applied.ManifestsRetired < 1 || applied.More {
		t.Fatalf("bounded metadata/data drain failed: %+v %v", applied, err)
	}
}

func TestSnapshotSweepProtectsPinnedAndCurrentRows(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE pinned(v INTEGER)")
	h.exec("INSERT INTO pinned VALUES(1),(2),(3)")
	h.exec("CREATE TABLE live(v INTEGER)")
	h.exec("INSERT INTO live VALUES(25)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(ctx, "public", "pinned")
	if err != nil {
		t.Fatal(err)
	}
	h.exec("TRUNCATE pinned")
	h.exec("INSERT INTO pinned VALUES(9)")
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	res, err := SweepReleased(h.ctx, options, SweepOptions{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if swept(res, old.prefix()).Deleted != 0 || raw.Exists(h.ctx, old.prefix()+"1", old.prefix()+"2", old.prefix()+"3").Val() != 3 {
		t.Fatal("sweep reclaimed pinned rows")
	}
	rows, err := s.fetchSnapshotVisible(ctx, old, []string{old.prefix() + "1", old.prefix() + "2", old.prefix() + "3"}, nil)
	if err != nil || len(rows) != 3 {
		t.Fatal("sweep invalidated old reader", err)
	}
	for i, row := range rows {
		if row["v"] != fmt.Sprint(i+1) {
			t.Fatal("pinned value changed")
		}
	}
	h.expectRows("SELECT v FROM pinned", "9")
	h.expectRows("SELECT v FROM live", "25")
	pin.release()
	res, err = SweepReleased(h.ctx, options, SweepOptions{Apply: true})
	if err != nil || swept(res, old.prefix()).Deleted != 3 {
		t.Fatalf("released pin was not reclaimed: %+v %v", res, err)
	}
	h.expectRows("SELECT v FROM pinned", "9")
	h.expectRows("SELECT v FROM live", "25")
}

func TestSnapshotSweepProtectsActiveWriter(t *testing.T) {
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
	meta := &tableMeta{Schema: "public", Name: "writer", Columns: []columnMeta{{Name: "v", Type: typeInt32, Indexed: true}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.insertRows(wctx, meta, [][]Value{{intValue(typeInt32, 7)}}); err != nil {
		t.Fatal(err)
	}
	res, err := SweepReleased(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, SweepOptions{Apply: true})
	if err != nil || swept(res, meta.prefix()).Deleted != 0 || raw.Exists(h.ctx, meta.prefix()+"1").Val() != 1 {
		t.Fatalf("active writer data reclaimed: %+v %v", res, err)
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal("writer could not publish after sweep", err)
	}
	h.expectRows("SELECT v FROM writer", "7")
}

func TestSnapshotSweepDirectoryFailureAndResume(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbage(t, h, s, true)
	directory := snapshotMembers(snapshotDataID(meta.prefix()))
	if err := raw.ZAdd(h.ctx, directory, goredis.Z{Score: 0, Member: "invalid"}).Err(); err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	for _, apply := range []bool{false, true} {
		report, err := SweepReleased(h.ctx, options, SweepOptions{Apply: apply})
		var ae adbc.Error
		if err == nil || !errors.As(err, &ae) || ae.Code != adbc.StatusIO || !strings.Contains(err.Error(), "invalid snapshot row directory member") || report == nil || !report.More || report.Deleted != 0 {
			t.Fatalf("corrupt directory did not fail closed: %+v %v", report, err)
		}
		if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Val() != 3 {
			t.Fatal("invalid directory deleted rows")
		}
	}
	if err := raw.ZRem(h.ctx, directory, "invalid").Err(); err != nil {
		t.Fatal(err)
	}
	report, err := SweepReleased(h.ctx, options, SweepOptions{Apply: true})
	if err != nil || report.Deleted != 3 || report.More {
		t.Fatalf("corrected directory could not resume: %+v %v", report, err)
	}
}

// Both collectors receive the same claim. The second observes its reply only
// after the first has completed retirement, reproducing the background-GC race.
type snapshotSweepClaimBarrier struct {
	snapshotHook
	calls     *atomic.Int64
	ready     chan struct{}
	firstDone chan struct{}
	second    bool
	seen      atomic.Bool
}

func (h *snapshotSweepClaimBarrier) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if err != nil || len(args) < 4 || !strings.EqualFold(fmt.Sprint(args[0]), "EVAL") {
			return err
		}
		n, _ := strconv.Atoi(fmt.Sprint(args[2]))
		if n < 0 || len(args) <= 3+n || fmt.Sprint(args[3+n]) != "claim" || !h.seen.CompareAndSwap(false, true) {
			return err
		}
		if h.calls.Add(1) == 2 {
			close(h.ready)
		}
		select {
		case <-h.ready:
		case <-ctx.Done():
			return ctx.Err()
		}
		if h.second {
			select {
			case <-h.firstDone:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
}
func TestSnapshotSweepConcurrentRetirement(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbage(t, h, s, false)
	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()
	calls := &atomic.Int64{}
	ready, firstDone := make(chan struct{}), make(chan struct{})
	clients := make([]*hashConn, 2)
	for i := range clients {
		c, err := dialHashesMode(ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, false)
		if err != nil {
			t.Fatal(err)
		}
		defer c.close(h.ctx)
		c.client.AddHook(&snapshotSweepClaimBarrier{calls: calls, ready: ready, firstDone: firstDone, second: i == 1})
		clients[i] = c
	}
	type result struct {
		report *Sweep
		err    error
	}
	done := make(chan result, 2)
	for i, c := range clients {
		go func() {
			report, err := c.sweepSnapshots(c.context(ctx), SweepOptions{Apply: true}, &Sweep{Applied: true}, func(string) {})
			if i == 0 {
				close(firstDone)
			}
			done <- result{report, err}
		}()
	}
	var deleted int64
	manifests := 0
	for range 2 {
		got := <-done
		if got.err != nil {
			t.Fatal("competing retirement was reported as failure", got.err)
		}
		deleted += got.report.Deleted
		manifests += got.report.ManifestsRetired
	}
	if calls.Load() != 2 || deleted != 3 || manifests != 1 || raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Val() != 0 {
		t.Fatalf("concurrent cleanup accounting: claims=%d deleted=%d manifests=%d", calls.Load(), deleted, manifests)
	}
}
