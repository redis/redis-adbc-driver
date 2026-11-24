// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestDeferredUnionStreamPagesNamesAndResidual(t *testing.T) {
	e, p := deferredTestPlan(t, `WITH a(k,v) AS (VALUES (1,10),(1,20),(2,30),(NULL,40)) SELECT key,amount FROM (SELECT k,v FROM a UNION ALL SELECT k,v FROM a UNION ALL SELECT k,v FROM a) u(key,amount) WHERE key=1 AND amount+1=21`)
	ctx := context.Background()
	meta := p.meta
	prefix := ""
	if meta.join != nil && len(meta.join.items) == 1 {
		prefix = meta.join.items[0].prefix
		meta = meta.join.items[0].view()
	}
	wp, err := e.planWhere(ctx, p.sel.Where, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	handled, err := e.scanDeferredUnionPages(ctx, scanRequest{meta: meta, where: wp, need: map[string]bool{prefix + "key": true, prefix + "amount": true}}, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			got = append(got, row[prefix+"key"].Text()+"|"+row[prefix+"amount"].Text())
		}
		return nil
	})
	if err != nil || !handled || !reflect.DeepEqual(got, []string{"1|20", "1|20", "1|20"}) {
		t.Fatalf("stream=%v %v %v", handled, got, err)
	}
	if bound, ok := e.deferredUnionRowBound(ctx, meta); !ok || bound != 12 {
		t.Fatalf("upper bound %d %v", bound, ok)
	}
	// A qualified fact view uses positional output names, never physical aliases.
	item := &joinItem{base: meta, prefix: "f."}
	qualified := item.view()
	if bound, ok := e.deferredUnionRowBound(ctx, qualified); !ok || bound != 12 {
		t.Fatal("qualified bound lost")
	}
	count := 0
	handled, err = e.scanDeferredUnionPages(ctx, scanRequest{meta: qualified, need: map[string]bool{"f." + prefix + "key": true}}, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			if _, ok := row["f."+prefix+"key"]; !ok {
				t.Fatal("unqualified output")
			}
			count++
		}
		return nil
	})
	if err != nil || !handled || count != 12 {
		t.Fatalf("qualified stream %d %v", count, err)
	}
}

func TestDeferredUnionStreamCaseDecimalWeeklyInputs(t *testing.T) {
	e, p := deferredTestPlan(t, `WITH a(w,y,day,price) AS (VALUES (7,2000,'Sunday',CAST(10.05 AS DECIMAL(7,2))),(7,2001,'Sunday',CAST(0.10 AS DECIMAL(7,2))),(7,2001,'Monday',CAST(2.20 AS DECIMAL(7,2))),(7,2001,'Monday',CAST(NULL AS DECIMAL(7,2)))) SELECT w,SUM(CASE WHEN day='Sunday' THEN price ELSE NULL END),SUM(CASE WHEN day='Monday' THEN price ELSE NULL END) FROM (SELECT w,y,day,price FROM a UNION ALL SELECT w,y,day,price FROM a) u GROUP BY w`)
	ctx := context.Background()
	aggs := p.aggregateCalls()
	types := p.meta.types()
	env := e.newEnv(ctx, types, nil)
	accs := []*accumulator{newAccumulator(aggs[0]), newAccumulator(aggs[1])}
	rowsSeen := 0
	handled, err := e.scanDeferredUnionPages(ctx, scanRequest{meta: p.meta, need: map[string]bool{"w": true, "y": true, "day": true, "price": true}}, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			rowsSeen++
			env.row = row
			for _, acc := range accs {
				if err := acc.addRow(env); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil || !handled || rowsSeen != 8 {
		t.Fatalf("weekly complete inputs %d %v", rowsSeen, err)
	}
	for i, want := range []string{"20.30", "4.40"} {
		typ, err := inferType(aggs[i], types, nil)
		if err != nil {
			t.Fatal(err)
		}
		v, err := accs[i].result(env, typ)
		if err != nil || v.Text() != want {
			t.Fatalf("CASE decimal sum%d=%s %v", i, v.Text(), err)
		}
	}
}

func TestDeferredUnionStreamFallbackAndCancellation(t *testing.T) {
	volatileE, volatileP := deferredTestPlan(t, `WITH a(k) AS (VALUES (1),(2)) SELECT * FROM (SELECT k,random() AS v FROM a UNION ALL SELECT k,random() FROM a) u`)
	if handled, err := volatileE.scanDeferredUnionPages(context.Background(), scanRequest{meta: volatileP.meta}, nil, func([]map[string]Value) error { t.Fatal("volatile source consumed"); return nil }); handled || err != nil {
		t.Fatal("volatile source did not retain original path")
	}
	if got := memQuery(`WITH a(k) AS (VALUES (1),(2)) SELECT * FROM (SELECT k,1/(k-2) AS v FROM a UNION ALL SELECT k,k FROM a) u WHERE k=1`); !strings.Contains(got, "division by zero") {
		t.Fatalf("projected error disappeared: %s", got)
	}
	e, p := deferredTestPlan(t, `WITH a(k) AS (VALUES (1),(2)) SELECT * FROM (SELECT k FROM a UNION ALL SELECT k FROM a) u`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if handled, err := e.scanDeferredUnionPages(ctx, scanRequest{meta: p.meta}, nil, func([]map[string]Value) error { t.Fatal("canceled callback"); return nil }); !handled || err == nil {
		t.Fatal("cancellation lost")
	}
	limit := [2]int64{0, 1}
	if handled, err := e.scanDeferredUnionPages(context.Background(), scanRequest{meta: p.meta, limit: &limit}, nil, func([]map[string]Value) error { t.Fatal("unsupported request consumed"); return nil }); handled || err != nil {
		t.Fatal("unsupported shape not refused")
	}
	p.meta.view.plan.setop.left.items[0].expr = &Func{Name: "RANDOM"}
	if _, ok := e.deferredUnionRowBound(context.Background(), p.meta); ok {
		t.Fatal("unsafe mutated source accepted")
	}
}

type unionStreamReplies struct {
	*streamNativeReplies
	ranges      int
	failAt      int
	sourceError error
}

func (h *unionStreamReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		if cmd.Name() != "zrange" {
			return h.reply(cmd)
		}
		h.ranges++
		if h.ranges == h.failAt {
			cmd.SetErr(h.sourceError)
			return h.sourceError
		}
		start := cmd.Args()[2].(int64)
		end := cmd.Args()[3].(int64)
		n := int64(len(h.rows))
		var ids []string
		for i := start; i <= end && i < n; i++ {
			ids = append(ids, fmt.Sprintf("%020d", i+1))
		}
		cmd.(*goredis.StringSliceCmd).SetVal(ids)
		return nil
	}
}
func unionNativeStreamFixture(t *testing.T) (*executor, *selectPlan, context.Context, *unionStreamReplies) {
	t.Helper()
	d, ctx, base := streamNativeFixture(t)
	e, p := deferredTestPlan(t, `WITH a(v) AS (VALUES (1)) SELECT v FROM (SELECT v FROM a UNION ALL SELECT v FROM a) u`)
	n := int64(wideFetchChunk + 1)
	d.meta.SnapshotRows = &n
	d.meta.Columns = []columnMeta{{Name: "v", Type: typeInt32}}
	seg := d.meta.snapshotSegments()[0]
	base.segments[seg.Index] = seg
	base.rows = map[string]aggRow{}
	for i := int64(1); i <= n; i++ {
		base.rows[seg.Prefix+strconv.FormatInt(i, 10)] = aggRow{rowIDField: strconv.FormatInt(i, 10), "v": strconv.FormatInt(i, 10)}
	}
	hook := &unionStreamReplies{streamNativeReplies: base, sourceError: errors.New("authoritative directory reply lost")}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	d.store.client = client
	e.store = d.store
	sp := p.meta.view.plan.setop
	sp.left.meta = d.meta
	sp.right.meta = d.meta
	return e, p, ctx, hook
}
func TestDeferredUnionStreamBoundedNativePagesAndFailurePriority(t *testing.T) {
	e, p, ctx, h := unionNativeStreamFixture(t)
	count, maxPage := 0, 0
	handled, err := e.scanDeferredUnionPages(ctx, scanRequest{meta: p.meta, need: map[string]bool{"v": true}}, nil, func(rows []map[string]Value) error { count += len(rows); maxPage = max(maxPage, len(rows)); return nil })
	if !handled || err != nil || count != 2*(wideFetchChunk+1) || maxPage > pipelineChunk || h.ranges != 4 {
		t.Fatalf("native pages count%d max%d ranges%d err%v", count, maxPage, h.ranges, err)
	}
	e, p, ctx, h = unionNativeStreamFixture(t)
	h.failAt = 3
	consumer := errors.New("consumer accumulator failed")
	calls := 0
	handled, err = e.scanDeferredUnionPages(ctx, scanRequest{meta: p.meta, need: map[string]bool{"v": true}}, nil, func([]map[string]Value) error { calls++; return consumer })
	if !handled || err == nil || errors.Is(err, consumer) || !strings.Contains(err.Error(), h.sourceError.Error()) || calls != 1 || h.ranges != 3 {
		t.Fatalf("source failure did not precede consumer: calls%d ranges%d %v", calls, h.ranges, err)
	}
}

func TestDeferredUnionStreamBoundOverflowAndOpaqueFallback(t *testing.T) {
	e, p, ctx, _ := unionNativeStreamFixture(t)
	sp := p.meta.view.plan.setop
	n := int64(math.MaxInt64)
	sp.left.meta.SnapshotRows = &n
	if _, ok := e.deferredUnionRowBound(ctx, p.meta); ok {
		t.Fatal("overflowed source sum accepted")
	}
	sp.left.meta.Live = true
	if handled, err := e.scanDeferredUnionPages(ctx, scanRequest{meta: p.meta}, nil, func([]map[string]Value) error { t.Fatal("opaque fallback consumed"); return nil }); handled || err != nil {
		t.Fatal("opaque source must use original path")
	}
}

func TestDeferredUnionStreamRetainedContextRefusesBeforeInput(t *testing.T) {
	e, p, ctx, h := unionNativeStreamFixture(t)
	budget := newRetainedBudget(0)
	ctx = context.WithValue(ctx, retainedAllocationKey{}, budget)
	handled, err := e.scanDeferredUnionPages(ctx, scanRequest{meta: p.meta, need: map[string]bool{"v": true}}, nil, func([]map[string]Value) error { t.Fatal("retained callback consumed input"); return nil })
	if handled || err != nil || h.ranges != 0 || h.commands != 0 || budget.failed.Load() {
		t.Fatalf("retained fallback touched source/admission: handled%v ranges%d err%v", handled, h.ranges, err)
	}
}
