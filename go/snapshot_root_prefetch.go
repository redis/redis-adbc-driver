// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// A root is bounded before enumeration, atomically with its read. MEMORY USAGE
// must inspect every nested value: the default sample is not a size bound.
// Native command/key ACL checks still apply. A definite decline leaves the
// original separate HGETALL path in place after all acquisition proofs succeed.
const snapshotRootPrefetchLua = `
if not redis.acl_check_cmd('HLEN',KEYS[1]) or
 not redis.acl_check_cmd('HGETALL',KEYS[1]) or
 not redis.acl_check_cmd('MEMORY','USAGE',KEYS[1],'SAMPLES','0') then
 return redis.error_reply('ADBC_ROOT_PREFETCH_DECLINED')
end
local count=redis.call('HLEN',KEYS[1])
if count>256 then return redis.error_reply('ADBC_ROOT_PREFETCH_DECLINED') end
if count==0 then return {} end
local bytes=redis.call('MEMORY','USAGE',KEYS[1],'SAMPLES','0')
if not bytes or bytes>65536 then return redis.error_reply('ADBC_ROOT_PREFETCH_DECLINED') end
return redis.call('HGETALL',KEYS[1])
`

func snapshotRootPrefetchDeclined(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return false
	}
	message := native.Error()
	if message == "ADBC_ROOT_PREFETCH_DECLINED" || message == "ERR ADBC_ROOT_PREFETCH_DECLINED" {
		return true
	}
	return message == "NOPERM this user has no permissions to run the 'eval_ro' command" || strings.HasPrefix(message, "NOPERM User ") && strings.HasSuffix(message, " has no permissions to run the 'eval_ro' command")
}

func snapshotSameNativeReply(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, ok := a.(interface {
		error
		RedisError()
	})
	right, native := b.(interface {
		error
		RedisError()
	})
	return ok && native && left.Error() == right.Error()
}

func (s *store) queueSnapshotRootPrefetch(ctx context.Context, root string, pin *snapshotPin, pipe goredis.Pipeliner) *goredis.Cmd {
	scope := snapshotOf(ctx)
	if pin == nil || pin.shared != nil || pin.reader || s.restrictedReader || scope == nil || scope.store != s || scope.pin != pin || scope.id != root || scope.write {
		return nil
	}
	return once(ctx, pipe, "EVAL_RO", snapshotRootPrefetchLua, 1, snapshotMetadataMap(root))
}

func readSnapshotRootPrefetch(command *goredis.Cmd, expected int64) (map[string]string, error) {
	if command == nil || snapshotRootPrefetchDeclined(command.Err()) {
		return nil, nil
	}
	if err := command.Err(); err != nil {
		return nil, wrapRedis(err, "failed to prefetch snapshot metadata root")
	}
	values, err := command.Slice()
	if err != nil {
		return nil, wrapRedis(err, "invalid snapshot metadata root reply")
	}
	if len(values)%2 != 0 || len(values) > 512 || int64(len(values)/2) != expected {
		return nil, errorf(adbc.StatusIO, "invalid snapshot metadata root reply")
	}
	roots := make(map[string]string, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		field, fieldOK := values[i].(string)
		owner, ownerOK := values[i+1].(string)
		if !fieldOK || !ownerOK {
			return nil, errorf(adbc.StatusIO, "invalid snapshot metadata root reply")
		}
		if _, duplicate := roots[field]; duplicate {
			return nil, errorf(adbc.StatusIO, "invalid snapshot metadata root reply")
		}
		roots[field] = owner
	}
	return roots, nil
}

func rememberSnapshotRootPrefetch(ctx context.Context, root string, pin *snapshotPin, roots map[string]string) error {
	if roots == nil {
		return nil
	}
	scope := snapshotOf(ctx)
	if scope == nil || scope.id != root || scope.pin != pin {
		return errorf(adbc.StatusInvalidState, "snapshot root prefetch scope changed")
	}
	scope.metadataMu.Lock()
	scope.metadata.prefetchedRoots = roots
	scope.metadataMu.Unlock()
	return nil
}
