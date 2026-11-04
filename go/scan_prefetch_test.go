// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// This is a retained-heap diagnostic, not a ClickBench elapsed result. The
// source models an owned 4096 x 105 reply with unique 32-byte string payloads.
func TestHashPrefetchAdditionalLiveMemory(t *testing.T) {
	makePage := func(context.Context) hashPage {
		page := hashPage{keys: make([]string, 4096), values: make([][]any, 4096)}
		for i := range page.values {
			page.keys[i] = "row:" + strconv.Itoa(i)
			page.values[i] = make([]any, 106)
			for j := range page.values[i] {
				page.values[i][j] = strings.Repeat("x", 24) + strconv.Itoa(i*106+j)
			}
		}
		return page
	}
	runtime.GC()
	var base, one, two runtime.MemStats
	runtime.ReadMemStats(&base)
	first := makePage(context.Background())
	runtime.GC()
	runtime.ReadMemStats(&one)
	ready := make(chan struct{})
	p := newHashPagePrefetch(context.Background(), func(ctx context.Context) hashPage {
		page := makePage(ctx)
		close(ready)
		return page
	})
	awaitPrefetch(t, ready)
	runtime.GC()
	runtime.ReadMemStats(&two)
	t.Logf("one page live heap delta=%d bytes; additional lookahead delta=%d bytes (GC heap samples; not RSS or total allocations)", int64(one.HeapAlloc)-int64(base.HeapAlloc), int64(two.HeapAlloc)-int64(one.HeapAlloc))
	runtime.KeepAlive(first)
	p.close()
}

func awaitPrefetch(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("prefetch did not stop")
	}
}

func TestHashPrefetchOnePageAndReplyOwnership(t *testing.T) {
	second := make(chan struct{})
	var calls atomic.Int64
	p := newHashPagePrefetch(context.Background(), func(context.Context) hashPage {
		n := calls.Add(1)
		if n == 2 {
			close(second)
		}
		return hashPage{keys: []string{"key"}, values: [][]any{{n, "a\x00b"}}}
	})
	defer p.close()
	first := p.next(context.Background())
	awaitPrefetch(t, second)
	// While the consumer owns page 1, page 2 is blocked on delivery. The
	// worker cannot begin page 3; no timing-dependent polling is needed.
	if calls.Load() != 2 {
		t.Fatalf("fetched %d pages", calls.Load())
	}
	if first.values[0][0] != int64(1) || first.values[0][1] != "a\x00b" {
		t.Fatal("reply overwritten")
	}
	if next := p.next(context.Background()); next.values[0][0] != int64(2) {
		t.Fatal("pages reordered")
	}
}

func TestHashPrefetchCancellation(t *testing.T) {
	for _, stage := range []string{"fetch", "delivery", "conversion"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			var calls atomic.Int64
			p := newHashPagePrefetch(ctx, func(ctx context.Context) hashPage {
				if calls.Add(1) == 1 {
					close(entered)
				}
				if stage == "fetch" {
					<-ctx.Done()
					return hashPage{err: ctx.Err()}
				}
				return hashPage{values: [][]any{{"1"}}}
			})
			awaitPrefetch(t, entered)
			if stage == "conversion" {
				page := p.next(ctx)
				// Consumer cancellation while converting an owned reply must
				// also interrupt a producer fetching/delivering the next page.
				if len(page.values) != 1 {
					t.Fatal("missing conversion input")
				}
			}
			cancel()
			joined := make(chan struct{})
			go func() { p.close(); close(joined) }()
			awaitPrefetch(t, joined)
			awaitPrefetch(t, p.done)
		})
	}
}

func TestHashPrefetchTerminalErrors(t *testing.T) {
	failure := errors.New("fetch error")
	for _, want := range []error{io.EOF, failure} {
		p := newHashPagePrefetch(context.Background(), func(context.Context) hashPage { return hashPage{err: want} })
		if got := p.next(context.Background()).err; got != want {
			t.Fatalf("got %v want %v", got, want)
		}
		awaitPrefetch(t, p.done)
		p.close()
	}
	p := newHashPagePrefetch(context.Background(), func(context.Context) hashPage { panic("worker panic") })
	if got := p.next(context.Background()).err; got == nil {
		t.Fatal("worker panic lost")
	}
	p.close()
}

func TestHashPrefetchCanceledClosedQueueIsNotEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := newHashPagePrefetch(ctx, func(ctx context.Context) hashPage {
		<-ctx.Done()
		return hashPage{err: ctx.Err()}
	})
	cancel()
	p.close()
	for range 100 {
		if page := p.next(ctx); !isStatus(page.err, adbc.StatusCancelled) {
			t.Fatalf("canceled closed queue returned %v", page.err)
		}
	}
}
