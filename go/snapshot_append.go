// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func snapshotParallelAppendEligible(m *tableMeta) bool {
	return m != nil && m.KeyPrefix != "" && !m.Unmanaged && !m.Live && !m.Adopted && !m.KeyRows && m.MaterializedSQL == ""
}

func (s *store) snapshotAppendAdmission(ctx context.Context, m *tableMeta) (string, error) {
	seed, err := s.redis(ctx).Get(ctx, seqKey(m.Schema, m.Name)).Int64()
	if err != nil && err != goredis.Nil {
		return "", wrapRedis(err, "failed to read append row counter")
	}
	if seed < 0 {
		return "", errorf(adbc.StatusIO, "invalid append row counter")
	}
	raw, err := json.Marshal(map[string]any{"target": snapshotIngestAdmissionTarget(m), "seed": strconv.FormatInt(seed, 10), "segments": len(m.snapshotSegments())})
	return string(raw), err
}

// The shared high water lives only in active leased pins. Publication retains
// the maximum private counter; new peers seed from the current committed root.
// Retry only an exact CAS refusal. A lost allocation reply aborts this append;
// input and allocations are never replayed after an uncertain outcome.
func (s *store) allocateSnapshotAppendRows(ctx context.Context, m *tableMeta, count int) (int64, error) {
	scope := snapshotOf(ctx)
	seed, err := s.redis(ctx).Get(ctx, seqKey(m.Schema, m.Name)).Int64()
	if err != nil && err != goredis.Nil {
		return 0, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, cancelled(ctx)
		}
		limit := int64(math.MaxInt64)
		if s.flex {
			limit = exactDoubleLimit
		}
		if seed < 0 || seed > limit-int64(count) {
			return 0, errorf(adbc.StatusNotImplemented, "append row ID limit reached")
		}
		next := seed + int64(count)
		raw, _ := json.Marshal(map[string]string{"expected": strconv.FormatInt(seed, 10), "next": strconv.FormatInt(next, 10)})
		result, err := s.snapshotControl(ctx, "append_allocate", scope.pin.token, scope.id, scope.pin.root, string(raw)).Text()
		if err != nil {
			return 0, wrapRedis(err, "append row allocation failed; allocation was not replayed")
		}
		if result == "ok" {
			return next, nil
		}
		seed, err = strconv.ParseInt(result, 10, 64)
		if err != nil {
			return 0, errorf(adbc.StatusIO, "invalid append allocation refusal")
		}
	}
}

func snapshotAppendKeys(pin *snapshotPin) (table, sequence string) {
	m := pin.appendTarget
	return strings.TrimPrefix(metaKey(m.Schema, m.Name), metaPrefix), strings.TrimPrefix(seqKey(m.Schema, m.Name), metaPrefix)
}

// Build only two native metadata values over the latest root. Row images and
// their final index are already immutable. All other dependencies still need
// the ordinary owner-equality check in rebaseSnapshotPublication.
func (s *store) mergeSnapshotAppend(ctx, original context.Context) (*tableMeta, error) {
	base := snapshotOf(original).pin.appendTarget
	latest, err := s.getTable(ctx, base.Schema, base.Name)
	if err != nil {
		return nil, err
	}
	staged, err := s.getTable(original, base.Schema, base.Name)
	if err != nil {
		return nil, err
	}
	// Exclusive writers are fenced by admission. Check the complete definition
	// as well, retaining identity/schema dependencies across a publication race.
	if !snapshotAppendSameDefinition(latest, base) || !snapshotAppendSameDefinition(staged, base) {
		return nil, stateErrorf("40001", "append table changed concurrently")
	}
	segments := latest.snapshotSegments()
	if len(segments) >= snapshotMaxSegments {
		return nil, stateErrorf("40001", "append segment capacity exhausted")
	}
	// Retain latest schema/attributes, adding conservative facts from this input.
	next := *latest
	next.Columns = append([]columnMeta(nil), latest.Columns...)
	if len(next.Columns) != len(staged.Columns) {
		return nil, stateErrorf("40001", "append columns changed concurrently")
	}
	for i := range next.Columns {
		if next.Columns[i].field() != staged.Columns[i].field() {
			return nil, stateErrorf("40001", "append fields changed concurrently")
		}
		if tagRank(staged.Columns[i].TagValues) > tagRank(next.Columns[i].TagValues) {
			next.Columns[i].TagValues = staged.Columns[i].TagValues
		}
		next.Columns[i].TagsChecked = next.Columns[i].TagsChecked && staged.Columns[i].TagsChecked
		next.Columns[i].NaNs = next.Columns[i].NaNs || staged.Columns[i].NaNs
	}
	next.KeyPrefix, next.IndexName, next.PrefixGen = staged.KeyPrefix, staged.IndexName, staged.PrefixGen
	next.SnapshotRows = staged.SnapshotRows
	next.SnapshotSegments = segments
	next.SnapshotOverlay = true
	raw, err := json.Marshal(&next)
	if err != nil {
		return nil, err
	}
	if err = s.redis(ctx).Set(ctx, metaKey(base.Schema, base.Name), raw, 0).Err(); err != nil {
		return nil, err
	}
	var high int64
	for _, source := range []context.Context{ctx, original} {
		n, err := s.redis(source).Get(source, seqKey(base.Schema, base.Name)).Int64()
		if err != nil && err != goredis.Nil {
			return nil, err
		}
		high = max(high, n)
	}
	if err = s.redis(ctx).Set(ctx, seqKey(base.Schema, base.Name), high, 0).Err(); err != nil {
		return nil, err
	}
	return &next, nil
}

func snapshotAppendSameDefinition(a, b *tableMeta) bool {
	definition := func(m *tableMeta) []byte {
		copy := *m
		copy.KeyPrefix, copy.IndexName, copy.PrefixGen = "", "", 0
		copy.SnapshotRows, copy.SnapshotSegments, copy.SnapshotOverlay = nil, nil, false
		copy.Columns = append([]columnMeta(nil), m.Columns...)
		for i := range copy.Columns {
			copy.Columns[i].TagValues = ""
			copy.Columns[i].TagsChecked = false
			copy.Columns[i].NaNs = false
		}
		raw, _ := json.Marshal(&copy)
		return raw
	}
	return bytes.Equal(definition(a), definition(b))
}
