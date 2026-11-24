// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// snapshotPageFilter uses Search only as a bounded candidate membership test
// after the authoritative directory merge. It does not enumerate visibility or
// replace the complete SQL residual. Compaction/private write scans never use it.
type snapshotPageFilter struct {
	store    *store
	meta     *tableMeta
	query    string
	segments []snapshotSegment
}

func newSnapshotPageFilter(ctx context.Context, s *store, m *tableMeta, query string) *snapshotPageFilter {
	if s == nil || !managedRowIDTable(m) || !s.flexSnapshotTable(m) ||
		m.Live || m.Unmanaged || !m.PlainIndex || len(m.PendingCleanup) != 0 ||
		m.SnapshotRows == nil || query == "" || query == "*" || len(query) > 1<<20 {
		return nil
	}
	scope := snapshotOf(ctx)
	if scope == nil || scope.pin == nil || validateSnapshotDescriptor(ctx, m) != nil {
		return nil
	}
	segments := m.snapshotSegments()
	for _, seg := range segments {
		if snapshotOwns(ctx, seg.Prefix) {
			return nil
		}
	}
	return &snapshotPageFilter{store: s, meta: m, query: query, segments: segments}
}

func (f *snapshotPageFilter) filter(ctx context.Context, logical, physical []string) ([]string, []string, error) {
	if err := f.store.checkKeys(ctx, f.meta, false); err != nil {
		return nil, nil, err
	}
	if len(logical) != len(physical) {
		return nil, nil, errorf(adbc.StatusIO, "invalid snapshot candidate page")
	}
	groups := make([][]string, len(f.segments))
	seen := make(map[string]bool, len(physical))
	// Validate all winners before Search, including nonmatches and tombstones.
	// A missing/corrupt authoritative HASH must never become a silent omission.
	for i, key := range physical {
		id, ok := strings.CutPrefix(logical[i], f.meta.prefix())
		n, err := strconv.ParseInt(id, 10, 64)
		if !ok || err != nil || n < 1 || strconv.FormatInt(n, 10) != id || seen[key] {
			return nil, nil, errorf(adbc.StatusIO, "invalid snapshot candidate identity")
		}
		found := false
		for _, seg := range f.segments {
			if key == seg.Prefix+id {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, errorf(adbc.StatusIO, "snapshot candidate outside published sources")
		}
		seen[key] = true
	}
	rows, err := f.controls(ctx, physical)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) != len(physical) {
		return nil, nil, errorf(adbc.StatusIO, "incomplete snapshot control page")
	}
	for i, row := range rows {
		if row == nil || row[rowIDField] != strings.TrimPrefix(logical[i], f.meta.prefix()) {
			return nil, nil, errorf(adbc.StatusIO, "snapshot row identity does not match its directory")
		}
		if row[snapshotDeletedField] == "1" {
			continue
		}
		for j, seg := range f.segments {
			if strings.HasPrefix(physical[i], seg.Prefix) {
				groups[j] = append(groups[j], physical[i])
				break
			}
		}
	}
	matches := make(map[string]bool, len(physical))
	for i, keys := range groups {
		for start := 0; start < len(keys); start += snapshotDeltaPage {
			end := min(start+snapshotDeltaPage, len(keys))
			selected, err := f.candidates(ctx, f.segments[i], keys[start:end])
			if err != nil {
				return nil, nil, err
			}
			for key := range selected {
				matches[key] = true
			}
		}
	}
	if err := f.store.checkKeys(ctx, f.meta, false); err != nil {
		return nil, nil, err
	}
	// FT.SEARCH ordering is irrelevant. Preserve the directory's physical order.
	outLogical, outPhysical := logical[:0], physical[:0]
	for i, key := range physical {
		if matches[key] {
			outLogical = append(outLogical, logical[i])
			outPhysical = append(outPhysical, key)
		}
	}
	return outLogical, outPhysical, nil
}

// Do not use a missing-reply repair path here: even a pipeline error joined
// with redis.Nil can also contain reply loss and must stop the candidate read.
func (f *snapshotPageFilter) controls(ctx context.Context, keys []string) ([]aggRow, error) {
	out := make([]aggRow, len(keys))
	for start := 0; start < len(keys); start += pipelineChunk {
		end := min(start+pipelineChunk, len(keys))
		pipe := f.store.redis(ctx).Pipeline()
		cmds := queueHashFetchCommands(ctx, pipe, keys[start:end], []string{rowIDField, snapshotDeletedField})
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, wrapRedis(err, "failed snapshot candidate control read")
		}
		for i, cmd := range cmds {
			values, err := cmd.Result()
			if err != nil {
				return nil, wrapRedis(err, "failed snapshot candidate control read")
			}
			if len(values) != 2 {
				return nil, errorf(adbc.StatusIO, "incomplete snapshot candidate control reply")
			}
			id, ok := values[0].(string)
			if !ok {
				return nil, errorf(adbc.StatusIO, "missing snapshot candidate control identity")
			}
			row := aggRow{rowIDField: id}
			if values[1] != nil {
				deleted, ok := values[1].(string)
				if !ok {
					return nil, errorf(adbc.StatusIO, "invalid snapshot candidate deletion marker")
				}
				row[snapshotDeletedField] = deleted
			}
			out[start+i] = row
		}
	}
	return out, nil
}

func (f *snapshotPageFilter) candidates(ctx context.Context, seg snapshotSegment, keys []string) (map[string]bool, error) {
	node, err := f.store.search(ctx, seg.Index)
	if err != nil {
		return nil, err
	}
	source, ok := node.c.(interface{ Pipeline() goredis.Pipeliner })
	if !ok {
		return nil, errorf(adbc.StatusNotImplemented, "bounded snapshot candidate pipeline unavailable")
	}
	pipe := source.Pipeline()
	before := goredis.NewCmd(ctx, "FT.INFO", seg.Index)
	args := []any{"FT.SEARCH", seg.Index, f.query, "INKEYS", len(keys)}
	for _, key := range keys {
		args = append(args, key)
	}
	args = append(args, "NOCONTENT", "LIMIT", 0, len(keys), "TIMEOUT", 0, "DIALECT", 2)
	search := goredis.NewCmd(ctx, args...)
	after := goredis.NewCmd(ctx, "FT.INFO", seg.Index)
	for _, cmd := range []*goredis.Cmd{before, search, after} {
		if err := pipe.Process(ctx, onceCmd{cmd}); err != nil {
			return nil, err
		}
	}
	_, execErr := pipe.Exec(ctx)
	// Unknown/composed transport failures and ACL refusals are never fallback.
	if execErr != nil {
		return nil, wrapRedis(execErr, "failed bounded snapshot candidates")
	}
	for _, cmd := range []*goredis.Cmd{before, search, after} {
		if cmd.Err() != nil {
			return nil, wrapRedis(cmd.Err(), "failed bounded snapshot candidates")
		}
	}
	if err := checkSnapshotPageIndex(before.Val(), seg); err != nil {
		return nil, err
	}
	if err := checkSnapshotPageIndex(after.Val(), seg); err != nil {
		return nil, err
	}
	return snapshotPageMembership(search.Val(), keys)
}

func checkSnapshotPageIndex(reply any, seg snapshotSegment) error {
	if snapshotReplyWarning(reply) {
		return errorf(adbc.StatusIO, "warning in snapshot candidate index proof")
	}
	if err := checkRangeReadinessReply(reply); err != nil {
		return err
	}
	info := indexInfoOf(reply)
	fields, ok := redisReplyFields(reply)
	docs, docsOK := redisReplyInt64(fields["num_docs"])
	failures, failuresOK := redisReplyInt64(fields["hash_indexing_failures"])
	if !ok || !docsOK || !failuresOK || docs != seg.Rows || failures != 0 ||
		!strings.EqualFold(info.keyType, "HASH") || info.filter != "" || len(info.prefixes) != 1 || info.prefixes[0] != seg.Prefix {
		return errorf(adbc.StatusIO, "snapshot candidate index identity or completeness changed")
	}
	return nil
}

func snapshotPageMembership(reply any, keys []string) (map[string]bool, error) {
	if snapshotReplyWarning(reply) {
		return nil, errorf(adbc.StatusIO, "partial snapshot candidate reply")
	}
	count, err := parseSearchCount(reply)
	if err != nil {
		return nil, err
	}
	result, err := parseSearchKeys(reply)
	if err != nil {
		return nil, err
	}
	if count != int64(len(result)) || count > int64(len(keys)) {
		return nil, errorf(adbc.StatusIO, "incomplete bounded snapshot candidate reply")
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	out := make(map[string]bool, len(result))
	for _, key := range result {
		if !allowed[key] || out[key] {
			return nil, errorf(adbc.StatusIO, "invalid bounded snapshot candidate identity")
		}
		out[key] = true
	}
	return out, nil
}
