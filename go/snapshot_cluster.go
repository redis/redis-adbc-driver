// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strings"
	"sync"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Cluster pipelines pass through both coordinator and node hooks. Fenced
// commands are EVAL by then and already-scoped keys are left unchanged. Direct
// Search/WATCH calls must also have fences, including on newly discovered nodes.
// The returned function attaches to nodes discovered before installation.
func addSnapshotHooks(client goredis.UniversalClient) func(context.Context) error {
	cluster, ok := client.(*goredis.ClusterClient)
	hook := snapshotHook{cluster: cluster}
	client.AddHook(hook)
	if !ok {
		return func(context.Context) error { return nil }
	}
	var seen sync.Map
	add := func(node *goredis.Client) {
		if _, loaded := seen.LoadOrStore(node, true); !loaded {
			node.AddHook(hook)
		}
	}
	cluster.OnNewNode(add)
	return func(ctx context.Context) error {
		return cluster.ForEachShard(ctx, func(_ context.Context, node *goredis.Client) error { add(node); return nil })
	}
}

func checkSnapshotServerInfo(info string, cluster bool) error {
	if !strings.Contains(info, "maxmemory_policy:noeviction") {
		return errorf(adbc.StatusInvalidState, "snapshot catalogs require maxmemory-policy noeviction")
	}
	if strings.Contains(strings.ToLower(info), "crdt_") || !strings.Contains(info, "role:master") {
		return errorf(adbc.StatusNotImplemented, "snapshot catalogs require primary reads without Active-Active replication")
	}
	if !cluster && !strings.Contains(info, "cluster_enabled:0") {
		return errorf(adbc.StatusInvalidArgument, "Cluster API endpoints require adbc.redis.cluster=true or auto")
	}
	return nil
}

func (s *store) checkSnapshotDeployment(ctx context.Context, requireEmpty bool) error {
	check := func(client goredis.Cmdable, cluster bool) error {
		info, err := client.Info(ctx, "all").Result()
		if err != nil {
			return wrapRedis(err, "snapshot deployment verification failed")
		}
		if err := checkSnapshotServerInfo(info, cluster); err != nil {
			return err
		}
		if requireEmpty {
			n, err := client.DBSize(ctx).Result()
			if err != nil {
				return err
			}
			if n != 0 {
				return errorf(adbc.StatusInvalidState, "managed-table snapshots require a fresh empty database; existing storage is not upgraded or modified")
			}
		}
		return nil
	}
	if cluster, ok := s.client.(*goredis.ClusterClient); ok {
		return cluster.ForEachMaster(ctx, func(_ context.Context, node *goredis.Client) error { return check(node, true) })
	}
	return check(s.client, false)
}
