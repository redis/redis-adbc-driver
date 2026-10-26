// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func sharedScanPlan(t *testing.T, h *sqlHarness, q string, params []Value) (*executor, *selectPlan) {
	t.Helper()
	e := &executor{store: &store{client: h.rawClient()}, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
	// Force the formerly materializing branch even on normal-cap CI servers;
	// fresh discovery still verifies the real cap before any cursor is opened.
	e.store.searchCapsOnce.Do(func() { e.store.searchCaps = [2]int64{1_000_000, 137} })
	types := make([]ColType, len(params))
	for i, p := range params {
		types[i] = p.T
	}
	e.params, e.paramTypes = params, types
	parsed, err := ParseScript(q)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(h.ctx, parsed[0].Stmt.(*SelectStmt), types)
	if err != nil {
		t.Fatal(err)
	}
	// These tests explicitly exercise the bounded path. A table undergoing
	// initial/ALTER backfill correctly uses the ordinary HASH fallback instead.
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := e.store.checkRangeReadiness(h.ctx, plan.meta.index())
		if err == nil {
			break
		}
		if !isStatus(err, adbc.StatusInvalidState) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return e, plan
}

func valueRows(rows [][]Value) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(row))
		for j, v := range row {
			cells[j] = partitionValueString(v)
		}
		out[i] = strings.Join(cells, "|")
	}
	return out
}

func TestSharedScanExactResults(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_shared_scan_exact"
	h.setupStreamTable(table, 2500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("ALTER TABLE " + table + " ADD COLUMN amount DECIMAL(12,2)")
	h.exec("UPDATE " + table + " SET amount=12.34 WHERE id % 2 = 0")
	h.exec("UPDATE " + table + " SET label=NULL WHERE id % 7 = 0")
	h.exec("DELETE FROM " + table + " WHERE id >= 100 AND id < 200")
	for _, q := range []string{
		"SELECT id FROM " + table,
		"SELECT id,amount,label FROM " + table + " WHERE id >= 20 AND qty % 3 = 0 LIMIT 400 OFFSET 130",
		"SELECT id + ?, COALESCE(label,'missing'), amount FROM " + table + " WHERE id >= ? LIMIT 300 OFFSET 140",
		"SELECT id FROM " + table + " ORDER BY __rowid LIMIT 500 OFFSET 150",
		"SELECT id FROM " + table + " WHERE id > 99999",
	} {
		t.Run(q, func(t *testing.T) {
			var params []Value
			if strings.Contains(q, "?") {
				params = []Value{intValue(typeInt64, 1<<54), intValue(typeInt64, 10)}
			}
			e, plan := sharedScanPlan(t, h, q, params)
			sc, err := e.prepareScan(h.ctx, plan, params)
			if err != nil {
				t.Fatal(err)
			}
			if !sc.req.where.none && sc.req.ranges == nil {
				t.Fatal("eligible scan did not use bounded ranges")
			}
			actual, err := e.selectScanned(h.ctx, plan, sc, params)
			if err != nil {
				t.Fatal(err)
			}
			// Compare against the previous complete HASH fallback, including one global
			// LIMIT/OFFSET and the exact insertion order across sparse windows.
			old := sc
			old.req.ranges = nil
			old.req.hashScan = true
			// HASH pagination cannot apply Search terms: restore the complete
			// predicate when deliberately replacing the bounded index source.
			old.req.where.residual = plan.sel.Where
			old.req.sortBy = nil
			old.indexSort = false
			old.pushLimit = false
			expected, err := e.selectScanned(h.ctx, plan, old, params)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(valueRows(actual), valueRows(expected)) {
				t.Fatalf("range result differs from HASH fallback: %d versus %d rows", len(actual), len(expected))
			}
			res, err := e.streamSelect(h.ctx, plan, params, &streamOpts{mem: memory.DefaultAllocator, batchRows: 31})
			if err != nil {
				t.Fatal(err)
			}
			rows := res.rows
			if res.stream != nil {
				r := res.stream
				if !r.Schema().Equal(resultSchema(plan.columns())) {
					t.Fatal("schema changed")
				}
				for r.Next() {
					rec := r.RecordBatch()
					if rec.NumRows() > 31 {
						t.Fatal("unbounded batch")
					}
					for i := range int(rec.NumRows()) {
						row := make([]Value, rec.NumCols())
						for j := range row {
							row[j], err = valueAt(rec.Column(j), i)
							if err != nil {
								t.Fatal(err)
							}
						}
						rows = append(rows, row)
					}
				}
				err = r.Err()
				r.Release()
				if err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(valueRows(rows), valueRows(expected)) {
				t.Fatal("stream result/order differs from complete ordinary execution")
			}
		})
	}
}

// An unrestricted server cap used to bypass bounded scanning, allowing a
// streaming SELECT to build a table-sized Search sort before its first page.
func TestSharedScanUnrestrictedCapStillBoundsSort(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_shared_scan_unrestricted"
	h.setupStreamTable(table, 2500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	for _, q := range []string{
		"SELECT id FROM " + table,
		"SELECT id FROM " + table + " WHERE id % 7 = 3",
		"SELECT id FROM " + table + " ORDER BY __rowid",
	} {
		t.Run(q, func(t *testing.T) {
			e, plan := sharedScanPlan(t, h, q, nil)
			e.store.searchCaps[1] = 1 << 31
			sc, err := e.prepareScan(h.ctx, plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			if sc.req.ranges == nil || sc.req.ranges.cap > maxScanRangeRows || sc.req.ranges.cap < 1 {
				t.Fatalf("unbounded Search sort: %+v", sc.req.ranges)
			}
			actual, err := e.selectScanned(h.ctx, plan, sc, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(actual) == 0 {
				t.Fatal("bounded scan omitted all rows")
			}
		})
	}
}

func TestSharedScanFallbacksAndPushdown(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_shared_scan_scope"
	h.setupStreamTable(table, 500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	for _, q := range []string{
		"SELECT id FROM " + table + " ORDER BY id DESC",
		"SELECT id, row_number() OVER (ORDER BY id) FROM " + table,
	} {
		e, p := sharedScanPlan(t, h, q, nil)
		sc, err := e.prepareScan(h.ctx, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if sc.req.ranges != nil || !sc.req.hashScan {
			t.Fatal("unsupported ordering/plan lost its HASH fallback")
		}
	}
	for _, q := range []string{"SELECT id FROM " + table + " WHERE __rowid=2"} {
		e, p := sharedScanPlan(t, h, q, nil)
		sc, err := e.prepareScan(h.ctx, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if sc.req.where.keys == nil || sc.req.ranges != nil {
			t.Fatal("direct-key lookup replaced")
		}
	}
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table+" WHERE id >= 20", nil)
	sc, err := e.prepareScan(h.ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.req.ranges == nil || sc.req.where.query == "*" {
		t.Fatal("selective predicate lost index pushdown")
	}
	it, err := e.openScan(h.ctx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	it.close(h.ctx)
	// Layout and precision guards must retain complete fallback behavior.
	for _, mutate := range []func(*tableMeta){
		func(m *tableMeta) { m.Adopted = true }, func(m *tableMeta) { m.KeyRows = true }, func(m *tableMeta) { m.KeyPrefix = "" },
		func(m *tableMeta) { m.AppIndex = true }, func(m *tableMeta) { m.Schema = "pg_temp" },
	} {
		m := *p.meta
		mutate(&m)
		if managedRowIDTable(&m) {
			t.Fatal("unsafe layout accepted")
		}
	}
	raw := h.rawClient()
	old, err := raw.Get(h.ctx, seqKey(defaultSchema, table)).Result()
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Set(h.ctx, seqKey(defaultSchema, table), old, 0)
	if err := raw.Set(h.ctx, seqKey(defaultSchema, table), exactDoubleLimit, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if bounds, err := e.boundedOrdinaryScan(h.ctx, p, 137); err != nil || bounds != nil {
		t.Fatalf("large-ID fallback: %v", err)
	}
	// Global operators and joins keep the ordinary public execution contract.
	h.expectRows("SELECT COUNT(*) FROM "+table, "500")
	h.expectRows("SELECT DISTINCT qty FROM "+table+" ORDER BY qty LIMIT 3 OFFSET 2", "2", "3", "4")
	h.expectRows("SELECT a.id FROM "+table+" a JOIN "+table+" b ON a.id=b.id WHERE a.id<3 ORDER BY a.id", "0", "1", "2")
}

func TestSharedScanLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_shared_scan_lifecycle"
	h.setupStreamTable(table, 2500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	for _, action := range []string{"release", "cancel", "ddl"} {
		t.Run(action, func(t *testing.T) {
			e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
			owner := &streams{}
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			res, err := e.streamSelect(ctx, p, nil, &streamOpts{mem: memory.DefaultAllocator, batchRows: 10, owners: []*streams{owner}})
			if err != nil || res.stream == nil {
				t.Fatalf("lowered-cap query did not stream: %v", err)
			}
			r := res.stream.(*streamReader)
			if len(r.pending) > 147 {
				t.Fatal("reader buffered beyond batch plus cap window")
			}
			if !r.Next() {
				t.Fatal(r.Err())
			}
			switch action {
			case "release":
				r.Release()
				if owner.closeAll(errConnClosed) != 0 {
					t.Fatal("release retained owner")
				}
				return
			case "cancel":
				owner.closeAll(stateErrorf(stateQueryCanceled, "cancelled"))
				if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
					t.Fatal(r.Err())
				}
			case "ddl":
				h.exec("TRUNCATE " + table)
				for r.Next() {
				}
				if !isStatus(r.Err(), adbc.StatusIO) || !strings.Contains(r.Err().Error(), "truncated or dropped") {
					t.Fatalf("stale read must preserve the ordinary stale-prefix error: %v", r.Err())
				}
			}
			r.Release()
		})
	}
	h.exec("TRUNCATE " + table)
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
	res, err := e.streamSelect(h.ctx, p, nil, &streamOpts{mem: memory.DefaultAllocator, batchRows: 10})
	if err != nil || res.stream != nil || len(res.rows) != 0 {
		t.Fatalf("empty result: %v", err)
	}
}

// Counts wire operations without changing server settings or replaying commands.
type sharedScanCounter struct {
	mu       sync.Mutex
	commands map[string]int
}

func (h *sharedScanCounter) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *sharedScanCounter) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		h.mu.Lock()
		for _, cmd := range cmds {
			h.commands[cmd.Name()]++
		}
		h.mu.Unlock()
		return next(ctx, cmds)
	}
}
func (h *sharedScanCounter) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		h.mu.Lock()
		h.commands[cmd.Name()]++
		h.mu.Unlock()
		return next(ctx, cmd)
	}
}
func TestSharedScanMeasurement(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_shared_scan_measure"
	h.setupStreamTable(table, 5000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	q := "SELECT id FROM " + table
	for _, mode := range []string{"bounded", "hash"} {
		e, p := sharedScanPlan(t, h, q, nil)
		counter := &sharedScanCounter{commands: map[string]int{}}
		// Use a separate connection so hooks do not accumulate across measurements.
		client := h.rawClient()
		switch c := client.(type) {
		case *goredis.Client:
			c.AddHook(counter)
		case *goredis.ClusterClient:
			if err := c.ForEachMaster(h.ctx, func(_ context.Context, n *goredis.Client) error { n.AddHook(counter); return nil }); err != nil {
				t.Fatal(err)
			}
		}
		e.store.client = client
		sc, err := e.prepareScan(h.ctx, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "hash" {
			sc.req.ranges = nil
			sc.req.hashScan = true
			sc.req.where.residual = p.sel.Where
			sc.req.sortBy = nil
			sc.indexSort = false
		}
		started := time.Now()
		rows, err := e.selectScanned(h.ctx, p, sc, nil)
		elapsed := time.Since(started)
		if err != nil || len(rows) != 5000 {
			t.Fatalf("measurement rows=%d err=%v", len(rows), err)
		}
		for i, row := range rows {
			if row[0].I != int64(i) {
				t.Fatal("measurement changed order/results")
			}
		}
		if mode == "bounded" && counter.commands["scan"] != 0 {
			t.Fatal("bounded scan materialized HASH keys")
		}
		t.Logf("mode=%s rows=%d elapsed=%s commands=%s", mode, len(rows), elapsed, fmt.Sprint(counter.commands))
	}
}

func TestSharedScanMultiPageCursor(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_shared_scan_pages"
	h.setupStreamTable(table, 25000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	e, p := sharedScanPlan(t, h, "SELECT id FROM "+table, nil)
	e.store = &store{client: e.store.client}
	e.store.searchCapsOnce.Do(func() { e.store.searchCaps = [2]int64{1_000_000, 12000} })
	sc, err := e.prepareScan(h.ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.req.ranges == nil {
		t.Fatal("expected bounded source")
	}
	for _, early := range []bool{false, true} {
		r, err := e.newScanReader(h.ctx, p, sc.req, nil, &streamOpts{mem: memory.DefaultAllocator, batchRows: 100})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Next() {
			t.Fatal(r.Err())
		}
		id := cursorOf(r)
		if sc.req.ranges.cap > cursorCount && id == 0 {
			t.Fatal("expected a live cursor across multiple pages in one range")
		}
		if early {
			r.Release()
			if id != 0 && h.delCursor(h.rawClient(), table, id) {
				t.Fatal("early release leaked cursor")
			}
			continue
		}
		values := sequence(0, 100, 1)
		rest, _, err := readInts(r)
		r.Release()
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, rest...)
		if !slices.Equal(values, sequence(0, 25000, 1)) {
			t.Fatal("cursor/range boundaries changed exact ordered coverage")
		}
	}
}
