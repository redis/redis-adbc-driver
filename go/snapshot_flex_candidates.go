// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Bound the complete, transient candidate materialization independently of the
// table size. Canonical managed keys are short; this conservative count cap
// avoids an unbudgeted million-key reply/map/sort allocation per scan/worker.
const snapshotFlexCandidateCap = 131072
const snapshotFlexCandidateBytes = 16 << 20

var errCompleteSnapshotLimit = errors.New("native complete Search result limit")

func completeSnapshotLimit(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	return ok && strings.HasPrefix(native.Error(), "LIMIT exceeds maximum")
}

// Search supplies a COMPLETE bounded candidate set, never visibility. Broad
// sources retain exact-directory scanning without candidate1's per-page probes.
func openFlexSnapshotCandidates(ctx context.Context, s *store, m *tableMeta, query string, fields []string, bounds *rowIDRangeScan) (*snapshotCandidateScan, error) {
	filter := newSnapshotPageFilter(ctx, s, m, query)
	if filter == nil {
		return nil, nil
	}
	if err := s.checkKeys(ctx, m, false); err != nil {
		return nil, err
	}
	counts := make([]int64, len(filter.segments))
	total := int64(0)
	keyBytes := int64(0)
	for i, seg := range filter.segments {
		reply, err := completeSnapshotSearch(ctx, s, seg, query, 0)
		if err == errCompleteSnapshotLimit {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if snapshotReplyWarning(reply) {
			return nil, errorf(adbc.StatusIO, "partial snapshot candidate count")
		}
		count, err := parseSearchCount(reply)
		if err != nil {
			return nil, err
		}
		keys, err := parseSearchKeys(reply)
		if err != nil {
			return nil, err
		}
		if count < 0 || count > seg.Rows || len(keys) != 0 {
			return nil, errorf(adbc.StatusIO, "invalid snapshot candidate count")
		}
		if count > snapshotFlexCandidateCap-total || count > int64(snapshotFlexCandidateBytes)/(int64(len(seg.Prefix))+19) {
			return nil, nil
		}
		keyBytes += count * (int64(len(seg.Prefix)) + 19)
		if keyBytes > snapshotFlexCandidateBytes {
			return nil, nil
		}
		counts[i] = count
		total += count
	}
	d := &snapshotCandidateScan{store: s, meta: m, query: query, fields: fields, page: snapshotDeltaPage, completeFlex: true}
	for i, seg := range filter.segments {
		src := snapshotCandidateSource{segment: seg, done: true}
		if counts[i] > 0 {
			reply, err := completeSnapshotSearch(ctx, s, seg, query, int(counts[i]))
			if err == errCompleteSnapshotLimit {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			ids, err := completeSnapshotIDs(reply, seg, counts[i])
			if err != nil {
				return nil, err
			}
			src.ids = ids
		}
		// Prove Search's own source membership before the RAM visibility merger
		// probes newer sources. This also verifies directories for empty sources.
		if err := checkCompleteSnapshotDirectory(ctx, s, seg, src.ids); err != nil {
			return nil, err
		}
		if bounds != nil {
			kept := src.ids[:0]
			for _, id := range src.ids {
				n, _ := strconv.ParseInt(id, 10, 64)
				if n >= bounds.lo && n <= bounds.hi {
					kept = append(kept, id)
				}
			}
			src.ids = kept
		}
		d.sources = append(d.sources, src)
	}
	if err := s.checkKeys(ctx, m, false); err != nil {
		return nil, err
	}
	return d, nil
}

// The LIMIT 0 count probe and complete LIMIT n fetch use no sorting, numeric
// row-ID bounds, cursor, or offset traversal. Unknown/ACL/transport failures
// fail closed rather than being reinterpreted as lack of capability.
func completeSnapshotSearch(ctx context.Context, s *store, seg snapshotSegment, query string, n int) (any, error) {
	node, err := s.search(ctx, seg.Index)
	if err != nil {
		return nil, err
	}
	source, ok := node.c.(interface{ Pipeline() goredis.Pipeliner })
	if !ok {
		return nil, errorf(adbc.StatusNotImplemented, "complete snapshot candidates require a pipeline-capable primary")
	}
	pipe := source.Pipeline()
	before := goredis.NewCmd(ctx, "FT.INFO", seg.Index)
	search := goredis.NewCmd(ctx, "FT.SEARCH", seg.Index, query, "NOCONTENT", "LIMIT", 0, n, "TIMEOUT", 0, "DIALECT", 2)
	after := goredis.NewCmd(ctx, "FT.INFO", seg.Index)
	for _, cmd := range []*goredis.Cmd{before, search, after} {
		if err := pipe.Process(ctx, onceCmd{cmd}); err != nil {
			return nil, err
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		// Only a direct native result-limit refusal, with both index proofs
		// confirmed, permits an ordinary directory fallback. Composed errors
		// may contain reply loss even when a native error is among their causes.
		if completeSnapshotLimit(err) && completeSnapshotLimit(search.Err()) && before.Err() == nil && after.Err() == nil {
			if proof := checkSnapshotPageIndex(before.Val(), seg); proof != nil {
				return nil, proof
			}
			if proof := checkSnapshotPageIndex(after.Val(), seg); proof != nil {
				return nil, proof
			}
			return nil, errCompleteSnapshotLimit
		}
		return nil, wrapRedis(err, "failed complete snapshot candidate read")
	}
	for _, cmd := range []*goredis.Cmd{before, search, after} {
		if cmd.Err() != nil {
			return nil, wrapRedis(cmd.Err(), "failed complete snapshot candidate read")
		}
	}
	if err := checkSnapshotPageIndex(before.Val(), seg); err != nil {
		return nil, err
	}
	if err := checkSnapshotPageIndex(after.Val(), seg); err != nil {
		return nil, err
	}
	return search.Val(), nil
}

func completeSnapshotIDs(reply any, seg snapshotSegment, expected int64) ([]string, error) {
	if snapshotReplyWarning(reply) {
		return nil, errorf(adbc.StatusIO, "partial complete snapshot candidate reply")
	}
	count, err := parseSearchCount(reply)
	if err != nil {
		return nil, err
	}
	keys, err := parseSearchKeys(reply)
	if err != nil {
		return nil, err
	}
	if expected < 0 || expected > snapshotFlexCandidateCap || count != expected || count != int64(len(keys)) {
		return nil, errorf(adbc.StatusIO, "incomplete snapshot candidate materialization")
	}
	ids := make([]string, len(keys))
	seen := make(map[string]bool, len(keys))
	for i, key := range keys {
		suffix, ok := strings.CutPrefix(key, seg.Prefix)
		n, err := strconv.ParseInt(suffix, 10, 64)
		if !ok || err != nil || n < 1 || strconv.FormatInt(n, 10) != suffix || seen[key] {
			return nil, errorf(adbc.StatusIO, "invalid complete snapshot candidate identity")
		}
		seen[key] = true
		ids[i] = fmt.Sprintf("%020d", n)
	}
	sort.Strings(ids)
	return ids, nil
}

func snapshotDirectoryZero(score any) bool {
	if score == nil {
		return false
	}
	n, err := strconv.ParseFloat(fmt.Sprint(score), 64)
	return err == nil && n == 0
}

func checkCompleteSnapshotDirectory(ctx context.Context, s *store, seg snapshotSegment, ids []string) error {
	for start := 0; start < len(ids) || start == 0; start += pipelineChunk {
		end := min(start+pipelineChunk, len(ids))
		pipe := s.redis(ctx).Pipeline()
		count := pipe.ZCard(ctx, snapshotMembers(snapshotDataID(seg.Prefix)))
		var scores *goredis.Cmd
		if end > start {
			args := []any{"ZMSCORE", snapshotMembers(snapshotDataID(seg.Prefix))}
			for _, id := range ids[start:end] {
				args = append(args, id)
			}
			scores = pipe.Do(ctx, args...)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return wrapRedis(err, "failed complete snapshot directory proof")
		}
		if count.Err() != nil {
			return wrapRedis(count.Err(), "failed complete snapshot directory count")
		}
		if count.Val() != seg.Rows {
			return errorf(adbc.StatusIO, "published snapshot row directory changed")
		}
		if scores != nil {
			values, err := scores.Slice()
			if err != nil {
				return wrapRedis(err, "failed complete snapshot source membership")
			}
			if len(values) != end-start {
				return errorf(adbc.StatusIO, "incomplete snapshot source membership reply")
			}
			for _, score := range values {
				if !snapshotDirectoryZero(score) {
					return errorf(adbc.StatusIO, "Search candidate is absent from its exact source directory")
				}
			}
		}
		if len(ids) == 0 {
			break
		}
	}
	return nil
}
