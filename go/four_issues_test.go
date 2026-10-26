// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"github.com/apache/arrow-adbc/go/adbc"
	"strings"
	"testing"
)

func TestFourIssueSQL(t *testing.T) {
	for _, c := range []struct{ sql, want string }{
		{"select array[1,2] || array[3,4]", "{1,2,3,4}"},
		{"select array(select generate_series(1,3))", "{1,2,3}"},
		{"select array_position(array[1,null,3],null), array_positions(array[1,2,1],1)", "2|{1,3}"},
		{"select array_remove(array[1,null,1],1),array_replace(array[1,null],null,2)", "{NULL}|{1,2}"},
		{"select array_upper(array[1,2],1),array_lower(array[1,2],1),array_dims(array[1,2])", "2|1|[1:2]"},
		{"select array_to_string(array['a',null,'b'],',','N')", "a,N,b"},
		{"select array[1,2] @> array[2],array[1] <@ array[1,2],array[1] && array[2]", "true|true|false"},
		{"select generate_series(1,2)+10,unnest(array['a','b','c'])", "11|a\n12|b\nNULL|c"},
		{"select * from unnest(array[1,2],array['a']) with ordinality u(a,b,i)", "1|a|1\n2|NULL|2"},
		{"select * from regexp_matches('a1 b2','([a-z])([0-9])','g') r(m)", "{a,1}\n{b,2}"},
		{"select regexp_split_to_array('a,b',',')", "{a,b}"},
		{"select * from generate_series(1,2) g, lateral generate_series(1,g) x", "1|1\n2|1\n2|2"},
		{"select (array[1,2,3])[2]", "2"},
		{"select '{1,2,NULL}'::integer[]", "{1,2,NULL}"},
		{"select string_to_array('a,b',',')", "{a,b}"},
		{"select array_append(array[1,2],3)", "{1,2,3}"},
		{"select array[1,2,3][2:3]", "{2,3}"},
		{"select generate_series(1,3)", "1\n2\n3"},
		{"select * from generate_series(1,3) with ordinality as g(n,i)", "1|1\n2|2\n3|3"},
		{"select * from string_to_table('a,b',',')", "a\nb"},
		{"select * from pg_catalog.unnest(array[1,2])", "1\n2"},
		{"select * from regexp_split_to_table('a,b',',')", "a\nb"},
		{"select * from unnest(array[1,2]) x(n)", "1\n2"},
		{"select * from rows from (generate_series(1,2),generate_series(1,3)) r(a,b)", "1|1\n2|2\nNULL|3"},
	} {
		t.Run(c.sql, func(t *testing.T) {
			if got := memQuery(c.sql); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestFourIssueDropPreflight(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("CREATE TABLE it_four_drop_a(id integer)")
	h.exec("CREATE TABLE it_four_drop_b(id integer)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_four_drop_a"); h.exec("DROP TABLE IF EXISTS it_four_drop_b") })
	h.expectError("DROP TABLE it_four_drop_a, it_four_drop_missing", "does not exist")
	h.expectRows("SELECT COUNT(*) FROM it_four_drop_a", "0")
	h.exec("DROP TABLE it_four_drop_a, it_four_drop_b")
}

func TestArrayStorage(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_array_storage")
	h.exec("CREATE TABLE it_array_storage(id integer, vals integer[], tags text ARRAY, exacts numeric(20,2)[4])")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_array_storage") })
	h.exec("INSERT INTO it_array_storage VALUES(1,ARRAY[1,NULL,3],ARRAY['a,b','NULL',''],ARRAY[9007199254740993.25]),(2,'{}','{NULL}',NULL)")
	h.expectRows("SELECT id, vals[3], cardinality(vals), tags[1],exacts[1] FROM it_array_storage ORDER BY id", "1|3|3|a,b|9007199254740993.25", "2|NULL|0|NULL|NULL")
	h.exec("UPDATE it_array_storage SET vals=array_append(vals,4) WHERE id=1")
	h.expectRows("SELECT vals[4] FROM it_array_storage WHERE id=1", "4")
	h.expectRows("SELECT id FROM it_array_storage WHERE 3=ANY(vals)", "1")
}
func TestSRFPlacement(t *testing.T) {
	for _, q := range []string{"select case when true then generate_series(1,2) end", "select coalesce(unnest(array[1]),0)", "select sum(generate_series(1,2))", "select 1 where generate_series(1,2)>0"} {
		if got := memQuery(q); !strings.Contains(got, "set-returning functions are not allowed") {
			t.Fatalf("%s: %s", q, got)
		}
	}
}

func TestDropViewPreflight(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("CREATE VIEW it_drop_v1 AS SELECT 1 n")
	h.exec("CREATE VIEW it_drop_v2 AS SELECT 2 n")
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS it_drop_v1,it_drop_v2") })
	h.expectError("DROP VIEW it_drop_v1,it_drop_no_view", "does not exist")
	h.expectRows("SELECT * FROM it_drop_v1", "1")
	h.exec("DROP VIEW IF EXISTS it_drop_v1,it_drop_no_view,it_drop_v2")
}
func TestArrayView(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("CREATE VIEW it_array_view AS SELECT ARRAY[1,NULL,3] a")
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS it_array_view") })
	h.expectRows("SELECT a[3] FROM it_array_view", "3")
}

func TestArrayStoredScalarRoundTrip(t *testing.T) {
	for _, expr := range []string{"ARRAY[true,false,NULL]", "ARRAY[1::smallint,NULL]", "ARRAY[9007199254740993::bigint,NULL]", "ARRAY[1.25::numeric(20,2),NULL]", "ARRAY[1.25::real,NULL]", "ARRAY[1.25::double precision,NULL]", "ARRAY['a,b','NULL','',NULL]", "ARRAY[from_hex('00ff'),NULL]", "ARRAY[DATE '2024-02-29',NULL]", "ARRAY[TIME '12:34:56.123456',NULL]", "ARRAY[TIMETZ '12:34:56+05:30',NULL]", "ARRAY[TIMESTAMP '2024-01-02 03:04:05.123456',NULL]", "ARRAY[TIMESTAMPTZ '2024-01-02 03:04:05+05:30',NULL]", "ARRAY[INTERVAL '1 month 2 days 3 seconds',NULL]"} {
		t.Run(expr, func(t *testing.T) {
			v, err := (&evalEnv{}).eval(parseTestExpr(t, expr))
			if err != nil {
				t.Fatal(err)
			}
			raw := encodeStored(v)
			data, err := json.Marshal(v.T)
			if err != nil {
				t.Fatal(err)
			}
			var typ ColType
			if err = json.Unmarshal(data, &typ); err != nil {
				t.Fatal(err)
			}
			got, err := decodeStored(raw, typ)
			if err != nil {
				t.Fatal(err)
			}
			if c, ok := compareArrays(v, got, utcZone); !ok || c != 0 {
				t.Fatalf("round trip changed %s: %s -> %s", expr, raw, got.Text())
			}
		})
	}
}

func TestSRFResourceBounds(t *testing.T) {
	old := maxSeriesRows
	maxSeriesRows = 2
	t.Cleanup(func() { maxSeriesRows = old })
	for _, q := range []string{"select * from unnest(array[1,2,3])", "select unnest(array[1,2,3])", "select * from regexp_split_to_table('a,b,c',',')", "select * from rows from(generate_series(1,3))"} {
		if got := memQuery(q); !strings.Contains(got, "more than 2 rows") {
			t.Fatalf("%s: %s", q, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := srfRows(&evalEnv{ctx: ctx}, &Func{Name: "GENERATE_SERIES"}); !isStatus(err, adbc.StatusCancelled) {
		t.Fatalf("cancelled SRF: %v", err)
	}
}
