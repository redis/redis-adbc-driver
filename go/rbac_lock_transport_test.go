// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type rbacLockLostReply struct {
	lock, token string
	calls       atomic.Int64
	noRetry     atomic.Bool
}

func (*rbacLockLostReply) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*rbacLockLostReply) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *rbacLockLostReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() != "set" || len(args) < 4 || fmt.Sprint(args[1]) != h.lock {
			return next(ctx, cmd)
		}
		h.calls.Add(1)
		h.token = fmt.Sprint(args[2])
		marker, ok := cmd.(interface{ NoRetry() bool })
		h.noRetry.Store(ok && marker.NoRetry())
		if err := next(ctx, cmd); err != nil {
			return err
		}
		cmd.SetErr(io.ErrUnexpectedEOF)
		return io.ErrUnexpectedEOF
	}
}

func checkRBACLockLostReply(t *testing.T, admin *sqlHarness, role string, writes func() int64) {
	t.Helper()
	st, err := admin.conn.NewStatement(admin.ctx)
	if err != nil {
		t.Fatal(err)
	}
	base := st.(*statementImpl).conn
	_ = st.Close(admin.ctx)
	opts := *base.store.client.(*goredis.Client).Options()
	client := goredis.NewClient(&opts)
	defer client.Close()
	client.AddHook(snapshotHook{})
	s := &store{client: client, snapshots: true, flex: base.store.flex, readerNamespace: base.store.readerNamespace}
	hook := &rbacLockLostReply{lock: s.rbacControlKey("lock")}
	if base.store.client.Exists(admin.ctx, hook.lock).Val() != 0 {
		t.Fatal("fixture already has an administrative lock")
	}
	client.AddHook(hook)
	ctx, pin, err := s.acquireSnapshot(admin.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	defer func() {
		_ = once(context.Background(), base.store.client, "EVAL", `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, 1, hook.lock, hook.token).Err()
	}()
	parsed, err := ParseScript("GRANT SELECT ON it_rbac_renamed TO " + role)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, rbac: base.rbac, sess: newSession("public"), schema: "public"}
	before := writes()
	err = e.runPrivilege(ctx, parsed[0].Stmt.(*PrivilegeStmt))
	if err == nil {
		t.Fatal("unknown lock outcome did not stop management", err)
	}
	if !hook.noRetry.Load() || hook.calls.Load() != 1 {
		t.Fatal("global SET NX lacked the transport no-retry marker or was resent", hook.noRetry.Load(), hook.calls.Load())
	}
	if !strings.Contains(err.Error(), "lock acquisition outcome is unknown") {
		t.Fatal("unknown lock outcome was not reported explicitly", err)
	}
	if writes() != before {
		t.Fatal("management API was mutated before lock ownership was confirmed")
	}
	if token, err := base.store.client.Get(admin.ctx, hook.lock).Result(); err != nil || token != hook.token {
		t.Fatal("lost successful lock reply did not retain the original owner", err)
	}
}
