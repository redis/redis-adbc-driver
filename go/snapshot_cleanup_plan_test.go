// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type cleanupSearchCounter struct {
	snapshotHook
	aggregates atomic.Int64
}

func (h *cleanupSearchCounter) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "ft.aggregate" {
			h.aggregates.Add(1)
		}
		return next(ctx, cmd)
	}
}

// Pending-cleanup planner guards operate on a copy of pinned metadata. Never
// mutate a published descriptor merely to force a fallback in a regression.
func cleanupSnapshotPlan(t *testing.T, h *sqlHarness, query string, hooks ...goredis.Hook) (*executor, *selectPlan, context.Context) {
	t.Helper()
	st, ctx, pin := h.inspectionSnapshot(h.rawClient(hooks...))
	t.Cleanup(pin.release)
	st.searchCapsOnce.Do(func() { st.searchCaps = [2]int64{1000000, 1 << 31} })
	e := &executor{store: st, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC(), sess: newSession(defaultSchema)}
	parsed, err := ParseScript(query)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	p.meta = cloneMeta(p.meta)
	p.meta.PendingCleanup = []string{"retired_field"}
	return e, p, ctx
}
