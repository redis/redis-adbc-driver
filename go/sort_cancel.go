// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"sort"
)

// Test seam for deterministic interruption inside active comparison work.
var testHookSortCheckpoint func()

// sortSlice uses the established stable ordering algorithm and comparators.
// Cancellation aborts the stack immediately, never returning inconsistent
// comparison answers. Only this invocation's private token is recovered.
// Callers must discard the private working slice on error, before publication.
func sortSlice(ctx context.Context, rows any, less func(int, int) bool) error {
	return sortSliceWith(ctx, rows, less, sort.SliceStable)
}
func sortSliceUnstable(ctx context.Context, rows any, less func(int, int) bool) error {
	return sortSliceWith(ctx, rows, less, sort.Slice)
}
func sortSliceWith(ctx context.Context, rows any, less func(int, int) bool, sorter func(any, func(int, int) bool)) (err error) {
	if err = cancelled(ctx); err != nil {
		return err
	}
	token := new(struct{ cancelled bool })
	defer func() {
		if p := recover(); p != nil {
			if p != token {
				panic(p)
			}
		}
	}()
	comparisons := 0
	sorter(rows, func(i, j int) bool {
		if comparisons&255 == 0 {
			if testHookSortCheckpoint != nil {
				testHookSortCheckpoint()
			}
			if err = cancelled(ctx); err != nil {
				panic(token)
			}
		}
		comparisons++
		return less(i, j)
	})
	return cancelled(ctx)
}
