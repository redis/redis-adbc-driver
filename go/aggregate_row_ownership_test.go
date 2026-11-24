// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"testing"
)

func TestOwnedScanRowsMarkerIsCallbackScoped(t *testing.T) {
	for _, kind := range []string{"ok", "error", "cancel", "panic"} {
		t.Run(kind, func(t *testing.T) {
			e, req := starTestFixture(23)
			owned := false
			req.ownedRows = &owned
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("consumer failure")
			calls := 0
			func() {
				defer func() {
					if p := recover(); p != nil && kind != "panic" {
						panic(p)
					}
				}()
				err := e.scanPages(ctx, req, nil, func(rows []map[string]Value) error {
					calls++
					if !owned {
						t.Error("proven leaf callback was not marked owned")
					}
					if len(rows) == 0 {
						t.Error("fixture emitted empty callback")
					}
					switch kind {
					case "error":
						return sentinel
					case "cancel":
						cancel()
						return ctx.Err()
					case "panic":
						panic("consumer panic")
					}
					return nil
				})
				if kind == "error" && !errors.Is(err, sentinel) {
					t.Errorf("callback error lost: %v", err)
				}
				if kind == "cancel" && err == nil {
					t.Error("cancellation lost")
				}
				if kind == "ok" && err != nil {
					t.Error(err)
				}
			}()
			if owned || calls != 1 {
				t.Fatalf("ownership marker escaped callback: owned%v calls%d", owned, calls)
			}
		})
	}
}

func TestOwnedScanRowsNestedAndFallbackStayShared(t *testing.T) {
	e, req := starTestFixture(23)
	owned := false
	req.ownedRows = &owned
	err := e.scanPages(context.Background(), req, nil, func([]map[string]Value) error {
		if !owned {
			t.Error("outer leaf not owned")
		}
		innerOwned := false
		source := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "v", Type: typeInt64}}, mem: []map[string]Value{{"v": intValue(typeInt64, 7)}}}
		sub := scanRequest{meta: source, where: wherePlan{query: "*"}, ownedRows: &innerOwned}
		if err := e.scanPages(context.Background(), sub, nil, func([]map[string]Value) error {
			if innerOwned || !owned {
				t.Error("nested source inherited marker or changed outer scope")
			}
			return nil
		}); err != nil {
			return err
		}
		if innerOwned {
			t.Error("nested marker leaked")
		}
		return nil
	})
	if err != nil || owned {
		t.Fatalf("nested ownership: %v %v", owned, err)
	}
	for _, kind := range []string{"refused", "retained", "ordinary_memory"} {
		t.Run(kind, func(t *testing.T) {
			e, req := starTestFixture(23)
			flag := false
			req.ownedRows = &flag
			ctx := context.Background()
			switch kind {
			case "refused":
				req.meta.join.items[1].kind = "LEFT"
			case "retained":
				ctx = context.WithValue(ctx, retainedAllocationKey{}, newRetainedBudget(16<<20))
			case "ordinary_memory":
				req.meta = &tableMeta{isMem: true, Columns: []columnMeta{{Name: "v", Type: typeInt64}}, mem: []map[string]Value{{"v": intValue(typeInt64, 1)}}}
				req.where = wherePlan{query: "*"}
			}
			calls := 0
			err := e.scanPages(ctx, req, nil, func([]map[string]Value) error {
				calls++
				if flag {
					t.Error("eligibility/fallback inferred owned rows")
				}
				return nil
			})
			if err != nil || calls == 0 || flag {
				t.Fatalf("shared fallback: calls%d owned%v err%v", calls, flag, err)
			}
		})
	}
}

func TestOwnedScanRowsPrivateNativeSourceKeepsFallbackMarker(t *testing.T) {
	e, ctx, req, h := parallelNativeStarFixture(t)
	scope := snapshotOf(ctx)
	scope.write = true
	scope.owned = map[string]bool{snapshotDataID(req.meta.join.items[1].base.prefix()): true}
	owned := false
	req.ownedRows = &owned
	calls := 0
	err := e.scanPages(ctx, req, nil, func([]map[string]Value) error {
		calls++
		if owned {
			t.Error("private native fallback marked owned")
		}
		return nil
	})
	if err != nil || calls == 0 || owned || e.workerGrant.Load() != 0 || h.rangeReads == 0 {
		t.Fatalf("private fallback: err%v calls%d marker%v grant%d", err, calls, owned, e.workerGrant.Load())
	}
}

func TestAggregateRepresentativeSharedCloneAndOwnedTransfer(t *testing.T) {
	keep := map[string]bool{"g": true}
	shared := map[string]Value{"g": stringValue("group"), "v": intValue(typeInt64, 10)}
	rep := aggregateRepresentativeRow(shared, keep, true)
	if _, ok := shared["v"]; !ok {
		t.Fatal("shared source pruned")
	}
	rep["g"] = stringValue("consumer")
	if shared["g"].S != "group" {
		t.Fatal("representative aliases shared source")
	}
	owned := maps.Clone(shared)
	rep = aggregateRepresentativeRow(owned, keep, false)
	if _, ok := owned["v"]; ok {
		t.Fatal("owned map did not become pruned representative")
	}
	rep["g"] = stringValue("transferred")
	if owned["g"].S != "transferred" || shared["g"].S != "group" {
		t.Fatal("owned map transfer or shared isolation failed")
	}
}

func TestSerialAggregateSharedMemoryRowsRemainIntact(t *testing.T) {
	source := []map[string]Value{{"g": stringValue("a"), "v": intValue(typeInt64, 10)}, {"g": stringValue("a"), "v": intValue(typeInt64, 20)}}
	before := []map[string]Value{maps.Clone(source[0]), maps.Clone(source[1])}
	meta := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "g", Type: typeString}, {Name: "v", Type: typeInt64}}, mem: source}
	f := &Func{Name: "SUM", Args: []Expr{&ColumnRef{Name: "v"}}}
	p := &selectPlan{meta: meta, sel: &SelectStmt{GroupBy: []Expr{&ColumnRef{Name: "g"}}}}
	groups, err := (&executor{store: &store{}}).serialDriverAggregate(context.Background(), p, wherePlan{query: "*"}, []*Func{f}, nil)
	if err != nil || len(groups) != 1 || !reflect.DeepEqual(source, before) {
		t.Fatalf("shared aggregate source mutated: groups%d err%v", len(groups), err)
	}
	if _, ok := groups[0].rep["v"]; ok {
		t.Fatal("representative retained unneeded aggregate input")
	}
}

func groupRepresentativeFixture() (*starRowProbe, map[string]Value, map[string]bool) {
	shape, fact := leafProbeFixture(1, 1, false)
	projectProbeBenchmarkRows(shape, fact)
	keep := map[string]bool{"c.customer_id": true, "c.first_name": true, "c.last_name": true, "c.preferred": true, "c.country": true, "c.login": true, "c.email": true, "d.year": true}
	return newStarRowProbe(shape), fact, keep
}
func TestOwnedGroupRepresentativeAllocationControl(t *testing.T) {
	p, fact, keep := groupRepresentativeFixture()
	var sink map[string]Value
	measure := func(shared bool) float64 {
		return testing.AllocsPerRun(100, func() {
			if err := p.row(context.Background(), fact, func(row map[string]Value) error { sink = aggregateRepresentativeRow(row, keep, shared); return nil }); err != nil {
				panic(err)
			}
		})
	}
	clone, owned := measure(true), measure(false)
	if owned >= clone || len(sink) != 8 || len(fact) != 6 {
		t.Fatalf("representative allocation/ownership: clone%.0f owned%.0f sink%d fact%d", clone, owned, len(sink), len(fact))
	}
	t.Logf("first group row, including identical fresh leaf production: shared clone %.0f; owned transfer %.0f allocs", clone, owned)
}
func BenchmarkGroupRepresentativeOwnership(b *testing.B) {
	for _, shared := range []bool{true, false} {
		name := "clone"
		if !shared {
			name = "owned"
		}
		b.Run(name, func(b *testing.B) {
			p, fact, keep := groupRepresentativeFixture()
			var sink map[string]Value
			emit := func(row map[string]Value) error { sink = aggregateRepresentativeRow(row, keep, shared); return nil }
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := p.row(context.Background(), fact, emit); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if len(sink) != 8 || len(fact) != 6 {
				b.Fatal("benchmark mutated source or omitted retained representative")
			}
		})
	}
}

func TestOwnedParallelLeavesMayBePrunedByCoordinator(t *testing.T) {
	e, ctx, req, h := parallelNativeStarFixture(t)
	owned := false
	req.ownedRows = &owned
	keep := map[string]bool{"f.id": true, "d.v": true}
	count := 0
	err := e.scanPages(ctx, req, nil, func(rows []map[string]Value) error {
		if !owned {
			t.Error("parallel leaf callback not owned")
		}
		for _, row := range rows {
			count++
			aggregateRepresentativeRow(row, keep, false)
			if len(row) != 2 || row["f.id"].I != 1 {
				t.Error("owned leaf prune failed")
			}
		}
		return nil
	})
	if err != nil || count != 8194 || owned {
		t.Fatalf("coordinator ownership: count%d marker%v err%v", count, owned, err)
	}
	for _, row := range h.rows {
		if _, ok := row["v"]; !ok {
			t.Fatal("native source was pruned")
		}
	}
	for _, row := range req.meta.join.items[0].base.mem {
		if _, ok := row["v"]; !ok {
			t.Fatal("dimension/CTE row was pruned")
		}
	}
}

func TestOwnedParallelPanicMarkerRestoresAfterWorkerJoin(t *testing.T) {
	owned := false
	exerciseParallelStarPanicLifecycle(t, func(req *scanRequest) { req.ownedRows = &owned }, func() {
		if !owned {
			t.Error("parallel panic callback was not marked owned")
		}
	}, func() {
		if owned {
			t.Error("parallel panic leaked callback ownership")
		}
	})
}
