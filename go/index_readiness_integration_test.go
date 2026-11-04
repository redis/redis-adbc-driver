// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
				testSnapshotAlterReadinessCase(t, stateError, cancelWait)
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
			meta := h.inspectTable(h.rawClient(), defaultSchema, table)
			hook := &alterReadyHook{index: meta.index(), stateError: tc.message, waiting: make(chan struct{})}
			hook.altered.Store(true)
			st, ctx, pin := h.inspectionSnapshot(h.rawClient(hook))
			defer pin.release()
			if err := st.checkRangeReadiness(ctx, meta.index()); !isStatus(err, tc.code) {
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
	const highRowID = int64(9007199254741999)
	h.sparseSnapshotTable(table, highRowID, map[int64]int64{20: highRowID})
	e, p, readCtx := cleanupSnapshotPlan(t, h, "SELECT id FROM "+table+" WHERE id>=10 ORDER BY id LIMIT 3 OFFSET 4")
	readCtx, stop := context.WithTimeout(readCtx, 15*time.Second)
	defer stop()
	sc, err := e.prepareScan(readCtx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.req.hashScan || sc.req.limit != nil || sc.req.ranges != nil || sc.indexSort || sc.pushLimit {
		t.Error("unsafe Search/limit during cleanup", sc)
	}
	_, rows, err := e.scan(readCtx, sc.req, nil)
	if err != nil || len(rows) != 2395 {
		t.Fatal("incomplete global input", len(rows), err)
	}
	seenIDs := map[int64]bool{}
	for _, row := range rows {
		id := row["id"].I
		if id < 10 || id > 2504 || id >= 100 && id < 200 || seenIDs[id] {
			t.Fatal("wrong visible cleanup membership", id)
		}
		seenIDs[id] = true
	}
	for id := int64(10); id <= 2504; id++ {
		if !(id >= 100 && id < 200) && !seenIDs[id] {
			t.Fatal("cleanup omitted a row", id)
		}
	}
	bounded := sc.req
	bounded.ranges = &rowIDRangeScan{lo: highRowID, hi: highRowID}
	var boundedIDs []int64
	if err := e.scanBeyondCapPages(readCtx, bounded, nil, func(_ []string, rows []map[string]Value) error {
		for _, row := range rows {
			boundedIDs = append(boundedIDs, row["id"].I)
		}
		return nil
	}); err != nil || !slices.Equal(boundedIDs, []int64{19}) {
		t.Fatal("exact cleanup bounds lost", boundedIDs, err)
	}
	limited, err := e.selectScanned(readCtx, p, sc, nil)
	if err != nil || len(limited) != 3 {
		t.Fatal("global LIMIT lost", limited, err)
	}
	for i, row := range limited {
		if row[0].I != int64(14+i) {
			t.Fatal("global ORDER/OFFSET lost", limited)
		}
	}
	ctx, cancel := context.WithCancel(readCtx)
	visits := 0
	err = e.scanAllocatedCleanupPages(ctx, sc.req, nil, func([]string, []map[string]Value) error { visits++; cancel(); return nil })
	if !isStatus(err, adbc.StatusCancelled) || visits != 1 {
		t.Fatal("cleanup page cancellation lost", visits, err)
	}
	visits, seen := 0, 0
	err = e.scanAllocatedCleanupPages(readCtx, sc.req, nil, func(_ []string, rows []map[string]Value) error {
		visits++
		seen += len(rows)
		if visits == 1 {
			h.exec("TRUNCATE " + table)
			snapshotReapTest(e.store, h.ctx)
		}
		return nil
	})
	if err != nil || visits < 2 || seen != 2395 {
		t.Fatal("pinned cleanup input changed through TRUNCATE/GC", visits, seen, err)
	}
	h.expectRows("SELECT COUNT(*) FROM "+table, "0")
}

func TestPendingCleanupAggregateAndPartitionInputs(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_cleanup_aggregate"
	h.setupStreamTable(table, 300)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	raw := h.rawClient()
	hook := &cleanupSearchCounter{}
	e, p, readCtx := cleanupSnapshotPlan(t, h, "SELECT COUNT(*), COUNT(DISTINCT id), SUM(id) FROM "+table+" WHERE id>=10", hook)
	rows, err := e.selectAggregate(readCtx, p, nil)
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
	e2, p2, ctx2 := cleanupSnapshotPlan(t, h, "SELECT COUNT(*),SUM(id) FROM "+table, hook)
	all, err := e2.selectAggregate(ctx2, p2, nil)
	if err != nil || len(all) != 1 || all[0][0].I != 300 {
		t.Fatal("unfiltered cleanup aggregate lost input", all, err)
	}
	if cmp, ok := compareValues(all[0][1], intValue(typeInt64, 44850)); !ok || cmp != 0 {
		t.Fatal("unfiltered cleanup sum", all)
	}
	if hook.aggregates.Load() != 0 {
		t.Fatal("unfiltered aggregate consumed unsafe Search input")
	}
	_, broad := planPartitions(t, h, "SELECT id FROM "+table, 1)
	_, direct := planPartitions(t, h, "SELECT id FROM "+table+" WHERE __rowid=1", 1)
	releaseLive := func(parts adbc.Partitions) {
		t.Helper()
		d, err := decodePartition(parts.PartitionIDs[0])
		if err != nil || d.Snapshot == nil {
			t.Fatal("missing shared generation", err)
		}
		t.Cleanup(func() { h.exec("DROP PARTITION SNAPSHOT '" + d.Snapshot.Token + "'") })
	}
	releaseLive(broad)
	releaseLive(direct)
	setPending := func(fields []string) {
		t.Helper()
		m := h.inspectTable(raw, defaultSchema, table)
		m.PendingCleanup = fields
		doc, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		h.writeTableJSON(raw, defaultSchema, table, string(doc))
	}
	setPending([]string{"retired_field"})
	want := make([]int64, 300)
	for i := range want {
		want[i] = int64(i)
	}
	readParts := func(parts adbc.Partitions, expected []int64) {
		t.Helper()
		var got []int64
		for _, id := range parts.PartitionIDs {
			r, err := h.conn.ReadPartition(h.ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			vals, _, err := readInts(r)
			r.Release()
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, vals...)
		}
		if !slices.Equal(got, expected) {
			t.Fatal("partition membership changed", got)
		}
	}
	readParts(broad, want)
	r, err := h.conn.ReadPartition(h.ctx, direct.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err := readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, []int64{0}) {
		t.Fatal(vals, err)
	}
	_, duringCleanup := planPartitions(t, h, "SELECT id FROM "+table, 3)
	releaseLive(duringCleanup)
	readParts(duringCleanup, want)
	storage := retainedFixture(t, raw, defaultRetainedLimits())
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	impl := st.(*statementImpl)
	impl.conn.retainedNamespace = storage.namespace
	impl.conn.partitionControlPersistence = "guaranteed" // Software setup verified noeviction.
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	retainedSchema, retained, _ := retainedAPIPlan(t, h.conn, "SELECT id FROM "+table+" ORDER BY id")
	d, err := decodeRetainedPartition(retained.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.exec("DROP PARTITION RESULT '" + d.Reference.JobID + "'") })
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
	h.exec("TRUNCATE " + table)
	h.exec("DROP TABLE " + table)
	snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
	readParts(broad, want)
	readParts(duringCleanup, want)
	readParts(direct, []int64{0})
	if got := retainedAPIReadIDs(t, h.conn, retainedSchema, retained); !slices.Equal(got, want) {
		t.Fatal("retained cleanup capture changed", got)
	}
}

func TestTruncateAttributeReadinessRetry(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_truncate_ready_retry"
	h.exec("CREATE TABLE " + table + " (id INTEGER)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	meta := h.inspectTable(h.rawClient(), defaultSchema, table)
	tr := &truncation{names: tableNames{index: meta.index()}, attrs: map[string]string{}, attrsPending: true}
	for _, c := range meta.Columns {
		if c.Indexed {
			tr.attrs[c.field()] = fmt.Sprint(indexAttrArgs(c))
		}
	}
	hook := &rangeReadinessHook{index: meta.index()}
	hook.building.Store(true)
	// Exercise the legacy readiness primitive with attributes already present;
	// this issues only FT.INFO, never FT.ALTER on published snapshot storage.
	st := &store{client: h.rawClient(hook)}
	ctx, cancel := context.WithTimeout(h.ctx, time.Millisecond)
	defer cancel()
	if err := st.addIndexedColumns(ctx, tr, meta); !isStatus(err, adbc.StatusTimeout) || !tr.attrsPending {
		t.Fatal("lost pending readiness on retry", err, tr.attrsPending)
	}
	hook.building.Store(false)
	if err := st.addIndexedColumns(h.ctx, tr, meta); err != nil || tr.attrsPending {
		t.Fatal("retry did not establish readiness", err, tr.attrsPending)
	}
}
