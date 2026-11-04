// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestSortSliceInterruptDuringComparisons(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			}
			defer cancel()
			rows := make([]int, 100000)
			for i := range rows {
				rows[i] = rand.IntN(1000)
			}
			calls := 0
			err := sortSlice(ctx, rows, func(i, j int) bool {
				calls++
				if calls == 1000 {
					if deadline {
						<-ctx.Done()
					} else {
						cancel()
					}
				}
				return rows[i] < rows[j]
			})
			var ae adbc.Error
			want := adbc.StatusCancelled
			if deadline {
				want = adbc.StatusTimeout
			}
			if !errors.As(err, &ae) || ae.Code != want || string(ae.SqlState[:]) != stateQueryCanceled {
				t.Fatalf("%+v", err)
			}
			if calls < 1000 || calls > 1256 {
				t.Fatalf("sort continued for %d comparisons", calls)
			}
			// The same helper remains usable after an aborted invocation.
			if err := sortSlice(context.Background(), rows, func(i, j int) bool { return rows[i] < rows[j] }); err != nil || !sort.IntsAreSorted(rows) {
				t.Fatal(err)
			}
		})
	}
}

func TestSortSliceStableAndPanic(t *testing.T) {
	type row struct{ key, ordinal int }
	rows := make([]row, 20000)
	for i := range rows {
		rows[i] = row{rand.IntN(20), i}
	}
	want := slices.Clone(rows)
	sort.SliceStable(want, func(i, j int) bool { return want[i].key < want[j].key })
	if err := sortSlice(nil, rows, func(i, j int) bool { return rows[i].key < rows[j].key }); err != nil || !slices.Equal(rows, want) {
		t.Fatal("stable order changed", err)
	}
	sentinel := errors.New("comparator panic")
	defer func() {
		if recover() != sentinel {
			t.Fatal("comparator panic swallowed")
		}
	}()
	_ = sortSlice(nil, rows, func(int, int) bool { panic(sentinel) })
}

func BenchmarkSortSlice(b *testing.B) {
	for _, n := range []int{10000, 100000} {
		input := make([]int, n)
		for i := range input {
			input[i] = rand.IntN(n / 10)
		}
		for _, cooperative := range []bool{false, true} {
			b.Run(strconv.Itoa(n)+map[bool]string{false: "/standard", true: "/cooperative"}[cooperative], func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rows := slices.Clone(input)
					less := func(i, j int) bool { return rows[i] < rows[j] }
					if cooperative {
						_ = sortSlice(context.Background(), rows, less)
					} else {
						sort.SliceStable(rows, less)
					}
				}
			})
		}
	}
}
