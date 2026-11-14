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

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
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

// Actual Go ADBC SQL execute and complete typed Arrow fetch. This is still not
// the Python/C-boundary native scenario matrix. The source, query shape and
// independent oracle match its 100K fixture and 10K filtered aggregate.
func TestUnmanagedProjectionSQLConcurrencyProfile(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_SQL_CONCURRENCY_PROFILE") != "true" {
		t.Skip("opt-in owned Software full SQL concurrency profile")
	}
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	const prefix, index, table = "unmanaged_sql_concurrency:", "unmanaged_sql_concurrency_idx", "unmanaged_sql_concurrency"
	const rows, matched, operations = 100000, 10000, 32
	if err := raw.Do(h.ctx, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "bench_ordinal", "NUMERIC", "SORTABLE", "id", "NUMERIC", "grp", "NUMERIC", "amount", "NUMERIC", "label", "TAG").Err(); err != nil {
		t.Fatal(err)
	}
	for start := 1; start <= rows; start += 1000 {
		p := raw.Pipeline()
		for id := start; id < min(start+1000, rows+1); id++ {
			p.HSet(h.ctx, prefix+strconv.Itoa(id), "id", id, "grp", id%16, "amount", id*3, "label", fmt.Sprintf("row-%08d", id), "bench_ordinal", id)
		}
		if _, err := p.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, index); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Mode: "unmanaged", Prefix: prefix, Index: index, Table: table, Types: map[string]string{"id": "BIGINT", "grp": "BIGINT", "amount": "BIGINT", "label": "VARCHAR", "bench_ordinal": "BIGINT"}, Encodings: map[string]string{"id": "native", "grp": "native", "amount": "native", "label": "native", "bench_ordinal": "native"}, Apply: true}); err != nil {
		t.Fatal(err)
	}
	for repeat := -1; repeat < 3; repeat++ {
		for _, clients := range []int{1, 4} {
			observer := &unmanagedAggregateProfileHook{commands: map[string]int64{}, wait: map[string]time.Duration{}}
			conns := make([]adbc.ConnectionWithContext, clients)
			stmts := make([]adbc.StatementWithContext, clients)
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
				st.(*statementImpl).conn.store.client.AddHook(observer)
			}
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
						query := fmt.Sprintf("SELECT CAST(COUNT(*) AS BIGINT),CAST(SUM(amount) AS BIGINT) FROM %s WHERE bench_ordinal BETWEEN %d AND %d", table, lo, lo+matched-1)
						started := time.Now()
						if err := stmts[c].SetSqlQuery(h.ctx, query); err != nil {
							errs <- err
							return
						}
						reader, _, err := stmts[c].ExecuteQuery(h.ctx)
						if err != nil {
							errs <- err
							return
						}
						var gotRows int
						var n, total int64
						var resultErr error
						for reader.Next() {
							rec := reader.RecordBatch()
							if rec.NumCols() != 2 || rec.Schema().Field(0).Type.ID() != arrow.INT64 || rec.Schema().Field(1).Type.ID() != arrow.INT64 {
								resultErr = fmt.Errorf("full SQL physical result types differ")
								break
							}
							for i := 0; i < int(rec.NumRows()); i++ {
								if rec.Column(0).IsNull(i) || rec.Column(1).IsNull(i) {
									resultErr = fmt.Errorf("unexpected NULL SQL aggregate")
									break
								}
								gotRows++
								n = rec.Column(0).(*array.Int64).Value(i)
								total = rec.Column(1).(*array.Int64).Value(i)
							}
						}
						if err = reader.Err(); resultErr == nil {
							resultErr = err
						}
						reader.Release()
						samples[op] = time.Since(started).Nanoseconds()
						if resultErr != nil {
							errs <- resultErr
							return
						}
						if gotRows != 1 || n != matched || total != 3*int64(lo+lo+matched-1)*matched/2 {
							errs <- fmt.Errorf("independent aggregate oracle differs rows=%d count=%d sum=%d", gotRows, n, total)
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
			for c := range conns {
				if err := stmts[c].Close(h.ctx); err != nil {
					t.Fatal(err)
				}
				if err := conns[c].Close(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			b, err := json.Marshal(map[string]any{"scope": "Go ADBC SQL execute and complete typed Arrow fetch; workerless fixture; no Python/C bridge; command wait includes pool/decode, not server CPU", "clients": clients, "repeat": repeat, "warmup": repeat < 0, "operations": operations, "fixture_rows": rows, "matched_rows": matched, "latency_ns": samples, "seconds": elapsed.Seconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "commands": observer.counts(), "wait_seconds": observer.seconds(), "validated": true})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("UNMANAGED_SQL_CONCURRENCY_PROFILE %s", b)
		}
	}
}
