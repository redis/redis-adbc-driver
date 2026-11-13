// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"testing"
)

func TestSnapshotImageByteBudget(t *testing.T) {
	for _, fields := range [][]any{
		{"__rowid", "1", "n", "9007199254740993", "text", "a\x00b"},
		{"n", int64(-9223372036854775808), "u", uint64(18446744073709551615), "b", true},
	} {
		row := snapshotInsertRow{key: "rows:1", fields: fields}
		want := len(row.key) + 128
		for _, f := range fields {
			want += len(fmt.Sprint(f))
		}
		if got := snapshotImageBytes(row); got != want {
			t.Fatalf("byte budget changed: %d want %d", got, want)
		}
	}
}
func BenchmarkSnapshotImageByteBudget(b *testing.B) {
	fields := make([]any, 0, 128)
	for c := range 64 {
		fields = append(fields, fmt.Sprintf("c%d", c), "9007199254740993")
	}
	row := snapshotInsertRow{key: "rows:1", fields: fields}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if snapshotImageBytes(row) <= 0 {
			b.Fatal("invalid byte budget")
		}
	}
}
