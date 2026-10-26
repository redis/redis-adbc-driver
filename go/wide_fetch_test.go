// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func TestWideHashFetchSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*scanRequest, *scanIter, *aggRequest)
		want   bool
	}{
		{"unordered", func(*scanRequest, *scanIter, *aggRequest) {}, true},
		{"implicit order", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.sortBy = []sortKey{{field: rowIDField}} }, true},
		{"narrow", func(_ *scanRequest, i *scanIter, _ *aggRequest) { delete(i.need, "c63") }, false},
		{"half omitted", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.meta.Columns = make([]columnMeta, 128) }, false},
		{"exact load", func(_ *scanRequest, _ *scanIter, a *aggRequest) { a.loadAll = false }, false},
		{"residual", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.where.residual = &Literal{} }, false},
		{"ties", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.ties = &tieSort{} }, false},
		{"late", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.late = &lateProjection{} }, false},
		{"limit", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.limit = &[2]int64{0, 10} }, false},
		{"descending", func(r *scanRequest, _ *scanIter, _ *aggRequest) {
			r.sortBy = []sortKey{{field: rowIDField, desc: true}}
		}, false},
		{"other sort", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.sortBy = []sortKey{{field: "c0"}} }, false},
		{"adopted", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.meta.Adopted = true }, false},
		{"key rows", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.meta.KeyRows = true }, false},
		{"foreign", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.meta.Foreign = true }, false},
		{"application index", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.meta.AppIndex = true }, false},
		{"moving", func(r *scanRequest, _ *scanIter, _ *aggRequest) { r.meta.RekeyTo = "other" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := &tableMeta{Schema: defaultSchema, Name: "wide", KeyPrefix: "wide:", Columns: make([]columnMeta, 65)}
			it := &scanIter{meta: meta, need: map[string]bool{}}
			for c := range 64 {
				it.need[fmt.Sprintf("c%d", c)] = true
			}
			req, ar := scanRequest{meta: meta}, &aggRequest{loadAll: true}
			tc.change(&req, it, ar)
			if got := wideHashFetch(req, it, ar); got != tc.want {
				t.Fatalf("selected=%v, want %v", got, tc.want)
			}
		})
	}
}

type wideFetchCancelHook struct {
	cancel context.CancelFunc
	t      *testing.T
	calls  int
}

func (h *wideFetchCancelHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *wideFetchCancelHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *wideFetchCancelHook) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		h.calls++
		if len(cmds) != wideFetchChunk {
			h.t.Fatalf("pipeline has %d commands, want %d", len(cmds), wideFetchChunk)
		}
		for _, cmd := range cmds {
			cmd.(*goredis.SliceCmd).SetVal([]any{"1", "9007199254740993"})
		}
		h.cancel()
		return nil
	}
}

func TestWideFetchCancelledBetweenBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := goredis.NewClient(&goredis.Options{})
	defer client.Close()
	hook := &wideFetchCancelHook{cancel: cancel, t: t}
	client.AddHook(hook)
	keys := make([]string, wideFetchChunk+1)
	for i := range keys {
		keys[i] = "row"
	}
	_, err := (&store{client: client}).fetchRowsChunk(ctx, keys, []string{"v"}, wideFetchChunk)
	if !isStatus(err, adbc.StatusCancelled) || hook.calls != 1 {
		t.Fatalf("got %v after %d pipelines; want cancellation after one", err, hook.calls)
	}
}
