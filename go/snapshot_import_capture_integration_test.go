// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotClusterImportCaptureCompletion(t *testing.T) {
	h, s, ctx, meta, p := snapshotClusterCaptureFixture(t, 513)
	scope := snapshotOf(ctx)
	if err := p.apply(ctx); err == nil {
		t.Fatal("output started before complete capture")
	}
	done, err := p.captureControl(ctx, "complete")
	if err != nil || done {
		t.Fatal("expected one bounded incomplete validation checkpoint", done, err)
	}
	var work struct {
		Capture struct {
			State                     string
			Checked, Total, Remaining int64
		} `json:"import_capture"`
	}
	raw, err := s.client.HGet(h.ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
	if err != nil || json.Unmarshal(raw, &work) != nil || work.Capture.State != "checking" || work.Capture.Checked != 128 || work.Capture.Total != 513 {
		t.Fatal("capture completion did not persist its bounded checkpoint", err)
	}
	if err := p.append(ctx, []map[string]Value{{keyField: stringValue("late-source"), "v": intValue(typeInt64, 42)}}); err == nil || !strings.Contains(err.Error(), "capture is immutable") {
		t.Fatal("validation allowed new input", err)
	}
	if err := p.complete(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := p.ready(ctx); err != nil || n != 513 {
		t.Fatal("completed capture count", n, err)
	}
	if err := p.apply(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := p.ready(ctx); err != nil || n != 0 {
		t.Fatal("output did not checkpoint exact consumption", n, err)
	}
	// A second Go journal handle reads the durable outcome without a source
	// callback. This is private-workspace continuation, not process ownership.
	other := newSnapshotImportSpool(ctx, s, meta)
	if n, err := other.ready(ctx); err != nil || n != 0 {
		t.Fatal("completion only existed in process memory", n, err)
	}
	if err := s.publishSnapshot(ctx, scope.pin, scope.id); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v),COUNT(__key) FROM capture_barrier", "513|9007199254740993|9007199254740993|513")
}

func TestSnapshotClusterEmptyImportCaptureCompletion(t *testing.T) {
	h, s, ctx, _, p := snapshotClusterCaptureFixture(t, 0)
	if err := p.complete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.apply(ctx); err != nil {
		t.Fatal(err)
	}
	scope := snapshotOf(ctx)
	if err := s.publishSnapshot(ctx, scope.pin, scope.id); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*) FROM capture_barrier", "0")
}

func TestSnapshotClusterImportCaptureRejectsMismatchedMembership(t *testing.T) {
	h, s, ctx, _, p := snapshotClusterCaptureFixture(t, 2)
	scope := snapshotOf(ctx)
	keys := snapshotWorkspace(scope.id) + "import:keys:" + scope.id
	if err := s.client.ZRem(h.ctx, keys, "capture:00001").Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.client.ZAdd(h.ctx, keys, goredis.Z{Score: 0, Member: "not-captured"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := p.complete(ctx); err == nil || !strings.Contains(err.Error(), "membership is incomplete") {
		t.Fatal("equal counts hid missing input", err)
	}
	if err := p.append(ctx, []map[string]Value{{keyField: stringValue("repair"), "v": intValue(typeInt64, 1)}}); err == nil {
		t.Fatal("failed validation reopened source capture")
	}
}

func TestSnapshotClusterImportCaptureWideKeyValidationBudget(t *testing.T) {
	h, s, ctx, _, p := snapshotClusterCaptureFixture(t, 0)
	var rows []map[string]Value
	for i := 0; i < 10; i++ {
		rows = append(rows, map[string]Value{keyField: stringValue(strings.Repeat("k", 200<<10) + fmt.Sprint(i)), "v": intValue(typeInt64, 1)})
	}
	if err := p.append(ctx, rows); err != nil {
		t.Fatal(err)
	}
	done, err := p.captureControl(ctx, "complete")
	if err != nil || done {
		t.Fatal("wide key completion was not bounded", done, err)
	}
	var w struct {
		Capture struct{ Checked int64 } `json:"import_capture"`
	}
	raw, err := s.client.HGet(h.ctx, snapshotCatalogKey, "w:"+snapshotOf(ctx).id).Bytes()
	if err != nil || json.Unmarshal(raw, &w) != nil || w.Capture.Checked != 5 {
		t.Fatal("wide keys exceeded validation byte target", err, w.Capture.Checked)
	}
	if err := p.complete(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := p.ready(ctx); err != nil || n != 10 {
		t.Fatal("wide capture count", n, err)
	}
}

type lostImportCompletionReply struct {
	snapshotHook
	completeCalls atomic.Int64
	lost          atomic.Bool
	data          string
}

func (h *lostImportCompletionReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		complete := len(a) > 8 && a[0] == "EVAL" && a[1] == snapshotImportCaptureLua && a[10] == "complete"
		if complete {
			h.completeCalls.Add(1)
		}
		err := next(ctx, cmd)
		if complete && err == nil && h.lost.CompareAndSwap(false, true) {
			h.data = fmt.Sprint(a[13])
			err = errors.New("injected lost input-completion reply")
			cmd.SetErr(err)
		}
		return err
	}
}
func TestSnapshotClusterImportCaptureLostCompletionReplyDoesNotReplay(t *testing.T) {
	h, s, ctx, meta, p := snapshotClusterCaptureFixture(t, 2)
	observer := &lostImportCompletionReply{}
	s.client.AddHook(observer)
	if err := p.complete(ctx); err == nil || !strings.Contains(err.Error(), "injected lost input-completion reply") {
		t.Fatal("unknown completion unexpectedly succeeded", err)
	}
	if !observer.lost.Load() || observer.completeCalls.Load() != 1 {
		t.Fatal("completion mutation was replayed")
	}
	if n, err := p.ready(ctx); err != nil || n != 2 {
		t.Fatal("durable completion could not be observed read-only", n, err)
	}
	if s.client.ZCard(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 0 {
		t.Fatal("unknown completion emitted output")
	}
}

func TestSnapshotClusterImportCaptureLostCompletionReplyAbortsImport(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	s := st.(*statementImpl).conn.store
	observer := &lostImportCompletionReply{}
	s.client.AddHook(observer)
	source := &tableMeta{Name: "source", KeyPrefix: "app:lost-capture:", KeyRows: true, Columns: []columnMeta{{Name: "v", Type: typeInt64}}}
	calls := 0
	next := func(context.Context) ([]map[string]Value, bool, error) {
		calls++
		return []map[string]Value{{keyField: stringValue("app:lost-capture:\x00\xff"), "v": intValue(typeInt64, 9007199254740993)}}, true, nil
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	err = s.importSnapshotTable(h.ctx, source, "public", "capture_lost_reply", false, nil, next)
	if err == nil || !strings.Contains(err.Error(), "injected lost input-completion reply") || calls != 1 || observer.completeCalls.Load() != 1 || observer.data == "" {
		t.Fatal("unknown completion replayed source/transition or succeeded", err, calls, observer.completeCalls.Load())
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root || raw.ZCard(h.ctx, snapshotMembers(observer.data)).Val() != 0 {
		t.Fatal("unknown completion published or emitted output")
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='capture_lost_reply'", "0")
}

func TestSnapshotClusterImportCaptureReservesStableRowIDs(t *testing.T) {
	h, s, ctx, meta, p := snapshotClusterCaptureFixture(t, 3)
	if err := p.complete(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := s.redis(ctx).Get(ctx, seqKey(meta.Schema, meta.Name)).Int64(); err != nil || n != 3 {
		t.Fatal("completed capture did not reserve its immutable row ID range", n, err)
	}
	if err := p.applyPage(ctx, []string{"\x00\xfforiginal-key"}); err != nil {
		t.Fatal(err)
	}
	// Reconstruct only the private spool, with no process-local allocation state.
	other := newSnapshotImportSpool(ctx, s, meta)
	if err := other.apply(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := s.redis(ctx).Get(ctx, seqKey(meta.Schema, meta.Name)).Int64(); err != nil || n != 3 {
		t.Fatal("continuation allocated new row IDs", n, err)
	}
	for i, key := range []string{"\x00\xfforiginal-key", "capture:00001", "capture:00002"} {
		if got, err := s.client.HGet(h.ctx, meta.prefix()+fmt.Sprint(i+1), snapshotSourceKeyField).Result(); err != nil || got != key {
			t.Fatal("unstable source-to-row binding", i, err)
		}
	}
	scope := snapshotOf(ctx)
	if err := s.publishSnapshot(ctx, scope.pin, scope.id); err != nil {
		t.Fatal(err)
	}
	h.exec("INSERT INTO capture_barrier VALUES (42)")
	h.expectRows("SELECT COUNT(*),COUNT(__key),MAX(v) FROM capture_barrier", "4|3|9007199254740993")
}
