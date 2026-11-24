// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"github.com/apache/arrow-adbc/go/adbc"
)

// Share the exact complete-result admission policy between candidate execution
// and parallel directory-fallback proof. Saturation prevents int64 overflow;
// callers still validate every remaining source after a planning refusal.
func completeSnapshotBudget(prefix string, count, total, keyBytes int64) (int64, int64, bool) {
	if count < 0 || total > snapshotFlexCandidateCap || keyBytes > snapshotFlexCandidateBytes {
		return snapshotFlexCandidateCap + 1, snapshotFlexCandidateBytes + 1, true
	}
	width := int64(len(prefix)) + 19
	if count > snapshotFlexCandidateCap-total || count > int64(snapshotFlexCandidateBytes)/width {
		return snapshotFlexCandidateCap + 1, snapshotFlexCandidateBytes + 1, true
	}
	bytes := keyBytes + count*width
	if bytes > snapshotFlexCandidateBytes {
		return snapshotFlexCandidateCap + 1, snapshotFlexCandidateBytes + 1, true
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
	filter := newSnapshotPageFilter(ctx, s, m, query)
	if filter == nil {
		return false, nil
	}
	if err := s.checkKeys(ctx, m, false); err != nil {
		return false, err
	}
	var total, keyBytes int64
	over := false
	for _, seg := range filter.segments {
		reply, err := completeSnapshotSearch(ctx, s, seg, query, 0)
		if err == errCompleteSnapshotLimit {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		count, err := completeSnapshotCount(reply, seg)
		if err != nil {
			return false, err
		}
		var refused bool
		total, keyBytes, refused = completeSnapshotBudget(seg.Prefix, count, total, keyBytes)
		over = over || refused
	}
	if err := s.checkKeys(ctx, m, false); err != nil {
		return false, err
	}
	return over, nil
}
