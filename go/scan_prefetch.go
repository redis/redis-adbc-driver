// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"io"

	"github.com/apache/arrow-adbc/go/adbc"
)

// The worker alone discovers and fetches pages; the consumer alone decodes and
// appends Arrow. Unbuffered handoff permits one additional page while the
// consumer holds its current page, including a page blocked on delivery. There
// is no command/reply pool: each page owns its reply slices through consumption.
type hashPage struct {
	keys   []string
	values [][]any
	err    error
}

type hashPagePrefetch struct {
	pages  chan hashPage
	done   chan struct{}
	cancel context.CancelFunc
}

func newHashPagePrefetch(ctx context.Context, fetch func(context.Context) hashPage) *hashPagePrefetch {
	ctx, cancel := context.WithCancel(ctx)
	p := &hashPagePrefetch{pages: make(chan hashPage), done: make(chan struct{}), cancel: cancel}
	transport, _ := ctx.Value(transportContextKey{}).(*executionTransport)
	if transport != nil && !transport.addBackground(p.done) {
		cancel()
		close(p.pages)
		close(p.done)
		return p
	}
	go func() {
		defer func() {
			close(p.pages)
			if transport != nil {
				transport.completeBackground(p.done)
			} else {
				close(p.done)
			}
		}()
		for ctx.Err() == nil {
			page := func() (page hashPage) {
				defer func() {
					if v := recover(); v != nil {
						page = hashPage{err: errorf(adbc.StatusInternal, "prefetch failed: %v", v)}
					}
				}()
				return fetch(ctx)
			}()
			select {
			case p.pages <- page:
			case <-ctx.Done():
				return
			}
			if page.err != nil {
				return
			}
		}
	}()
	return p
}

func (p *hashPagePrefetch) next(ctx context.Context) hashPage {
	select {
	case <-ctx.Done():
		return hashPage{err: cancelled(ctx)}
	case page, ok := <-p.pages:
		// A canceled producer can close pages concurrently with the context.
		// Do not turn cancellation into successful EOF (or deliver another page).
		if ctx.Err() != nil {
			return hashPage{err: cancelled(ctx)}
		}
		if !ok {
			return hashPage{err: io.EOF}
		}
		return page
	}
}

func (p *hashPagePrefetch) close() {
	p.cancel()
	<-p.done // no READ can race DEL, transport close or worker-token release
}

func (it *scanIter) nextPrefetchedHash(ctx context.Context) ([]string, []map[string]Value, error) {
	if it.prefetch == nil {
		// The worker gets immutable scan metadata and exclusive cursor access.
		// It never sees the consumer's mutable projection, env or Arrow builder.
		worker := *it
		worker.prefetch, worker.projection = nil, nil
		it.prefetch = newHashPagePrefetch(ctx, worker.fetchHashPage)
	}
	page := it.prefetch.next(ctx)
	if page.err != nil {
		return nil, nil, page.err
	}
	return it.consumeHashValues(page.keys, page.values)
}

func (it *scanIter) fetchHashPage(ctx context.Context) hashPage {
	if err := it.checkReadiness(ctx); err != nil {
		return hashPage{err: err}
	}
	rows, err := it.cur.next(ctx)
	if checkErr := it.checkReadiness(ctx); checkErr != nil {
		return hashPage{err: checkErr}
	}
	if err != nil {
		return hashPage{err: err}
	}
	keys := make([]string, len(rows))
	for i, row := range rows {
		if it.exists(row) {
			keys[i] = row["__key"]
		}
	}
	values, err := it.store.fetchRowSlices(ctx, keys, it.fetch, wideFetchChunk)
	return hashPage{keys: keys, values: values, err: err}
}
