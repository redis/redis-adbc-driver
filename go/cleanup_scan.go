// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// A managed cleanup fallback must not add SCAN permissions to ordinary readers.
// Capture the allocated-ID watermark and fetch bounded HASH pages instead.
// Gaps/deletes are filtered by fetchRows; raw int64 IDs need no Search doubles.
func (e *executor) scanAllocatedCleanupPages(ctx context.Context, req scanRequest, params []Value, visit func([]string, []map[string]Value) error) error {
	meta := req.meta
	last, err := e.store.client.Get(ctx, seqKey(meta.Schema, meta.Name)).Int64()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		return wrapRedis(err, "reading cleanup scan row sequence failed")
	}
	if last < 0 {
		return errorf(adbc.StatusInvalidState, "invalid cleanup scan row sequence")
	}
	it, fields, _ := e.newScanIter(ctx, req, params)
	for start := int64(1); start <= last; {
		if err := cancelled(ctx); err != nil {
			return err
		}
		if err := e.store.checkKeys(ctx, meta, false); err != nil {
			return err
		}
		n := min(int64(pipelineChunk), last-start+1)
		keys := make([]string, int(n))
		for i := range keys {
			keys[i] = meta.prefix() + strconv.FormatInt(start+int64(i), 10)
		}
		fetched, err := e.store.fetchRows(ctx, keys, fields)
		if err != nil {
			return err
		}
		if err := e.store.checkKeys(ctx, meta, false); err != nil {
			return err
		}
		kept, rows, err := it.keep(keys, fetched)
		if err != nil {
			return err
		}
		if err := visit(kept, rows); err != nil {
			return err
		}
		if n == last-start+1 {
			break // no int64 overflow at MaxInt64
		}
		start += n
	}
	return nil
}
