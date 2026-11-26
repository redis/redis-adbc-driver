// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestStarFactDemandIndependentCompositeKeysAndExactIntegers(t *testing.T) {
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Type: typeInt64}, {Name: "f.b", Type: typeInt64}}}
	dims := []starDimension{{factCols: []string{"f.a", "f.b"}, dimCols: []string{"d.a", "d.b"}, buckets: map[string][]map[string]Value{"one": {{"d.a": intValue(typeInt64, 9007199254740993), "d.b": intValue(typeInt64, 10)}}, "two": {{"d.a": intValue(typeInt64, 2), "d.b": intValue(typeInt64, 20)}, {"d.a": intValue(typeInt64, 2), "d.b": intValue(typeInt64, 20)}}}}}
	ctx := context.Background()
	demand, err := starFactDimensionDemand(ctx, fact, dims, maxUnionTerms)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{}
	env := e.newEnv(ctx, fact.types(), nil)
	for _, tt := range []struct {
		a, b int64
		want bool
	}{{9007199254740993, 20, true}, {9007199254740992, 20, false}, {2, 10, true}, {2, 99, false}} {
		env.row = map[string]Value{"f.a": intValue(typeInt64, tt.a), "f.b": intValue(typeInt64, tt.b)}
		v, err := env.eval(demand)
		if err != nil || v.Null || (v.I != 0) != tt.want {
			t.Fatalf("necessary key set (%d,%d): %v %v", tt.a, tt.b, v, err)
		}
	}
	again, err := starFactDimensionDemand(ctx, fact, dims, maxUnionTerms)
	if err != nil || !reflect.DeepEqual(demand, again) {
		t.Fatal("bucket iteration changed query construction")
	}
}

func TestStarFactDemandPairCapAndUnsafeFallback(t *testing.T) {
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Type: typeInt64}, {Name: "f.b", Type: typeInt64}}}
	var rows []map[string]Value
	for i := 0; i <= maxUnionTerms; i++ {
		rows = append(rows, map[string]Value{"d.a": intValue(typeInt64, int64(i)), "d.b": intValue(typeInt64, 7)})
	}
	dims := []starDimension{{factCols: []string{"f.a", "f.b"}, dimCols: []string{"d.a", "d.b"}, buckets: map[string][]map[string]Value{"all": rows}}}
	demand, err := starFactDimensionDemand(context.Background(), fact, dims, maxUnionTerms)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]bool{}
	columnRefs(demand, refs)
	if refs["f.a"] || !refs["f.b"] {
		t.Fatalf("over-cap pair poisoned other key demand: %v", refs)
	}
	rows[0]["d.b"] = intValue(typeInt32, 7)
	demand, err = starFactDimensionDemand(context.Background(), fact, dims, maxUnionTerms)
	if err != nil || demand != nil {
		t.Fatal("unsafe actual type should ignore only its pair")
	}
}

func TestStarFactDemandNullEmptyAndCancellation(t *testing.T) {
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Type: typeInt64}}}
	dim := starDimension{factCols: []string{"f.a"}, dimCols: []string{"d.a"}}
	if demand, err := starFactDimensionDemand(context.Background(), fact, []starDimension{dim}, maxUnionTerms); err != nil || demand != nil {
		t.Fatal("unprepared bucket is not an emptiness proof")
	}
	dim.buckets = map[string][]map[string]Value{}
	demand, err := starFactDimensionDemand(context.Background(), fact, []starDimension{dim}, maxUnionTerms)
	lit, ok := demand.(*Literal)
	if err != nil || !ok || lit.V.Null || lit.V.I != 0 {
		t.Fatal("proven empty dimension must produce FALSE")
	}
	dim.buckets = map[string][]map[string]Value{"null": {{"d.a": nullValue(typeInt64)}}}
	demand, err = starFactDimensionDemand(context.Background(), fact, []starDimension{dim}, maxUnionTerms)
	lit, ok = demand.(*Literal)
	if err != nil || !ok || lit.V.I != 0 {
		t.Fatal("NULL keys fabricated equality")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := starFactDimensionDemand(ctx, fact, []starDimension{dim}, maxUnionTerms); err == nil {
		t.Fatal("canceled demand planning continued")
	}
}

func TestStarFactDemandReachesNativeDeferredBranches(t *testing.T) {
	e, p, ctx, h := factDemandNativeFixture(t)
	native := p.meta.view.plan.setop.left.meta
	native.Columns[0].Indexed = true
	native.Columns[0].PlainIndex = true
	h.matching[native.prefix()+"1"] = true
	dim := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "id", Type: typeInt32}}, mem: []map[string]Value{{"id": intValue(typeInt32, 1)}, {"id": intValue(typeInt32, 1)}}}
	eq := &Binary{Op: "=", L: &ColumnRef{Name: "f.v"}, R: &ColumnRef{Name: "d.id"}}
	jp := &joinPlan{items: []*joinItem{{base: p.meta, prefix: "f."}, {base: dim, prefix: "d.", kind: "INNER", on: eq}}}
	meta := &tableMeta{isMem: true, join: jp, Columns: []columnMeta{{Name: "f.v", Type: typeInt32}, {Name: "d.id", Type: typeInt32}}}
	count := 0
	err := e.scanPages(ctx, scanRequest{meta: meta, where: wherePlan{query: "*", residual: eq}, need: map[string]bool{"f.v": true}, streamJoin: true}, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			if row["f.v"].I != 1 {
				t.Fatal("fact predicate admitted unexpected row")
			}
			count++
		}
		return nil
	})
	if err != nil || count != 4 || len(h.searches) != 4 || h.ranges != 0 {
		t.Fatalf("prepared key demand did not reach both native UNION leaves: rows%d searches%d directoryPages%d err%v", count, len(h.searches), h.ranges, err)
	}
	for _, args := range h.searches {
		if !strings.Contains(args[2].(string), "@v:") {
			t.Fatalf("missing native typed key filter: %v", args)
		}
	}
}

func TestStarFactDemandUsesLiteralInListHashSet(t *testing.T) {
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.key", Type: typeInt64}}}
	var rows []map[string]Value
	for i := int64(0); i < inListMin; i++ {
		rows = append(rows, map[string]Value{"d.key": intValue(typeInt64, i)})
	}
	rows = append(rows, map[string]Value{"d.key": intValue(typeInt64, 9007199254740993)}, map[string]Value{"d.key": nullValue(typeInt64)})
	demand, err := starFactDimensionDemand(context.Background(), fact, []starDimension{{factCols: []string{"f.key"}, dimCols: []string{"d.key"}, buckets: map[string][]map[string]Value{"prepared": rows}}}, maxUnionTerms)
	if err != nil {
		t.Fatal(err)
	}
	or, ok := demand.(*Binary)
	if !ok || or.Op != "OR" {
		t.Fatal("fixture must build one long scalar key set")
	}
	xs, values, recognized := literalInList(or)
	if !recognized || len(xs) != 1 || len(values) != inListMin+1 {
		t.Fatal("generated key terms do not share their column node")
	}
	e := &executor{cache: newExecCache()}
	env := e.newEnv(context.Background(), fact.types(), nil)
	before := subqueryStats.inLists.Load()
	var cached *inList
	for _, tt := range []struct {
		key        Value
		want, null bool
	}{
		{key: intValue(typeInt64, 5), want: true},
		{key: intValue(typeInt64, 9007199254740993), want: true},
		{key: intValue(typeInt64, 9007199254740992)},
		{key: intValue(typeInt64, 99)},
		{key: nullValue(typeInt64), null: true},
	} {
		env.row = map[string]Value{"f.key": tt.key}
		v, err := env.eval(demand)
		if err != nil || v.Null != tt.null || !v.Null && (v.I != 0) != tt.want {
			t.Fatalf("hash membership changed exact/NULL semantics: %v %v", v, err)
		}
		list := e.cache.inLists[or]
		if list == nil || list.set == nil {
			t.Fatal("env.eval fell back to the linear OR chain")
		}
		if cached != nil && list != cached {
			t.Fatal("env.eval rebuilt the key set per fact row")
		}
		cached = list
	}
	if subqueryStats.inLists.Load()-before != 5 {
		t.Fatal("actual membership evaluations did not use the hashed IN-list path")
	}
}

// Self-contained transport adapter on public-main fixtures, independent of the
// streaming PR's private prototype test adapter and its field names.
type factDemandNativeReplies struct {
	*snapshotPageReplies
	ranges int
}

func (h *factDemandNativeReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "zrange" {
			h.ranges++
			err := fmt.Errorf("unexpected full-directory fallback")
			cmd.SetErr(err)
			return err
		}
		return h.reply(cmd)
	}
}
func factDemandNativeFixture(t *testing.T) (*executor, *selectPlan, context.Context, *factDemandNativeReplies) {
	t.Helper()
	d, ctx, base := snapshotPageFixture(t)
	e, p := deferredTestPlan(t, `WITH a(v) AS (VALUES (1)) SELECT v FROM (SELECT v FROM a UNION ALL SELECT v FROM a) u`)
	n := int64(wideFetchChunk + 1)
	d.meta.SnapshotRows = &n
	d.meta.Columns = []columnMeta{{Name: "v", Type: typeInt32}}
	seg := d.meta.snapshotSegments()[0]
	base.segments[seg.Index] = seg
	base.rows = map[string]aggRow{}
	for i := int64(1); i <= n; i++ {
		id := strconv.FormatInt(i, 10)
		base.rows[seg.Prefix+id] = aggRow{rowIDField: id, "v": id}
	}
	hook := &factDemandNativeReplies{snapshotPageReplies: base}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	d.store.client = client
	e.store = d.store
	p.meta.view.plan.setop.left.meta = d.meta
	p.meta.view.plan.setop.right.meta = d.meta
	return e, p, ctx, hook
}

// With Flex, a streamed join's fact key demand may exceed maxUnionTerms (TPC-H
// Q8's 1,333 parts at SF1) and is still pushed down as an index union, up to
// flexDemandTerms. Without Flex, and for any other OR, maxUnionTerms remains.
func TestFlexFactKeyDemandUnionLimit(t *testing.T) {
	ctx := context.Background()
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Field: "a", Type: typeInt64, Indexed: true}}}
	demandOf := func(n int, limit int) Expr {
		t.Helper()
		// Odd keys: no two are consecutive, so each stays its own Flex term
		// (integerRunUnionQuery).
		rows := make([]map[string]Value, n)
		for i := range rows {
			rows[i] = map[string]Value{"d.a": intValue(typeInt64, int64(2*i+1))}
		}
		dims := []starDimension{{factCols: []string{"f.a"}, dimCols: []string{"d.a"}, buckets: map[string][]map[string]Value{"all": rows}}}
		demand, err := starFactDimensionDemand(ctx, fact, dims, limit)
		if err != nil {
			t.Fatal(err)
		}
		return demand
	}
	ram := &executor{store: &store{}}
	flex := &executor{store: &store{flex: true}}
	if ram.demandTermLimit() != maxUnionTerms || flex.demandTermLimit() != flexDemandTerms {
		t.Fatalf("demand limits: ram %d flex %d", ram.demandTermLimit(), flex.demandTermLimit())
	}
	if demandOf(maxUnionTerms+1, ram.demandTermLimit()) != nil {
		t.Fatal("RAM demand exceeded maxUnionTerms")
	}
	if demandOf(flexDemandTerms+1, flex.demandTermLimit()) != nil {
		t.Fatal("Flex demand exceeded flexDemandTerms")
	}
	wide := demandOf(1500, flex.demandTermLimit())
	root, ok := wide.(*Binary)
	if !ok || root.Op != "OR" || !root.keyDemand {
		t.Fatalf("Flex demand root %T is not a marked OR", wide)
	}
	env := flex.newEnv(ctx, fact.types(), nil)
	q, pushed, err := flex.unionTerm(ctx, wide, fact, env)
	if err != nil || !pushed || strings.Count(q, "@a:[") != 1500 {
		t.Fatalf("Flex demand union not pushed: %v %v (%d terms)", pushed, err, strings.Count(q, "@a:["))
	}
	if _, pushed, _ := ram.unionTerm(ctx, wide, fact, ram.newEnv(ctx, fact.types(), nil)); pushed {
		t.Fatal("RAM pushed a demand union over maxUnionTerms")
	}
	plain := *root
	plain.keyDemand = false
	if _, pushed, _ := flex.unionTerm(ctx, &plain, fact, env); pushed {
		t.Fatal("Flex pushed an unmarked OR over maxUnionTerms")
	}
}

// A correlated subquery's streamed join (outer set) runs once per distinct
// set of outer values, so with Flex its fact key demand keeps maxUnionTerms:
// a 1,500-key demand is neither built nor pushed (TPC-H Q2 at SF1).
func TestFlexCorrelatedFactKeyDemandKeepsUnionLimit(t *testing.T) {
	ctx := context.Background()
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Field: "a", Type: typeInt64, Indexed: true}}}
	rows := make([]map[string]Value, 1500)
	for i := range rows {
		rows[i] = map[string]Value{"d.a": intValue(typeInt64, int64(i+1))}
	}
	dims := []starDimension{{factCols: []string{"f.a"}, dimCols: []string{"d.a"}, buckets: map[string][]map[string]Value{"all": rows}}}
	flex := &executor{store: &store{flex: true}}
	correlated := &executor{store: &store{flex: true}, outer: &evalEnv{}}
	if correlated.demandTermLimit() != maxUnionTerms || flex.demandTermLimit() != flexDemandTerms {
		t.Fatalf("demand limits: correlated %d uncorrelated %d", correlated.demandTermLimit(), flex.demandTermLimit())
	}
	if demand, err := starFactDimensionDemand(ctx, fact, dims, correlated.demandTermLimit()); err != nil || demand != nil {
		t.Fatalf("correlated Flex demand over maxUnionTerms: %v %v", demand, err)
	}
	wide, err := starFactDimensionDemand(ctx, fact, dims, flex.demandTermLimit())
	if err != nil || wide == nil {
		t.Fatalf("uncorrelated Flex demand not built: %v", err)
	}
	if _, pushed, _ := correlated.unionTerm(ctx, wide, fact, correlated.newEnv(ctx, fact.types(), nil)); pushed {
		t.Fatal("correlated Flex executor pushed a demand union over maxUnionTerms")
	}
	if _, pushed, _ := flex.unionTerm(ctx, wide, fact, flex.newEnv(ctx, fact.types(), nil)); !pushed {
		t.Fatal("uncorrelated Flex executor refused its demand union")
	}
}

// With Flex, a streamed join's fact key demand on an integer column is pushed
// as one range per run of consecutive keys: SSB's yyyymmdd dates of a month
// are one run, so 1993 is 12 ranges instead of 365 terms (integerRunUnionQuery).
// The ranges hold exactly the keys: no gap is bridged, the int64 extremes stay
// exact, NULL and duplicate keys add nothing, and a missing value is still
// widened. Without Flex, and for an unmarked OR, each key keeps its own term.
func TestFlexFactKeyDemandCoalescesIntegerRuns(t *testing.T) {
	ctx := context.Background()
	for name, typ := range map[string]ColType{"int64": typeInt64, "int32": typeInt32} {
		fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Field: "a", Type: typ, Indexed: true}}}
		keys := []int64{7, -3, -1, -2, 0, 19930228, 19930101, 7}
		for d := int64(19930102); d <= 19930131; d++ {
			keys = append(keys, d)
		}
		for d := int64(19930201); d < 19930228; d++ {
			keys = append(keys, d)
		}
		want := "(@a:[-3 0] | @a:[7 7] | @a:[19930101 19930131] | @a:[19930201 19930228])"
		if typ == typeInt64 {
			keys = append(keys, math.MinInt64, math.MaxInt64, math.MaxInt64-1, math.MaxInt64-3)
			want = "(@a:[-9223372036854775808 -9223372036854775808] | @a:[-3 0] | @a:[7 7] | @a:[19930101 19930131] | @a:[19930201 19930228] | @a:[9223372036854775804 9223372036854775804] | @a:[9223372036854775806 9223372036854775807])"
		}
		rows := []map[string]Value{{"d.a": nullValue(typ)}}
		distinct := map[int64]bool{}
		for _, k := range keys {
			rows = append(rows, map[string]Value{"d.a": intValue(typ, k)})
			distinct[k] = true
		}
		// Two buckets, so a key may repeat across them.
		dims := []starDimension{{factCols: []string{"f.a"}, dimCols: []string{"d.a"}, buckets: map[string][]map[string]Value{"x": rows[:5], "y": rows[3:]}}}
		flex := &executor{store: &store{flex: true}}
		demand, err := starFactDimensionDemand(ctx, fact, dims, flex.demandTermLimit())
		if err != nil {
			t.Fatal(err)
		}
		env := flex.newEnv(ctx, fact.types(), nil)
		q, pushed, err := flex.unionTerm(ctx, demand, fact, env)
		if err != nil || !pushed || q != want {
			t.Fatalf("%s Flex demand: pushed %v err %v\n%s\nwant\n%s", name, pushed, err, q, want)
		}
		ram := &executor{store: &store{}}
		q, pushed, err = ram.unionTerm(ctx, demand, fact, ram.newEnv(ctx, fact.types(), nil))
		if err != nil || !pushed || strings.Count(q, "@a:[") != len(distinct) {
			t.Fatalf("%s RAM demand changed: %v %v %s", name, pushed, err, q)
		}
		plain := *demand.(*Binary)
		plain.keyDemand = false
		if q, _, _ := flex.unionTerm(ctx, &plain, fact, env); strings.Count(q, "@a:[") != len(distinct) {
			t.Fatalf("%s Flex coalesced an unmarked OR: %s", name, q)
		}
		// A missing value among the keys still widens the coalesced union.
		missing := *fact
		missing.Columns = []columnMeta{fact.Columns[0]}
		missing.Columns[0].MissingThrough, missing.Columns[0].Missing = 40, "19930115"
		q, pushed, err = flex.unionTerm(ctx, demand, &missing, env)
		if err != nil || !pushed || q != "("+want+" | @"+rowIDField+":[-inf 40])" {
			t.Fatalf("%s missing value not widened: %v %v %s", name, pushed, err, q)
		}
		missing.Columns[0].Missing = "8"
		if q, _, _ = flex.unionTerm(ctx, demand, &missing, env); q != want {
			t.Fatalf("%s widened for a missing value outside the keys: %s", name, q)
		}
	}
}

// A dimension proven unfiltered keeps maxUnionTerms even with Flex: its keys
// exclude only fact rows the inner join drops anyway (SSB Q2.x's 2,556 dates).
// A filtered dimension's demand, and an unfiltered one within maxUnionTerms,
// are still built.
func TestFlexUnfilteredDimensionDemandKeepsUnionLimit(t *testing.T) {
	ctx := context.Background()
	fact := &tableMeta{Columns: []columnMeta{{Name: "f.a", Field: "a", Type: typeInt64, Indexed: true}, {Name: "f.b", Field: "b", Type: typeInt64, Indexed: true}}}
	keyed := func(col string, n int) map[string][]map[string]Value {
		rows := make([]map[string]Value, n)
		for i := range rows {
			rows[i] = map[string]Value{col: intValue(typeInt64, int64(i+1))}
		}
		return map[string][]map[string]Value{"all": rows}
	}
	limit := (&executor{store: &store{flex: true}}).demandTermLimit()
	refsOf := func(dims []starDimension) map[string]bool {
		t.Helper()
		demand, err := starFactDimensionDemand(ctx, fact, dims, limit)
		if err != nil {
			t.Fatal(err)
		}
		refs := map[string]bool{}
		columnRefs(demand, refs)
		return refs
	}
	dims := []starDimension{
		{factCols: []string{"f.a"}, dimCols: []string{"d.a"}, buckets: keyed("d.a", 1500), unfiltered: true},
		{factCols: []string{"f.b"}, dimCols: []string{"e.b"}, buckets: keyed("e.b", 1500)},
	}
	if refs := refsOf(dims); refs["f.a"] || !refs["f.b"] {
		t.Fatalf("1,500 keys: unfiltered pushed or filtered dropped: %v", refs)
	}
	dims[0].buckets = keyed("d.a", maxUnionTerms)
	if refs := refsOf(dims); !refs["f.a"] || !refs["f.b"] {
		t.Fatalf("unfiltered demand within maxUnionTerms dropped: %v", refs)
	}
	// An empty unfiltered dimension still proves the join empty.
	dims[0].buckets = map[string][]map[string]Value{}
	demand, err := starFactDimensionDemand(ctx, fact, dims, limit)
	if lit, ok := demand.(*Literal); err != nil || !ok || lit.V.Null || lit.V.I != 0 {
		t.Fatalf("empty unfiltered dimension: %v %v", demand, err)
	}
}

// starDimensionUnfiltered holds only for stored tables without a filter of
// their own and without another condition inside the dimension. The planners
// leave it unproven for the snowflake fixture's in-memory relations.
func TestStarDimensionUnfilteredProof(t *testing.T) {
	stored := func() *tableMeta { return &tableMeta{Columns: []columnMeta{{Name: "x", Type: typeInt64}}} }
	jp := &joinPlan{items: []*joinItem{{base: stored(), prefix: "f."}, {base: stored(), prefix: "d."}, {base: stored(), prefix: "n."}, {base: &tableMeta{isMem: true}, prefix: "m."}}}
	ref := func(name string) *ColumnRef { return &ColumnRef{Name: name} }
	filter := &Binary{Op: "=", L: ref("n.x"), R: &Literal{V: intValue(typeInt64, 3)}}
	inside := &Binary{Op: "<", L: ref("d.x"), R: ref("n.x")}
	spanning := &Binary{Op: "<", L: ref("f.x"), R: ref("n.x")}
	for _, tt := range []struct {
		name    string
		items   []int
		filters map[int][]Expr
		conds   []Expr
		want    bool
	}{
		{"stored", []int{1}, nil, nil, true},
		{"branch", []int{1, 2}, nil, []Expr{spanning}, true},
		{"own filter", []int{1}, map[int][]Expr{1: {filter}}, nil, false},
		{"branch filter", []int{1, 2}, map[int][]Expr{2: {filter}}, nil, false},
		{"condition inside the branch", []int{1, 2}, nil, []Expr{inside}, false},
		{"in-memory relation", []int{3}, nil, nil, false},
	} {
		if got := starDimensionUnfiltered(jp, tt.filters, tt.items, tt.conds); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	e, p, sjp := snowflakeTestShape(t, snowflakeQueries["q5"])
	s, ok := e.planSnowflakeShape(context.Background(), sjp, map[string]bool{}, p.sel.Where)
	if !ok {
		t.Fatal("snowflake rejected")
	}
	for _, d := range s.dims {
		if d.unfiltered {
			t.Fatalf("in-memory dimension %s proven unfiltered", sjp.items[d.item].prefix)
		}
	}
}
