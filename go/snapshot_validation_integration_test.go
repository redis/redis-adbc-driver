// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"sync"
	"testing"
)

func TestSnapshotValidationOnlyTouchesReferencedTables(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE healthy(v INTEGER)")
	h.exec("INSERT INTO healthy VALUES(42)")
	h.exec("CREATE TABLE damaged(v INTEGER)")
	h.exec("INSERT INTO damaged VALUES(99)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := snapshotTableTest(t, s, ctx, "damaged")
	pin.release()
	if err := raw.Do(h.ctx, "FT.DROPINDEX", m.index()).Err(); err != nil {
		t.Fatal(err)
	}
	// A broken unrelated source must not prevent a statement from using a
	// complete, independently pinned source. The referenced source still fails.
	h.expectRows("SELECT v FROM healthy", "42")
	h.exec("UPDATE healthy SET v=v+1")
	h.expectRows("SELECT v FROM healthy", "43")
	h.expectError("SELECT COUNT(*) FROM damaged", "snapshot index is unavailable")
}

func TestSnapshotValidationCachesImmutableSourcesPerStatement(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	for i := 0; i < 8; i++ {
		h.exec(fmt.Sprintf("CREATE TABLE t%d(v INTEGER)", i))
	}
	h.exec("INSERT INTO t0 VALUES(42)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	if n := guard.info.Load(); n != 0 {
		t.Fatalf("acquisition checked %d unrelated indexes", n)
	}
	m := snapshotTableTest(t, s, ctx, "t0")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.getTable(ctx, "public", "t0"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := guard.info.Load(); n != int64(len(m.snapshotSegments())) {
		t.Fatalf("repeated source validation: %d", n)
	}
	// A changed descriptor must not reuse the old validation result.
	broken := *m
	wrong := *m.SnapshotRows + 1
	broken.SnapshotRows = &wrong
	if err := s.validateSnapshotTable(ctx, &broken); err == nil {
		t.Fatal("validation cache accepted a changed descriptor")
	}
	before := guard.info.Load()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if _, err := s.getTable(wctx, "public", "t0"); err != nil {
		t.Fatal(err)
	}
	if guard.info.Load() != before {
		t.Fatal("private workspace repeated an immutable input proof")
	}
}

func TestSnapshotPointReadRejectsMissingBaselineRow(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(42)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	m := snapshotTableTest(t, s, ctx, "t")
	if m.SnapshotOverlay {
		t.Fatal("test requires a baseline without overlays")
	}
	if err := raw.Del(h.ctx, m.prefix()+"1").Err(); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseScript("SELECT v FROM t WHERE __rowid=1")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	res, err := e.execute(ctx, parsed[0], nil, nil, nil)
	if err == nil && res.stream != nil {
		defer res.stream.Release()
		for res.stream.Next() {
		}
		err = res.stream.Err()
	}
	if err == nil {
		t.Fatal("point read silently discarded a missing published row")
	}
}
