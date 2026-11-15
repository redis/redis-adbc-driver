// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// No server or Search: isolates complete production batch construction and
// decoding, with native replies supplied at the transport-hook boundary.
// Compare identical harnesses on baseline/candidate sources; this is not SQL
// latency, network throughput or evidence of reduced source-command fanout.
func BenchmarkUnmanagedProjectionBatch(b *testing.B) {
	for _, keysPerBatch := range []int{1, pipelineChunk} {
		for _, width := range []int{2, 16, 128} {
			b.Run(fmt.Sprintf("keys_%d/fields_%d", keysPerBatch, width), func(b *testing.B) {
				ctx := context.Background()
				keys := make([]string, keysPerBatch)
				fields := make([]string, width)
				values := make([]any, width)
				for i := range keys {
					keys[i] = fmt.Sprintf("app:batch:%d", i)
				}
				for i := range fields {
					fields[i] = fmt.Sprintf("source_field_%d", i)
					values[i] = "9007199254740993"
				}
				client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
				defer client.Close()
				client.AddHook(unmanagedProjectionBatchBenchHook{values})
				reader := &unmanagedProjectionRead{store: &store{client: client}, fields: fields}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, denied, err := reader.batch(ctx, keys)
					if err != nil || denied || len(got) != len(keys) {
						b.Fatalf("batch differs: %d %t %v", len(got), denied, err)
					}
				}
			})
		}
	}
}

type unmanagedProjectionBatchBenchHook struct{ values []any }

func (unmanagedProjectionBatchBenchHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}
func (unmanagedProjectionBatchBenchHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h unmanagedProjectionBatchBenchHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			setProjectionReply(cmd, h.values, nil)
		}
		return nil
	}
}
