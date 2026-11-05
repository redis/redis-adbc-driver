// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

func (s *store) flexEnabled() bool { return s != nil && s.flex }

func flexUnsupported(operation string) error {
	return errorf(adbc.StatusNotImplemented, "experimental Flex mode does not support %s", operation)
}

// Resource, permission and transport errors are not evidence that a feature
// is absent. Preserve their ordinary I/O diagnostic instead of advertising a
// false capability rejection.
func flexProbeError(err error, capability string) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not supported") || strings.Contains(message, "unsupported") || strings.Contains(message, "unknown command") {
		return errorf(adbc.StatusNotImplemented, "Flex requires %s: %v", capability, err)
	}
	return wrapRedis(err, "Flex capability probe failed")
}

// configureSearch probes the exact plain schema used by the opt-in stopgap.
// Probe objects have unique names and are removed before metadata initialization.
func (s *store) configureSearch(ctx context.Context, flex bool) (resultErr error) {
	if !flex {
		return s.refuseFlex(ctx)
	}
	name := "adbc-flex-probe:" + uuid.NewString()
	meta := &tableMeta{IndexName: name, KeyPrefix: name + ":", PlainIndex: true,
		Columns: []columnMeta{{Name: "n", Type: typeInt64, Indexed: true, PlainIndex: true}, {Name: "s", Type: typeString, Indexed: true, PlainIndex: true}}}
	if err := s.searchDo(ctx, name, indexCreateArgs(meta)...).Err(); err != nil {
		return flexProbeError(err, "plain NUMERIC and TAG indexes")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		delErr := s.redis(cleanup).Del(cleanup, meta.prefix()+"1").Err()
		dropErr := s.searchDo(cleanup, name, "FT.DROPINDEX", name).Err()
		if resultErr == nil && (delErr != nil || dropErr != nil) {
			resultErr = errorf(adbc.StatusIO, "Flex probe cleanup failed: %v", errors.Join(delErr, dropErr))
		}
	}()
	key := meta.prefix() + "1"
	if err := s.redis(ctx).HSet(ctx, key, rowIDField, "1", "n", "10", "s", "hello").Err(); err != nil {
		return wrapRedis(err, "Flex capability probe write failed")
	}
	// Index visibility can lag a write. Do not pass a connection on an empty reply.
	for attempt := 0; attempt < 100; attempt++ {
		reply, err := s.searchDo(ctx, name, "FT.SEARCH", name, "@n:[10 10]", "NOCONTENT", "LIMIT", 0, 2, "TIMEOUT", 0, "DIALECT", 2).Result()
		if err != nil {
			return flexProbeError(err, "bounded numeric Search reads")
		}
		count, keys, err := parseBoundedSearch(reply)
		if err != nil {
			return err
		}
		if count == 1 && len(keys) == 1 && keys[0] == key {
			rows, err := s.fetchRows(ctx, keys, []string{"n", "s"})
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0]["n"] != "10" || rows[0]["s"] != "hello" {
				return flexUnsupported("HASH field retrieval on this deployment")
			}
			s.flex = true
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return errorf(adbc.StatusIO, "Flex capability probe did not become visible in Search")
}

func (s *store) dropEmptyIndex(ctx context.Context, index string) error {
	args := []any{"FT.DROPINDEX", index}
	if !s.flex {
		args = append(args, "DD")
	}
	return s.searchDo(ctx, index, args...).Err()
}

// deleteFlexRows only visits the permanently reserved driver-owned prefix.
// It never runs on adopted HASHes, including nested application collections.
func (s *store) deleteFlexRows(ctx context.Context, meta *tableMeta) error {
	if meta.Adopted || meta.KeyRows || meta.AppIndex || meta.Foreign {
		return flexUnsupported("deletion of application data")
	}
	ks, err := (&hashConn{client: s.redis(ctx)}).scanKeys(ctx, meta.prefix(), "hash")
	if err != nil {
		return err
	}
	for {
		if err := cancelled(ctx); err != nil {
			return err
		}
		keys, done, err := ks.next(ctx)
		if err != nil {
			return err
		}
		for start := 0; start < len(keys); start += pipelineChunk {
			chunk := keys[start:min(start+pipelineChunk, len(keys))]
			rows, err := s.fetchRows(ctx, chunk, nil)
			if err != nil {
				return err
			}
			pipe := s.redis(ctx).Pipeline()
			for i, key := range chunk {
				text := strings.TrimPrefix(key, meta.prefix())
				id, err := strconv.ParseInt(text, 10, 64)
				if err == nil && id > 0 && strconv.FormatInt(id, 10) == text && rows[i] != nil && rows[i][rowIDField] == text {
					pipe.Del(ctx, key)
				}
			}
			if _, err := pipe.Exec(ctx); err != nil {
				return wrapRedis(err, "deleting Flex rows failed")
			}
		}
		if done {
			return nil
		}
	}
}

type idRange struct{ lo, hi int64 }
type rangeReader struct {
	store  *store
	req    *aggRequest
	ranges []idRange // depth-first, low IDs first; at most 54 pending ranges
	page   int
	closed bool
}

func (s *store) openRangeReader(ctx context.Context, req *aggRequest) (*aggCursor, error) {
	meta := req.meta
	if meta == nil || meta.Adopted || meta.KeyRows || len(req.groupBy) > 0 || len(req.sortBy) > 0 || req.limit != nil || req.rowFilter != "" {
		return nil, flexUnsupported("unbounded server pipelines or adopted indexes")
	}
	last, err := s.rowSequence(ctx, meta)
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, wrapRedis(err, "reading Flex row-ID boundary failed")
	}
	if last < 0 || last > exactDoubleLimit {
		return nil, flexUnsupported("row IDs beyond 2^53")
	}
	caps := s.paginationCaps(ctx, meta.index())
	page := min(req.pageRows(), 1000)
	if caps[0] > 0 {
		page = min(page, int(caps[0]))
	}
	r := &rangeReader{store: s, req: req, page: max(1, page)}
	if last > 0 {
		r.ranges = []idRange{{1, last}}
	}
	return &aggCursor{bounded: r}, nil
}

func (r *rangeReader) next(ctx context.Context) (pageRows []aggRow, resultErr error) {
	defer func() {
		if resultErr != nil {
			r.closed = true
			r.ranges = nil
		}
	}()
	for !r.closed && len(r.ranges) > 0 {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		i := len(r.ranges) - 1
		span := r.ranges[i]
		r.ranges = r.ranges[:i]
		query := fmt.Sprintf("@%s:[%d %d]", rowIDField, span.lo, span.hi)
		if r.req.query != "" && r.req.query != "*" {
			query = "(" + r.req.query + ") " + query
		}
		reply, err := r.store.searchDo(ctx, r.req.index, "FT.SEARCH", r.req.index, query, "NOCONTENT", "LIMIT", 0, r.page, "TIMEOUT", 0, "DIALECT", 2).Result()
		if err != nil {
			return nil, wrapRedis(err, "bounded Flex read failed")
		}
		count, keys, err := parseBoundedSearch(reply)
		if err != nil {
			return nil, err
		}
		// Full pages are split even if a server caps its reported count. Never
		// use offsets, and never emit rows from a range that must be retried.
		if count != int64(len(keys)) || len(keys) >= r.page && span.lo < span.hi {
			if span.lo == span.hi {
				return nil, errorf(adbc.StatusIO, "incomplete Flex singleton range; query aborted")
			}
			mid := span.lo + (span.hi-span.lo)/2
			r.ranges = append(r.ranges, idRange{mid + 1, span.hi}, idRange{span.lo, mid})
			continue
		}
		seen := map[string]bool{}
		ids := make(map[string]int64, len(keys))
		for _, key := range keys {
			text, ok := strings.CutPrefix(key, r.req.meta.prefix())
			id, err := strconv.ParseInt(text, 10, 64)
			if !ok || err != nil || id < span.lo || id > span.hi || seen[key] {
				return nil, errorf(adbc.StatusIO, "invalid or duplicate key in bounded Flex reply")
			}
			seen[key] = true
			ids[key] = id
		}
		sort.Slice(keys, func(i, j int) bool { return ids[keys[i]] < ids[keys[j]] })
		fields := r.req.load
		if r.req.loadAll {
			fields = nil
			for _, c := range r.req.meta.Columns {
				fields = append(fields, c.field())
				if c.MissingThrough > 0 {
					fields = append(fields, nullMarker(c.field()))
				}
			}
		}
		rows, err := r.store.fetchRows(ctx, keys, fields)
		if err != nil {
			return nil, err
		}
		for i, row := range rows {
			if row != nil {
				if row[rowIDField] != strconv.FormatInt(ids[keys[i]], 10) {
					return nil, errorf(adbc.StatusIO, "Flex row ID disagrees with its key")
				}
				row["__key"] = keys[i]
			}
		}
		return rows, nil
	}
	return nil, io.EOF
}

// NOCONTENT yields [total,key...] in RESP2 and results[{id:...}] in RESP3.
// Warnings are errors: an incomplete response must never look successful.
func parseBoundedSearch(reply any) (int64, []string, error) {
	count, err := parseSearchCount(reply)
	if err != nil {
		return 0, nil, err
	}
	var keys []string
	if list, ok := reply.([]any); ok {
		for _, v := range list[1:] {
			key, ok := v.(string)
			if !ok {
				return 0, nil, errorf(adbc.StatusIO, "malformed bounded Search key")
			}
			keys = append(keys, key)
		}
	} else if fields, ok := redisReplyMap(reply); ok {
		if w, exists := fields["warning"]; exists {
			warnings, ok := w.([]any)
			if !ok || len(warnings) > 0 {
				return 0, nil, errorf(adbc.StatusIO, "bounded Search returned warnings; query aborted")
			}
		}
		results, ok := fields["results"].([]any)
		if !ok {
			return 0, nil, errorf(adbc.StatusIO, "malformed bounded Search results")
		}
		for _, v := range results {
			fields, ok := redisReplyMap(v)
			if !ok {
				return 0, nil, errorf(adbc.StatusIO, "malformed bounded Search result")
			}
			key, ok := fields["id"].(string)
			if !ok {
				return 0, nil, errorf(adbc.StatusIO, "missing bounded Search key")
			}
			keys = append(keys, key)
		}
	}
	if count < 0 || count < int64(len(keys)) {
		return 0, nil, errorf(adbc.StatusIO, "invalid bounded Search count")
	}
	return count, keys, nil
}

// Both keys share {meta}. Redis checks and advances the sequence atomically;
// a rejected limit allocation must not make the table's reader reject its
// high-water mark. The accepted arithmetic stays within exact Lua integers.
const flexAllocateRowIDs = `
local n = tonumber(ARGV[1])
local maximum = tonumber(ARGV[2])
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
if not current or current < 0 or n < 1 or current > maximum - n then
    return redis.error_reply('ADBC_FLEX_ROW_ID_LIMIT')
end
local metadata = redis.call('GET', KEYS[2]) or ''
local last = redis.call('INCRBY', KEYS[1], ARGV[1])
return {last, metadata}
`

func (s *store) allocateFlexRowIDs(ctx context.Context, meta *tableMeta, n int) (int64, string, error) {
	reply, err := s.redis(ctx).Eval(ctx, flexAllocateRowIDs, []string{seqKey(meta.Schema, meta.Name), metaKey(meta.Schema, meta.Name)}, n, exactDoubleLimit).Slice()
	if err != nil {
		if strings.Contains(err.Error(), "ADBC_FLEX_ROW_ID_LIMIT") {
			return 0, "", flexUnsupported("row IDs beyond 2^53")
		}
		return 0, "", wrapRedis(err, "failed to allocate row ids")
	}
	if len(reply) != 2 {
		return 0, "", errorf(adbc.StatusInternal, "malformed Flex allocation reply")
	}
	last, ok := redisReplyInt64(reply[0])
	raw, rawOK := reply[1].(string)
	if !ok || !rawOK {
		return 0, "", errorf(adbc.StatusInternal, "malformed Flex allocation values")
	}
	return last, raw, nil
}
