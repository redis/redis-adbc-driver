// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	goredis "github.com/redis/go-redis/v9"
)

var snapshotControlDigest = redisFunctionName(snapshotControl)

type snapshotControlExecutor struct{}

func (e *snapshotControlExecutor) run(ctx context.Context, client processor, keys []any, argv ...any) *goredis.Cmd {
	args := []any{"FCALL", snapshotControlDigest, len(keys)}
	args = append(args, keys...)
	args = append(args, argv...)
	return e.runScript(ctx, client, snapshotControlDigest, args)
}

// Every outcome, including a missing function or an ACL refusal, is returned
// without fallback. Unknown mutation replies retain their durable resolution.
func (e *snapshotControlExecutor) runScript(ctx context.Context, client processor, name string, args []any) *goredis.Cmd {
	args[0], args[1] = "FCALL", name
	if err := ctx.Err(); err != nil {
		cmd := goredis.NewCmd(ctx, args...)
		cmd.SetErr(err)
		return cmd
	}
	return once(ctx, client, args...)
}
