// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// A published segment's index supplies candidates, never logical visibility.
// Keyset pages keep server work/results bounded without a mutable cursor or
// deep OFFSET. Newer exact directories suppress old candidates even when their
// replacement/tombstone did not match the pushed predicate.
type snapshotCandidateSource struct {
	segment snapshotSegment
	ids     []string
	at      int
	after   int64
	done    bool
}
type snapshotCandidateScan struct {
	store   *store
	meta    *tableMeta
	sources []snapshotCandidateSource
	query   string
	fields  []string
	page    int
}

// Numeric keyset bounds are exact only below 2^53. A nonempty private segment
// has unsettled incremental indexes and must use authoritative directories.
// Empty private tails are safe and skipped, keeping selective UPDATE/DELETE
// discovery fast before their first mutation.
func (e *executor) snapshotIndexable(ctx context.Context, m *tableMeta) bool {
	if e.store == nil || !m.SnapshotOverlay {
		return false
	}
	for _, seg := range m.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			n, err := e.store.redis(ctx).ZCard(ctx, snapshotMembers(snapshotDataID(seg.Prefix))).Result()
			if err != nil || n != 0 {
				return false
			}
		}
	}
	n, err := e.store.redis(ctx).Get(ctx, seqKey(m.Schema, m.Name)).Int64()
	return err == nil && n >= 0 && n < exactDoubleLimit
}
func newSnapshotCandidateScan(ctx context.Context, s *store, m *tableMeta, query string, fields []string) *snapshotCandidateScan {
	cap := s.paginationCaps(ctx, snapshotSourceIndex(ctx, m))[0]
	d := &snapshotCandidateScan{store: s, meta: m, query: query, fields: fields, page: int(min(int64(snapshotDeltaPage), cap))}
	for _, seg := range m.snapshotSegments() {
		d.sources = append(d.sources, snapshotCandidateSource{segment: seg, done: snapshotOwns(ctx, seg.Prefix)})
	}
	return d
}
func (d *snapshotCandidateScan) head(ctx context.Context, i int) (string, error) {
	src := &d.sources[i]
	if src.at == len(src.ids) && !src.done {
		query := d.query
		if src.after > 0 {
			query = fmt.Sprintf("(%s) @%s:[(%d +inf]", query, rowIDField, src.after)
		}
		reply, err := d.store.searchDo(ctx, src.segment.Index, "FT.SEARCH", src.segment.Index, query, "NOCONTENT", "SORTBY", rowIDField, "ASC", "LIMIT", 0, d.page, "TIMEOUT", 0, "DIALECT", 2).Result()
		if err != nil {
			return "", wrapRedis(err, "failed to read snapshot index candidates")
		}
		if snapshotReplyWarning(reply) {
			return "", errorf(adbc.StatusIO, "snapshot candidate query returned a partial-result warning")
		}
		keys, err := parseSearchKeys(reply)
		if err != nil {
			return "", err
		}
		src.ids = make([]string, len(keys))
		src.at = 0
		src.done = len(keys) < d.page
		for j, key := range keys {
			suffix, ok := strings.CutPrefix(key, src.segment.Prefix)
			n, err := strconv.ParseInt(suffix, 10, 64)
			if !ok || err != nil || n <= src.after || n >= exactDoubleLimit || strconv.FormatInt(n, 10) != suffix {
				return "", errorf(adbc.StatusIO, "snapshot candidate row order or identity is invalid")
			}
			src.ids[j] = fmt.Sprintf("%020d", n)
			src.after = n
		}
	}
	if src.at == len(src.ids) {
		return "", nil
	}
	return src.ids[src.at], nil
}
func (d *snapshotCandidateScan) verify(ctx context.Context) error {
	pipe := d.store.redis(ctx).Pipeline()
	var cmds []*goredis.IntCmd
	for _, src := range d.sources {
		if !snapshotOwns(ctx, src.segment.Prefix) {
			cmds = append(cmds, pipe.ZCard(ctx, snapshotMembers(snapshotDataID(src.segment.Prefix))))
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	j := 0
	for _, src := range d.sources {
		if !snapshotOwns(ctx, src.segment.Prefix) {
			if cmds[j].Val() != src.segment.Rows {
				return errorf(adbc.StatusIO, "published snapshot row directory changed")
			}
			j++
		}
	}
	return nil
}
func (d *snapshotCandidateScan) nextKeys(ctx context.Context) ([]string, []string, error) {
	var ids []string
	var winners []int
	for len(ids) < snapshotDeltaPage {
		if err := cancelled(ctx); err != nil {
			return nil, nil, err
		}
		smallest := ""
		heads := make([]string, len(d.sources))
		for i := range d.sources {
			id, err := d.head(ctx, i)
			if err != nil {
				return nil, nil, err
			}
			heads[i] = id
			if id != "" && (smallest == "" || id < smallest) {
				smallest = id
			}
		}
		if smallest == "" {
			break
		}
		winner := -1
		for i, id := range heads {
			if id == smallest {
				winner = i
				d.sources[i].at++
			}
		}
		ids = append(ids, smallest)
		winners = append(winners, winner)
	}
	if len(ids) == 0 {
		if err := d.verify(ctx); err != nil {
			return nil, nil, err
		}
		return nil, nil, io.EOF
	}
	// Batch exact newer-directory probes. Missing ZMSCORE replies are nil;
	// score zero is membership, never absence.
	actual := append([]int(nil), winners...)
	pipe := d.store.redis(ctx).Pipeline()
	probes := make([]*goredis.Cmd, len(d.sources))
	for seg := 1; seg < len(d.sources); seg++ {
		args := []any{"ZMSCORE", snapshotMembers(snapshotDataID(d.sources[seg].segment.Prefix))}
		for _, id := range ids {
			args = append(args, id)
		}
		probes[seg] = pipe.Do(ctx, args...)
	}
	if len(d.sources) > 1 {
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, nil, wrapRedis(err, "failed to resolve snapshot candidate visibility")
		}
	}
	for seg := 1; seg < len(d.sources); seg++ {
		scores, err := probes[seg].Slice()
		if err != nil || len(scores) != len(ids) {
			return nil, nil, errorf(adbc.StatusIO, "invalid snapshot visibility reply")
		}
		for i, score := range scores {
			if score != nil && seg > actual[i] {
				actual[i] = seg
			}
		}
	}
	var logical, physical []string
	// Even a shadowed match must not hide a missing newest HASH. Validate its
	// authoritative ID without transferring fields that failed the query.
	check := d.store.redis(ctx).Pipeline()
	var headers []*goredis.StringCmd
	var expected []string
	for i, winner := range winners {
		n, err := strconv.ParseInt(ids[i], 10, 64)
		if err != nil {
			return nil, nil, err
		}
		id := strconv.FormatInt(n, 10)
		if actual[i] != winner {
			headers = append(headers, check.HGet(ctx, d.sources[actual[i]].segment.Prefix+id, rowIDField))
			expected = append(expected, id)
			continue
		}
		logical = append(logical, d.meta.prefix()+id)
		physical = append(physical, d.sources[winner].segment.Prefix+id)
	}
	if len(headers) > 0 {
		if _, err := check.Exec(ctx); err != nil {
			return nil, nil, wrapRedis(err, "newest snapshot candidate row is unavailable")
		}
		for i, cmd := range headers {
			if cmd.Val() != expected[i] {
				return nil, nil, errorf(adbc.StatusIO, "newest snapshot candidate row identity changed")
			}
		}
	}
	if len(physical) == 0 {
		return nil, nil, nil
	}
	return logical, physical, nil
}

func (d *snapshotCandidateScan) next(ctx context.Context) ([]string, []aggRow, error) {
	logical, physical, err := d.nextKeys(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := d.store.fetchSnapshotPhysical(ctx, physical, d.fields)
	if err != nil {
		return nil, nil, err
	}
	keys := logical[:0]
	out := rows[:0]
	for i, row := range rows {
		if row == nil || row[rowIDField] != strings.TrimPrefix(logical[i], d.meta.prefix()) {
			return nil, nil, errorf(adbc.StatusIO, "snapshot candidate references a missing or invalid row")
		}
		if row[snapshotDeletedField] == "1" {
			continue
		}
		keys = append(keys, logical[i])
		out = append(out, row)
	}
	return keys, out, nil
}

// Counts are planning upper bounds: old matching versions can overestimate,
// while every visible match must occur in at least one physical index. Actual
// join/semi-join reads still resolve visibility through the ordinary scan path.
func (s *store) countSnapshotCandidates(ctx context.Context, m *tableMeta, query string) (int64, error) {
	var count int64
	for _, seg := range m.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			continue
		}
		n, err := s.countMatches(ctx, seg.Index, query)
		if err != nil {
			return 0, err
		}
		if n < 0 || count > int64(^uint64(0)>>1)-n {
			return 0, errorf(adbc.StatusIO, "snapshot candidate count overflow")
		}
		count += n
	}
	return count, nil
}
