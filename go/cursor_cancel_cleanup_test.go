// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// A canceled READ must also return when its best-effort DEL gets no reply.
// The command-only interruption matrix doesn't exercise this cleanup boundary.
func TestCursorCancelBoundsCleanup(t *testing.T) {
	for _, clustered := range []bool{false, true} {
		for _, timeout := range []time.Duration{0, time.Minute} {
			for _, mode := range []string{"context", "owner", "deadline"} {
				t.Run(fmt.Sprintf("cluster=%t/read=%s/cancel=%s", clustered, timeout, mode), func(t *testing.T) {
					server := newFakeRedis(t, "FT.CURSOR")
					cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
					cfg.cluster.ClusterSlots = func(context.Context) ([]goredis.ClusterSlot, error) {
						return []goredis.ClusterSlot{{Start: 0, End: 16383, Nodes: []goredis.ClusterNode{{Addr: server.addr}}}}, nil
					}
					ordinary := newClient(cfg, clustered, timeouts{read: timeout, write: timeout})
					defer ordinary.Close()
					c := &connectionImpl{store: &store{client: ordinary}, clientCfg: cfg, timeouts: timeouts{read: timeout, write: timeout}}
					ctx, cancel := context.WithCancel(context.Background())
					if mode == "deadline" {
						cancel()
						ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
					}
					defer cancel()
					owner := &streams{}
					active, transport := c.executionContext(ctx, owner)
					defer transport.finish()
					node, err := c.store.search(active, "idx")
					if err != nil {
						t.Fatal(err)
					}
					cursor := &aggCursor{store: c.store, node: node, index: "idx", cursor: 42, count: 10}
					mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
					defer mem.AssertSize(t, 0)
					cols := []resultColumn{{Name: "id", Type: typeInt64}}
					record, err := buildRecord(mem, cols, [][]Value{{intValue(typeInt64, 1)}})
					if err != nil {
						t.Fatal(err)
					}
					readerCtx, stop := context.WithCancelCause(active)
					reader := &streamReader{ctx: readerCtx, cancel: stop, it: &scanIter{cur: cursor},
						cur: record, cols: cols, batchRows: 1, left: -1, mem: mem, transport: transport}
					reader.refs.Store(1)
					reader.attachOwners([]*streams{owner})
					defer reader.Release()
					done := make(chan bool, 1)
					go func() { done <- reader.Next() }()
					deadline := time.Now().Add(2 * time.Second)
					for server.count("FT.CURSOR") == 0 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					// Owner cancellation waits for reader cleanup, so invoke it
					// asynchronously as a real StatementCancel caller would.
					cancelDone := make(chan struct{})
					go func() {
						defer close(cancelDone)
						if mode == "owner" {
							owner.closeAll(stateErrorf(stateQueryCanceled, "cancelled"))
						} else if mode != "deadline" {
							cancel()
						}
					}()
					select {
					case next := <-done:
						if next {
							t.Fatal("canceled cursor returned a batch")
						}
					case <-time.After(2 * time.Second):
						_ = ordinary.Close() // unblock the regression before failing
						transport.finish()
						<-done
						<-cancelDone
						t.Fatal("cursor cleanup blocked cancellation")
					}
					<-cancelDone
					status := adbc.StatusCancelled
					if mode == "deadline" {
						status = adbc.StatusTimeout
					}
					if !isStatus(reader.Err(), status) || sqlState(reader.Err()) != stateQueryCanceled {
						t.Fatal(reader.Err())
					}
					if server.count("FT.CURSOR") != 2 || cursor.cursor != 0 {
						t.Fatal("expected one READ and one best-effort DEL, with local cursor released")
					}
					if err := ordinary.Ping(context.Background()).Err(); err != nil {
						t.Fatal("maintenance pool reuse:", err)
					}
					owner.mu.Lock()
					remaining := len(owner.open)
					owner.mu.Unlock()
					if remaining != 0 {
						t.Fatal("execution/reader ownership leaked")
					}
				})
			}
		}
	}
}
