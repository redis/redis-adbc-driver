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
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
)

func waitQueued(t *testing.T, b *workerBudget, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for b.option(OptionIntQueryWorkersQueued) != fmt.Sprint(n) {
		if time.Now().After(deadline) {
			t.Fatalf("queue: want %d, got %s", n, b.option(OptionIntQueryWorkersQueued))
		}
		runtime.Gosched()
	}
}

func TestWorkerBudgetRuntimeAndCeiling(t *testing.T) {
	for _, limit := range []int{1, 4, 16} {
		old := runtime.GOMAXPROCS(limit)
		b := &workerBudget{}
		for _, ceiling := range []int{0, 1, 3, 32} {
			got, err := b.acquire(context.Background(), ceiling)
			if err != nil || got != 1 {
				t.Fatalf("limit %d ceiling %d: %d %v", limit, ceiling, got, err)
			}
			b.release(got)
		}
		runtime.GOMAXPROCS(old)
	}
}

func TestWorkerBudgetFIFOAndSharing(t *testing.T) {
	b := &workerBudget{configured: 4}
	held, _ := b.acquire(context.Background(), 32)
	type grant struct {
		n    int
		done chan struct{}
	}
	results := make([]chan grant, 4)
	for i := range results {
		results[i] = make(chan grant, 1)
		go func(i int) {
			n, err := b.acquire(context.Background(), 32)
			if err != nil {
				t.Error(err)
				return
			}
			done := make(chan struct{})
			results[i] <- grant{n, done}
			<-done
			b.release(n)
		}(i)
		waitQueued(t, b, i+1)
	}
	b.release(held)
	for _, result := range results {
		select {
		case g := <-result:
			if g.n != 1 {
				t.Fatalf("four waiting queries should share four tokens: %d", g.n)
			}
			close(g.done)
		case <-time.After(5 * time.Second):
			t.Fatal("starved query")
		}
	}
	// Two queries can also borrow spare capacity without waiting.
	deadline := time.Now().Add(5 * time.Second)
	for b.option(OptionIntQueryWorkersActive) != "0" {
		if time.Now().After(deadline) {
			t.Fatal("leaked grants")
		}
		runtime.Gosched()
	}
	a, _ := b.acquire(context.Background(), 3)
	c, _ := b.acquire(context.Background(), 32)
	if a != 3 || c != 1 {
		t.Fatalf("spare capacity: %d %d", a, c)
	}
	b.release(a)
	b.release(c)
}

func TestWorkerBudgetCancellationAndResize(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	b := &workerBudget{tuner: workerTuner{current: 4, cpu: 4}}
	held, _ := b.acquire(context.Background(), 32)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := b.acquire(ctx, 32); errc <- err }()
	waitQueued(t, b, 1)
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatal(err)
	}
	waitQueued(t, b, 0)
	runtime.GOMAXPROCS(1)
	got := make(chan int, 1)
	go func() { n, _ := b.acquire(context.Background(), 32); got <- n }()
	waitQueued(t, b, 1)
	if b.option(OptionIntQueryWorkersActive) != "4" {
		t.Fatal("preempted existing workers")
	}
	b.release(held)
	n := <-got
	if n != 1 {
		t.Fatalf("lower runtime limit: %d", n)
	}
	b.release(n)
	// A larger override wakes queued work even when the old grant is retained.
	held, _ = b.acquire(context.Background(), 32)
	go func() { n, _ := b.acquire(context.Background(), 32); got <- n }()
	waitQueued(t, b, 1)
	if err := b.configure("4"); err != nil {
		t.Fatal(err)
	}
	n = <-got
	if n != 3 {
		t.Fatalf("new capacity: %d", n)
	}
	b.release(n)
	b.release(held)
}

func TestWorkerBudgetCancellationGrantRace(t *testing.T) {
	b := &workerBudget{configured: 4}
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go cancel()
			n, err := b.acquire(ctx, 32)
			if err == nil {
				b.release(n)
			}
		}()
	}
	wg.Wait()
	if b.option(OptionIntQueryWorkersActive) != "0" || b.option(OptionIntQueryWorkersQueued) != "0" {
		t.Fatal("leaked capacity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	n, err := b.acquire(ctx, 32)
	if err != nil || n != 4 {
		t.Fatalf("budget did not recover: %d %v", n, err)
	}
	b.release(n)
}

func TestWorkerBudgetOptions(t *testing.T) {
	d := newDriverImpl(memory.DefaultAllocator)
	for _, value := range []string{"auto", "1", "4", "64"} {
		db, err := d.newDatabaseImpl(context.Background(), map[string]string{OptionStringQueryWorkerBudget: value})
		if err != nil {
			t.Fatal(err)
		}
		want := value
		if value == "auto" {
			want = "1"
		}
		got, err := db.GetOption(context.Background(), OptionStringQueryWorkerBudget)
		if err != nil || got != want {
			t.Fatalf("budget %s: %s %v", value, got, err)
		}
		if _, err := db.GetOption(context.Background(), OptionIntQueryWorkersActive); err != nil {
			t.Fatal(err)
		}
		if err := db.SetOption(context.Background(), OptionStringQueryWorkerBudget, "2"); err != nil {
			t.Fatal(err)
		}
		if got := db.workerBudget.capacity(); got != 2 {
			t.Fatalf("budget override: %d", got)
		}
	}
	for _, value := range []string{"0", "-1", "bad"} {
		if _, err := d.newDatabaseImpl(context.Background(), map[string]string{OptionStringQueryWorkerBudget: value}); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestWorkerBudgetConcurrentLimit(t *testing.T) {
	for _, limit := range []int{1, 4, 16} {
		old := runtime.GOMAXPROCS(limit)
		b := &workerBudget{}
		var active atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				n, err := b.acquire(ctx, 32)
				if err != nil {
					t.Error(err)
					return
				}
				if n < 1 {
					t.Error("admitted query without a worker")
				}
				if got := active.Add(int64(n)); got > int64(limit) {
					t.Errorf("active %d exceeds budget %d", got, limit)
				}
				time.Sleep(time.Millisecond)
				active.Add(-int64(n))
				b.release(n)
			}()
		}
		wg.Wait()
		if b.option(OptionIntQueryWorkersActive) != "0" {
			t.Fatal("leaked workers")
		}
		runtime.GOMAXPROCS(old)
	}
}

func TestWorkerBudgetDeadline(t *testing.T) {
	b := &workerBudget{configured: 1}
	n, _ := b.acquire(context.Background(), 32)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := b.acquire(ctx, 32); err != context.DeadlineExceeded {
		t.Fatalf("deadline: %v", err)
	}
	b.release(n)
	if b.option(OptionIntQueryWorkersQueued) != "0" || b.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatal("deadline leaked capacity")
	}
}
