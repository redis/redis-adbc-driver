// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// A published segment's index supplies candidates, never logical visibility.
// Keyset pages keep server work/results bounded without a mutable cursor or
// deep OFFSET. Newer exact directories suppress old candidates even when their
// replacement/tombstone did not match the pushed predicate.
//
// Search evaluates an open-ended `@__rowid:[(after +inf]` page against every
// remaining matching document before selecting the lowest row IDs, so open
// keyset paging costs O(rows²/page) over a whole scan (#599: at SF1 a single
// open-ended page over 6M documents took 11.7 s, so a 1.5M-row scan through
// adbc_scanner could not finish). Pages instead read bounded row-ID windows
// (after, after+width] up to the table's row sequence, then one open-ended
// tail page past it proves that no later candidate exists. A window costs
// about the rows it spans, so width follows the observed density: a full page
// sets it to the span that page needed, and short windows double it. Search
// drives a selective intersection from its smaller iterator, so sparse windows
// stay cheap while they widen.
type snapshotCandidateSource struct {
	segment snapshotSegment
	ids     []string
	at      int
	after   int64
	done    bool
	// width is the current row-ID window (0: open-ended keyset pages).
	width int64
	// tried is set once a large union's complete read was attempted.
	tried bool
}

// snapshotCandidateWindowPages is the initial window width in pages.
const snapshotCandidateWindowPages = 16

// snapshotCandidateMaxWindow bounds window growth below the exact-double range.
const snapshotCandidateMaxWindow = int64(1) << 40

// A pushed query with at least snapshotUnionCompleteTerms union alternatives
// reads each source's candidates completely, in at most two unsorted Search
// queries, instead of in row-ID windows (#631). Search's cost for a union
// grows with its term count (about 40 µs per term on Software 8.8), and every
// window evaluates the whole union again, so a 1,000-key lookup paged through
// twenty windows cost 8–9× a range that returns as many rows.
const snapshotUnionCompleteTerms = 64

// The complete read is bounded like Flex's (snapshotFlexCandidateCap): a
// union with more candidates keeps row-ID windows.
const snapshotUnionCandidateCap = snapshotFlexCandidateCap
const snapshotUnionCandidateBytes = snapshotFlexCandidateBytes

// unionAlternatives counts a pushed query's union alternatives beyond the
// first of each union: numeric `(@c:[a a] | @c:[b b])` and TAG `@c:{a | b}`
// separate them with " | ", which escaped TAG text never contains (spaces
// are escaped). It only selects how candidates are read.
func unionAlternatives(query string) int {
	return strings.Count(query, " | ")
}

type snapshotCandidateScan struct {
	store        *store
	meta         *tableMeta
	sources      []snapshotCandidateSource
	query        string
	fields       []string
	page         int
	completeFlex bool // prefetched complete plain Search candidates; exact int64 identities
	// completeUnion reads each source's candidates completely on first use
	// (see snapshotUnionCompleteTerms); cap is the Search result cap.
	completeUnion bool
	cap           int64
	// bound is an upper bound of the sources' row IDs (the table's row
	// sequence, read once). Windows end there; 0 keeps open-ended pages.
	bound int64
}

// Numeric keyset bounds are exact only below 2^53. A nonempty private segment
// has unsettled incremental indexes and must use authoritative directories.
// Empty private tails are safe and skipped, keeping selective UPDATE/DELETE
// discovery fast before their first mutation.
func (e *executor) snapshotIndexable(ctx context.Context, m *tableMeta) bool {
	return e.store != nil && !e.store.flexEnabled() && e.snapshotSearchable(ctx, m)
}

// snapshotSearchable is snapshotIndexable without its Flex exclusion, which
// only sorted Search candidate reads need (see snapshotCountQueries).
func (e *executor) snapshotSearchable(ctx context.Context, m *tableMeta) bool {
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
func newSnapshotCandidateScan(ctx context.Context, s *store, m *tableMeta, query string, fields []string, pageRows int) *snapshotCandidateScan {
	cap := s.paginationCaps(ctx, snapshotSourceIndex(ctx, m))[0]
	d := &snapshotCandidateScan{store: s, meta: m, query: query, fields: fields, page: int(min(int64(pageRows), cap)), cap: cap}
	d.completeUnion = d.page > 0 && unionAlternatives(query) >= snapshotUnionCompleteTerms
	// The pinned published segments' row IDs were allocated before this read,
	// so the current sequence bounds them. Any read failure keeps the original
	// open-ended pages; a stale bound only costs the final tail page.
	if d.page > 0 {
		if last, err := s.rowSequence(ctx, m); err == nil && last > 0 && last < exactDoubleLimit {
			d.bound = last
		}
	}
	for _, seg := range m.snapshotSegments() {
		src := snapshotCandidateSource{segment: seg, done: snapshotOwns(ctx, seg.Prefix)}
		if d.bound > 0 {
			src.width = int64(d.page) * snapshotCandidateWindowPages
		}
		d.sources = append(d.sources, src)
	}
	return d
}

// candidateQuery is the keyset page query after src.after: a bounded row-ID
// window below d.bound, else the open-ended tail. hi is the window's
// inclusive end, or 0 for an open-ended page.
func (d *snapshotCandidateScan) candidateQuery(src *snapshotCandidateSource) (query string, hi int64) {
	rangeTerm := ""
	if src.width > 0 && src.after < d.bound {
		hi = min(src.after+src.width, d.bound)
		rangeTerm = fmt.Sprintf("@%s:[(%d %d]", rowIDField, src.after, hi)
	} else if src.after > 0 {
		rangeTerm = fmt.Sprintf("@%s:[(%d +inf]", rowIDField, src.after)
	}
	switch {
	case rangeTerm == "":
		return d.query, 0
	case d.query == "*":
		return rangeTerm, hi
	}
	return fmt.Sprintf("(%s) %s", d.query, rangeTerm), hi
}

// readCompleteUnion reads every Search candidate of src in at most two
// unsorted queries: one page-sized read, and a complete one when more match
// (up to snapshotUnionCandidateCap). It reports false, leaving src
// unchanged, when the union has more candidates or its reply is not
// provably complete; row-ID windows then read src as before. Candidates get
// the windows' identity checks, and visibility, HASH identity and directory
// verification stay with the merge.
func (d *snapshotCandidateScan) readCompleteUnion(ctx context.Context, src *snapshotCandidateSource) (bool, error) {
	limit := int64(d.page)
	maxRows := min(d.cap, snapshotUnionCandidateCap, snapshotUnionCandidateBytes/int64(len(src.segment.Prefix)+20))
	for attempt := 0; attempt < 2; attempt++ {
		if err := cancelled(ctx); err != nil {
			return false, err
		}
		reply, err := d.store.searchDo(ctx, src.segment.Index, "FT.SEARCH", src.segment.Index, d.query, "NOCONTENT", "LIMIT", 0, limit, "TIMEOUT", 0, "DIALECT", 2).Result()
		if err != nil {
			return false, wrapRedis(err, "failed to read snapshot index candidates")
		}
		if snapshotReplyWarning(reply) {
			return false, errorf(adbc.StatusIO, "snapshot candidate query returned a partial-result warning")
		}
		total, err := parseSearchCount(reply)
		if err != nil {
			return false, err
		}
		if total > limit {
			if attempt > 0 || total > maxRows {
				return false, nil
			}
			limit = total
			continue
		}
		keys, err := parseSearchKeys(reply)
		if err != nil {
			return false, err
		}
		if int64(len(keys)) != total {
			return false, nil
		}
		ids := make([]string, len(keys))
		for j, key := range keys {
			suffix, ok := strings.CutPrefix(key, src.segment.Prefix)
			n, err := strconv.ParseInt(suffix, 10, 64)
			if !ok || err != nil || n < 1 || n >= exactDoubleLimit || strconv.FormatInt(n, 10) != suffix {
				return false, errorf(adbc.StatusIO, "snapshot candidate row order or identity is invalid")
			}
			ids[j] = fmt.Sprintf("%020d", n)
		}
		sort.Strings(ids)
		for j := 1; j < len(ids); j++ {
			if ids[j] == ids[j-1] {
				return false, errorf(adbc.StatusIO, "snapshot candidate row order or identity is invalid")
			}
		}
		src.ids, src.at, src.done = ids, 0, true
		return true, nil
	}
	return false, nil
}

func (d *snapshotCandidateScan) head(ctx context.Context, i int) (string, error) {
	src := &d.sources[i]
	if d.completeUnion && !src.tried && !src.done && src.at == len(src.ids) {
		src.tried = true
		if _, err := d.readCompleteUnion(ctx, src); err != nil {
			return "", err
		}
	}
	// An exhausted window that matched nothing reads the next one, so an empty
	// head still means the source is done for this page.
	for src.at == len(src.ids) && !src.done {
		if err := cancelled(ctx); err != nil {
			return "", err
		}
		query, hi := d.candidateQuery(src)
		lo := src.after
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
		for j, key := range keys {
			suffix, ok := strings.CutPrefix(key, src.segment.Prefix)
			n, err := strconv.ParseInt(suffix, 10, 64)
			if !ok || err != nil || n <= src.after || hi > 0 && n > hi || n >= exactDoubleLimit || strconv.FormatInt(n, 10) != suffix {
				return "", errorf(adbc.StatusIO, "snapshot candidate row order or identity is invalid")
			}
			src.ids[j] = fmt.Sprintf("%020d", n)
			src.after = n
		}
		switch {
		case d.page > 0 && len(keys) == d.page:
			// More candidates may follow inside the window (or the tail). The
			// next window spans what this full page needed.
			if hi > 0 {
				src.width = max(src.after-lo, int64(d.page))
			}
		case hi == 0:
			// A short open-ended page completes the source.
			src.done = true
		default:
			// A short window holds every candidate in (after, hi]. Continue at
			// its end; sparse windows widen. The tail page follows at d.bound.
			src.after = hi
			if len(keys) < d.page/2 {
				src.width = min(src.width*2, snapshotCandidateMaxWindow)
			}
		}
	}
	if src.at == len(src.ids) {
		return "", nil
	}
	return src.ids[src.at], nil
}

// split is a copy of a complete Flex candidate scan restricted to row IDs in
// [lo, hi] (zero-padded), for one parallel worker. Discovery and directory
// proofs stay the coordinator's; each copy owns its source positions and
// still resolves visibility, fetches rows and verifies directories itself.
func (d *snapshotCandidateScan) split(lo, hi string, fields []string) *snapshotCandidateScan {
	c := *d
	c.fields = fields
	c.sources = make([]snapshotCandidateSource, len(d.sources))
	for i, src := range d.sources {
		a := sort.SearchStrings(src.ids, lo)
		b := sort.Search(len(src.ids), func(j int) bool { return src.ids[j] > hi })
		src.ids, src.at = src.ids[a:b:b], 0
		c.sources[i] = src
	}
	return &c
}

// chunkBounds divides a complete Flex candidate scan's distinct row IDs, across
// every source, into ranges of at most rows IDs each.
func (d *snapshotCandidateScan) chunkBounds(rows int) [][2]string {
	var merged []string
	for _, src := range d.sources {
		merged = append(merged, src.ids...)
	}
	sort.Strings(merged)
	merged = slices.Compact(merged)
	var out [][2]string
	for start := 0; start < len(merged); start += rows {
		end := min(start+rows, len(merged))
		out = append(out, [2]string{merged[start], merged[end-1]})
	}
	return out
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
			if d.completeFlex && cmds[j].Err() != nil {
				return wrapRedis(cmds[j].Err(), "failed snapshot directory verification")
			}
			if cmds[j].Val() != src.segment.Rows {
				return errorf(adbc.StatusIO, "published snapshot row directory changed")
			}
			j++
		}
	}
	return nil
}
func (d *snapshotCandidateScan) nextKeys(ctx context.Context) ([]string, []string, error) {
	if d.completeFlex {
		if err := d.store.checkKeys(ctx, d.meta, false); err != nil {
			return nil, nil, err
		}
	}
	var ids []string
	var winners []int
	// Reuse merge scratch for the whole page, rather than allocating per row.
	heads := make([]string, len(d.sources))
	limit := d.page
	if limit == 0 { // Internal merge-only callers can supply prefetched heads.
		limit = snapshotDeltaPage
	}
	for len(ids) < limit {
		if err := cancelled(ctx); err != nil {
			return nil, nil, err
		}
		smallest := ""
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
			if d.completeFlex && score != nil && !snapshotDirectoryZero(score) {
				return nil, nil, errorf(adbc.StatusIO, "invalid snapshot directory membership score")
			}
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
		l, p := snapshotKeyPair(d.meta.prefix(), d.sources[winner].segment.Prefix, id)
		logical, physical = append(logical, l), append(physical, p)
	}
	if len(headers) > 0 {
		if _, err := check.Exec(ctx); err != nil {
			return nil, nil, wrapRedis(err, "newest snapshot candidate row is unavailable")
		}
		for i, cmd := range headers {
			if cmd.Err() != nil {
				return nil, nil, wrapRedis(cmd.Err(), "newest snapshot candidate row is unavailable")
			}
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
