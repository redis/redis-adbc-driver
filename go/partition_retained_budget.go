// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A cumulative logical allocation/work budget deliberately never refunds credits
// within a retained execution. This bounds abandoned intermediates as well as
// reachable data without assuming a GC schedule. It is not a process RSS limit.
type retainedBudget struct {
	remaining atomic.Int64
	failed    atomic.Bool
}

func newRetainedBudget(limit int64) *retainedBudget {
	b := &retainedBudget{}
	b.remaining.Store(max(limit, 0))
	return b
}
func retainedBudgetError() error {
	return codeStateErrorf(adbc.StatusInvalidState, "53200", "retained execution resource budget exhausted")
}
func (b *retainedBudget) reserve(amount int64) error {
	for {
		if b.failed.Load() {
			return retainedBudgetError()
		}
		left := b.remaining.Load()
		if amount < 0 || amount > left {
			// Poison the budget before returning an error. An evaluator that catches
			// an error (e.g. TRY_CAST) must never revive an exhausted execution.
			if b.remaining.CompareAndSwap(left, 0) {
				b.failed.Store(true)
				return retainedBudgetError()
			}
			continue
		}
		if b.remaining.CompareAndSwap(left, left-amount) {
			return nil
		}
	}
}
