// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// Read-only diagnostic on the independently loaded transfer fixture. Use
// -cpuprofile/-memprofile; the native paired benchmark validates every value.
func TestSearchReplyScanProfile(t *testing.T) {
	if os.Getenv("REDIS_REPLY_PROFILE") == "" {
		t.Skip("set REDIS_REPLY_PROFILE on the immutable 100K transfer fixture")
	}
	h := newSQLHarness(t)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	for range 3 {
		r, _ := h.openStream(h.ctx, h.conn, "SELECT * FROM transferbench.hits", nil, nil)
		var rows int64
		for r.Next() {
			rows += r.RecordBatch().NumRows()
		}
		err := r.Err()
		r.Release()
		if err != nil || rows != 100000 {
			t.Fatalf("rows=%d error=%v", rows, err)
		}
	}
	runtime.ReadMemStats(&after)
	t.Logf("three scans: elapsed=%s allocated_bytes=%d allocations=%d GCs=%d", time.Since(start), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, after.NumGC-before.NumGC)
}
