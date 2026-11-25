// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// A Flex partition combines its partial tuples by key in memory before the
// spill. With a tight group limit the same key is handed on more than once,
// but after the spill's own combine every group equals the direct sequential
// combine of all its tuples: exact decimal partials (mixed scales), NULL
// contributions, MIN and counts.
func TestPartialGroupCombinerMatchesSequentialCombine(t *testing.T) {
	p := &tupleAggregatePlan{
		partialGroups: true,
		cols:          []columnMeta{{Name: "g", Type: typeInt64}},
		functions:     []*Func{{Name: "COUNT", Star: true}, {Name: "SUM"}, {Name: "MIN"}},
		decimalSums:   []bool{false, true, false},
		types:         []ColType{typeInt64, decimalType(38, 4), typeInt64},
	}
	rng := rand.New(rand.NewSource(7))
	var tuples []spillTuple
	for i := 0; i < 5000; i++ {
		g := rng.Intn(9)
		n, sum, minN, minV := "1", fmt.Sprintf("%d:%d", rng.Int63n(2000000)-1000000, rng.Intn(5)), "1", fmt.Sprint(rng.Int63n(1000)-500)
		if i%17 == 0 { // NULL inputs contribute nothing
			n, sum, minN, minV = "0", "0", "0", "0"
		}
		tuples = append(tuples, spillTuple{key: fmt.Sprint(g), groupKey: fmt.Sprint(g), count: 1,
			raw: []string{fmt.Sprint(g), n, sum, minN, minV}, null: make([]bool, 5)})
	}
	sequential := map[string]*spillTuple{}
	for _, tuple := range tuples {
		tuple.raw = append([]string(nil), tuple.raw...)
		if dst := sequential[tuple.key]; dst != nil {
			if err := combineSpillTuple(p.combine, dst, tuple); err != nil {
				t.Fatal(err)
			}
			continue
		}
		copied := tuple
		sequential[tuple.key] = &copied
	}
	var handed []spillTuple
	visit := func(tuple spillTuple) error {
		tuple.raw = append([]string(nil), tuple.raw...)
		handed = append(handed, tuple)
		return nil
	}
	c := newPartialGroupCombiner(p.combine, 3)
	for _, tuple := range tuples {
		tuple.raw = append([]string(nil), tuple.raw...)
		if err := c.add(tuple, visit); err != nil {
			t.Fatal(err)
		}
		if len(c.groups) > 3 {
			t.Fatalf("held %d groups over the limit", len(c.groups))
		}
	}
	if err := c.flush(visit); err != nil {
		t.Fatal(err)
	}
	if len(handed) >= len(tuples) || len(handed) <= len(sequential) {
		t.Fatalf("handed on %d tuples for %d rows and %d groups", len(handed), len(tuples), len(sequential))
	}
	merged := map[string]*spillTuple{}
	for _, tuple := range handed {
		if dst := merged[tuple.key]; dst != nil {
			if err := combineSpillTuple(p.combine, dst, tuple); err != nil {
				t.Fatal(err)
			}
			continue
		}
		copied := tuple
		merged[tuple.key] = &copied
	}
	if len(merged) != len(sequential) {
		t.Fatalf("%d groups, want %d", len(merged), len(sequential))
	}
	for key, want := range sequential {
		got := merged[key]
		if got == nil || got.count != want.count || !reflect.DeepEqual(got.raw[1:], want.raw[1:]) {
			t.Fatalf("group %s: %+v, want %+v", key, got, want)
		}
	}
}
