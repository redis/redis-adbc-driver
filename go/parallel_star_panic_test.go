// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Hold an actual peer's native read cleanup after cancellation. The caller's
// panic must not escape/release the query grant while that worker still owns it.
type panicStarReplies struct {
	*parallelNativeReplies
	peerStarted, peerCanceled, allowExit chan struct{}
	startOnce, cancelOnce                sync.Once
	peerExited                           atomic.Bool
}

func (h *panicStarReplies) command(ctx context.Context, c goredis.Cmder) error {
	if c.Name() != "zrange" {
		err := h.replyParallel(c)
		if err != nil {
			c.SetErr(err)
		}
		return err
	}
	// A seek's first page reads by value; any page past the first row ID
	// belongs to the peer chunk.
	later := false
	if from, _, ok := mockDirectorySeek(c.Args()); ok {
		later = from > fmt.Sprintf("%020d", 1)
	} else {
		later = c.Args()[2].(int64) > 0
	}
	if later {
		h.startOnce.Do(func() { close(h.peerStarted) })
		<-ctx.Done()
		h.cancelOnce.Do(func() { close(h.peerCanceled) })
		<-h.allowExit
		h.peerExited.Store(true)
		c.SetErr(ctx.Err())
		return ctx.Err()
	}
	select {
	case <-h.peerStarted:
	case <-ctx.Done():
		c.SetErr(ctx.Err())
		return ctx.Err()
	}
	err := h.replyParallel(c)
	if err != nil {
		c.SetErr(err)
	}
	return err
}

func (h *panicStarReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error { return h.command(ctx, c) }
}

func (h *panicStarReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		var errs []error
		for _, c := range cs {
			if err := h.command(ctx, c); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) == 1 {
			return errs[0]
		}
		return errors.Join(errs...)
	}
}

func TestParallelStarConsumerPanicJoinsBeforeGrantRelease(t *testing.T) {
	exerciseParallelStarPanicLifecycle(t, nil, nil, nil)
}

func exerciseParallelStarPanicLifecycle(t *testing.T, configure func(*scanRequest), onVisit func(), after func()) {
	t.Helper()
	e, parent, req, base := parallelNativeStarFixture(t)
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	h := &panicStarReplies{parallelNativeReplies: base, peerStarted: make(chan struct{}), peerCanceled: make(chan struct{}), allowExit: make(chan struct{})}
	var exitOnce sync.Once
	allowExit := func() { exitOnce.Do(func() { close(h.allowExit) }) }
	defer allowExit()
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = c.Close() })
	c.AddHook(h)
	e.store.client = c
	if configure != nil {
		configure(&req)
	}
	sentinel := errors.New("coordinator callback panic")
	recovered := make(chan any, 1)
	go func() {
		defer func() { recovered <- recover() }()
		_ = e.scanPages(ctx, req, nil, func([]map[string]Value) error {
			if onVisit != nil {
				onVisit()
			}
			panic(sentinel)
		})
	}()
	select {
	case <-h.peerCanceled:
	case <-ctx.Done():
		t.Fatal("peer did not observe callback panic cancellation")
	}
	var panicValue any
	escaped := false
	// A gated peer cannot finish during this interval; this is a lifecycle gate,
	// not a query-speed assertion. The unfixed path returns the panic immediately.
	select {
	case panicValue = <-recovered:
		escaped = true
		t.Error("consumer panic escaped before peer worker cleanup")
	case <-time.After(50 * time.Millisecond):
	}
	if h.peerExited.Load() {
		t.Error("gated worker exited without permission")
	}
	if active := e.workerBudget.option(OptionIntQueryWorkersActive); active != "2" {
		t.Errorf("grant released before worker join: %s", active)
	}
	allowExit()
	if !escaped {
		select {
		case panicValue = <-recovered:
		case <-ctx.Done():
			t.Fatal("panic cleanup failed to join workers")
		}
	}
	if panicValue != sentinel || !h.peerExited.Load() || e.workerBudget.option(OptionIntQueryWorkersActive) != "0" || parent.Err() != nil {
		t.Fatalf("panic/lease/grant lifecycle: panic%v peerExited%v active%s parent%v", panicValue, h.peerExited.Load(), e.workerBudget.option(OptionIntQueryWorkersActive), parent.Err())
	}
	if after != nil {
		after()
	}
}
