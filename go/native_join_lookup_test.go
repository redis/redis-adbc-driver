// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type nativeOrderReplies struct {
	*snapshotPageReplies
	rows int64
}

func (h *nativeOrderReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		// joinOrder's count is over the full source before dynamic join demand.
		if cmd.Name() == "ft.search" && fmt.Sprint(cmd.Args()[3]) == "LIMIT" {
			cmd.(onceCmd).SetVal([]any{h.rows})
			return nil
		}
		return h.reply(cmd)
	}
}

func nativeQ8OrderFixture(t *testing.T) (*executor, *selectPlan, context.Context, *nativeOrderReplies) {
	t.Helper()
	var dates, stores []string
	for i := 0; i < 91; i++ {
		dates = append(dates, fmt.Sprintf("(%d,2,1998)", 78999+i))
	}
	for i := 1; i <= 12; i++ {
		stores = append(stores, fmt.Sprintf("(%d,'store%d','24199')", i, i))
	}
	// Preserve Q8's actual parsed outer graph and predicates; V1 is its already
	// materialized ZIP cohort. This cost fixture does not execute external SQL.
	q := `WITH store_sales(ss_store_sk,ss_sold_date_sk,ss_net_profit) AS (VALUES (1,78999,CAST(1.25 AS DECIMAL(7,2)))), date_dim(d_date_sk,d_qoy,d_year) AS (VALUES ` + strings.Join(dates, ",") + `), store(s_store_sk,s_store_name,s_zip) AS (VALUES ` + strings.Join(stores, ",") + `), v1(ca_zip) AS (VALUES ('24128'),('24129'),('24130'),('24131'),('24132')) SELECT s_store_name,SUM(ss_net_profit) FROM store_sales,date_dim,store,v1 WHERE ss_store_sk=s_store_sk AND ss_sold_date_sk=d_date_sk AND d_qoy=2 AND d_year=1998 AND SUBSTRING(s_zip,1,2)=SUBSTRING(v1.ca_zip,1,2) GROUP BY s_store_name ORDER BY s_store_name LIMIT 100`
	e, p := deferredTestPlan(t, q)
	native, ctx, base := snapshotPageFixture(t)
	n := int64(2880404)
	native.meta.SnapshotRows = &n
	native.meta.Columns = append([]columnMeta(nil), p.meta.join.items[0].base.Columns...)
	for i := range native.meta.Columns {
		native.meta.Columns[i].Indexed = true
		native.meta.Columns[i].PlainIndex = true
	}
	p.meta.join.items[0].base = native.meta
	seg := native.meta.snapshotSegments()[0]
	base.segments = map[string]snapshotSegment{seg.Index: seg}
	base.rows = map[string]aggRow{seg.Prefix + "1": {rowIDField: "1", "ss_store_sk": "1", "ss_sold_date_sk": "78999", "ss_net_profit": "1.25"}}
	base.matching = map[string]bool{seg.Prefix + "1": true}
	hook := &nativeOrderReplies{snapshotPageReplies: base, rows: n}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	native.store.client = client
	e.store = native.store
	return e, p, ctx, hook
}

func TestNativeQ8ParsedSelectionAndAllLookupPairs(t *testing.T) {
	e, p, ctx, h := nativeQ8OrderFixture(t)
	jp := p.meta.join
	wp := make([]wherePlan, len(jp.items))
	for i, it := range jp.items {
		var err error
		wp[i], err = e.planWhere(ctx, andAll(it.pushed), it.view(), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	steps, err := e.joinOrder(ctx, jp, wp)
	if err != nil {
		t.Fatal(err)
	}
	var order []int
	for _, step := range steps {
		order = append(order, step.item)
	}
	if !reflect.DeepEqual(order, []int{3, 2, 1, 0}) {
		t.Fatalf("Q8 native fact preceded disconnected91-day dimension: %v", order)
	}
	need := map[string]bool{}
	for _, c := range p.meta.Columns {
		need[c.Name] = true
	}
	rows, err := e.runJoin(ctx, jp, need, nil)
	if err != nil || len(rows) != 5 {
		t.Fatalf("native outer shape rows%d err%v", len(rows), err)
	}
	found := false
	for _, args := range h.searches {
		query := fmt.Sprint(args[2])
		if strings.Contains(query, "@ss_store_sk:") && strings.Contains(query, "@ss_sold_date_sk:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("native lookup used only its first equality pair: %v", h.searches)
	}
}

func TestNativeDimensionOrderingRequiresLargePublishedSource(t *testing.T) {
	for _, kind := range []string{"private", "unpinned", "small_filtered", "small_physical", "over_product", "unmanaged", "operation", "write", "retained_context"} {
		t.Run(kind, func(t *testing.T) {
			e, p, ctx, _ := nativeQ8OrderFixture(t)
			jp := p.meta.join
			counts := []int64{2880404, 91, 12, 5}
			chosen := map[int]bool{2: true, 3: true}
			connected := []bool{true, false, false, false}
			product := int64(60)
			switch kind {
			case "private":
				scope := snapshotOf(ctx)
				scope.write = true
				scope.owned = map[string]bool{snapshotDataID(jp.items[0].base.prefix()): true}
			case "unpinned":
				snapshotOf(ctx).pin = nil
			case "small_filtered":
				counts[0] = 500
			case "small_physical":
				n := int64(500)
				jp.items[0].base.SnapshotRows = &n
			case "over_product":
				product = 91
			case "unmanaged":
				jp.items[0].base.Unmanaged = true
			case "operation":
				snapshotOf(ctx).operation = true
			case "write":
				snapshotOf(ctx).write = true
			case "retained_context":
				ctx = context.WithValue(ctx, retainedAllocationKey{}, newRetainedBudget(1<<20))
			}
			if got := e.boundedDeferredDimension(ctx, jp, counts, chosen, connected, 0, product); got != 0 {
				t.Fatalf("unsafe native postponement: %d", got)
			}
		})
	}
}

func TestNativeLookupPairCapsNullAndPrecisionFallback(t *testing.T) {
	e := &executor{}
	ctx := context.Background()
	view := &tableMeta{Columns: []columnMeta{{Name: "ra", Field: "a", Type: typeInt64, Indexed: true, PlainIndex: true}, {Name: "rb", Field: "b", Type: typeInt64, Indexed: true, PlainIndex: true}}}
	env := e.newEnv(ctx, map[string]ColType{"la": typeInt64, "lb": typeInt64}, nil)
	pairs := []equiPair{{left: &ColumnRef{Name: "la"}, right: &ColumnRef{Name: "ra"}}, {left: &ColumnRef{Name: "lb"}, right: &ColumnRef{Name: "rb"}}}
	original := &Literal{V: boolValue(true)}
	wp := wherePlan{query: "@static:[1 1]", original: original, residual: original}
	left := []map[string]Value{{"la": intValue(typeInt64, 1), "lb": intValue(typeInt64, 7)}, {"la": intValue(typeInt64, 2), "lb": intValue(typeInt64, 8)}, {"la": nullValue(typeInt64), "lb": intValue(typeInt64, 7)}}
	got, err := e.nativeJoinLookups(ctx, env, pairs, left, view, wp)
	if err != nil || !strings.Contains(got.query, "@a:") || !strings.Contains(got.query, "@b:") || got.original != original || got.residual != original {
		t.Fatalf("independent pair/residual loss: %+v %v", got, err)
	}
	left = nil
	for i := 0; i <= maxUnionTerms; i++ {
		left = append(left, map[string]Value{"la": intValue(typeInt64, int64(i)), "lb": intValue(typeInt64, 7)})
	}
	got, err = e.nativeJoinLookups(ctx, env, pairs, left, view, wherePlan{query: "*"})
	if err != nil || strings.Contains(got.query, "@a:") || !strings.Contains(got.query, "@b:") {
		t.Fatal("over-cap pair discarded safe second key")
	}
	left = []map[string]Value{{"la": intValue(typeInt64, 9007199254740993), "lb": intValue(typeInt64, 7)}}
	got, err = e.nativeJoinLookups(ctx, env, pairs, left, view, wherePlan{query: "*"})
	if err != nil || strings.Contains(got.query, "@a:") || !strings.Contains(got.query, "@b:") {
		t.Fatal("unsafe large integer Search equality was introduced")
	}
	left[0]["lb"] = nullValue(typeInt64)
	got, err = e.nativeJoinLookups(ctx, env, pairs, left, view, wherePlan{query: "*"})
	if err != nil || !got.none {
		t.Fatal("proven NULL equality set did not suppress right matches")
	}
}

func TestNativeDimensionProduct8192BoundaryAndUnion1000Limit(t *testing.T) {
	for _, count := range []int{8192, 8193} {
		e, p, ctx, _ := nativeQ8OrderFixture(t)
		jp := p.meta.join
		jp.items[1].base.mem = make([]map[string]Value, count)
		counts := []int64{2880404, int64(count), 1, 1}
		got := e.boundedDeferredDimension(ctx, jp, counts, map[int]bool{2: true, 3: true}, []bool{true, false, false, false}, 0, 1)
		if (got == 1) != (count == 8192) {
			t.Fatalf("native product%d selected%d", count, got)
		}
	}
	for _, count := range []int{1000, 1001} {
		e, jp, _ := deferredOrderFixture(t, count, 1)
		got := e.boundedDeferredDimension(context.Background(), jp, []int64{1 << 60, int64(count), 1}, map[int]bool{2: true}, []bool{true, false, false}, 0, 1)
		if (got == 1) != (count == 1000) {
			t.Fatalf("UNION cap changed: product%d selected%d", count, got)
		}
	}
	for _, tc := range []struct{ a, b, want int64 }{{64, 128, 8192}, {1, 8193, 8193}, {9223372036854775807, 9223372036854775807, 8193}, {8192, 2, 8193}, {0, 9223372036854775807, 0}} {
		if got := boundedDimensionProduct(tc.a, tc.b, nativeDimensionProductRows); got != tc.want {
			t.Fatalf("planning product%d*%d=%d want%d", tc.a, tc.b, got, tc.want)
		}
	}
}
