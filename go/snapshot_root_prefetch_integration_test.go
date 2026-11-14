// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSnapshotRootPrefetchNativeBounds(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	key := snapshotMetadataMap(snapshotID())
	for _, kind := range []string{"absent", "small", "full_count_bound", "too_many", "large_field", "large_value", "large_tail_value", "wrongtype"} {
		t.Run(kind, func(t *testing.T) {
			if err := raw.Del(h.ctx, key).Err(); err != nil {
				t.Fatal(err)
			}
			want := []any{}
			switch kind {
			case "small":
				want = []any{"00", strings.Repeat("a", 32)}
				if err := raw.HSet(h.ctx, key, want...).Err(); err != nil {
					t.Fatal(err)
				}
			case "full_count_bound", "too_many", "large_tail_value":
				n := 256
				if kind == "too_many" {
					n = 257
				}
				for i := 0; i < n; i++ {
					value := strings.Repeat("a", 32)
					if kind == "large_tail_value" && i == n-1 {
						value = strings.Repeat("z", 128<<10)
					}
					if err := raw.HSet(h.ctx, key, fmt.Sprintf("%02x", i), value).Err(); err != nil {
						t.Fatal(err)
					}
				}
			case "large_field":
				if err := raw.HSet(h.ctx, key, strings.Repeat("f", 128<<10), strings.Repeat("a", 32)).Err(); err != nil {
					t.Fatal(err)
				}
			case "large_value":
				if err := raw.HSet(h.ctx, key, "00", strings.Repeat("v", 128<<10)).Err(); err != nil {
					t.Fatal(err)
				}
			case "wrongtype":
				if err := raw.Set(h.ctx, key, "string", 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			var original map[string]string
			if kind != "wrongtype" {
				var err error
				original, err = raw.HGetAll(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
			}
			cmd := once(h.ctx, raw, "EVAL_RO", snapshotRootPrefetchLua, 1, key)
			switch kind {
			case "too_many", "large_field", "large_value", "large_tail_value":
				if !snapshotRootPrefetchDeclined(cmd.Err()) {
					t.Fatal("unbounded root reached Lua HGETALL", kind, cmd.Err())
				}
				// The fixed refusal is read-only. The full original native
				// path returns every exact field/value, without truncation.
				after, err := raw.HGetAll(h.ctx, key).Result()
				if err != nil || !reflect.DeepEqual(after, original) {
					t.Fatal("decline changed original root", kind, err)
				}
			case "wrongtype":
				if cmd.Err() == nil || snapshotRootPrefetchDeclined(cmd.Err()) || !strings.Contains(cmd.Err().Error(), "WRONGTYPE") {
					t.Fatal("wrong type silently selected compatibility", cmd.Err())
				}
			case "full_count_bound":
				roots, err := readSnapshotRootPrefetch(cmd, 256)
				if err != nil || !reflect.DeepEqual(roots, original) {
					t.Fatal("legitimate bounded root reply changed", len(roots), err)
				}
			default:
				values, err := cmd.Slice()
				if err != nil || !reflect.DeepEqual(values, want) {
					t.Fatal("native map reply changed", values, err)
				}
			}
		})
	}
	if err := raw.Del(h.ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
}
