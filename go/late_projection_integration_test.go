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
	"fmt"
	"io"
	"testing"
	"time"
)

func TestSQLLateProjection(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_late")
	h.exec("CREATE TABLE it_late (id INTEGER, v BIGINT, url VARCHAR, payload VARCHAR, spare VARCHAR)")
	h.exec(`INSERT INTO it_late VALUES
 (1,9007199254740993,'google','one','x'),
 (2,9007199254740992,'google','two',NULL),
 (3,9007199254740994,'Google','three','x'),
 (4,9007199254740995,'a%google','four','x'),
 (5,9007199254740996,NULL,'five','x')`)
	for _, off := range []bool{false, true} {
		opts := map[string]string{OptionStringStreamResults: "true"}
		if off {
			opts[OptionStringStreamResults] = "false"
		}
		rdr, _ := h.openStream(h.ctx, h.conn, "SELECT v,payload FROM it_late WHERE url LIKE '%google%' ORDER BY v LIMIT 1 OFFSET 1", nil, opts)
		ids, _, err := readInts(rdr)
		rdr.Release()
		if err != nil || len(ids) != 1 || ids[0] != 9007199254740993 {
			t.Fatalf("stream off=%v: %v %v", off, ids, err)
		}
	}
	// Verify planning and actual cursor payload, rather than only SQL results.
	raw := h.rawClient()
	st := &store{client: raw, snapshots: true}
	if err := addSnapshotHooks(raw)(h.ctx); err != nil {
		t.Fatal(err)
	}
	readCtx, pin, err := st.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	e := &executor{store: st, schema: defaultSchema, cache: newExecCache(), now: time.Now().UTC()}
	parsed, err := ParseScript("SELECT * FROM it_late WHERE url LIKE '%google%' ORDER BY v LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(readCtx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := e.prepareScan(readCtx, plan, nil)
	if err != nil || sc.req.late == nil || sc.req.limit != nil {
		t.Fatalf("late plan: %v %v", sc, err)
	}
	it, err := e.openScan(readCtx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(readCtx)
	cursors := []*scanIter{it}
	if it.ordered != nil {
		cursors = nil
		for _, source := range it.ordered.sources {
			cursors = append(cursors, source.iter)
		}
	}
	if len(cursors) == 0 {
		t.Fatal("late projection has no input cursors")
	}
	for _, source := range cursors {
		if source.cur == nil {
			t.Fatal("late projection bypassed indexed cursor")
		}
		for _, raw := range source.cur.first {
			if _, ok := raw["payload"]; ok {
				t.Fatal("cursor loaded projection")
			}
		}
	}
	it.close(readCtx)
	h.expectRows(`SELECT * FROM it_late WHERE url LIKE '%\%google%' ESCAPE '\' ORDER BY v LIMIT 1`, "4|9007199254740995|a%google|four|x")
	h.expectRows("SELECT * FROM it_late WHERE url ILIKE '%google%' ORDER BY v LIMIT 1 OFFSET 2", "3|9007199254740994|Google|three|x")
	h.expectRows("SELECT * FROM it_late WHERE url LIKE '%google%' ORDER BY v LIMIT 1", "2|9007199254740992|google|two|NULL")
	h.expectRows("SELECT * FROM it_late WHERE url LIKE '%google%' ORDER BY v LIMIT 1 OFFSET 1", "1|9007199254740993|google|one|x")
	h.expectRows("SELECT payload, v FROM it_late WHERE url LIKE '%google%' ORDER BY v DESC LIMIT 2", "four|9007199254740995", "one|9007199254740993")
	h.expectRows("SELECT * FROM it_late WHERE url LIKE '%absent%' ORDER BY v LIMIT 10")
	h.expectRows("SELECT payload FROM it_late WHERE url LIKE '%google%' ORDER BY id LIMIT 10 OFFSET 9")
	h.expectRows("SELECT payload FROM it_late WHERE url LIKE '%google%' ORDER BY id LIMIT 0")
	h.expectRows("SELECT payload FROM it_late WHERE url LIKE '%google%' ORDER BY id LIMIT 10", "one", "two", "four")
	h.dropTables("it_late_ns")
	h.exec("CREATE TABLE it_late_ns (id INTEGER, ts TIMESTAMP(9), url VARCHAR, payload VARCHAR)")
	h.exec("INSERT INTO it_late_ns VALUES (1,TIMESTAMP '2026-10-05 12:00:00.000000002','google','a'),(2,TIMESTAMP '2026-10-05 12:00:00.000000001','google','b'),(3,TIMESTAMP '2026-10-05 12:00:00.000000003','absent','c')")
	h.expectRows("SELECT id,payload FROM it_late_ns WHERE url LIKE '%google%' ORDER BY ts LIMIT 1 OFFSET 1", "1|a")
	h.expectRows("SELECT id,payload FROM it_late_ns WHERE url LIKE '%google%' ORDER BY ts DESC LIMIT 1 OFFSET 1", "2|b")
}

func TestLateProjectionDeletedAndCancelled(t *testing.T) {
	h := newSQLHarness(t)
	st := &store{client: h.rawClient()}
	// Exercise missing-HASH refill on an explicitly owned, unregistered source.
	// Deleting a published managed HASH would violate immutable snapshot storage.
	id := snapshotID()
	meta := &tableMeta{
		Schema: defaultSchema, Name: "late_primitive",
		KeyPrefix: "late_primitive:{meta}:" + id + ":", IndexName: "late_primitive_" + id,
		Columns: []columnMeta{{Name: "id", Type: typeInt32, Indexed: true},
			{Name: "payload", Type: typeString, Indexed: true}, {Name: "spare", Type: typeString, Indexed: true}},
	}
	if err := st.client.Do(h.ctx, indexCreateArgs(meta)...).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.client.Do(h.ctx, "FT.DROPINDEX", meta.index(), "DD").Err() })
	for i, payload := range []string{"one", "two", "three"} {
		if err := st.client.HSet(h.ctx, meta.prefix()+fmt.Sprint(i+1), rowIDField, i+1, "id", i+1, "payload", payload, "spare", "x").Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.awaitIndexReady(h.ctx, meta.index()); err != nil {
		t.Fatal(err)
	}
	e := &executor{store: st}
	req := scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{"id": true}, sortBy: []sortKey{{field: "id"}}, late: &lateProjection{need: map[string]bool{"payload": true}, fields: []string{"payload"}, limit: 2}}
	it, err := e.openScan(h.ctx, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(h.ctx)
	for _, raw := range it.cur.first {
		if _, ok := raw["payload"]; ok {
			t.Fatal("wide cursor")
		}
	}
	keys, _, err := it.nextPage(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.client.Del(h.ctx, keys[0]).Err(); err != nil {
		t.Fatal(err)
	}
	_, got, err := it.materialize(h.ctx, keys)
	if err != nil || len(got) != 2 {
		t.Fatalf("refill: %v %v", got, err)
	}
	if got[0]["payload"].S != "two" || got[1]["payload"].S != "three" {
		t.Fatalf("refill returned wrong surviving payloads: %v", got)
	}
	if _, _, err = it.next(h.ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("LIMIT: %v", err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	it.left = 1
	if _, _, err := it.materialize(ctx, keys[1:]); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestSnapshotLateProjectionRetainsDeletedRows(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE late_snapshot(id INTEGER,v BIGINT,url VARCHAR,payload VARCHAR,spare VARCHAR)")
	h.exec("INSERT INTO late_snapshot VALUES (1,9007199254740993,'google','one','x'),(2,9007199254740994,'google',NULL,'y'),(3,9007199254740995,'google','three','z')")
	st := snapshotTestStore(t, raw)
	ctx, pin, err := st.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	e := &executor{store: st, schema: defaultSchema, cache: newExecCache(), now: time.Now().UTC()}
	parsed, err := ParseScript("SELECT * FROM late_snapshot WHERE url LIKE '%google%' ORDER BY v LIMIT 2")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := e.prepareScan(ctx, plan, nil)
	if err != nil || sc.req.late == nil {
		t.Fatalf("expected late projection: %v", err)
	}
	it, err := e.openScan(ctx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	// No projection HASH fetch has happened. All following mutations publish
	// fresh generations rather than deleting or changing the pinned images.
	h.exec("DELETE FROM late_snapshot WHERE id=1")
	h.exec("UPDATE late_snapshot SET payload='changed' WHERE id=2")
	h.expectRows("SELECT id,payload FROM late_snapshot ORDER BY id", "2|changed", "3|three")
	h.exec("TRUNCATE late_snapshot")
	h.exec("DROP TABLE late_snapshot")
	st.reapSnapshot(h.ctx)
	var rows []map[string]Value
	for {
		_, page, err := it.next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, page...)
	}
	if len(rows) != 2 || rows[0]["id"].I != 1 || rows[1]["id"].I != 2 ||
		rows[0]["v"].I != 9007199254740993 || rows[1]["v"].I != 9007199254740994 ||
		rows[0]["payload"].S != "one" || !rows[1]["payload"].Null {
		t.Fatalf("pinned late projection lost original values/limit: %v", rows)
	}
}
