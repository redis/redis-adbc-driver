// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// TYPE and HMGET run atomically, so a missing/non-HASH source is distinct from
// an existing HASH whose selected fields are all absent. Values are native HASH
// strings, never Search sorting vectors. Each invocation declares one key;
// ordinary application keys need not share a Cluster slot. ACL preflight uses a
// fixed error token so only definite permission refusals select HGETALL.
const unmanagedProjectionReadLua = `
if not redis.acl_check_cmd('TYPE',KEYS[1]) then return redis.error_reply('ADBC_UNMANAGED_PROJECTION_TYPE_DENIED') end
if #ARGV>0 and not redis.acl_check_cmd('HMGET',KEYS[1],unpack(ARGV)) then return redis.error_reply('ADBC_UNMANAGED_PROJECTION_HMGET_DENIED') end
if redis.call('TYPE',KEYS[1]).ok~='hash' then return false end
if #ARGV==0 then return {} end
-- Bound values before HMGET materializes native bulk strings in the Lua VM.
-- This check and the eventual fetch are atomic with respect to live writers.
local bytes=0
for _,field in ipairs(ARGV) do
 if not redis.acl_check_cmd('HSTRLEN',KEYS[1],field) then return redis.error_reply('ADBC_UNMANAGED_PROJECTION_HSTRLEN_DENIED') end
 bytes=bytes+redis.call('HSTRLEN',KEYS[1],field)+16
 if bytes>1048576 then return redis.error_reply('ADBC_UNMANAGED_PROJECTION_TOO_LARGE') end
end
return redis.call('HMGET',KEYS[1],unpack(ARGV))
`

var unmanagedProjectionReadDigest = goredis.NewScript(unmanagedProjectionReadLua).Hash()

const unmanagedProjectionReadBytes = 1 << 20

type unmanagedProjectionRead struct {
	store              *store
	fields             []string
	bytes              int
	prepared, fallback bool // statement-local: never cache application permissions
}

func newUnmanagedProjectionRead(s *store, meta *tableMeta, need map[string]bool) *unmanagedProjectionRead {
	r := &unmanagedProjectionRead{store: s, bytes: len(unmanagedProjectionReadLua) + 64}
	for _, c := range meta.scanColumns() {
		if !need[c.Name] {
			continue
		}
		if c.MissingThrough > 0 {
			return nil
		}
		r.fields = append(r.fields, c.field())
		r.bytes += len(c.field()) + 16
		if len(r.fields) > 256 || r.bytes > unmanagedProjectionReadBytes {
			return nil
		}
	}
	return r
}

func unmanagedProjectionPermission(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return false
	}
	message := native.Error()
	if message == "ADBC_UNMANAGED_PROJECTION_TYPE_DENIED" || message == "ADBC_UNMANAGED_PROJECTION_HMGET_DENIED" || message == "ADBC_UNMANAGED_PROJECTION_HSTRLEN_DENIED" {
		return true
	}
	for _, command := range []string{"evalsha_ro", "eval_ro"} {
		if message == "NOPERM this user has no permissions to run the '"+command+"' command" || strings.HasPrefix(message, "NOPERM User ") && strings.HasSuffix(message, " has no permissions to run the '"+command+"' command") {
			return true
		}
	}
	return false
}
func unmanagedProjectionMissingScript(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	return ok && strings.HasPrefix(native.Error(), "NOSCRIPT ")
}

// Only the fixed read-only guard can decline the fast path on value size.
// Wrapped/joined refusals are unknown outcomes, as with permission refusals.
func unmanagedProjectionTooLarge(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	return ok && native.Error() == "ADBC_UNMANAGED_PROJECTION_TOO_LARGE"
}

// A pipeline error is accounted for only by the same direct native outcome.
// errors.Is/As would accept joined/wrapped absence or a refusal carrying a lost
// transport reply. Such results are indeterminate, never proof of a missing row
// or permission/script-cache refusal. Compare native messages only after direct
// native assertions; unrelated command failures are rejected by batch below.
func unmanagedProjectionSameNativeError(a, b error) bool {
	if a == goredis.Nil || b == goredis.Nil {
		return a == goredis.Nil && b == goredis.Nil
	}
	left, leftOK := a.(interface {
		error
		RedisError()
	})
	right, rightOK := b.(interface {
		error
		RedisError()
	})
	return leftOK && rightOK && left.Error() == right.Error()
}

func (r *unmanagedProjectionRead) read(ctx context.Context, keys []string) ([]aggRow, error) {
	if r == nil {
		return nil, errorf(adbc.StatusInvalidState, "missing unmanaged projection reader")
	}
	for _, key := range keys {
		if !unmanagedSourceKey(key) {
			return nil, errorf(adbc.StatusInvalidArgument, "unmanaged index contains a key overlapping driver storage")
		}
	}
	if r.fallback {
		return r.store.fetchKeyRows(ctx, keys, nil)
	}
	out := make([]aggRow, len(keys))
	for start := 0; start < len(keys); {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		end, bytes := start, 0
		limit := pipelineChunk
		if !r.prepared {
			limit = 1
		} // fail an extra command permission once, not 1000 times
		for end < len(keys) && end-start < limit {
			n := r.bytes + len(keys[end])
			if n > unmanagedProjectionReadBytes {
				if end == start {
					end++
				}
				break
			}
			if end > start && bytes+n > unmanagedProjectionReadBytes {
				break
			}
			bytes += n
			end++
		}
		part := keys[start:end]
		if r.bytes+len(part[0]) > unmanagedProjectionReadBytes {
			rows, err := r.store.fetchKeyRows(ctx, part, nil)
			if err != nil {
				return nil, err
			}
			copy(out[start:end], rows)
			start = end
			continue
		}
		rows, fallback, err := r.batch(ctx, part, false)
		if err != nil {
			return nil, err
		}
		copy(out[start:end], rows)
		r.prepared = true
		if fallback {
			r.fallback = true
		}
		start = end
		if r.fallback && start < len(keys) {
			rows, err := r.store.fetchKeyRows(ctx, keys[start:], nil)
			if err != nil {
				return nil, err
			}
			copy(out[start:], rows)
			break
		}
	}
	return out, nil
}

// Retry only a native NOSCRIPT refusal with the full read-only body. No unknown
// result, cancellation or unaccounted pipeline error is resent. Both the first
// command and full-body attempt carry NoRetry even on Cluster API pipelines.
func (r *unmanagedProjectionRead) batch(ctx context.Context, keys []string, body bool) ([]aggRow, bool, error) {
	pipe := r.store.redis(ctx).Pipeline()
	cmds := make([]*goredis.Cmd, len(keys))
	command, script := "EVALSHA_RO", unmanagedProjectionReadDigest
	if body {
		command, script = "EVAL_RO", unmanagedProjectionReadLua
	}
	for i, key := range keys {
		if !unmanagedSourceKey(key) {
			return nil, false, errorf(adbc.StatusInvalidArgument, "unmanaged index contains a key overlapping driver storage")
		}
		args := make([]any, 0, 4+len(r.fields))
		args = append(args, command, script, 1, key)
		for _, field := range r.fields {
			args = append(args, field)
		}
		cmd := goredis.NewCmd(ctx, args...)
		cmd.SetFirstKeyPos(3)
		cmds[i] = cmd
		if err := pipe.Process(ctx, onceCmd{Cmd: cmd}); err != nil {
			return nil, false, wrapRedis(err, "failed to queue unmanaged projection read")
		}
	}
	_, pipelineErr := pipe.Exec(ctx)
	accountedPipelineError := pipelineErr == nil
	// Inspect every outcome before any fallback or script-cache repair. A failed
	// pipeline may already have populated replies; that alone is not success.
	for _, cmd := range cmds {
		err := cmd.Err()
		if err == nil {
			continue
		}
		accountedPipelineError = accountedPipelineError || unmanagedProjectionSameNativeError(pipelineErr, err)
		if err == goredis.Nil || unmanagedProjectionPermission(err) || unmanagedProjectionTooLarge(err) || !body && unmanagedProjectionMissingScript(err) {
			continue
		}
		return nil, false, wrapRedis(transportContextError(ctx, err), "failed to read unmanaged projection")
	}
	if pipelineErr != nil && (!accountedPipelineError || !(pipelineErr == goredis.Nil || unmanagedProjectionPermission(pipelineErr) || unmanagedProjectionTooLarge(pipelineErr) || !body && unmanagedProjectionMissingScript(pipelineErr))) {
		return nil, false, wrapRedis(transportContextError(ctx, pipelineErr), "failed to read unmanaged projection pipeline")
	}
	out := make([]aggRow, len(keys))
	var compatible, missing []int
	permissionDenied := false
	for i, cmd := range cmds {
		err := cmd.Err()
		if unmanagedProjectionPermission(err) {
			compatible = append(compatible, i)
			permissionDenied = true
			continue
		}
		if unmanagedProjectionTooLarge(err) {
			compatible = append(compatible, i)
			continue
		}
		if !body && unmanagedProjectionMissingScript(err) {
			missing = append(missing, i)
			continue
		}
		if err == goredis.Nil {
			continue
		}
		values, ok := cmd.Val().([]any)
		if !ok || len(values) != len(r.fields) {
			return nil, false, errorf(adbc.StatusInternal, "unexpected unmanaged projection reply %T", cmd.Val())
		}
		row := make(aggRow, len(r.fields)+1)
		for field, value := range values {
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok {
				return nil, false, errorf(adbc.StatusInternal, "unexpected unmanaged projection field %T", value)
			}
			row[r.fields[field]] = text
		}
		// Application HASHes may themselves contain a field named __key.
		// Like fetchKeyRows, the actual source address always wins.
		row[keyField] = keys[i]
		out[i] = row
	}
	fallback := permissionDenied
	if len(missing) > 0 {
		retry := make([]string, len(missing))
		for i, n := range missing {
			retry[i] = keys[n]
		}
		rows, deniedBody, err := r.batch(ctx, retry, true)
		if err != nil {
			return nil, false, err
		}
		for i, n := range missing {
			out[n] = rows[i]
		}
		fallback = fallback || deniedBody
	}
	if len(compatible) > 0 {
		retry := make([]string, len(compatible))
		for n, i := range compatible {
			retry[n] = keys[i]
		}
		rows, err := r.store.fetchKeyRows(ctx, retry, nil)
		if err != nil {
			return nil, false, err
		}
		for i, n := range compatible {
			out[n] = rows[i]
		}
	}
	return out, fallback, nil
}
