// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotUtilityMetadataObserver struct {
	active    atomic.Bool
	writes    atomic.Int64
	loseReply bool
	lost      atomic.Bool
}

func (*snapshotUtilityMetadataObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (o *snapshotUtilityMetadataObserver) observe(cmd goredis.Cmder) bool {
	a := cmd.Args()
	if !o.active.Load() || len(a) < 3 || a[1] != snapshotMutation {
		return false
	}
	n, _ := strconv.Atoi(fmt.Sprint(a[2]))
	i := 3 + n + 4
	if i+1 < len(a) && fmt.Sprint(a[i]) == "set" && strings.HasSuffix(fmt.Sprint(a[i+1]), ":table:public:capability_copy") {
		o.writes.Add(1)
		return true
	}
	return false
}
func (o *snapshotUtilityMetadataObserver) reply(cmd goredis.Cmder) error {
	if o.loseReply && o.lost.CompareAndSwap(false, true) {
		err := errors.New("injected capability metadata reply loss")
		cmd.SetErr(err)
		return err
	}
	return nil
}
func (o *snapshotUtilityMetadataObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		matched := o.observe(cmd)
		if err := next(ctx, cmd); err != nil {
			return err
		}
		if matched {
			return o.reply(cmd)
		}
		return nil
	}
}
func (o *snapshotUtilityMetadataObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		var matched goredis.Cmder
		for _, cmd := range cmds {
			if o.observe(cmd) {
				matched = cmd
			}
		}
		if err := next(ctx, cmds); err != nil {
			return err
		}
		if matched != nil {
			return o.reply(matched)
		}
		return nil
	}
}

// Two actual capability transitions must not become one descriptor rewrite per
// captured page. The original input descriptor intentionally remains unchanged.
func TestSnapshotUtilityCapabilityWorkIsBounded(t *testing.T) {
	testSnapshotUtilityCapabilities(t, false)
}

func TestSnapshotUtilityCapabilityUnknownReplyAborts(t *testing.T) {
	testSnapshotUtilityCapabilities(t, true)
}

func testSnapshotUtilityCapabilities(t *testing.T, loseReply bool) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE preserved(id BIGINT)")
	h.exec("INSERT INTO preserved VALUES(42)")
	s := snapshotTestStore(t, raw)
	o := &snapshotUtilityMetadataObserver{loseReply: loseReply}
	s.client.AddHook(o)
	base, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(base, "public", "preserved")
	if err != nil {
		t.Fatal(err)
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	source := &tableMeta{KeyPrefix: "app:capability:", KeyRows: true, Columns: []columnMeta{
		{Name: "id", Type: typeInt64}, {Name: "text", Type: typeString}, {Name: "number", Type: typeFloat64},
	}}
	const pages = 17
	calls := 0
	var workspace string
	next := func(ctx context.Context) ([]map[string]Value, bool, error) {
		workspace = snapshotOf(ctx).id
		page := calls
		calls++
		rows := make([]map[string]Value, snapshotImportPage)
		for i := range rows {
			id := int64(page*snapshotImportPage + i + 1)
			text, number := " normalized ", 1.0
			if page >= 2 {
				text, number = "truncated\x00exact", math.NaN()
			}
			rows[i] = map[string]Value{keyField: stringValue(fmt.Sprintf("app:capability:%d", id)),
				"id": intValue(typeInt64, id), "text": stringValue(text), "number": floatValue(typeFloat64, number)}
		}
		done := calls == pages
		if done {
			o.active.Store(true) // Capture is complete before destination row batches.
		}
		return rows, done, nil
	}
	err = s.importSnapshotTable(h.ctx, source, "public", "capability_copy", false, nil, next)
	if calls != pages {
		t.Fatalf("source input replayed: calls=%d", calls)
	}
	if loseReply {
		if err == nil || !strings.Contains(err.Error(), "injected capability metadata reply loss") ||
			!o.lost.Load() || o.writes.Load() != 1 || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
			t.Fatalf("uncertain capability mutation replayed or published: writes=%d error=%v", o.writes.Load(), err)
		}
		h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='capability_copy'", "0")
	} else {
		if err != nil {
			t.Fatal(err)
		}
		if o.writes.Load() > 3 || o.writes.Load() < 2 {
			t.Fatalf("metadata work scales with captured pages: pages=%d descriptor_writes=%d", calls, o.writes.Load())
		}
		h.expectRows("SELECT COUNT(*),MIN(id),MAX(id) FROM capability_copy", fmt.Sprintf("%d|1|%d", pages*snapshotImportPage, pages*snapshotImportPage))
		h.expectRows("SELECT COUNT(*) FROM capability_copy WHERE text=' normalized '", "256")
		h.expectRows("SELECT COUNT(*) FROM capability_copy WHERE number='NaN'::DOUBLE PRECISION", fmt.Sprint((pages-2)*snapshotImportPage))
		if exists, err := s.tableExists(base, "public", "capability_copy"); err != nil || exists {
			t.Fatal("old pin observed the new publication", err)
		}
	}
	for range 32 {
		s.reapSnapshot(h.ctx)
	}
	if raw.Exists(h.ctx, snapshotWorkspace(workspace)+"import:rows:"+workspace, snapshotWorkspace(workspace)+"import:keys:"+workspace).Val() != 0 {
		t.Fatal("completed/failed private journal was not reclaimed")
	}
	if _, err := s.getTable(base, "public", "preserved"); err != nil || raw.HGet(h.ctx, old.prefix()+"1", "id").Val() != "42" {
		t.Fatal("cleanup removed referenced input", err)
	}
	h.expectRows("SELECT id FROM preserved", "42")
	t.Logf("captured_pages=%d descriptor_writes=%d lost_reply=%t", calls, o.writes.Load(), loseReply)
}

func TestSnapshotUtilitySingleRowByteBoundary(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	ctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	meta := &tableMeta{Schema: "public", Name: "packet_bound", Columns: []columnMeta{{Name: "payload", Type: typeString}}}
	if _, err := s.createTable(ctx, meta, false); err != nil {
		t.Fatal(err)
	}
	data := snapshotDataID(meta.prefix())
	row := snapshotInsertRow{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1)}
	// The native admission contract counts KEYS + ARGV bytes, excluding the Lua
	// script. Every operation ID is 32 bytes. Both numeric row suffixes have
	// equal length, so the second packet is exactly one byte over the ceiling.
	fixed := 0
	for _, value := range []string{snapshotCatalogKey, snapshotMetadataGraph, snapshotMembers(data), row.key,
		id, pin.token, data, strings.Repeat("0", 32), "insert", row.member, "4", rowIDField, "1", "payload"} {
		fixed += len(value)
	}
	payload := strings.Repeat("x", int(snapshotMaxBytes)-fixed+1)
	row.fields = []any{rowIDField, "1", "payload", payload[:len(payload)-1]}
	if err := s.insertSnapshotBatch(ctx, meta.prefix(), []snapshotInsertRow{row}); err != nil {
		t.Fatal("exact 64 MiB packet rejected", err)
	}
	oversized := snapshotInsertRow{key: meta.prefix() + "2", member: fmt.Sprintf("%020d", 2), fields: []any{rowIDField, "2", "payload", payload}}
	if err := s.insertSnapshotBatch(ctx, meta.prefix(), []snapshotInsertRow{oversized}); err == nil || !strings.Contains(err.Error(), "batch byte limit exceeded") {
		t.Fatal("one-byte-over packet accepted or wrong refusal", err)
	}
	if raw.ZCard(h.ctx, snapshotMembers(data)).Val() != 1 || raw.Exists(h.ctx, oversized.key).Val() != 0 ||
		raw.HStrLen(h.ctx, row.key, "payload").Val() != int64(len(payload)-1) {
		t.Fatal("byte refusal partially mutated the private directory or images")
	}
	s.abortSnapshot(pin, id)
	for range 32 {
		s.reapSnapshot(h.ctx)
	}
	if raw.Exists(h.ctx, row.key, oversized.key, snapshotMembers(data)).Val() != 0 {
		t.Fatal("wide private row was not reclaimed")
	}
}
