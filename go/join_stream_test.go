// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"strconv"
	"testing"
)

func starTestFixture(n int) (*executor, scanRequest) {
	cols := []columnMeta{{Name: "id", Type: typeInt64}, {Name: "v", Type: typeInt64}}
	fact := &tableMeta{isMem: true, Columns: cols}
	for i := 0; i < n; i++ {
		fact.mem = append(fact.mem, map[string]Value{"id": intValue(typeInt64, int64(i%3+1)), "v": intValue(typeInt64, int64(i)), "__key": stringValue(fmt.Sprint("source:", i))})
	}
	dim := &tableMeta{isMem: true, Columns: cols, mem: []map[string]Value{
		{"id": intValue(typeInt64, 1), "v": intValue(typeInt64, 10)},
		{"id": intValue(typeInt64, 1), "v": intValue(typeInt64, 11)},
		{"id": intValue(typeInt64, 2), "v": intValue(typeInt64, 20)},
		{"id": nullValue(typeInt64), "v": intValue(typeInt64, 99)},
	}}
	eq := &Binary{Op: "=", L: &ColumnRef{Name: "f.id"}, R: &ColumnRef{Name: "d.id"}}
	jp := &joinPlan{items: []*joinItem{{base: dim, prefix: "d."}, {base: fact, prefix: "f.", kind: "CROSS", extra: []Expr{eq}}}}
	m := &tableMeta{isMem: true, join: jp, Columns: []columnMeta{{Name: "f.id", Type: typeInt64}, {Name: "f.v", Type: typeInt64}, {Name: "d.v", Type: typeInt64}}}
	return &executor{store: &store{}}, scanRequest{meta: m, where: wherePlan{query: "*", residual: eq}, need: map[string]bool{"f.id": true, "f.v": true, "d.v": true, "f.__key": true}, streamJoin: true}
}

func TestStarJoinStreamingBoundedMultiplicityAndProvenance(t *testing.T) {
	e, req := starTestFixture(4097)
	var count, maxBatch int
	var gotLast bool
	handled, err := e.scanStarJoinPages(context.Background(), req, nil, func(rows []map[string]Value) error {
		if len(rows) > maxBatch {
			maxBatch = len(rows)
		}
		if count == 0 {
			req.meta.join.items[1].base.mem[4095]["__key"] = stringValue("future-page")
		}
		for _, r := range rows {
			count++
			if r["f.v"].I == 4095 {
				gotLast = r["f.__key"].S == "future-page"
			}
			if r["f.id"].I == 3 || r["d.v"].I == 99 {
				t.Fatal("NULL or missing match admitted")
			}
		}
		return nil
	})
	if !handled || err != nil || count != 4098 || maxBatch != starOutputPage || !gotLast {
		t.Fatalf("handled=%v error=%v count=%d max=%d future=%v", handled, err, count, maxBatch, gotLast)
	}
	if _, ok := req.meta.join.items[1].base.mem[0]["f.id"]; ok {
		t.Fatal("source map mutated")
	}
}

func TestStarJoinStreamingMatchesEagerBag(t *testing.T) {
	e, req := starTestFixture(23)
	counts := map[string]int{}
	_, err := e.scanStarJoinPages(context.Background(), req, nil, func(rows []map[string]Value) error {
		for _, r := range rows {
			counts[fmt.Sprint(r["f.v"].I, ":", r["d.v"].I)]++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.runJoin(context.Background(), req.meta.join, req.need, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		counts[fmt.Sprint(r["f.v"].I, ":", r["d.v"].I)]--
	}
	for k, n := range counts {
		if n != 0 {
			t.Fatalf("multiplicity %s: %d", k, n)
		}
	}
}

func TestStarJoinStreamingMultipleDimensions(t *testing.T) {
	e, req := starTestFixture(23)
	d := *req.meta.join.items[0]
	d.prefix = "x."
	d.kind = "CROSS"
	d.extra = []Expr{&Binary{Op: "=", L: &ColumnRef{Name: "f.id"}, R: &ColumnRef{Name: "x.id"}}}
	req.meta.join.items = append(req.meta.join.items, &d)
	req.need["x.v"] = true
	count := 0
	handled, err := e.scanStarJoinPages(context.Background(), req, nil, func(rows []map[string]Value) error {
		for _, r := range rows {
			count++
			if _, ok := r["x.v"]; !ok {
				t.Fatal("second dimension missing")
			}
		}
		return nil
	})
	if !handled || err != nil || count != 40 {
		t.Fatalf("%v %v %d", handled, err, count)
	}
}

func TestStarJoinStreamingErrorsAndCancellation(t *testing.T) {
	sentinel := errors.New("consumer failed")
	e, req := starTestFixture(1025)
	calls := 0
	handled, err := e.scanStarJoinPages(context.Background(), req, nil, func([]map[string]Value) error { calls++; return sentinel })
	if !handled || !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("%v %v %d", handled, err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = e.scanStarJoinPages(ctx, req, nil, func([]map[string]Value) error { cancel(); return sentinel })
	if err == nil || errors.Is(err, sentinel) {
		t.Fatalf("remaining scan cancellation must override earlier reduction error: %v", err)
	}
}

func TestStarJoinStreamingEligibilityFallback(t *testing.T) {
	for _, kind := range []string{"outer", "lateral", "tag", "filter", "cast", "nonstar", "oversize", "derived", "mismatch", "inequality"} {
		t.Run(kind, func(t *testing.T) {
			e, req := starTestFixture(23)
			jp := req.meta.join
			switch kind {
			case "outer":
				jp.items[1].kind = "LEFT"
			case "lateral":
				jp.items[1].lateral = &lateralItem{}
			case "tag":
				jp.items[0].tag = "presence"
				req.need["presence"] = true
			case "filter":
				jp.items[0].pushed = []Expr{&Literal{V: boolValue(true)}}
			case "cast":
				jp.items[1].extra[0].(*Binary).L = &Cast{X: &ColumnRef{Name: "f.id"}, T: typeInt64}
			case "nonstar":
				jp.items = append(jp.items, &joinItem{base: jp.items[0].base, prefix: "x.", kind: "CROSS", extra: []Expr{&Binary{Op: "=", L: &ColumnRef{Name: "d.id"}, R: &ColumnRef{Name: "x.id"}}}})
			case "oversize":
				jp.items[0].base.mem = make([]map[string]Value, starDimensionRows+1)
				jp.items[1].base.mem = make([]map[string]Value, starDimensionRows+2)
			case "derived":
				jp.items[0].base.join = &joinPlan{}
			case "mismatch":
				jp.items[0].base.Columns = append([]columnMeta(nil), jp.items[0].base.Columns...)
				jp.items[0].base.Columns[0].Type = typeInt32
			case "inequality":
				jp.items[1].extra[0].(*Binary).Op = ">"
			}
			if _, ok := e.planStarShape(context.Background(), jp, req.need, req.where.residual); ok {
				t.Fatal("unsafe shape admitted")
			}
		})
	}
}

func TestStarJoinAggregateExactResults(t *testing.T) {
	const prefix = "WITH d(id,g) AS (VALUES (1,'a'),(1,'a'),(2,'b'),(NULL,'n')), f(id,v) AS (VALUES (1,10.25),(1,20.75),(2,8.00),(3,99.99),(NULL,100.00)) "
	q := prefix + "SELECT d.g, SUM((f.v-1.00)/2), COUNT(*), AVG(f.v) FROM d,f WHERE d.id=f.id GROUP BY d.g ORDER BY d.g"
	if got := memQuery(q); got != "a|29.0000000000000000|4|15.5000000000000000\nb|3.5000000000000000|1|8.0000000000000000" {
		t.Fatalf("exact decimal result: %q", got)
	}
}

func TestStarAggregateInputSafety(t *testing.T) {
	types := map[string]ColType{"d": decimalType(10, 2), "i": typeInt64, "f": typeFloat64}
	col := func(n string) Expr { return &ColumnRef{Name: n} }
	lit := func(n int64) Expr { return &Literal{V: intValue(typeInt64, n)} }
	for _, tc := range []struct {
		x    Expr
		want bool
	}{
		{col("d"), true}, {col("f"), false},
		{&Binary{Op: "+", L: col("i"), R: lit(1)}, false},
		{&Binary{Op: "/", L: col("d"), R: lit(2)}, true},
		{&Binary{Op: "/", L: col("d"), R: lit(0)}, false},
		{&Binary{Op: "/", L: col("d"), R: col("i")}, false},
		{&Func{Name: "RANDOM"}, false},
	} {
		if got := starAggregateInput(tc.x, types); got != tc.want {
			t.Fatalf("%#v: %v", tc.x, got)
		}
	}
}

func TestStarJoinAggregateEligibilityAndWiring(t *testing.T) {
	e, req := starTestFixture(4097)
	f := &Func{Name: "SUM", Args: []Expr{&ColumnRef{Name: "f.v"}}}
	p := &selectPlan{meta: req.meta, sel: &SelectStmt{GroupBy: []Expr{&ColumnRef{Name: "d.v"}}}}
	if !streamingStarAggregate(p, []*Func{f}) {
		t.Fatal("simple exact reduction rejected")
	}
	groups, err := e.serialDriverAggregate(context.Background(), p, req.where, []*Func{f}, nil)
	if err != nil || len(groups) != 3 {
		t.Fatalf("groups=%d error=%v", len(groups), err)
	}
	f.Distinct = true
	if streamingStarAggregate(p, []*Func{f}) {
		t.Fatal("DISTINCT admitted")
	}
	f.Distinct = false
	p.sel.GroupBy = []Expr{&Func{Name: "RANDOM"}}
	if streamingStarAggregate(p, []*Func{f}) {
		t.Fatal("volatile grouping admitted")
	}
}

func TestStarJoinTupleKeysAreLengthFramed(t *testing.T) {
	zero := string([]byte{'a', 0, 's', 'b'})
	a := map[string]Value{"x": stringValue(zero), "y": stringValue("c")}
	b := map[string]Value{"x": stringValue("a"), "y": stringValue(string([]byte{'b', 0, 's', 'c'}))}
	ka, _ := starRowKey(a, []string{"x", "y"})
	kb, _ := starRowKey(b, []string{"x", "y"})
	if ka == kb {
		t.Fatal("NUL-bearing tuple keys collided")
	}
}

type starTransportReplies struct {
	*streamNativeReplies
	n       int
	ranges  int
	failure error
}

func (h *starTransportReplies) replyStar(cmd goredis.Cmder) error {
	if cmd.Name() != "zrange" {
		return h.reply(cmd)
	}
	h.ranges++
	if h.ranges > 1 && h.failure != nil {
		return h.failure
	}
	lo, _ := strconv.Atoi(fmt.Sprint(cmd.Args()[2]))
	hi, _ := strconv.Atoi(fmt.Sprint(cmd.Args()[3]))
	var ids []string
	for i := lo; i <= hi && i < h.n; i++ {
		ids = append(ids, fmt.Sprintf("%020d", i+1))
	}
	cmd.(*goredis.StringSliceCmd).SetVal(ids)
	return nil
}
func (h *starTransportReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		err := h.replyStar(cmd)
		if err != nil {
			cmd.SetErr(err)
		}
		return err
	}
}
func (h *starTransportReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		var errs []error
		for _, cmd := range cmds {
			if err := h.replyStar(cmd); err != nil {
				cmd.SetErr(err)
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

func TestStarJoinStreamingUsesNativeDirectoryPagesAndFailsClosed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, ctx, base := streamNativeFixture(t)
			native := d.meta
			native.Columns = []columnMeta{{Name: "id", Field: "value", Type: typeInt64}}
			n := int64(wideFetchChunk + 1)
			native.SnapshotRows = &n
			seg := native.snapshotSegments()[0]
			base.segments[seg.Index] = seg
			base.rows = map[string]aggRow{}
			for i := 1; i <= int(n); i++ {
				base.rows[seg.Prefix+fmt.Sprint(i)] = aggRow{rowIDField: fmt.Sprint(i), "value": "1"}
			}
			client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
			t.Cleanup(func() { _ = client.Close() })
			hook := &starTransportReplies{streamNativeReplies: base, n: int(n)}
			if fail {
				hook.failure = errors.New("synthetic reply loss on later directory page")
			}
			client.AddHook(hook)
			d.store.client = client
			dim := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "id", Type: typeInt64}}, mem: []map[string]Value{{"id": intValue(typeInt64, 1)}}}
			eq := &Binary{Op: "=", L: &ColumnRef{Name: "f.id"}, R: &ColumnRef{Name: "d.id"}}
			jp := &joinPlan{items: []*joinItem{{base: dim, prefix: "d."}, {base: native, prefix: "f.", kind: "CROSS", extra: []Expr{eq}}}}
			req := scanRequest{meta: &tableMeta{isMem: true, join: jp}, where: wherePlan{query: "*", residual: eq}, need: map[string]bool{"f.id": true}, streamJoin: true}
			count, calls := 0, 0
			consumerErr := errors.New("earlier reducer error")
			handled, err := (&executor{store: d.store}).scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error {
				calls++
				count += len(rows)
				if fail {
					return consumerErr
				}
				return nil
			})
			if !handled {
				t.Fatal("native shape not admitted")
			}
			if fail {
				if err == nil || errors.Is(err, consumerErr) || calls != 1 || hook.ranges < 2 {
					t.Fatalf("failure order: error=%v calls=%d ranges=%d", err, calls, hook.ranges)
				}
			} else if err != nil || count != int(n) || calls < 2 || hook.ranges < 2 {
				t.Fatalf("pages: error=%v count=%d calls=%d ranges=%d", err, count, calls, hook.ranges)
			}
		})
	}
}

func TestStarJoinStreamingPureDimensionFilters(t *testing.T) {
	e, req := starTestFixture(23)
	pred := &Binary{Op: "=", L: &ColumnRef{Name: "d.v"}, R: &Literal{V: intValue(typeInt64, 10)}}
	req.meta.join.items[0].pushed = []Expr{pred}
	req.where.residual = &Binary{Op: "AND", L: req.where.residual, R: pred}
	n := 0
	handled, err := e.scanStarJoinPages(context.Background(), req, nil, func(rows []map[string]Value) error {
		for _, r := range rows {
			n++
			if r["d.v"].I != 10 {
				t.Fatal("full local predicate lost")
			}
		}
		return nil
	})
	if !handled || err != nil || n != 8 {
		t.Fatalf("%v %v %d", handled, err, n)
	}
}

func TestStarJoinStreamingOversizeFilteredDimension(t *testing.T) {
	for _, exceeds := range []bool{false, true} {
		t.Run(fmt.Sprint(exceeds), func(t *testing.T) {
			e, req := starTestFixture(5)
			dim := req.meta.join.items[0].base
			row := map[string]Value{"id": intValue(typeInt64, 1), "v": intValue(typeInt64, 10)}
			dim.mem = make([]map[string]Value, starDimensionRows+1)
			for i := range dim.mem {
				dim.mem[i] = row
			}
			fact := req.meta.join.items[1].base
			fact.mem = make([]map[string]Value, starDimensionRows+2)
			value := int64(11)
			if exceeds {
				value = 10
			}
			pred := &Binary{Op: "=", L: &ColumnRef{Name: "d.v"}, R: &Literal{V: intValue(typeInt64, value)}}
			req.meta.join.items[0].pushed = []Expr{pred}
			req.where.residual = &Binary{Op: "AND", L: req.where.residual, R: pred}
			calls := 0
			handled, err := e.scanStarJoinPages(context.Background(), req, nil, func([]map[string]Value) error { calls++; return nil })
			if err != nil || calls != 0 || handled == exceeds {
				t.Fatalf("cap handling: %v %v %d", handled, err, calls)
			}
		})
	}
}

func TestStarAggregateCaseLazinessAndExactness(t *testing.T) {
	const prefix = "WITH d(id,day) AS (VALUES (1,'Sunday'),(1,'Monday'),(2,NULL)), f(id,v) AS (VALUES (1,1.25),(1,2.75),(2,100.00),(NULL,9.00)) "
	q := prefix + "SELECT f.id, SUM(CASE WHEN d.day='Sunday' THEN f.v ELSE NULL END), SUM(CASE WHEN d.day='Monday' THEN f.v ELSE NULL END) FROM d,f WHERE d.id=f.id GROUP BY f.id ORDER BY f.id"
	if got := memQuery(q); got != "1|4.00|4.00\n2|NULL|NULL" {
		t.Fatalf("CASE semantics: %q", got)
	}
	types := map[string]ColType{"d.day": typeString, "f.v": decimalType(10, 2)}
	c := &Case{Whens: []WhenClause{{When: &Binary{Op: "=", L: &ColumnRef{Name: "d.day"}, R: &Literal{V: stringValue("Sunday")}}, Then: &ColumnRef{Name: "f.v"}}}, Else: &Literal{V: nullValue(ColType{Kind: KindNull})}}
	if !starAggregateInput(c, types) {
		t.Fatal("safe conditional decimal input rejected")
	}
	c.Else = &Binary{Op: "/", L: &ColumnRef{Name: "f.v"}, R: &Literal{V: intValue(typeInt64, 0)}}
	if starAggregateInput(c, types) {
		t.Fatal("error-capable branch admitted")
	}
}

func TestStarJoinPrivateSourceRefusesBeforeInput(t *testing.T) {
	for _, item := range []int{0, 1} {
		t.Run(fmt.Sprint(item), func(t *testing.T) {
			e, req := starTestFixture(23)
			d, ctx, h := streamNativeFixture(t)
			n := int64(30)
			d.meta.SnapshotRows = &n
			d.meta.Columns = []columnMeta{{Name: "id", Type: typeInt64}, {Name: "v", Type: typeInt64}}
			req.meta.join.items[item].base = d.meta
			e.store = d.store
			scope := snapshotOf(ctx)
			scope.write = true
			scope.owned = map[string]bool{snapshotDataID(d.meta.prefix()): true}
			handled, err := e.scanStarJoinPages(ctx, req, nil, func([]map[string]Value) error { t.Fatal("private source consumed"); return nil })
			if handled || err != nil || h.commands != 0 {
				t.Fatalf("private source did not retain original path: %v %v commands%d", handled, err, h.commands)
			}
		})
	}
}
