// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Inspect the same immutable metadata as an ordinary SQL statement, using the
// caller's inspection credentials. A legacy unscoped store cannot find it.
func (h *sqlHarness) inspectionSnapshot(raw goredis.UniversalClient, writing ...bool) (*store, context.Context, *snapshotPin) {
	h.t.Helper()
	// rawClient installs hooks before the client is shared with other test
	// goroutines; inspection must not mutate a live client's hook chain.
	s := &store{client: raw, snapshots: true}
	ctx, pin, err := s.acquireSnapshotMode(h.ctx, false, writing...)
	if err != nil {
		h.t.Fatal(err)
	}
	return s, ctx, pin
}

func (h *sqlHarness) patchStoredRow(schema, table, id, field, value string) {
	h.t.Helper()
	s, ctx, pin := h.inspectionSnapshot(h.rawClient(), true)
	defer pin.release()
	wctx, work, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		h.t.Fatal(err)
	}
	defer s.abortSnapshot(pin, work)
	if err := s.prepareSnapshotTable(wctx, schema, table); err != nil {
		h.t.Fatal(err)
	}
	m, err := s.getTable(wctx, schema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	key := m.prefix() + id
	rows, err := s.fetchSnapshotVisible(wctx, m, []string{key}, nil)
	if err != nil || len(rows) != 1 || rows[0] == nil {
		h.t.Fatalf("test patch source unavailable: %v", err)
	}
	rows[0][field] = value
	fields := []any{rowIDField, id}
	for name, v := range rows[0] {
		if name != rowIDField {
			fields = append(fields, name, v)
		}
	}
	if err := s.writeSnapshotImages(wctx, m.prefix(), []snapshotInsertRow{{key: key, member: snapshotDirectoryMember(key), fields: fields}}, true); err != nil {
		h.t.Fatal(err)
	}
	if err := s.publishSnapshot(wctx, pin, work); err != nil {
		h.t.Fatal(err)
	}
}

func (h *sqlHarness) expectNoIndexCursors(s *store, index string) {
	h.t.Helper()
	reply, err := s.searchDo(h.ctx, index, "FT.INFO", index).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	info, ok := redisReplyFields(reply)
	if !ok {
		h.t.Fatalf("unexpected FT.INFO reply %T", reply)
	}
	stats, ok := redisReplyFields(info["cursor_stats"])
	if !ok {
		h.t.Fatal("FT.INFO omitted cursor statistics")
	}
	n, ok := stats["index_total"]
	if !ok || fmt.Sprint(n) != "0" {
		h.t.Fatalf("missing or nonzero index cursor count: %v", stats)
	}
}

func TestSQLInspectionPinsAndReadOnly(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_inspection")
	h.exec("CREATE TABLE it_inspection(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO it_inspection VALUES(1,9007199254740993)")
	e, p, _ := distinctTestPlan(t, h, "SELECT id,v FROM it_inspection")
	h.exec("UPDATE it_inspection SET v=v+1")
	h.expectRows("SELECT id,v FROM it_inspection", "1|9007199254740994")
	h.exec("DROP TABLE it_inspection")
	if err := e.store.client.HSet(h.ctx, p.meta.prefix()+"1", "v", "0").Err(); err == nil {
		t.Fatal("inspection client mutated its pinned generation")
	}
	rows, err := e.runSelect(h.ctx, p, nil)
	if err != nil || len(rows) != 1 || rows[0][0].I != 1 || rows[0][1].I != 9007199254740993 {
		t.Fatalf("inspection did not retain the exact old generation: %v %v", rows, err)
	}
}

func (h *sqlHarness) storedTableJSON(raw goredis.UniversalClient, schema, table string) string {
	h.t.Helper()
	s, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	doc, err := s.redis(ctx).Get(ctx, metaKey(schema, table)).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	return doc
}

// Corrupt-definition regressions use a registered private metadata value and
// publish through the protocol; they must not overwrite an immutable source.
func (h *sqlHarness) writeTableJSON(raw goredis.UniversalClient, schema, table, doc string) {
	h.t.Helper()
	s, ctx, pin := h.inspectionSnapshot(raw, true)
	defer pin.release()
	m, err := s.getTable(ctx, schema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	if !sameKeys(m, doc) {
		h.t.Fatal("test metadata edit changed the table's physical identity")
	}
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		h.t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.redis(wctx).Set(wctx, metaKey(schema, table), doc, 0).Err(); err != nil {
		h.t.Fatal(err)
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		h.t.Fatal(err)
	}
}

func (h *sqlHarness) inspectTable(raw goredis.UniversalClient, schema, table string) *tableMeta {
	h.t.Helper()
	s, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	m, err := s.getTable(ctx, schema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	return m
}

// Low-level plan tests pass cancellation contexts independently of planning.
// Keep their pinned metadata on the plan's dedicated client without changing
// h.ctx: subsequent SQL on h must acquire its own current statement generation.
type snapshotInspectionPin struct {
	snapshotHook
	scope atomic.Pointer[snapshotScope]
}

func (p *snapshotInspectionPin) context(ctx context.Context) context.Context {
	if snapshotOf(ctx) == nil {
		if scope := p.scope.Load(); scope != nil {
			return context.WithValue(ctx, snapshotContextKey{}, scope)
		}
	}
	return ctx
}
func (p *snapshotInspectionPin) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { return next(p.context(ctx), cmd) }
}
func (p *snapshotInspectionPin) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error { return next(p.context(ctx), cmds) }
}

func (h *sqlHarness) frozenPlanStore(hooks ...goredis.Hook) (*store, context.Context) {
	h.t.Helper()
	bridge := &snapshotInspectionPin{}
	// Inject the pin before catalog admission/key resolution, not afterward.
	raw := h.rawClient(append([]goredis.Hook{bridge}, hooks...)...)
	s, ctx, pin := h.inspectionSnapshot(raw)
	bridge.scope.Store(snapshotOf(ctx))
	h.t.Cleanup(pin.release)
	return s, ctx
}
