// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"math/bits"
	"time"
)

const OptionStringPartitionStrategy = "adbc.redis.partition_strategy"
const partitionBalanceRequests = 256
const partitionBalanceTimeout = 2 * time.Second

type partitionSpan struct{ lo, hi int64 }

func equalPartitionSpans(lo, hi int64, n int) []partitionSpan {
	if hi < lo {
		return []partitionSpan{{1, 0}}
	}
	n = int(min(int64(n), hi-lo+1))
	out := make([]partitionSpan, n)
	for i := range out {
		out[i] = partitionSpan{lo + (hi-lo+1)*int64(i)/int64(n), lo + (hi-lo+1)*int64(i+1)/int64(n) - 1}
	}
	return out
}

// Counts are advisory weights only. Even under concurrent writes, cuts remain
// disjoint and cover all allocated IDs; no observed-empty membership is pruned.
// Abandon partial quantiles on budget exhaustion rather than publishing an
// accidentally skewed partial result.
func balancePartitionSpans(lo, hi int64, n int, budget int, count func(int64, int64) (int64, error)) ([]partitionSpan, error) {
	fallback := equalPartitionSpans(lo, hi, n)
	if len(fallback) < 2 {
		return fallback, nil
	}
	n = len(fallback)
	query := func(a, b int64) (int64, error) {
		if budget == 0 {
			return 0, errPartitionBalanceBudget
		}
		budget--
		return count(a, b)
	}
	total, err := query(lo, hi)
	if errors.Is(err, errPartitionBalanceBudget) {
		return fallback, nil
	}
	if err != nil {
		return nil, err
	}
	if total < int64(n) {
		return fallback, nil
	}
	type probe struct{ id, rows int64 }
	probes := []probe{{lo - 1, 0}, {hi, total}}
	out := make([]partitionSpan, 0, n)
	start := lo
	for i := 1; i < n; i++ {
		target := total/int64(n)*int64(i) + (total%int64(n))*int64(i)/int64(n)
		left, right := start, hi-int64(n-i)
		for left < right {
			// Reuse prior prefix probes. A count-equal cut anywhere in an empty
			// plateau is sufficient: boundaries need balanced rows, not minimal IDs.
			for _, p := range probes {
				if p.id < left || p.id > right {
					continue
				}
				if p.rows == target {
					left, right = p.id, p.id
					break
				}
				if p.rows < target {
					left = max(left, p.id+1)
				} else {
					right = min(right, p.id)
				}
			}
			if left > right {
				return fallback, nil
			} // inconsistent live counts: advisory only
			if left == right {
				break
			}
			mid := left + (right-left)/2
			lower, upper := probe{lo - 1, 0}, probe{hi, total}
			for _, p := range probes {
				if p.id < left && p.id > lower.id {
					lower = p
				}
				if p.id >= right && p.id < upper.id {
					upper = p
				}
			}
			if lower.rows < target && target < upper.rows {
				high, low := bits.Mul64(uint64(upper.id-lower.id), uint64(target-lower.rows))
				offset, _ := bits.Div64(high, low, uint64(upper.rows-lower.rows))
				mid = max(left, min(right-1, lower.id+int64(offset)))
			}
			matches, err := query(lo, mid)
			if errors.Is(err, errPartitionBalanceBudget) {
				return fallback, nil
			}
			if err != nil {
				return nil, err
			}
			probes = append(probes, probe{mid, matches})
			if matches == target {
				left, right = mid, mid
				break
			}
			if matches >= target {
				right = mid
			} else {
				left = mid + 1
			}
		}
		out = append(out, partitionSpan{start, left})
		start = left + 1
	}
	return append(out, partitionSpan{start, hi}), nil
}

var errPartitionBalanceBudget = errors.New("partition balancing request budget exhausted")

func (s *statementImpl) partitionStrategy() string {
	if s.partitionStrategyName == "" {
		return "rowid"
	}
	return s.partitionStrategyName
}

func (e *executor) balancedPartitionSpans(ctx context.Context, meta *tableMeta, lo, last int64, n int, configured int64) ([]partitionSpan, error) {
	fallback := equalPartitionSpans(lo, last, n)
	if len(fallback) < 2 || len(meta.PendingCleanup) > 0 {
		return fallback, nil
	}
	if err := e.store.checkRangeReadiness(ctx, meta.index()); err != nil {
		return nil, err
	}
	cap, err := e.store.partitionScanCap(ctx, meta.index(), configured)
	if err != nil {
		var ae adbc.Error
		if errors.As(err, &ae) && ae.Code == adbc.StatusNotImplemented {
			return fallback, nil
		}
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, partitionBalanceTimeout)
	defer cancel()
	spans, err := balancePartitionSpans(lo, last, n, partitionBalanceRequests, func(lo, hi int64) (int64, error) {
		if err := cancelled(bounded); err != nil {
			return 0, err
		}
		return e.store.countMatches(bounded, meta.index(), fmt.Sprintf("@%s:[%d %d]", rowIDField, lo, hi))
	})
	if ctx.Err() != nil {
		return nil, cancelled(ctx)
	}
	if bounded.Err() != nil {
		spans, err = fallback, nil
	}
	if err != nil {
		return nil, err
	}
	if err := e.store.checkRangeReadiness(ctx, meta.index()); err != nil {
		return nil, err
	}
	// Balancing must not turn a previously readable sparse range into a reader
	// exceeding the existing per-descriptor request bound.
	for _, span := range spans {
		if span.hi-span.lo+1 > cap*maxPartitionSubranges {
			return fallback, nil
		}
	}
	return spans, nil
}
