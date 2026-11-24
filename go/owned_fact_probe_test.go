// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"testing"
)

func decodedFactProbeFixture() (*tableMeta, aggRow, map[string]bool, *starShape) {
	shape, _ := leafProbeFixture(1, 1, false)
	projectProbeBenchmarkRows(shape, map[string]Value{})
	// Native decode also emits an unqualified physical ID. Preserve the same
	// last-dimension overwrite as the prior fresh-leaf map, after source checks.
	for i, dim := range shape.dims {
		for _, bucket := range dim.buckets {
			for _, row := range bucket {
				row[rowIDField] = intValue(typeInt64, int64(i+2))
			}
		}
	}
	meta := &tableMeta{Columns: []columnMeta{{Name: "f.customer", Field: "customer", Type: typeInt64}, {Name: "f.date", Field: "date", Type: typeInt64}}}
	for _, name := range []string{"list", "cost", "discount", "sales"} {
		meta.Columns = append(meta.Columns, columnMeta{Name: "f." + name, Field: name, Type: decimalType(7, 2)})
	}
	raw := aggRow{rowIDField: "1", "customer": "1", "date": "2", "list": "21.00", "cost": "10.00", "discount": "1.00", "sales": "20.00"}
	need := map[string]bool{}
	for _, c := range meta.Columns {
		need[c.Name] = true
	}
	complete := maps.Clone(need)
	for _, dim := range shape.dims {
		for _, bucket := range dim.buckets {
			for _, row := range bucket {
				for name := range row {
					if name != rowIDField {
						complete[name] = true
					}
				}
			}
		}
	}
	shape.need = complete
	return meta, raw, need, shape
}

func TestOwnedFactSingletonTransfersMapAfterAllKeys(t *testing.T) {
	meta, raw, need, shape := decodedFactProbeFixture()
	rawBefore := maps.Clone(raw)
	fact, err := decodeRow(meta, raw, need)
	if err != nil {
		t.Fatal(err)
	}
	controlFact := maps.Clone(fact)
	var want map[string]Value
	if err := newStarRowProbe(shape).row(context.Background(), controlFact, func(row map[string]Value) error { want = row; return nil }); err != nil {
		t.Fatal(err)
	}
	var got map[string]Value
	if err := newStarRowProbe(shape).ownedRow(context.Background(), fact, func(row map[string]Value) error { got = row; return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || got[rowIDField].I != 3 {
		t.Fatalf("singleton output differs: got%v want%v", got, want)
	}
	got["consumer_owned"] = stringValue("same map")
	if fact["consumer_owned"].S != "same map" || !reflect.DeepEqual(raw, rawBefore) {
		t.Fatal("fact not transferred or raw source changed")
	}
	for _, dim := range shape.dims {
		for _, bucket := range dim.buckets {
			for _, row := range bucket {
				if _, ok := row["consumer_owned"]; ok {
					t.Fatal("dimension map aliases output")
				}
			}
		}
	}
}

func TestOwnedFactFanoutKeepsFreshLeavesAndFact(t *testing.T) {
	shape, fact := leafProbeFixture(2, 3, false)
	before := maps.Clone(fact)
	var got, want []map[string]Value
	if err := newStarRowProbe(shape).row(context.Background(), fact, func(row map[string]Value) error { want = append(want, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := newStarRowProbe(shape).ownedRow(context.Background(), fact, func(row map[string]Value) error { got = append(got, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 6 || !reflect.DeepEqual(fact, before) {
		t.Fatal("fanout changed bag/order or mutated fact")
	}
	got[0]["shared"] = stringValue("owned leaf")
	if got[1]["shared"].S != "date" || fact["shared"].S != "fact" {
		t.Fatal("fanout output aliases siblings/fact")
	}
}

func TestOwnedFactLateMissLeavesFactUnchanged(t *testing.T) {
	shape, fact := leafProbeFixture(1, 1, true)
	before := maps.Clone(fact)
	if err := newStarRowProbe(shape).ownedRow(context.Background(), fact, func(map[string]Value) error { t.Fatal("late nonmatch emitted"); return nil }); err != nil || !reflect.DeepEqual(fact, before) {
		t.Fatalf("late miss mutated source: %v", err)
	}
}

func TestDecodedRowCapacityHintBoundsAndCompatibility(t *testing.T) {
	meta, raw, need, shape := decodedFactProbeFixture()
	baseline, err := decodeRow(meta, raw, need)
	if err != nil {
		t.Fatal(err)
	}
	if ownedFactRowCapacity(len(shape.need)) != 17 {
		t.Fatal("fixture is not projected native Q4 width")
	}
	for _, hint := range []int{0, -1, 17, 64, 65, math.MaxInt} {
		got, err := decodeRowCapacity(meta, raw, need, hint)
		if err != nil || !reflect.DeepEqual(got, baseline) {
			t.Fatalf("hint%d changed decode: %v", hint, err)
		}
		cap := decodedRowCapacity(need, hint)
		if (hint <= len(need)+1 || hint > 64) && cap != len(need)+1 {
			t.Fatalf("invalid/unbounded hint%d admitted: %d", hint, cap)
		}
	}
}

func TestOwnedFactCancelledBeforeLookupPreservesSource(t *testing.T) {
	shape, fact := leafProbeFixture(1, 1, false)
	before := maps.Clone(fact)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := newStarRowProbe(shape).ownedRow(ctx, fact, func(map[string]Value) error { t.Fatal("cancelled row emitted"); return nil })
	if err == nil || !reflect.DeepEqual(fact, before) {
		t.Fatalf("cancelled source changed: %v", err)
	}
}

func TestParallelDefaultSingletonSourceRemainsSharedSafe(t *testing.T) {
	shape, fact := leafProbeFixture(1, 1, false)
	before := maps.Clone(fact)
	source, consumer := parallelStarJoinPages(context.Background(), shape, 1, func(ctx context.Context, w int, visit func([]map[string]Value) error) error {
		return visit([]map[string]Value{fact})
	}, func(rows []map[string]Value) error { clear(rows[0]); return nil })
	if source != nil || consumer != nil || !reflect.DeepEqual(fact, before) {
		t.Fatalf("shared source changed: %v %v", source, consumer)
	}
}

func TestOwnedNativeFactsRemainFreshAcrossPagesAndConsumerPruning(t *testing.T) {
	for _, cancelConsumer := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelConsumer), func(t *testing.T) {
			e, ctx, req, h := parallelNativeStarFixture(t)
			jp := req.meta.join
			jp.items[0].base.mem = jp.items[0].base.mem[:1]
			second := *jp.items[0]
			second.prefix = "z."
			second.kind = "CROSS"
			eq := &Binary{Op: "=", L: &ColumnRef{Name: "f.id"}, R: &ColumnRef{Name: "z.id"}}
			second.extra = []Expr{eq}
			jp.items = append(jp.items, &second)
			req.need["z.v"] = true
			req.where.residual = &Binary{Op: "AND", L: req.where.residual, R: eq}
			pred := &Binary{Op: ">=", L: &ColumnRef{Name: "f.v"}, R: &Literal{V: intValue(typeInt64, 1)}}
			jp.items[1].pushed = []Expr{pred}
			req.where.residual = &Binary{Op: "AND", L: req.where.residual, R: pred}
			n := int64(9001)
			jp.items[1].base.SnapshotRows = &n
			h.n = n
			seg := jp.items[1].base.snapshotSegments()[0]
			seg.Rows = n
			h.segments[seg.Index] = seg
			for i := int64(1); i <= n; i++ {
				h.rows[seg.Prefix+fmt.Sprint(i)] = aggRow{rowIDField: fmt.Sprint(i), "id": "1", "v": fmt.Sprint(i)}
			}
			before := maps.Clone(jp.items[0].base.mem[0])
			count := int64(0)
			consumerErr := errors.New("consumer stop")
			handled, err := e.scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error {
				for _, row := range rows {
					count++
					if row["f.v"].I != count || row["z.v"].I != 10 {
						t.Fatalf("source progression damaged at%d: %v", count, row)
					}
					clear(row)
				}
				if cancelConsumer {
					return consumerErr
				}
				return nil
			})
			if !handled || e.workerBudget.active != 0 || !reflect.DeepEqual(before, jp.items[0].base.mem[0]) {
				t.Fatalf("ownership/lifecycle: %v %v", handled, err)
			}
			if cancelConsumer {
				if err != consumerErr {
					t.Fatalf("consumer error lost: %v", err)
				}
			} else if err != nil || count != n || h.rangeReads < 4 {
				t.Fatalf("fresh pages: count%d reads%d err%v", count, h.rangeReads, err)
			}
			for _, raw := range h.rows {
				if raw[rowIDField] == "" {
					t.Fatal("raw source pruned")
				}
			}
		})
	}
}

func BenchmarkDecodedFactProbeOwnership(b *testing.B) {
	for _, mode := range []string{"fresh_leaf", "owned_fact_grow", "owned_fact_reserved"} {
		b.Run(mode, func(b *testing.B) {
			meta, raw, need, shape := decodedFactProbeFixture()
			probe := newStarRowProbe(shape)
			var sink map[string]Value
			capacity := 0
			if mode == "owned_fact_reserved" {
				capacity = ownedFactRowCapacity(len(shape.need))
			}
			emit := func(row map[string]Value) error { sink = row; return nil }
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				fact, err := decodeRowCapacity(meta, raw, need, capacity)
				if err != nil {
					b.Fatal(err)
				}
				if mode == "fresh_leaf" {
					err = probe.row(context.Background(), fact, emit)
				} else {
					err = probe.ownedRow(context.Background(), fact, emit)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if len(sink) != 17 || len(raw) != 7 {
				b.Fatal("output shape or raw source changed")
			}
		})
	}
}
