// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Pause after a real second source page has been read. The first page has
// already been converted and acknowledged as a private atomic row batch.
type snapshotAlterReadBarrier struct {
	snapshotHook
	prefix      string
	pages       atomic.Int64
	ready       chan context.Context
	resume      chan struct{}
	ignoreClose bool
}

func (h *snapshotAlterReadBarrier) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		source := false
		for _, cmd := range cmds {
			args := cmd.Args()
			if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "HGETALL") && strings.HasPrefix(fmt.Sprint(args[1]), h.prefix) {
				source = true
			}
		}
		err := next(ctx, cmds)
		if err == nil && source && h.pages.Add(1) == 2 {
			h.ready <- ctx
			if h.ignoreClose {
				<-h.resume
			} else {
				select {
				case <-ctx.Done():
				case <-h.resume:
				}
			}
			// Return the successful read result; production code must notice the real
			// cancellation/Close and preserve its cause rather than an injected error.
		}
		return err
	}
}

func snapshotPausedAlter(t *testing.T, h *sqlHarness, sql, prefix string, timeout ...time.Duration) (adbc.StatementWithContext, context.Context, context.CancelFunc, *executionTransport, *snapshotAlterReadBarrier) {
	t.Helper()
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stmt.SetSqlQuery(h.ctx, sql); err != nil {
		t.Fatal(err)
	}
	impl := stmt.(*statementImpl)
	limit := 10 * time.Second
	if len(timeout) > 0 {
		limit = timeout[0]
	}
	base, cancel := context.WithTimeout(h.ctx, limit)
	ctx, transport := impl.conn.executionContext(base, &impl.streams, &impl.conn.streams)
	barrier := &snapshotAlterReadBarrier{prefix: prefix, ready: make(chan context.Context, 1), resume: make(chan struct{})}
	transport.client.AddHook(barrier) // before the execution starts using its client
	t.Cleanup(func() { cancel(); transport.finish(); _ = stmt.Close(h.ctx) })
	return stmt, ctx, cancel, transport, barrier
}

func snapshotAwaitAlterBatch(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, b *snapshotAlterReadBarrier, done <-chan error) *snapshotScope {
	t.Helper()
	var ctx context.Context
	select {
	case ctx = <-b.ready:
	case err := <-done:
		t.Fatalf("ALTER did not pause during a real rewrite: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("ALTER never read its second source page")
	}
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write {
		t.Fatal("rewrite was not running in a private generation")
	}
	data, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
	var work struct {
		Batch struct {
			Rows int `json:"rows"`
		} `json:"row_batch"`
	}
	if err != nil || json.Unmarshal(data, &work) != nil || work.Batch.Rows != snapshotDeltaPage {
		t.Fatal("first private row batch was not durably acknowledged", err)
	}
	return scope
}

func snapshotAwaitAlterError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("interrupted ALTER did not stop promptly")
		return nil
	}
}

func snapshotExpectAlterLeaseReleased(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, scope *snapshotScope) {
	t.Helper()
	if exists, err := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+scope.pin.token).Result(); err != nil || exists {
		t.Errorf("interrupted ALTER retained its reader lease: %v", err)
	}
	data, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
	if err == goredis.Nil {
		return
	}
	var work struct {
		State string `json:"state"`
	}
	if err != nil || json.Unmarshal(data, &work) != nil || work.State != "deleting" {
		t.Errorf("interrupted ALTER retained active private work: %s %v", work.State, err)
	}
}

type snapshotAlterReadCounter struct {
	snapshotHook
	prefix      string
	pages, rows atomic.Int64
}

func (h *snapshotAlterReadCounter) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		if err != nil {
			return err
		}
		var rows int64
		for _, cmd := range cmds {
			args := cmd.Args()
			if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "HGETALL") && strings.HasPrefix(fmt.Sprint(args[1]), h.prefix) {
				rows++
			}
		}
		if rows > 0 {
			h.pages.Add(1)
			h.rows.Add(rows)
		}
		return nil
	}
}
func snapshotCountedAlter(t *testing.T, h *sqlHarness, sql, prefix string) (int64, int64) {
	t.Helper()
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	if err := stmt.SetSqlQuery(h.ctx, sql); err != nil {
		t.Fatal(err)
	}
	impl := stmt.(*statementImpl)
	ctx, transport := impl.conn.executionContext(h.ctx, &impl.streams, &impl.conn.streams)
	defer transport.finish()
	counter := &snapshotAlterReadCounter{prefix: prefix}
	transport.client.AddHook(counter)
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	return counter.pages.Load(), counter.rows.Load()
}
