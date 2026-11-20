// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Synthetic complete Go ADBC/Arrow comparisons, not ClickBench or a cold-data
// test. RAM's sparse high-ID layout forces the exact directory path; compatible
// Flex uses plain indexes and the same logical values with ordinary row IDs.
func TestSnapshotDirectoryAggregateProfile(t *testing.T) {
	if os.Getenv("REDIS_DIRECTORY_AGGREGATE_PROFILE") != "true" {
		t.Skip("opt-in exact-directory aggregate profile")
	}
	if os.Getenv("REDIS_FLEX_BENCH_CONTAINER") == "" {
		t.Fatal("owned Software process counters required")
	}
	flex := os.Getenv("REDIS_SNAPSHOT_FLEX") == "true"
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT,d DECIMAL(30,4),f DOUBLE PRECISION)")
	const rows = 8192
	s := snapshotTestStore(t, raw)
	if flex {
		h.exec("INSERT INTO t SELECT i,CASE WHEN i%2=0 THEN 9007199254740993 ELSE -9007199254740993 END,CAST(i AS DECIMAL(30,4)),CAST(i AS DOUBLE PRECISION)/7 FROM generate_series(1,8192) AS g(i)")
	} else {
		base, pin, err := s.acquireWritableSnapshot(h.ctx, func(ctx context.Context) (*tableMeta, error) { return s.getTable(ctx, defaultSchema, "t") })
		if err != nil {
			t.Fatal(err)
		}
		work, id, err := s.beginSnapshotWrite(base, pin)
		if err != nil {
			pin.release()
			t.Fatal(err)
		}
		defer s.abortSnapshot(pin, id)
		if err := s.prepareSnapshotTable(work, defaultSchema, "t"); err != nil {
			t.Fatal(err)
		}
		for block := 0; block < 16; block++ {
			seed := int64(block+1) * (1 << 50)
			if err := s.redis(work).Set(work, seqKey(defaultSchema, "t"), seed, 0).Err(); err != nil {
				t.Fatal(err)
			}
			snapshotExecuteTest(t, s, work, fmt.Sprintf("INSERT INTO t SELECT i,CASE WHEN i%%2=0 THEN 9007199254740993 ELSE -9007199254740993 END,CAST(i AS DECIMAL(30,4)),CAST(i AS DOUBLE PRECISION)/7 FROM generate_series(%d,%d) AS g(i)", block*512+1, (block+1)*512))
		}
		if err := s.publishSnapshot(work, pin, id); err != nil {
			t.Fatal(err)
		}
		pin.release()
	}
	h.exec("UPDATE t SET v=7,d=NULL WHERE id IN (512,2048,4096)")
	h.exec("DELETE FROM t WHERE id IN (1,1025,4097,8191)")
	h.expectRows("SELECT COUNT(*),SUM(id),SUM(v),SUM(d) FROM t", "8188|33545214|9007199254741014|33538558.0000")
	sql := "SELECT COUNT(*),SUM(id),COUNT(v),SUM(v),AVG(v),SUM(d),AVG(d),SUM(f),AVG(f) FROM t WHERE id>=2"
	var oracle []string
	var oracleSchema *arrow.Schema
	for _, mode := range []string{"1", "4", "auto"} {
		t.Run("workers="+mode, func(t *testing.T) {
			impl, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: strconv.FormatBool(flex), OptionStringQueryWorkers: mode, OptionStringQueryWorkerBudget: mode, OptionStringAggregatePushdown: PushdownNone}))
			if err != nil {
				t.Fatal(err)
			}
			impl.snapshotCompactionDisabled = true
			db := driverbase.NewDatabase(impl)
			defer db.Close(h.ctx)
			conn, err := db.Open(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(h.ctx)
			current := &sqlHarness{t: t, ctx: h.ctx, conn: conn}
			warmups := 2
			if mode == "auto" {
				warmups = 12
			}
			for range warmups {
				got, schema := current.query(sql)
				if oracle == nil {
					oracle = got
					oracleSchema = schema
				}
				if !reflect.DeepEqual(got, oracle) || !schema.Equal(oracleSchema) {
					t.Fatal("warmup mismatch", got, oracle)
				}
			}
			opts := conn.(adbc.GetSetOptionsWithContext)
			for trial := 0; trial < 5; trial++ {
				serverBefore := flexBenchServerStats(t)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				cpu, _ := flexBenchCPU()
				start := time.Now()
				got, schema := current.query(sql)
				elapsed := time.Since(start)
				cpuAfter, rss := flexBenchCPU()
				runtime.ReadMemStats(&after)
				serverAfter := flexBenchServerStats(t)
				if !reflect.DeepEqual(got, oracle) || !schema.Equal(oracleSchema) {
					t.Fatal("complete Arrow values/types mismatch", got, oracle)
				}
				grant, err := opts.GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
				if err != nil {
					t.Fatal(err)
				}
				active, err := opts.GetOption(h.ctx, OptionIntQueryWorkersActive)
				if err != nil || active != "0" {
					t.Fatal("workers retained after fetch", active, err)
				}
				queued, err := opts.GetOption(h.ctx, OptionIntQueryWorkersQueued)
				if err != nil {
					t.Fatal(err)
				}
				tuning, err := opts.GetOption(h.ctx, OptionStringQueryWorkerTuning)
				if err != nil {
					t.Fatal(err)
				}
				payload := map[string]any{"workers": mode, "queued_after_fetch": queued, "active_after_fetch": active, "grant": grant, "trial": trial, "logical_source_rows": rows, "seconds": elapsed.Seconds(), "client_cpu_seconds": cpuAfter - cpu, "client_allocated_bytes": after.TotalAlloc - before.TotalAlloc, "client_heap_after_bytes": after.HeapAlloc, "client_lifetime_peak_rss_bytes": rss, "server_cpu_seconds": serverAfter.CPUSeconds - serverBefore.CPUSeconds, "server_rss_before_bytes": serverBefore.RSSBytes, "server_rss_after_bytes": serverAfter.RSSBytes, "flex": flex, "client_os": runtime.GOOS, "client_arch": runtime.GOARCH, "route": "host Docker published TLS port", "background_workers": false, "tuning": json.RawMessage(tuning)}
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("DIRECTORY_AGGREGATE_PROFILE %s", encoded)
			}
		})
	}
}
