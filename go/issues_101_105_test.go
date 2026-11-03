// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestUUIDInput(t *testing.T) {
	const canonical = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
	for _, s := range []string{canonical, strings.ToUpper(canonical), "{A0EEBC999C0B4EF8BB6D6BB9BD380A11}", "a0ee-bc99-9c0b-4ef8-bb6d-6bb9-bd38-0a11"} {
		v, err := Coerce(stringValue(s), typeUUID)
		if err != nil || v.S != canonical || v.T != typeUUID {
			t.Errorf("%q: %v %v", s, v, err)
		}
	}
	for _, s := range []string{"", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a1", "a0ee--bc999c0b4ef8bb6d6bb9bd380a11", "urn:uuid:" + canonical, canonical + " ", "{ " + canonical + "}"} {
		if _, err := parseUUID(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	for i := 0; i < 20; i++ {
		v, err := randomUUID()
		if err != nil || v.S[14] != '4' || !strings.ContainsRune("89ab", rune(v.S[19])) {
			t.Fatalf("bad v4 UUID: %v %v", v, err)
		}
	}
}

func TestSQLUUIDAndWithInsert(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_new_uuid")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_new_uuid") })
	h.exec("CREATE TABLE it_new_uuid (id UUID DEFAULT gen_random_uuid(), v INTEGER)")
	h.exec("WITH x AS (SELECT 5 AS v) INSERT INTO it_new_uuid(v) SELECT v FROM x")
	h.expectRows("SELECT count(*), count(DISTINCT id) FROM it_new_uuid", "1|1")
	schema := h.expectRows("SELECT '{A0EEBC999C0B4EF8BB6D6BB9BD380A11}'::uuid AS id", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	if v, ok := schema.Field(0).Metadata.GetValue(typeNameKey); !ok || v != "UUID" {
		t.Errorf("UUID field metadata: %v", schema)
	}
	h.exec("INSERT INTO it_new_uuid VALUES ('A0EEBC999C0B4EF8BB6D6BB9BD380A11', 6)")
	h.expectRows("SELECT v FROM it_new_uuid WHERE id = 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid", "6")
}

func TestSQLNamedIndexAndMatView(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_mv")
	h.exec("DROP TABLE IF EXISTS it_new_idx")
	t.Cleanup(func() {
		h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_mv")
		h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_mv2")
		h.exec("DROP TABLE IF EXISTS it_new_idx")
	})
	h.exec("CREATE TABLE it_new_idx(id INTEGER NOINDEX, label VARCHAR NOINDEX)")
	h.exec("INSERT INTO it_new_idx VALUES (1,'one'),(2,'two')")
	h.exec("CREATE INDEX CONCURRENTLY IF NOT EXISTS it_new_idx_label ON it_new_idx USING btree(label) INCLUDE(id) WITH (fillfactor=80)")
	h.expectRows("SELECT is_indexed FROM information_schema.columns WHERE table_name='it_new_idx' AND column_name='label'", "YES")
	h.expectRows("SELECT tablename FROM pg_indexes WHERE indexname='it_new_idx_label'", "it_new_idx")
	h.expectRows("SELECT id FROM it_new_idx WHERE label='two'", "2")
	h.exec("CREATE MATERIALIZED VIEW it_new_mv AS SELECT count(*) AS n FROM it_new_idx")
	h.expectRows("SELECT n FROM it_new_mv", "2")
	h.exec("INSERT INTO it_new_idx VALUES (3,'three')")
	h.expectRows("SELECT n FROM it_new_mv", "2")
	h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_new_mv")
	h.expectRows("SELECT n FROM it_new_mv", "3")
	h.exec("REFRESH MATERIALIZED VIEW it_new_mv WITH NO DATA")
	h.exec("REFRESH MATERIALIZED VIEW it_new_mv")
	h.expectRows("SELECT n FROM it_new_mv", "3")
	h.expectRows("SELECT ispopulated FROM pg_matviews WHERE matviewname='it_new_mv'", "true")
	h.exec("ALTER MATERIALIZED VIEW it_new_mv RENAME TO it_new_mv2")
	h.expectRows("SELECT n FROM it_new_mv2", "3")
	h.exec("DROP MATERIALIZED VIEW it_new_mv2")
	h.exec("CREATE MATERIALIZED VIEW it_new_mv AS SELECT * FROM it_new_idx WITH NO DATA")
	h.exec("REFRESH MATERIALIZED VIEW it_new_mv")
	h.expectRows("SELECT count(*) FROM it_new_mv", "3")
	h.exec("DROP INDEX IF EXISTS it_new_idx_label CASCADE")
	h.expectRows("SELECT count(*) FROM pg_indexes WHERE indexname='it_new_idx_label'", "0")
}

func TestSQLSequencesIdentityAndDMLCTE(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_new_serial")
	h.exec("DROP TABLE IF EXISTS it_new_identity")
	h.exec("DROP SEQUENCE IF EXISTS it_new_seq")
	t.Cleanup(func() {
		h.exec("DROP TABLE IF EXISTS it_new_serial")
		h.exec("DROP TABLE IF EXISTS it_new_identity")
		h.exec("DROP SEQUENCE IF EXISTS it_new_seq")
	})
	h.exec("CREATE SEQUENCE it_new_seq START WITH 10 INCREMENT BY 2 MAXVALUE 14 CYCLE")
	h.expectRows("SELECT nextval('it_new_seq')", "10")
	h.expectRows("SELECT currval('it_new_seq'), lastval()", "10|10")
	h.expectRows("SELECT nextval('it_new_seq')", "12")
	h.expectRows("SELECT setval('it_new_seq',14)", "14")
	h.expectRows("SELECT nextval('it_new_seq')", "1")
	h.exec("ALTER SEQUENCE it_new_seq RESTART WITH 6")
	h.expectRows("SELECT nextval('it_new_seq')", "6")
	h.exec("CREATE TABLE it_new_serial(id SERIAL PRIMARY KEY, v TEXT)")
	h.expectRows("INSERT INTO it_new_serial(v) VALUES ('a') RETURNING id", "1")
	h.expectRows("INSERT INTO it_new_serial(v) VALUES ('b') RETURNING id", "2")
	h.exec("TRUNCATE it_new_serial RESTART IDENTITY")
	h.expectRows("INSERT INTO it_new_serial(v) VALUES ('a') RETURNING id", "1")
	h.exec("CREATE TABLE it_new_identity(id BIGINT GENERATED ALWAYS AS IDENTITY (START WITH 5 INCREMENT BY 5), v TEXT)")
	h.expectRows("INSERT INTO it_new_identity(v) VALUES ('a') RETURNING id", "5")
	h.expectRows("INSERT INTO it_new_identity(id,v) OVERRIDING USER VALUE VALUES (999,'b') RETURNING id", "10")
	h.expectRows("INSERT INTO it_new_identity(id,v) OVERRIDING SYSTEM VALUE VALUES (99,'c') RETURNING id", "99")
	h.expectRows("WITH d AS (DELETE FROM it_new_identity WHERE id=10 RETURNING *) SELECT count(*) FROM d", "1")
	h.expectRows("WITH d AS (DELETE FROM it_new_identity WHERE id=5 RETURNING *) SELECT count(*) FROM it_new_identity", "2")
	h.expectRows("SELECT count(*) FROM it_new_identity", "1")
	h.exec("WITH moved AS (DELETE FROM it_new_identity RETURNING v) INSERT INTO it_new_serial(v) SELECT v FROM moved")
	h.expectRows("SELECT v FROM it_new_serial ORDER BY id", "a", "c")
	h.exec("WITH unused AS (INSERT INTO it_new_identity(v) VALUES ('once') RETURNING id) SELECT 1")
	h.expectRows("SELECT v FROM it_new_identity", "once")
	h.expectRows("WITH u AS (UPDATE it_new_identity SET v='twice' RETURNING v) SELECT a.v,b.v FROM u a CROSS JOIN u b", "twice|twice")
}

func TestSQLNewFeatureErrorsAndMetadata(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_new_meta")
	h.exec("DROP SEQUENCE IF EXISTS it_new_cache")
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_empty")
	t.Cleanup(func() {
		h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_empty")
		h.exec("DROP TABLE IF EXISTS it_new_meta")
		h.exec("DROP SEQUENCE IF EXISTS it_new_cache")
	})
	h.exec("CREATE TABLE it_new_meta(id BIGINT NOT NULL, u UUID)")
	h.exec("ALTER TABLE it_new_meta ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (START WITH 7)")
	h.expectRows("INSERT INTO it_new_meta(u) VALUES ('A0EEBC999C0B4EF8BB6D6BB9BD380A11') RETURNING id", "7")
	h.expectRows("SELECT is_identity,identity_generation,identity_start FROM information_schema.columns WHERE table_name='it_new_meta' AND column_name='id'", "YES|ALWAYS|7")
	h.expectRows("SELECT sequence_name FROM information_schema.sequences WHERE sequence_name='it_new_meta_id_seq'", "it_new_meta_id_seq")
	h.expectError("INSERT INTO it_new_meta VALUES (99,NULL)", "cannot insert a non-DEFAULT value")
	h.exec("ALTER TABLE it_new_meta ALTER COLUMN id SET GENERATED BY DEFAULT")
	h.expectRows("INSERT INTO it_new_meta VALUES (99,NULL) RETURNING id", "99")
	h.exec("ALTER TABLE it_new_meta ALTER COLUMN id RESTART WITH 50")
	h.expectRows("INSERT INTO it_new_meta(u) VALUES (NULL) RETURNING id", "50")
	h.expectRows("SELECT id FROM it_new_meta WHERE u='A0EEBC999C0B4EF8BB6D6BB9BD380A11'", "7")
	h.expectError("SELECT 'bad'::uuid", "invalid input syntax for type uuid")
	h.exec("CREATE SEQUENCE it_new_cache MINVALUE 0 START 0 MAXVALUE 4 CACHE 3")
	h.expectRows("SELECT nextval('it_new_cache') FROM generate_series(1,5)", "0", "1", "2", "3", "4")
	h.expectError("SELECT nextval('it_new_cache')", "reached limit")
	h.exec("ALTER SEQUENCE it_new_cache RESTART 2")
	h.expectRows("SELECT nextval('it_new_cache')", "2")
	h.exec("CREATE MATERIALIZED VIEW it_new_empty AS SELECT * FROM it_new_meta WITH NO DATA")
	h.expectError("SELECT * FROM it_new_empty", "has not been populated")
	h.expectError("INSERT INTO it_new_empty VALUES (1,NULL)", "cannot change materialized view")
	h.exec("REFRESH MATERIALIZED VIEW it_new_empty")
	h.expectError("DELETE FROM it_new_empty WHERE false", "cannot change materialized view")
	h.expectError("UPDATE it_new_empty SET id=0 WHERE false", "cannot change materialized view")
	h.expectError("DROP TABLE it_new_empty", "use DROP MATERIALIZED VIEW")
	h.exec("ALTER TABLE it_new_meta ALTER COLUMN id DROP IDENTITY")
	h.expectRows("SELECT is_identity FROM information_schema.columns WHERE table_name='it_new_meta' AND column_name='id'", "NO")
	h.expectRows("SELECT count(*) FROM information_schema.sequences WHERE sequence_name='it_new_meta_id_seq'", "0")
}

func TestSQLDMLCTESnapshotAndSchema(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_new_cte")
	h.exec("CREATE TABLE it_new_cte(id INTEGER, v INTEGER)")
	h.exec("INSERT INTO it_new_cte VALUES (1,10),(2,20)")
	h.expectRows("WITH a AS (UPDATE it_new_cte SET v=100 WHERE id=1 RETURNING id,v), b AS (UPDATE it_new_cte SET v=200 WHERE id=2 RETURNING id,v) SELECT a.v,b.v,t.v FROM a CROSS JOIN b JOIN it_new_cte t ON t.id=a.id", "100|200|10")
	h.expectRows("SELECT v FROM it_new_cte ORDER BY id", "100", "200")
	h.expectRows("WITH moved(x) AS (DELETE FROM it_new_cte WHERE id=1 RETURNING v) INSERT INTO it_new_cte SELECT 3,x FROM moved RETURNING id,v", "3|100")
	h.exec("WITH unused AS (DELETE FROM it_new_cte WHERE id=2) SELECT 1")
	h.expectRows("SELECT id FROM it_new_cte", "3")
	h.expectSchemaErrorText("WITH d AS (DELETE FROM it_new_cte RETURNING id) SELECT missing FROM d", `column "missing" does not exist in table "d"`)
	h.expectRows("SELECT id FROM it_new_cte", "3")
}

func TestSQLSequenceConcurrentConnections(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP SEQUENCE IF EXISTS it_new_concurrent")
	t.Cleanup(func() { h.exec("DROP SEQUENCE IF EXISTS it_new_concurrent") })
	h.exec("CREATE SEQUENCE it_new_concurrent CACHE 5")
	const workers, perWorker = 6, 20
	conns := make([]*sqlHarness, workers)
	for i := range conns {
		conns[i] = newSQLHarness(t)
		conns[i].expectError("SELECT currval('it_new_concurrent')", "not yet defined")
	}
	results := make(chan string, workers*perWorker)
	var wg sync.WaitGroup
	for _, conn := range conns {
		wg.Add(1)
		go func(h *sqlHarness) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				rows, _ := h.query("SELECT nextval('it_new_concurrent')")
				if len(rows) != 1 {
					t.Errorf("nextval returned %d rows", len(rows))
					return
				}
				results <- rows[0]
			}
		}(conn)
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for v := range results {
		if seen[v] {
			t.Errorf("duplicate nextval %s", v)
		}
		seen[v] = true
	}
	if len(seen) != workers*perWorker {
		t.Errorf("allocated %d unique values", len(seen))
	}
	h.expectError("SELECT currval('it_new_concurrent')", "not yet defined")
	h.expectRows("SELECT nextval('it_new_concurrent')", "121")
	h.exec("ALTER SEQUENCE it_new_concurrent RESTART WITH 9223372036854775807")
	h.expectRows("SELECT nextval('it_new_concurrent')", "9223372036854775807")
	h.expectError("SELECT nextval('it_new_concurrent')", "reached limit")
}

func TestSQLAutoIngestOwnershipAndFailedRefresh(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_new_bulk", "it_new_bulk_renamed")
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_refresh")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_refresh") })
	h.exec("CREATE TABLE it_new_bulk(generated_id BIGINT GENERATED ALWAYS AS IDENTITY, id BIGINT)")
	if err := h.ingest("it_new_bulk", adbc.OptionValueIngestModeAppend, false, 10, 20); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT generated_id,id FROM it_new_bulk ORDER BY generated_id", "1|10", "2|20")
	h.expectRows("SELECT pg_get_serial_sequence('it_new_bulk','generated_id')", "public.it_new_bulk_generated_id_seq")
	h.exec("CREATE MATERIALIZED VIEW it_new_refresh AS SELECT 100/id AS n FROM it_new_bulk")
	h.exec("UPDATE it_new_bulk SET id=0")
	h.expectError("REFRESH MATERIALIZED VIEW it_new_refresh", "division by zero")
	h.expectRows("SELECT n FROM it_new_refresh ORDER BY n", "5", "10")
	h.exec("ALTER TABLE it_new_bulk RENAME COLUMN generated_id TO serial_id")
	h.exec("ALTER TABLE it_new_bulk RENAME TO it_new_bulk_renamed")
	h.expectRows("SELECT pg_get_serial_sequence('it_new_bulk_renamed','serial_id')", "public.it_new_bulk_generated_id_seq")
	h.exec("DROP TABLE it_new_bulk_renamed")
	h.expectRows("SELECT count(*) FROM information_schema.sequences WHERE sequence_name='it_new_bulk_generated_id_seq'", "0")
	h.exec("CREATE TEMP TABLE it_new_temp_serial(id SERIAL)")
	h.expectRows("INSERT INTO it_new_temp_serial DEFAULT VALUES RETURNING id", "1")
	h.exec("DROP TABLE it_new_temp_serial")
}

func TestSQLNewFeatureSQLStatesAndDependencies(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_new_dependency")
	h.exec("DROP SEQUENCE IF EXISTS it_new_dependency_id_seq")
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_state_mv")
	t.Cleanup(func() {
		h.exec("DROP MATERIALIZED VIEW IF EXISTS it_new_state_mv")
		h.exec("DROP SEQUENCE IF EXISTS it_new_dependency_id_seq")
	})
	h.exec("CREATE TABLE it_new_dependency(id SERIAL, v INTEGER NOINDEX)")
	h.exec("CREATE INDEX it_new_dependency_v_idx ON it_new_dependency(v)")
	h.expectError("CREATE SEQUENCE it_new_dependency_v_idx", "already exists")
	h.expectError("ALTER TABLE it_new_dependency RENAME TO it_new_dependency_v_idx", "already exists")
	h.exec("ALTER SEQUENCE it_new_dependency_id_seq OWNED BY NONE")
	h.exec("ALTER TABLE it_new_dependency DROP COLUMN id")
	h.expectRows("SELECT nextval('it_new_dependency_id_seq')", "1")
	h.exec("ALTER TABLE it_new_dependency DROP COLUMN v, ADD COLUMN x INTEGER")
	h.expectRows("SELECT count(*) FROM pg_indexes WHERE indexname='it_new_dependency_v_idx'", "0")
	h.exec("CREATE INDEX it_new_dependency_v_idx ON it_new_dependency(x)")
	h.exec("CREATE MATERIALIZED VIEW it_new_state_mv AS SELECT * FROM it_new_dependency WITH NO DATA")
	checkState(t, "unpopulated materialized view", h.stmtErr("SELECT * FROM it_new_state_mv", nil), adbc.StatusInvalidState, "55000", "has not been populated")
	checkState(t, "materialized write", h.stmtErr("DELETE FROM it_new_state_mv", nil), adbc.StatusInvalidArgument, "42809", "cannot change materialized view")
	checkState(t, "invalid UUID", h.stmtErr("SELECT 'invalid'::uuid", nil), adbc.StatusInvalidData, "22P02", "invalid input syntax for type uuid")
	checkState(t, "unique index", h.stmtErr("CREATE UNIQUE INDEX no_unique ON it_new_dependency(x)", nil), adbc.StatusNotImplemented, "0A000", "UNIQUE indexes")
	h.exec("INSERT INTO it_new_dependency VALUES (1)")
	h.expectError("WITH d AS (DELETE FROM it_new_dependency) SELECT * FROM d", "does not have a RETURNING clause")
	h.expectRows("SELECT x FROM it_new_dependency", "1")
	h.exec("WITH d AS (DELETE FROM it_new_dependency RETURNING x) SELECT * FROM d LIMIT 0")
	h.expectRows("SELECT count(*) FROM it_new_dependency", "0")
}
