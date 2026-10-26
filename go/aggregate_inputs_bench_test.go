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
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

// One operation is one decoded row, containing n related expression SUMs.
// The staged benchmarks keep Redis loading/decoding out of these CPU costs.
func BenchmarkAggregateInputStages(b *testing.B) {
	for _, n := range []int{1, 10, 90} {
		funcs := make([]*Func, n)
		for i := range funcs {
			funcs[i] = &Func{Name: "SUM", Args: []Expr{&Binary{Op: "+", L: &ColumnRef{Name: "x"}, R: &Literal{V: intValue(typeInt64, int64(i+1))}}}}
		}
		types := map[string]ColType{"x": typeInt16}
		row := map[string]Value{"x": intValue(typeInt16, 1920)}
		env := &evalEnv{row: row, types: types}
		for _, typed := range []bool{false, true} {
			label := "scalar"
			if typed {
				label = "typed"
			}
			b.Run(fmt.Sprintf("%d/evaluate/%s", n, label), func(b *testing.B) {
				p := compileAggregateInputs(funcs, types)
				b.ReportAllocs()
				b.ResetTimer()
				var value Value
				var err error
				for j := 0; j < b.N; j++ {
					p.nextRow()
					for i, f := range funcs {
						if typed {
							value, err = p.value(p.roots[i], row)
						} else {
							value, err = env.eval(f.Args[0])
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				}
				if value.I != 1920+int64(n) {
					b.Fatal(value)
				}
			})
			b.Run(fmt.Sprintf("%d/evaluate_update/%s", n, label), func(b *testing.B) {
				p := compileAggregateInputs(funcs, types)
				accs := make([]*accumulator, n)
				for i, f := range funcs {
					accs[i] = newAccumulator(f)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for j := 0; j < b.N; j++ {
					p.nextRow()
					for i, acc := range accs {
						if typed {
							v, err := p.value(p.roots[i], row)
							if err != nil {
								b.Fatal(err)
							}
							acc.add(v)
						} else if err := acc.addRow(env); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
		b.Run(fmt.Sprintf("%d/update", n), func(b *testing.B) {
			accs := make([]*accumulator, n)
			values := make([]Value, n)
			for i, f := range funcs {
				accs[i] = newAccumulator(f)
				values[i] = intValue(typeInt64, 1921+int64(i))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for j := 0; j < b.N; j++ {
				for i, acc := range accs {
					acc.add(values[i])
				}
			}
		})
	}
}

// The load profile uses the same one-column cursor/decode path as Q30,
// without expression evaluation or accumulator updates.
func TestAggregateInputLoadProfile(t *testing.T) {
	table := os.Getenv("REDIS_INPUT_LOAD_PROFILE_TABLE")
	if table == "" {
		t.Skip("set REDIS_INPUT_LOAD_PROFILE_TABLE to profile a preloaded fixture")
	}
	h := newSQLHarness(t)
	client := h.rawClient()
	st := &store{client: client}
	meta, err := st.getTable(h.ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: st}
	req := scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{"ResolutionWidth": true}}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	count := 0
	start := time.Now()
	err = e.scanPages(h.ctx, req, nil, func(rows []map[string]Value) error { count += len(rows); return nil })
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"rows": count, "seconds": elapsed.Seconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs})
	t.Logf("LOAD_PROFILE %s", b)
}
