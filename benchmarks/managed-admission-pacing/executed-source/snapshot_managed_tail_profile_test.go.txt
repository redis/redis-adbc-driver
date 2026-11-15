// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type managedTailOperationKey struct{}
type managedTailCall struct {
	Category         string `json:"category"`
	StartNS          int64  `json:"start_ns"`
	ElapsedNS        int64  `json:"elapsed_ns"`
	Commands         int    `json:"commands"`
	Failed           bool   `json:"failed,omitempty"`
	AdmissionGranted bool   `json:"admission_granted,omitempty"`
}
type managedTailSample struct {
	Writer                  int    `json:"writer"`
	Operation               int    `json:"operation"`
	Kind                    string `json:"kind"`
	StartNS                 int64  `json:"start_ns"`
	ElapsedNS               int64  `json:"elapsed_ns"`
	AdmissionNS             int64  `json:"admission_ns"`
	AdmissionCommandNS      int64  `json:"admission_command_ns"`
	AdmissionOffTransportNS int64  `json:"admission_off_transport_ns"`
	AcquireAttempts         int    `json:"acquire_attempts"`
	GoGCCompletedPauseNS    int64  `json:"go_gc_completed_pause_ns"`
	Error                   bool   `json:"error"`
	SQLState                string `json:"sqlstate,omitempty"`
	mu                      sync.Mutex
	Calls                   []managedTailCall `json:"calls"`
}

// Context attribution avoids a shared mutable "current operation" while writers
// overlap. Maintenance uses a separate hook and never inherits an operation.
// Background pin-release contexts are not attributed to foreground operations.
type managedTailHook struct {
	epoch       time.Time
	maintenance bool
	mu          sync.Mutex
	calls       []managedTailCall
	commands    map[string]int
}

func (*managedTailHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *managedTailHook) observe(ctx context.Context, started time.Time, cmds []goredis.Cmder, err error) {
	categories := make([]string, len(cmds))
	granted := false
	for i, cmd := range cmds {
		categories[i] = compactionProfileCategory(cmd)
		if categories[i] == "control_admit_ingest" && cmd.Err() == nil {
			if c, ok := cmd.(interface{ Int() (int, error) }); ok {
				n, e := c.Int()
				granted = e == nil && n == 1
			}
		}
	}
	h.mu.Lock()
	if h.commands == nil {
		h.commands = map[string]int{}
	}
	for _, category := range categories {
		h.commands[category]++
	}
	h.mu.Unlock()
	category := compactionProfilePipelineShape(categories)
	call := managedTailCall{Category: category, StartNS: started.Sub(h.epoch).Nanoseconds(), ElapsedNS: time.Since(started).Nanoseconds(), Commands: len(cmds), Failed: err != nil, AdmissionGranted: granted}
	if sample, ok := ctx.Value(managedTailOperationKey{}).(*managedTailSample); ok && !h.maintenance {
		sample.mu.Lock()
		sample.Calls = append(sample.Calls, call)
		sample.mu.Unlock()
	} else if h.maintenance {
		h.mu.Lock()
		h.calls = append(h.calls, call)
		h.mu.Unlock()
	}
}
func (h *managedTailHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		started := time.Now()
		err := next(ctx, cmd)
		h.observe(ctx, started, []goredis.Cmder{cmd}, err)
		return err
	}
}
func (h *managedTailHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		started := time.Now()
		err := next(ctx, cmds)
		h.observe(ctx, started, cmds, err)
		return err
	}
}
func (s *managedTailSample) summarize() {
	for _, c := range s.Calls {
		if c.Category == "control_acquire" {
			s.AcquireAttempts++
		}
		if c.AdmissionGranted {
			s.AdmissionNS = c.StartNS + c.ElapsedNS - s.StartNS
			break
		}
	}
	for _, c := range s.Calls {
		if s.AdmissionNS > 0 && c.StartNS >= s.StartNS && c.StartNS+c.ElapsedNS <= s.StartNS+s.AdmissionNS {
			s.AdmissionCommandNS += c.ElapsedNS
		}
	}
	s.AdmissionOffTransportNS = max(0, s.AdmissionNS-s.AdmissionCommandNS)
}

// Full SQL execute/ack diagnostic, including admission, publication and cleanup.
// This is Go ADBC, not Python/C-boundary latency or Redis server CPU. All writers
// share one logical table; each changes disjoint rows without caller retries.
func TestManagedInterleavedWriterTailProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_TAIL_PROFILE") != "true" {
		t.Skip("opt-in owned Software managed writer tail profile")
	}
	writes := 144
	if value := os.Getenv("REDIS_MANAGED_TAIL_WRITES"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 48 || n > 1440 || n%24 != 0 {
			t.Fatal("writes must be a multiple of 24 in [48,1440]")
		}
		writes = n
	}
	for trial := -1; trial < 3; trial++ {
		order := []int{1, 4, 8}
		rotation := (trial + 1) % 3
		order = append(order[rotation:], order[:rotation]...)
		for _, writers := range order {
			operations := writes / writers
			t.Run(fmt.Sprintf("trial_%d_writers_%d", trial, writers), func(t *testing.T) {
				h, _, db := newSnapshotHarnessMode(t, false, false)
				h.exec("CREATE TABLE managed_tail(id BIGINT,grp BIGINT,amount BIGINT,label VARCHAR)")
				h.exec("INSERT INTO managed_tail SELECT i,i%16,i*3,'row-'||i FROM generate_series(1,1000) AS g(i)")
				epoch := time.Now()
				foreground := &managedTailHook{epoch: epoch}
				maintenance := &managedTailHook{epoch: epoch, maintenance: true}
				conns := make([]adbc.ConnectionWithContext, writers)
				stmts := make([]adbc.StatementWithContext, writers)
				for c := range conns {
					conn, err := db.Open(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					conns[c] = conn
					t.Cleanup(func() { _ = conn.Close(h.ctx) })
					st, err := conn.NewStatement(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					stmts[c] = st
					t.Cleanup(func() { _ = st.Close(h.ctx) })
					impl := st.(*statementImpl).conn
					impl.store.client.AddHook(foreground)
					// Pre-create an idle pool for hooks; each ExecuteUpdate still runs
					// normal executionContext registration and transport.finish.
					pool := impl.newExecutionPool(impl.timeouts)
					pool.client.AddHook(foreground)
					impl.store.idleExecution = pool
					// Database was opened with workers disabled so hooks are installed before
					// any worker uses its client. Each writer retains its production worker.
					impl.store.startSnapshotCompactor(impl.clientCfg, impl.timeouts, maintenance)
				}
				samples := make([]*managedTailSample, writers*operations)
				errs := make(chan error, writers)
				start := make(chan struct{})
				var wg sync.WaitGroup
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				wallEpoch := time.Now()
				began := wallEpoch.Sub(epoch).Nanoseconds()
				for writer := 0; writer < writers; writer++ {
					wg.Add(1)
					go func(writer int) {
						defer wg.Done()
						<-start
						for op := 0; op < operations; op++ {
							step := op / 3
							id := writer*(operations/3) + step + 1
							sample := &managedTailSample{Writer: writer, Operation: op, Kind: []string{"insert", "update", "delete"}[op%3]}
							samples[writer*operations+op] = sample
							sql := ""
							switch op % 3 {
							case 0:
								key := 1000 + id
								sql = fmt.Sprintf("INSERT INTO managed_tail VALUES(%d,%d,%d,'row-%d')", key, key%16, key*3, key)
							case 1:
								sql = fmt.Sprintf("UPDATE managed_tail SET amount=%d WHERE __rowid=%d", -id, id)
							case 2:
								sql = fmt.Sprintf("DELETE FROM managed_tail WHERE __rowid=%d", 500+id)
							}
							ctx := context.WithValue(h.ctx, managedTailOperationKey{}, sample)
							started := time.Now()
							sample.StartNS = started.Sub(epoch).Nanoseconds()
							err := stmts[writer].SetSqlQuery(ctx, sql)
							var affected int64
							if err == nil {
								affected, err = stmts[writer].ExecuteUpdate(ctx)
							}
							sample.ElapsedNS = time.Since(started).Nanoseconds()
							sample.Error = err != nil || affected != 1
							sample.SQLState = sqlState(err)
							sample.summarize()
							if err != nil || affected != 1 {
								errs <- fmt.Errorf("writer %d operation %d failed=%t sqlstate=%s affected=%d", writer, op, err != nil, sample.SQLState, affected)
								return
							}
						}
					}(writer)
				}
				close(start)
				wg.Wait()
				elapsed := time.Since(wallEpoch)
				runtime.ReadMemStats(&after)
				close(errs)
				for err := range errs {
					t.Error(err)
				}
				// Capture all observed failures and samples before reporting a test failure;
				// never discard a trial, retry SQL, or pretend a conflict was successful.
				pauses := min(uint32(len(after.PauseNs)), after.NumGC-before.NumGC)
				for i := uint32(0); i < pauses; i++ {
					idx := (after.NumGC - 1 - i) % uint32(len(after.PauseNs))
					end := int64(after.PauseEnd[idx]) - wallEpoch.UnixNano() + began
					for _, sample := range samples {
						if sample != nil && end >= sample.StartNS && end <= sample.StartNS+sample.ElapsedNS {
							sample.GoGCCompletedPauseNS += int64(after.PauseNs[idx])
						}
					}
				}
				maintenance.mu.Lock()
				calls := append([]managedTailCall(nil), maintenance.calls...)
				maintenanceCounts := make(map[string]int, len(maintenance.commands))
				for key, n := range maintenance.commands {
					maintenanceCounts[key] = n
				}
				maintenance.mu.Unlock()
				foreground.mu.Lock()
				foregroundCounts := make(map[string]int, len(foreground.commands))
				for key, n := range foreground.commands {
					foregroundCounts[key] = n
				}
				foreground.mu.Unlock()
				payload, err := json.Marshal(map[string]any{"scope": "Go SQL execute/ack; shared table; disjoint rows; maintenance overlap is not causal attribution; client elapsed is not server CPU", "trial": trial, "warmup": trial < 0, "writers": writers, "operations_per_writer": operations, "seed_rows": 1000, "caller_retries": 0, "elapsed_ns": elapsed.Nanoseconds(), "samples": samples, "maintenance_calls": calls, "foreground_command_counts": foregroundCounts, "maintenance_command_counts": maintenanceCounts, "go_gc_cycles": after.NumGC - before.NumGC, "go_gc_definition": "sum of STW pause durations for GC cycles completed during each operation; association, not exact pause overlap or concurrent GC CPU", "go_gc_pause_ring_complete": after.NumGC-before.NumGC <= uint32(len(after.PauseNs)), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "admission_definition": "operation start through successful admit_ingest; off-transport includes timer sleep, Go work and scheduling; pin release uses background context and is excluded from command attribution"})
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), "snapshot:17:rows:") {
					t.Fatal("private identity in diagnostic")
				}
				t.Logf("MANAGED_WRITER_TAIL_PROFILE %s", payload)
				if t.Failed() {
					return
				}
				for c := range conns {
					if err := stmts[c].Close(h.ctx); err != nil {
						t.Fatal(err)
					}
					if err := conns[c].Close(h.ctx); err != nil {
						t.Fatal(err)
					}
				}
				// Independent complete result oracle after timing, including exact values.
				rows, schema := h.query("SELECT id,grp,amount,label FROM managed_tail ORDER BY id")
				changed := writers * (operations / 3)
				var want []string
				for id := 1; id <= 1000+changed; id++ {
					if id > 500 && id <= 500+changed {
						continue
					}
					amount := id * 3
					if id <= changed {
						amount = -id
					}
					want = append(want, fmt.Sprintf("%d|%d|%d|row-%d", id, id%16, amount, id))
				}
				if schema.NumFields() != 4 || schema.Field(0).Type.String() != "int64" || schema.Field(1).Type.String() != "int64" || schema.Field(2).Type.String() != "int64" || schema.Field(3).Type.String() != "utf8" || len(rows) != len(want) {
					t.Fatal("final result shape differs")
				}
				for i := range want {
					if rows[i] != want[i] {
						t.Fatalf("final row %d differs", i)
					}
				}
				t.Logf("MANAGED_WRITER_TAIL_VALIDATED trial=%d writers=%d writes=%d", trial, writers, writes)
			})
		}
	}
}

func TestManagedTailProfileAttribution(t *testing.T) {
	epoch := time.Now().Add(-time.Second)
	hook := &managedTailHook{epoch: epoch}
	var wg sync.WaitGroup
	samples := make([]*managedTailSample, 8)
	for i := range samples {
		samples[i] = &managedTailSample{Writer: i, StartNS: time.Since(epoch).Nanoseconds()}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.WithValue(context.Background(), managedTailOperationKey{}, samples[i])
			acquire := goredis.NewCmd(ctx, "eval", snapshotControl, 1, "private-control", "acquire")
			admit := goredis.NewCmd(ctx, "eval", snapshotControl, 1, "private-control", "admit_ingest")
			run := hook.ProcessHook(func(_ context.Context, cmd goredis.Cmder) error {
				cmd.(interface{ SetVal(any) }).SetVal(int64(1))
				return nil
			})
			_ = run(ctx, acquire)
			_ = run(ctx, onceCmd{admit})
			samples[i].summarize()
		}(i)
	}
	wg.Wait()
	for i, sample := range samples {
		if sample.Writer != i || len(sample.Calls) != 2 || sample.AcquireAttempts != 1 || sample.AdmissionNS <= 0 || sample.AdmissionCommandNS <= 0 || sample.AdmissionOffTransportNS != sample.AdmissionNS-sample.AdmissionCommandNS {
			t.Fatalf("operation %d attribution differs: %+v", i, sample)
		}
	}
	maintenance := &managedTailHook{epoch: epoch, maintenance: true}
	ctx := context.WithValue(context.Background(), managedTailOperationKey{}, samples[0])
	maintenance.observe(ctx, time.Now(), []goredis.Cmder{goredis.NewCmd(ctx, "ft.info", "private-index")}, nil)
	if len(samples[0].Calls) != 2 || len(maintenance.calls) != 1 || maintenance.calls[0].Category != "ft.info" {
		t.Fatal("maintenance leaked into foreground attribution")
	}
	payload, err := json.Marshal(maintenance.calls)
	if err != nil || strings.Contains(string(payload), "private-index") {
		t.Fatal("command arguments leaked")
	}
}
