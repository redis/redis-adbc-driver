// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotCommandScoping(t *testing.T) {
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: "generation", write: true})
	literal := metaPrefix + "this is a user value"
	for _, args := range [][]any{
		{"HSET", metaKey("public", "t"), "field", literal},
		{"SADD", tablesKey("public"), literal},
		{"ZADD", metaPrefix + "import:keys:test", 0, literal},
		{"ZREM", metaPrefix + "import:keys:test", literal},
		{"SET", metaKey("public", "t"), literal},
	} {
		cmd := goredis.NewCmd(ctx, args...)
		if err := scopeSnapshotCommand(ctx, cmd); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(cmd.Args()[1].(string), snapshotWorkspace("generation")) {
			t.Fatalf("unscoped key: %v", cmd.Args())
		}
		if cmd.Args()[len(args)-1] != literal {
			t.Fatalf("user value rewritten: %v", cmd.Args())
		}
		before := cmd.Args()[1]
		if err := scopeSnapshotCommand(ctx, cmd); err != nil {
			t.Fatal(err)
		}
		if cmd.Args()[1] != before {
			t.Fatal("retry double-scoped key")
		}
	}
	readCtx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: "generation"})
	if err := scopeSnapshotCommand(readCtx, goredis.NewCmd(readCtx, "HSET", snapshotRows("generation")+"1", "a", "b")); err == nil {
		t.Fatal("published generation was writable")
	}
	for _, args := range [][]any{{"GET", metaKey("public", "t")}, {"GETRANGE", viewKey("public", "v"), int64(0), int64(4096)}} {
		if err := scopeSnapshotCommand(context.Background(), goredis.NewCmd(ctx, args...)); err == nil {
			t.Fatal("unscoped metadata read accepted", args)
		}
	}
	cmd := goredis.NewCmd(readCtx, "GETRANGE", viewKey("public", "v"), int64(0), int64(4096))
	if err := scopeSnapshotCommand(readCtx, cmd); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cmd.Args()[1].(string), snapshotWorkspace("generation")) || cmd.Args()[2] != int64(0) || cmd.Args()[3] != int64(4096) {
		t.Fatal("bounded view read lost scope or bounds", cmd.Args())
	}
}

func TestSnapshotPartialResultWarnings(t *testing.T) {
	for _, reply := range []any{
		map[any]any{"warning": []any{"Timeout limit was reached"}},
		[]any{map[any]any{"warning": []any{"Insufficient memory"}}, int64(42)},
	} {
		if !snapshotReplyWarning(reply) {
			t.Fatalf("partial reply accepted: %v", reply)
		}
	}
	for _, reply := range []any{
		map[any]any{"warning": []any{}},
		[]any{map[any]any{"warning": []any{}}, int64(42)},
		[]any{[]any{int64(0)}, int64(0)},
	} {
		if snapshotReplyWarning(reply) {
			t.Fatalf("complete reply rejected: %v", reply)
		}
	}
}

func TestSnapshotContextDeadlines(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		d := &databaseImpl{snapshots: enabled, address: "localhost:6379"}
		cfg, err := d.clientConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.single.ContextTimeoutEnabled != enabled || cfg.cluster.ContextTimeoutEnabled != enabled {
			t.Fatalf("context deadline enforcement=%v; want snapshot mode %v", cfg.single.ContextTimeoutEnabled, enabled)
		}
	}
}

func TestSnapshotAlwaysEnabled(t *testing.T) {
	ctx := context.Background()
	db, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	if !db.snapshots {
		t.Fatal("managed-table snapshots were not enabled automatically")
	}
	if got, err := db.GetOption(ctx, OptionStringStatementSnapshots); err != nil || got != "true" {
		t.Fatalf("snapshot capability: %q %v", got, err)
	}
	if err := db.SetOption(ctx, OptionStringStatementSnapshots, "false"); err == nil {
		t.Fatal("managed-table snapshots could be disabled")
	}
	if !db.snapshots {
		t.Fatal("rejected disable changed snapshot state")
	}
}

func TestSnapshotConnectionCapability(t *testing.T) {
	ctx := context.Background()
	c := &connectionImpl{store: &store{snapshots: true}}
	if err := c.SetOption(ctx, OptionStringStatementSnapshots, "true"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOption(ctx, OptionStringStatementSnapshots, "false"); err == nil {
		t.Fatal("connection disabled snapshots")
	}
}

func TestSnapshotDeltaDirectoryUsesExactIntegerOrdering(t *testing.T) {
	id := strings.Repeat("a", 32)
	var members []string
	for _, n := range []int64{9223372036854775807, 9007199254740993, 2, 1, 9007199254740992} {
		member := snapshotDirectoryMember(snapshotRows(id) + strconv.FormatInt(n, 10))
		if len(member) != 20 {
			t.Fatalf("invalid exact directory member: %q", member)
		}
		members = append(members, member)
	}
	sort.Strings(members)
	for i, want := range []int64{1, 2, 9007199254740992, 9007199254740993, 9223372036854775807} {
		got, err := strconv.ParseInt(members[i], 10, 64)
		if err != nil || got != want {
			t.Fatalf("directory reordered exact IDs: %v %v", members, err)
		}
	}
	for _, suffix := range []string{"0", "-1", "1:extra", "01", "9223372036854775808"} {
		if got := snapshotDirectoryMember(snapshotRows(id) + suffix); got != "" {
			t.Fatalf("invalid row identity accepted: %q", suffix)
		}
	}
}
