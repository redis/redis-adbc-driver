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

	goredis "github.com/redis/go-redis/v9"
)

// Covers the all-NULL fallback cost separately from the dense 100K-row SQL
// fixture. This is source retrieval only, with independently checked complete
// images; it is not SQL/Arrow/C-boundary latency or server CPU attribution.
func TestUnmanagedProjectionSparseProfile(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_SPARSE_PROFILE") != "true" {
		t.Skip("opt-in owned Software sparse source profile")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const rows, operations = 256, 8
	const prefix = "unmanaged_sparse_profile:"
	meta := &tableMeta{Unmanaged: true, KeyRows: true, Columns: []columnMeta{{Name: "n"}, {Name: "text"}}}
	nul := string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(nul, 0) {
		t.Fatal("fixture lacks NUL")
	}
	wide := strings.Repeat("w", 16<<10)
	keys := make([]string, rows+2)
	for i := range keys {
		keys[i] = prefix + strconv.Itoa(i)
	}
	for _, shape := range []string{"dense", "sparse90", "all_null", "sparse90_wide"} {
		expected := make([]aggRow, len(keys))
		nonNull := 0
		p := raw.Pipeline()
		p.Del(h.ctx, keys...)
		for i, key := range keys[:rows] {
			source := map[string]any{"other": fmt.Sprintf("source-%d", i)}
			row := aggRow{keyField: key, "other": fmt.Sprintf("source-%d", i)}
			if shape == "dense" || shape != "all_null" && i%10 == 0 {
				nonNull++
				source["n"], source["text"] = "9007199254740993", nul
				row["n"], row["text"] = "9007199254740993", nul
			}
			if shape == "sparse90_wide" {
				source["payload"], row["payload"] = wide, wide
			}
			expected[i] = row
			p.HSet(h.ctx, key, source)
		}
		p.Set(h.ctx, keys[rows], "wrong type", 0) // final key remains absent
		if _, err := p.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
		for repeat := -1; repeat < 3; repeat++ {
			modes := []string{"native_hgetall", "projected_fields"}
			if repeat%2 == 0 {
				modes[0], modes[1] = modes[1], modes[0]
			}
			for _, clients := range []int{1, 4} {
				for _, mode := range modes {
					observer := &unmanagedAggregateProfileHook{commands: map[string]int64{}, wait: map[string]time.Duration{}}
					readers := make([]*unmanagedProjectionRead, clients)
					clientsRaw := make([]goredis.UniversalClient, clients)
					for c := range readers {
						clientsRaw[c] = h.rawClient(observer)
						readers[c] = newUnmanagedProjectionRead(&store{client: clientsRaw[c]}, meta, map[string]bool{"n": true, "text": true})
					}
					samples := make([]int64, operations)
					errs := make(chan error, clients)
					var wg sync.WaitGroup
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					start := make(chan struct{})
					observer.active.Store(true)
					began := time.Now()
					for c := range readers {
						wg.Add(1)
						go func(c int) {
							defer wg.Done()
							<-start
							for op := c; op < operations; op += clients {
								// A production reader is scan-local; start a fresh scan each operation.
								reader := *readers[c]
								started := time.Now()
								var got []aggRow
								var err error
								if mode == "native_hgetall" {
									got, err = reader.native(context.Background(), keys)
								} else {
									got, err = reader.read(context.Background(), keys)
								}
								samples[op] = time.Since(started).Nanoseconds()
								if err != nil {
									errs <- err
									return
								}
								if len(got) != len(expected) {
									errs <- fmt.Errorf("row count %d", len(got))
									return
								}
								for i, row := range got {
									want := expected[i]
									if (row == nil) != (want == nil) {
										errs <- fmt.Errorf("existence differs at %d", i)
										return
									}
									if want == nil {
										continue
									}
									// Projection emits only selected values for non-NULL rows. A later
									// fallback must emit the complete HGETALL image, including payload.
									complete := mode == "native_hgetall" || want["n"] == ""
									count := len(want)
									if !complete {
										count = 3
									}
									if len(row) != count {
										errs <- fmt.Errorf("image width differs at %d: %d != %d", i, len(row), count)
										return
									}
									for field, value := range want {
										if complete || field == keyField || field == "n" || field == "text" {
											if actual, ok := row[field]; !ok || actual != value {
												errs <- fmt.Errorf("source image differs at %d field %s", i, field)
												return
											}
										}
									}
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
						t.Error(err)
					}
					for _, client := range clientsRaw {
						if err := client.Close(); err != nil {
							t.Error(err)
						}
					}
					counts := observer.counts()
					wantHMGET, wantHGETALL := int64(operations*len(keys)), int64(operations*(rows-nonNull+1))
					if mode == "native_hgetall" {
						wantHMGET, wantHGETALL = 0, int64(operations*len(keys))
					}
					if counts["hmget"] != wantHMGET || counts["hgetall"] != wantHGETALL {
						t.Errorf("source commands differ: %v, want HMGET %d HGETALL %d", counts, wantHMGET, wantHGETALL)
					}
					if t.Failed() {
						t.FailNow()
					}
					record := map[string]any{"scope": "native source retrieval only; no SQL/Search/Arrow/C bridge; command wait includes pool/decode", "shape": shape, "mode": mode, "clients": clients, "repeat": repeat, "warmup": repeat < 0, "operations": operations, "hash_rows": rows, "non_null_selected_rows": nonNull, "wrong_type_keys": 1, "missing_keys": 1, "wide_unselected_bytes": 0, "latency_ns": samples, "seconds": elapsed.Seconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "commands": observer.counts(), "wait_seconds": observer.seconds(), "validated": true}
					if shape == "sparse90_wide" {
						record["wide_unselected_bytes"] = len(wide)
					}
					b, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("UNMANAGED_SPARSE_PROFILE %s", b)
				}
			}
		}
	}
}
