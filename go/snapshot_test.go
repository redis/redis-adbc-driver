// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
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
	if err := scopeSnapshotCommand(context.Background(), goredis.NewCmd(ctx, "GET", metaKey("public", "t"))); err == nil {
		t.Fatal("unscoped metadata access accepted")
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

func TestSnapshotConnectionCapabilityAndPartitionGuard(t *testing.T) {
	ctx := context.Background()
	c := &connectionImpl{store: &store{snapshots: true}}
	if err := c.SetOption(ctx, OptionStringStatementSnapshots, "true"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOption(ctx, OptionStringStatementSnapshots, "false"); err == nil {
		t.Fatal("connection disabled snapshots")
	}
	stmt := &statementImpl{conn: c}
	if _, _, _, err := stmt.ExecutePartitions(ctx); err == nil {
		t.Fatal("partition production bypassed snapshot ownership")
	}
}
