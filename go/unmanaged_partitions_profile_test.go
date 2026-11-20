// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

// Opt-in synthetic comparison for #549; not ClickBench or a scaling guarantee.
import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestUnmanagedPartitionProfile(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_PARTITION_PROFILE") != "true" {
		t.Skip("opt-in owned Software unmanaged partition profile")
	}
	if os.Getenv("REDIS_FLEX_BENCH_CONTAINER") == "" {
		t.Fatal("owned Software container required for process counters")
	}
	const count = 4096
	for trial := 0; trial < 3; trial++ {
		order := []int{0, 1, 4}
		if trial%2 == 1 {
			order = []int{4, 1, 0}
		}
		for _, workers := range order {
			t.Run(fmt.Sprintf("trial=%d/workers=%d", trial, workers), func(t *testing.T) {
				h, raw, db := newSnapshotHarnessMode(t, false, false)
				for start := 0; start < count; start += 128 {
					pipe := raw.Pipeline()
					for i := start; i < min(start+128, count); i++ {
						pipe.HSet(h.ctx, fmt.Sprintf("app:%d", i), "n", i, "v", int64(9007199254740993)+int64(i)*7, "s", fmt.Sprintf("row-%d", i), "unknown", "unmodeled application data")
					}
					if _, err := pipe.Exec(h.ctx); err != nil {
						t.Fatal(err)
					}
				}
				if err := raw.Do(h.ctx, "FT.CREATE", "profile_source", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC", "v", "NUMERIC", "s", "TAG").Err(); err != nil {
					t.Fatal(err)
				}
				if err := (&store{client: raw}).awaitIndexReady(h.ctx, "profile_source"); err != nil {
					t.Fatal(err)
				}
				setter := h.conn.(adbc.GetSetOptionsWithContext)
				if err := setter.SetOption(h.ctx, OptionStringForeignIndexesMode, "unmanaged"); err != nil {
					t.Fatal(err)
				}
				if err := setter.SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
				query := "SELECT CAST(__key AS BYTEA),n,v,s FROM redis_foreign.profile_source WHERE n%3=0"
				expected := map[string]unmanagedPartitionTestRow{}
				for i := 0; i < count; i += 3 {
					key := fmt.Sprintf("app:%d", i)
					expected[key] = unmanagedPartitionTestRow{[]byte(key), strconv.Itoa(i), strconv.FormatInt(9007199254740993+int64(i)*7, 10), fmt.Sprintf("row-%d", i)}
				}
				stmt, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer stmt.Close(h.ctx)
				if err := stmt.SetSqlQuery(h.ctx, query); err != nil {
					t.Fatal(err)
				}
				var parts adbc.Partitions
				var discovery time.Duration
				var connections []adbc.ConnectionWithContext
				if workers > 0 {
					if err := stmt.SetOption(h.ctx, OptionStringPartitionSourceMode, "unmanaged"); err != nil {
						t.Fatal(err)
					}
					if err := stmt.SetOption(h.ctx, OptionIntPartitionCount, strconv.Itoa(workers)); err != nil {
						t.Fatal(err)
					}
					start := time.Now()
					_, parts, _, err = stmt.ExecutePartitions(h.ctx)
					discovery = time.Since(start)
					if err != nil {
						t.Fatal(err)
					}
					for range parts.PartitionIDs {
						conn, err := db.Open(h.ctx)
						if err != nil {
							t.Fatal(err)
						}
						defer conn.Close(h.ctx)
						connections = append(connections, conn)
					}
				}
				serverBefore := flexBenchServerStats(t)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				cpu, _ := flexBenchCPU()
				start := time.Now()
				var rows []unmanagedPartitionTestRow
				if workers == 0 {
					reader, _, err := stmt.ExecuteQuery(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					for reader.Next() {
						rec := reader.RecordBatch()
						keys := rec.Column(0).(*array.Binary)
						for i := 0; i < int(rec.NumRows()); i++ {
							rows = append(rows, unmanagedPartitionTestRow{append([]byte(nil), keys.Value(i)...), unmanagedPartitionValue(rec.Column(1), i), unmanagedPartitionValue(rec.Column(2), i), unmanagedPartitionValue(rec.Column(3), i)})
						}
					}
					err = reader.Err()
					reader.Release()
					if err != nil {
						t.Fatal(err)
					}
				} else {
					type result struct {
						rows []unmanagedPartitionTestRow
						err  error
					}
					done := make(chan result, len(parts.PartitionIDs))
					for i, part := range parts.PartitionIDs {
						go func() { got, err := unmanagedPartitionRows(connections[i], h.ctx, part); done <- result{got, err} }()
					}
					for range parts.PartitionIDs {
						got := <-done
						if got.err != nil {
							t.Fatal(got.err)
						}
						rows = append(rows, got.rows...)
					}
				}
				elapsed := time.Since(start)
				clientCPU, rss := flexBenchCPU()
				runtime.ReadMemStats(&after)
				serverAfter := flexBenchServerStats(t)
				assertUnmanagedPartitionRows(t, rows, expected)
				data, err := json.Marshal(map[string]any{"trial": trial, "partition_readers": workers, "source_rows": count, "result_rows": len(rows), "read_seconds": elapsed.Seconds(), "discovery_seconds": discovery.Seconds(), "client_cpu_seconds": clientCPU - cpu, "client_allocated_bytes": after.TotalAlloc - before.TotalAlloc, "client_heap_after_bytes": after.HeapAlloc, "client_lifetime_peak_rss_bytes": rss, "server_cpu_seconds": serverAfter.CPUSeconds - serverBefore.CPUSeconds, "server_rss_before_bytes": serverBefore.RSSBytes, "server_rss_after_bytes": serverAfter.RSSBytes, "client_os": runtime.GOOS, "client_arch": runtime.GOARCH, "route": "host Docker published TLS port", "flex": os.Getenv("REDIS_SNAPSHOT_FLEX") == "true"})
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("UNMANAGED_PARTITION_PROFILE %s", data)
			})
		}
	}
}
