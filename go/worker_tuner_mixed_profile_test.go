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
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Opt-in safety integration check against the controlled runner's existing
// fixture shape. Seed a temporary candidate so the scenario does not depend on
// statistical learning luck. This checks SQL/admission/fallback, not readiness
// throughput. It never runs as part of the frozen performance matrix.
func TestAggregateWorkerMixedProbe(t *testing.T) {
	rows, err := strconv.Atoi(os.Getenv("REDIS_WORKER_PROFILE_MIXED_ROWS"))
	if err != nil || rows < 8192 {
		t.Skip("set REDIS_WORKER_PROFILE_MIXED_ROWS >= 8192 on an isolated disposable server")
	}
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	h := newSQLHarness(t)
	table := fmt.Sprintf("it_worker_mixed_%d", rows)
	h.dropTables(table)
	defer h.dropTables(table)
	h.exec("CREATE TABLE " + table + " (v BIGINT, f DOUBLE PRECISION)")
	client := h.rawClient()
	store := &store{client: client}
	meta, err := store.getTable(h.ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < rows; start += pipelineChunk {
		pipe := client.Pipeline()
		for i := start; i < min(start+pipelineChunk, rows); i++ {
			pipe.HSet(h.ctx, fmt.Sprint(meta.prefix(), i+1), rowIDField, i+1, "v", i%10001, "f", fmt.Sprint(float64(i%997)/7))
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Set(h.ctx, seqKey(meta.Schema, meta.Name), rows, 0).Err(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	for {
		n, err := store.countMatches(h.ctx, meta.index(), "*")
		if err != nil {
			t.Fatal(err)
		}
		if n == int64(rows) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mixed fixture indexing timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	queries := []string{"SELECT SUM(v), AVG(f), MIN(v), MAX(v) FROM " + table,
		"SELECT SUM(v), AVG(f), MIN(v), MAX(v) FROM " + table + " WHERE v >= 1"}
	reference, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringAggregatePushdown: PushdownNone,
		OptionStringQueryWorkers: "1", OptionStringQueryWorkerBudget: "1"})
	if err != nil {
		t.Fatal(err)
	}
	refConn, err := reference.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ref := &sqlHarness{t: t, ctx: h.ctx, conn: refConn}
	wants := make([][]string, len(queries))
	for i, query := range queries {
		wants[i], _ = ref.query(query)
	}
	refConn.Close(h.ctx)
	reference.Close(h.ctx)
	db, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringAggregatePushdown: PushdownNone})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	connections := make([]*sqlHarness, 2)
	for i := range connections {
		conn, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(h.ctx)
		connections[i] = &sqlHarness{t: t, ctx: h.ctx, conn: conn}
	}
	b := db.workerBudget
	b.mu.Lock()
	b.tuner = workerTuner{current: 3, cpu: 2, best: 2, phase: "probe", cooldown: tuningHold}
	b.mu.Unlock()
	history := make([]any, 0, 24)
	started := time.Now()
	for round := 0; round < 24; round++ {
		var wg sync.WaitGroup
		barrier := make(chan struct{})
		for i := range connections {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-barrier
				got, _ := connections[i].query(queries[i])
				if !reflect.DeepEqual(got, wants[i]) {
					t.Errorf("mixed result: %v want %v", got, wants[i])
				}
			}(i)
		}
		close(barrier)
		wg.Wait()
		if b.option(OptionIntQueryWorkersActive) != "0" || b.option(OptionIntQueryWorkersQueued) != "0" {
			t.Fatal("mixed round leaked grants")
		}
		var state any
		if err := json.Unmarshal([]byte(b.option(OptionStringQueryWorkerTuning)), &state); err != nil {
			t.Fatal(err)
		}
		history = append(history, state)
	}
	b.mu.Lock()
	current, phase, pending := b.tuner.current, b.tuner.phase, b.cohort.pending
	b.mu.Unlock()
	if current != 2 || phase == "probe" || phase == "verify" || pending != 0 {
		t.Fatalf("persistent mixed candidate: current=%d phase=%s pending=%d", current, phase, pending)
	}
	encoded, err := json.Marshal(map[string]any{"rows": rows, "rounds": 24, "queries": 48, "elapsed_seconds": time.Since(started).Seconds(), "allocation_history": history,
		"note": "Injected candidate for a safety check; not a performance-readiness benchmark."})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MIXED_PROBE %s", encoded)
}
