// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"github.com/apache/arrow-adbc/go/adbc"
)

// flexCandidateBudget bounds one complete Flex candidate materialization: its
// candidate count and the key bytes of its Search replies.
type flexCandidateBudget struct{ rows, bytes int64 }

var (
	// flexScanCandidates bounds a scan that discovers its own candidates. A
	// range-bounded reader (a partition or aggregate range worker) discovers
	// the complete set and keeps its range, so this stays small.
	flexScanCandidates = flexCandidateBudget{rows: snapshotFlexCandidateCap, bytes: snapshotFlexCandidateBytes}
	// flexSharedCandidates bounds a parallel reader's coordinator, which
	// discovers the complete set once and gives each worker a row-ID slice
	// (sharedFlexCandidates).
	flexSharedCandidates = flexCandidateBudget{rows: snapshotFlexSharedCandidateCap, bytes: snapshotFlexSharedCandidateBytes}
)

// Share the exact complete-result admission policy between candidate execution
// and parallel directory-fallback proof. Saturation prevents int64 overflow;
// callers still validate every remaining source after a planning refusal.
func completeSnapshotBudget(prefix string, count, total, keyBytes int64) (int64, int64, bool) {
	return flexScanCandidates.add(prefix, count, total, keyBytes)
}

func (b flexCandidateBudget) add(prefix string, count, total, keyBytes int64) (int64, int64, bool) {
	if count < 0 || total > b.rows || keyBytes > b.bytes {
		return b.rows + 1, b.bytes + 1, true
	}
	width := int64(len(prefix)) + 19
	if count > b.rows-total || count > b.bytes/width {
		return b.rows + 1, b.bytes + 1, true
	}
	bytes := keyBytes + count*width
	if bytes > b.bytes {
		return b.rows + 1, b.bytes + 1, true
	}
	return total + count, bytes, false
}
func completeSnapshotCount(reply any, seg snapshotSegment) (int64, error) {
	if snapshotReplyWarning(reply) {
		return 0, errorf(adbc.StatusIO, "partial snapshot candidate count")
	}
	count, err := parseSearchCount(reply)
	if err != nil {
		return 0, err
	}
	keys, err := parseSearchKeys(reply)
	if err != nil {
		return 0, err
	}
	if count < 0 || count > seg.Rows || len(keys) != 0 {
		return 0, errorf(adbc.StatusIO, "invalid snapshot candidate count")
	}
	return count, nil
}

// True proves the existing complete-candidate path must fall back solely due
// to its current count/byte budgets. It never fetches candidate rows, guesses a
// hidden native limit, or converts an uncertain reply into fallback. All source
// identities/counts and the shared lease are checked before returning true.
func (s *store) completeFlexOverBudget(ctx context.Context, m *tableMeta, query string) (bool, error) {
	counts, err := s.completeFlexCandidateCounts(ctx, m, query)
	if err != nil || counts == nil {
		return false, err
	}
	return counts.refused(flexScanCandidates), nil
}

// flexCandidateCounts are every source's complete candidate count for one
// query, checked as completeFlexOverBudget checks them.
type flexCandidateCounts struct {
	segments []snapshotSegment
	counts   []int64
}

func (c *flexCandidateCounts) total() int64 {
	var n int64
	for _, v := range c.counts {
		n += v
	}
	return n
}

// refused reports whether budget refuses the complete set, as the
// materialization would.
func (c *flexCandidateCounts) refused(budget flexCandidateBudget) bool {
	var total, keyBytes int64
	over := false
	for i, seg := range c.segments {
		var refused bool
		total, keyBytes, refused = budget.add(seg.Prefix, c.counts[i], total, keyBytes)
		over = over || refused
	}
	return over
}

// completeFlexCandidateCounts counts a query's complete candidates in every
// source without fetching them. nil means the complete-candidate path does not
// apply (no plain Flex index) or Search refused a count with its own result
// limit; unknown replies fail.
func (s *store) completeFlexCandidateCounts(ctx context.Context, m *tableMeta, query string) (*flexCandidateCounts, error) {
	filter := newSnapshotPageFilter(ctx, s, m, query)
	if filter == nil {
		return nil, nil
	}
	if err := s.checkKeys(ctx, m, false); err != nil {
		return nil, err
	}
	out := &flexCandidateCounts{segments: filter.segments}
	for _, seg := range filter.segments {
		reply, err := completeSnapshotSearch(ctx, s, seg, query, 0)
		if err == errCompleteSnapshotLimit {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		count, err := completeSnapshotCount(reply, seg)
		if err != nil {
			return nil, err
		}
		out.counts = append(out.counts, count)
	}
	if err := s.checkKeys(ctx, m, false); err != nil {
		return nil, err
	}
	return out, nil
}
