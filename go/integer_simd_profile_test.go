// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"runtime/pprof"
	runtimetrace "runtime/trace"
	"strconv"
	"testing"
	"time"

	"github.com/adbc-drivers/redis/go/internal/intpredicate"
	"github.com/apache/arrow-go/v18/arrow"
)

func integerBatch(rows []map[string]Value) ([]int64, []uint64) {
	values := make([]int64, len(rows))
	valid := make([]uint64, (len(rows)+63)/64)
	for i, row := range rows {
		v, ok := row["x"]
		if ok && !v.Null {
			values[i] = v.I
			valid[i/64] |= uint64(1) << uint(i%64)
		}
	}
	return values, valid
}

func BenchmarkIntegerBatchConversion(b *testing.B) {
	for _, n := range []int{64, 1024, 65536} {
		for _, nulls := range []int{0, 50, 100} {
			rows := make([]map[string]Value, n)
			for i := range rows {
				v := intValue(typeInt64, int64(i%100))
				if i%100 < nulls {
					v = nullValue(typeInt64)
				}
				rows[i] = map[string]Value{"x": v}
			}
			for _, selectivity := range []int{0, 1, 50, 100} {
				for _, mode := range []string{"row", "convert-scalar", "convert-dispatch"} {
					b.Run(fmt.Sprintf("%s/n=%d/null=%d/select=%d", mode, n, nulls, selectivity), func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							if mode == "row" {
								out := make([]uint64, (n+63)/64)
								for i, row := range rows {
									v := row["x"]
									if !v.Null && v.I < int64(selectivity) {
										out[i/64] |= uint64(1) << uint(i%64)
									}
								}
								integerProfileSink = out
							} else {
								v, valid := integerBatch(rows)
								var err error
								integerProfileSink, err = intpredicate.Match(context.Background(), v, valid, int64(selectivity), intpredicate.Less, mode == "convert-scalar")
								if err != nil {
									b.Fatal(err)
								}
							}
						}
					})
				}
			}
		}
	}
}

var integerProfileSink []uint64

// Opt-in disposable fixture; does not instrument or alter production execution.
// Wall phase timings are inclusive and not additive CPU/wait decompositions.
func TestIntegerSIMDProfile(t *testing.T) {
	if os.Getenv("REDIS_INTEGER_PROFILE") != "1" {
		t.Skip("set REDIS_INTEGER_PROFILE=1 with verified Software 4x QPF")
	}
	h := newSQLHarness(t)
	h.dropTables("it_integer_simd")
	t.Cleanup(func() { h.dropTables("it_integer_simd") })
	h.exec("CREATE TABLE it_integer_simd (id BIGINT, x BIGINT)")
	client := h.rawClient()
	defer client.Close()
	st := &store{client: client}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_integer_simd")
	if err != nil {
		t.Fatal(err)
	}
	const n = 10000
	keys := make([]string, n)
	expected := make([]string, 0, n)
	pipe := client.Pipeline()
	for i := range keys {
		id := int64(i + 1)
		x := int64(i%101) - 50
		if i%29 == 0 {
			x = 9007199254740993
		}
		if i%31 == 0 {
			x = -9007199254740993
		}
		keys[i] = meta.prefix() + strconv.FormatInt(id, 10)
		fields := []any{rowIDField, id, "id", id}
		if i%7 != 0 {
			fields = append(fields, "x", x)
			if x >= 0 {
				expected = append(expected, fmt.Sprintf("%d|%d", id, x))
			}
		}
		pipe.HSet(h.ctx, keys[i], fields...)
	}
	if _, err = pipe.Exec(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err = client.Set(h.ctx, seqKey(meta.Schema, meta.Name), n, 0).Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "integer SIMD indexed", func() bool { count, err := st.countMatches(h.ctx, meta.index(), "*"); return err == nil && count == n })
	t.Logf("environment go=%s os=%s arch=%s backend=%s rows=%d", runtime.Version(), runtime.GOOS, runtime.GOARCH, intpredicate.Backend(), n)
	phase := func(name string, run func()) {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		pprof.Do(h.ctx, pprof.Labels("phase", name), func(ctx context.Context) { runtimetrace.WithRegion(ctx, name, run) })
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		t.Logf("phase=%s wall_ns=%d alloc_bytes=%d allocations=%d", name, elapsed.Nanoseconds(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
	}
	// The wait phase includes Redis, TLS, protocol decoding and go-redis allocation.
	// Use a runtime trace to distinguish socket waits; never call it pure CPU time.
	var raw []aggRow
	phase("fetch-including-network-protocol-and-map-allocation", func() {
		raw, err = st.fetchRows(h.ctx, keys, []string{"id", "x"})
		if err != nil {
			t.Fatal(err)
		}
	})
	rows := make([]map[string]Value, len(raw))
	phase("decode-typed-row-maps", func() {
		for i, row := range raw {
			rows[i], err = decodeRow(meta, row, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	var v []int64
	var valid []uint64
	phase("batch-construction", func() { v, valid = integerBatch(rows) })
	var masks [2][]uint64
	for i, scalar := range []bool{true, false} {
		phase(fmt.Sprintf("kernel-scalar=%t", scalar), func() {
			masks[i], err = intpredicate.Match(h.ctx, v, valid, 0, intpredicate.GreaterEqual, scalar)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	if !reflect.DeepEqual(masks[0], masks[1]) {
		t.Fatal("kernel parity")
	}
	residual := &Binary{Op: ">=", L: &ColumnRef{Name: "x"}, R: &Literal{V: intValue(typeInt64, 0)}}
	env := &evalEnv{ctx: h.ctx, types: meta.types()}
	matches := make([]string, 0, len(expected))
	phase("residual-evaluation", func() {
		for i, row := range rows {
			env.row = row
			v, err := env.eval(residual)
			if err != nil {
				t.Fatal(err)
			}
			selected := !v.Null && v.I != 0
			if selected != (masks[1][i/64]&(uint64(1)<<uint(i%64)) != 0) {
				t.Fatalf("residual/kernel mismatch at %d", i)
			}
			if selected {
				matches = append(matches, fmt.Sprintf("%d|%d", row["id"].I, row["x"].I))
			}
		}
	})
	if !reflect.DeepEqual(matches, expected) {
		t.Fatal("independent fixture oracle mismatch")
	}
	for trial := 0; trial < 3; trial++ {
		phase("execute-through-full-fetch", func() {
			got, schema := h.query("SELECT id,x FROM it_integer_simd WHERE x+0>=0 ORDER BY id")
			if !reflect.DeepEqual(got, expected) {
				t.Fatal("full-fetch oracle mismatch")
			}
			for _, field := range schema.Fields() {
				if field.Type.ID() != arrow.INT64 {
					t.Fatal("physical integer type changed")
				}
			}
		})
	}
}
