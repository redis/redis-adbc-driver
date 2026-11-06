// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Isolate the merge CPU/allocations from Redis and Arrow. Every source is a
// completely fetched immutable page, with overlapping identities and gaps.
// Full-query/native profiles remain necessary for end-to-end conclusions.
func BenchmarkSnapshotDirectoryMerge(b *testing.B) {
	for _, segments := range []int{1, 4, 8} {
		b.Run(fmt.Sprint(segments), func(b *testing.B) {
			sources := make([]snapshotDeltaSource, segments)
			for s := range sources {
				sources[s].segment.Prefix = fmt.Sprintf("segment%d:", s)
				sources[s].done = true
				for i := 1; i <= snapshotDeltaPage; i++ {
					if s == 0 || i%(s+1) == 0 {
						sources[s].ids = append(sources[s].ids, fmt.Sprintf("%020d", i))
					}
				}
			}
			d := snapshotDeltaScan{meta: &tableMeta{KeyPrefix: "logical:"}, sources: sources}
			wantLogical, wantPhysical := make([]string, snapshotDeltaPage), make([]string, snapshotDeltaPage)
			for i := range wantLogical {
				winner := 0
				for s := 1; s < segments; s++ {
					if (i+1)%(s+1) == 0 {
						winner = s
					}
				}
				wantLogical[i] = fmt.Sprint("logical:", i+1)
				wantPhysical[i] = fmt.Sprintf("segment%d:%d", winner, i+1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for s := range d.sources {
					d.sources[s].at = 0
				}
				logical, physical, err := d.nextKeys(context.Background())
				if err != nil || len(logical) != snapshotDeltaPage {
					b.Fatal("incomplete merge", err)
				}
				for i := range logical {
					if logical[i] != wantLogical[i] || physical[i] != wantPhysical[i] {
						b.Fatal("merge changed identity or newest winner")
					}
				}
			}
		})
	}
}

// Full Arrow consumption with independent golden rows. Diagnostics cover
// driver connection bytes (including control traffic), not server CPU or TLS
// framing. MemStats is process-wide; the controlled fixture disables workers.
func TestSnapshotReadResourceProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_READ_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_READ_PROFILE on an owned Software fixture")
	}
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	columns := []string{"id BIGINT", "v BIGINT"}
	values := []string{"i", "9007199254740993+i"}
	for i := range 30 {
		columns = append(columns, fmt.Sprintf("p%d VARCHAR", i))
		values = append(values, "'payload'")
	}
	h.exec("CREATE TABLE read_profile(" + strings.Join(columns, ",") + ")")
	h.exec("INSERT INTO read_profile SELECT " + strings.Join(values, ",") + " FROM generate_series(1,4096) AS g(i)")
	h.exec("UPDATE read_profile SET v=0 WHERE id=1")
	h.exec("DELETE FROM read_profile WHERE id=2")
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	c := stmt.(*statementImpl).conn
	for _, wide := range []bool{false, true} {
		projection := "id,v"
		if wide {
			projection = "*"
		}
		for sample := range 4 {
			runtime.GC()
			d := &executionDiagnostics{}
			c.clientCfg.diagnostics.Store(d)
			prior := h.ctx
			h.ctx = context.WithValue(prior, diagnosticsContextKey{}, d)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			cpu, _ := flexBenchCPU()
			start := time.Now()
			rows, _ := h.query("SELECT " + projection + " FROM read_profile")
			elapsed := time.Since(start)
			cpuAfter, _ := flexBenchCPU()
			runtime.ReadMemStats(&after)
			h.ctx = prior
			c.clientCfg.diagnostics.Store(nil)
			if len(rows) != 4095 {
				t.Fatal("incomplete full Arrow fetch", len(rows))
			}
			for j, row := range rows {
				id := j + 1
				if id >= 2 {
					id++
				}
				v := int64(9007199254740993) + int64(id)
				if id == 1 {
					v = 0
				}
				want := fmt.Sprintf("%d|%d", id, v)
				if wide {
					want += strings.Repeat("|payload", 30)
				}
				if row != want {
					t.Fatal("exact replacement/delete or payload differs at row", j)
				}
			}
			if sample > 0 {
				r, _ := json.Marshal(map[string]any{"wide": wide, "sample": sample, "rows": len(rows), "elapsed_ms": float64(elapsed) / float64(time.Millisecond), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "read_bytes": d.readBytes.Load(), "written_bytes": d.writtenBytes.Load(), "client_cpu_seconds": cpuAfter - cpu})
				t.Logf("snapshot_read_profile %s", r)
			}
		}
	}
}
