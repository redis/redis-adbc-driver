// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type readinessServerError string

func (e readinessServerError) Error() string { return string(e) }
func (e readinessServerError) RedisError()   {}

type alterReadyHook struct {
	index          string
	stateError     string
	altered, ready atomic.Bool
	alters         atomic.Int64
	once           sync.Once
	waiting        chan struct{}
}

func (h *alterReadyHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *alterReadyHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *alterReadyHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		target := len(cmd.Args()) > 1 && fmt.Sprint(cmd.Args()[1]) == h.index
		if target && cmd.Name() == "ft.info" && h.altered.Load() {
			if !h.ready.Load() && h.stateError != "" {
				h.once.Do(func() { close(h.waiting) })
				err := readinessServerError(h.stateError)
				cmd.SetErr(err)
				return err
			}
			building := int64(1)
			if h.ready.Load() {
				building = 0
			}
			cmd.(interface{ SetVal(any) }).SetVal([]any{"indexing", building, "percent_indexed", "1"})
			h.once.Do(func() { close(h.waiting) })
			return nil
		}
		err := next(ctx, cmd)
		if target && cmd.Name() == "ft.alter" && err == nil {
			h.alters.Add(1)
			h.altered.Store(true)
		}
		return err
	}
}

func TestAlterWaitsBeforeMetadataCommit(t *testing.T) {
	for _, stateError := range []string{"", "Inconsistent index state", "SEARCH_VALUE_BAD Inconsistent index state"} {
		for _, cancelWait := range []bool{false, true} {
			t.Run(fmt.Sprint(stateError, "/", cancelWait), func(t *testing.T) {
				h := newSQLHarness(t)
				const table = "it_alter_ready"
				h.exec("CREATE TABLE " + table + " (id INTEGER)")
				h.exec("INSERT INTO " + table + " VALUES(1)")
				t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
				e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
				hook := &alterReadyHook{index: p.meta.index(), stateError: stateError, waiting: make(chan struct{})}
				switch client := e.store.client.(type) {
				case *goredis.Client:
					client.AddHook(hook)
				case *goredis.ClusterClient:
					if err := client.ForEachMaster(h.ctx, func(_ context.Context, node *goredis.Client) error { node.AddHook(hook); return nil }); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					done <- e.alterTable(ctx, p.meta, parseAlter(t, "ALTER TABLE "+table+" ADD COLUMN extra INTEGER DEFAULT 7").Cmds)
				}()
				select {
				case <-hook.waiting:
				case err := <-done:
					t.Fatal("ALTER completed before readiness", err)
				case <-time.After(5 * time.Second):
					t.Fatal("ALTER never checked readiness")
				}
				meta, err := e.store.getTable(h.ctx, defaultSchema, table)
				if err != nil {
					t.Fatal(err)
				}
				if slices.ContainsFunc(meta.Columns, func(c columnMeta) bool { return c.Name == "extra" }) {
					t.Fatal("published metadata before readiness")
				}
				if cancelWait {
					cancel()
				} else {
					hook.ready.Store(true)
				}
				select {
				case err = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("ALTER did not finish")
				}
				if cancelWait && !isStatus(err, adbc.StatusCancelled) || !cancelWait && err != nil {
					t.Fatal(err)
				}
				if hook.alters.Load() != 1 {
					t.Fatal("replayed FT.ALTER", hook.alters.Load())
				}
				meta, err = e.store.getTable(h.ctx, defaultSchema, table)
				if err != nil {
					t.Fatal(err)
				}
				if got := slices.ContainsFunc(meta.Columns, func(c columnMeta) bool { return c.Name == "extra" }); got == cancelWait {
					t.Fatal("wrong commit state", got, cancelWait)
				}
				if cancelWait {
					// The unused physical attribute remains; a new attempt must choose a
					// fresh field and succeed without publishing the cancelled definition.
					e.store.client = h.rawClient()
					if err := e.alterTable(h.ctx, meta, parseAlter(t, "ALTER TABLE "+table+" ADD COLUMN extra INTEGER DEFAULT 9").Cmds); err != nil {
						t.Fatal(err)
					}
					if c := h.tableColumn(h.rawClient(), table, "extra"); c.field() == "extra" || c.Default != "9" {
						t.Fatal(c)
					}
				}
			})
		}
	}
}

// Partition callers reject uncertain input immediately; only the ALTER waiter
// polls. Permissions and missing indexes must retain their terminal status.
func TestRangeReadinessSchemaConvergence(t *testing.T) {
	for _, tc := range []struct {
		message string
		code    adbc.Status
	}{
		{"Inconsistent index state", adbc.StatusInvalidState},
		{"SEARCH_VALUE_BAD Inconsistent index state", adbc.StatusInvalidState},
		{"NOPERM no FT.INFO permission", adbc.StatusUnauthorized},
		{"Unknown index name", adbc.StatusIO},
	} {
		t.Run(tc.message, func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_readiness_convergence"
			h.exec("CREATE TABLE " + table + " (id INTEGER)")
			t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
			e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
			hook := &alterReadyHook{index: p.meta.index(), stateError: tc.message, waiting: make(chan struct{})}
			hook.altered.Store(true)
			hookRangeClient(t, e.store.client, hook)
			if err := e.store.checkRangeReadiness(h.ctx, p.meta.index()); !isStatus(err, tc.code) {
				t.Fatal("wrong readiness status", err, tc.code)
			}
		})
	}
}

func TestPendingCleanupUsesCompleteHashInput(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_cleanup_input"
	h.setupStreamTable(table, 2505)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("DELETE FROM " + table + " WHERE id>=100 AND id<200")
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table+" WHERE id>=10 ORDER BY id LIMIT 3 OFFSET 4", nil)
	e.store = &store{client: e.store.client}
	e.store.searchCapsOnce.Do(func() { e.store.searchCaps = [2]int64{1000000, 1 << 31} })
	p.meta.PendingCleanup = []string{"retired_field"}
	sc, err := e.prepareScan(h.ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.req.hashScan || sc.req.limit != nil || sc.req.ranges != nil || sc.indexSort || sc.pushLimit {
		t.Fatal("unsafe Search/limit during cleanup", sc)
	}
	_, rows, err := e.scan(h.ctx, sc.req, nil)
	if err != nil || len(rows) != 2395 {
		t.Fatal("incomplete global input", len(rows), err)
	}
	for _, row := range rows {
		if row["id"].I < 10 {
			t.Fatal("residual lost", row)
		}
	}
	limited, err := e.selectScanned(h.ctx, p, sc, nil)
	if err != nil || len(limited) != 3 {
		t.Fatal("global LIMIT lost", limited, err)
	}
	for i, row := range limited {
		if row[0].I != int64(14+i) {
			t.Fatal("global ORDER/OFFSET lost", limited)
		}
	}
	ctx, cancel := context.WithCancel(h.ctx)
	visits := 0
	err = e.scanAllocatedCleanupPages(ctx, sc.req, nil, func([]string, []map[string]Value) error { visits++; cancel(); return nil })
	if !isStatus(err, adbc.StatusCancelled) || visits != 1 {
		t.Fatal("cleanup page cancellation lost", visits, err)
	}
	visits = 0
	err = e.scanAllocatedCleanupPages(h.ctx, sc.req, nil, func([]string, []map[string]Value) error { visits++; h.exec("TRUNCATE " + table); return nil })
	if !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "truncated or dropped") || visits != 1 {
		t.Fatal("stale cleanup page input accepted", visits, err)
	}
}

func TestPendingCleanupAggregateAndPartitionInputs(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_cleanup_aggregate"
	h.setupStreamTable(table, 300)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	e, p := sharedScanPlan(t, h, "SELECT COUNT(*), COUNT(DISTINCT id), SUM(id) FROM "+table+" WHERE id>=10", nil)
	e.store = &store{client: e.store.client}
	e.store.searchCapsOnce.Do(func() { e.store.searchCaps = [2]int64{1000000, 1 << 31} })
	p.meta.PendingCleanup = []string{"retired_field"}
	hook := &rangeReadinessHook{index: p.meta.index()}
	hookRangeClient(t, e.store.client, hook)
	rows, err := e.selectAggregate(h.ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0]) != 3 || rows[0][0].I != 290 || rows[0][1].I != 290 {
		t.Fatal(rows)
	}
	if cmp, ok := compareValues(rows[0][2], intValue(typeInt64, 44805)); !ok || cmp != 0 {
		t.Fatal(rows[0][2])
	}
	if hook.aggregates.Load() != 0 {
		t.Fatal("aggregate consumed an unsafe Search input")
	}
	_, broad := planPartitions(t, h, "SELECT id FROM "+table, 1)
	_, direct := planPartitions(t, h, "SELECT id FROM "+table+" WHERE __rowid=1", 1)
	st := &store{client: h.rawClient()}
	setPending := func(fields []string) {
		t.Helper()
		if err := st.updateTable(h.ctx, defaultSchema, table, func(meta *tableMeta) error { meta.PendingCleanup = fields; return nil }, nil); err != nil {
			t.Fatal(err)
		}
	}
	setPending([]string{"retired_field"})
	if r, err := h.conn.ReadPartition(h.ctx, broad.PartitionIDs[0]); !isStatus(err, adbc.StatusInvalidState) {
		if r != nil {
			r.Release()
		}
		t.Fatal("unsafe range accepted", err)
	}
	r, err := h.conn.ReadPartition(h.ctx, direct.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err := readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, []int64{0}) {
		t.Fatal(vals, err)
	}
	setPending(nil)
	r, err = h.conn.ReadPartition(h.ctx, broad.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err = readInts(r)
	r.Release()
	if err != nil || len(vals) != 300 {
		t.Fatal(len(vals), err)
	}
}

func TestTruncateAttributeReadinessRetry(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_truncate_ready_retry"
	h.exec("CREATE TABLE " + table + " (id INTEGER)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
	tr := &truncation{names: tableNames{index: p.meta.index()}, attrs: map[string]string{}, attrsPending: true}
	for _, c := range p.meta.Columns {
		if c.Indexed {
			tr.attrs[c.field()] = fmt.Sprint(indexAttrArgs(c))
		}
	}
	hook := &rangeReadinessHook{index: p.meta.index()}
	hook.building.Store(true)
	hookRangeClient(t, e.store.client, hook)
	ctx, cancel := context.WithTimeout(h.ctx, time.Millisecond)
	defer cancel()
	if err := e.store.addIndexedColumns(ctx, tr, p.meta); !isStatus(err, adbc.StatusTimeout) || !tr.attrsPending {
		t.Fatal("lost pending readiness on retry", err, tr.attrsPending)
	}
	hook.building.Store(false)
	if err := e.store.addIndexedColumns(h.ctx, tr, p.meta); err != nil || tr.attrsPending {
		t.Fatal("retry did not establish readiness", err, tr.attrsPending)
	}
}
