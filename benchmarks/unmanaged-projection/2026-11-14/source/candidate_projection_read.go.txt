// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const unmanagedProjectionReadBytes = 1 << 20

type unmanagedProjectionRead struct {
	store              *store
	fields             []string
	bytes              int  // conservative encoded HMGET request bytes excluding its key
	prepared, fallback bool // scan-local: never cache application permissions
}

func newUnmanagedProjectionRead(s *store, meta *tableMeta, need map[string]bool) *unmanagedProjectionRead {
	r := &unmanagedProjectionRead{store: s, bytes: 64}
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

// Every native command owns its outcome. Do not let a client retry a read after
// a lost reply and silently turn it into a later live source observation.
type unmanagedProjectionCmd struct{ goredis.Cmder }

func (unmanagedProjectionCmd) NoRetry() bool { return true }

func unmanagedProjectionPermission(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return false
	}
	message := native.Error()
	return message == "NOPERM this user has no permissions to run the 'hmget' command" || strings.HasPrefix(message, "NOPERM User ") && strings.HasSuffix(message, " has no permissions to run the 'hmget' command")
}
func unmanagedProjectionWrongType(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	return ok && isWrongType(native)
}
func unmanagedProjectionSameNativeError(a, b error) bool {
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

// A non-NULL HMGET value (including an empty string) proves that a HASH existed
// at that atomic native read. An all-NULL vector cannot distinguish a missing
// key from an existing HASH with absent selected fields, so read that row with
// the original atomic HGETALL path. This may observe a later live row image;
// unmanaged scans do not promise a statement snapshot. Never combine the first
// vector with fields or existence from the later read: emit the complete later
// image, and let the caller recheck the existing residual predicate.
func (r *unmanagedProjectionRead) read(ctx context.Context, keys []string) ([]aggRow, error) {
	if r == nil {
		return nil, errorf(adbc.StatusInvalidState, "missing unmanaged projection reader")
	}
	for _, key := range keys {
		if !unmanagedSourceKey(key) {
			return nil, errorf(adbc.StatusInvalidArgument, "unmanaged index contains a key overlapping driver storage")
		}
	}
	if r.fallback || len(r.fields) == 0 {
		return r.native(ctx, keys)
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
		}
		for end < len(keys) && end-start < limit {
			n := r.bytes + len(keys[end])
			if n > unmanagedProjectionReadBytes {
				break
			}
			if end > start && bytes+n > unmanagedProjectionReadBytes {
				break
			}
			bytes += n
			end++
		}
		if end == start {
			rows, err := r.native(ctx, keys[start:start+1])
			if err != nil {
				return nil, err
			}
			out[start] = rows[0]
			start++
			continue
		}
		rows, denied, err := r.batch(ctx, keys[start:end])
		if err != nil {
			return nil, err
		}
		copy(out[start:end], rows)
		r.prepared = true
		r.fallback = denied
		start = end
		if r.fallback && start < len(keys) {
			rows, err := r.native(ctx, keys[start:])
			if err != nil {
				return nil, err
			}
			copy(out[start:], rows)
			break
		}
	}
	return out, nil
}

func (r *unmanagedProjectionRead) batch(ctx context.Context, keys []string) ([]aggRow, bool, error) {
	pipe := r.store.redis(ctx).Pipeline()
	cmds := make([]*goredis.SliceCmd, len(keys))
	for i, key := range keys {
		args := make([]any, 0, 2+len(r.fields))
		args = append(args, "HMGET", key)
		for _, field := range r.fields {
			args = append(args, field)
		}
		cmd := goredis.NewSliceCmd(ctx, args...)
		cmd.SetFirstKeyPos(1)
		cmds[i] = cmd
		if err := pipe.Process(ctx, unmanagedProjectionCmd{cmd}); err != nil {
			return nil, false, wrapRedis(err, "failed to queue unmanaged projection read")
		}
	}
	_, pipelineErr := pipe.Exec(ctx)
	accounted := pipelineErr == nil
	// Inspect every reply and the outer outcome before any all-NULL/permission
	// fallback. A wrapped/joined native refusal or Nil can also carry reply loss.
	for _, cmd := range cmds {
		err := cmd.Err()
		if err == nil {
			continue
		}
		accounted = accounted || unmanagedProjectionSameNativeError(pipelineErr, err)
		if !unmanagedProjectionWrongType(err) && !unmanagedProjectionPermission(err) {
			return nil, false, wrapRedis(transportContextError(ctx, err), "failed to read unmanaged projection")
		}
	}
	if pipelineErr != nil && (!accounted || !unmanagedProjectionWrongType(pipelineErr) && !unmanagedProjectionPermission(pipelineErr)) {
		return nil, false, wrapRedis(transportContextError(ctx, pipelineErr), "failed to read unmanaged projection pipeline")
	}
	out := make([]aggRow, len(keys))
	var fallback []int
	denied := false
	for i, cmd := range cmds {
		if unmanagedProjectionWrongType(cmd.Err()) {
			continue
		}
		if unmanagedProjectionPermission(cmd.Err()) {
			fallback = append(fallback, i)
			denied = true
			continue
		}
		values := cmd.Val()
		if len(values) != len(r.fields) {
			return nil, false, errorf(adbc.StatusInternal, "unexpected unmanaged projection width %d", len(values))
		}
		var row aggRow
		for field, value := range values {
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok {
				return nil, false, errorf(adbc.StatusInternal, "unexpected unmanaged projection field %T", value)
			}
			if row == nil {
				row = make(aggRow, len(r.fields)+1)
			}
			row[r.fields[field]] = text
		}
		if row == nil {
			fallback = append(fallback, i)
			continue
		}
		row[keyField] = keys[i]
		out[i] = row
	}
	if len(fallback) > 0 {
		retry := make([]string, len(fallback))
		for i, n := range fallback {
			retry[i] = keys[n]
		}
		rows, err := r.native(ctx, retry)
		if err != nil {
			return nil, false, err
		}
		for i, n := range fallback {
			out[n] = rows[i]
		}
	}
	return out, denied, nil
}

// native retains original HGETALL source semantics but checks only direct
// WRONGTYPE outcomes and accounts for the whole pipeline before returning rows.
// A large external field is copied once as a native reply, never into a Lua VM.
func (r *unmanagedProjectionRead) native(ctx context.Context, keys []string) ([]aggRow, error) {
	out := make([]aggRow, len(keys))
	for start := 0; start < len(keys); {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		end, bytes := start, 0
		for end < len(keys) && end-start < pipelineChunk {
			n := 64 + len(keys[end])
			if end > start && bytes+n > unmanagedProjectionReadBytes {
				break
			}
			bytes += n
			end++
		}
		pipe := r.store.redis(ctx).Pipeline()
		cmds := make([]*goredis.MapStringStringCmd, end-start)
		for i, key := range keys[start:end] {
			cmd := goredis.NewMapStringStringCmd(ctx, "HGETALL", key)
			cmd.SetFirstKeyPos(1)
			cmds[i] = cmd
			if err := pipe.Process(ctx, unmanagedProjectionCmd{cmd}); err != nil {
				return nil, wrapRedis(err, "failed to queue unmanaged rows")
			}
		}
		_, pipelineErr := pipe.Exec(ctx)
		accounted := pipelineErr == nil
		for _, cmd := range cmds {
			err := cmd.Err()
			if err == nil {
				continue
			}
			accounted = accounted || unmanagedProjectionSameNativeError(pipelineErr, err)
			if !unmanagedProjectionWrongType(err) {
				return nil, wrapRedis(transportContextError(ctx, err), "failed to read unmanaged rows")
			}
		}
		if pipelineErr != nil && (!accounted || !unmanagedProjectionWrongType(pipelineErr)) {
			return nil, wrapRedis(transportContextError(ctx, pipelineErr), "failed to read unmanaged rows pipeline")
		}
		for i, cmd := range cmds {
			if unmanagedProjectionWrongType(cmd.Err()) || len(cmd.Val()) == 0 {
				continue
			}
			row := aggRow(cmd.Val())
			row[keyField] = keys[start+i]
			out[start+i] = row
		}
		start = end
	}
	return out, nil
}
