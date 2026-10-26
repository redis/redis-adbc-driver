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

// An opt-in allocation/heap profile on an already loaded benchmark table.
// Run each query in a fresh test process, outside the integration suite.
import (
	"encoding/json"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestAggregatePagesProfile(t *testing.T) {
	sql := os.Getenv("REDIS_AGG_PROFILE_QUERY")
	if sql == "" {
		t.Skip("set REDIS_AGG_PROFILE_QUERY to profile an existing test fixture")
	}
	h := newSQLHarness(t)
	e := &executor{store: &store{client: h.rawClient()}, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC()}
	parsed, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(h.ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapAlloc)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for prev := peak.Load(); m.HeapAlloc > prev; prev = peak.Load() {
					if peak.CompareAndSwap(prev, m.HeapAlloc) {
						break
					}
				}
			}
		}
	}()
	started := time.Now()
	rows, err := e.runSelect(h.ctx, plan, nil)
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if after.HeapAlloc > peak.Load() {
		peak.Store(after.HeapAlloc)
	}
	runtime.GC()
	var retained runtime.MemStats
	runtime.ReadMemStats(&retained)
	runtime.KeepAlive(rows)
	b, _ := json.Marshal(map[string]any{
		"seconds": elapsed.Seconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc,
		"allocations": after.Mallocs - before.Mallocs, "sampled_peak_heap_bytes": peak.Load(),
		"heap_before_bytes": before.HeapAlloc, "retained_result_heap_bytes": retained.HeapAlloc,
		"result_rows": len(rows), "query": sql,
	})
	t.Logf("PROFILE %s", b)
}
