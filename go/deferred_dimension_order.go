// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// Saturate before multiplication: counts are cardinality estimates, not an
// admission grant. Runtime retained-memory/work limits still own actual rows.
const nativeDimensionProductRows = int64(4096)

func deferredDimensionProduct(product, count int64) int64 {
	return boundedDimensionProduct(product, count, int64(maxUnionTerms))
}

func boundedDimensionProduct(product, count, cap int64) int64 {
	if product == 0 || count == 0 {
		return 0
	}
	if product < 0 || count < 0 || product > cap || count > cap/product {
		return cap + 1
	}
	return product * count
}

// Postpone only a connected deferred UNION or proven large published fact.
// Planning products cap at 1000 for UNION and 4096 for native targets;
// each independently generated key set retains its separate 1000-value cap.
// Outer/lateral joins never reach this call.
func (e *executor) boundedDeferredDimension(ctx context.Context, jp *joinPlan, counts []int64, chosen map[int]bool, connected []bool, best int, product int64) int {
	if best < 0 || !connected[best] || product <= 0 || product > nativeDimensionProductRows {
		return best
	}
	target := jp.items[best].base
	deferred := target.view != nil && target.view.unionDemand
	nativeLarge := counts[best] > maxUnionTerms && e.publishedJoinSource(ctx, target)
	if nativeLarge {
		rows := int64(0)
		for _, seg := range target.snapshotSegments() {
			rows = min(int64(maxUnionTerms+1), rows+min(seg.Rows, int64(maxUnionTerms+1)))
		}
		nativeLarge = rows > maxUnionTerms
	}
	if !deferred && !nativeLarge {
		return best
	}
	limit := int64(maxUnionTerms)
	if nativeLarge {
		limit = nativeDimensionProductRows
	}
	if product > limit {
		return best
	}
	selected := -1
	for i, it := range jp.items {
		if chosen[i] || connected[i] || i == best || counts[i] < 0 || boundedDimensionProduct(product, counts[i], limit) > limit {
			continue
		}
		m := it.base
		if m.view != nil || m.lazy != nil || m.group != nil || m.join != nil || m.Live || m.Unmanaged {
			continue
		}
		stable := m.isMem
		if !stable {
			stable = e.publishedJoinSource(ctx, m)
		}
		if !stable {
			continue
		}
		if selected < 0 || counts[i] < counts[selected] {
			selected = i
		}
	}
	if selected >= 0 {
		return selected
	}
	return best
}
