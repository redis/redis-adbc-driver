// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type unmanagedLostReply struct {
	snapshotHook
	calls int
	t     *testing.T
}

func (h *unmanagedLostReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if len(args) < 4 || !strings.EqualFold(cmd.Name(), "eval") || args[3] != "app:1" {
			return next(ctx, cmd)
		}
		h.calls++
		if snapshotOf(ctx) != nil {
			h.t.Fatal("source mutation entered a managed metadata-slot fence")
		}
		if once, ok := cmd.(interface{ NoRetry() bool }); !ok || !once.NoRetry() {
			h.t.Fatal("source mutation permits transport retries")
		}
		if err := next(ctx, cmd); err != nil {
			return err
		}
		cmd.SetErr(io.ErrUnexpectedEOF)
		return cmd.Err()
	}
}

// Model a broken pipeline read after app:1: the rest of the submitted batch
// may have applied, but its replies are unknown. No subsequent batch may run.
func (h *unmanagedLostReply) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		first := -1
		for i, cmd := range cmds {
			args := cmd.Args()
			if len(args) >= 4 && cmd.Name() == "eval" && args[1] == keyRowInsertLua {
				if snapshotOf(ctx) != nil {
					h.t.Fatal("source pipeline entered a managed fence")
				}
				marker, ok := cmd.(interface{ NoRetry() bool })
				if !ok || !marker.NoRetry() {
					h.t.Fatal("source pipeline permits retries")
				}
				if args[3] == "app:1" {
					first = i
				}
			}
		}
		if first < 0 {
			return next(ctx, cmds)
		}
		h.calls++
		if err := next(ctx, cmds); err != nil {
			return err
		}
		for _, cmd := range cmds[first:] {
			cmd.SetErr(io.ErrUnexpectedEOF)
		}
		return io.ErrUnexpectedEOF
	}
}

func TestUnmanagedUnknownWriteDoesNotReplay(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	for _, key := range []string{"app:1", "app:2"} {
		if err := raw.HSet(h.ctx, key, "n", "10").Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	s := snapshotTestStore(t, raw)
	if err := s.awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: "live", Apply: true}); err != nil {
		t.Fatal(err)
	}
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "live")
	if err != nil {
		t.Fatal(err)
	}
	ctx = context.WithValue(ctx, unmanagedWriteKey{}, metaKey("public", "live"))
	hook := &unmanagedLostReply{t: t}
	s.client.AddHook(hook)
	err = s.updateUnmanagedRows(ctx, meta, []rowChange{
		{key: "app:1", set: []any{"n", "20"}},
		{key: "app:2", set: []any{"n", "30"}},
	})
	if err == nil || !strings.Contains(err.Error(), "partially applied") || hook.calls != 1 {
		t.Fatalf("unknown mutation was replayed or lost its diagnostic: calls=%d, err=%v", hook.calls, err)
	}
	if raw.HGet(h.ctx, "app:1", "n").Val() != "20" || raw.HGet(h.ctx, "app:2", "n").Val() != "10" {
		t.Fatal("unknown reply did not stop after the one applied source mutation")
	}
}

func TestUnmanagedForeignFilteredMembership(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	for key, n := range map[string]string{"app:1": "1", "app:2": "2", "other:1": "1"} {
		if err := raw.HSet(h.ctx, key, "n", n, "note", "old").Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "filtered", "ON", "HASH", "PREFIX", 2, "app:", "other:", "FILTER", "@n == 1", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "filtered"); err != nil {
		t.Fatal(err)
	}
	setter := h.conn.(adbc.GetSetOptionsWithContext)
	if err := setter.SetOption(h.ctx, OptionStringForeignIndexesMode, "unmanaged"); err != nil {
		t.Fatal(err)
	}
	if err := setter.SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT __key FROM redis_foreign.filtered ORDER BY __key", "app:1", "other:1")
	h.exec("UPDATE redis_foreign.filtered SET note='changed'")
	if raw.HGet(h.ctx, "app:2", "note").Val() != "old" || raw.HGet(h.ctx, "other:1", "note").Val() != "changed" {
		t.Fatal("source UPDATE escaped filtered multi-prefix membership")
	}
	h.expectError("INSERT INTO redis_foreign.filtered (__key,n) VALUES ('app:3',1)", "filtered foreign index")
	h.exec("DELETE FROM redis_foreign.filtered WHERE __key='app:2'")
	if raw.Exists(h.ctx, "app:2").Val() != 1 {
		t.Fatal("source DELETE escaped filtered membership")
	}
	h.exec("DELETE FROM redis_foreign.filtered")
	if raw.Exists(h.ctx, "app:1", "other:1").Val() != 0 || raw.Exists(h.ctx, "app:2").Val() != 1 {
		t.Fatal("source DELETE lost filtered multi-prefix ownership")
	}
}
