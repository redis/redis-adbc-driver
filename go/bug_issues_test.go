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
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

func TestBugIssueScalarRegressions(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"round(2.5::float8)", "2", "DOUBLE PRECISION"},
		{"round(-0.5::float8)", "-0", "DOUBLE PRECISION"},
		{"2.5::float8::integer", "2", "INTEGER"},
		{"(-2.5)::float8::integer", "-2", "INTEGER"},
		{"1234567::float8::text", "1234567", "VARCHAR"},
		{"100000000000000::float8::text", "100000000000000", "VARCHAR"},
		{"1e15::float8::text", "1e+15", "VARCHAR"},
		{"1234567::float4::text", "1.234567e+06", "VARCHAR"},
		{"'x' || 2500000.5::float8", "x2500000.5", "VARCHAR"},
		{"to_json(1000000::float8)", "1000000", "VARCHAR"},
		{"1", "1", "INTEGER"},
		{"2147483648", "2147483648", "BIGINT"},
		{"2147483647::bigint + 1", "2147483648", "BIGINT"},
		{"(1 = 1) is true", "true", "BOOLEAN"},
		{"(1 = 2) is not false", "false", "BOOLEAN"},
		{"null::boolean is unknown", "true", "BOOLEAN"},
		{"null isnull", "true", "BOOLEAN"},
		{"1 notnull", "true", "BOOLEAN"},
		{`length(E'a\nb')`, "3", "BIGINT"},
		{`E'it\'s'`, "it's", "VARCHAR"},
		{`E'\x41\102'`, "AB", "VARCHAR"},
		{`U&'d\0061t\+000061'`, "data", "VARCHAR"},
		{`U&'d!0061t!+000061' UESCAPE '!'`, "data", "VARCHAR"},
		{`'{"a":1}'::jsonb || '{"b":2}'`, `{"a": 1, "b": 2}`, "VARCHAR"},
		{`'[1]'::jsonb || '[2]'`, `[1, 2]`, "VARCHAR"},
		{`'{"a":1}'::jsonb || '{"a":2}'::jsonb`, `{"a": 2}`, "VARCHAR"},
		{`'1'::jsonb || '[2]'::jsonb`, `[1, 2]`, "VARCHAR"},
		{`regexp_like('aa', '\(a\)\1', 'b')`, "true", "BOOLEAN"},
		{`regexp_like('abc', 'a b c', 'x')`, "true", "BOOLEAN"},
		{`regexp_like('abc', 'a|ab', 'e')`, "true", "BOOLEAN"},
		{`regexp_like('a.b', 'a.b', 'q')`, "true", "BOOLEAN"},
		{`E'\uD83D\uDE00'`, "😀", "VARCHAR"},
		{`substring('xyz' from 'x|xy')`, "xy", "VARCHAR"},
		{`'abab' ~ '(ab)\1'`, "true", "BOOLEAN"},
		{`'foobar' ~ 'foo(?=bar)'`, "true", "BOOLEAN"},
		{`'foobar' ~ '(?<=foo)bar'`, "true", "BOOLEAN"},
		{`'hello world' ~ '\yworld\y'`, "true", "BOOLEAN"},
		{`regexp_replace('hello world', '\mworld', 'X')`, "hello X", "VARCHAR"},
		{`date_add(timestamptz '2024-01-01 00:00+00', interval '1 day')`, "2024-01-02 00:00:00+00", "TIMESTAMP(6) WITH TIME ZONE"},
		{`"date_add"(date '2024-01-01', interval '1 day')`, "2024-01-02 00:00:00+00", "TIMESTAMP(6) WITH TIME ZONE"},
		{`date_subtract(timestamptz '2024-01-01 00:00+00', interval '1 day')`, "2023-12-31 00:00:00+00", "TIMESTAMP(6) WITH TIME ZONE"},
		{`date_add(timestamptz '2024-03-09 12:00-05', interval '1 day', 'America/New_York')`, "2024-03-10 16:00:00+00", "TIMESTAMP(6) WITH TIME ZONE"},
	})
	for _, sql := range []string{"2147483647 + 1", "32767::smallint + 1::smallint", "1 is true"} {
		expr := parseTestExpr(t, sql)
		_, err := inferType(expr, nil, nil)
		if err == nil {
			_, err = (&evalEnv{}).eval(expr)
		}
		var se *sqlError
		if !errors.As(err, &se) || se.state != map[bool]string{true: stateDatatypeMismatch, false: stateNumericRange}[sql == "1 is true"] {
			t.Errorf("%s: %v", sql, err)
		}
	}
	v, err := (&evalEnv{}).eval(parseTestExpr(t, "round(-0.5::float8)"))
	if err != nil || !math.Signbit(v.F) {
		t.Fatalf("negative zero: %v %v", v, err)
	}
	for _, sql := range []string{`select E'\xff'`, `select E'\x00'`} {
		_, err := ParseScript(sql)
		var se *sqlError
		if !errors.As(err, &se) || se.state != "22021" {
			t.Errorf("%s: %v", sql, err)
		}
	}
}

func TestBugIssueInMemoryQueries(t *testing.T) {
	const tables = `with t(id,name,score,grp) as (select 1,'apple',10,'a' union all select 2,'Banana',null,'a' union all select 3,'cherry',30,'b' union all select 4,'_x',20,'b'), u(id,label) as (select 1,'one' union all select 2,'two') `
	cases := []struct{ q, want string }{
		{tables + `select id from t order by score desc`, "2\n3\n4\n1"},
		{tables + `select id,row_number() over(order by score desc) from t order by id`, "1|4\n2|1\n3|2\n4|3"},
		{tables + `select string_agg(coalesce(score::text,'null'),',' order by score desc) from t`, "null,30,20,10"},
		{tables + `select id,label from t join u using(id) order by id`, "1|one\n2|two"},
		{tables + `select * from t join u using(id) order by id`, "1|apple|10|a|one\n2|Banana|NULL|a|two"},
		{`with a(id,x) as(select 1,'a' union all select 2,'b'), b(id,y) as(select 1,'c' union all select 3,'d') select * from a full join b using(id) order by id`, "1|a|c\n2|b|NULL\n3|NULL|d"},
		{`with a(id) as(select 1 union all select 2), b(id) as(select 1 union all select 3) select id,a.id,b.id from a right join b using(id) order by 1`, "1|1|1\n3|NULL|3"},
		{`select a from (select 1 as "A",2 as a) s`, "2"},
		{`select "A" from (select 1 as "A",2 as a) s`, "1"},
		{`select U&"d\0061ta" from (select 3 as data) s`, "3"},
		{tables + `select variance(score), stddev(score) from t`, "100.0000000000000000|10.0000000000000000"},
		{tables + `select variance(score::bigint),variance(score::numeric(10,2)) from t`, "100.0000000000000000|100.0000000000000000"},
	}
	cases = append(cases, struct{ q, want string }{
		`with t(x) as (select 1 union all select 2 union all select 3) select var_pop(x)+0,coalesce(var_pop(x),0) from t`,
		"0.66666666666666666667|0.66666666666666666667",
	}, struct{ q, want string }{
		`with t(x) as (select 1.00000000000000000001::numeric(40,20) union all select 1.00000000000000000002::numeric(40,20)) select var_samp(x)::text,stddev(x)::text from t`,
		"0.000000000000000000000000000000000000000050000000000000000000|0.000000000000000000007071067811865475244008443621048490392848",
	})
	for _, c := range cases {
		if got := memQuery(c.q); got != c.want {
			t.Errorf("%s\n got %q want %q", c.q, got, c.want)
		}
	}
	for _, q := range []string{tables + `select grp,name from t group by grp`, tables + `select grp from t group by grp having score > 1`, tables + `select grp from t group by grp order by name`} {
		if got := memQuery(q); !strings.Contains(got, "must appear in the GROUP BY clause") {
			t.Errorf("%s: %s", q, got)
		}
	}
}

func TestBugIssueAutocommit(t *testing.T) {
	c := &connectionImpl{}
	for _, v := range []string{"true", "false", "invalid"} {
		err := c.SetOption(context.Background(), adbc.OptionKeyAutoCommit, v)
		if v == "true" {
			if err != nil {
				t.Error(err)
			}
			continue
		}
		var ae adbc.Error
		want := adbc.StatusNotImplemented
		if v == "invalid" {
			want = adbc.StatusInvalidArgument
		}
		if !errors.As(err, &ae) || ae.Code != want {
			t.Errorf("%s: %v", v, err)
		}
	}
}

func TestSQLBugIssues(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`drop schema if exists it_bug_fixes cascade`)
	h.exec(`create schema it_bug_fixes`)
	t.Cleanup(func() { h.exec(`drop schema if exists it_bug_fixes cascade`) })
	h.exec(`create table it_bug_fixes.Foo (id integer primary key, "Id" integer, "id2" integer, score integer, name text)`)
	h.exec(`insert into it_bug_fixes.foo values(1,11,12,10,'a'),(2,21,22,null,'b'),(3,31,32,30,'c')`)
	h.expectRows(`select id,"Id" from it_bug_fixes.FOO order by id`, "1|11", "2|21", "3|31")
	h.expectError(`select * from it_bug_fixes."Foo"`, "does not exist")
	h.expectError(`create table it_bug_fixes.foo(x integer)`, "already exists")
	h.exec(`create table it_bug_fixes.quoted ("Id" integer,"id" integer)`)
	h.exec(`insert into it_bug_fixes.quoted values(1,2)`)
	h.expectRows(`select "Id",id from it_bug_fixes.quoted`, "1|2")
	h.exec(`create table it_bug_fixes.legacy_case ("AdvEngineID" integer)`)
	h.exec(`insert into it_bug_fixes.legacy_case values(7)`)
	key := metaKey("it_bug_fixes", "legacy_case")
	stored, err := h.rawClient().Get(h.ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal([]byte(stored), &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "folded_identifiers")
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	stored = string(b)
	if err := h.rawClient().Set(h.ctx, key, stored, 0).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`select advengineid from it_bug_fixes.legacy_case`, "7")
	h.expectRows(`select id from it_bug_fixes.foo order by score desc limit 1`, "2")
	h.expectRows(`select id,name from it_bug_fixes.foo group by id order by id`, "1|a", "2|b", "3|c")
	h.expectError(`select score,name from it_bug_fixes.foo group by score`, "must appear in the GROUP BY")
	for _, tail := range []string{`limit 2147483649`, `limit 9223372036854775807`, `order by id fetch first 3000000000 rows only`} {
		h.expectRows(`select id from it_bug_fixes.foo `+tail, "1", "2", "3")
	}
	h.expectRows(`select id from it_bug_fixes.foo limit 2147483647 offset 2`, "3")
	h.expectRows(`select id from it_bug_fixes.foo limit 1 offset 1000001`)
	h.expectRows(`select id from it_bug_fixes.foo order by score desc limit 10 offset 1000001`)
	h.exec(`create table it_bug_fixes.lit as select 1 as one,2147483648 as big`)
	schema := h.expectRows(`select * from it_bug_fixes.lit`, "1|2147483648")
	if schema.Field(0).Type.ID() != arrow.INT32 || schema.Field(1).Type.ID() != arrow.INT64 {
		t.Error(schema)
	}
	schema = h.expectRows(`select variance(score) from it_bug_fixes.foo`, "200.0000000000000000")
	if schema.Field(0).Type.ID() != arrow.DECIMAL128 && schema.Field(0).Type.ID() != arrow.DECIMAL256 {
		t.Error(schema)
	}
}

func TestSQLPaginationConfiguredCaps(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_bug_caps")
	h.exec("CREATE TABLE it_bug_caps(id INTEGER NOT NULL)")
	h.exec("INSERT INTO it_bug_caps VALUES(1),(2),(3),(4),(5),(6)")
	h.expectRows("SELECT id FROM it_bug_caps ORDER BY id LIMIT 20 OFFSET 3", "4", "5", "6")
	h.expectRows("SELECT id FROM it_bug_caps LIMIT 1 OFFSET 5", "6")
}
