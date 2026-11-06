// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"math"
	"testing"
)

func TestSnapshotCompactionSourceSelection(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []int64
		want int
	}{
		{"small_suffix", []int64{20000, 1, 1, 1}, 1},
		{"balanced", []int64{10, 10, 10, 10}, 0},
		{"empty_prefix", []int64{0, 20000, 1, 1}, 2},
		{"two_large_segments", []int64{20000, 20000, 1, 1}, 2},
		{"no_single_source_job", []int64{20000, 20000, 1}, 0},
		{"ratio_boundary", []int64{8, 1, 1}, 0},
		{"all_empty", []int64{0, 0, 0, 0}, 0},
		{"overflow_falls_back", []int64{math.MaxInt64, 1, 1, 1}, 0},
		{"large_exact_sum", []int64{math.MaxInt64 - 3, 1, 1, 1}, 1},
		{"large_empty_suffix", []int64{math.MaxInt64 / 2, math.MaxInt64 / 2, 0, 0}, 2},
		{"invalid_count", []int64{-1, 1, 1, 1}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			segments := make([]snapshotSegment, len(test.rows))
			for i, n := range test.rows {
				segments[i].Rows = n
			}
			if got := snapshotCompactionSourceStart(segments); got != test.want {
				t.Fatalf("selected start %d, want %d", got, test.want)
			}
		})
	}
}
