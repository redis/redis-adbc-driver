// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// Close interrupts private snapshot rewrites and preserves a committed rename.
// Uncooperative work remains fenced after the caller's bounded Close deadline.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func rekeyCloseFixture(t *testing.T, from, to string) (*sqlHarness, goredis.UniversalClient, *tableMeta, string) {
	t.Helper()
	h := newSQLHarness(t)
	h.dropTables(from, to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " SELECT i FROM generate_series(1,600) AS g(i)")
	raw := h.rawClient()
	return h, raw, h.inspectTable(raw, defaultSchema, from), h.rawMeta(raw, defaultSchema, from)
}

func expectRekeyExactRows(t *testing.T, h *sqlHarness, table string, count int) {
	t.Helper()
	rows, _ := h.query("SELECT id FROM " + table + " ORDER BY id")
	if len(rows) != count {
		t.Fatal("incomplete rekey rows", len(rows), count)
	}
	for i, row := range rows {
		if row != strconv.Itoa(i+1) {
			t.Fatal("wrong rekey row", i, row)
		}
	}
}

func pausedCloseRekey(t *testing.T, h *sqlHarness, from, to, prefix string, ignoreClose bool) (*sqlHarness, *snapshotScope, *snapshotAlterReadBarrier, func(), <-chan error) {
	t.Helper()
	closing := openWith(t, os.Getenv("REDIS_URI"), map[string]string{OptionStringRenameRekey: "true"})
	stmt, ctx, _, transport, barrier := snapshotPausedAlter(t, closing, "ALTER TABLE "+from+" RENAME TO "+to, prefix)
	barrier.ignoreClose = ignoreClose
	var once sync.Once
	resume := func() { once.Do(func() { close(barrier.resume) }) }
	t.Cleanup(resume)
	done := make(chan error, 1)
	go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
	scope := snapshotAwaitAlterBatch(t, h, h.rawClient(), barrier, done)
	return closing, scope, barrier, resume, done
}

func rekeyPrivateOutput(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, scope *snapshotScope) (string, string) {
	t.Helper()
	scope.mu.Lock()
	var ids []string
	for id := range scope.owned {
		ids = append(ids, id)
	}
	scope.mu.Unlock()
	if len(ids) != 1 {
		t.Fatal("expected one private rekey output", len(ids))
	}
	data := ids[0]
	payload, err := raw.HGet(h.ctx, snapshotMetadataGraph, "d:"+data).Bytes()
	var owned struct {
		Prefix string `json:"prefix"`
	}
	if err != nil || json.Unmarshal(payload, &owned) != nil || owned.Prefix != snapshotRows(data, defaultSchema, snapshotAccessID(owned.Prefix)) {
		t.Fatal("private rekey ownership missing", err)
	}
	if n, err := raw.ZCard(h.ctx, snapshotMembers(data)).Result(); err != nil || n != snapshotDeltaPage {
		t.Fatal("private batch directory is not exact", n, err)
	}
	return owned.Prefix, snapshotIndexPrefix(data) + "working"
}

func expectClosedRename(t *testing.T, err error) {
	t.Helper()
	if !isStatus(err, adbc.StatusInvalidState) || !strings.Contains(err.Error(), "closed") {
		t.Errorf("rename after Close: %v", err)
	}
}

func TestSQLRekeyCloseDuringMove(t *testing.T) {
	const from, to = "it_rk_cm", "it_rk_cm2"
	h, raw, old, before := rekeyCloseFixture(t, from, to)
	closing, scope, _, _, done := pausedCloseRekey(t, h, from, to, old.prefix(), false)
	prefix, index := rekeyPrivateOutput(t, h, raw, scope)
	closed := make(chan error, 1)
	began := time.Now()
	go func() { closed <- closing.conn.Close(context.Background()) }()
	expectClosedRename(t, snapshotAwaitAlterError(t, done))
	if err := snapshotAwaitAlterError(t, closed); err != nil {
		t.Fatal("Close", err)
	}
	if time.Since(began) > 5*time.Second {
		t.Fatal("Close did not join interrupted rekey")
	}
	snapshotExpectAlterLeaseReleased(t, h, raw, scope)
	h.expectRolledBack(raw, from, before)
	h.expectSnapshotReleased(raw, prefix, index)
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("aborted target became public", err)
	}
	expectRekeyExactRows(t, h, from, 600)
	h.exec("INSERT INTO " + from + " VALUES (601)")
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE " + from + " RENAME TO " + to)
	expectRekeyExactRows(t, h, to, 601)
	for _, segment := range old.snapshotSegments() {
		h.expectSnapshotReleased(raw, segment.Prefix, segment.Index)
	}
}

// Pause only after Redis has committed the public root. Returning a lost reply
// after Close must resolve that durable outcome without rerunning the rewrite.
type rekeyCommittedReply struct {
	snapshotHook
	lose    bool
	commits atomic.Int64
	ready   chan context.Context
	resume  chan struct{}
}

func (h *rekeyCommittedReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		args := snapshotObservedControlArgs(cmd)
		if err != nil || len(args) < 4 || args[1] != snapshotControl {
			return err
		}
		n, parseErr := strconv.Atoi(fmt.Sprint(args[2]))
		if parseErr != nil || 3+n >= len(args) || args[3+n] != "publish" {
			return err
		}
		val, ok := cmd.(interface{ Val() any })
		if !ok || val.Val() != int64(1) {
			return err
		}
		if h.commits.Add(1) != 1 {
			return err
		}
		h.ready <- ctx
		select {
		case <-ctx.Done():
		case <-h.resume:
		}
		if h.lose {
			cmd.SetErr(io.ErrUnexpectedEOF)
			return io.ErrUnexpectedEOF
		}
		return nil
	}
}

func TestSQLRekeyCloseAfterSwitch(t *testing.T) {
	for _, lose := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_reply_%t", lose), func(t *testing.T) {
			const from, to = "it_rk_cs", "it_rk_cs2"
			h, raw, old, _ := rekeyCloseFixture(t, from, to)
			source, oldCtx, oldPin := h.inspectionSnapshot(raw)
			defer oldPin.release()
			oldKeys := []string{old.prefix() + "1", old.prefix() + "600"}
			oldRows, err := source.fetchSnapshotVisible(oldCtx, old, oldKeys, nil)
			if err != nil {
				t.Fatal(err)
			}
			closing := openWith(t, os.Getenv("REDIS_URI"), map[string]string{OptionStringRenameRekey: "true"})
			hook := &rekeyCommittedReply{lose: lose, ready: make(chan context.Context, 1), resume: make(chan struct{})}
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(hook.resume) }) })
			stmt, err := closing.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			impl := stmt.(*statementImpl).conn
			if len(impl.store.activeExecutions()) != 0 {
				t.Fatal("publication hook requires an idle connection")
			}
			// The compactor owns a separate client; install before a statement executes.
			impl.store.client.AddHook(hook)
			t.Cleanup(func() { _ = stmt.Close(h.ctx) })
			if err := stmt.SetSqlQuery(h.ctx, "ALTER TABLE "+from+" RENAME TO "+to); err != nil {
				t.Fatal(err)
			}
			base, cancel := context.WithTimeout(h.ctx, 10*time.Second)
			defer cancel()
			ctx, transport := impl.executionContext(base, &stmt.(*statementImpl).streams, &impl.streams)
			counter := &snapshotAlterReadCounter{prefix: old.prefix()}
			transport.client.AddHook(counter)
			t.Cleanup(transport.finish)
			done := make(chan error, 1)
			go func() { _, err := stmt.ExecuteUpdate(ctx); transport.finish(); done <- err }()
			var scope *snapshotScope
			select {
			case pubCtx := <-hook.ready:
				scope = snapshotOf(pubCtx)
			case err := <-done:
				t.Fatalf("rename did not reach committed reply: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("rename did not commit")
			}
			if scope == nil {
				t.Fatal("committed publication had no snapshot scope")
			}
			target := h.inspectTable(raw, defaultSchema, to)
			if target.prefix() == old.prefix() || target.index() == old.index() {
				t.Fatal("rekey reused source storage")
			}
			if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, from); !isStatus(err, adbc.StatusNotFound) {
				t.Fatal("both names remained public", err)
			}
			expectRekeyExactRows(t, h, to, 600)
			closed := make(chan error, 1)
			go func() { closed <- closing.conn.Close(context.Background()) }()
			if err := snapshotAwaitAlterError(t, done); err != nil {
				t.Fatal("committed rename was downgraded", err)
			}
			if err := snapshotAwaitAlterError(t, closed); err != nil {
				t.Fatal("Close", err)
			}
			if hook.commits.Load() != 1 || counter.rows.Load() != 600 || counter.pages.Load() != 3 {
				t.Fatal("rename replayed or skipped source", hook.commits.Load(), counter.rows.Load(), counter.pages.Load())
			}
			if exists, err := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+scope.pin.token).Result(); err != nil || exists {
				t.Fatal("committed rekey retained reader lease", err)
			}
			// A committed outcome record is durable evidence, not active private work.
			payload, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
			if err != goredis.Nil {
				var work struct {
					State string `json:"state"`
				}
				if err != nil || json.Unmarshal(payload, &work) != nil || work.State != "committed" {
					t.Fatal("committed rename retained active writer state", err)
				}
			}
			rows, err := source.fetchSnapshotVisible(oldCtx, old, oldKeys, nil)
			if err != nil || len(rows) != 2 || rows[0]["id"] != oldRows[0]["id"] || rows[1]["id"] != oldRows[1]["id"] {
				t.Fatal("committed rekey changed pinned source", err)
			}
			oldPin.release()
			for _, segment := range old.snapshotSegments() {
				h.expectSnapshotReleased(raw, segment.Prefix, segment.Index)
			}
			h.exec("INSERT INTO " + to + " VALUES (601)")
			expectRekeyExactRows(t, h, to, 601)
		})
	}
}

func expireRekeyControl(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, field string) {
	t.Helper()
	// The heartbeat may already have released the reader. Expire atomically so
	// that this fixture never recreates an already released control record.
	const script = `local raw=redis.call('HGET',KEYS[1],ARGV[1]);if not raw then return 0 end;local v=cjson.decode(raw);v.expires=0;redis.call('HSET',KEYS[1],ARGV[1],cjson.encode(v));return 1`
	n, err := raw.Eval(h.ctx, script, []string{snapshotCatalogKey}, field).Int64()
	if err != nil || n == 0 && strings.HasPrefix(field, "w:") {
		t.Fatal("private control record unavailable", err)
	}
}

func TestSQLRekeyCloseGivesUp(t *testing.T) {
	const from, to = "it_rk_cg", "it_rk_cg2"
	h, raw, old, before := rekeyCloseFixture(t, from, to)
	closing, scope, barrier, resume, done := pausedCloseRekey(t, h, from, to, old.prefix(), true)
	prefix, index := rekeyPrivateOutput(t, h, raw, scope)
	closeCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	began := time.Now()
	err := closing.conn.Close(closeCtx)
	if err == nil || !strings.Contains(err.Error(), "active execution cleanup did not finish before Close") {
		t.Fatal("Close did not report bounded incomplete cleanup", err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("uncooperative rewrite exceeded Close bound")
	}
	if n := len(scope.store.activeExecutions()); n != 1 {
		t.Fatal("Close hid an unfinished execution", n)
	}
	select {
	case err := <-done:
		t.Fatalf("uncooperative rewrite stopped before release: %v", err)
	default:
	}
	// Simulate lease time passing on owned control records; no published data is edited.
	expireRekeyControl(t, h, raw, "w:"+scope.id)
	expireRekeyControl(t, h, raw, "p:"+scope.pin.token)
	snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
	h.expectSnapshotReleased(raw, prefix, index)
	h.expectRolledBack(raw, from, before)
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("partial target became public", err)
	}
	fresh := newSQLHarness(t)
	fresh.exec("INSERT INTO " + from + " VALUES (601)")
	resume()
	expectClosedRename(t, snapshotAwaitAlterError(t, done))
	if n := len(scope.store.activeExecutions()); n != 0 {
		t.Fatal("completed execution remained registered", n)
	}
	if barrier.pages.Load() != 2 {
		t.Fatal("closed rekey continued reading source", barrier.pages.Load())
	}
	snapshotExpectAlterLeaseReleased(t, h, raw, scope)
	h.expectSnapshotReleased(raw, prefix, index)
	expectRekeyExactRows(t, h, from, 601)
}
