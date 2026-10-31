// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const OptionStringPartitionControlPersistence = "adbc.redis.partition_control_persistence"

func retainedControlPolicyAllowed(policy string) bool {
	switch policy {
	case "noeviction", "volatile-lru", "volatile-lfu", "volatile-random", "volatile-ttl", "volatile-lrm":
		return true
	}
	return false
}

// Logical quotas rely on persistent control ledgers surviving payload eviction.
// Volatile policies are supported: a missing TTL-bearing job/chunk fails replay
// without dropping its persistent quota charge. Allkeys policies are not safe.
func (s *retainedStore) checkControlPolicy(ctx context.Context, declaration string) error {
	if declaration == "guaranteed" {
		return nil
	}
	check := func(client *goredis.Client) error {
		config, err := client.ConfigGet(ctx, "maxmemory-policy").Result()
		return retainedControlPolicyReply(config, err)
	}
	if cluster, ok := s.client.(*goredis.ClusterClient); ok {
		return cluster.ForEachMaster(ctx, func(_ context.Context, client *goredis.Client) error { return check(client) })
	}
	return check(s.client.(*goredis.Client))
}

// Software's proxy may return an empty successful CONFIG reply. That is an
// unavailable capability, not evidence of an unsafe (or safe) eviction policy.
func retainedControlPolicyReply(config map[string]string, err error) error {
	policy, present := config["maxmemory-policy"]
	if err != nil || !present || strings.TrimSpace(policy) == "" {
		return errorf(adbc.StatusNotImplemented, "retained quotas require CONFIG GET maxmemory-policy or an externally verified partition_control_persistence=guaranteed declaration")
	}
	if !retainedControlPolicyAllowed(strings.ToLower(policy)) {
		return errorf(adbc.StatusInvalidState, "retained quotas require noeviction or a volatile eviction policy that preserves persistent control keys")
	}
	return nil
}
