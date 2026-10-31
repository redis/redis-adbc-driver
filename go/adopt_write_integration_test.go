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
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestAdoptKeyedWrites(t *testing.T) {
	f := newHashFixture(t, "it:aw:users:")
	h := f.h
	const table = "it_aw_users"
	f.dropAdopted(table)
	t.Cleanup(func() { f.dropAdopted(table) })
	f.hset(f.prefix+"seed", "name", "Seed", "active", "YES")
	f.hset(f.prefix+"seed:prefs", "theme", "dark")
	f.adopt(HashAdoptOptions{Table: table, Apply: true})
	h.expectReturning(`INSERT INTO it_aw_users (__key,name,active) VALUES ('it:aw:users:new','New',false) RETURNING __key,*`, "__key|name|active", "it:aw:users:new|New|false")
	raw, err := f.raw.HGetAll(h.ctx, f.prefix+"new").Result()
	if err != nil || len(raw) != 2 || raw["name"] != "New" || raw["active"] != "NO" {
		t.Fatalf("stored fields: %v, %v", raw, err)
	}
	h.expectRows(`SELECT name,active FROM it_aw_users WHERE __key = 'it:aw:users:new'`, "New|false")
	h.expectReturning(`INSERT INTO it_aw_users (__key,name) SELECT 'it:aw:users:select','Selected' RETURNING __key,name`, "__key|name", "it:aw:users:select|Selected")
	for _, sql := range []string{
		`INSERT INTO it_aw_users (name) VALUES ('Missing')`,
		`INSERT INTO it_aw_users (__key,name) VALUES (NULL,'Null')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('other:bad','Outside')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:seed:prefs','Nested')`,
		`INSERT INTO it_aw_users (__key) VALUES ('it:aw:users:empty')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:dupe','A'),('it:aw:users:dupe','B')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:before','Before'),('it:aw:users:seed','Overwrite')`,
	} {
		h.expectError(sql, "")
	}
	for _, suffix := range []string{"before", "dupe", "empty"} {
		key := f.prefix + suffix
		n, err := f.raw.Exists(h.ctx, key).Result()
		if err != nil {
			t.Fatalf("check refused batch key %q: %v", key, err)
		}
		if n != 0 {
			t.Fatalf("refused batch wrote key %q", key)
		}
	}
	if v, _ := f.raw.HGet(h.ctx, f.prefix+"seed", "name").Result(); v != "Seed" {
		t.Fatal("overwrote existing row")
	}
	if err := f.raw.Set(h.ctx, f.prefix+"string", "keep", 0).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError(`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:string','Wrong type')`, "already exists")
	// Prepared parameters recognize __key as an input, too.
	types, err := h.paramTypes(`INSERT INTO it_aw_users (__key,name) VALUES ($1,$2)`)
	if err != nil || len(types) != 2 || types[0] != "utf8" {
		t.Fatalf("parameter types: %v %v", types, err)
	}
	h.exec(`MERGE INTO it_aw_users t USING (SELECT 'it:aw:users:merge' AS k,'Merged' AS n) s ON t.name=s.n WHEN NOT MATCHED THEN INSERT (__key,name) VALUES (s.k,s.n)`)
	h.expectRows(`SELECT name FROM it_aw_users WHERE __key='it:aw:users:merge'`, "Merged")
	// Arrow append uses the same keyed writer and collision protection.
	ingest := func(key, mode string) error {
		schema := arrow.NewSchema([]arrow.Field{{Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "name", Type: arrow.BinaryTypes.String}}, nil)
		b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		defer b.Release()
		b.Field(0).(*array.StringBuilder).Append(key)
		b.Field(1).(*array.StringBuilder).Append("Arrow")
		rec := b.NewRecordBatch()
		defer rec.Release()
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			return err
		}
		defer st.Close(h.ctx)
		for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
			if err := st.SetOption(h.ctx, k, v); err != nil {
				return err
			}
		}
		if err := st.Bind(h.ctx, rec); err != nil {
			return err
		}
		_, err = st.ExecuteUpdate(h.ctx)
		return err
	}
	if err := ingest(f.prefix+"arrow", adbc.OptionValueIngestModeAppend); err != nil {
		t.Fatal(err)
	}
	if err := ingest(f.prefix+"arrow", adbc.OptionValueIngestModeAppend); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate ingest: %v", err)
	}
	h.expectRows(`SELECT name FROM it_aw_users WHERE __key='it:aw:users:arrow'`, "Arrow")
	if err := ingest(f.prefix+"createappend", adbc.OptionValueIngestModeCreateAppend); err != nil {
		t.Fatal(err)
	}
	if err := ingest(f.prefix+"replace", adbc.OptionValueIngestModeReplace); err == nil {
		t.Fatal("accepted replace with __key")
	}
	h.expectRows(`SELECT name FROM it_aw_users WHERE __key='it:aw:users:createappend'`, "Arrow")
	h.expectError(`TRUNCATE it_aw_users`, "TRUNCATE isn't supported")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionStringTruncateAdopted, "yes"); err == nil {
		t.Fatal("accepted invalid option")
	}
	if err := st.SetOption(h.ctx, OptionStringTruncateAdopted, "true"); err != nil {
		t.Fatal(err)
	}
	h.dropTables("it_aw_managed")
	t.Cleanup(func() { h.dropTables("it_aw_managed") })
	h.exec(`CREATE TABLE it_aw_managed (n integer)`)
	h.exec(`INSERT INTO it_aw_managed VALUES (1)`)
	if err := st.SetSqlQuery(h.ctx, `TRUNCATE it_aw_users, it_aw_managed`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err == nil {
		t.Fatal("accepted mixed truncate")
	}
	h.expectRows(`SELECT name FROM it_aw_users WHERE __key='it:aw:users:new'`, "New")
	h.expectRows(`SELECT n FROM it_aw_managed`, "1")
	if err := st.SetSqlQuery(h.ctx, `TRUNCATE it_aw_users RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT COUNT(*) FROM it_aw_users`, "0")
	for _, suffix := range []string{"seed:prefs", "string"} {
		key := f.prefix + suffix
		n, err := f.raw.Exists(h.ctx, key).Result()
		if err != nil {
			t.Fatalf("check preserved key %q: %v", key, err)
		}
		if n != 1 {
			t.Fatalf("truncate deleted protected key %q", key)
		}
	}
	h.exec(`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:after','After')`)
	h.expectRows(`SELECT name FROM it_aw_users`, "After")
}

func TestAdoptInsertCollisionAtomic(t *testing.T) {
	f := newHashFixture(t, "it:aw:race:")
	f.dropAdopted("it_aw_race")
	t.Cleanup(func() { f.dropAdopted("it_aw_race") })
	f.hset(f.prefix+"seed", "name", "Seed")
	f.adopt(HashAdoptOptions{Table: "it_aw_race", Apply: true})
	st := &store{client: f.raw}
	meta, err := st.getTable(f.h.ctx, "public", "it_aw_race")
	if err != nil {
		t.Fatal(err)
	}
	meta = insertMeta(meta)
	rows := [][]Value{{stringValue("Winner"), stringValue(f.prefix + "new")}}
	// Simulate two statements both passing preflight before either writes.
	if _, err := st.allocRows(f.h.ctx, meta, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := st.allocRows(f.h.ctx, meta, rows); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			local := *meta
			_, err := st.writeRows(f.h.ctx, &local, rowAlloc{}, rows)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !strings.Contains(err.Error(), "already exists") {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal(fmt.Sprintf("successful competing inserts: %d", success))
	}
	if n, _ := f.raw.HLen(f.h.ctx, f.prefix+"new").Result(); n != 1 {
		t.Fatalf("unexpected fields: %d", n)
	}
}
