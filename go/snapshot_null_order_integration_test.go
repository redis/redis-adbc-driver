// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func nullOrderPlan(t *testing.T, st *store, ctx context.Context, sql string) selectScan {
	t.Helper()
	sc, err := nullOrderPlanResult(t, st, ctx, sql)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func nullOrderPlanResult(t *testing.T, st *store, ctx context.Context, sql string) (selectScan, error) {
	t.Helper()
	e := &executor{store: st, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
	parsed, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		return selectScan{}, err
	}
	return e.prepareScan(ctx, p, nil)
}

func TestSnapshotNumericNullOrderVisibility(t *testing.T) {
	for _, change := range []string{"replace", "delete", "baseline"} {
		t.Run(change, func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_numeric_null_order"
			const base int64 = 9007199254740992
			h.dropTables(table)
			t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
			from := 0
			if change == "baseline" {
				from = 1
			}
			h.exec(fmt.Sprintf("CREATE TABLE %s AS SELECT i AS id,CASE WHEN i=0 THEN NULL ELSE %d+i END AS v FROM generate_series(%d,600) AS g(i)", table, base, from))
			raw := h.rawClient()
			oldStore, oldCtx, oldPin := h.inspectionSnapshot(raw)
			defer oldPin.release()
			query := "SELECT id,v FROM " + table + " ORDER BY v DESC,id ASC LIMIT 3"
			if change != "baseline" {
				if sc := nullOrderPlan(t, oldStore, oldCtx, query); sc.indexSort {
					t.Fatal("visible NULL admitted NULLS FIRST index ordering")
				}
				if change == "replace" {
					h.exec(fmt.Sprintf("UPDATE %s SET v=%d WHERE id=0", table, base+700))
				} else {
					h.exec("DELETE FROM " + table + " WHERE id=0")
				}
			}
			st, ctx, pin := h.inspectionSnapshot(raw)
			defer pin.release()
			meta, err := st.getTable(ctx, defaultSchema, table)
			if err != nil {
				t.Fatal(err)
			}
			col, _ := meta.column("v")
			if !col.Nullable {
				t.Fatal("fixture must exercise a nullable column")
			}
			sc := nullOrderPlan(t, st, ctx, query)
			if !sc.indexSort || !sc.pushLimit {
				t.Fatal("null-free snapshot did not preserve indexed sorting and limit", sc.indexSort, sc.pushLimit)
			}
			want := []string{fmt.Sprintf("600|%d", base+600), fmt.Sprintf("599|%d", base+599), fmt.Sprintf("598|%d", base+598)}
			if change == "replace" {
				want = []string{fmt.Sprintf("0|%d", base+700), want[0], want[1]}
			}
			ascending := "SELECT id,v FROM " + table + " ORDER BY v ASC NULLS FIRST,id ASC LIMIT 3 OFFSET 1"
			if asc := nullOrderPlan(t, st, ctx, ascending); !asc.indexSort || !asc.pushLimit {
				t.Fatal("null-free ASC NULLS FIRST lost indexed offset")
			}
			other := newSQLHarness(t)
			other.exec("INSERT INTO " + table + " VALUES (-1,NULL)")
			snapshotReapTest(st, h.ctx)
			if retained := nullOrderPlan(t, st, ctx, query); !retained.indexSort || !retained.pushLimit {
				t.Fatal("pinned absence proof used newer NULL rows")
			}
			if got := valueRows(snapshotExecuteTest(t, st, ctx, query).rows); strings.Join(got, ";") != strings.Join(want, ";") {
				t.Fatal("pinned null-free order changed", got, want)
			}
			if asc := nullOrderPlan(t, st, ctx, ascending); !asc.indexSort || !asc.pushLimit {
				t.Fatal("pinned ASC NULLS FIRST used newer NULL rows")
			}
			if got := valueRows(snapshotExecuteTest(t, st, ctx, ascending).rows); strings.Join(got, ";") != fmt.Sprintf("2|%d;3|%d;4|%d", base+2, base+3, base+4) {
				t.Fatal("pinned ASC NULLS FIRST offset changed", got)
			}
			h.expectRows(query, "-1|NULL", want[0], want[1])
			h.expectRows(ascending, fmt.Sprintf("1|%d", base+1), fmt.Sprintf("2|%d", base+2), fmt.Sprintf("3|%d", base+3))
			fresh, freshCtx, freshPin := h.inspectionSnapshot(raw)
			defer freshPin.release()
			if next := nullOrderPlan(t, fresh, freshCtx, query); next.indexSort {
				t.Fatal("new visible NULL reused an old absence proof")
			}
			if next := nullOrderPlan(t, fresh, freshCtx, ascending); next.indexSort {
				t.Fatal("visible NULL admitted ASC NULLS FIRST offset")
			}
			filtered := "SELECT id,v FROM " + table + " WHERE id>=0 ORDER BY v DESC,id ASC LIMIT 3"
			if next := nullOrderPlan(t, fresh, freshCtx, filtered); !next.indexSort || !next.pushLimit {
				t.Fatal("NULL outside the candidate predicate prevented indexed ordering")
			}
			if got := valueRows(snapshotExecuteTest(t, fresh, freshCtx, filtered).rows); strings.Join(got, ";") != strings.Join(want, ";") {
				t.Fatal("filtered NULL order changed", got, want)
			}
			if change != "baseline" {
				if initial := nullOrderPlan(t, oldStore, oldCtx, query); initial.indexSort {
					t.Fatal("old NULL reader inherited newer facts")
				}
				got := valueRows(snapshotExecuteTest(t, oldStore, oldCtx, query).rows)
				wantOld := fmt.Sprintf("0|NULL;600|%d;599|%d", base+600, base+599)
				if strings.Join(got, ";") != wantOld {
					t.Fatal("initial NULL generation changed", got)
				}
			}
		})
	}
}

type nullOrderProofHook struct {
	active              atomic.Bool
	index, prefix, mode string
	probes, bounded     atomic.Int64
	cancel              context.CancelFunc
}

func (h *nullOrderProofHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *nullOrderProofHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		if h.active.Load() && h.mode == "directory denied" {
			key := snapshotMembers(snapshotDataID(h.prefix))
			for _, cmd := range cmds {
				if cmd.Name() == "zrange" && len(cmd.Args()) > 1 && fmt.Sprint(cmd.Args()[1]) == key {
					err := readinessServerError("NOPERM row directory unavailable")
					for _, queued := range cmds {
						queued.SetErr(err)
					}
					h.probes.Add(1)
					return err
				}
			}
		}
		return next(ctx, cmds)
	}
}
func (h *nullOrderProofHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if !h.active.Load() {
			return next(ctx, cmd)
		}
		args := cmd.Args()
		if len(args) > 1 && fmt.Sprint(args[1]) == h.index {
			if cmd.Name() == "ft.info" && h.mode == "info warning" {
				cmd.(interface{ SetVal(any) }).SetVal([]any{"indexing", int64(0), "percent_indexed", "1", "num_docs", int64(3), "index_definition", []any{"prefixes", []any{h.prefix}}, "warning", []any{"Timeout limit was reached"}})
				h.probes.Add(1)
				return nil
			}
			if cmd.Name() == "ft.info" && h.mode == "info denied" {
				err := readinessServerError("NOPERM immutable index metadata unavailable")
				cmd.SetErr(err)
				h.probes.Add(1)
				return err
			}
			if cmd.Name() == "ft.aggregate" {
				for _, arg := range args[2:] {
					if token := fmt.Sprint(arg); token == "SORTBY" || token == "LIMIT" {
						h.bounded.Add(1)
						break
					}
				}
			}
			if cmd.Name() == "ft.info" && (h.mode == "building" || h.mode == "incomplete") {
				building, docs := int64(0), int64(3)
				if h.mode == "building" {
					building = 1
				} else {
					docs = 2
				}
				cmd.(interface{ SetVal(any) }).SetVal([]any{"indexing", building, "percent_indexed", "1", "num_docs", docs, "index_definition", []any{"prefixes", []any{h.prefix}}})
				h.probes.Add(1)
				return nil
			}
			if cmd.Name() == "ft.search" && len(args) > 2 && strings.Contains(fmt.Sprint(args[2]), "-@v:[-inf +inf]") {
				h.probes.Add(1)
				if h.mode == "cancel" {
					h.cancel()
					cmd.SetErr(context.Canceled)
					return context.Canceled
				}
				if h.mode == "denied" {
					err := readinessServerError("NOPERM optional NULL-order proof unavailable")
					cmd.SetErr(err)
					return err
				}
				if h.mode == "warning" {
					cmd.(interface{ SetVal(any) }).SetVal(map[any]any{"total_results": int64(0), "warning": []any{"Timeout limit was reached"}})
					return nil
				}
			}
		}
		return next(ctx, cmd)
	}
}

func TestSnapshotNumericNullOrderUnavailableProof(t *testing.T) {
	for _, mode := range []string{"denied", "warning", "building", "incomplete", "cancel", "info denied", "directory denied", "info warning"} {
		t.Run(mode, func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_null_order_proof"
			h.dropTables(table)
			t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
			h.exec("CREATE TABLE " + table + " AS SELECT i AS id,i AS v FROM generate_series(1,3) AS g(i)")
			meta := h.inspectTable(h.rawClient(), defaultSchema, table)
			hook := &nullOrderProofHook{index: meta.index(), prefix: meta.prefix(), mode: mode}
			st, ctx, pin := h.inspectionSnapshot(h.rawClient(hook))
			defer pin.release()
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				hook.cancel = cancel
				defer cancel()
			}
			st.searchCapsOnce.Do(func() { st.searchCaps = [2]int64{1000000, 1 << 31} })
			// Establish ordinary descriptor validation before changing only
			// the optional proof response for this pinned statement.
			if _, err := st.getTable(ctx, defaultSchema, table); err != nil {
				t.Fatal(err)
			}
			hook.active.Store(true)
			query := "SELECT id,v FROM " + table + " ORDER BY v DESC,id ASC LIMIT 2"
			if mode == "cancel" {
				_, err := nullOrderPlanResult(t, st, ctx, query)
				if !isStatus(err, adbc.StatusCancelled) && !errors.Is(err, context.Canceled) {
					t.Fatal("NULL-order proof lost cancellation", err)
				}
				h.expectRows(query, "3|3", "2|2")
			} else if mode == "building" || mode == "incomplete" || mode == "info denied" || mode == "directory denied" || mode == "info warning" {
				_, err := nullOrderPlanResult(t, st, ctx, query)
				want := adbc.StatusIO
				if mode == "building" {
					want = adbc.StatusInvalidState
				}
				if !isStatus(err, want) {
					t.Fatal("known incomplete index did not refuse the read", err, want)
				}
				h.expectRows(query, "3|3", "2|2")
			} else {
				if sc := nullOrderPlan(t, st, ctx, query); sc.indexSort || sc.pushLimit {
					t.Fatal("unavailable proof admitted indexed ordering")
				}
				got := valueRows(snapshotExecuteTest(t, st, ctx, query).rows)
				if strings.Join(got, ";") != "3|3;2|2" {
					t.Fatal("proof fallback changed rows", got)
				}
			}
			if hook.probes.Load() == 0 || hook.bounded.Load() != 0 {
				t.Fatal("unproved NULL order admitted a bounded index read", hook.probes.Load(), hook.bounded.Load())
			}
		})
	}
}

func TestSnapshotNullOrderConservativeFallback(t *testing.T) {
	for _, mode := range []string{"wide IDs", "string", "private output"} {
		t.Run(mode, func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_null_order_fallback"
			h.dropTables(table)
			t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
			h.exec("CREATE TABLE " + table + " (id INTEGER,v INTEGER,s VARCHAR)")
			h.exec("INSERT INTO " + table + " VALUES (1,1,'a'),(2,2,'z'),(3,3,'m')")
			if mode == "wide IDs" {
				h.sparseSnapshotTable(table, 9007199254740993, map[int64]int64{3: 9007199254740993})
			}
			st, ctx, pin := h.inspectionSnapshot(h.rawClient())
			defer pin.release()
			if mode == "private output" {
				var work string
				ctx, pin, work = snapshotStageSQL(t, st, h.ctx, table, "INSERT INTO "+table+" VALUES (4,4,'q')")
				defer func() { st.abortSnapshot(pin, work); pin.release() }()
			}
			query := "SELECT id FROM " + table + " ORDER BY v DESC LIMIT 3"
			want := "3;2;1"
			if mode == "private output" {
				want = "4;3;2"
			}
			if mode == "string" {
				query = "SELECT id FROM " + table + " ORDER BY s DESC LIMIT 3"
				want = "2;3;1"
			}
			if sc := nullOrderPlan(t, st, ctx, query); sc.indexSort || sc.pushLimit {
				t.Fatal("unproved NULL ordering used index")
			}
			if got := valueRows(snapshotExecuteTest(t, st, ctx, query).rows); strings.Join(got, ";") != want {
				t.Fatal("fallback order changed", got, want)
			}
		})
	}
}

func TestSnapshotNullOrderSkipsUnneededProof(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_null_order_no_probe"
	h.dropTables(table)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("CREATE TABLE " + table + " AS SELECT i AS id,i AS v FROM generate_series(1,3) AS g(i)")
	meta := h.inspectTable(h.rawClient(), defaultSchema, table)
	hook := &nullOrderProofHook{index: meta.index(), prefix: meta.prefix(), mode: "denied"}
	st, ctx, pin := h.inspectionSnapshot(h.rawClient(hook))
	defer pin.release()
	st.searchCapsOnce.Do(func() { st.searchCaps = [2]int64{1000000, 1 << 31} })
	if _, err := st.getTable(ctx, defaultSchema, table); err != nil {
		t.Fatal(err)
	}
	hook.active.Store(true)
	for _, tc := range []struct{ query, want string }{
		{"SELECT id,v FROM " + table + " WHERE 1=0 ORDER BY v DESC LIMIT 2", ""},
		{"SELECT id,v FROM " + table + " WHERE __rowid=2 ORDER BY v DESC LIMIT 2", "2|2"},
		{"SELECT id,v FROM " + table + " ORDER BY v DESC,id+0 LIMIT 2", "3|3;2|2"},
	} {
		nullOrderPlan(t, st, ctx, tc.query)
		if got := valueRows(snapshotExecuteTest(t, st, ctx, tc.query).rows); strings.Join(got, ";") != tc.want {
			t.Fatal("unnecessary proof path changed rows", got, tc.want)
		}
	}
	if hook.probes.Load() != 0 {
		t.Fatal("empty/direct/unsortable input ran an unnecessary absence proof", hook.probes.Load())
	}
}
