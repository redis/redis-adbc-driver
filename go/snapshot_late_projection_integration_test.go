// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotLatePayloadReads struct {
	snapshotHook
	rows atomic.Int64
}

func (h *snapshotLatePayloadReads) record(cmd goredis.Cmder) {
	args := cmd.Args()
	if len(args) < 2 || snapshotRowPrefix(fmt.Sprint(args[1])) == "" {
		return
	}
	if strings.EqualFold(fmt.Sprint(args[0]), "HGETALL") {
		h.rows.Add(1)
	}
	if strings.EqualFold(fmt.Sprint(args[0]), "HMGET") {
		for _, field := range args[2:] {
			if fmt.Sprint(field) == "payload" {
				h.rows.Add(1)
				break
			}
		}
	}
}
func (h *snapshotLatePayloadReads) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { h.record(cmd); return next(ctx, cmd) }
}
func (h *snapshotLatePayloadReads) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			h.record(cmd)
		}
		return next(ctx, cmds)
	}
}

func TestSnapshotOrderedLateProjectionGlobalLimits(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_snapshot_ordered_late"
	h.dropTables(table)
	t.Cleanup(func() { h.dropTables(table) })
	h.exec("CREATE TABLE " + table + " (id INTEGER,url VARCHAR,payload VARCHAR,spare VARCHAR)")
	h.exec("INSERT INTO " + table + " SELECT i,CASE WHEN i%2=0 THEN 'match' ELSE 'miss' END,'row-'||i||repeat('p',4096),NULL FROM generate_series(1,600) AS g(i)")
	h.exec("UPDATE " + table + " SET url=CASE WHEN id=2 THEN 'miss' ELSE url END,payload=CASE WHEN id=4 THEN 'changed' ELSE payload END,spare=CASE WHEN id=4 THEN 'x' ELSE spare END WHERE id IN (2,4)")
	h.exec("DELETE FROM " + table + " WHERE id=6")
	for _, tc := range []struct {
		tail string
		ids  []int
	}{
		{"ORDER BY id LIMIT 2 OFFSET 1", []int{8, 10}},
		{"ORDER BY id DESC NULLS LAST LIMIT 2 OFFSET 1", []int{598, 596}},
		{"ORDER BY id LIMIT 1", []int{4}},
		{"ORDER BY id LIMIT 0", nil},
		{"ORDER BY id LIMIT 2 OFFSET 1000", nil},
	} {
		t.Run(tc.tail, func(t *testing.T) {
			h := &sqlHarness{t: t, ctx: h.ctx, conn: h.conn}
			query := "SELECT id,payload,spare FROM " + table + " WHERE url LIKE '%match%' " + tc.tail
			counter := &snapshotLatePayloadReads{}
			e, plan, _ := distinctTestPlan(t, h, query, counter)
			sc, err := e.prepareScan(h.ctx, plan, nil)
			if err != nil || !plan.meta.SnapshotOverlay || sc.req.late == nil {
				t.Fatal("segmented late plan missing", err)
			}
			it, err := e.openScan(h.ctx, sc.req, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer it.close(h.ctx)
			if it.ordered == nil {
				t.Fatal("global ordered merge was bypassed")
			}
			for _, source := range it.ordered.sources {
				if source.iter.need["payload"] || source.iter.late != nil {
					t.Fatal("source fetched output or applied a local late limit")
				}
			}
			var got []string
			text := func(v Value) string {
				if v.Null {
					return "NULL"
				}
				return v.Text()
			}
			for {
				_, rows, err := it.next(h.ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					got = append(got, text(row["id"])+"|"+text(row["payload"])+"|"+text(row["spare"]))
				}
			}
			var want []string
			for _, id := range tc.ids {
				payload, spare := fmt.Sprintf("row-%d", id)+strings.Repeat("p", 4096), "NULL"
				if id == 4 {
					payload, spare = "changed", "x"
				}
				want = append(want, fmt.Sprint(id)+"|"+payload+"|"+spare)
			}
			if !slices.Equal(got, want) {
				t.Fatal("global late rows differ", tc.ids)
			}
			if n := counter.rows.Load(); n != int64(len(tc.ids)) {
				t.Fatal("output fetched before global selection", n, len(tc.ids))
			}
			h.expectRows(query, want...)
		})
	}
	query := "SELECT id,payload FROM " + table + " WHERE url LIKE '%match%' ORDER BY id LIMIT 2 OFFSET 1"
	e, plan, _ := distinctTestPlan(t, h, query)
	e.store.searchCapsOnce.Do(func() { e.store.searchCaps = [2]int64{1, 1} })
	sc, err := e.prepareScan(h.ctx, plan, nil)
	if err != nil || sc.req.late != nil || !sc.req.need["payload"] {
		t.Fatal("cap fallback lost output fields", err)
	}
	rows, err := e.selectScanned(h.ctx, plan, sc, nil)
	if err != nil || len(rows) != 2 || rows[0][0].I != 8 || rows[1][0].I != 10 || rows[0][1].S != "row-8"+strings.Repeat("p", 4096) {
		t.Fatal("cap fallback changed results", err)
	}
	// Compare actual wide-field transfers in the parallel heap with an eager
	// baseline on the same pinned data, independently checking complete output.
	h.sparseSnapshotTable(table, aggregatePartitionRows*2, nil)
	counter := &snapshotLatePayloadReads{}
	e, plan, _ = distinctTestPlan(t, h, query, counter)
	e.queryWorkers = 2
	sc, err = e.prepareScan(h.ctx, plan, nil)
	if err != nil || sc.req.late == nil {
		t.Fatal("parallel late plan missing", err)
	}
	optimized, handled, err := e.parallelSelectTopK(h.ctx, plan, sc, nil)
	if err != nil || !handled {
		t.Fatal("parallel late execution unavailable", handled, err)
	}
	lateReads := counter.rows.Swap(0)
	eager := sc
	eager.req.need, eager.req.late = sc.req.late.need, nil
	baseline, handled, err := e.parallelSelectTopK(h.ctx, plan, eager, nil)
	if err != nil || !handled {
		t.Fatal("eager comparison unavailable", handled, err)
	}
	eagerReads := counter.rows.Load()
	want := []string{"8|row-8" + strings.Repeat("p", 4096), "10|row-10" + strings.Repeat("p", 4096)}
	if !slices.Equal(snapshotResultStrings(optimized), want) || !slices.Equal(snapshotResultStrings(baseline), want) {
		t.Fatal("parallel comparison changed complete output")
	}
	// Each heap entrant can probe every immutable segment before finding its
	// newest image. Bound requests by the three retained candidates and lineage,
	// rather than assuming one physical lookup per logical row.
	if lateReads < 3 || lateReads > 3*int64(len(plan.meta.snapshotSegments())) || eagerReads <= lateReads {
		t.Fatal("parallel heap fetched unselected payloads", lateReads, eagerReads)
	}
	t.Logf("payload HASH fetches: late=%d eager=%d", lateReads, eagerReads)
}
