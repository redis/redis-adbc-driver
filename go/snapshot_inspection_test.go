// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Sparse worker-range fixtures must publish canonical row keys/directories and
// native sequence metadata together, rather than edit a pinned source HASH.
func (h *sqlHarness) sparseSnapshotTable(table string, last int64, moves map[int64]int64) {
	h.t.Helper()
	s, ctx, pin := h.inspectionSnapshot(h.rawClient(), true)
	defer pin.release()
	old, err := s.getTable(ctx, defaultSchema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	seq := seqKey(defaultSchema, table)
	previous, err := s.redis(ctx).Get(ctx, seq).Int64()
	if err != nil || last < previous {
		h.t.Fatal("invalid sparse sequence fixture", previous, last, err)
	}
	var keys []string
	for from, to := range moves {
		if from < 1 || to < 1 || to > last || from == to {
			h.t.Fatal("invalid sparse row move", from, to)
		}
		keys = append(keys, old.prefix()+strconv.FormatInt(from, 10), old.prefix()+strconv.FormatInt(to, 10))
	}
	before, err := s.fetchSnapshotVisible(ctx, old, keys, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	wctx, work, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		h.t.Fatal(err)
	}
	defer s.abortSnapshot(pin, work)
	if len(moves) > 0 {
		if err := s.prepareSnapshotTable(wctx, defaultSchema, table); err != nil {
			h.t.Fatal(err)
		}
		m, err := s.getTable(wctx, defaultSchema, table)
		if err != nil {
			h.t.Fatal(err)
		}
		var images []snapshotInsertRow
		seen := map[string]bool{}
		for i := 0; i < len(keys); i += 2 {
			from, to := strings.TrimPrefix(keys[i], old.prefix()), strings.TrimPrefix(keys[i+1], old.prefix())
			if before[i] == nil || before[i+1] != nil || seen[to] {
				h.t.Fatal("sparse move source missing or destination occupied", from, to)
			}
			seen[to] = true
			fields := []any{rowIDField, to}
			for name, value := range before[i] {
				if name != rowIDField {
					fields = append(fields, name, value)
				}
			}
			for _, row := range []struct {
				id     string
				fields []any
			}{
				{from, []any{rowIDField, from, snapshotDeletedField, "1"}}, {to, fields},
			} {
				key := m.prefix() + row.id
				images = append(images, snapshotInsertRow{key: key, member: snapshotDirectoryMember(key), fields: row.fields})
			}
		}
		if err := s.writeSnapshotImages(wctx, m.prefix(), images, true); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := s.redis(wctx).Set(wctx, seq, last, 0).Err(); err != nil {
		h.t.Fatal(err)
	}
	if err := s.publishSnapshot(wctx, pin, work); err != nil {
		h.t.Fatal(err)
	}
	after, err := s.fetchSnapshotVisible(ctx, old, keys, nil)
	if err != nil || !reflect.DeepEqual(before, after) {
		h.t.Fatal("sparse fixture changed its pinned source", err)
	}
	if n, err := s.redis(ctx).Get(ctx, seq).Int64(); err != nil || n != previous {
		h.t.Fatal("sparse fixture changed its pinned sequence", n, previous, err)
	}
	next, nextPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	defer nextPin.release()
	if n, err := s.redis(next).Get(next, seq).Int64(); err != nil || n != last {
		h.t.Fatal("sparse sequence was not published", n, last, err)
	}
	m, err := s.getTable(next, defaultSchema, table)
	if err != nil {
		h.t.Fatal(err)
	}
	nextKeys := make([]string, len(keys))
	for i, key := range keys {
		nextKeys[i] = m.prefix() + strings.TrimPrefix(key, old.prefix())
	}
	visible, err := s.fetchSnapshotVisible(next, m, nextKeys, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	for i := 0; i < len(keys); i += 2 {
		want := map[string]string{}
		for name, value := range before[i] {
			want[name] = value
		}
		want[rowIDField] = strings.TrimPrefix(nextKeys[i+1], m.prefix())
		if visible[i] != nil || !reflect.DeepEqual(map[string]string(visible[i+1]), want) {
			h.t.Fatal("sparse row move was not published exactly", keys[i], keys[i+1])
		}
	}
}

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

// Resolve a logical SQL id and inspect its newest stored image under the same
// pin. A latest-segment prefix alone cannot locate unchanged older rows.
func (h *sqlHarness) storedRowByID(raw goredis.UniversalClient, table string, id int) aggRow {
	h.t.Helper()
	s, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	parsed, err := ParseScript("SELECT __rowid FROM " + table + " WHERE id = " + strconv.Itoa(id))
	if err != nil {
		h.t.Fatal(err)
	}
	e := &executor{store: s, schema: defaultSchema, sess: newSession(defaultSchema), pushdown: PushdownExact, cache: newExecCache()}
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		h.t.Fatal(err)
	}
	ids, err := e.runSelect(ctx, plan, nil)
	if err != nil || len(ids) != 1 || len(ids[0]) != 1 || ids[0][0].Null {
		h.t.Fatalf("row %d of %s unavailable: %v", id, table, err)
	}
	key := plan.meta.prefix() + strconv.FormatInt(ids[0][0].I, 10)
	rows, err := s.fetchSnapshotVisible(ctx, plan.meta, []string{key}, nil)
	if err != nil || len(rows) != 1 || rows[0] == nil {
		h.t.Fatalf("stored row %d of %s unavailable: %v", id, table, err)
	}
	return rows[0]
}

func TestSQLStoredRowInspectionVisibility(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_stored_row_inspection"
	h.dropTables(table)
	h.exec("CREATE TABLE " + table + " (id INTEGER,v BIGINT)")
	h.exec("INSERT INTO " + table + " VALUES (10,9007199254740993),(-1,NULL)")
	h.exec("UPDATE " + table + " SET v=9007199254740995 WHERE id=10")
	raw := h.rawClient()
	if row := h.storedRowByID(raw, table, 10); row["v"] != "9007199254740995" || row["id"] != "10" {
		t.Fatal("inspection lost exact replacement", row)
	}
	if row := h.storedRowByID(raw, table, -1); row["id"] != "-1" || row[rowIDField] != "2" {
		t.Fatal("inspection lost older-segment row", row)
	} else if _, ok := row["v"]; ok {
		t.Fatal("inspection materialized NULL", row)
	}
	h.expectRows("SELECT id,v FROM "+table+" ORDER BY id", "-1|NULL", "10|9007199254740995")
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

// Snapshot ownership is the reference graph, not the legacy name registries.
// Reclamation may lag publication but must finish after the final pin retires.
func (h *sqlHarness) expectSnapshotReleased(raw goredis.UniversalClient, prefix, index string) {
	h.t.Helper()
	st := &store{client: raw, snapshots: true}
	snapshotReapTest(st, h.ctx)
	if n := h.prefixKeyCount(raw, prefix); n != 0 {
		h.t.Errorf("%d rows remain in released generation", n)
	}
	if err := raw.Do(h.ctx, "FT.INFO", index).Err(); err == nil || !isUnknownIndex(err) {
		h.t.Errorf("released snapshot index remains: %v", err)
	}
	data := snapshotDataID(prefix)
	if data == "" {
		h.t.Fatal("invalid released snapshot prefix")
	}
	if n, err := raw.Exists(h.ctx, snapshotMembers(data)).Result(); err != nil || n != 0 {
		h.t.Errorf("released row directory remains: %d %v", n, err)
	}
	for _, field := range []string{"d:" + data, "r:" + data} {
		if exists, err := raw.HExists(h.ctx, snapshotMetadataGraph, field).Result(); err != nil || exists {
			h.t.Errorf("released ownership field remains: %s %v", field, err)
		}
	}
}
