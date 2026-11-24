// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// Freeze the preceding chained-merge algorithm as a semantic/allocation control.
// This models exactly the old hot loop, including key lookup and cancellation.
func chainedStarProbeControl(ctx context.Context, shape *starShape, row map[string]Value, emit func(map[string]Value) error) error {
	var probe func(int, map[string]Value) error
	probe = func(d int, row map[string]Value) error {
		if err := cancelled(ctx); err != nil {
			return err
		}
		if d == len(shape.dims) {
			return emit(row)
		}
		dim := &shape.dims[d]
		key, ok := starRowKey(row, dim.factCols)
		if !ok {
			return nil
		}
		for _, right := range dim.buckets[key] {
			if err := probe(d+1, mergeRows(row, right)); err != nil {
				return err
			}
		}
		return nil
	}
	return probe(0, row)
}
func leafProbeFixture(left, right int, miss bool) (*starShape, map[string]Value) {
	fact := map[string]Value{"f.customer": intValue(typeInt64, 1), "f.date": intValue(typeInt64, 2), "f.list": intValue(typeInt64, 2100), "f.cost": intValue(typeInt64, 1000), "f.discount": intValue(typeInt64, 100), "f.sales": intValue(typeInt64, 2000), "f.__key": stringValue("original:fact"), "nullable": nullValue(typeString), "shared": stringValue("fact")}
	customer := map[string]Value{"c.sk": intValue(typeInt64, 1), "c.customer_id": stringValue("customer-1"), "c.first_name": stringValue("First"), "c.last_name": stringValue("Last"), "c.preferred": stringValue("Y"), "c.country": stringValue("Country"), "c.login": stringValue("login"), "c.email": stringValue("email"), "c.__key": stringValue("original:customer"), "shared": stringValue("customer")}
	date := map[string]Value{"d.sk": intValue(typeInt64, 2), "d.year": intValue(typeInt64, 2001), "d.__key": stringValue("original:date"), "shared": stringValue("date")}
	a := starDimension{factCols: []string{"f.customer"}, dimCols: []string{"c.sk"}, buckets: map[string][]map[string]Value{}}
	b := starDimension{factCols: []string{"f.date"}, dimCols: []string{"d.sk"}, buckets: map[string][]map[string]Value{}}
	ka, _ := starRowKey(customer, a.dimCols)
	kb, _ := starRowKey(date, b.dimCols)
	for i := 0; i < left; i++ {
		r := maps.Clone(customer)
		r["c.customer_id"] = stringValue(fmt.Sprint("customer-", i))
		a.buckets[ka] = append(a.buckets[ka], r)
	}
	if !miss {
		for i := 0; i < right; i++ {
			r := maps.Clone(date)
			r["d.year"] = intValue(typeInt64, int64(2001+i))
			b.buckets[kb] = append(b.buckets[kb], r)
		}
	}
	return &starShape{dims: []starDimension{a, b}}, fact
}

func TestStarLeafProbeBagsOrderOverwriteAndOwnership(t *testing.T) {
	shape, fact := leafProbeFixture(2, 3, false)
	original := maps.Clone(fact)
	var want, got []map[string]Value
	if err := chainedStarProbeControl(context.Background(), shape, fact, func(row map[string]Value) error { want = append(want, row); return nil }); err != nil {
		t.Fatal(err)
	}
	p := newStarRowProbe(shape)
	if err := p.row(context.Background(), fact, func(row map[string]Value) error { got = append(got, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 || !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered Cartesian bag differs: got%v want%v", got, want)
	}
	for i, row := range got {
		if row["shared"].S != "date" || !row["nullable"].Null || row["f.__key"].S != "original:fact" || row["c.__key"].S != "original:customer" || row["d.__key"].S != "original:date" {
			t.Fatalf("overwrite/NULL/provenance row%d: %v", i, row)
		}
	}
	got[0]["shared"] = stringValue("consumer changed output")
	got[0]["c.first_name"] = stringValue("changed")
	if !reflect.DeepEqual(fact, original) || got[1]["shared"].S != "date" {
		t.Fatal("output map aliases source or sibling")
	}
	// Reuse scratch for another fact and retain the original emitted maps.
	second := maps.Clone(fact)
	second["f.__key"] = stringValue("second:fact")
	if err := p.row(context.Background(), second, func(row map[string]Value) error {
		if row["f.__key"].S != "second:fact" || row["c.first_name"].S != "First" {
			t.Fatal("scratch leaked output mutation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got[0]["f.__key"].S != "original:fact" {
		t.Fatal("retained output changed during scratch reuse")
	}
	for _, dim := range shape.dims {
		for _, bucket := range dim.buckets {
			for _, row := range bucket {
				if v, ok := row["c.first_name"]; ok && v.S != "First" {
					t.Fatal("source dimension map mutated")
				}
			}
		}
	}
}

func TestStarLeafProbeMissingNullAndScratchReuse(t *testing.T) {
	shape, fact := leafProbeFixture(2, 3, false)
	p := newStarRowProbe(shape)
	for _, field := range []string{"f.customer", "f.date"} {
		missing := maps.Clone(fact)
		delete(missing, field)
		null := maps.Clone(fact)
		null[field] = nullValue(typeInt64)
		absent := maps.Clone(fact)
		absent[field] = intValue(typeInt64, 999)
		for _, row := range []map[string]Value{missing, null, absent} {
			if err := p.row(context.Background(), row, func(map[string]Value) error { t.Fatal("nonmatch emitted"); return nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := 0
	if err := p.row(context.Background(), fact, func(map[string]Value) error { count++; return nil }); err != nil || count != 6 {
		t.Fatalf("early refusal poisoned scratch: count%d err%v", count, err)
	}
}

func TestStarLeafProbeErrorsAndCancellation(t *testing.T) {
	shape, fact := leafProbeFixture(2, 3, false)
	p := newStarRowProbe(shape)
	sentinel := errors.New("consumer stopped")
	calls := 0
	err := p.row(context.Background(), fact, func(map[string]Value) error { calls++; return sentinel })
	if err != sentinel || calls != 1 {
		t.Fatalf("consumer error not immediate: %v calls%d", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	err = p.row(ctx, fact, func(map[string]Value) error { calls++; cancel(); return nil })
	if err == nil || calls != 1 {
		t.Fatalf("cancellation not preserved: %v calls%d", err, calls)
	}
}

func TestStarLeafProbeWorkerScratchIsIndependent(t *testing.T) {
	shape, fact := leafProbeFixture(2, 3, false)
	var count atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := newStarRowProbe(shape)
			for i := 0; i < 100; i++ {
				if err := p.row(context.Background(), fact, func(row map[string]Value) error {
					row["shared"] = stringValue("owned output")
					count.Add(1)
					return nil
				}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if count.Load() != 2400 || fact["shared"].S != "fact" {
		t.Fatalf("shared ownership failed: %d", count.Load())
	}
}

// Match Q4's projected widths: six fact values, eight customer values,
// and two date values. Ownership/provenance-only fixture extras stay in the
// functional tests rather than inflating the allocation control.
func projectProbeBenchmarkRows(shape *starShape, fact map[string]Value) {
	for _, name := range []string{"f.__key", "nullable", "shared"} {
		delete(fact, name)
	}
	for _, dim := range shape.dims {
		for _, bucket := range dim.buckets {
			for _, row := range bucket {
				for name := range row {
					if name == "shared" || name == "c.__key" || name == "d.__key" {
						delete(row, name)
					}
				}
			}
		}
	}
}

func TestStarLeafProbeAllocatesLessThanChainedControl(t *testing.T) {
	for _, miss := range []bool{false, true} {
		t.Run(fmt.Sprint(miss), func(t *testing.T) {
			shape, fact := leafProbeFixture(1, 1, miss)
			projectProbeBenchmarkRows(shape, fact)
			p := newStarRowProbe(shape)
			var sink map[string]Value
			emit := func(row map[string]Value) error { sink = row; return nil }
			control := testing.AllocsPerRun(100, func() {
				if err := chainedStarProbeControl(context.Background(), shape, fact, emit); err != nil {
					panic(err)
				}
			})
			leaf := testing.AllocsPerRun(100, func() {
				if err := p.row(context.Background(), fact, emit); err != nil {
					panic(err)
				}
			})
			if leaf >= control {
				t.Fatalf("leaf allocations %.0f must improve on chained %.0f", leaf, control)
			}
			if !miss && sink == nil {
				t.Fatal("allocation measurement omitted output")
			}
			t.Logf("chained %.0f allocs/row; leaf %.0f allocs/row", control, leaf)
		})
	}
}

func BenchmarkStarProbeAllocation(b *testing.B) {
	for _, tc := range []struct {
		name        string
		left, right int
		miss        bool
	}{{"q4_shaped", 1, 1, false}, {"duplicates", 3, 4, false}, {"late_nonmatch", 1, 1, true}} {
		shape, fact := leafProbeFixture(tc.left, tc.right, tc.miss)
		projectProbeBenchmarkRows(shape, fact)
		for _, leaf := range []bool{false, true} {
			name := "chained"
			if leaf {
				name = "leaf"
			}
			b.Run(tc.name+"/"+name, func(b *testing.B) {
				p := newStarRowProbe(shape)
				var sink map[string]Value
				emit := func(row map[string]Value) error { sink = row; return nil }
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var err error
					if leaf {
						err = p.row(context.Background(), fact, emit)
					} else {
						err = chainedStarProbeControl(context.Background(), shape, fact, emit)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if !tc.miss && sink == nil {
					b.Fatal("missing output")
				}
			})
		}
	}
}
