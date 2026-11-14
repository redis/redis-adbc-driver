// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// RESP3 protocol fixture models connection-local WATCH/MULTI state. It does not
// run Redis. Native cleanup refusals deliberately leave state on the socket,
// exactly the case that closing the old per-execution client previously hid.
func executionPoolStateFixture(t *testing.T, denyUnwatch, denyExec bool) (*connectionImpl, *fakeRedis) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	server := &fakeRedis{addr: ln.Addr().String(), seen: map[string]int{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				rd := bufio.NewReader(conn)
				watch, multi := false, false
				queued := 0
				for {
					args, err := readRESPArray(rd)
					if err != nil {
						return
					}
					name := strings.ToUpper(args[0])
					server.mu.Lock()
					server.seen[name]++
					server.mu.Unlock()
					reply := "+OK\r\n"
					switch name {
					case "HELLO":
						reply = "%1\r\n+proto\r\n:3\r\n"
					case "CLIENT":
						reply = "-ERR unsupported optional client command\r\n"
					case "WATCH":
						watch = true
					case "MULTI":
						multi = true
						queued = 0
					case "UNWATCH":
						if multi {
							queued++
							reply = "+QUEUED\r\n"
						} else if denyUnwatch {
							reply = "-NOPERM UNWATCH refused\r\n"
						} else {
							watch = false
						}
					case "EXEC":
						if denyExec {
							reply = "-NOPERM EXEC refused\r\n"
						} else {
							multi, watch = false, false
							reply = fmt.Sprintf("*%d\r\n", queued) + strings.Repeat("+OK\r\n", queued)
							queued = 0
						}
					case "PING":
						if multi {
							queued++
							reply = "+QUEUED\r\n"
						} else {
							reply = "+PONG\r\n"
						}
					case "SET":
						if multi {
							queued++
							reply = "+QUEUED\r\n"
						}
					case "WATCHSTATE":
						if watch {
							reply = ":1\r\n"
						} else {
							reply = ":0\r\n"
						}
					default:
						reply = "-ERR unsupported fixture command\r\n"
					}
					if _, err := conn.Write([]byte(reply)); err != nil {
						return
					}
				}
			}()
		}
	}()
	cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true})
	c := &connectionImpl{store: &store{client: newClient(cfg, false, timeouts{})}, clientCfg: cfg}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, server
}

func TestExecutionPoolDiscardsRefusedUnwatchAfterEarlyCallback(t *testing.T) {
	c, server := executionPoolStateFixture(t, true, false)
	ctx, transport := c.executionContext(context.Background())
	defer transport.finish()
	expected := errors.New("validation stopped before EXEC")
	err := transport.client.(*goredis.Client).Watch(ctx, func(*goredis.Tx) error { return expected }, "source")
	if err != expected {
		t.Fatal("callback error changed", err)
	}
	if server.count("WATCH") != 1 || server.count("UNWATCH") != 1 || server.count("EXEC") != 0 {
		t.Fatal("did not exercise early WATCH cleanup refusal")
	}
	// Confirm the modeled peer kept WATCH armed despite Client.Watch returning
	// only the callback error and Tx.Close ignoring the cleanup refusal.
	if got, err := transport.client.Do(ctx, "WATCHSTATE").Int(); err != nil || got != 1 {
		t.Fatal("fixture did not retain WATCH", got, err)
	}
	transport.finish()
	next, active := c.executionContext(context.Background())
	defer active.finish()
	if active.pool == transport.pool {
		t.Fatal("refused UNWATCH pool reused with armed WATCH")
	}
	if err := c.store.redis(next).Ping(next).Err(); err != nil {
		t.Fatal(err)
	}
	if server.count("HELLO") != 2 {
		t.Fatal("next execution did not negotiate a clean socket")
	}
}

func TestExecutionPoolDiscardsRefusedExecWithQueuedCleanup(t *testing.T) {
	c, server := executionPoolStateFixture(t, false, true)
	ctx, transport := c.executionContext(context.Background())
	defer transport.finish()
	err := transport.client.(*goredis.Client).Watch(ctx, func(tx *goredis.Tx) error {
		_, err := tx.TxPipelined(ctx, func(p goredis.Pipeliner) error { p.Set(ctx, "destination", "value", 0); return nil })
		return err
	}, "source")
	if err == nil || !strings.Contains(err.Error(), "EXEC refused") {
		t.Fatal("missing native EXEC refusal", err)
	}
	if server.count("MULTI") != 1 || server.count("EXEC") != 1 || server.count("UNWATCH") != 1 || server.count("SET") != 1 {
		t.Fatal("transaction or queued cleanup replayed/missing")
	}
	transport.finish()
	next, active := c.executionContext(context.Background())
	defer active.finish()
	if active.pool == transport.pool {
		t.Fatal("failed transaction pool reused with MULTI armed")
	}
	if err := c.store.redis(next).Ping(next).Err(); err != nil {
		t.Fatal("next execution inherited MULTI", err)
	}
	if server.count("HELLO") != 2 {
		t.Fatal("next execution did not negotiate a clean socket")
	}
}

func TestExecutionPoolCleanupRequiresExpectedStatus(t *testing.T) {
	for _, name := range []string{"unwatch", "discard", "reset"} {
		for _, status := range []string{"QUEUED", "unexpected", ""} {
			cmd := goredis.NewStatusCmd(context.Background(), name)
			cmd.SetVal(status)
			if !executionPoolCleanupFailed(cmd, nil) {
				t.Fatalf("%s accepted cleanup status %q", name, status)
			}
		}
		ok := goredis.NewStatusCmd(context.Background(), name)
		expected := "OK"
		if name == "reset" {
			expected = "RESET"
		}
		ok.SetVal(expected)
		if executionPoolCleanupFailed(ok, nil) {
			t.Fatal("expected cleanup refused", name)
		}
		if !executionPoolCleanupFailed(ok, redisErr("NOPERM cleanup refused")) {
			t.Fatal("native cleanup refusal accepted", name)
		}
	}
}
