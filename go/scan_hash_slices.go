// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
)

// fetchRowSlices retains each command's owned reply slice for one cursor page.
// It skips vanished rows before projection; callers never retain these replies
// in Arrow records. The generic path still materializes aggRow maps.
func (s *store) fetchRowSlices(ctx context.Context, keys []string, fields []string, chunk int) ([][]any, error) {
	fields = append([]string{rowIDField}, fields...)
	out := make([][]any, len(keys))
	for start := 0; start < len(keys); start += chunk {
		end := min(start+chunk, len(keys))
		cmds, err := s.fetchHashCommands(ctx, keys[start:end], fields)
		if err != nil {
			return nil, err
		}
		for i, cmd := range cmds {
			if cmd == nil {
				continue
			}
			vals, err := cmd.Result()
			if err != nil {
				return nil, wrapRedis(err, "failed to read rows")
			}
			if len(vals) != len(fields) {
				return nil, errorf(adbc.StatusInternal, "unexpected HMGET row width %d", len(vals))
			}
			if vals[0] == nil {
				continue // row vanished
			}
			out[start+i] = vals
		}
	}
	return out, nil
}

// nextHashValues mirrors keep's cancellation and diagnostics, bypassing raw
// row maps only while the simple projection owns an active Arrow/scalar callback.
func (it *scanIter) nextHashValues(ctx context.Context, keys []string) ([]string, []map[string]Value, error) {
	chunk := pipelineChunk
	if it.hashFetch {
		chunk = wideFetchChunk
	}
	values, err := it.store.fetchRowSlices(ctx, keys, it.fetch, chunk)
	if err != nil {
		return nil, nil, err
	}
	if it.diagnostics != nil {
		it.diagnostics.processed.Add(int64(len(values)))
	}
	kept := make([]string, 0, len(keys))
	for i, row := range values {
		if row == nil {
			continue
		}
		if err := cancelled(it.env.ctx); err != nil {
			return nil, nil, err
		}
		if err := it.projection.consumeHash(row); err != nil {
			return nil, nil, err
		}
		kept = append(kept, keys[i])
	}
	if it.diagnostics != nil {
		it.diagnostics.matched.Add(int64(len(kept)))
	}
	return kept, nil, nil
}
