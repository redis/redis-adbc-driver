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
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
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

// Opt-in, destructive only to it_worker_profile_<rows>. Keep other clients idle:
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
	warmup := 0
	if value := os.Getenv("REDIS_WORKER_PROFILE_WARMUP"); value != "" {
		warmup, err = strconv.Atoi(value)
		if err != nil || warmup < 0 {
			t.Fatal("warmup must be nonnegative")
		}
	}
	mode := os.Getenv("REDIS_WORKER_PROFILE_MODE")
	if mode == "" {
		mode = "waves"
	}
	if mode != "waves" && mode != "continuous" && mode != "server-transition" {
		t.Fatal("mode must be waves, continuous, or server-transition")
	}
	h := newSQLHarness(t)
	table := fmt.Sprintf("it_worker_profile_%d", rows)
	reuse := os.Getenv("REDIS_WORKER_PROFILE_REUSE") == "true"
	client := h.rawClient()
	st := &store{client: client}
	meta, lookupErr := st.getTable(h.ctx, defaultSchema, table)
	create := !reuse || lookupErr != nil
	if create {
		h.dropTables(table)
		h.exec("CREATE TABLE " + table + " (v BIGINT, f DOUBLE PRECISION)")
		meta, err = st.getTable(h.ctx, defaultSchema, table)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reuse {
		defer h.dropTables(table)
	}
	if create {
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
	}
	if sequence, err := client.Get(h.ctx, seqKey(meta.Schema, meta.Name)).Int64(); err != nil || sequence != int64(rows) {
		t.Fatalf("fixture sequence: %d %v", sequence, err)
	}
	ready := time.Now().Add(time.Minute)
	for {
		n, err := st.countMatches(h.ctx, meta.index(), "*")
		if err != nil {
			t.Fatal(err)
		}
		if n == int64(rows) {
			break
		}
		if time.Now().After(ready) {
			t.Fatalf("fixture index has %d rows, want %d", n, rows)
		}
		time.Sleep(10 * time.Millisecond)
	}
	serverInfo, err := client.Info(h.ctx, "server").Result()
	if err != nil {
		t.Fatal(err)
	}
	serverVersion := ""
	for _, line := range strings.Split(serverInfo, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "redis_version:"); ok {
			serverVersion = value
		}
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
	query := "SELECT SUM(v), AVG(f), MIN(v), MAX(v) FROM " + table
	// Verify against an independent serial executor without warming the policy.
	reference, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringAggregatePushdown: PushdownNone, OptionStringQueryWorkers: "1", OptionStringQueryWorkerBudget: "1"})
	if err != nil {
		t.Fatal(err)
	}
	referenceConn, err := reference.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	referenceHarness := &sqlHarness{t: t, ctx: h.ctx, conn: referenceConn}
	want, _ := referenceHarness.query(query)
	if len(want) != 1 {
		t.Fatalf("fixture reference returned %d rows", len(want))
	}
	cells := strings.Split(want[0], "|")
	var expectedSum int64
	for i := 0; i < rows; i++ {
		expectedSum += int64(i % 10001)
	}
	if len(cells) != 4 || cells[0] != strconv.FormatInt(expectedSum, 10) || cells[2] != "0" || cells[3] != strconv.Itoa(min(rows-1, 10000)) {
		t.Fatalf("fixture reference disagrees with independently calculated integer aggregates: %v", want)
	}
	referenceConn.Close(h.ctx)
	reference.Close(h.ctx)
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
	concurrencies := []int{1, 2, 4}
	if value := os.Getenv("REDIS_WORKER_PROFILE_CONCURRENCY"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 4 {
			t.Fatal("concurrency must be 1..4")
		}
		concurrencies = []int{n}
	}
	// The outer runner owns Docker. File handshakes change the isolated
	// server quota at deterministic phase boundaries without embedding Docker
	// access or a timing guess in the client container.
	serverQuota := func(quota int) {
		if mode != "server-transition" {
			return
		}
		directory := os.Getenv("REDIS_WORKER_PROFILE_TRANSITION_DIR")
		if directory == "" {
			t.Fatal("server transition requires a runner handshake directory")
		}
		token := fmt.Sprintf("%d:%d", time.Now().UnixNano(), quota)
		request := filepath.Join(directory, "request")
		ack := filepath.Join(directory, "ack")
		if err := os.WriteFile(request+".tmp", []byte(token), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(request+".tmp", request); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			if data, err := os.ReadFile(ack); err == nil && string(data) == token {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("server quota handshake timed out")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	for _, concurrency := range concurrencies {
		serverQuota(2)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		peak := before.HeapAlloc
		var peakMu sync.Mutex
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
					peakMu.Lock()
					peak = max(peak, sample.HeapAlloc)
					peakMu.Unlock()
				}
			}
		}()
		redisBefore := redisMetrics()
		cpuBefore := cpu()
		started := time.Now()
		latencies := make([]float64, 0, rounds*concurrency)
		coldLatencies := make([]float64, 0, warmup*concurrency)
		var coldCPU, coldElapsed float64
		var coldPeak uint64
		totalStarted := started
		history := make([]map[string]any, 0, rounds)
		var historyMu sync.Mutex
		record := func(round int, durations []float64, roundStarted time.Time) {
			options := db.(adbc.GetSetOptionsWithContext)
			target, _ := options.GetOption(h.ctx, OptionStringQueryWorkerBudget)
			tuning, _ := options.GetOption(h.ctx, OptionStringQueryWorkerTuning)
			var state any
			_ = json.Unmarshal([]byte(tuning), &state)
			roundGrants := make([]string, concurrency)
			for i := range roundGrants {
				roundGrants[i], _ = connections[i].conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
			}
			historyMu.Lock()
			defer historyMu.Unlock()
			if round < warmup {
				coldLatencies = append(coldLatencies, durations...)
			} else {
				latencies = append(latencies, durations...)
			}
			history = append(history, map[string]any{"round": round, "measurement": round >= warmup, "seconds": time.Since(roundStarted).Seconds(), "query_seconds": durations, "budget": target, "grants": roundGrants, "tuning": state})
		}
		execute := func(i int) float64 {
			queryStart := time.Now()
			got, _ := connections[i].query(query)
			duration := time.Since(queryStart).Seconds()
			if !reflect.DeepEqual(got, want) {
				t.Errorf("profile result mismatch: %v want %v", got, want)
			}
			return duration
		}
		runPhase := func(begin, end int) {
			if mode == "continuous" {
				var wg sync.WaitGroup
				start := make(chan struct{})
				for i := 0; i < concurrency; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						<-start
						for round := begin; round < end; round++ {
							queryStart := time.Now()
							record(round, []float64{execute(i)}, queryStart)
						}
					}(i)
				}
				close(start)
				wg.Wait()
				return
			}
			for round := begin; round < end; round++ {
				roundStarted := time.Now()
				var wg sync.WaitGroup
				durations := make([]float64, concurrency)
				start := make(chan struct{})
				for i := 0; i < concurrency; i++ {
					wg.Add(1)
					go func(i int) { defer wg.Done(); <-start; durations[i] = execute(i) }(i)
				}
				close(start)
				wg.Wait()
				record(round, durations, roundStarted)
			}
		}
		if warmup > 0 {
			runPhase(0, warmup)
			coldElapsed = time.Since(totalStarted).Seconds()
			coldCPU = cpu() - cpuBefore
			peakMu.Lock()
			coldPeak = peak
			runtime.ReadMemStats(&before)
			peak = before.HeapAlloc
			peakMu.Unlock()
			serverQuota(1)
			redisBefore = redisMetrics()
			cpuBefore = cpu()
			started = time.Now()
		}
		runPhase(warmup, warmup+rounds)

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
		var usage syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
		rss := uint64(usage.Maxrss)
		if runtime.GOOS == "linux" {
			rss *= 1024
		}
		var cold any
		if warmup > 0 {
			slices.Sort(coldLatencies)
			cold = map[string]any{"rounds": warmup, "elapsed_seconds": coldElapsed, "queries_per_second": float64(len(coldLatencies)) / coldElapsed, "p95_query_seconds": coldLatencies[(len(coldLatencies)*95+99)/100-1], "client_cpu_seconds": coldCPU, "peak_heap_bytes": coldPeak}
		}
		result := map[string]any{
			"last_worker_grants": grants,
			"cold_learning":      cold, "warmup_rounds": warmup, "elapsed_seconds": elapsed, "client_process_peak_rss_bytes": rss, "rss_scope": "process lifetime including setup and warmup",
			"allocation_history": history, "arrival_mode": mode, "redis_server_version": serverVersion,
			"rows": rows, "rounds": rounds, "concurrency": concurrency, "GOMAXPROCS": runtime.GOMAXPROCS(0), "go_version": runtime.Version(),
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
