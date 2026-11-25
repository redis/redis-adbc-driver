// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type overBudgetParallelReplies struct {
	*parallelNativeReplies
	count    int64
	searches atomic.Int64
	failure  error
	// keys, when set, answer a complete candidate fetch (LIMIT 0 n).
	keys []string
	// fetched counts HASH reads, by key.
	fetched map[string]int
}

func (h *overBudgetParallelReplies) replyProof(c goredis.Cmder) error {
	switch c.Name() {
	case "ft.info":
		seg, ok := h.segments[fmt.Sprint(c.Args()[1])]
		if !ok {
			return errors.New("unknown proof source")
		}
		c.(onceCmd).SetVal(pageReadyInfo(seg))
		return nil
	case "ft.search":
		h.searches.Add(1)
		if n := c.Args()[6].(int); n != 0 {
			if h.keys == nil || int64(n) != h.count {
				return errors.New("parallel fallback repeated candidate fetch")
			}
			reply := []any{h.count}
			for _, k := range h.keys {
				reply = append(reply, k)
			}
			c.(onceCmd).SetVal(reply)
			return nil
		}
		if strings.Contains(fmt.Sprint(c.Args()[2]), rowIDField) {
			return errors.New("parallel fallback introduced numeric row-ID filter")
		}
		if h.failure != nil {
			return h.failure
		}
		c.(onceCmd).SetVal([]any{h.count})
		return nil
	case "zmscore":
		scores := make([]any, len(c.Args())-2)
		for i := range scores {
			scores[i] = "0"
		}
		c.(*goredis.Cmd).SetVal(scores)
		return nil
	case "hmget":
		h.mu.Lock()
		if h.fetched != nil {
			h.fetched[fmt.Sprint(c.Args()[1])]++
		}
		h.mu.Unlock()
	}
	return h.replyParallel(c)
}
func (h *overBudgetParallelReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error {
		err := h.replyProof(c)
		if err != nil {
			c.SetErr(err)
		}
		return err
	}
}
func (h *overBudgetParallelReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		var es []error
		for _, c := range cs {
			if err := h.replyProof(c); err != nil {
				c.SetErr(err)
				es = append(es, err)
			}
		}
		if len(es) == 1 {
			return es[0]
		}
		return errors.Join(es...)
	}
}

func overBudgetParallelFixture(t *testing.T, n int64) (*executor, context.Context, scanRequest, *overBudgetParallelReplies) {
	t.Helper()
	e, ctx, req, base := parallelNativeStarFixture(t)
	fact := req.meta.join.items[1].base
	fact.SnapshotRows = &n
	fact.PlainIndex = true
	fact.SnapshotOverlay = false
	fact.Columns[0].Indexed = true
	base.n = n
	seg := fact.snapshotSegments()[0]
	base.segments[seg.Index] = seg
	for i := int64(4098); i <= n; i++ {
		base.rows[seg.Prefix+fmt.Sprint(i)] = aggRow{rowIDField: fmt.Sprint(i), "id": "1", "v": fmt.Sprint(i)}
	}
	e.store.flex = true
	hook := &overBudgetParallelReplies{parallelNativeReplies: base, count: n}
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = c.Close() })
	c.AddHook(hook)
	e.store.client = c
	pred := &Binary{Op: "=", L: &ColumnRef{Name: "f.id"}, R: &Literal{V: intValue(typeInt64, 1)}}
	req.meta.join.items[1].pushed = []Expr{pred}
	req.where.residual = &Binary{Op: "AND", L: req.where.residual, R: pred}
	return e, ctx, req, hook
}

func TestParallelStarProvenOverBudgetFlexUsesOnlyCountThenRanges(t *testing.T) {
	// Over the shared budget too, so no shared materialization applies.
	shared := flexSharedCandidates
	flexSharedCandidates = flexScanCandidates
	t.Cleanup(func() { flexSharedCandidates = shared })
	n := int64(snapshotFlexCandidateCap + 1)
	e, ctx, req, h := overBudgetParallelFixture(t, n)
	count := 0
	handled, err := e.scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			if row["f.id"].I != 1 {
				t.Error("complete predicate lost")
			}
		}
		count += len(rows)
		return nil
	})
	if !handled || err != nil || count != int(n)*2 || e.workerGrant.Load() != 2 || h.searches.Load() != 1 || e.workerBudget.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatalf("proof/ranges: handled%v err%v count%d grant%d searches%d", handled, err, count, e.workerGrant.Load(), h.searches.Load())
	}
}

// A complete set smaller than two chunks keeps the serial candidate path.
func TestParallelStarAdmittedFlexStillKeepsSelectivePath(t *testing.T) {
	e, ctx, req, h := overBudgetParallelFixture(t, 4097)
	h.count = 2*starChunkRows - 1
	shape, ok := e.planStarShape(ctx, req.meta.join, req.need, req.where.residual)
	if !ok {
		t.Fatal("fixture shape refused")
	}
	handled, err := e.scanParallelStarFacts(ctx, shape, req.meta.join, nil, func([]map[string]Value) error { t.Error("admitted selective source consumed"); return nil })
	if handled || err != nil || e.workerGrant.Load() != 0 || h.searches.Load() != 1 || h.rangeReads != 0 {
		t.Fatalf("admitted query changed execution: %v %v grant%d searches%d", handled, err, e.workerGrant.Load(), h.searches.Load())
	}
}

func TestParallelStarAmbiguousFlexCountNeverFallsBack(t *testing.T) {
	e, ctx, req, h := overBudgetParallelFixture(t, 4097)
	h.failure = errors.Join(goredis.Nil, errors.New("candidate count reply lost"))
	count := 0
	handled, err := e.scanStarJoinPages(ctx, req, nil, func([]map[string]Value) error { count++; return nil })
	if !handled || err == nil || !strings.Contains(err.Error(), "reply lost") || count != 0 || e.workerGrant.Load() != 0 || h.rangeReads != 0 {
		t.Fatalf("ambiguous proof retried source: %v %v count%d grant%d", handled, err, count, e.workerGrant.Load())
	}
}

// A selective Flex fact within the shared budget discovers its complete
// candidates once and splits them across the workers. Only candidate rows
// are read, and each one is checked against the complete predicate: the
// candidate whose row no longer matches is dropped, and no directory range
// is read. Before shared candidates, this set kept the serial scan.
func TestParallelStarSharesFlexCandidatesAcrossWorkers(t *testing.T) {
	e, ctx, req, h := overBudgetParallelFixture(t, 4097)
	fact := req.meta.join.items[1].base
	seg := fact.snapshotSegments()[0]
	const k = 2000
	h.count, h.fetched = k, map[string]int{}
	for i := 1; i <= k; i++ {
		h.keys = append(h.keys, seg.Prefix+fmt.Sprint(i))
	}
	h.rows[seg.Prefix+"1500"]["id"] = "2"
	got := map[int64]int{}
	handled, err := e.scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			if row["f.id"].I != 1 {
				t.Error("complete predicate lost")
			}
			got[row["f.v"].I]++
		}
		return nil
	})
	if !handled || err != nil || e.workerGrant.Load() != 2 || h.searches.Load() != 3 || h.rangeReads != 0 || e.workerBudget.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatalf("shared candidates: handled%v err%v grant%d searches%d ranges%d", handled, err, e.workerGrant.Load(), h.searches.Load(), h.rangeReads)
	}
	for i := int64(1); i <= k; i++ {
		want := 2
		if i == 1500 {
			want = 0
		}
		if got[i] != want {
			t.Fatalf("row %d produced %d outputs, want %d", i, got[i], want)
		}
	}
	for key, n := range h.fetched {
		id, _ := strconv.ParseInt(strings.TrimPrefix(key, seg.Prefix), 10, 64)
		if !strings.HasPrefix(key, seg.Prefix) || id < 1 || id > k || n != 1 {
			t.Fatalf("read %s %d times; only candidates, once each", key, n)
		}
	}
	if len(h.fetched) != k {
		t.Fatalf("read %d candidate rows, want %d", len(h.fetched), k)
	}
}
