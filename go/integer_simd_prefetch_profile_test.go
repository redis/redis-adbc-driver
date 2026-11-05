// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adbc-drivers/redis/go/internal/intpredicate"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// This is a post-Arrow consumer experiment, not a SQL WHERE backend. PR #297's
// bare-column wide path bypasses decoded maps, so it needs its own experiment.
func TestIntegerSIMDPrefetchProfile(t *testing.T) {
	if os.Getenv("REDIS_INTEGER_PROFILE") != "1" {
		t.Skip("set REDIS_INTEGER_PROFILE=1 with verified Software 4x QPF")
	}
	n := 20000
	if setting := os.Getenv("REDIS_INTEGER_PROFILE_ROWS"); setting != "" {
		var err error
		n, err = strconv.Atoi(setting)
		if err != nil || n < 8193 || n > 1000000 {
			t.Fatal("wide profile rows must be between 8193 and 1000000")
		}
	}
	h := newSQLHarness(t)
	const table = "it_integer_simd_wide"
	h.dropTables(table)
	t.Cleanup(func() { h.dropTables(table) })
	defs := []string{"id BIGINT", "x BIGINT"}
	for c := 2; c < 64; c++ {
		defs = append(defs, fmt.Sprintf("p%d BIGINT", c))
	}
	h.exec("CREATE TABLE " + table + " (" + strings.Join(defs, ",") + ")")
	client := h.rawClient()
	defer client.Close()
	st := &store{client: client}
	meta, err := st.getTable(h.ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	oracle := func(id int64, c int) (int64, bool) {
		if c == 0 {
			return id, false
		}
		if c == 1 {
			x := (id*37)%101 - 50
			if id%29 == 0 {
				x = 9007199254740993
			}
			if id%31 == 0 {
				x = -9007199254740993
			}
			return x, id%7 == 0
		}
		return id*3 + int64(c), (id+int64(c))%17 == 0
	}
	for start := 1; start <= n; start += 512 {
		pipe := client.Pipeline()
		for id := start; id < min(start+512, n+1); id++ {
			fields := []any{rowIDField, id}
			for c, col := range meta.Columns {
				v, null := oracle(int64(id), c)
				if !null {
					fields = append(fields, col.field(), v)
				}
			}
			pipe.HSet(h.ctx, meta.prefix()+strconv.Itoa(id), fields...)
		}
		if _, err = pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = client.Set(h.ctx, seqKey(meta.Schema, meta.Name), n, 0).Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "wide integer SIMD indexed", func() bool {
		count, err := st.countMatches(h.ctx, meta.index(), "*")
		return err == nil && count == int64(n)
	})
	type owned struct {
		rec  arrow.RecordBatch
		mask []uint64
	}
	type measurement struct {
		Mode        string `json:"mode"`
		Trial       int    `json:"trial"`
		Prefetch    bool   `json:"prefetch"`
		Backend     string `json:"backend"`
		Rows        int    `json:"rows"`
		Batches     int    `json:"batches"`
		FirstRows   int    `json:"first_rows"`
		WallNS      int64  `json:"wall_ns"`
		PredicateNS int64  `json:"predicate_ns"`
		AllocBytes  uint64 `json:"alloc_bytes"`
		Allocs      uint64 `json:"allocs"`
	}
	var results []measurement
	run := func(mode string, trial int, record bool) {
		var kept []owned
		defer func() {
			for _, item := range kept {
				item.rec.Release()
			}
		}()
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		statement, r, _ := h.openStmtStream(h.ctx, h.conn, "SELECT * FROM "+table, nil, map[string]string{OptionIntStreamBatchRows: "4096"})
		stream, ok := r.(*streamReader)
		if !ok || stream.it == nil || !stream.it.hashFetch || stream.it.projection == nil || stream.it.allowPrefetch != scanPrefetchEnabled {
			r.Release()
			t.Fatal("wide positional Arrow path not selected")
		}
		// ExecuteQuery fills the first batch. The fixture is large enough that the
		// cursor remains active, so a build-tag assertion cannot pass on an EOF path.
		if (stream.it.prefetch != nil) != scanPrefetchEnabled {
			r.Release()
			t.Fatal("expected prefetch worker not active")
		}
		m := measurement{Mode: mode, Trial: trial, Prefetch: scanPrefetchEnabled, Backend: intpredicate.Backend(), Rows: n}
		for r.Next() {
			rec := r.RecordBatch()
			rec.Retain()
			kept = append(kept, owned{rec: rec})
			x, ok := rec.Column(1).(*array.Int64)
			if !ok {
				r.Release()
				t.Fatal("physical integer type changed")
			}
			m.Batches++
			if m.FirstRows == 0 {
				m.FirstRows = x.Len()
			}
			predStart := time.Now()
			var mask []uint64
			if mode == "row" {
				mask = make([]uint64, (x.Len()+63)/64)
				for i, v := range x.Int64Values() {
					if !x.IsNull(i) && v >= 0 {
						mask[i/64] |= uint64(1) << uint(i%64)
					}
				}
			} else {
				values := x.Int64Values()
				if mode != "borrowed-simd" {
					values = append([]int64(nil), values...)
				}
				valid := make([]uint64, (x.Len()+63)/64)
				for i := 0; i < x.Len(); i++ {
					if !x.IsNull(i) {
						valid[i/64] |= uint64(1) << uint(i%64)
					}
				}
				mask, err = intpredicate.Match(h.ctx, values, valid, 0, intpredicate.GreaterEqual, mode == "packed-scalar")
				if err != nil {
					r.Release()
					t.Fatal(err)
				}
			}
			m.PredicateNS += time.Since(predStart).Nanoseconds()
			kept[len(kept)-1].mask = mask
		}
		if err = r.Err(); err != nil {
			r.Release()
			t.Fatal(err)
		}
		r.Release()
		if err = statement.Close(h.ctx); err != nil {
			t.Fatal(err)
		}
		m.WallNS = time.Since(start).Nanoseconds()
		runtime.ReadMemStats(&after)
		m.AllocBytes = after.TotalAlloc - before.TotalAlloc
		m.Allocs = after.Mallocs - before.Mallocs
		// Validate every field, physical type, NULL bit, row multiplicity and mask
		// outside timing, after reader release, using retained input ownership.
		seen := make([]bool, n+1)
		rows := 0
		for _, item := range kept {
			rec := item.rec
			if rec.NumCols() != 64 {
				t.Fatal("wrong projection width")
			}
			for _, field := range rec.Schema().Fields() {
				if field.Type.ID() != arrow.INT64 {
					t.Fatal("wrong physical type")
				}
			}
			for i := 0; i < int(rec.NumRows()); i++ {
				ids := rec.Column(0).(*array.Int64)
				if ids.IsNull(i) {
					t.Fatal("NULL fixture id")
				}
				id := ids.Value(i)
				if id < 1 || id > int64(n) || seen[id] {
					t.Fatal("missing/duplicate/unexpected row")
				}
				seen[id] = true
				rows++
				for c := 0; c < 64; c++ {
					col := rec.Column(c).(*array.Int64)
					want, null := oracle(id, c)
					if col.IsNull(i) != null || !null && col.Value(i) != want {
						t.Fatalf("oracle mismatch row=%d column=%d", id, c)
					}
				}
				want, null := oracle(id, 1)
				if (item.mask[i/64]&(uint64(1)<<uint(i%64)) != 0) != (!null && want >= 0) {
					t.Fatal("predicate mask oracle mismatch")
				}
			}
		}
		if rows != n {
			t.Fatal("incomplete full fetch")
		}
		if record {
			results = append(results, m)
		}
	}
	modes := []string{"row", "packed-scalar", "packed-simd", "borrowed-simd"}
	for _, mode := range modes {
		run(mode, -1, false)
	} // excluded warmups
	for trial := 0; trial < 3; trial++ {
		for j := range modes {
			run(modes[(j+trial)%len(modes)], trial, true)
		}
	}
	data, err := json.MarshalIndent(map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "columns": 64, "batch_rows": 4096, "results": results}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("REDIS_INTEGER_WIDE_REPORT"); path != "" {
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("validated wide consumer trials=%d prefetch=%t backend=%s rows=%d", len(results), scanPrefetchEnabled, intpredicate.Backend(), n)
}
