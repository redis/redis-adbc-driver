// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Real cursors run normally; control readiness without relying on backfill timing.
type rangeReadinessHook struct {
	index          string
	active         atomic.Bool
	building       atomic.Bool
	aggregates     atomic.Int64
	afterAggregate atomic.Bool
	afterPage      atomic.Bool
}

func (h *rangeReadinessHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *rangeReadinessHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *rangeReadinessHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "ft.info" && fmt.Sprint(cmd.Args()[1]) == h.index && h.active.Load() {
			building := int64(0)
			if h.building.Load() {
				building = 1
			}
			cmd.(interface{ SetVal(any) }).SetVal([]any{"indexing", building, "percent_indexed", "1"})
			return nil
		}
		err := next(ctx, cmd)
		if cmd.Name() == "ft.cursor" && len(cmd.Args()) > 1 && fmt.Sprint(cmd.Args()[1]) == "READ" && h.afterPage.Load() {
			h.building.Store(true)
		}
		if cmd.Name() == "ft.aggregate" && fmt.Sprint(cmd.Args()[1]) == h.index {
			h.aggregates.Add(1)
			if h.afterAggregate.Load() {
				h.building.Store(true)
			}
		}
		return err
	}
}

// Keep cursor-transition primitives on one pinned immutable physical source.
// This changes only the in-memory test plan, never the published descriptor.
// Native segmented fallback is tested separately below.
func readinessScanPlan(t *testing.T, h *sqlHarness, query string, hook *rangeReadinessHook, primitive bool) (context.Context, *executor, *selectPlan) {
	t.Helper()
	st, ctx, pin := h.inspectionSnapshot(h.rawClient(hook))
	t.Cleanup(pin.release)
	e := &executor{store: st, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
	e.store.searchCapsOnce.Do(func() {
		e.store.searchCaps = [2]int64{1_000_000, 137}
		e.store.searchAggregateCapKnown = true // deliberate controlled planning bound
	})
	parsed, err := ParseScript(query)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	if primitive {
		sources := 0
		for _, segment := range p.meta.snapshotSegments() {
			if segment.Rows > 0 {
				sources++
				if segment.Prefix != p.meta.prefix() {
					t.Fatal("primitive must use the current physical source")
				}
			}
		}
		if sources != 1 {
			t.Fatal("cursor primitive requires one immutable physical source", sources)
		}
		p.meta = cloneMeta(p.meta)
		p.meta.SnapshotOverlay, p.meta.SnapshotSegments = false, nil
		p.meta.SnapshotRows = nil // validated/pinned physical cursor leaf only
	}
	return ctx, e, p
}

func TestBoundedScanRejectsBuildingIndex(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_readiness"
	h.setupStreamTable(table, 25000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	hook := &rangeReadinessHook{index: h.inspectTable(h.rawClient(), defaultSchema, table).index()}
	ctx, e, p := readinessScanPlan(t, h, "SELECT id FROM "+table, hook, true)
	sc, err := e.prepareScan(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc.req.ranges = &rowIDRangeScan{lo: 1, hi: 25000, cap: 50000, configured: 50000}
	hook.active.Store(true)
	hook.building.Store(true)
	it, err := e.openScan(ctx, sc.req, nil)
	if it != nil {
		it.close(ctx)
	}
	if !isStatus(err, adbc.StatusInvalidState) {
		t.Fatalf("building index must reject before opening a cursor, got %v", err)
	}
	if hook.aggregates.Load() != 0 {
		t.Fatal("opened an unsafe aggregate")
	}
}

func TestBoundedScanReadinessTransitions(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_readiness_transition"
	h.setupStreamTable(table, 25000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	for _, phase := range []string{"after_open", "between_pages", "during_page", "between_ranges"} {
		t.Run(phase, func(t *testing.T) {
			hook := &rangeReadinessHook{index: h.inspectTable(h.rawClient(), defaultSchema, table).index()}
			ctx, e, p := readinessScanPlan(t, h, "SELECT id FROM "+table, hook, true)
			sc, err := e.prepareScan(ctx, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			cap := int64(50000)
			if phase == "between_ranges" {
				cap = 1000
			}
			sc.req.ranges = &rowIDRangeScan{lo: 1, hi: 25000, cap: cap, configured: cap}
			hook.active.Store(true)
			hook.afterAggregate.Store(phase == "after_open")
			it, err := e.openScan(ctx, sc.req, nil)
			if phase == "after_open" {
				if it != nil {
					it.close(ctx)
				}
				if !isStatus(err, adbc.StatusInvalidState) {
					t.Fatalf("unsafe first page: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer it.close(ctx)
				_, rows, err := it.next(ctx)
				if err != nil || len(rows) == 0 {
					t.Fatalf("ready first page: %d %v", len(rows), err)
				}
				if phase == "during_page" {
					hook.afterPage.Store(true)
				} else {
					hook.active.Store(true)
					hook.building.Store(true)
				}
				_, rows, err = it.next(ctx)
				if !isStatus(err, adbc.StatusInvalidState) || len(rows) != 0 {
					t.Fatalf("unsafe page exposed %d rows: %v", len(rows), err)
				}
			}
			st := &store{client: h.rawClient()}
			reply, err := st.searchDo(ctx, p.meta.index(), "FT.INFO", p.meta.index()).Result()
			if err != nil {
				t.Fatal(err)
			}
			fields, _ := redisReplyFields(reply)
			stats, _ := redisReplyFields(fields["cursor_stats"])
			count, ok := redisReplyInt64(stats["index_total"])
			if !ok || count != 0 {
				t.Fatalf("leaked cursor: %v", stats)
			}
			if hook.aggregates.Load() != 1 {
				t.Fatal("opened another range after readiness failure")
			}
		})
	}
}

func TestOrdinaryBuildingIndexUsesHashFallback(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_ordinary_readiness"
	h.setupStreamTable(table, 2500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	hook := &rangeReadinessHook{index: h.inspectTable(h.rawClient(), defaultSchema, table).index()}
	ctx, e, p := readinessScanPlan(t, h, "SELECT id FROM "+table+" WHERE id >= 10 LIMIT 200 OFFSET 3", hook, true)
	hook.active.Store(true)
	hook.building.Store(true)
	sc, err := e.prepareScan(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.req.ranges != nil || !sc.req.hashScan {
		t.Fatal("ordinary read lost complete HASH fallback")
	}
	rows, err := e.selectScanned(ctx, p, sc, nil)
	if err != nil || len(rows) != 200 || rows[0][0].I != 13 || rows[199][0].I != 212 {
		t.Fatalf("HASH fallback changed values/limits: %d %v", len(rows), err)
	}
}

func TestRangeReadinessChecksAllPrimaries(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_readiness_cluster"
	h.setupStreamTable(table, 100)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	raw := h.rawClient()
	cc, ok := raw.(*goredis.ClusterClient)
	if !ok {
		t.Skip("Cluster API-specific non-coordinator check")
	}
	meta := h.inspectTable(raw, defaultSchema, table)
	coordinator, err := cc.MasterForKey(h.ctx, meta.index())
	if err != nil {
		t.Fatal(err)
	}
	var hooked atomic.Bool
	err = cc.ForEachMaster(h.ctx, func(_ context.Context, node *goredis.Client) error {
		if node.Options().Addr != coordinator.Options().Addr && hooked.CompareAndSwap(false, true) {
			hook := &rangeReadinessHook{index: meta.index()}
			hook.active.Store(true)
			hook.building.Store(true)
			node.AddHook(hook)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hooked.Load() {
		t.Fatal("test requires a non-coordinator primary")
	}
	// Install node hooks before acquiring a pin and starting its heartbeat.
	st, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	reply, err := coordinator.Do(ctx, "FT.INFO", meta.index()).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRangeReadinessReply(reply); err != nil {
		t.Fatal("coordinator must remain ready", err)
	}
	if err := st.checkRangeReadiness(ctx, meta.index()); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatalf("missed building non-coordinator primary: %v", err)
	}
}

func TestSnapshotReadinessFallbackPreservesVisibility(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_snapshot_readiness_fallback"
	h.setupStreamTable(table, 2500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	raw := h.rawClient()
	base := h.inspectTable(raw, defaultSchema, table)
	h.exec("UPDATE " + table + " SET id=9999 WHERE id=12")
	h.exec("DELETE FROM " + table + " WHERE id=13")
	hook := &rangeReadinessHook{index: base.index()}
	ctx, e, p := readinessScanPlan(t, h, "SELECT id FROM "+table+" WHERE id>=10 ORDER BY id LIMIT 200 OFFSET 3", hook, false)
	if !p.meta.SnapshotOverlay || len(p.meta.snapshotSegments()) < 2 {
		t.Fatal("test requires segmented native visibility")
	}
	// Initial planning validates the real immutable index. Inject a later
	// readiness transition so optimization must retain complete exact visibility.
	before := h.storedTableJSON(raw, defaultSchema, table)
	hook.active.Store(true)
	hook.building.Store(true)
	sc, err := e.prepareScan(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.selectScanned(ctx, p, sc, nil)
	if err != nil || len(rows) != 200 {
		t.Fatal("incomplete snapshot fallback", len(rows), err)
	}
	for i, row := range rows {
		if len(row) != 1 || row[0].Null || row[0].I != int64(15+i) {
			t.Fatal("wrong snapshot fallback row", i, row)
		}
	}
	if hook.aggregates.Load() != 0 {
		t.Fatal("building source opened an unsafe aggregate")
	}
	if h.storedTableJSON(raw, defaultSchema, table) != before {
		t.Fatal("readiness fallback changed published metadata")
	}
	h.expectNoIndexCursors(&store{client: raw}, base.index())
}
