// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Snapshot discovery has already selected the newest physical version. Unlike
// live unmanaged HASH reads, a vanished winning row is corruption, not omission.
// Tombstones and exact row identities are checked before positional projection.
func (it *scanIter) nextSnapshotHashValues(ctx context.Context, logical, physical []string) ([]string, []map[string]Value, error) {
	fields := append(append([]string(nil), it.fetch...), snapshotDeletedField)
	values, err := it.store.fetchRowSlices(ctx, physical, fields, wideFetchChunk)
	if err != nil {
		return nil, nil, err
	}
	return it.consumeSnapshotHashValues(ctx, logical, values)
}

func (it *scanIter) consumeSnapshotHashValues(ctx context.Context, logical []string, values [][]any) ([]string, []map[string]Value, error) {
	if it.diagnostics != nil {
		it.diagnostics.processed.Add(int64(len(values)))
	}
	kept := make([]string, 0, len(logical))
	for i, row := range values {
		if err := cancelled(it.env.ctx); err != nil {
			return nil, nil, err
		}
		if row == nil || row[0] != strings.TrimPrefix(logical[i], it.meta.prefix()) {
			return nil, nil, errorf(adbc.StatusIO, "snapshot directory references a missing or invalid row")
		}
		if row[len(row)-1] == "1" {
			continue
		}
		if err := it.projection.consumeHash(row[:len(row)-1]); err != nil {
			return nil, nil, err
		}
		kept = append(kept, logical[i])
	}
	if it.diagnostics != nil {
		it.diagnostics.matched.Add(int64(len(kept)))
	}
	return kept, nil, nil
}

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
	return it.consumeHashValues(keys, values)
}

func (it *scanIter) consumeHashValues(keys []string, values [][]any) ([]string, []map[string]Value, error) {
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
