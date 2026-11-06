// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strings"
	"sync/atomic"

	goredis "github.com/redis/go-redis/v9"
)

var snapshotControlDigest = goredis.NewScript(snapshotControl).Hash()

// Only standalone control calls use this executor. Row/image pipelines retain
// their existing full-body commands and uncertain-outcome handling.
type snapshotControlExecutor struct {
	evalOnly atomic.Bool
}

func snapshotControlScriptRefusal(err error) (missing, commandDenied bool) {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return false, false
	}
	message := native.Error()
	if strings.HasPrefix(message, "NOSCRIPT ") {
		return true, false
	}
	// The fixed control script never invokes EVALSHA. Match only the native
	// top-level command refusal, not a Lua/key/transport permission failure.
	if message == "NOPERM this user has no permissions to run the 'evalsha' command" ||
		strings.HasPrefix(message, "NOPERM User ") && strings.HasSuffix(message, " has no permissions to run the 'evalsha' command") {
		return false, true
	}
	return false, false
}

func (e *snapshotControlExecutor) run(ctx context.Context, client processor, keys []any, argv ...any) *goredis.Cmd {
	command, body := "EVALSHA", snapshotControlDigest
	if e.evalOnly.Load() {
		command, body = "EVAL", snapshotControl
	}
	args := []any{command, body, len(keys)}
	args = append(args, keys...)
	args = append(args, argv...)
	if err := ctx.Err(); err != nil {
		cmd := goredis.NewCmd(ctx, args...)
		cmd.SetErr(err)
		return cmd
	}
	cmd := once(ctx, client, args...)
	if command != "EVALSHA" {
		return cmd
	}
	missing, denied := snapshotControlScriptRefusal(cmd.Err())
	if !missing && !denied {
		return cmd
	}
	if err := ctx.Err(); err != nil {
		cmd.SetErr(err)
		return cmd
	}
	if denied {
		e.evalOnly.Store(true)
	}
	// A definite refusal means the script did not execute. The one full-body
	// attempt keeps identical keys/arguments and the existing no-retry marker.
	// Never mutate the first command's argument slice or resend an unknown reply.
	fallback := append([]any(nil), args...)
	fallback[0], fallback[1] = "EVAL", snapshotControl
	return once(ctx, client, fallback...)
}
