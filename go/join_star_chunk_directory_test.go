// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

type chunkDirectoryReplies struct {
	*streamNativeReplies
	mu       sync.Mutex
	dirs     map[string][]string
	failKey  string
	failure  error
	maxReply int
}

func (h *chunkDirectoryReplies) replyChunk(c goredis.Cmder) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch c.Name() {
	case "zlexcount":
		ids := h.dirs[fmt.Sprint(c.Args()[1])]
		lo := strings.TrimPrefix(fmt.Sprint(c.Args()[3]), "(")
		c.(*goredis.IntCmd).SetVal(int64(sort.SearchStrings(ids, lo)))
		return nil
	case "zrange":
		ids := h.dirs[fmt.Sprint(c.Args()[1])]
		lo, hi := c.Args()[2].(int64), c.Args()[3].(int64)
		h.maxReply = max(h.maxReply, int(hi-lo+1))
		lo = min(lo, int64(len(ids)))
		hi = min(hi+1, int64(len(ids)))
		c.(*goredis.StringSliceCmd).SetVal(ids[lo:hi])
		return nil
	case "hmget":
		if h.failKey != "" && fmt.Sprint(c.Args()[1]) == h.failKey {
			return h.failure
		}
	}
	return h.reply(c)
}
func (h *chunkDirectoryReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error {
		err := h.replyChunk(c)
		if err != nil {
			c.SetErr(err)
		}
		return err
	}
}
func (h *chunkDirectoryReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		var errs []error
		for _, c := range cs {
			if err := h.replyChunk(c); err != nil {
				c.SetErr(err)
				errs = append(errs, err)
			}
		}
		if len(errs) == 1 {
			return errs[0]
		}
		return errors.Join(errs...)
	}
}

func TestStarChunkSparseOverlayPageHintAndSourceErrors(t *testing.T) {
	for _, kind := range []string{"ok", "missing_winner", "partial_error", "consumer_then_source"} {
		t.Run(kind, func(t *testing.T) {
			d, ctx, base := streamNativeFixture(t)
			old := d.meta.snapshotSegments()[0]
			old.Rows = 7
			id := snapshotID()
			newer := snapshotSegment{Prefix: snapshotRows(id, defaultSchema, d.meta.SnapshotAccessID), Index: snapshotIndexPrefix(id) + "working", Rows: 3}
			base.segments[old.Index] = old
			base.segments[newer.Index] = newer
			d.meta.SnapshotSegments = []snapshotSegment{old}
			d.meta.KeyPrefix = newer.Prefix
			d.meta.IndexName = newer.Index
			n := int64(3)
			d.meta.SnapshotRows = &n
			d.meta.Columns = []columnMeta{{Name: "id", Type: typeInt64}, {Name: "v", Type: typeInt64}}
			h := &chunkDirectoryReplies{streamNativeReplies: base, dirs: map[string][]string{}, failure: errors.New("partial pipeline reply loss")}
			add := func(seg snapshotSegment, ids []int64) {
				dir := snapshotMembers(snapshotDataID(seg.Prefix))
				for _, id := range ids {
					h.dirs[dir] = append(h.dirs[dir], fmt.Sprintf("%020d", id))
					base.rows[seg.Prefix+fmt.Sprint(id)] = aggRow{rowIDField: fmt.Sprint(id), "id": fmt.Sprint(id), "v": fmt.Sprint(id)}
				}
			}
			add(old, []int64{1, 256, 257, 511, 512, 768, 1024})
			add(newer, []int64{256, 257, 600})
			base.rows[newer.Prefix+"256"][snapshotDeletedField] = "1"
			base.rows[newer.Prefix+"257"]["v"] = "900"
			if kind == "missing_winner" {
				delete(base.rows, newer.Prefix+"257")
			}
			if kind == "partial_error" || kind == "consumer_then_source" {
				h.failKey = old.Prefix + "1024"
			}
			c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
			t.Cleanup(func() { _ = c.Close() })
			c.AddHook(h)
			d.store.client = c
			e := &executor{store: d.store, cache: newExecCache()}
			locals := []executor{*e, *e}
			for w := range locals {
				locals[w].cache = newExecCache()
			}
			reducer := errors.New("consumer failed")
			var got []int64
			src, consumer := parallelStarJoinChunks(ctx, &starShape{}, 2, 4, func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
				bounds := starFactChunkRange(1024, 4, chunk)
				bounds.cap = 2
				return locals[chunk%2].scanPages(ctx, scanRequest{meta: d.meta, where: wherePlan{query: "*"}, need: map[string]bool{"id": true, "v": true}, ranges: &bounds}, nil, consume)
			}, func(rows []map[string]Value) error {
				for _, row := range rows {
					got = append(got, row["v"].I)
				}
				if kind == "consumer_then_source" {
					return reducer
				}
				return nil
			})
			if kind == "ok" {
				if src != nil || consumer != nil || !reflect.DeepEqual(got, []int64{1, 900, 511, 512, 600, 768, 1024}) {
					t.Fatalf("%v %v %v", src, consumer, got)
				}
			} else if src == nil {
				t.Fatalf("source failure hidden: %v", got)
			}
			if kind == "consumer_then_source" && consumer != reducer {
				t.Fatal("consumer error lost")
			}
			if h.maxReply > 2 {
				t.Fatal("directory overfetch hint ignored", h.maxReply)
			}
		})
	}
}
