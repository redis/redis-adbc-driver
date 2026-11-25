// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// keyspaceWaitPerKey is the extra wait keyspaceWait allows for each key the
// database holds. Search's initial scan of a new index and a SCAN for a test's
// own prefix both visit every key, not only the test's. On one Software 8.8
// shard with 1.94M unrelated keys an initial scan took 36 µs per key (70 s for
// an index matching no keys) and SCAN COUNT 1000 0.5 µs; see #653 and
// docs/sdlc/changes/keyspace-scaled-test-waits.
const keyspaceWaitPerKey = 50 * time.Microsecond

// keyspaceWait scales a test's fixed wait (base) with the number of keys the
// database holds, summed over a cluster's primaries, so that a wait that suits
// a dedicated database does not fail on one that also holds unrelated data,
// such as a loaded TPC-H subset (#653). A DBSIZE failure keeps base.
func keyspaceWait(ctx context.Context, client goredis.UniversalClient, base time.Duration) time.Duration {
	var keys atomic.Int64
	count := func(ctx context.Context, c *goredis.Client) error {
		n, err := c.DBSize(ctx).Result()
		keys.Add(n)
		return err
	}
	var err error
	if cc, ok := client.(*goredis.ClusterClient); ok {
		err = cc.ForEachMaster(ctx, count)
	} else if c, ok := client.(*goredis.Client); ok {
		err = count(ctx, c)
	}
	if err != nil {
		return base
	}
	return base + time.Duration(keys.Load())*keyspaceWaitPerKey
}

// clearPrefix deletes every key under prefix on each primary of client, with a
// keyspace-scaled deadline (keyspaceWait) and SCAN pages of 1,000 keys.
func clearPrefix(t *testing.T, client goredis.UniversalClient, prefix string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), keyspaceWait(context.Background(), client, 10*time.Second))
	defer cancel()
	clear := func(ctx context.Context, c *goredis.Client) error {
		iter := c.Scan(ctx, 0, globEscape(prefix)+"*", 1000).Iterator()
		for iter.Next(ctx) {
			if err := c.Del(ctx, iter.Val()).Err(); err != nil {
				return err
			}
		}
		return iter.Err()
	}
	if cc, ok := client.(*goredis.ClusterClient); ok {
		if err := cc.ForEachMaster(ctx, clear); err != nil {
			t.Error(err)
		}
	} else if err := clear(ctx, client.(*goredis.Client)); err != nil {
		t.Error(err)
	}
}
