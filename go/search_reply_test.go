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
	"reflect"
	"slices"
	"testing"
)

func TestSearchCountReplies(t *testing.T) {
	for name, reply := range map[string]any{
		"RESP2": []any{int64(7)},
		"RESP3": map[any]any{
			"attributes": []any{}, "format": "STRING", "results": []any{},
			"total_results": int64(7), "warning": []any{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseSearchCount(reply)
			if err != nil || got != 7 {
				t.Fatalf("parseSearchCount = %d, %v; want 7", got, err)
			}
		})
	}
}

func TestCursorReplies(t *testing.T) {
	resp2 := []any{
		[]any{int64(2), []any{"label", "one", "n", int64(1)}, []any{"label", "two"}},
		int64(42),
	}
	resp3 := []any{
		map[any]any{
			"attributes":    []any{},
			"format":        "STRING",
			"total_results": int64(2),
			"warning":       []any{},
			"results": []any{
				map[any]any{"extra_attributes": map[any]any{"label": "one", "n": int64(1)}, "values": []any{}},
				map[any]any{"extra_attributes": map[any]any{"label": "two"}, "values": []any{}},
			},
		},
		int64(42),
	}
	want := []aggRow{{"label": "one", "n": "1"}, {"label": "two"}}
	for name, reply := range map[string]any{"RESP2": resp2, "RESP3": resp3} {
		t.Run(name, func(t *testing.T) {
			rows, cursor, err := parseCursorReply(reply)
			if err != nil || cursor != 42 || !reflect.DeepEqual(rows, want) {
				t.Fatalf("parseCursorReply = %#v, %d, %v; want %#v, 42", rows, cursor, err, want)
			}
		})
	}
}

func TestIndexInfoReplies(t *testing.T) {
	resp2 := []any{
		"index_definition", []any{"key_type", "HASH", "prefixes", []any{"public:t:"}, "filter", "@kind == 1"},
		"num_docs", int64(3), "hash_indexing_failures", int64(1), "indexing", int64(0), "percent_indexed", "1",
		"Index Errors", []any{"last indexing error", "bad value", "last indexing error key", "public:t:2"},
		"attributes", []any{
			[]any{"identifier", "s", "attribute", "s", "type", "TAG", "SEPARATOR", "|", "CASESENSITIVE", "SORTABLE"},
			[]any{"identifier", "n", "attribute", "n", "type", "NUMERIC", "SORTABLE"},
		},
	}
	resp3 := map[any]any{
		"index_definition": map[any]any{"key_type": "HASH", "prefixes": []any{"public:t:"}, "filter": "@kind == 1"},
		"num_docs":         int64(3), "hash_indexing_failures": int64(1), "indexing": int64(0), "percent_indexed": float64(1),
		"Index Errors": map[any]any{"last indexing error": "bad value", "last indexing error key": "public:t:2"},
		"attributes": []any{
			map[any]any{"identifier": "s", "attribute": "s", "type": "TAG", "SEPARATOR": "|", "flags": []any{"CASESENSITIVE", "SORTABLE"}},
			map[any]any{"identifier": "n", "attribute": "n", "type": "NUMERIC", "flags": []any{"SORTABLE"}},
		},
	}
	for name, reply := range map[string]any{"RESP2": resp2, "RESP3": resp3} {
		t.Run(name, func(t *testing.T) {
			got := indexInfoOf(reply)
			if got.keyType != "HASH" || !slices.Equal(got.prefixes, []string{"public:t:"}) || got.filter != "@kind == 1" ||
				got.numDocs != 3 || got.failures != 1 || got.indexing || got.percent != 1 ||
				got.lastError != "bad value" || got.lastErrorKey != "public:t:2" || len(got.attrs) != 2 {
				t.Fatalf("indexInfoOf = %+v", got)
			}
			if a := got.attrs[0]; a.identifier != "s" || a.name != "s" || a.typ != "TAG" ||
				!slices.Contains(a.options, "SEPARATOR |") || !slices.Contains(a.options, "CASESENSITIVE") || !slices.Contains(a.options, "SORTABLE") {
				t.Errorf("TAG attribute = %+v", a)
			}
		})
	}
}

func TestProfileReplies(t *testing.T) {
	resp2 := []any{"Shards", []any{[]any{"Warning", []any{"None"}}}, "Coordinator", []any{}}
	resp3 := map[any]any{"Shards": []any{map[any]any{"Warning": []any{"None"}}}, "Coordinator": map[any]any{}}
	for name, reply := range map[string]any{"RESP2": resp2, "RESP3": resp3} {
		t.Run(name, func(t *testing.T) {
			if !profileComplete(reply, 1) {
				t.Fatalf("profileComplete(%#v, 1) = false", reply)
			}
		})
	}
}
