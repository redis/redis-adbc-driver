// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// A protocol fixture injects missing replies without running server workloads.
// RESP3 and a disabled read timeout must obey caller deadlines.
func TestTransportDeadlineBoundsBlockedCommandAndPipeline(t *testing.T) {
	for _, protocol := range []int{3} {
		for _, readTimeout := range []time.Duration{0, time.Minute} {
			for _, clustered := range []bool{false, true} {
				for _, mode := range []string{"command", "pipeline", "cursor"} {
					t.Run(fmt.Sprintf("resp%d/read=%s/cluster=%t/%s", protocol, readTimeout, clustered, mode), func(t *testing.T) {
						server := newFakeRedis(t, "HGET", "SET", "FT.CURSOR")
						cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: protocol, DisableIdentity: true})
						cfg.cluster.ClusterSlots = func(context.Context) ([]goredis.ClusterSlot, error) {
							return []goredis.ClusterSlot{{Start: 0, End: 16383, Nodes: []goredis.ClusterNode{{Addr: server.addr}}}}, nil
						}
						client := newClient(cfg, clustered, timeouts{read: readTimeout, write: readTimeout})
						defer client.Close()
						if err := client.Ping(context.Background()).Err(); err != nil {
							t.Fatal(err)
						}
						ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
						defer cancel()
						start := time.Now()
						var err error
						if mode == "pipeline" {
							_, err = client.Pipelined(ctx, func(p goredis.Pipeliner) error {
								p.Set(ctx, "unknown-write-outcome", "value", 0)
								p.HGet(ctx, "k", "f")
								return nil
							})
						} else if mode == "cursor" {
							err = (&store{client: client}).searchDo(ctx, "idx", "FT.CURSOR", "READ", "idx", 1, "COUNT", 10).Err()
						} else {
							err = client.HGet(ctx, "k", "f").Err()
						}
						if !errors.Is(err, context.DeadlineExceeded) {
							t.Fatalf("deadline error: %v", err)
						}
						if elapsed := time.Since(start); elapsed > time.Second {
							t.Fatalf("deadline took %s", elapsed)
						}
						wrapped := wrapRedis(err, "blocked transport")
						if !isStatus(wrapped, adbc.StatusTimeout) || sqlState(wrapped) != stateQueryCanceled {
							t.Fatal(wrapped)
						}
						// A discarded socket must not put stale replies into the next use.
						if err := client.Ping(context.Background()).Err(); err != nil {
							t.Fatal("pool reuse:", err)
						}
						readCommand := "HGET"
						if mode == "cursor" {
							readCommand = "FT.CURSOR"
						}
						if server.count(readCommand) != 1 || mode == "pipeline" && server.count("SET") != 1 {
							t.Fatal("command was replayed")
						}
					})
				}
			}
		}
	}
}
