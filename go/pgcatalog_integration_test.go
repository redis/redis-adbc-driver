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
	"strings"
	"testing"
)

func TestSQLPostgresCatalogCompatibility(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_pgcat")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_pgcat") })
	h.exec("CREATE TABLE it_pgcat (id INTEGER, name VARCHAR(20))")

	h.expectRows("SELECT nspname FROM pg_catalog.pg_namespace WHERE nspname = 'public'", "public")
	h.expectRows("SELECT relname FROM pg_class WHERE relname = 'it_pgcat'", "it_pgcat")
	h.expectRows("SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = 'it_pgcat'", "it_pgcat")
	h.expectRows("SELECT 'it_pgcat'::regclass", "it_pgcat")
	h.expectRows("SELECT obj_description('it_pgcat'::regclass)", "NULL")
	h.expectRows("SELECT format_type(23, NULL)", "integer")

	h.expectRows(`SELECT column_name, data_type, udt_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'it_pgcat' ORDER BY ordinal_position`,
		"id|integer|int4", "name|character varying|varchar")
	h.expectRows(`SELECT table_catalog, is_identity, is_generated, collation_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'it_pgcat' ORDER BY ordinal_position`,
		"redis|NO|NEVER|NULL", "redis|NO|NEVER|NULL")
}

func TestSQLCatalogOIDLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP SCHEMA IF EXISTS it_cat_oid CASCADE`)
	h.exec(`CREATE SCHEMA it_cat_oid`)
	t.Cleanup(func() { h.exec(`DROP SCHEMA IF EXISTS it_cat_oid CASCADE`) })
	h.exec(`CREATE TABLE it_cat_oid.t (id integer PRIMARY KEY, name varchar(12) COLLATE "C", note text DEFAULT 'hello', CHECK (id>0))`)
	h.exec(`COMMENT ON TABLE it_cat_oid.t IS 'table comment'`)
	h.exec(`COMMENT ON COLUMN it_cat_oid.t.note IS 'column comment'`)
	h.exec(`CREATE VIEW it_cat_oid.v AS SELECT * FROM it_cat_oid.t`)
	h.exec(`INSERT INTO it_cat_oid.t VALUES (1,'name','note')`)
	h.expectRows(`SELECT pg_relation_size('it_cat_oid.t')>0,pg_total_relation_size('it_cat_oid.t')>=pg_relation_size('it_cat_oid.t')`, "true|true")
	h.expectRows(`SELECT data_type,udt_name,collation_name FROM information_schema.columns WHERE table_schema='it_cat_oid' AND table_name='t' ORDER BY ordinal_position`, "integer|int4|NULL", "character varying|varchar|C", "text|text|NULL")
	h.expectRows(`SELECT obj_description('it_cat_oid.t'::regclass), col_description('it_cat_oid.t'::regclass,3)`, "table comment|column comment")
	h.expectRows(`SELECT format_type(1043,16),format_type(1700,655366),pg_typeof(NULL::integer),pg_typeof('x'::text)`, "character varying(12)|numeric(10,2)|integer|text")
	h.expectRows(`SELECT 'int4'::regtype::oid,'integer'::regtype::oid,'public'::regnamespace::oid`, "23|23|2200")
	h.expectRows(`SELECT to_regclass('missing_catalog_relation'),to_regtype('missing_catalog_type')`, "NULL|NULL")
	h.expectError(`SELECT 'missing_catalog_relation'::regclass`, "does not exist")
	id, _ := h.query(`SELECT 'it_cat_oid.t'::regclass::oid`)
	vID, _ := h.query(`SELECT 'it_cat_oid.v'::regclass::oid`)
	h.exec(`ALTER TABLE it_cat_oid.t RENAME TO renamed`)
	h.expectRows(`SELECT 'it_cat_oid.renamed'::regclass::oid`, id...)
	h.exec(`ALTER VIEW it_cat_oid.v RENAME TO v2`)
	h.expectRows(`SELECT 'it_cat_oid.v2'::regclass::oid`, vID...)
	h.exec(`ALTER TABLE it_cat_oid.renamed ADD COLUMN score integer DEFAULT 7`)
	h.expectRows(`SELECT a.attname,pg_get_expr(d.adbin,d.adrelid) FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid='it_cat_oid.renamed'::regclass ORDER BY a.attnum`, "note|'hello'", "score|7")
	h.expectRows(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='it_cat_oid.renamed'::regclass AND contype='p'`, `PRIMARY KEY ("id")`)
	h2 := newSQLHarness(t)
	h2.expectRows(`SELECT 'it_cat_oid.renamed'::regclass::oid`, id...)
	h.exec(`TRUNCATE it_cat_oid.renamed`)
	h.expectRows(`SELECT 'it_cat_oid.renamed'::regclass::oid`, id...)
	h.exec(`DROP TABLE it_cat_oid.renamed`)
	h.exec(`CREATE TABLE it_cat_oid.renamed (id integer)`)
	newID, _ := h.query(`SELECT 'it_cat_oid.renamed'::regclass::oid`)
	if strings.Join(id, ",") == strings.Join(newID, ",") {
		t.Fatal("DROP/recreate reused the table OID")
	}
}

func TestSQLCatalogArrayOperations(t *testing.T) {
	h := newSQLHarness(t)
	h.expectRows(`SELECT 2=ANY(ARRAY[1,2]),3=ANY(ARRAY[1,NULL]),3<>ALL(ARRAY[1,2]),2=ANY(ARRAY[])`, "true|NULL|true|false")
	h.expectRows(`SELECT unnest(ARRAY[3,1,2]),generate_subscripts(ARRAY[3,1,2],1) LIMIT 2 OFFSET 1`, "1|2", "2|3")
	h.expectRows(`SELECT unnest(a) FROM (SELECT ARRAY[1,2] a UNION ALL SELECT ARRAY[3,4]) s LIMIT 2 OFFSET 1`, "2", "3")
	h.expectRows(`SELECT unnest(a) FROM (SELECT array_agg(n ORDER BY n DESC) a FROM generate_series(1,3) n) s`, "3", "2", "1")
	h.expectRows(`SELECT unnest(ARRAY[3,1,2]) n ORDER BY n LIMIT 2`, "1", "2")
	h.expectRows(`SELECT COUNT(*) FROM (SELECT ARRAY[1] a UNION SELECT ARRAY[2]) s`, "2")
	h.expectError(`SELECT 1=ANY(2)`, "requires an array")
}

func TestACLForeignIndexDiscovery(t *testing.T) {
	a := newACLHarness(t)
	f := newHashFixture(t, "it:foreign:acl:")
	f.hset(f.prefix+"a", "n", 1, "active", "true")
	f.appIndex("it_foreign_acl_idx", "SCHEMA", "n", "NUMERIC", "active", "TAG")
	a.user("it_foreign_reader", append(a.readOnlyCommands(), "+ft.info", "~adbc:*", "~it:foreign:acl:*")...)
	u := a.connect("it_foreign_reader", map[string]string{OptionStringForeignIndexes: "true"})
	u.expectRows(`SELECT n,active FROM redis_foreign.it_foreign_acl_idx`, "1|true")
	u.expectRows(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='redis_foreign' AND table_name='it_foreign_acl_idx'`, "2")
	u.expectError(`UPDATE redis_foreign.it_foreign_acl_idx SET n=2`, "read-only")
}
