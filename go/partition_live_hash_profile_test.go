// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Synthetic fixed-source comparison. This is not ClickBench or a claim that
// repeated discovery is faster than one ordinary SELECT.
func TestLiveHashPartitionProfile(t *testing.T) {
	if os.Getenv("REDIS_LIVE_PARTITION_PROFILE") != "true" {
		t.Skip("opt-in live HASH partition profile")
	}
	if os.Getenv("REDIS_FLEX_BENCH_CONTAINER") == "" {
		t.Fatal("owned Software container required for server counters")
	}
	f := newHashFixture(t, "it:live:partition_profile550:")
	name := "it_live_partition_profile550"
	const count = 8192
	expected := make([]string, count)
	for i := range count {
		key := fmt.Sprintf("%s%d", f.prefix, i)
		f.hset(key, "n", fmt.Sprint(i), "v", fmt.Sprintf("row-%d", i))
		expected[i] = fmt.Sprintf("%s|%d|row-%d", key, i, i)
	}
	sort.Strings(expected)
	if _, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: name, Apply: true, Types: map[string]string{"n": "BIGINT", "v": "VARCHAR"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(context.Background(), f.opts, name); err != nil {
			t.Error(err)
		}
	})
	if err := f.h.conn.(interface {
		SetOption(context.Context, string, string) error
	}).SetOption(f.h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	// Use normal large Arrow batches for the measurement rather than the
	// one-row test-worker helper used to stress buffered invalidation.
	workers := make([]adbc.ConnectionWithContext, 4)
	for i := range workers {
		options := map[string]string{}
		for k, v := range f.opts {
			options[k] = v
		}
		options[OptionStringLiveTables] = "true"
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(f.h.ctx, options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close(context.Background()) })
		conn, err := db.Open(f.h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		workers[i] = conn
	}
	query := "SELECT CAST(__key AS BYTEA),n,v FROM redis_live." + name
	for trial := 0; trial < 3; trial++ {
		order := []int{1, 4}
		if trial%2 == 1 {
			order = []int{4, 1}
		}
		for _, concurrency := range order {
			parts := planLiveHashPartitions(t, f, query, concurrency, "")
			beforeServer := flexBenchServerStats(t)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			beforeCPU, _ := flexBenchCPU()
			start := time.Now()
			results := make([][]string, concurrency)
			errs := make([]error, concurrency)
			var wg sync.WaitGroup
			for i, id := range parts.PartitionIDs {
				wg.Go(func() { results[i], errs[i] = readLiveHashPart(f.h.ctx, workers[i], id) })
			}
			wg.Wait()
			elapsed := time.Since(start)
			afterCPU, rss := flexBenchCPU()
			runtime.ReadMemStats(&after)
			afterServer := flexBenchServerStats(t)
			var actual []string
			for i, rows := range results {
				if errs[i] != nil {
					t.Fatal(errs[i])
				}
				actual = append(actual, rows...)
			}
			sort.Strings(actual)
			if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
				t.Fatal("profile exact coverage/value oracle failed")
			}
			d, err := decodeLiveHashPartition(parts.PartitionIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := (&store{client: f.raw, snapshots: true}).releasePartitionSnapshot(f.h.ctx, d.Snapshot.Token); err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{"trial": trial, "concurrency": concurrency, "rows": count, "seconds": elapsed.Seconds(), "client_cpu_seconds": afterCPU - beforeCPU, "client_allocated_bytes": after.TotalAlloc - before.TotalAlloc, "client_heap_after_bytes": after.HeapAlloc, "client_lifetime_peak_rss_bytes": rss, "server_cpu_seconds": afterServer.CPUSeconds - beforeServer.CPUSeconds, "server_rss_before_bytes": beforeServer.RSSBytes, "server_rss_after_bytes": afterServer.RSSBytes, "client_os": runtime.GOOS, "client_arch": runtime.GOARCH, "route": "host Docker published TLS port", "discovery": "each bucket repeats source SCAN", "source_mode": "live_hash"}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("LIVE_PARTITION_PROFILE %s", data)
		}
	}
}
