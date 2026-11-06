// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type restrictedAcquireFault struct {
	snapshotHook
	calls  atomic.Int64
	before func(goredis.Cmder)
	after  func(goredis.Cmder, error)
	lost   bool
}

func (h *restrictedAcquireFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if len(args) < 4 || args[0] != "EVAL" || args[1] != snapshotReaderControl {
			return next(ctx, cmd)
		}
		n, _ := strconv.Atoi(fmt.Sprint(args[2]))
		if len(args) <= 3+n || args[3+n] != "acquire" {
			return next(ctx, cmd)
		}
		first := h.calls.Add(1) == 1
		if first && h.before != nil {
			h.before(cmd)
		}
		err := next(ctx, cmd)
		if first && h.after != nil {
			h.after(cmd, err)
		}
		if first && h.lost && err == nil {
			return io.ErrUnexpectedEOF
		}
		return err
	}
}

func checkSnapshotRestrictedTransport(t *testing.T, admin, reader *sqlHarness) {
	t.Helper()
	ac := *admin
	ac.t = t
	admin = &ac
	st, err := reader.conn.NewStatement(reader.ctx)
	if err != nil {
		t.Fatal(err)
	}
	base := st.(*statementImpl).conn.store
	_ = st.Close(reader.ctx)
	adminStmt, err := admin.conn.NewStatement(admin.ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := adminStmt.(*statementImpl).conn.store
	_ = adminStmt.Close(admin.ctx)
	for _, mode := range []string{"stale_directory", "lost_reply"} {
		t.Run(mode, func(t *testing.T) {
			opts := *base.client.(*goredis.Client).Options()
			client := goredis.NewClient(&opts)
			defer client.Close()
			hook := &restrictedAcquireFault{}
			client.AddHook(hook)
			s := &store{client: client, snapshots: true, restrictedReader: true, readerNamespace: base.readerNamespace}
			root := a.client.HGet(admin.ctx, snapshotCatalogKey, "root").Val()
			if mode == "stale_directory" {
				ns := snapshotReaderNamespace("injected-directory-churn")
				hook.before = func(goredis.Cmder) {
					if err := a.client.SAdd(admin.ctx, snapshotReaderDirectory, ns).Err(); err != nil {
						t.Fatal(err)
					}
				}
				hook.after = func(cmd goredis.Cmder, cause error) {
					if cause == nil {
						t.Fatal("stale directory did not cause a pre-mutation refusal")
					}
					args := cmd.Args()
					n, _ := strconv.Atoi(fmt.Sprint(args[2]))
					token := fmt.Sprint(args[3+n+2])
					if a.client.HExists(admin.ctx, snapshotReaderPrefix+s.readerNamespace, token).Val() {
						t.Fatal("stale directory partially admitted a pin")
					}
				}
				defer a.client.SRem(admin.ctx, snapshotReaderDirectory, ns)
			} else {
				hook.lost = true
				hook.after = func(goredis.Cmder, error) { admin.exec("CREATE TABLE it_rbac_lost_probe(v INTEGER)") }
				defer admin.dropTables("it_rbac_lost_probe")
			}
			ctx, pin, err := s.acquireSnapshot(reader.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			if pin.root != root {
				t.Fatal("acquisition silently moved to another root")
			}
			want := int64(1)
			if mode == "stale_directory" {
				want = 2
			}
			if hook.calls.Load() != want {
				t.Fatal("unexpected acquire replay count", hook.calls.Load(), want)
			}
			if mode == "lost_reply" && a.client.HGet(admin.ctx, snapshotCatalogKey, "root").Val() == root {
				t.Fatal("lost-reply test did not advance the published root")
			}
			if err := pin.valid(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
