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
	"reflect"
	"testing"

	"github.com/adbc-drivers/driverbase-go/driverbase"
)

// This checks persisted metadata through the same callback GetObjects uses,
// including ordered FK pairs and the absence of UNIQUE/FK enforcement.
func TestSQLDeclarationMetadataLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_decl_parent", "it_decl_child", "it_decl_renamed", "it_decl_rekey")
	h.exec(`CREATE TABLE it_decl_parent(a integer,b integer,PRIMARY KEY(a,b))`)
	h.exec(`CREATE TABLE it_decl_child(x integer UNIQUE,y integer,CONSTRAINT pair UNIQUE(y,x),CONSTRAINT parent_fk FOREIGN KEY(y,x) REFERENCES it_decl_parent(a,b))`)
	h.exec(`INSERT INTO it_decl_child VALUES(1,99),(1,99)`)
	h.expectRows(`SELECT count(*) FROM it_decl_child`, "2")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	impl := st.(*statementImpl).conn
	constraints := func() []driverbase.ConstraintInfo {
		t.Helper()
		table := "it_decl_child"
		tables, err := impl.GetTablesForDBSchema(h.ctx, catalogName, defaultSchema, &table, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(tables) != 1 {
			t.Fatalf("tables: %#v", tables)
		}
		return tables[0].TableConstraints
	}
	assertFK := func(table string, columns ...string) {
		t.Helper()
		all := constraints()
		for _, c := range all {
			if c.ConstraintType != "FOREIGN KEY" {
				continue
			}
			if !reflect.DeepEqual([]string(c.ConstraintColumnNames), []string{"y", "x"}) {
				t.Fatalf("local order: %#v", c)
			}
			if len(columns) == 0 {
				if c.ConstraintColumnUsage != nil {
					t.Fatalf("unexpected usage: %#v", c)
				}
				return
			}
			if len(c.ConstraintColumnUsage) != len(columns) {
				t.Fatalf("usage: %#v", c)
			}
			for i, u := range c.ConstraintColumnUsage {
				if u.ForeignKeyTable != table || u.ForeignKeyColumn != columns[i] || *u.ForeignKeyCatalog != "redis" || *u.ForeignKeyDbSchema != "public" {
					t.Fatalf("usage: %#v", c)
				}
			}
			return
		}
		t.Fatal("missing FOREIGN KEY")
	}
	all := constraints()
	if len(all) != 3 || all[0].ConstraintType != "UNIQUE" || all[0].ConstraintColumnUsage != nil || !reflect.DeepEqual([]string(all[1].ConstraintColumnNames), []string{"y", "x"}) {
		t.Fatalf("constraints: %#v", all)
	}
	assertFK("it_decl_parent", "a", "b")
	h.exec(`ALTER TABLE it_decl_parent RENAME COLUMN a TO aa`)
	h.exec(`ALTER TABLE it_decl_parent RENAME TO it_decl_renamed`)
	assertFK("it_decl_renamed", "aa", "b")
	rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	rk.exec(`ALTER TABLE it_decl_renamed RENAME TO it_decl_rekey`)
	assertFK("it_decl_rekey", "aa", "b")
	h.exec(`ALTER TABLE it_decl_rekey ALTER COLUMN aa TYPE bigint`)
	assertFK("it_decl_rekey", "aa", "b")
	h.exec(`TRUNCATE it_decl_child`)
	assertFK("it_decl_rekey", "aa", "b")
	h.exec(`DROP TABLE it_decl_rekey`)
	h.exec(`CREATE TABLE it_decl_rekey(aa integer,b integer,PRIMARY KEY(aa,b))`)
	assertFK("")
	h.exec(`ALTER TABLE it_decl_child ADD COLUMN z integer REFERENCES missing_parent(id) UNIQUE`)
	h.exec(`ALTER TABLE it_decl_child DROP CONSTRAINT pair`)
	h.exec(`ALTER TABLE it_decl_child DROP COLUMN z`)
	if len(constraints()) != 2 {
		t.Fatal("ALTER declarations not retained/dropped")
	}
	h.exec(`ALTER TABLE it_decl_child DROP COLUMN x`)
	if len(constraints()) != 0 {
		t.Fatal("local column dependencies not dropped")
	}
}
