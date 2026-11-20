// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	goredis "github.com/redis/go-redis/v9"
	"strings"
)

func unmanagedPartitionWrongType(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	return ok && strings.HasPrefix(native.Error(), "WRONGTYPE ")
}

// A vanished/retyped source row is a live omission only after exact native
// evidence. A wrapped/joined refusal can also carry reply loss; never turn it
// or an unaccounted outer pipeline error into successful partial output.
func (s *store) fetchUnmanagedPartitionRows(ctx context.Context, keys []string) ([]aggRow, error) {
	out := make([]aggRow, len(keys))
	for start := 0; start < len(keys); start += 128 {
		end := min(start+128, len(keys))
		pipe := s.redis(ctx).Pipeline()
		cmds := make([]*goredis.MapStringStringCmd, end-start)
		for i, key := range keys[start:end] {
			cmds[i] = goredis.NewMapStringStringCmd(ctx, "HGETALL", key)
			_ = pipe.Process(ctx, unmanagedProjectionCmd{cmds[i]})
		}
		_, outer := pipe.Exec(ctx)
		nativeWrong := false
		for i, cmd := range cmds {
			row, err := cmd.Result()
			if unmanagedPartitionWrongType(err) {
				nativeWrong = true
				continue
			}
			if err != nil {
				return nil, wrapRedis(err, "unmanaged partition HASH read failed")
			}
			if len(row) == 0 {
				continue
			}
			row[keyField] = keys[start+i]
			out[start+i] = aggRow(row)
		}
		if outer != nil && !(nativeWrong && unmanagedPartitionWrongType(outer)) {
			return nil, wrapRedis(outer, "unmanaged partition HASH pipeline failed")
		}
	}
	return out, nil
}
