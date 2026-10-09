//go:build linux || darwin

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
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Opt-in, destructive only to it_worker_profile. Keep other clients idle:
// INFO counters measure the whole Redis server, not just this benchmark.
func TestAggregateWorkerConcurrencyProfile(t *testing.T) {
	rows, err := strconv.Atoi(os.Getenv("REDIS_WORKER_PROFILE_ROWS"))
	if err != nil || rows < 2048 {
		t.Skip("set REDIS_WORKER_PROFILE_ROWS >= 2048")
	}
	rounds := 10
	if value := os.Getenv("REDIS_WORKER_PROFILE_ROUNDS"); value != "" {
		rounds, err = strconv.Atoi(value)
		if err != nil || rounds < 1 {
			t.Fatal("rounds must be positive")
		}
	}
	h := newSQLHarness(t)
	h.dropTables("it_worker_profile")
	h.exec("CREATE TABLE it_worker_profile (v BIGINT, f DOUBLE PRECISION)")
	defer h.dropTables("it_worker_profile")
	client := h.rawClient()
	st := &store{client: client}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_worker_profile")
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
	budget := os.Getenv("REDIS_WORKER_PROFILE_BUDGET")
	if budget == "" {
		budget = "auto"
	}
	ceiling := os.Getenv("REDIS_WORKER_PROFILE_CEILING")
	if ceiling == "" {
		ceiling = "auto"
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringAggregatePushdown: PushdownNone,
		OptionStringQueryWorkers: ceiling, OptionStringQueryWorkerBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	connections := make([]*sqlHarness, 4)
	for i := range connections {
		c, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(h.ctx)
		connections[i] = &sqlHarness{t: t, ctx: h.ctx, conn: c}
	}
	query := "SELECT SUM(v), AVG(f), MIN(v), MAX(v) FROM it_worker_profile"
	want, _ := connections[0].query(query)
	redisMetrics := func() map[string]float64 {
		info, err := client.Info(context.Background(), "stats", "cpu", "memory").Result()
		if err != nil {
			t.Fatal(err)
		}
		result := map[string]float64{}
		for _, line := range strings.Split(info, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ok {
				if n, err := strconv.ParseFloat(value, 64); err == nil {
					result[key] = n
				}
			}
		}
		return result
	}
	cpu := func() float64 {
		var usage syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
			t.Fatal(err)
		}
		return float64(usage.Utime.Nano()+usage.Stime.Nano()) / 1e9
	}
	for _, concurrency := range []int{1, 2, 4} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		peak := before.HeapAlloc
		stop, stopped := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(stopped)
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					var sample runtime.MemStats
					runtime.ReadMemStats(&sample)
					peak = max(peak, sample.HeapAlloc)
				}
			}
		}()
		redisBefore := redisMetrics()
		cpuBefore := cpu()
		started := time.Now()
		latencies := make([]float64, 0, rounds*concurrency)
		history := make([]map[string]any, 0, rounds)
		for round := 0; round < rounds; round++ {
			var wg sync.WaitGroup
			durations := make([]float64, concurrency)
			start := make(chan struct{})
			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					queryStart := time.Now()
					got, _ := connections[i].query(query)
					durations[i] = time.Since(queryStart).Seconds()
					if !reflect.DeepEqual(got, want) {
						t.Errorf("profile result mismatch: %v want %v", got, want)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			latencies = append(latencies, durations...)
			options := db.(adbc.GetSetOptionsWithContext)
			target, _ := options.GetOption(h.ctx, OptionStringQueryWorkerBudget)
			tuning, _ := options.GetOption(h.ctx, OptionStringQueryWorkerTuning)
			var state any
			_ = json.Unmarshal([]byte(tuning), &state)
			roundGrants := make([]string, concurrency)
			for i := range roundGrants {
				roundGrants[i], _ = connections[i].conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
			}
			history = append(history, map[string]any{"round": round, "budget": target, "grants": roundGrants, "tuning": state})
		}
		elapsed := time.Since(started).Seconds()
		clientCPU := cpu() - cpuBefore
		close(stop)
		<-stopped
		runtime.ReadMemStats(&after)
		redisAfter := redisMetrics()
		sort.Float64s(latencies)
		sum := 0.0
		for _, latency := range latencies {
			sum += latency
		}
		resolved, _ := db.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringQueryWorkerBudget)
		grants := make([]string, concurrency)
		for i := range grants {
			grants[i], _ = connections[i].conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
		}
		result := map[string]any{
			"last_worker_grants": grants,
			"allocation_history": history,
			"rows":               rows, "rounds": rounds, "concurrency": concurrency, "GOMAXPROCS": runtime.GOMAXPROCS(0),
			"worker_ceiling": ceiling, "worker_budget": resolved,
			"mean_query_seconds": sum / float64(len(latencies)), "p95_query_seconds": latencies[(len(latencies)*95+99)/100-1],
			"queries_per_second": float64(len(latencies)) / elapsed, "client_cpu_seconds": clientCPU,
			"client_cpu_percent": 100 * clientCPU / elapsed, "client_peak_heap_bytes": peak,
			"client_allocated_bytes":         after.TotalAlloc - before.TotalAlloc,
			"redis_network_bytes_per_second": (redisAfter["total_net_input_bytes"] - redisBefore["total_net_input_bytes"] + redisAfter["total_net_output_bytes"] - redisBefore["total_net_output_bytes"]) / elapsed,
			"redis_commands_per_second":      (redisAfter["total_commands_processed"] - redisBefore["total_commands_processed"]) / elapsed,
			"redis_cpu_seconds":              redisAfter["used_cpu_user"] + redisAfter["used_cpu_sys"] - redisBefore["used_cpu_user"] - redisBefore["used_cpu_sys"],
			"redis_memory_bytes":             redisAfter["used_memory"],
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("WORKER_PROFILE %s", encoded)
	}
}
