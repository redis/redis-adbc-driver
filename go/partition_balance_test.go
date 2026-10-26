// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestBalancedPartitionCoverage(t *testing.T) {
	for _, layout := range []string{"dense", "sparse", "tail", "clusters", "empty"} {
		t.Run(layout, func(t *testing.T) {
			var ids []int
			for i := 1; i <= 100000; i++ {
				if layout == "dense" || layout == "sparse" && i%10 == 0 || layout == "tail" && i > 90000 || layout == "clusters" && (i <= 5000 || i > 95000) {
					ids = append(ids, i)
				}
			}
			calls := 0
			count := func(lo, hi int64) (int64, error) {
				calls++
				return int64(sort.SearchInts(ids, int(hi)+1) - sort.SearchInts(ids, int(lo))), nil
			}
			spans, err := balancePartitionSpans(1, 100000, 8, partitionBalanceRequests, count)
			if err != nil {
				t.Fatal(err)
			}
			if len(spans) != 8 || spans[0].lo != 1 || spans[7].hi != 100000 {
				t.Fatal(spans)
			}
			for i, s := range spans {
				if s.hi < s.lo || i > 0 && s.lo != spans[i-1].hi+1 {
					t.Fatal("lost or overlapping allocated IDs", spans)
				}
				if len(ids) > 0 {
					n, _ := count(s.lo, s.hi)
					if n != int64(len(ids)/8) {
						t.Fatalf("unbalanced %s: %d rows in %+v", layout, n, s)
					}
				}
			}
			if (layout == "dense" || layout == "tail") && calls > 72 {
				t.Fatalf("excessive probes for a simple distribution: %d", calls)
			}
			if calls > partitionBalanceRequests+8 {
				t.Fatal("unbounded planning requests")
			}
		})
	}
}
func TestBalancedPartitionBudgetAndFailure(t *testing.T) {
	calls := 0
	count := func(lo, hi int64) (int64, error) { calls++; return hi - lo + 1, nil }
	got, err := balancePartitionSpans(1, 100000, 8, 1, count)
	if err != nil || calls != 1 || !reflect.DeepEqual(got, equalPartitionSpans(1, 100000, 8)) {
		t.Fatalf("partial budget fallback: %+v %v %d", got, err, calls)
	}
	sentinel := errors.New("count failed")
	if _, err := balancePartitionSpans(1, 100, 4, 256, func(int64, int64) (int64, error) { return 0, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("count error hidden", err)
	}
	for _, last := range []int64{0, 1, 2} {
		spans, err := balancePartitionSpans(1, last, 8, 256, count)
		if err != nil || len(spans) != int(max(last, 1)) {
			t.Fatal(spans, err)
		}
	}
}
