// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"github.com/apache/arrow-adbc/go/adbc"
	"testing"
)

func TestRangeReadinessReplies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  any
		status adbc.Status
	}{
		{"RESP2 ready", []any{"indexing", int64(0), "percent_indexed", "1"}, adbc.StatusOK},
		{"RESP3 ready", map[any]any{"indexing": int64(0), "percent_indexed": float64(1)}, adbc.StatusOK},
		{"backfill", []any{"indexing", int64(1), "percent_indexed", "1.907"}, adbc.StatusInvalidState},
		{"partial", []any{"indexing", int64(0), "percent_indexed", "0.9"}, adbc.StatusInvalidState},
		{"missing", []any{}, adbc.StatusNotImplemented},
		{"multiple building shards", []any{"indexing", int64(2), "percent_indexed", "1"}, adbc.StatusInvalidState},
		{"building with NaN progress", []any{"indexing", float64(1), "percent_indexed", "nan"}, adbc.StatusInvalidState},
		{"numeric flag", []any{"indexing", float64(0), "percent_indexed", "1"}, adbc.StatusOK},
		{"nan", []any{"indexing", int64(0), "percent_indexed", "nan"}, adbc.StatusNotImplemented},
		{"infinite", []any{"indexing", int64(0), "percent_indexed", "inf"}, adbc.StatusNotImplemented},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkRangeReadinessReply(tc.reply)
			if tc.status == adbc.StatusOK {
				if err != nil {
					t.Fatal(err)
				}
			} else if !isStatus(err, tc.status) {
				t.Fatalf("expected %v, got %v", tc.status, err)
			}
		})
	}
}
