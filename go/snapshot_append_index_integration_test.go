// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotAppendIndexObserver struct {
	snapshotHook
	creates, rows, notifications atomic.Int64
	fault                        string
	index                        string
	raw                          *goredis.Client
}

func snapshotAppendIndexTransport(t *testing.T, ctx context.Context, st adbc.StatementWithContext, hooks ...goredis.Hook) context.Context {
	t.Helper()
	impl := st.(*statementImpl).conn
	ctx, transport := impl.executionContext(ctx, &impl.streams)
	for _, hook := range hooks {
		impl.store.client.AddHook(hook)
		transport.client.AddHook(hook)
	}
	t.Cleanup(transport.finish)
	return ctx
}

func TestSnapshotAppendIndexedWideExact(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	declarations := make([]string, 105)
	for i := range declarations {
		name := fmt.Sprintf("c%d", i)
		if i == 0 {
			name = "id"
		}
		declarations[i] = name + " BIGINT NOINDEX"
	}
	h.exec("CREATE TABLE wide(" + strings.Join(declarations, ",") + ")")
	rec := followupRecord(4096)
	defer rec.Release()
	observer := &snapshotAppendIndexObserver{}
	ctx, _ := statisticsHookedTransport(t, h, observer)
	if err := followupIngest(ctx, h.conn, "wide", adbc.OptionValueIngestModeAppend, rec); err != nil {
		t.Fatal(err)
	}
	if observer.creates.Load() != 1 || observer.rows.Load() != 4096 || observer.notifications.Load() != 0 {
		t.Fatalf("wide append unexpectedly rebuilt/replayed rows: %+v", observer)
	}
	actual, err := followupFetch(h.ctx, h.conn, "SELECT * FROM wide ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	followupValidate(t, actual, 1, 4096)
}

func TestSnapshotAppendIndexDeferredIndexedColumns(t *testing.T) {
	h, _, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT)")
	st, _ := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 1, nil)
	observer := &snapshotIndexNotifyObserver{}
	ctx := snapshotAppendIndexTransport(t, h.ctx, st, observer)
	if n, err := st.ExecuteUpdate(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if observer.rows.Load() != 1 {
		t.Fatal("public-indexed append did not retain deferred population", observer.rows.Load())
	}
	h.expectRows("SELECT id FROM t", "1")
}

func (o *snapshotAppendIndexObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		create := cmd.Name() == "ft.create"
		rows := snapshotInsertBatchRowKeys(cmd)
		if create {
			o.creates.Add(1)
			o.index = fmt.Sprint(a[1])
			if o.fault == "create-denied" {
				err := readinessServerError("NOPERM injected append index creation denial")
				cmd.SetErr(err)
				return err
			}
		}
		if len(a) > 1 && a[1] == snapshotIndexNotifyLua {
			o.notifications.Add(1)
		}
		if err := next(ctx, cmd); err != nil {
			return err
		}
		if create && o.fault == "create-lost" {
			err := errors.New("lost successful append index creation reply")
			cmd.SetErr(err)
			return err
		}
		if len(rows) > 0 {
			if o.creates.Load() != 1 {
				return errors.New("row image preceded final index creation")
			}
			o.rows.Add(int64(len(rows)))
			if o.fault == "index-dropped" {
				if err := o.raw.Do(ctx, "FT.DROPINDEX", o.index).Err(); err != nil {
					return err
				}
			}
			if o.fault == "row-lost" {
				err := errors.New("lost successful indexed row reply")
				cmd.SetErr(err)
				return err
			}
		}
		return nil
	}
}

func TestSnapshotAppendIndexedFaultIsolation(t *testing.T) {
	for _, fault := range []string{"", "create-denied", "create-lost", "index-dropped", "row-lost", "proof-warning", "proof-transport", "proof-denied"} {
		t.Run(fault, func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOINDEX)")
			h.exec("INSERT INTO t VALUES(0)")
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			st, input := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 9007199254740993, nil)
			observer := &snapshotAppendIndexObserver{fault: fault, raw: raw}
			store := st.(*statementImpl).conn.store
			proof := &snapshotIndexNotifyObserver{proofWarning: fault == "proof-warning", proofTransport: fault == "proof-transport", proofDenied: fault == "proof-denied"}
			ctx := snapshotAppendIndexTransport(t, h.ctx, st, observer, proof)
			n, err := st.ExecuteUpdate(ctx)
			wantSuccess := fault == "" || fault == "row-lost" || fault == "proof-denied" && !store.flexEnabled()
			if wantSuccess {
				if err != nil || n != 1 {
					t.Fatalf("indexed append %d %v", n, err)
				}
				h.expectRows("SELECT id FROM t ORDER BY id", "0", "9007199254740993")
			} else {
				if err == nil || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
					t.Fatalf("failed append published: %d %v", n, err)
				}
				h.expectRows("SELECT id FROM t", "0")
			}
			if observer.notifications.Load() != 0 {
				t.Fatal("indexed append repeated row notifications")
			}
			if fault == "create-denied" || fault == "create-lost" {
				if observer.creates.Load() != 1 || input.calls.Load() != 0 || observer.rows.Load() != 0 {
					t.Fatalf("failed creation retried or consumed input: %+v next=%d", observer, input.calls.Load())
				}
			} else if observer.rows.Load() != 1 || input.batches.Load() != 1 || input.calls.Load() != 2 {
				t.Fatalf("row/input replayed: %+v batches=%d next=%d", observer, input.batches.Load(), input.calls.Load())
			}
		})
	}
}
