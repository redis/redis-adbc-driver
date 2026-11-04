// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotAlterReadyHook struct {
	snapshotHook
	index           atomic.Pointer[string]
	creates, alters atomic.Int64
	infoReads       atomic.Int64
	ready           atomic.Bool
	stateError      string
	transportError  bool
	once            sync.Once
	waiting         chan struct{}
}

func (h *snapshotAlterReadyHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if index := h.index.Load(); index != nil && len(args) > 1 && cmd.Name() == "ft.info" && fmt.Sprint(args[1]) == *index && !h.ready.Load() {
			h.infoReads.Add(1)
			h.once.Do(func() { close(h.waiting) })
			if h.stateError != "" {
				var err error = readinessServerError(h.stateError)
				if h.transportError {
					err = errors.New(h.stateError)
				}
				cmd.SetErr(err)
				return err
			}
			cmd.(interface{ SetVal(any) }).SetVal([]any{"indexing", int64(1), "percent_indexed", "0"})
			return nil
		}
		err := next(ctx, cmd)
		if cmd.Name() == "ft.alter" {
			h.alters.Add(1)
		}
		if cmd.Name() == "ft.create" && err == nil && len(args) > 1 {
			index := fmt.Sprint(args[1])
			h.creates.Add(1)
			h.index.CompareAndSwap(nil, &index)
		}
		return err
	}
}

func TestSnapshotIndexReadinessTerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		transport     bool
		code          adbc.Status
	}{
		{"permission", "NOPERM no FT.INFO permission", false, adbc.StatusUnauthorized},
		{"missing", "Unknown index name", false, adbc.StatusIO},
		{"transport", "Inconsistent index state", true, adbc.StatusIO},
		{"other_server_error", "SEARCH_VALUE_BAD Inconsistent index state: another failure", false, adbc.StatusIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_readiness_terminal"
			h.dropTables(table)
			h.exec("CREATE TABLE " + table + "(id INTEGER)")
			h.exec("INSERT INTO " + table + " VALUES(1)")
			raw := h.rawClient()
			root, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
			if err != nil {
				t.Fatal(err)
			}
			hook := &snapshotAlterReadyHook{stateError: tc.message, transportError: tc.transport, waiting: make(chan struct{})}
			st := &store{client: h.rawClient(hook), snapshots: true}
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			wctx, pin, work := snapshotStageSQL(t, st, ctx, "", "ALTER TABLE "+table+" ADD COLUMN extra INTEGER DEFAULT 7")
			private, err := st.getTable(wctx, defaultSchema, table)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- st.publishSnapshot(wctx, pin, work) }()
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("publication did not stop after cancellation")
				}
				t.Fatal("terminal error was polled")
			}
			if !isStatus(err, tc.code) {
				t.Fatal("wrong terminal status", err, tc.code)
			}
			if hook.creates.Load() != 1 || hook.infoReads.Load() != 1 || hook.alters.Load() != 0 {
				t.Fatal("terminal error replayed index work", hook.creates.Load(), hook.infoReads.Load())
			}
			if got, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || got != root {
				t.Fatal("terminal error published", err)
			}
			st.abortSnapshot(pin, work)
			pin.release()
			if index := hook.index.Load(); index == nil {
				t.Fatal("private index missing")
			} else {
				h.expectSnapshotReleased(raw, private.prefix(), *index)
			}
			h.expectColumns("SELECT * FROM "+table, "id", "1")
		})
	}
}

func testSnapshotAlterReadinessCase(t *testing.T, stateError string, cancelWait bool) {
	t.Helper()
	h := newSQLHarness(t)
	const table = "it_alter_ready"
	h.dropTables(table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	h.exec("INSERT INTO " + table + " VALUES(9007199254740993)")
	raw := h.rawClient()
	oldStore, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	old, err := oldStore.getTable(oldCtx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	before, err := oldStore.redis(oldCtx).Get(oldCtx, metaKey(defaultSchema, table)).Result()
	if err != nil {
		t.Fatal(err)
	}
	beforeIndex, err := raw.Do(h.ctx, "FT.INFO", old.index()).Result()
	if err != nil {
		t.Fatal(err)
	}
	root, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
	if err != nil {
		t.Fatal(err)
	}
	hook := &snapshotAlterReadyHook{stateError: stateError, waiting: make(chan struct{})}
	st := &store{client: h.rawClient(hook), snapshots: true}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	wctx, pin, work := snapshotStageSQL(t, st, ctx, "", "ALTER TABLE "+table+" ADD COLUMN extra INTEGER DEFAULT 7")
	private, err := st.getTable(wctx, defaultSchema, table)
	if err != nil || private.prefix() == old.prefix() {
		t.Fatal("ALTER did not stage a new layout", err)
	}
	finished := make(chan struct{})
	var publishErr error
	go func() { publishErr = st.publishSnapshot(wctx, pin, work); close(finished) }()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("publication did not join after cancellation")
		}
		st.abortSnapshot(pin, work)
		pin.release()
	}()
	select {
	case <-hook.waiting:
	case <-finished:
		t.Fatal("publication completed before readiness", publishErr)
	case <-time.After(5 * time.Second):
		t.Fatal("publication never checked readiness")
	}
	if got, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || got != root {
		t.Fatal("published before readiness", err)
	}
	if got := h.rawMeta(raw, defaultSchema, table); got != before {
		t.Fatal("changed current metadata before readiness")
	}
	h.expectColumns("SELECT * FROM "+table, "id", "9007199254740993")
	if cancelWait {
		cancel()
	} else {
		hook.ready.Store(true)
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not finish")
	}
	if cancelWait {
		if !errors.Is(publishErr, context.Canceled) && !isStatus(publishErr, adbc.StatusCancelled) {
			t.Fatal("wrong cancellation", publishErr)
		}
		if got, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || got != root {
			t.Fatal("cancelled ALTER published", err)
		}
		st.abortSnapshot(pin, work)
		pin.release()
		if index := hook.index.Load(); index == nil {
			t.Fatal("no actual private index")
		} else {
			h.expectSnapshotReleased(raw, private.prefix(), *index)
		}
		h.expectColumns("SELECT * FROM "+table, "id", "9007199254740993")
		h.exec("ALTER TABLE " + table + " ADD COLUMN extra INTEGER DEFAULT 9")
		if c := h.tableColumn(raw, table, "extra"); c.field() != "extra" || c.Default != "9" {
			t.Fatal("cancelled private field poisoned fresh ALTER", c)
		}
		h.expectRows("SELECT id,extra FROM "+table, "9007199254740993|9")
	} else {
		if publishErr != nil {
			t.Fatal(publishErr)
		}
		pin.release()
		h.expectRows("SELECT id,extra FROM "+table, "9007199254740993|7")
	}
	if hook.creates.Load() != 1 || hook.alters.Load() != 0 {
		t.Fatal("replayed index creation or altered a published index", hook.creates.Load(), hook.alters.Load())
	}
	if got, err := oldStore.redis(oldCtx).Get(oldCtx, metaKey(defaultSchema, table)).Result(); err != nil || got != before {
		t.Fatal("pinned definition changed", err)
	}
	rows, err := oldStore.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1"}, nil)
	if err != nil || len(rows) != 1 || rows[0]["id"] != "9007199254740993" || len(rows[0]) != 2 {
		t.Fatal("pinned row changed", rows, err)
	}
	snapshotReapTest(oldStore, h.ctx)
	afterIndex, err := raw.Do(h.ctx, "FT.INFO", old.index()).Result()
	if err != nil || !reflect.DeepEqual(indexInfoOf(beforeIndex).attrs, indexInfoOf(afterIndex).attrs) {
		t.Fatal("ALTER or GC changed the pinned index schema", err)
	}
	oldPin.release()
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
}

// Empty TRUNCATE output must still seal one fresh index before publication.
// Polling or cancellation must not mutate the retained source or replay CREATE.
func TestSnapshotTruncateIndexReadiness(t *testing.T) {
	for _, state := range []string{"", "Inconsistent index state", "SEARCH_VALUE_BAD Inconsistent index state"} {
		for _, cancelWait := range []bool{false, true} {
			t.Run(fmt.Sprint(state, "/", cancelWait), func(t *testing.T) {
				h := newSQLHarness(t)
				const table = "it_truncate_private_ready"
				h.dropTables(table)
				h.exec("CREATE TABLE " + table + " (id BIGINT,note VARCHAR)")
				h.exec("INSERT INTO " + table + " VALUES (9007199254740993,NULL)")
				raw := h.rawClient()
				oldStore, oldCtx, oldPin := h.inspectionSnapshot(raw)
				defer oldPin.release()
				old, err := oldStore.getTable(oldCtx, defaultSchema, table)
				if err != nil {
					t.Fatal(err)
				}
				before := h.rawMeta(raw, defaultSchema, table)
				root, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
				if err != nil {
					t.Fatal(err)
				}
				hook := &snapshotAlterReadyHook{stateError: state, waiting: make(chan struct{})}
				st := &store{client: h.rawClient(hook), snapshots: true}
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				wctx, pin, work := snapshotStageSQL(t, st, ctx, "", "TRUNCATE "+table+" RESTART IDENTITY")
				private, err := st.getTable(wctx, defaultSchema, table)
				if err != nil || private.prefix() == old.prefix() {
					t.Fatal("TRUNCATE did not allocate fresh output", err)
				}
				finished := make(chan struct{})
				var publishErr error
				go func() { publishErr = st.publishSnapshot(wctx, pin, work); close(finished) }()
				defer func() {
					cancel()
					select {
					case <-finished:
					case <-time.After(5 * time.Second):
						t.Error("TRUNCATE publication did not join")
					}
					st.abortSnapshot(pin, work)
					pin.release()
				}()
				select {
				case <-hook.waiting:
				case <-finished:
					t.Fatal("TRUNCATE published without readiness", publishErr)
				case <-time.After(5 * time.Second):
					t.Fatal("TRUNCATE never checked private index readiness")
				}
				if got, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || got != root {
					t.Fatal("TRUNCATE changed root before readiness", err)
				}
				if h.rawMeta(raw, defaultSchema, table) != before {
					t.Fatal("TRUNCATE changed definition before readiness")
				}
				h.expectRows("SELECT __rowid,id,note FROM "+table, "1|9007199254740993|NULL")
				if cancelWait {
					cancel()
				} else {
					hook.ready.Store(true)
				}
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("TRUNCATE readiness did not finish")
				}
				if cancelWait {
					if !errors.Is(publishErr, context.Canceled) && !isStatus(publishErr, adbc.StatusCancelled) {
						t.Fatal("wrong TRUNCATE cancellation", publishErr)
					}
					if got, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || got != root {
						t.Fatal("cancelled TRUNCATE published", err)
					}
					st.abortSnapshot(pin, work)
					pin.release()
					index := hook.index.Load()
					if index == nil {
						t.Fatal("private index was not created")
					}
					h.expectSnapshotReleased(raw, private.prefix(), *index)
					h.expectRows("SELECT __rowid,id,note FROM "+table, "1|9007199254740993|NULL")
					h.exec("TRUNCATE " + table + " RESTART IDENTITY")
				} else {
					if publishErr != nil {
						t.Fatal(publishErr)
					}
					pin.release()
				}
				if hook.creates.Load() != 1 || hook.alters.Load() != 0 || hook.infoReads.Load() == 0 {
					t.Fatal("TRUNCATE replayed index work or missed readiness", hook.creates.Load(), hook.alters.Load(), hook.infoReads.Load())
				}
				h.expectRows("SELECT COUNT(*) FROM "+table, "0")
				h.exec("INSERT INTO " + table + " (id) VALUES (8)")
				h.expectRows("SELECT __rowid,id,note FROM "+table, "1|8|NULL")
				if got, err := oldStore.redis(oldCtx).Get(oldCtx, metaKey(defaultSchema, table)).Result(); err != nil || got != before {
					t.Fatal("old TRUNCATE definition changed", err)
				}
				if got := strings.Join(valueRows(snapshotExecuteTest(t, oldStore, oldCtx, "SELECT __rowid,id,note FROM "+table).rows), "|"); got != "1|9007199254740993|NULL" {
					t.Fatal("old TRUNCATE rows changed", got)
				}
				oldPin.release()
				for _, seg := range old.snapshotSegments() {
					h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
				}
			})
		}
	}
}
