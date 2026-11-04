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

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func TestSQLNarrowExactScan(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_narrow")
	h.exec("CREATE TABLE it_narrow (id INTEGER, v BIGINT, pad VARCHAR)")
	h.exec(`INSERT INTO it_narrow VALUES
        (1, 9007199254740993, 'a'), (2, 9007199254740992, 'b'),
        (3, -9223372036854775808, 'c'), (4, 9223372036854775807, 'd'), (5, NULL, 'e')`)
	h.expectRows("SELECT v FROM it_narrow ORDER BY id", "9007199254740993", "9007199254740992", "-9223372036854775808", "9223372036854775807", "NULL")
	h.expectRows("SELECT COUNT(DISTINCT v) FROM it_narrow", "4")
	h.expectRows("SELECT v, COUNT(*) FROM it_narrow GROUP BY v ORDER BY v", "-9223372036854775808|1", "9007199254740992|1", "9007199254740993|1", "9223372036854775807|1", "NULL|1")
	h.expectRows("SELECT v FROM it_narrow WHERE v = 9007199254740993", "9007199254740993")
	h.expectRows("SELECT v FROM it_narrow ORDER BY v LIMIT 1 OFFSET 1", "9007199254740992")

	h.exec("ALTER TABLE it_narrow ADD COLUMN later BIGINT DEFAULT 9007199254740993")
	h.exec("UPDATE it_narrow SET later = NULL WHERE id = 2")
	h.expectRows("SELECT later FROM it_narrow ORDER BY id", "9007199254740993", "NULL", "9007199254740993", "9007199254740993", "9007199254740993")
	h.exec("ALTER TABLE it_narrow RENAME COLUMN later TO renamed")
	h.expectRows("SELECT renamed FROM it_narrow WHERE id = 2", "NULL")

	raw := h.rawClient()
	pinned, readCtx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	meta, err := pinned.getTable(readCtx, defaultSchema, "it_narrow")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := (&executor{store: pinned}).openScan(readCtx, scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{"id": true, "v": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer managed.close(readCtx)
	h.exec("DELETE FROM it_narrow WHERE id=1")
	h.expectRows("SELECT COUNT(*) FROM it_narrow", "4")
	pinned.reapSnapshot(h.ctx)
	seen := map[int64]Value{}
	for {
		_, rows, err := managed.next(readCtx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			id := row["id"].I
			if _, duplicate := seen[id]; duplicate {
				t.Fatal("duplicate pinned narrow row", id)
			}
			seen[id] = row["v"]
		}
	}
	if len(seen) != 5 || seen[1].I != 9007199254740993 || seen[2].I != 9007199254740992 ||
		seen[3].I != -9223372036854775808 || seen[4].I != 9223372036854775807 || !seen[5].Null {
		t.Fatalf("pinned narrow scan lost exact values/NULL/deleted row: %v", seen)
	}
	pin.release()
	// Missing-HASH refill is a mutable-source primitive. Give it its own
	// unregistered HASHes/index rather than corrupting a published managed row.
	primitive := *meta
	id := snapshotID()
	primitive.KeyPrefix, primitive.IndexName = "narrow_primitive:{meta}:"+id+":", "narrow_primitive_"+id
	primitive.SnapshotOverlay, primitive.SnapshotSegments, primitive.SnapshotRows = false, nil, nil
	meta = &primitive
	st := &store{client: raw}
	if err := raw.Do(h.ctx, indexCreateArgs(meta)...).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Do(h.ctx, "FT.DROPINDEX", meta.index(), "DD").Err() })
	for i, v := range []string{"9007199254740993", "9007199254740992", "-9223372036854775808", "9223372036854775807", ""} {
		key := meta.prefix() + strconv.Itoa(i+1)
		if err := raw.HSet(h.ctx, key, rowIDField, i+1, "id", i+1, "pad", "x").Err(); err != nil {
			t.Fatal(err)
		}
		if v != "" {
			if err := raw.HSet(h.ctx, key, "v", v).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.awaitIndexReady(h.ctx, meta.index()); err != nil {
		t.Fatal(err)
	}
	e := &executor{store: st}
	it, err := e.openScan(h.ctx, scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{"id": true, "v": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(h.ctx)
	if len(it.fetch) != 1 || it.fetch[0] != "v" {
		t.Fatalf("fetch = %v", it.fetch)
	}
	for _, r := range it.cur.first {
		if _, ok := r["pad"]; ok {
			t.Fatal("cursor loaded an unneeded column")
		}
		if _, ok := r["id"]; !ok {
			t.Fatal("cursor did not load the exact-safe column")
		}
		if _, ok := r["v"]; ok {
			t.Fatal("cursor loaded the unsafe numeric column")
		}
	}
	// The initial page has matched these rows; delete one before HMGET.
	first := it.cur.first[0]["__key"]
	if err := st.client.Del(h.ctx, first).Err(); err != nil {
		t.Fatal(err)
	}
	var n int
	for {
		keys, _, err := it.next(h.ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range keys {
			if k == first {
				t.Fatal("returned deleted row")
			}
		}
		n += len(keys)
	}
	if n != 4 {
		t.Fatalf("read %d rows, want 4", n)
	}
}

func TestNarrowFetchCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&store{}).fetchRows(ctx, []string{"unused"}, []string{"v"})
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusCancelled {
		t.Fatalf("got %v, want CANCELLED", err)
	}
}

type cancelFetchHook struct {
	cancel context.CancelFunc
	calls  int
	t      *testing.T
}

func (h *cancelFetchHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *cancelFetchHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *cancelFetchHook) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		h.calls++
		if len(cmds) > pipelineChunk {
			h.t.Fatalf("pipeline contains %d commands", len(cmds))
		}
		for _, cmd := range cmds {
			cmd.(*goredis.SliceCmd).SetVal([]any{"1", "9007199254740993"})
		}
		h.cancel()
		return nil
	}
}

func TestNarrowFetchCancelledBetweenBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := goredis.NewClient(&goredis.Options{})
	defer client.Close()
	hook := &cancelFetchHook{cancel: cancel, t: t}
	client.AddHook(hook)
	keys := make([]string, pipelineChunk+1)
	for i := range keys {
		keys[i] = "row"
	}
	_, err := (&store{client: client}).fetchRows(ctx, keys, []string{"v"})
	if !isStatus(err, adbc.StatusCancelled) || hook.calls != 1 {
		t.Fatalf("got %v, %d pipelines; want CANCELLED after one pipeline", err, hook.calls)
	}
}
