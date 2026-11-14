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
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Retrieval attribution only: no SQL, Search, Arrow or native C bridge. Owned
// Software slot required. Keep the workload and every returned source value
// independently checked; wall time and endpoint CPU are not capacity claims.
func TestUnmanagedProjectionConcurrencyProfile(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_CONCURRENCY_PROFILE") != "true" {
		t.Skip("opt-in owned Software concurrency profile")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const prefix = "unmanaged_concurrency_profile:"
	const rows, matched, operations = 100000, 10000, 32
	for start := 1; start <= rows; start += 1000 {
		p := raw.Pipeline()
		for id := start; id < min(start+1000, rows+1); id++ {
			p.HSet(h.ctx, prefix+strconv.Itoa(id), "id", id, "grp", id%16, "amount", id*3, "label", fmt.Sprintf("row-%08d", id), "bench_ordinal", id)
		}
		if _, err := p.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Construct the production reader from a real unmanaged descriptor, retaining
	// actual field/budget computation rather than using an unguarded probe script.
	meta := &tableMeta{Unmanaged: true, KeyRows: true, Columns: []columnMeta{{Name: "id"}, {Name: "grp"}, {Name: "amount"}, {Name: "label"}, {Name: "bench_ordinal"}}}
	type observation struct {
		Mode                                                                         string `json:"mode"`
		Clients, Repeat                                                              int
		LatencyNS                                                                    []int64
		Seconds                                                                      float64
		AllocatedBytes, Allocations                                                  uint64
		EndpointCPUBefore, EndpointCPUAfter, EndpointStatsBefore, EndpointStatsAfter string
		Commands                                                                     map[string]int64
		WaitSeconds                                                                  map[string]float64
		Validated                                                                    bool
	}
	for repeat := -1; repeat < 3; repeat++ {
		modes := []string{"native_hgetall", "guarded_projection"}
		if repeat%2 == 0 {
			modes[0], modes[1] = modes[1], modes[0]
		}
		for _, clients := range []int{1, 4} {
			for _, mode := range modes {
				observer := &unmanagedAggregateProfileHook{commands: map[string]int64{}, wait: map[string]time.Duration{}}
				clientsRaw := make([]goredis.UniversalClient, clients)
				stores := make([]*store, clients)
				for c := 0; c < clients; c++ {
					clientsRaw[c] = h.rawClient(observer)
					stores[c] = &store{client: clientsRaw[c]}
				}
				cpuBefore, _ := raw.Info(h.ctx, "cpu").Result()
				statsBefore, _ := raw.Info(h.ctx, "stats").Result()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				samples := make([]int64, operations)
				errs := make(chan error, clients)
				var wg sync.WaitGroup
				start := make(chan struct{})
				observer.active.Store(true)
				began := time.Now()
				for c := 0; c < clients; c++ {
					wg.Add(1)
					go func(c int) {
						defer wg.Done()
						<-start
						for op := c; op < operations; op += clients {
							lo := 1 + (op*997)%(rows-matched+1)
							keys := make([]string, matched)
							for i := range keys {
								keys[i] = prefix + strconv.Itoa(lo+i)
							}
							started := time.Now()
							var got []aggRow
							var err error
							if mode == "native_hgetall" {
								got, err = stores[c].fetchKeyRows(context.Background(), keys, nil)
							} else {
								reader := newUnmanagedProjectionRead(stores[c], meta, map[string]bool{"bench_ordinal": true, "amount": true})
								got, err = reader.read(context.Background(), keys)
							}
							samples[op] = time.Since(started).Nanoseconds()
							if err != nil {
								errs <- err
								return
							}
							if len(got) != matched {
								errs <- fmt.Errorf("rows %d", len(got))
								return
							}
							var total int64
							for i, row := range got {
								id := lo + i
								if row["bench_ordinal"] != strconv.Itoa(id) || row["amount"] != strconv.Itoa(id*3) || row[keyField] != keys[i] {
									errs <- fmt.Errorf("source values differ at %d", id)
									return
								}
								total += int64(id * 3)
								if mode == "native_hgetall" && (row["id"] != strconv.Itoa(id) || row["grp"] != strconv.Itoa(id%16) || row["label"] != fmt.Sprintf("row-%08d", id)) {
									errs <- fmt.Errorf("full source values differ at %d", id)
									return
								}
							}
							if total != 3*int64(lo+lo+matched-1)*matched/2 {
								errs <- fmt.Errorf("sum %d", total)
								return
							}
						}
					}(c)
				}
				close(start)
				wg.Wait()
				elapsed := time.Since(began)
				observer.active.Store(false)
				runtime.ReadMemStats(&after)
				close(errs)
				for err := range errs {
					t.Fatal(err)
				}
				cpuAfter, _ := raw.Info(h.ctx, "cpu").Result()
				statsAfter, _ := raw.Info(h.ctx, "stats").Result()
				for _, c := range clientsRaw {
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
				}
				b, err := json.Marshal(observation{mode, clients, repeat, samples, elapsed.Seconds(), after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs, cpuBefore, cpuAfter, statsBefore, statsAfter, observer.counts(), observer.seconds(), true})
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("UNMANAGED_CONCURRENCY_PROFILE %s", b)
			}
		}
	}
}
