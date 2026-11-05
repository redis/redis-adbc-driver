// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"github.com/apache/arrow-go/v18/arrow"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Opt-in, correctness-gated allocation/timing evidence, not a benchmark promise.
func TestArrowKeyStreamingProfile(t *testing.T) {
	if os.Getenv("ARROW_INTEROP_PROFILE") != "1" {
		t.Skip("set ARROW_INTEROP_PROFILE=1")
	}
	h := newSQLHarness(t)
	const table = "it_arrow_stream_profile"
	h.setupStreamTable(table, 8192)
	defer h.dropTables(table)
	for _, count := range []int{128, 2048, 8192} {
		ids := make([]string, count)
		for i := range ids {
			ids[i] = fmt.Sprint(i + 1)
		}
		q := "SELECT id FROM " + table + " WHERE __rowid IN (" + strings.Join(ids, ",") + ")"
		for _, mode := range []string{"false", "true"} {
			runtime.GC()
			var before, first, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			r, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: mode, OptionIntStreamBatchRows: "64"})
			if !r.Next() {
				t.Fatal(r.Err())
			}
			firstElapsed := time.Since(start)
			runtime.ReadMemStats(&first)
			var batches []arrow.RecordBatch
			for {
				rec := r.RecordBatch()
				rec.Retain()
				batches = append(batches, rec)
				if !r.Next() {
					break
				}
			}
			err := r.Err()
			r.Release()
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			// Validate every value only after the timed fetch. Consumer retention
			// is explicit; TotalAlloc is not a peak-RSS/memory-bound measurement.
			var values []int64
			for _, rec := range batches {
				for i := 0; i < int(rec.NumRows()); i++ {
					v, e := valueAt(rec.Column(0), i)
					if e != nil {
						t.Fatal(e)
					}
					values = append(values, v.I)
				}
				rec.Release()
			}
			if err != nil || len(values) != count {
				t.Fatal(len(values), count, err)
			}
			seen := make([]bool, count)
			for _, v := range values {
				if v < 0 || v >= int64(count) || seen[v] {
					t.Fatal("wrong/duplicate row", v)
				}
				seen[v] = true
			}
			t.Logf("rows=%d stream=%s first=%s full=%s first_alloc_bytes=%d full_alloc_bytes=%d", count, mode, firstElapsed, elapsed, first.TotalAlloc-before.TotalAlloc, after.TotalAlloc-before.TotalAlloc)
		}
	}
}
