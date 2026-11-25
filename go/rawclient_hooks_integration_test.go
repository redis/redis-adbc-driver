// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// commandCounter counts the commands its client's hooks see, by name.
type commandCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *commandCounter) add(cmds ...goredis.Cmder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cmd := range cmds {
		c.counts[cmd.Name()]++
	}
}

func (*commandCounter) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (c *commandCounter) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		c.add(cmd)
		return next(ctx, cmd)
	}
}

func (c *commandCounter) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		c.add(cmds...)
		return next(ctx, cmds)
	}
}

// A hook given to rawClient sees each command once, on a cluster as on a
// single endpoint: Search commands and WATCH … EXEC, which go to a node's
// client directly, and the commands the cluster client passes on to a node.
// Fault-injection tests rely on it.
func TestRawClientHooksSeeEachCommandOnce(t *testing.T) {
	h := newSQLHarness(t)
	counter := &commandCounter{counts: map[string]int{}}
	raw := h.rawClient(counter)
	st := &store{client: raw}
	const key = "it:rawclient:hooks"
	t.Cleanup(func() { _ = raw.Del(h.ctx, key).Err() })
	if err := raw.Set(h.ctx, key, "a", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Pipelined(h.ctx, func(p goredis.Pipeliner) error {
		p.Get(h.ctx, key)
		p.Exists(h.ctx, key)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.searchDo(h.ctx, key, "FT._LIST").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Watch(h.ctx, func(tx *goredis.Tx) error {
		if err := tx.Get(h.ctx, key).Err(); err != nil {
			return err
		}
		_, err := tx.TxPipelined(h.ctx, func(p goredis.Pipeliner) error {
			p.Set(h.ctx, key, "b", 0)
			return nil
		})
		return err
	}, key); err != nil {
		t.Fatal(err)
	}
	counter.mu.Lock()
	defer counter.mu.Unlock()
	for name, want := range map[string]int{"set": 2, "get": 2, "exists": 1, "ft._list": 1, "watch": 1} {
		if got := counter.counts[name]; got != want {
			t.Errorf("hook saw %s %d times, want %d", name, got, want)
		}
	}
}
