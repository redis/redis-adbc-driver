// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"testing"
)

// CPU/allocation microbenchmark of the decode/group/reduce stage only. No Redis,
// TLS, SQL planning or server reducer work is included; do not interpret this as
// an end-to-end query speedup. Each iteration consumes one exact native row.
func BenchmarkUnmanagedAggregateInputVector(b *testing.B) {
	meta := &tableMeta{Schema: defaultSchema, Name: "live", KeyPrefix: "app:", IndexName: "app_idx", Adopted: true, KeyRows: true, AppIndex: true, Unmanaged: true, UnmanagedPrefixes: []string{"app:"}, Columns: []columnMeta{
		{Name: "g", Type: typeInt64}, {Name: "v", Type: typeInt64},
	}}
	need := map[string]bool{"g": true, "v": true}
	group := &ColumnRef{Name: "g"}
	sum := &Func{Name: "SUM", Args: []Expr{&ColumnRef{Name: "v"}}}
	raw := aggRow{keyField: "app:one", "g": "7", "v": "9007199254740993"}
	for _, typed := range []bool{false, true} {
		name := "row_map"
		if typed {
			name = "typed_vector"
		}
		b.Run(name, func(b *testing.B) {
			env := &evalEnv{ctx: context.Background(), types: meta.types()}
			acc := newAccumulator(sum)
			var key string
			it := &scanIter{meta: meta, need: need, env: env}
			p := newScanProjection(it, []planItem{{expr: group}, {expr: sum.Args[0]}})
			scratch := make([]Value, 2)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if typed {
					if err := p.decodeInto(raw, scratch); err != nil {
						b.Fatal(err)
					}
					key = rowKey(scratch[:1])
					acc.add(scratch[1])
				} else {
					row, err := decodeRow(meta, raw, need)
					if err != nil {
						b.Fatal(err)
					}
					env.row = row
					g, err := env.eval(group)
					if err != nil {
						b.Fatal(err)
					}
					key = rowKey([]Value{g})
					if err := acc.addRow(env); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			if key != rowKey([]Value{intValue(typeInt64, 7)}) || acc.count != int64(b.N) {
				b.Fatal("incomplete reduction")
			}
		})
	}
}
