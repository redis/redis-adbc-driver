// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type heartbeatLostReplyClient struct {
	goredis.UniversalClient
	mu      sync.Mutex
	before  bool
	renews  int
	ops     []string
	renewed chan struct{}
}

func (c *heartbeatLostReplyClient) Process(ctx context.Context, cmd goredis.Cmder) error {
	a := snapshotObservedControlArgs(cmd)
	if len(a) < 4 || a[0] != "EVAL" || (a[1] != snapshotControl && a[1] != snapshotReaderControl) {
		return c.UniversalClient.Process(ctx, cmd)
	}
	var keys int
	fmt.Sscan(fmt.Sprint(a[2]), &keys)
	op := fmt.Sprint(a[3+keys])
	c.mu.Lock()
	c.ops = append(c.ops, op)
	if op == "renew" {
		c.renews++
	}
	lose := op == "renew" && c.renews == 1
	c.mu.Unlock()
	var err error
	if !lose || !c.before {
		err = c.UniversalClient.Process(ctx, cmd)
	}
	if lose && err == nil {
		err = io.ErrUnexpectedEOF
		cmd.SetErr(err)
	}
	if op == "renew" {
		c.renewed <- struct{}{}
	}
	return err
}

func TestSnapshotHeartbeatUnmanagedCountAfterLostRenewal(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("loss_before_execution_%t", before), func(t *testing.T) {
			h, raw, _ := unmanagedIngestFixture(t, false)
			s := snapshotTestStore(t, raw)
			client := &heartbeatLostReplyClient{UniversalClient: s.client, before: before, renewed: make(chan struct{}, 4)}
			s.client = client
			p := &snapshotPin{ctx: h.ctx, store: s, token: snapshotID(), stop: make(chan struct{}), done: make(chan struct{})}
			var err error
			p.root, err = s.snapshotControl(h.ctx, "acquire", p.token, "", "").Text()
			if err != nil {
				t.Fatal(err)
			}
			ticks := make(chan time.Time)
			go p.heartbeatTicks(ticks)
			defer p.release()
			ctx := context.WithValue(h.ctx, snapshotContextKey{}, &snapshotScope{id: p.root, pin: p, store: s})
			waitRenewal := func() {
				t.Helper()
				select {
				case <-client.renewed:
				case <-time.After(5 * time.Second):
					t.Fatal("renewal did not finish")
				}
			}
			heartbeatTestTick(t, p, ticks)
			waitRenewal()
			// Fully consume the SQL result with this same metadata pin while
			// its renewal outcome is uncertain; source rows remain unmanaged.
			got := snapshotResultStrings(snapshotExecuteTest(t, s, ctx, "SELECT COUNT(*) FROM live").rows)
			if len(got) != 1 || got[0] != "1" {
				t.Fatal("wrong unmanaged count", got)
			}
			heartbeatTestTick(t, p, ticks)
			waitRenewal()
			client.mu.Lock()
			n := len(client.ops)
			proof := n >= 2 && client.ops[n-2] == "valid" && client.ops[n-1] == "renew" && client.renews == 2
			client.mu.Unlock()
			if !proof {
				t.Fatal("scheduled renewal lacked a fresh live-lease proof")
			}
			if err := p.valid(ctx); err != nil {
				t.Fatal(err)
			}
			p.release()
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+p.token).Val() {
				t.Fatal("lease survived release")
			}
			if raw.HGet(h.ctx, "app:seed", "n").Val() != "10" {
				t.Fatal("count changed its source HASH")
			}
		})
	}
}
