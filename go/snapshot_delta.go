// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotMaxSegments = 8
const snapshotDeletedField = "__adbc_snapshot_deleted"
const snapshotDeltaPage = 256

type snapshotSegment struct {
	Prefix string `json:"prefix"`
	Index  string `json:"index"`
	Rows   int64  `json:"rows"`
}

func snapshotMembers(id string) string { return snapshotPrefix + "members:" + id }
func (m *tableMeta) snapshotSegments() []snapshotSegment {
	out := append([]snapshotSegment(nil), m.SnapshotSegments...)
	n := int64(0)
	if m.SnapshotRows != nil {
		n = *m.SnapshotRows
	}
	return append(out, snapshotSegment{Prefix: m.prefix(), Index: m.index(), Rows: n})
}
func snapshotDirectoryMember(key string) string {
	id := snapshotDataID(key)
	if id == "" {
		return ""
	}
	suffix, ok := strings.CutPrefix(key, snapshotRowPrefix(key))
	if !ok {
		return ""
	}
	n, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != suffix {
		return ""
	}
	return fmt.Sprintf("%020d", n)
}

// Each source is an immutable sorted row-ID directory. Equal IDs from every
// segment are advanced together; the newest wins, including tombstones. Only
// winning HASHes are fetched, and predicates run after this merge.
type snapshotDeltaSource struct {
	segment snapshotSegment
	offset  int64
	ids     []string
	at      int
	done    bool
}
type snapshotDeltaScan struct {
	store       *store
	meta        *tableMeta
	sources     []snapshotDeltaSource
	fields      []string
	last        string
	pageRows    int
	through     string
	keepDeleted bool // partial compaction must shadow preserved older segments
}

func newSnapshotDeltaScan(s *store, m *tableMeta, fields []string) *snapshotDeltaScan {
	d := &snapshotDeltaScan{store: s, meta: m, fields: fields}
	for _, seg := range m.snapshotSegments() {
		d.sources = append(d.sources, snapshotDeltaSource{segment: seg})
	}
	return d
}

// Seek each exact directory to a live partition's lower bound. Directories
// have score zero and lexically ordered decimal IDs, so no floating point
// interval or repeated full-table scan is needed.
func (d *snapshotDeltaScan) seek(ctx context.Context, lo, hi int64) error {
	d.through = fmt.Sprintf("%020d", hi)
	for i := range d.sources {
		n, err := d.store.redis(ctx).ZLexCount(ctx, snapshotMembers(snapshotDataID(d.sources[i].segment.Prefix)), "-", "("+fmt.Sprintf("%020d", lo)).Result()
		if err != nil {
			return wrapRedis(err, "failed to seek snapshot partition directory")
		}
		d.sources[i].offset = n
	}
	return nil
}
func (d *snapshotDeltaScan) head(ctx context.Context, i int) (string, error) {
	src := &d.sources[i]
	if src.at == len(src.ids) && !src.done {
		// A checkpointed compaction discards this scanner after one batch.
		// Respect its smaller page bound instead of repeatedly fetching and
		// throwing away directory entries beyond that checkpoint.
		page := snapshotDeltaPage
		if d.pageRows > 0 {
			page = min(page, d.pageRows)
		}
		ids, err := d.store.redis(ctx).ZRange(ctx, snapshotMembers(snapshotDataID(src.segment.Prefix)), src.offset, src.offset+int64(page)-1).Result()
		if err != nil {
			return "", wrapRedis(err, "failed to read snapshot row directory")
		}
		src.ids, src.at = ids, 0
		src.offset += int64(len(ids))
		src.done = len(ids) < page
		if src.done && !snapshotOwns(ctx, src.segment.Prefix) && src.offset != src.segment.Rows {
			return "", errorf(adbc.StatusIO, "published snapshot row directory changed")
		}
	}
	if src.at == len(src.ids) {
		return "", nil
	}
	id := src.ids[src.at]
	if len(id) != 20 {
		return "", errorf(adbc.StatusIO, "invalid snapshot row directory entry")
	}
	return id, nil
}
func (d *snapshotDeltaScan) nextKeys(ctx context.Context) ([]string, []string, error) {
	var logical, physical []string
	limit := d.pageRows
	if limit == 0 {
		limit = snapshotDeltaPage
	}
	for len(logical) < limit {
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
		if smallest == "" || d.through != "" && smallest > d.through {
			break
		}
		winner := -1
		for i, id := range heads {
			if id == smallest {
				winner = i
				d.sources[i].at++
			}
		}
		n, err := strconv.ParseInt(smallest, 10, 64)
		if err != nil || n < 1 || fmt.Sprintf("%020d", n) != smallest {
			return nil, nil, errorf(adbc.StatusIO, "invalid snapshot row identity")
		}
		d.last = smallest
		id := strconv.FormatInt(n, 10)
		logical = append(logical, d.meta.prefix()+id)
		physical = append(physical, d.sources[winner].segment.Prefix+id)
	}
	if len(logical) == 0 {
		return nil, nil, io.EOF
	}
	return logical, physical, nil
}

func (d *snapshotDeltaScan) next(ctx context.Context) ([]string, []aggRow, error) {
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
		if row == nil {
			return nil, nil, errorf(adbc.StatusIO, "snapshot directory references a missing row")
		}
		if row[rowIDField] != strings.TrimPrefix(logical[i], d.meta.prefix()) {
			return nil, nil, errorf(adbc.StatusIO, "snapshot row identity does not match its directory")
		}
		if row[snapshotDeletedField] == "1" && !d.keepDeleted {
			continue
		}
		keys = append(keys, logical[i])
		out = append(out, row)
	}
	return keys, out, nil
}
func (s *store) fetchSnapshotPhysical(ctx context.Context, keys, fields []string) ([]aggRow, error) {
	if fields == nil {
		out := make([]aggRow, len(keys))
		p := s.redis(ctx).Pipeline()
		cmds := make([]*goredis.MapStringStringCmd, len(keys))
		for i, key := range keys {
			cmds[i] = p.HGetAll(ctx, key)
		}
		if _, err := p.Exec(ctx); err != nil {
			return nil, wrapRedis(err, "failed to read snapshot row images")
		}
		for i, cmd := range cmds {
			v := cmd.Val()
			if len(v) > 0 {
				out[i] = aggRow(v)
			}
		}
		return out, nil
	}
	fields = append(append([]string(nil), fields...), snapshotDeletedField)
	return s.fetchRows(ctx, keys, fields)
}

// Direct row-ID lookups preserve exact IDs and resolve newest-to-oldest without
// enumerating other rows. Tombstones stop lookup just like a live replacement.
func (s *store) fetchSnapshotVisible(ctx context.Context, m *tableMeta, keys, fields []string) ([]aggRow, error) {
	strict, _ := ctx.Value(snapshotDirectReadKey{}).(bool)
	out := make([]aggRow, len(keys))
	resolved := make([]bool, len(keys))
	segments := m.snapshotSegments()
	for seg := len(segments) - 1; seg >= 0; seg-- {
		var physical []string
		var slots []int
		for i, key := range keys {
			if !resolved[i] {
				suffix, ok := strings.CutPrefix(key, m.prefix())
				if !ok {
					return nil, errorf(adbc.StatusIO, "invalid logical snapshot key")
				}
				physical = append(physical, segments[seg].Prefix+suffix)
				slots = append(slots, i)
			}
		}
		if len(slots) == 0 {
			break
		}
		rows, err := s.fetchSnapshotPhysical(ctx, physical, fields)
		if err != nil {
			return nil, err
		}
		var members []bool
		if strict {
			members, err = s.snapshotDirectMembers(ctx, segments[seg], physical)
			if err != nil {
				return nil, err
			}
		}
		for j, row := range rows {
			if strict {
				if row == nil && members[j] {
					return nil, errorf(adbc.StatusIO, "snapshot directory references a missing row")
				}
				if row != nil && !members[j] {
					return nil, errorf(adbc.StatusIO, "direct snapshot HASH is not a registered row")
				}
				if row == nil {
					continue
				}
			} else if row == nil {
				if !snapshotOwns(ctx, segments[seg].Prefix) {
					count, err := s.redis(ctx).ZCard(ctx, snapshotMembers(snapshotDataID(segments[seg].Prefix))).Result()
					if err != nil {
						return nil, wrapRedis(err, "failed to verify snapshot row directory")
					}
					if count != segments[seg].Rows {
						return nil, errorf(adbc.StatusIO, "published snapshot row directory changed")
					}
				}
				_, err := s.redis(ctx).ZScore(ctx, snapshotMembers(snapshotDataID(segments[seg].Prefix)), snapshotDirectoryMember(physical[j])).Result()
				if err == nil {
					return nil, errorf(adbc.StatusIO, "snapshot directory references a missing row")
				}
				if !errors.Is(err, goredis.Nil) {
					return nil, wrapRedis(err, "failed to verify snapshot row directory")
				}
			}
			if row != nil {
				if row[rowIDField] != strings.TrimPrefix(physical[j], segments[seg].Prefix) {
					return nil, errorf(adbc.StatusIO, "snapshot row identity does not match its key")
				}
				i := slots[j]
				resolved[i] = true
				if row[snapshotDeletedField] != "1" {
					out[i] = row
				}
			}
		}
	}
	return out, nil
}

func (e *executor) writeSnapshotUpdates(ctx context.Context, m *tableMeta, changes []rowChange) error {
	keys := make([]string, len(changes))
	for i, ch := range changes {
		keys[i] = ch.key
	}
	for start := 0; start < len(keys); start += pipelineChunk {
		end := min(start+pipelineChunk, len(keys))
		rows, err := e.store.fetchSnapshotVisible(ctx, m, keys[start:end], nil)
		if err != nil {
			return err
		}
		images := make([]snapshotInsertRow, 0, len(rows))
		for i, row := range rows {
			if row == nil {
				return errorf(adbc.StatusIO, "snapshot UPDATE target disappeared")
			}
			ch := changes[start+i]
			for _, f := range ch.del {
				delete(row, f)
			}
			for j := 0; j < len(ch.set); j += 2 {
				row[fmt.Sprint(ch.set[j])] = fmt.Sprint(ch.set[j+1])
			}
			delete(row, snapshotDeletedField)
			fields := []any{rowIDField, row[rowIDField]}
			for field, value := range row {
				if field != rowIDField {
					fields = append(fields, field, value)
				}
			}
			images = append(images, snapshotInsertRow{key: ch.key, member: snapshotDirectoryMember(ch.key), fields: fields})
		}
		if err := e.store.writeSnapshotImages(ctx, m.prefix(), images, true); err != nil {
			return wrapRedis(err, "failed to stage snapshot replacements")
		}
		if testHookRowsWritten != nil {
			testHookRowsWritten(m, end)
		}
	}
	return e.store.checkWritten(ctx, m)
}
func (e *executor) writeSnapshotDeletes(ctx context.Context, m *tableMeta, keys []string) error {
	for start := 0; start < len(keys); start += pipelineChunk {
		images := make([]snapshotInsertRow, 0, min(pipelineChunk, len(keys)-start))
		for _, key := range keys[start:min(start+pipelineChunk, len(keys))] {
			id, ok := strings.CutPrefix(key, m.prefix())
			if !ok {
				return errorf(adbc.StatusIO, "invalid snapshot DELETE target")
			}
			images = append(images, snapshotInsertRow{key: key, member: snapshotDirectoryMember(key), fields: []any{rowIDField, id, snapshotDeletedField, "1"}})
		}
		if err := e.store.writeSnapshotImages(ctx, m.prefix(), images, true); err != nil {
			return wrapRedis(err, "failed to stage snapshot tombstones")
		}
		if testHookRowsWritten != nil {
			testHookRowsWritten(m, min(start+pipelineChunk, len(keys)))
		}
	}
	return e.store.checkWritten(ctx, m)
}

// Ordinary writes allocate one empty delta. Compaction runs separately; the
// hard segment bound applies backpressure without copying table rows here.
func (s *store) prepareSnapshotDelta(ctx context.Context, schema, table string) error {
	old, err := s.getTable(ctx, schema, table)
	if err != nil {
		return err
	}
	if snapshotOwns(ctx, old.prefix()) {
		return nil
	}
	if len(old.snapshotSegments()) >= snapshotMaxSegments {
		s.wakeSnapshotCompactor()
		return stateErrorf("40001", "snapshot segment limit reached; background compaction is pending; retry the statement")
	}
	nm, err := s.claimNames(ctx, schema, table)
	if err != nil {
		return err
	}
	next := *old
	next.KeyPrefix, next.IndexName, next.PrefixGen = nm.prefix, nm.index, 0
	next.SnapshotRows = nil
	segments := old.snapshotSegments()
	// Empty initial tables need no history or overlay decoding.
	if len(old.SnapshotSegments) == 0 && old.SnapshotRows != nil && *old.SnapshotRows == 0 && !old.SnapshotOverlay {
		next.SnapshotSegments = nil
		next.SnapshotOverlay = false
	} else {
		next.SnapshotSegments = segments
		next.SnapshotOverlay = true
	}

	deferSnapshotIndex(ctx, &next)
	raw, err := json.Marshal(&next)
	if err != nil {
		return err
	}
	p := s.redis(ctx).Pipeline()
	p.Set(ctx, metaKey(schema, table), raw, 0)
	if _, err := p.Exec(ctx); err != nil {
		return err
	}
	return nil
}
