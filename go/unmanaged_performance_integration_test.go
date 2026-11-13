// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type unmanagedPerformanceHook struct {
	snapshotHook
	inserts, pipelines int
}

func (h *unmanagedPerformanceHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "eval" && len(args) > 1 && args[1] == keyRowInsertLua {
			h.inserts++
		}
		return next(ctx, cmd)
	}
}
func (h *unmanagedPerformanceHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		writes, bytes := 0, 0
		for _, cmd := range cmds {
			args := cmd.Args()
			if cmd.Name() == "eval" && len(args) > 1 && args[1] == keyRowInsertLua {
				writes++
				bytes += 32
				for _, arg := range args {
					bytes += len(fmt.Sprint(arg)) + 32
				}
				h.inserts++
				if snapshotOf(ctx) != nil {
					return fmt.Errorf("source pipeline retained managed scope")
				}
				marker, ok := cmd.(interface{ NoRetry() bool })
				if !ok || !marker.NoRetry() {
					return fmt.Errorf("source pipeline permits replay")
				}
			}
		}
		if writes > unmanagedInsertPipelineRows || writes > 1 && bytes > unmanagedInsertPipelineBytes {
			return fmt.Errorf("unbounded source pipeline: %d rows, %d bytes", writes, bytes)
		}
		if writes > 0 {
			h.pipelines++
		}
		return next(ctx, cmds)
	}
}

func TestUnmanagedInsertUsesBoundedPipelines(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "live")
	if err != nil {
		t.Fatal(err)
	}
	meta = insertMeta(meta)
	ctx = context.WithValue(ctx, unmanagedWriteKey{}, metaKey("public", "live"))
	rows := make([][]Value, 300)
	ki, _ := meta.resolve(keyField)
	ni, _ := meta.resolve("n")
	for i := range rows {
		rows[i] = make([]Value, len(meta.Columns))
		for j, c := range meta.Columns {
			rows[i][j] = nullValue(c.Type)
		}
		rows[i][ki] = stringValue(fmt.Sprintf("app:bulk%d", i))
		rows[i][ni] = intValue(typeInt64, int64(i))
	}
	hook := &unmanagedPerformanceHook{}
	s.client.AddHook(hook)
	n, err := s.insertRows(ctx, meta, rows)
	if err != nil || n != int64(len(rows)) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if hook.inserts != len(rows) || hook.pipelines < 2 || hook.pipelines > 4 {
		t.Fatalf("serial/unbounded writes: rows=%d pipelines=%d", hook.inserts, hook.pipelines)
	}
	for i := range rows {
		if got := raw.HGet(h.ctx, fmt.Sprintf("app:bulk%d", i), "n").Val(); got != fmt.Sprint(i) {
			t.Fatalf("row %d: %q", i, got)
		}
	}
}

func TestUnmanagedNumericRangeUsesIndexAndRechecks(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "live")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.loadUnmanagedIndex(ctx, meta); err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s}
	predicate := &Binary{Op: ">=", L: &ColumnRef{Name: "n"}, R: &Literal{V: intValue(typeInt64, 9)}}
	wp, err := e.planWhere(ctx, predicate, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wp.query, "@n:") || wp.residual == nil {
		t.Fatalf("range scans whole table or trusts index values: %+v", wp)
	}
	h.expectRows("SELECT n FROM live WHERE n >= 9", "10")
}

func TestUnmanagedCandidateRechecksLiveHASH(t *testing.T) {
	for _, change := range []string{"changed", "deleted", "wrongtype"} {
		t.Run(change, func(t *testing.T) {
			h, raw, _ := unmanagedIngestFixture(t, false)
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			meta, err := s.getTable(ctx, "public", "live")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.loadUnmanagedIndex(ctx, meta); err != nil {
				t.Fatal(err)
			}
			e := &executor{store: s, diagnostics: &executionDiagnostics{}}
			predicate := &Binary{Op: ">=", L: &ColumnRef{Name: "n"}, R: &Literal{V: intValue(typeInt64, 9)}}
			wp, err := e.planWhere(ctx, predicate, meta, nil)
			if err != nil {
				t.Fatal(err)
			}
			it, err := e.openScan(ctx, scanRequest{meta: meta, where: wp, need: map[string]bool{"n": true}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer it.close(ctx)
			if !it.unmanagedHashFetch {
				t.Fatal("scan still loads full Search documents")
			}
			switch change {
			case "changed":
				err = raw.HSet(h.ctx, "app:seed", "n", "1").Err()
			case "deleted":
				err = raw.Del(h.ctx, "app:seed").Err()
			case "wrongtype":
				if err = raw.Del(h.ctx, "app:seed").Err(); err == nil {
					err = raw.Set(h.ctx, "app:seed", "string", 0).Err()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			keys, rows, err := it.nextPage(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != 0 || len(rows) != 0 {
				t.Fatalf("stale/non-HASH candidate survived: %v", keys)
			}
		})
	}
}

func TestUnmanagedRangeLargeIntegersAndIndexChanges(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	for key, n := range map[string]string{"app:lo": "9007199254740992", "app:hi": "9007199254740993"} {
		if err := raw.HSet(h.ctx, key, "n", n).Err(); err != nil {
			t.Fatal(err)
		}
	}
	h.expectRows("SELECT n FROM live WHERE n > 9007199254740992", "9007199254740993")
	h.expectRows("SELECT n FROM live WHERE n = 9007199254740993", "9007199254740993")
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "app_idx").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "extra", "TEXT").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT n FROM live WHERE n = 9007199254740993", "9007199254740993")
}

type unmanagedInsertRaceHook struct {
	snapshotHook
	raw     *goredis.Client
	key     string
	batches int
}

func (h *unmanagedInsertRaceHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.Name() == "eval" && len(cmd.Args()) > 1 && cmd.Args()[1] == keyRowInsertLua {
				h.batches++
				if h.batches == 1 {
					if err := h.raw.HSet(ctx, h.key, "n", "999").Err(); err != nil {
						return err
					}
				}
				break
			}
		}
		return next(ctx, cmds)
	}
}
func TestUnmanagedInsertRaceCountsAllAcknowledgments(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	keys := make([]string, unmanagedInsertPipelineRows+1)
	values := make([]int64, len(keys))
	for i := range keys {
		keys[i], values[i] = fmt.Sprintf("app:race%d", i), int64(i)
	}
	st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, keys, values)
	hook := &unmanagedInsertRaceHook{raw: raw, key: keys[1]}
	st.(*statementImpl).conn.store.client.AddHook(hook)
	n, err := st.ExecuteUpdate(h.ctx)
	if n != unmanagedInsertPipelineRows-1 || err == nil || !strings.Contains(err.Error(), "already exists") || hook.batches != 1 {
		t.Fatalf("n=%d batches=%d err=%v", n, hook.batches, err)
	}
	assertUnmanagedAcknowledged(t, err, n)
	var ae adbc.Error
	if !errors.As(err, &ae) || string(ae.SqlState[:]) != stateUnique {
		t.Fatalf("lost conflict SQLSTATE: %v", err)
	}
	if raw.Exists(h.ctx, keys[len(keys)-1]).Val() != 0 || raw.HGet(h.ctx, keys[1], "n").Val() != "999" {
		t.Fatal("conflict overwritten or next pipeline submitted")
	}
	for i, key := range keys[:len(keys)-1] {
		if i != 1 && raw.HGet(h.ctx, key, "n").Val() != fmt.Sprint(i) {
			t.Fatalf("acknowledged row %d lost", i)
		}
	}
}

func TestUnmanagedInsertPipelineByteBound(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "live")
	if err != nil {
		t.Fatal(err)
	}
	meta = insertMeta(meta)
	ctx = context.WithValue(ctx, unmanagedWriteKey{}, metaKey("public", "live"))
	rows := make([][]Value, 3)
	payload := strings.Repeat("x", 600<<10)
	for i := range rows {
		rows[i] = make([]Value, len(meta.Columns))
		for j, c := range meta.Columns {
			switch c.Name {
			case keyField:
				rows[i][j] = stringValue(fmt.Sprintf("app:large%d", i))
			case "extra":
				rows[i][j] = stringValue(payload)
			default:
				rows[i][j] = intValue(c.Type, int64(i))
			}
		}
	}
	hook := &unmanagedPerformanceHook{}
	s.client.AddHook(hook)
	n, err := s.insertRows(ctx, meta, rows)
	if n != 3 || err != nil || hook.pipelines != 3 {
		t.Fatalf("n=%d pipelines=%d err=%v", n, hook.pipelines, err)
	}
	for i := range rows {
		if raw.HGet(h.ctx, fmt.Sprintf("app:large%d", i), "extra").Val() != payload {
			t.Fatal("large row differs")
		}
	}
}

func TestUnmanagedRangePrunesCandidates(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	for i := range 100 {
		if err := raw.HSet(h.ctx, fmt.Sprintf("app:range%d", i), "n", fmt.Sprint(i)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	s := snapshotTestStore(t, raw)
	if err := s.awaitIndexReady(h.ctx, "app_idx"); err != nil {
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
	if err := s.loadUnmanagedIndex(ctx, meta); err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, diagnostics: &executionDiagnostics{}}
	wp, err := e.planWhere(ctx, &Binary{Op: ">=", L: &ColumnRef{Name: "n"}, R: &Literal{V: intValue(typeInt64, 98)}}, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	it, err := e.openScan(ctx, scanRequest{meta: meta, where: wp, need: map[string]bool{"n": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	keys, rows, err := it.nextPage(ctx)
	if err != nil || len(keys) != 2 || len(rows) != 2 || e.diagnostics.processed.Load() != 2 {
		t.Fatalf("keys=%d rows=%d processed=%d err=%v", len(keys), len(rows), e.diagnostics.processed.Load(), err)
	}
}
