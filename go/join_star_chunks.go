// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
)

const starChunkRows int64 = 256
const maxStarChunks int64 = 65536

// Fixed upper bound prevents sparse or near-MaxInt64 directories from creating
// unbounded empty scans. Balanced chunk ranges use exact quotient arithmetic and
// retains the MaxInt64 tail. Only the optional CPU native composition selects
// this policy; shared compatibility entry points keep one chunk per worker.
func starFactChunkCount(last int64, workers int) int {
	if last < int64(workers) || workers < 1 || workers > 32 {
		return 0
	}
	return int(max(int64(workers), min((last-1)/starChunkRows+1, maxStarChunks)))
}

// Distribute the remainder across the finite prefix instead of accumulating it
// into the last chunk. Last hi still includes the authoritative MaxInt64 tail.
func starFactChunkRange(last int64, chunks, chunk int) rowIDRangeScan {
	q, r := last/int64(chunks), last%int64(chunks)
	n := q
	if int64(chunk) < r {
		n++
	}
	lo := int64(chunk)*q + min(int64(chunk), r) + 1
	hi := lo + (n - 1)
	if chunk == chunks-1 {
		hi = math.MaxInt64
	}
	return rowIDRangeScan{lo: lo, hi: hi}
}

type starChunkEvent struct {
	rows       []map[string]Value
	chunk      int
	done       bool
	err        error
	panicValue any
	didPanic   bool
}

// Round-robin workers retain one page queue each. End markers distinguish empty
// chunks from EOF and make source errors authoritative in global chunk order.
// Output order is unchanged; smaller chunks shorten head-of-line blocking.
func parallelStarJoinChunks(ctx context.Context, shape *starShape, workers, chunks int, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (sourceErr, consumerErr error) {
	return parallelStarJoinChunksMode(ctx, shape, workers, chunks, false, scan, visit)
}
func parallelStarJoinChunksMode(ctx context.Context, shape *starShape, workers, chunks int, ownedFacts bool, scan func(context.Context, int, func([]map[string]Value) error) error, visit func([]map[string]Value) error) (sourceErr, consumerErr error) {
	if workers < 1 || chunks < workers {
		return fmt.Errorf("invalid star chunk configuration"), nil
	}
	child, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	queues := make([]chan starChunkEvent, workers)
	var stop atomic.Bool
	for w := range queues {
		queues[w] = make(chan starChunkEvent, 1)
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			defer close(queues[w])
			probe := newStarRowProbe(shape)
			probeRow := probe.row
			if ownedFacts {
				probeRow = probe.ownedRow
			}
			send := func(event starChunkEvent) error {
				select {
				case queues[w] <- event:
					return nil
				case <-child.Done():
					return cancelled(child)
				}
			}
			for chunk := w; chunk < chunks; chunk += workers {
				if child.Err() != nil {
					return
				}
				err, panicValue, didPanic := func() (err error, panicValue any, didPanic bool) {
					normal := false
					defer func() {
						if !normal {
							panicValue = recover()
							didPanic = true
						}
					}()
					out := make([]map[string]Value, 0, starOutputPage)
					flush := func() error {
						if len(out) == 0 {
							return nil
						}
						if stop.Load() {
							out = nil
							return nil
						}
						if err := send(starChunkEvent{rows: out, chunk: chunk}); err != nil {
							return err
						}
						out = make([]map[string]Value, 0, starOutputPage)
						return nil
					}
					emit := func(row map[string]Value) error {
						if stop.Load() {
							return errStarProbeStopped
						}
						out = append(out, row)
						if len(out) == starOutputPage {
							return flush()
						}
						return nil
					}
					err = scan(child, chunk, func(page []map[string]Value) error {
						for _, row := range page {
							if err := cancelled(child); err != nil {
								return err
							}
							if !stop.Load() {
								if err := probeRow(child, row, emit); err != nil && err != errStarProbeStopped {
									return err
								}
							}
						}
						return nil
					})
					if err == nil {
						err = flush()
					}
					normal = true
					return err, nil, false
				}()
				if send(starChunkEvent{chunk: chunk, done: true, err: err, panicValue: panicValue, didPanic: didPanic}) != nil {
					return
				}
				if didPanic {
					return
				}
			}
		}(w)
	}
	for chunk := 0; chunk < chunks; chunk++ {
		queue := queues[chunk%workers]
		for {
			var event starChunkEvent
			var ok bool
			select {
			case event, ok = <-queue:
			case <-ctx.Done():
				return cancelled(ctx), consumerErr
			}
			if !ok {
				if ctx.Err() != nil {
					return cancelled(ctx), consumerErr
				}
				return fmt.Errorf("star worker closed before chunk completion"), consumerErr
			}
			if event.chunk != chunk {
				return fmt.Errorf("star chunk order mismatch"), consumerErr
			}
			if event.didPanic {
				panic(event.panicValue)
			}
			if event.done {
				if event.err != nil && sourceErr == nil {
					sourceErr = event.err
					stop.Store(true)
				}
				break
			}
			if ctx.Err() != nil {
				return cancelled(ctx), consumerErr
			}
			if sourceErr == nil && consumerErr == nil {
				if err := visit(event.rows); err != nil {
					consumerErr = err
					stop.Store(true)
				}
			}
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		return cancelled(ctx), consumerErr
	}
	return sourceErr, consumerErr
}
