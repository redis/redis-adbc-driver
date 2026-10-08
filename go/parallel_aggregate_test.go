// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestMergeExactSumPartitions(t *testing.T) {
	cases := [][]float64{{1e300, 1, -1e300, 0.25, -0.125}, {math.SmallestNonzeroFloat64, 1, -1}, {math.Inf(1), 2}, {math.Inf(1), math.Inf(-1)}, {math.NaN(), 1}}
	for _, values := range cases {
		serial := &exactSum{}
		parts := make([]exactSum, 4)
		for i, v := range values {
			x := floatValue(typeFloat64, v)
			serial.add(x)
			parts[i%len(parts)].add(x)
		}
		combined := &exactSum{}
		for i := range parts {
			mergeExactSum(combined, &parts[i])
		}
		want, _ := serial.sumValue(typeFloat64)
		got, _ := combined.sumValue(typeFloat64)
		if !(math.IsNaN(want.F) && math.IsNaN(got.F)) && math.Float64bits(want.F) != math.Float64bits(got.F) {
			t.Fatalf("%v: got %v want %v", values, got, want)
		}
		if combined.n != serial.n {
			t.Fatal("lost input count")
		}
	}
}

func TestQueryWorkersOption(t *testing.T) {
	d := NewDriver(memory.DefaultAllocator)
	for _, v := range []string{"0", "33", "-1", "bad"} {
		if _, err := d.NewDatabaseWithContext(context.Background(), map[string]string{OptionStringQueryWorkers: v}); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	db, err := d.NewDatabaseWithContext(context.Background(), map[string]string{OptionStringQueryWorkers: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.(adbc.GetSetOptionsWithContext).GetOption(context.Background(), OptionStringQueryWorkers); err != nil || got != "4" {
		t.Fatalf("option %q, %v", got, err)
	}
}

func TestSQLParallelAggregates(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_parallel_aggregate")
	h.exec("CREATE TABLE it_parallel_aggregate (v BIGINT, f DOUBLE PRECISION, g INTEGER)")
	st := &store{client: h.rawClient()}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_parallel_aggregate")
	if err != nil {
		t.Fatal(err)
	}
	const n = 10003
	for start := 0; start < n; start += pipelineChunk {
		pipe := st.client.Pipeline()
		for i := start; i < min(start+pipelineChunk, n); i++ {
			fields := []any{rowIDField, i + 1, "g", i % 7}
			if i%5 != 0 {
				v := int64(9007199254740993)
				if i%2 == 1 {
					v = -v
				}
				fields = append(fields, "v", fmt.Sprint(v-int64(i)), "f", fmt.Sprint(float64(i)/7))
			}
			pipe.HSet(h.ctx, fmt.Sprint(meta.prefix(), i+1), fields...)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.client.Set(h.ctx, seqKey(meta.Schema, meta.Name), n, 0).Err(); err != nil {
		t.Fatal(err)
	}
	// Include a hole across a partition boundary and rows whose inputs are NULL.
	st.client.Del(h.ctx, fmt.Sprint(meta.prefix(), 2500), fmt.Sprint(meta.prefix(), 5001))
	newHarness := func(workers string) *sqlHarness {
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringQueryWorkers: workers, OptionStringAggregatePushdown: PushdownNone})
		if err != nil {
			t.Fatal(err)
		}
		c, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close(h.ctx); db.Close(h.ctx) })
		return &sqlHarness{t: t, ctx: h.ctx, conn: c}
	}
	serial, parallel := newHarness("1"), newHarness("4")
	queries := []string{
		"SELECT COUNT(*), COUNT(v), SUM(v), AVG(v), MIN(v), MAX(v), SUM(f), AVG(f) FROM it_parallel_aggregate",
		"SELECT COUNT(*), SUM(v), AVG(f) FROM it_parallel_aggregate WHERE g <> 3",
		"SELECT COUNT(*), SUM(v), AVG(v), MIN(v), MAX(v) FROM it_parallel_aggregate WHERE g < 0",
		"SELECT g, COUNT(*), SUM(v) FROM it_parallel_aggregate GROUP BY g ORDER BY g",
		"SELECT COUNT(DISTINCT v), SUM(v+1) FROM it_parallel_aggregate",
	}
	for _, q := range queries {
		want, _ := serial.query(q)
		got, _ := parallel.query(q)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s\ngot %v\nwant %v", q, got, want)
		}
	}
}
