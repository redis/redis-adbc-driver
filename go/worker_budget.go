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
	"runtime"
	"strconv"
	"sync"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A database handle is one runtime: all its connections share this coordinator.
// Grants are non-preemptive. FIFO admission prevents new queries from passing
// queued queries; when several are waiting, available tokens are divided among
// them round-robin up to their ceilings. A lone waiter can borrow all capacity.
// The zero value starts with a conservative CPU-based budget and learns from completed scans.
type workerBudget struct {
	mu         sync.Mutex
	configured int
	active     int
	queue      []*workerRequest
	tuner      workerTuner
	cohort     workerCohort
	regime     string
}

type workerRequest struct {
	ctx        context.Context
	ceiling    int
	partitions int
	grant      int
	ready      chan struct{}
}

func (b *workerBudget) upper() int {
	if b.configured > 0 {
		return min(32, b.configured)
	}
	return min(32, max(1, runtime.GOMAXPROCS(0))*2)
}

func (b *workerBudget) limit() int {
	if b.configured > 0 {
		return b.configured
	}
	return b.tuner.capacity(b.upper())
}

func (b *workerBudget) capacity() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limit()
}

func (b *workerBudget) option(key string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch key {
	case OptionStringQueryWorkerTuning:
		return b.tuningJSON()
	case OptionIntQueryWorkersActive:
		return strconv.Itoa(b.active)
	case OptionIntQueryWorkersQueued:
		return strconv.Itoa(len(b.queue))
	default:
		return strconv.Itoa(b.limit())
	}
}

func (b *workerBudget) configure(value string) error {
	n := 0
	if value != "auto" {
		var err error
		n, err = strconv.Atoi(value)
		if err != nil || n < 1 {
			return errorf(adbc.StatusInvalidArgument, "invalid %s %q (want auto or a positive integer)", OptionStringQueryWorkerBudget, value)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configured = n
	b.tuner = workerTuner{}
	b.regime = ""
	b.dispatch()
	return nil
}

// dispatch runs under mu, re-reading runtime capacity without revoking grants.
func (b *workerBudget) dispatch() {
	available := b.limit() - b.active
	live := b.queue[:0]
	for _, r := range b.queue {
		if r.ctx.Err() != nil {
			close(r.ready)
		} else {
			live = append(live, r)
		}
	}
	clear(b.queue[len(live):])
	b.queue = live
	admitted := min(max(0, available), len(b.queue))
	if admitted == 0 {
		return
	}
	requests := b.queue[:admitted]
	for available > 0 {
		assigned := false
		for _, r := range requests {
			ceiling := effectiveQueryWorkers(r.ceiling)
			if r.partitions > 0 {
				ceiling = min(ceiling, r.partitions)
			}
			if r.ceiling == 0 {
				ceiling = min(ceiling, b.tuner.capacity(b.upper()))
			}
			if available > 0 && r.grant < ceiling {
				r.grant++
				available--
				assigned = true
			}
		}
		if !assigned {
			break
		}
	}
	for _, r := range requests {
		b.active += r.grant
		close(r.ready)
	}
	clear(b.queue[:admitted])
	b.queue = b.queue[admitted:]
	if len(b.queue) == 0 {
		b.queue = nil
	}
}

func (b *workerBudget) acquire(ctx context.Context, ceiling int) (int, error) {
	return b.acquireBounded(ctx, ceiling, 0)
}

func (b *workerBudget) acquireBounded(ctx context.Context, ceiling, partitions int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r := &workerRequest{ctx: ctx, ceiling: ceiling, partitions: partitions, ready: make(chan struct{})}
	b.mu.Lock()
	b.queue = append(b.queue, r)
	b.dispatch()
	b.mu.Unlock()
	select {
	case <-r.ready:
	case <-ctx.Done():
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		// Cancellation may race a grant. Return it under the same lock before
		// dispatching more work, or remove the still-queued request.
		b.active -= r.grant
		for i, queued := range b.queue {
			if queued == r {
				copy(b.queue[i:], b.queue[i+1:])
				b.queue[len(b.queue)-1] = nil
				b.queue = b.queue[:len(b.queue)-1]
				break
			}
		}
		b.dispatch()
		return 0, err
	}
	return r.grant, nil
}

func (b *workerBudget) release(grant int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active -= grant
	b.dispatch()
}

func (b *workerBudget) automatic() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.configured == 0
}
