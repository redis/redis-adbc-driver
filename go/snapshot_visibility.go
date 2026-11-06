// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotMaskIDs = 4096
const snapshotMaskRanges = 256

type snapshotMaskSpan struct{ lo, hi int64 }

func snapshotMaskID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n < 1 || n >= exactDoubleLimit || fmt.Sprintf("%020d", n) != id {
		return 0, errorf(adbc.StatusIO, "invalid snapshot visibility row ID")
	}
	return n, nil
}

type snapshotIndexQuery struct {
	segment   snapshotSegment
	query     string
	rowFilter string
}

func andSnapshotQuery(a, b string) string {
	if a == "" || a == "*" {
		return b
	}
	if b == "" || b == "*" {
		return a
	}
	return "(" + a + ") (" + b + ")"
}

// Immutable exact directories define per-segment visibility. Append-only ranges
// do not intersect, so no exclusion list is transferred. Complete contiguous
// runs use cardinality/endpoint proofs; small scattered changes
// are coalesced into numeric ranges. Excessive masks fall back to the bounded
// candidate/HASH path rather than constructing unbounded Search expressions.
func (e *executor) snapshotIndexQueries(ctx context.Context, m *tableMeta) ([]snapshotIndexQuery, bool, error) {
	if !e.snapshotIndexable(ctx, m) {
		return nil, false, nil
	}
	segments := m.snapshotSegments()
	type bounds struct {
		lo, hi          string
		firstID, lastID int64
		first, last     *goredis.StringSliceCmd
	}
	bs := make([]bounds, len(segments))
	pipe := e.store.redis(ctx).Pipeline()
	for i, seg := range segments {
		if seg.Rows == 0 || snapshotOwns(ctx, seg.Prefix) {
			continue
		}
		dir := snapshotMembers(snapshotDataID(seg.Prefix))
		bs[i].first = pipe.ZRange(ctx, dir, 0, 0)
		bs[i].last = pipe.ZRange(ctx, dir, -1, -1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, false, wrapRedis(err, "failed to read snapshot visibility bounds")
	}
	for i := range bs {
		if bs[i].first == nil {
			continue
		}
		first, last := bs[i].first.Val(), bs[i].last.Val()
		if len(first) != 1 || len(last) != 1 {
			return nil, false, errorf(adbc.StatusIO, "snapshot visibility directory is incomplete")
		}
		bs[i].lo, bs[i].hi = first[0], last[0]
		var err error
		bs[i].firstID, err = snapshotMaskID(first[0])
		if err != nil {
			return nil, false, err
		}
		bs[i].lastID, err = snapshotMaskID(last[0])
		if err != nil {
			return nil, false, err
		}
	}
	type probe struct {
		older int
		cmd   *goredis.StringSliceCmd
	}
	var probes []probe
	spans := make([][]snapshotMaskSpan, len(segments))
	pipe = e.store.redis(ctx).Pipeline()
	for i := range segments {
		if bs[i].lo == "" {
			continue
		}
		for j := i + 1; j < len(segments); j++ {
			if bs[j].lo == "" || bs[j].hi < bs[i].lo || bs[j].lo > bs[i].hi {
				continue
			}
			// Unique canonical directory IDs plus exact cardinality and bounds
			// prove a full run. Clip it without transferring every replaced ID.
			if segments[j].Rows == bs[j].lastID-bs[j].firstID+1 {
				spans[i] = append(spans[i], snapshotMaskSpan{
					max(bs[i].firstID, bs[j].firstID), min(bs[i].lastID, bs[j].lastID)})
				continue
			}
			cmd := pipe.ZRangeByLex(ctx, snapshotMembers(snapshotDataID(segments[j].Prefix)), &goredis.ZRangeBy{Min: "[" + bs[i].lo, Max: "[" + bs[i].hi, Count: snapshotMaskIDs + 1})
			probes = append(probes, probe{i, cmd})
		}
	}
	if len(probes) > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, false, wrapRedis(err, "failed to read snapshot visibility exclusions")
		}
	}
	masks := make([]map[string]bool, len(segments))
	for _, probe := range probes {
		ids := probe.cmd.Val()
		if len(ids) > snapshotMaskIDs {
			return nil, false, nil
		}
		if masks[probe.older] == nil {
			masks[probe.older] = map[string]bool{}
		}
		for _, id := range ids {
			masks[probe.older][id] = true
		}
		if len(masks[probe.older]) > snapshotMaskIDs {
			return nil, false, nil
		}
	}
	var queries []snapshotIndexQuery
	for i, seg := range segments {
		if bs[i].lo == "" {
			continue
		}
		for id := range masks[i] {
			n, err := snapshotMaskID(id)
			if err != nil {
				return nil, false, err
			}
			spans[i] = append(spans[i], snapshotMaskSpan{n, n})
		}
		slices.SortFunc(spans[i], func(a, b snapshotMaskSpan) int { return cmp.Compare(a.lo, b.lo) })
		var ranges []string
		var lo, hi int64
		flush := func() {
			if lo > 0 {
				ranges = append(ranges, fmt.Sprintf("@%s:[%d %d]", rowIDField, lo, hi))
			}
		}
		for _, span := range spans[i] {
			if lo == 0 {
				lo, hi = span.lo, span.hi
			} else if span.lo <= hi+1 {
				hi = max(hi, span.hi)
			} else {
				flush()
				lo, hi = span.lo, span.hi
			}
		}
		flush()
		if len(ranges) > snapshotMaskRanges {
			return nil, false, nil
		}
		q := "-@" + snapshotDeletedField + ":[1 1]"
		if len(ranges) > 0 {
			q += " -(" + strings.Join(ranges, " | ") + ")"
		}
		physical := *m
		physical.KeyPrefix, physical.IndexName = seg.Prefix, seg.Index
		physical.SnapshotOverlay = false
		queries = append(queries, snapshotIndexQuery{segment: seg, query: q, rowFilter: physical.rowFilter()})
	}
	return queries, true, nil
}
