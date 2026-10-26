// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
	"sync/atomic"
	"testing"
)

// Real cursors run normally; control readiness without relying on backfill timing.
type rangeReadinessHook struct {
	index          string
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
		if cmd.Name() == "ft.info" && fmt.Sprint(cmd.Args()[1]) == h.index {
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
func hookRangeClient(t *testing.T, client goredis.UniversalClient, hook goredis.Hook) {
	t.Helper()
	switch c := client.(type) {
	case *goredis.Client:
		c.AddHook(hook)
	case *goredis.ClusterClient:
		if err := c.ForEachMaster(context.Background(), func(_ context.Context, n *goredis.Client) error { n.AddHook(hook); return nil }); err != nil {
			t.Fatal(err)
		}
	}
}
func TestBoundedScanRejectsBuildingIndex(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_readiness"
	h.setupStreamTable(table, 25000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
	sc, err := e.prepareScan(h.ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc.req.ranges = &rowIDRangeScan{lo: 1, hi: 25000, cap: 50000, configured: 50000}
	hook := &rangeReadinessHook{index: p.meta.index()}
	hook.building.Store(true)
	e.store.client = h.rawClient()
	hookRangeClient(t, e.store.client, hook)
	it, err := e.openScan(h.ctx, sc.req, nil)
	if it != nil {
		it.close(h.ctx)
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
			e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
			sc, err := e.prepareScan(h.ctx, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			cap := int64(50000)
			if phase == "between_ranges" {
				cap = 1000
			}
			sc.req.ranges = &rowIDRangeScan{lo: 1, hi: 25000, cap: cap, configured: cap}
			hook := &rangeReadinessHook{index: p.meta.index()}
			hook.afterAggregate.Store(phase == "after_open")
			e.store.client = h.rawClient()
			hookRangeClient(t, e.store.client, hook)
			it, err := e.openScan(h.ctx, sc.req, nil)
			if phase == "after_open" {
				if it != nil {
					it.close(h.ctx)
				}
				if !isStatus(err, adbc.StatusInvalidState) {
					t.Fatalf("unsafe first page: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer it.close(h.ctx)
				_, rows, err := it.next(h.ctx)
				if err != nil || len(rows) == 0 {
					t.Fatalf("ready first page: %d %v", len(rows), err)
				}
				if phase == "during_page" {
					hook.afterPage.Store(true)
				} else {
					hook.building.Store(true)
				}
				_, rows, err = it.next(h.ctx)
				if !isStatus(err, adbc.StatusInvalidState) || len(rows) != 0 {
					t.Fatalf("unsafe page exposed %d rows: %v", len(rows), err)
				}
			}
			st := &store{client: h.rawClient()}
			reply, err := st.searchDo(h.ctx, p.meta.index(), "FT.INFO", p.meta.index()).Result()
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
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table+" WHERE id >= 10 LIMIT 200 OFFSET 3", nil)
	hook := &rangeReadinessHook{index: p.meta.index()}
	hook.building.Store(true)
	e.store.client = h.rawClient()
	hookRangeClient(t, e.store.client, hook)
	sc, err := e.prepareScan(h.ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.req.ranges != nil || !sc.req.hashScan {
		t.Fatal("ordinary read lost complete HASH fallback")
	}
	rows, err := e.selectScanned(h.ctx, p, sc, nil)
	if err != nil || len(rows) != 200 || rows[0][0].I != 13 || rows[199][0].I != 212 {
		t.Fatalf("HASH fallback changed values/limits: %d %v", len(rows), err)
	}
}

func TestRangeReadinessChecksAllPrimaries(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_readiness_cluster"
	h.setupStreamTable(table, 100)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
	e.store.client = h.rawClient()
	cc, ok := e.store.client.(*goredis.ClusterClient)
	if !ok {
		t.Skip("OSS Cluster-specific non-coordinator check")
	}
	coordinator, err := cc.MasterForKey(h.ctx, p.meta.index())
	if err != nil {
		t.Fatal(err)
	}
	var hooked atomic.Bool
	err = cc.ForEachMaster(h.ctx, func(_ context.Context, node *goredis.Client) error {
		if node.Options().Addr != coordinator.Options().Addr && hooked.CompareAndSwap(false, true) {
			hook := &rangeReadinessHook{index: p.meta.index()}
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
	if err := e.store.checkRangeReadiness(h.ctx, p.meta.index()); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatalf("missed building non-coordinator primary: %v", err)
	}
}
