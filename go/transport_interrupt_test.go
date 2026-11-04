// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
	"strings"
	"testing"
	"time"
)

func TestExecutionTransportInterruptsBlockedRead(t *testing.T) {
	for _, clustered := range []bool{false, true} {
		for _, timeout := range []time.Duration{0, time.Minute} {
			for _, mode := range []string{"command", "pipeline", "cursor"} {
				for _, ownerCancel := range []bool{false, true} {
					t.Run(fmt.Sprintf("cluster=%t/timeout=%s/%s/owner=%t", clustered, timeout, mode, ownerCancel), func(t *testing.T) {
						server := newFakeRedis(t, "HGET", "SET", "FT.CURSOR")
						cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
						cfg.cluster.ClusterSlots = func(context.Context) ([]goredis.ClusterSlot, error) {
							return []goredis.ClusterSlot{{Start: 0, End: 16383, Nodes: []goredis.ClusterNode{{Addr: server.addr}}}}, nil
						}
						ordinary := newClient(cfg, clustered, timeouts{read: timeout, write: timeout})
						defer ordinary.Close()
						c := &connectionImpl{store: &store{client: ordinary}, clientCfg: cfg, timeouts: timeouts{read: timeout, write: timeout}}
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						owner := &streams{}
						active, transport := c.executionContext(ctx, owner)
						defer transport.finish()
						done := make(chan error, 1)
						go func() {
							client := c.store.redis(active)
							var err error
							switch mode {
							case "pipeline":
								_, err = client.Pipelined(active, func(p goredis.Pipeliner) error {
									p.Set(active, "unknown-outcome", "value", 0)
									p.HGet(active, "k", "f")
									return nil
								})
							case "cursor":
								err = c.store.searchDo(active, "idx", "FT.CURSOR", "READ", "idx", 1, "COUNT", 10).Err()
							default:
								err = client.HGet(active, "k", "f").Err()
							}
							done <- err
						}()
						command := "HGET"
						if mode == "cursor" {
							command = "FT.CURSOR"
						}
						deadline := time.Now().Add(2 * time.Second)
						for server.count(command) == 0 && time.Now().Before(deadline) {
							time.Sleep(time.Millisecond)
						}
						if server.count(command) == 0 {
							t.Fatal("command never arrived")
						}
						start := time.Now()
						if ownerCancel {
							owner.closeAll(stateErrorf(stateQueryCanceled, "cancelled"))
						} else {
							cancel()
						}
						select {
						case err := <-done:
							wrapped := wrapRedis(err, "interrupted read")
							if !isStatus(wrapped, adbc.StatusCancelled) || sqlState(wrapped) != stateQueryCanceled {
								t.Fatal(wrapped)
							}
						case <-time.After(time.Second):
							t.Fatal("cancel did not interrupt blocked socket")
						}
						if time.Since(start) > time.Second {
							t.Fatal("cancellation exceeded one second")
						}
						transport.finish()
						if err := ordinary.Ping(context.Background()).Err(); err != nil {
							t.Fatal("unrelated pool failed", err)
						}
						if server.count(command) != 1 || mode == "pipeline" && server.count("SET") != 1 {
							t.Fatal("interrupted command replayed")
						}
						owner.mu.Lock()
						remaining := len(owner.open)
						owner.mu.Unlock()
						if remaining != 0 {
							t.Fatal("execution owner leaked")
						}
					})
				}
			}
		}
	}
}

// Internal probe deadlines may cancel a child context without ending the SQL
// execution. A late canceled dial must be rejected, but future active commands
// must still be able to acquire new execution-owned sockets.
func TestExecutionTransportChildCancellation(t *testing.T) {
	server := newFakeRedis(t, "HGET")
	cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
	ordinary := newClient(cfg, false, timeouts{})
	defer ordinary.Close()
	c := &connectionImpl{store: &store{client: ordinary}, clientCfg: cfg}
	active, transport := c.executionContext(context.Background())
	defer transport.finish()
	child, cancel := context.WithCancel(active)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.store.redis(child).HGet(child, "k", "f").Err() }()
	deadline := time.Now().Add(time.Second)
	for server.count("HGET") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if server.count("HGET") == 0 {
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !isStatus(wrapRedis(err, "probe"), adbc.StatusCancelled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not stop")
	}
	if err := c.store.redis(active).Ping(active).Err(); err != nil {
		t.Fatal("active execution could not redial", err)
	}
	if c.store.redis(context.WithoutCancel(child)) != ordinary {
		t.Fatal("cleanup did not select maintenance pool")
	}
}

// Socket teardown is not execution completion: snapshot aborts and pin releases
// still need the ordinary maintenance client while SQL unwinds.
func TestConnectionCloseWaitsForExecutionCleanup(t *testing.T) {
	server := newFakeRedis(t, "HGET")
	cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
	ordinary := newClient(cfg, false, timeouts{})
	c := &connectionImpl{store: &store{client: ordinary}, clientCfg: cfg}
	active, transport := c.executionContext(context.Background(), &c.streams)
	defer transport.finish()
	closed := make(chan error, 1)
	go func() { closed <- c.Close(context.Background()) }()
	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not interrupt execution")
	}
	if err := ordinary.Ping(context.Background()).Err(); err != nil {
		t.Fatal("Close shut maintenance pool before execution cleanup", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before execution cleanup: %v", err)
	default:
	}
	transport.finish()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join finished execution")
	}
	if err := ordinary.Ping(context.Background()).Err(); err == nil {
		t.Fatal("Close left maintenance client open")
	}
	if len(c.store.activeExecutions()) != 0 {
		t.Fatal("execution registration leaked")
	}
}

func TestConnectionCloseBoundsUncooperativeExecution(t *testing.T) {
	server := newFakeRedis(t, "HGET")
	cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
	ordinary := newClient(cfg, false, timeouts{})
	c := &connectionImpl{store: &store{client: ordinary}, clientCfg: cfg}
	_, transport := c.executionContext(context.Background(), &c.streams)
	defer transport.finish()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Close(ctx)
	if !isStatus(err, adbc.StatusTimeout) || time.Since(start) > time.Second {
		t.Fatalf("uncooperative execution left Close unbounded: %v", err)
	}
	if err := ordinary.Ping(context.Background()).Err(); err == nil {
		t.Fatal("timed-out Close left maintenance client open")
	}
}

func TestExecutionClosePreservesOutcomeDiagnostics(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errConnClosed)
	unknown := errorf(adbc.StatusIO, "snapshot publication outcome is unknown (operation test); do not blindly retry")
	if err := executionCloseError(ctx, unknown); err.Error() != unknown.Error() {
		t.Fatal("Close hid indeterminate publication outcome", err)
	}
	interrupted := stateErrorf(stateQueryCanceled, "sequence allocation outcome unknown; input was not replayed")
	err := executionCloseError(ctx, interrupted)
	if !isStatus(err, adbc.StatusInvalidState) || !strings.Contains(err.Error(), "outcome unknown; input was not replayed") {
		t.Fatal("Close lost cancellation cause or outcome diagnostics", err)
	}
	if err := executionCloseError(ctx, nil); err != nil {
		t.Fatal("Close converted successful publication into an error", err)
	}
}
