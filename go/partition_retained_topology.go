// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// SCAN across separate OSS primaries is not a snapshot across slot moves.
// Reject active/changed routing while capturing sources. Committed result
// readers still use normal single-key ASK/MOVED routing and never rescan SQL.
func retainedSelfTopology(reply string) (string, error) {
	for _, line := range strings.Split(reply, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		flags := "," + fields[2] + ","
		if !strings.Contains(flags, ",myself,") {
			continue
		}
		if !strings.Contains(flags, ",master,") || strings.Contains(flags, ",fail,") || strings.Contains(flags, ",fail?,") || fields[7] != "connected" {
			return "", errorf(adbc.StatusInvalidState, "retained source Cluster routing is not stable")
		}
		if _, err := strconv.ParseUint(fields[6], 10, 64); err != nil {
			return "", errorf(adbc.StatusInvalidData, "invalid retained source Cluster epoch")
		}
		for _, slot := range fields[8:] {
			if strings.HasPrefix(slot, "[") {
				return "", errorf(adbc.StatusInvalidState, "retained source has an active slot migration; retry after routing converges")
			}
		}
		// Exclude ping/pong timestamps, which change without a routing change.
		stable := append([]string{fields[0], fields[6]}, fields[8:]...)
		return strings.Join(stable, " "), nil
	}
	return "", errorf(adbc.StatusInvalidData, "retained source Cluster primary identity unavailable")
}
func (s *store) retainedTopology(ctx context.Context) (string, error) {
	cluster, ok := s.client.(*goredis.ClusterClient)
	if !ok {
		return "proxy", nil
	}
	slots, err := cluster.ClusterSlots(ctx).Result()
	if err != nil {
		return "", wrapRedis(err, "retained source Cluster slots unavailable")
	}
	type route struct {
		Start, End int
		Primary    string
	}
	routes := make([]route, len(slots))
	for i, slot := range slots {
		if len(slot.Nodes) == 0 || slot.Nodes[0].ID == "" {
			return "", errorf(adbc.StatusInvalidData, "retained source Cluster slot owner unavailable")
		}
		routes[i] = route{slot.Start, slot.End, slot.Nodes[0].ID}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Start < routes[j].Start })
	var mu sync.Mutex
	var primaries []string
	if err := cluster.ForEachMaster(ctx, func(ctx context.Context, node *goredis.Client) error {
		reply, err := node.ClusterNodes(ctx).Result()
		if err != nil {
			if strings.Contains(err.Error(), "NOPERM") {
				return errorf(adbc.StatusUnauthorized, "retained Cluster source capture requires CLUSTER NODES permission")
			}
			return wrapRedis(err, "retained source Cluster topology unavailable")
		}
		primary, err := retainedSelfTopology(reply)
		if err != nil {
			return err
		}
		mu.Lock()
		primaries = append(primaries, primary)
		mu.Unlock()
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(primaries)
	data, _ := json.Marshal([]any{routes, primaries})
	return retainedSHA(data), nil
}
