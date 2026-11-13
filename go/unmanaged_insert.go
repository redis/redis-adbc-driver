// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const unmanagedInsertPipelineRows = 128
const unmanagedInsertPipelineBytes = 1 << 20
const unmanagedInsertRowBytes = 64 << 20

// Each script touches one application key, so pipelines can route arbitrary
// source slots without involving managed row fences. Already submitted commands
// can succeed after another command fails. Count all positive acknowledgments,
// return the first failure, and never submit another pipeline or replay this one.
func (s *store) writeUnmanagedKeyRows(ctx context.Context, meta *tableMeta, rows [][]Value) (int64, error) {
	ki, _ := meta.resolve(keyField)
	var written int64
	for start := 0; start < len(rows); {
		var args [][]any
		var keys []string
		bytes := 0
		for i := start; i < len(rows) && len(args) < unmanagedInsertPipelineRows; i++ {
			row := rows[i]
			command := []any{"EVAL", keyRowInsertLua, 1, row[ki].S}
			for j, c := range meta.Columns {
				if j != ki && !row[j].Null {
					command = append(command, c.field(), c.encode(row[j], c.TextFormat))
				}
			}
			size := 32 // RESP array header and margin
			for _, arg := range command {
				size += len(fmt.Sprint(arg)) + 32
			}
			if size > unmanagedInsertRowBytes {
				return written, errorf(adbc.StatusInvalidArgument, "unmanaged INSERT row exceeds 64 MiB")
			}
			if len(args) > 0 && bytes+size > unmanagedInsertPipelineBytes {
				break
			}
			args, keys = append(args, command), append(keys, row[ki].S)
			bytes += size
		}
		rowCtx, err := unmanagedRowContext(ctx, meta, keys)
		if err != nil {
			return written, err
		}
		pipe := s.client.Pipeline()
		commands := make([]*goredis.Cmd, len(args))
		for i, command := range args {
			commands[i] = goredis.NewCmd(rowCtx, command...)
			_ = pipe.Process(rowCtx, onceCmd{commands[i]})
		}
		_, execErr := pipe.Exec(rowCtx)
		var firstErr error
		for i, command := range commands {
			n, err := command.Int()
			if err == nil && n == 1 {
				written++
				continue
			}
			if firstErr != nil {
				continue
			}
			switch {
			case err != nil:
				firstErr = wrapRedis(err, "unmanaged INSERT failed; source writes may be partially applied")
			case n == 0:
				firstErr = stateErrorf(stateUnique, "__key %q already exists", keys[i])
			default:
				firstErr = errorf(adbc.StatusIO, "unexpected unmanaged INSERT acknowledgment")
			}
		}
		if firstErr != nil {
			return written, firstErr
		}
		if execErr != nil {
			return written, wrapRedis(execErr, "unmanaged INSERT failed; source writes may be partially applied")
		}
		start += len(args)
	}
	return written, s.checkKeys(ctx, meta, true)
}
