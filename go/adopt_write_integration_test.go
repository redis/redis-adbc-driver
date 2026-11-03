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

// Imported __key is captured provenance. New SQL/Arrow rows have independent
// managed IDs and NULL provenance; all writes leave application objects alone.
func TestAdoptKeyedWrites(t *testing.T) {
	f := newHashFixture(t, "it:aw:users:")
	h := f.h
	const table = "it_aw_users"
	f.dropAdopted(table)
	t.Cleanup(func() { f.dropAdopted(table) })
	f.hset(f.prefix+"seed", "name", "Seed", "active", "YES")
	f.hset(f.prefix+"seed:prefs", "theme", "dark")
	if err := f.raw.Set(h.ctx, f.prefix+"string", "keep", 0).Err(); err != nil {
		t.Fatal(err)
	}
	f.adopt(HashAdoptOptions{Table: table, Apply: true})
	h.expectReturning(`INSERT INTO it_aw_users (name,active) VALUES ('New',false) RETURNING __key,*`, "__key|name|active", "NULL|New|false")
	h.expectReturning(`INSERT INTO it_aw_users (name) SELECT 'Selected' RETURNING __key,name`, "__key|name", "NULL|Selected")
	for _, sql := range []string{
		`INSERT INTO it_aw_users (__key,name) VALUES (NULL,'Null')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('other:bad','Outside')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:seed:prefs','Nested')`,
		`INSERT INTO it_aw_users (__key) VALUES ('it:aw:users:empty')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:dupe','A'),('it:aw:users:dupe','B')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:before','Before'),('it:aw:users:seed','Overwrite')`,
		`INSERT INTO it_aw_users (__key,name) VALUES ('it:aw:users:string','Wrong type')`,
		`UPDATE it_aw_users SET __key='it:aw:users:changed' WHERE __key='it:aw:users:seed'`,
		`MERGE INTO it_aw_users t USING (SELECT 'Forbidden' AS n) s ON t.name=s.n WHEN NOT MATCHED THEN INSERT (__key,name) VALUES ('it:aw:users:merge',s.n)`,
	} {
		h.expectError(sql, "__key")
	}
	h.expectRows(`SELECT COUNT(*),COUNT(__key) FROM it_aw_users`, "3|1")
	h.expectRows(`SELECT name FROM it_aw_users WHERE __key='it:aw:users:seed'`, "Seed")
	types, err := h.paramTypes(`INSERT INTO it_aw_users (name,active) VALUES ($1,$2)`)
	if err != nil || len(types) != 2 || types[0] != "utf8" || types[1] != "bool" {
		t.Fatalf("parameter types: %v %v", types, err)
	}
	h.exec(`MERGE INTO it_aw_users t USING (SELECT 'Merged' AS n) s ON t.name=s.n WHEN NOT MATCHED THEN INSERT (name) VALUES (s.n)`)
	h.exec(`MERGE INTO it_aw_users t USING (SELECT 'it:aw:users:seed' AS k) s ON t.__key=s.k WHEN MATCHED THEN UPDATE SET name='Private Seed'`)
	h.expectRows(`SELECT __key,name FROM it_aw_users WHERE name='Merged'`, "NULL|Merged")
	h.expectRows(`SELECT __key,name FROM it_aw_users WHERE name='Private Seed'`, "it:aw:users:seed|Private Seed")
	ingest := func(column, value, mode string) error {
		b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: column, Type: arrow.BinaryTypes.String, Nullable: true}}, nil))
		defer b.Release()
		b.Field(0).(*array.StringBuilder).Append(value)
		rec := b.NewRecordBatch()
		defer rec.Release()
		return h.ingestImportRecord(table, mode, rec)
	}
	for _, mode := range []string{adbc.OptionValueIngestModeAppend, adbc.OptionValueIngestModeCreateAppend, adbc.OptionValueIngestModeReplace} {
		if err := ingest(keyField, f.prefix+"arrow", mode); err == nil {
			t.Fatal("accepted writable provenance in Arrow", mode)
		}
		h.expectRows(`SELECT COUNT(*),COUNT(__key) FROM it_aw_users`, "4|1")
	}
	for range 2 {
		if err := ingest("name", "Arrow", adbc.OptionValueIngestModeAppend); err != nil {
			t.Fatal(err)
		}
	}
	if err := ingest("name", "CreateAppend", adbc.OptionValueIngestModeCreateAppend); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT COUNT(*),COUNT(DISTINCT __rowid),COUNT(__key) FROM it_aw_users`, "7|7|1")
	h.expectRows(`SELECT COUNT(*) FROM it_aw_users WHERE __key IS NULL AND name='Arrow'`, "2")
	h.dropTables("it_aw_managed")
	t.Cleanup(func() { h.dropTables("it_aw_managed") })
	h.exec(`CREATE TABLE it_aw_managed (n integer)`)
	h.exec(`INSERT INTO it_aw_managed VALUES (1)`)
	// The legacy option keeps its strict Boolean parsing, but managed copies
	// require no opt-in; the following TRUNCATE uses a fresh default statement.
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, OptionStringTruncateAdopted, "yes"); err == nil {
		t.Fatal("accepted invalid legacy Boolean option")
	}
	if err := st.SetOption(h.ctx, OptionStringTruncateAdopted, "true"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	// Both targets are private managed tables, so mixed TRUNCATE is atomic.
	h.exec(`TRUNCATE it_aw_users, it_aw_managed RESTART IDENTITY`)
	h.expectRows(`SELECT COUNT(*) FROM it_aw_users`, "0")
	h.expectRows(`SELECT COUNT(*) FROM it_aw_managed`, "0")
	h.expectReturning(`INSERT INTO it_aw_users (name) VALUES ('After') RETURNING __key,name`, "__key|name", "NULL|After")
	if f.raw.HGet(h.ctx, f.prefix+"seed", "name").Val() != "Seed" || f.raw.HGet(h.ctx, f.prefix+"seed", "active").Val() != "YES" || f.raw.HGet(h.ctx, f.prefix+"seed:prefs", "theme").Val() != "dark" || f.raw.Get(h.ctx, f.prefix+"string").Val() != "keep" {
		t.Fatal("SQL/Arrow writes changed an application object")
	}
	if n := h.prefixKeyCount(f.raw, f.prefix); n != 3 {
		t.Fatalf("private writes created/deleted external objects: %d", n)
	}
	if !f.adopt(HashAdoptOptions{Table: table, Apply: true}).Applied {
		t.Fatal("refresh after writes/TRUNCATE failed")
	}
	h.expectRows(`SELECT __key,name,active FROM it_aw_users`, "it:aw:users:seed|Seed|true")
}

func TestAdoptInsertCollisionAtomic(t *testing.T) {
	f := newHashFixture(t, "it:aw:race:")
	// Preserve the unmanaged keyed-writer collision test independently of the
	// managed-import SQL contract. No legacy table descriptor is fabricated.
	f.hset(f.prefix+"seed", "name", "Seed")
	if err := f.raw.SAdd(f.h.ctx, prefixesKey, f.prefix).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.raw.SRem(f.h.ctx, prefixesKey, f.prefix).Err() })
	st := &store{client: f.raw}
	meta := &tableMeta{Schema: "public", Name: "it_aw_race", KeyPrefix: f.prefix, Adopted: true, KeyRows: true,
		Columns: []columnMeta{{Name: "name", Type: typeString, Nullable: true}}}
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
