// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"slices"
	"testing"
)

func TestSearchKeyRepliesPreserveBinaryIdentity(t *testing.T) {
	key := []byte("prefix:byte\x00\xff")
	for name, reply := range map[string]any{
		"RESP2": []any{int64(200), "", key},
		"RESP3": map[any]any{"total_results": int64(200), "results": []any{
			map[any]any{"id": "", "values": []any{}},
			map[string]any{"id": key, "extra_attributes": nil},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseSearchKeys(reply)
			if err != nil || !slices.Equal(got, []string{"", "prefix:byte\x00\xff"}) {
				t.Fatalf("keys=%q error=%v", got, err)
			}
			key[0] = 'X'
			if got[1] != "prefix:byte\x00\xff" {
				t.Fatal("document ID borrowed mutable response bytes")
			}
			key[0] = 'p'
		})
	}
	for _, reply := range []any{[]any{int64(0)}, map[string]any{"total_results": int64(0), "results": []any{}}} {
		if got, err := parseSearchKeys(reply); err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
}

func TestSearchKeyRepliesRejectMalformedIdentity(t *testing.T) {
	for _, reply := range []any{
		nil, []any{}, []any{int64(-1)}, []any{int64(0), "key"}, []any{int64(1), nil},
		map[string]any{"total_results": int64(1)},
		map[string]any{"total_results": int64(1), "results": []any{map[string]any{"extra_attributes": map[string]any{"__key": "wrong"}}}},
		map[any]any{"total_results": int64(1), "results": []any{map[any]any{"id": int64(1)}}},
	} {
		if got, err := parseSearchKeys(reply); err == nil || got != nil {
			t.Fatalf("accepted malformed key response: %#v", reply)
		}
	}
}
