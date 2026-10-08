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
		rdr, _ := h.openStream(h.ctx, h.conn, "SELECT v FROM it_late WHERE url LIKE '%google%' ORDER BY v LIMIT 1 OFFSET 1", nil, opts)
		ids, _, err := readInts(rdr)
		rdr.Release()
		if err != nil || len(ids) != 1 || ids[0] != 9007199254740993 {
			t.Fatalf("stream off=%v: %v %v", off, ids, err)
		}
	}
	// Verify planning and actual cursor payload, rather than only SQL results.
	st := &store{client: h.rawClient()}
	e := &executor{store: st, schema: defaultSchema, cache: newExecCache(), now: time.Now().UTC()}
	parsed, err := ParseScript("SELECT * FROM it_late WHERE url LIKE '%google%' ORDER BY v LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(h.ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := e.prepareScan(h.ctx, plan, nil)
	if err != nil || sc.req.late == nil || sc.req.limit != nil {
		t.Fatalf("late plan: %v %v", sc, err)
	}
	it, err := e.openScan(h.ctx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range it.cur.first {
		if _, ok := raw["payload"]; ok {
			t.Fatal("cursor loaded projection")
		}
	}
	it.close(h.ctx)
	h.expectRows(`SELECT * FROM it_late WHERE url LIKE '%\%google%' ESCAPE '\' ORDER BY v LIMIT 1`, "4|9007199254740995|a%google|four|x")
	h.expectRows("SELECT * FROM it_late WHERE url ILIKE '%google%' ORDER BY v LIMIT 1 OFFSET 2", "3|9007199254740994|Google|three|x")
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
	h.dropTables("it_late_deleted")
	h.exec("CREATE TABLE it_late_deleted (id INTEGER, payload VARCHAR, spare VARCHAR)")
	h.exec("INSERT INTO it_late_deleted VALUES (1,'one','x'),(2,'two','x'),(3,'three','x')")
	st := &store{client: h.rawClient()}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_late_deleted")
	if err != nil {
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
	for _, raw := range it.cur.first {
		if _, ok := raw["payload"]; ok {
			t.Fatal("wide cursor")
		}
	}
	if err := st.client.Del(h.ctx, keys[0]).Err(); err != nil {
		t.Fatal(err)
	}
	_, got, err := it.materialize(h.ctx, keys)
	if err != nil || len(got) != 2 {
		t.Fatalf("refill: %v %v", got, err)
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
