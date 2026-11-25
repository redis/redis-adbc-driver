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
// A refused EXEC leaves MULTI armed, as a Redis ACL refusal of EXEC does.
// WATCHSTATE reports the socket's WATCH even inside MULTI; PING is queued there.
func executionPoolStateFixture(t *testing.T, denyUnwatch, denyExec bool) (*connectionImpl, *fakeRedis) {
	return transactionStateFixture(t, false, denyUnwatch, denyExec)
}

// transactionStateFixture is executionPoolStateFixture for a single-endpoint or
// a one-node cluster client. Each node's pool holds one socket, so a later
// borrower reuses the socket unless go-redis retired it.
func transactionStateFixture(t *testing.T, cluster, denyUnwatch, denyExec bool) (*connectionImpl, *fakeRedis) {
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
	cfg := newClientConfig(&goredis.Options{Addr: server.addr, Protocol: 3, DisableIdentity: true, PoolSize: 1})
	cfg.cluster.ClusterSlots = func(context.Context) ([]goredis.ClusterSlot, error) {
		return []goredis.ClusterSlot{{Start: 0, End: 16383, Nodes: []goredis.ClusterNode{{Addr: server.addr}}}}, nil
	}
	c := &connectionImpl{store: &store{client: newClient(cfg, cluster, timeouts{})}, clientCfg: cfg}
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
	// The modeled peer kept WATCH armed. Since go-redis v9.24.0
	// (redis/go-redis#4316) Tx.Close retires that socket, so even a later
	// borrower of the same pool gets a clean one.
	if got, err := transport.client.Do(ctx, "WATCHSTATE").Int(); err != nil || got != 0 {
		t.Fatal("later borrower inherited WATCH", got, err)
	}
	if server.count("HELLO") != 2 {
		t.Fatal("refused UNWATCH socket was reused")
	}
	transport.finish()
	next, active := c.executionContext(context.Background())
	defer active.finish()
	// The driver still never lends a pool after a refused cleanup.
	if active.pool == transport.pool {
		t.Fatal("refused UNWATCH pool reused")
	}
	if err := c.store.redis(next).Ping(next).Err(); err != nil {
		t.Fatal(err)
	}
	if server.count("HELLO") != 3 {
		t.Fatal("next execution did not negotiate a clean socket")
	}
}

func TestExecutionPoolDiscardsRefusedExec(t *testing.T) {
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
	// go-redis v9.24.0 (redis/go-redis#4316) retires the socket on the refused
	// EXEC, so it no longer queues an UNWATCH inside the still-open MULTI.
	if server.count("MULTI") != 1 || server.count("EXEC") != 1 || server.count("UNWATCH") != 0 || server.count("SET") != 1 {
		t.Fatal("transaction replayed or cleanup sent on the refused socket")
	}
	transport.finish()
	next, active := c.executionContext(context.Background())
	defer active.finish()
	if active.pool == transport.pool {
		t.Fatal("failed transaction pool reused with MULTI armed")
	}
	// Inside MULTI the PING would be queued: no error, but QUEUED.
	if got, err := c.store.redis(next).Ping(next).Result(); err != nil || got != "PONG" {
		t.Fatal("next execution inherited MULTI", got, err)
	}
	if server.count("HELLO") != 2 {
		t.Fatal("next execution did not negotiate a clean socket")
	}
}

// Outside an execution, Watch and TxPipelined run on the connection's shared
// pool, which has no driver-level discard (the WATCH … EXEC paths of store.go,
// tags.go and adopt.go). With go-redis v9.23.0 a later borrower inherited the
// transaction state in both cases of redis/go-redis#4315: v9.24.0 retires the
// socket (redis/go-redis#4316). One socket per pool makes that borrower reuse
// the refused socket unless it was retired.
func TestSharedPoolRetiresUncleanTransactionSocket(t *testing.T) {
	ctx := context.Background()
	for _, cluster := range []bool{false, true} {
		clean := func(t *testing.T, c *connectionImpl, server *fakeRedis) {
			t.Helper()
			if got, err := c.store.client.Ping(ctx).Result(); err != nil || got != "PONG" {
				t.Fatal("later borrower inherited MULTI", got, err)
			}
			if got, err := c.store.client.Do(ctx, "WATCHSTATE").Int(); err != nil || got != 0 {
				t.Fatal("later borrower inherited WATCH", got, err)
			}
			if n := server.count("HELLO"); n != 2 {
				t.Fatalf("%d handshakes: the refused socket was reused", n)
			}
		}
		t.Run(fmt.Sprintf("cluster=%v/callback error then refused UNWATCH", cluster), func(t *testing.T) {
			c, server := transactionStateFixture(t, cluster, true, false)
			expected := errors.New("validation stopped before EXEC")
			if err := c.store.client.Watch(ctx, func(*goredis.Tx) error { return expected }, "source"); err != expected {
				t.Fatal("callback error changed", err)
			}
			if server.count("WATCH") != 1 || server.count("UNWATCH") != 1 || server.count("EXEC") != 0 {
				t.Fatal("did not exercise early WATCH cleanup refusal")
			}
			clean(t, c, server)
		})
		t.Run(fmt.Sprintf("cluster=%v/refused EXEC in MULTI", cluster), func(t *testing.T) {
			c, server := transactionStateFixture(t, cluster, false, true)
			err := c.store.client.Watch(ctx, func(tx *goredis.Tx) error {
				_, err := tx.TxPipelined(ctx, func(p goredis.Pipeliner) error { p.Set(ctx, "destination", "value", 0); return nil })
				return err
			}, "source")
			if err == nil || !strings.Contains(err.Error(), "EXEC refused") {
				t.Fatal("missing native EXEC refusal", err)
			}
			if server.count("MULTI") != 1 || server.count("EXEC") != 1 || server.count("SET") != 1 {
				t.Fatal("transaction replayed or missing")
			}
			clean(t, c, server)
		})
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
