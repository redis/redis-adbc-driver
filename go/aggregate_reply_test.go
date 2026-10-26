// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestAggregateRawFieldsCompatibility(t *testing.T) {
	bytes := []byte("a\x00b")
	fields := []any{"s", bytes, "i", int64(-9223372036854775808), "f", float64(1.25),
		"r", float32(2.5), "n", int(3), "nil", nil, "list", []any{"x"},
		"dup", "first", "dup", nil, "last", nil, "last", "final", 4, "ignored", "trailing"}
	want := aggRow{"s": "a\x00b", "i": "-9223372036854775808", "f": "1.25", "r": "2.5", "n": "3", "last": "final"}
	for _, input := range []any{fields,
		map[any]any{"extra_attributes": map[any]any{"s": bytes, "i": int64(-9223372036854775808), "f": float64(1.25), "r": float32(2.5), "n": 3, "last": "final", "nil": nil, "list": []any{"x"}, 4: "ignored"}},
		map[string]any{"extra_attributes": map[string]any{"s": bytes, "i": int64(-9223372036854775808), "f": float64(1.25), "r": float32(2.5), "n": 3, "last": "final"}},
	} {
		got, ok := parseAggregateRow(input)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("%T: got %v, want %v", input, got, want)
		}
	}
	got, _ := parseAggregateRow(fields)
	bytes[0] = 'z'
	if got["s"] != "a\x00b" {
		t.Fatal("raw row borrowed mutable response bytes")
	}
}

func TestAggregateReplyMalformedAndReducers(t *testing.T) {
	for _, row := range []any{nil, "row", map[string]any{}, map[any]any{"extra_attributes": []any{"x", "y"}}} {
		reply := []any{[]any{int64(1), row}, int64(42)}
		for _, lists := range []map[string]string{nil, {"items": "count"}} {
			rows, cursor, err := parseCursorReplyLists(reply, lists)
			if rows != nil || cursor != 42 || err == nil || !strings.Contains(err.Error(), "unexpected FT.AGGREGATE row") {
				t.Fatalf("%#v: rows=%v cursor=%d error=%v", row, rows, cursor, err)
			}
		}
	}
	for _, reply := range []any{nil, []any{}, []any{nil, "bad"}, []any{nil, int64(42)}} {
		if _, _, err := parseCursorReply(reply); err == nil {
			t.Fatalf("accepted malformed outer reply %#v", reply)
		}
	}
	for _, row := range []any{
		[]any{"items", []any{"a", "b"}, "s", "ok"},
		map[any]any{"extra_attributes": map[any]any{"items": []any{"a", "b"}, "s": "ok"}},
	} {
		reply := []any{[]any{int64(1), row}, int64(0)}
		got, _, err := parseCursorReplyLists(reply, map[string]string{"items": "count"})
		if err != nil || !reflect.DeepEqual(got, []aggRow{{"s": "ok", "count": "2"}}) {
			t.Fatalf("list reducer: %v, %v", got, err)
		}
	}
	for _, row := range []any{[]any{}, map[any]any{"extra_attributes": map[any]any(nil)}} {
		got, ok := parseAggregateRow(row)
		if !ok || got == nil || len(got) != 0 {
			t.Fatalf("empty field container: %v, %v", got, ok)
		}
	}
}

var cursorBenchmarkRows []aggRow

// Generic uses the unchanged list-aware field normalization with no matching
// list alias. Both alternatives parse identical complete synthetic replies.
func BenchmarkCursorReply(b *testing.B) {
	for _, width := range []int{3, 105} {
		for _, protocol := range []int{2, 3} {
			results := []any{int64(256)}
			for range 256 {
				pairs := make([]any, 0, 2*width)
				fields := map[any]any{}
				for i := range width {
					key := fmt.Sprintf("c%d", i)
					pairs = append(pairs, key, "payload-0123456789")
					fields[key] = "payload-0123456789"
				}
				if protocol == 2 {
					results = append(results, pairs)
				} else {
					results = append(results, map[any]any{"extra_attributes": fields, "values": []any{}})
				}
			}
			var body any = results
			if protocol == 3 {
				body = map[any]any{"results": results[1:], "total_results": int64(256)}
			}
			reply := []any{body, int64(42)}
			for _, direct := range []bool{false, true} {
				b.Run(fmt.Sprintf("columns=%d/RESP%d/direct=%t", width, protocol, direct), func(b *testing.B) {
					var lists map[string]string
					if !direct {
						lists = map[string]string{"unused": "unused_length"}
					}
					b.ReportAllocs()
					for b.Loop() {
						var err error
						cursorBenchmarkRows, _, err = parseCursorReplyLists(reply, lists)
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestAggregateIdentityListFields(t *testing.T) {
	bytes := []byte("a\x00b")
	want := aggRow{"s": "a\x00b", "n": "9007199254740993", "items": "2", "allnull": "1", "empty": "0"}
	for _, row := range []any{
		[]any{"s", bytes, "n", int64(9007199254740993), "items", []any{"x", "y"}, "allnull", []any{nil}, "empty", []any{}, "removed", []any{"x"}, "removed", nil, "ignored", []any{"x"}, 4, "skip", "trailing"},
		map[any]any{"extra_attributes": map[any]any{"s": bytes, "n": int64(9007199254740993), "items": []any{"x", "y"}, "allnull": []any{nil}, "empty": []any{}, "removed": nil, "ignored": []any{"x"}, 4: "skip"}},
		map[string]any{"extra_attributes": map[string]any{"s": bytes, "n": int64(9007199254740993), "items": []any{"x", "y"}, "allnull": []any{nil}, "empty": []any{}, "removed": nil}},
	} {
		lists := map[string]string{"items": "items", "allnull": "allnull", "empty": "empty", "removed": "removed"}
		got, cursor, err := parseCursorReplyLists([]any{[]any{int64(1), row}, int64(42)}, lists)
		if err != nil || cursor != 42 || !reflect.DeepEqual(got, []aggRow{want}) {
			t.Fatalf("%T: %v %d %v", row, got, cursor, err)
		}
	}
	got, ok := parseAggregateRow([]any{"", []any{"x"}})
	if !ok || len(got) != 0 {
		t.Fatal("empty key became a reducer without registration", got)
	}
}
