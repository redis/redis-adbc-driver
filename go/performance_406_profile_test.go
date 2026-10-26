// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Read-only profiles on an independently loaded official fixture. Native
// interleaved trials validate all values separately. Use -cpuprofile/-memprofile;
// Go Arrow allocation differs from the C allocator in the primary native run.
func TestPerformance406Profile(t *testing.T) {
	schema := os.Getenv("REDIS_406_PROFILE_SCHEMA")
	if schema == "" {
		t.Skip("set REDIS_406_PROFILE_SCHEMA on an existing immutable hits fixture")
	}
	if !simpleName(schema) {
		t.Fatal("invalid profile schema")
	}
	number, err := strconv.Atoi(os.Getenv("REDIS_406_PROFILE_QUERY"))
	if err != nil {
		t.Fatal(err)
	}
	queries := map[int]string{
		2:  "SELECT COUNT(*) FROM hits WHERE AdvEngineID <> 0",
		4:  "SELECT AVG(UserID) FROM hits",
		9:  "SELECT RegionID, COUNT(DISTINCT UserID) AS u FROM hits GROUP BY RegionID ORDER BY u DESC LIMIT 10",
		10: "SELECT RegionID, SUM(AdvEngineID), COUNT(*) AS c, AVG(ResolutionWidth), COUNT(DISTINCT UserID) FROM hits GROUP BY RegionID ORDER BY c DESC LIMIT 10",
	}
	sql, ok := queries[number]
	if !ok {
		t.Fatal("profile only supports Q2/Q4/Q9/Q10")
	}
	workers := "4"
	if s := os.Getenv("REDIS_406_PROFILE_WORKERS"); s != "" {
		workers = s
	}
	h := openWith(t, os.Getenv("REDIS_URI"), map[string]string{OptionStringQueryWorkers: workers, OptionStringQueryWorkerBudget: workers, OptionStringAggregatePushdown: PushdownExact})
	h.exec("SET search_path TO " + schema)
	ctx, cancel := context.WithTimeout(h.ctx, 20*time.Minute)
	defer cancel()
	st, err := h.conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(context.Background())
	if err := st.SetSqlQuery(ctx, sql); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("REDIS_406_PROFILE_DIAGNOSTICS"); path != "" {
		if err := st.SetOption(ctx, OptionStringExecutionDiagnosticsFile, path); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	reader, _, err := st.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var values [][]string
	for reader.Next() {
		rec := reader.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			row := make([]string, rec.NumCols())
			for j := 0; j < int(rec.NumCols()); j++ {
				v, err := valueAt(rec.Column(j), i)
				if err != nil {
					reader.Release()
					t.Fatal(err)
				}
				if v.Null {
					row[j] = "NULL"
				} else {
					row[j] = v.textIn(utcZone)
				}
			}
			values = append(values, row)
		}
	}
	err = reader.Err()
	reader.Release()
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	want := 1
	if number == 9 || number == 10 {
		want = 10
	}
	if len(values) != want {
		t.Fatal("unexpected result row count", len(values))
	}
	result, _ := json.Marshal(map[string]any{"query": number, "sql": sql, "workers": workers, "rows": len(values), "values": values, "elapsed_seconds": elapsed.Seconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "gcs": after.NumGC - before.NumGC, "allocator": "Go Arrow; native confirmation uses C Arrow"})
	t.Log(string(result))
}
