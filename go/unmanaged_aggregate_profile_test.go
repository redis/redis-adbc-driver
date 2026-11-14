// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// Opt-in attribution on the same four public native columns + ordinal and
// non-SORTABLE source attributes as the scenario benchmark. This is a Go ADBC
// profile with instrumentation, not a Python/C-boundary benchmark or a new
// product comparison. The snapshot harness requires a disposable endpoint and
// clears its data/indexes; run only in the coordinated verified Software slot.
func TestUnmanagedAggregateExecutionProfile(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_AGG_PROFILE") != "true" {
		t.Skip("set REDIS_UNMANAGED_AGG_PROFILE=true for the 100K-row owned-fixture profile")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const rows = 100000
	const prefix, index, table = "agg_profile_source:", "agg_profile_source_idx", "agg_profile_live"
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
	types, encodings := map[string]string{}, map[string]string{}
	for _, name := range []string{"id", "grp", "amount", "label", "bench_ordinal"} {
		types[name], encodings[name] = "BIGINT", "native"
	}
	types["label"] = "VARCHAR"
	if _, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Mode: "unmanaged", Prefix: prefix, Index: index, Table: table, Types: types, Encodings: encodings, Apply: true}); err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("REDIS_UNMANAGED_AGG_PROFILE_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("REDIS_UNMANAGED_AGG_CPU_PROFILE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); _ = f.Close() }()
	}
	for _, grouped := range []bool{false, true} {
		name, query := "filtered", "SELECT CAST(COUNT(*) AS BIGINT),CAST(SUM(amount) AS BIGINT) FROM "+table+" WHERE bench_ordinal BETWEEN 1 AND 10000"
		want := []string{"10000|150015000"}
		if grouped {
			name, query = "grouped", "SELECT grp,CAST(COUNT(*) AS BIGINT),CAST(SUM(amount) AS BIGINT) FROM "+table+" GROUP BY grp"
			want = nil
			for g := 0; g < 16; g++ {
				first := g
				if first == 0 {
					first = 16
				}
				n := (rows-first)/16 + 1
				last := first + (n-1)*16
				want = append(want, fmt.Sprintf("%d|%d|%d", g, n, 3*(first+last)*n/2))
			}
		}
		slices.Sort(want)
		for repeat := -1; repeat < 3; repeat++ {
			path := filepath.Join(dir, fmt.Sprintf("%s-%d.json", name, repeat))
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetOption(h.ctx, OptionStringExecutionDiagnosticsFile, path); err != nil {
				_ = st.Close(h.ctx)
				t.Fatal(err)
			}
			if err := st.SetSqlQuery(h.ctx, query); err != nil {
				_ = st.Close(h.ctx)
				t.Fatal(err)
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			started := time.Now()
			r, _, err := st.ExecuteQuery(h.ctx)
			if err != nil {
				_ = st.Close(h.ctx)
				t.Fatal(err)
			}
			var got []string
			for r.Next() {
				rec := r.RecordBatch()
				for col := 0; col < int(rec.NumCols()); col++ {
					if rec.Schema().Field(col).Type.ID() != arrow.INT64 {
						r.Release()
						_ = st.Close(h.ctx)
						t.Fatal("profile output type differs from benchmark")
					}
				}
				for row := 0; row < int(rec.NumRows()); row++ {
					cells := make([]string, rec.NumCols())
					for col := 0; col < int(rec.NumCols()); col++ {
						a := rec.Column(col).(*array.Int64)
						if a.IsNull(row) {
							r.Release()
							_ = st.Close(h.ctx)
							t.Fatal("unexpected NULL profile result")
						}
						cells[col] = strconv.FormatInt(a.Value(row), 10)
					}
					got = append(got, strings.Join(cells, "|"))
				}
			}
			elapsed := time.Since(started)
			err = r.Err()
			r.Release()
			_ = st.Close(h.ctx)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatal("profile result differs from independent arithmetic oracle", got, want)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var d map[string]any
			if err := json.Unmarshal(b, &d); err != nil {
				t.Fatal(err)
			}
			plan, ok := d["plan"].(map[string]any)
			if !ok || plan["kind"] != "typed_aggregate_inputs" || plan["live_unmanaged"] != true || plan["bound_residual"] != !grouped {
				t.Fatal("native aggregate did not use the expected live typed path", plan)
			}
			d["case"], d["repeat"], d["warmup"], d["validated"] = name, repeat, repeat < 0, true
			d["fixture_rows"], d["public_columns"], d["result_rows"] = rows, 4, len(got)
			d["execute_and_full_arrow_seconds"] = elapsed.Seconds()
			d["allocated_bytes_full_call"], d["allocations_full_call"] = after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs
			d["scope"] = "Instrumented Go ADBC execute and complete typed Arrow consumption; excludes fixture/setup and Python/C bridge. Redis wait includes pool wait and reply decoding, not server CPU."
			b, _ = json.Marshal(d)
			t.Logf("UNMANAGED_AGG_PROFILE %s", b)
		}
		// Attribute the same scan/reduction after catalog planning separately.
		// The hook counts real commands, never INFO commandstats (Software may
		// hide those). No SQL, index membership or input values are modified.
		observer := &unmanagedAggregateProfileHook{commands: map[string]int64{}, wait: map[string]time.Duration{}}
		ctx, e, plan, _ := distinctSnapshotTestPlan(t, h, query, observer)
		e.diagnostics, e.telemetry = &executionDiagnostics{}, &scanTelemetry{}
		observer.active.Store(true)
		started := time.Now()
		out, err := e.selectAggregate(ctx, plan, nil)
		elapsed := time.Since(started)
		observer.active.Store(false)
		if err != nil {
			t.Fatal(err)
		}
		got := snapshotResultStrings(out)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatal("phase result differs", got, want)
		}
		b, _ := json.Marshal(map[string]any{"case": name, "validated": true, "scope": "Pinned/planned Go aggregate execution only; command wait includes decode/pool wait, not server CPU", "seconds": elapsed.Seconds(), "commands": observer.counts(), "round_trip_seconds": observer.seconds(), "scan_fetch_seconds": float64(e.telemetry.fetch.Load()) / 1e9, "scan_rows": e.telemetry.rows.Load()})
		t.Logf("UNMANAGED_AGG_PHASE %s", b)
	}
}

type unmanagedAggregateProfileHook struct {
	active   atomic.Bool
	mu       sync.Mutex
	commands map[string]int64
	wait     map[string]time.Duration
}

func (*unmanagedAggregateProfileHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *unmanagedAggregateProfileHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if !h.active.Load() {
			return next(ctx, cmd)
		}
		started := time.Now()
		err := next(ctx, cmd)
		h.mu.Lock()
		h.commands[cmd.Name()]++
		h.wait[cmd.Name()] += time.Since(started)
		h.mu.Unlock()
		return err
	}
}
func (h *unmanagedAggregateProfileHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		if !h.active.Load() {
			return next(ctx, cmds)
		}
		started := time.Now()
		err := next(ctx, cmds)
		category := "pipeline"
		if len(cmds) > 0 {
			category = cmds[0].Name() + "_pipeline"
		}
		h.mu.Lock()
		for _, cmd := range cmds {
			h.commands[cmd.Name()]++
		}
		h.wait[category] += time.Since(started)
		h.mu.Unlock()
		return err
	}
}
func (h *unmanagedAggregateProfileHook) seconds() map[string]float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]float64{}
	for k, v := range h.wait {
		out[k] = v.Seconds()
	}
	return out
}

func (h *unmanagedAggregateProfileHook) counts() map[string]int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]int64{}
	for k, v := range h.commands {
		out[k] = v
	}
	return out
}
