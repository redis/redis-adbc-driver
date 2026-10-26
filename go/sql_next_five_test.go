// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"reflect"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestNextSQLInMemory(t *testing.T) {
	for _, c := range []struct{ sql, want string }{
		{"select date('2099-12-31')", "2099-12-31"},
		{"select date(timestamp '2023-09-02 10:00:00')", "2023-09-02"},
		{"select count(*)", "1"},
		{"select count(*) where false", "0"},
		{"select sum(1), max(2)", "1|2"},
		{"select count(*) having count(*) = 1", "1"},
		{"select count(*) having false", ""},
		{"select count(*) group by ()", "1"},
		{"select count(*) group by grouping sets ((), ())", "1\n1"},
		{"select count(*) where false group by 1+0", ""},
		{"values (1,'a'),(2,'b')", "1|a\n2|b"},
		{"select * from (values (1,'a'),(2,'b')) v(n,s) order by n desc", "2|b\n1|a"},
		{"values (null),('2'),(1)", "NULL\n2\n1"},
		{"values (3),(1),(2) order by column1 limit 1+1 offset 1", "2\n3"},
		{"with v(x) as (values (1),(2)) select sum(x) from v", "3"},
		{"values (1) union all values (2)", "1\n2"},
		{"select 1 where 1 in (values (1),(2))", "1"},
		{"select 1 limit all offset null", "1"},
		{"select 1 limit null", "1"},
		{"values (1),(2),(3) limit 1.5", "1\n2"},
		{"with n(x) as (values (1)) select 1 limit (select x from n)", "1"},
		{"select 1 limit 0", ""},
		{"select 1 fetch first (1+1) rows only", "1"},
		{"select 1 offset 1 rows fetch next 1 row only", ""},
		{"select 1 limit (select 1)", "1"},
	} {
		t.Run(c.sql, func(t *testing.T) {
			if got := memQuery(c.sql); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNextPaginationErrorsAndRebinding(t *testing.T) {
	for _, c := range []struct{ sql, state string }{
		{"select 1 limit -1", "2201W"}, {"select 1 offset -1", "2201X"},
		{"select 1 limit true", "42804"},
		{"select 1 limit x", "42P10"}, {"select 1 limit count(*)", "42803"},
		{"values (1),(1,2)", "42601"},
		{"values (true),(1)", "42804"},
	} {
		ps, err := ParseScript(c.sql)
		if err == nil {
			e := &executor{}
			_, err = e.planSelect(context.Background(), ps[0].Stmt.(*SelectStmt), nil)
		}
		if got := sqlState(err); got != c.state {
			t.Errorf("%s: %v (%s), want %s", c.sql, err, got, c.state)
		}
	}
	ps, err := ParseScript("select 1 limit ?")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{}
	for _, n := range []int64{0, 1, 0, 2} {
		e.params = []Value{intValue(typeInt64, n)}
		p, err := e.planSelect(context.Background(), ps[0].Stmt.(*SelectStmt), []ColType{typeInt64})
		if err != nil {
			t.Fatal(err)
		}
		if p.sel.Limit == nil || *p.sel.Limit != n {
			t.Fatal("stale limit")
		}
	}
	if ps[0].Stmt.(*SelectStmt).Limit != nil {
		t.Fatal("mutated cached statement")
	}
}

func TestNextSQLIntegration(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("CREATE TABLE it_next_five (id integer, grp text)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_next_five") })
	h.exec("INSERT INTO it_next_five VALUES (1,'a'),(2,'b'),(3,'c')")
	h.expectRows("select id from it_next_five order by id limit all", "1", "2", "3")
	h.expectRows("select id from it_next_five order by id limit 1+1 offset 1", "2", "3")
	h.expectRows("select id from it_next_five where (id,grp) in (values (1,'a'))", "1")
	h.exec("SET TIME ZONE 'America/Los_Angeles'")
	h.expectRows("select date(timestamptz '2023-09-02 01:00:00+00')", "2023-09-01")
	h.expectRows("select date(null::timestamp)", "NULL")
	h.expectError("select date('not-a-date')", "invalid input syntax")
	h.exec("CREATE VIEW it_next_values_zone AS VALUES (TIMESTAMPTZ '2024-01-01 00:00+00'),('2024-01-02 00:00')")
	t.Cleanup(func() { h.exec("DROP VIEW IF EXISTS it_next_values_zone") })
	h.exec("SET TIME ZONE 'UTC'")
	h.expectRows("SELECT column1 FROM it_next_values_zone ORDER BY column1", "2024-01-01T00:00:00Z", "2024-01-02T08:00:00Z")

	// Schema inspection must type counts without evaluating an unbound value.
	types, err := h.paramTypes("select id from it_next_five limit ?::integer offset ?")
	if err != nil || !reflect.DeepEqual(types, []string{"int32", "int64"}) {
		t.Fatalf("parameter schema: %v %v", types, err)
	}
	if _, err := h.executeSchema("select id from it_next_five limit 1/0"); err != nil {
		t.Fatal(err)
	}
	// Rebind the same prepared statement, preserving its cached parsed AST.
	st := h.stmtWith("select id from it_next_five order by id limit ?::integer offset ?", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int64{2, 0, 1} {
		batch := nextPaginationBatch(n)
		err := st.Bind(h.ctx, batch)
		batch.Release()
		if err != nil {
			t.Fatal(err)
		}
		r, _, err := st.ExecuteQuery(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		var rows int64
		for r.Next() {
			rows += r.RecordBatch().NumRows()
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		r.Release()
		if rows != n {
			t.Fatalf("limit %d: got %d rows", n, rows)
		}
	}
}

func nextPaginationBatch(n int64) arrow.RecordBatch {
	b := array.NewInt64Builder(memory.DefaultAllocator)
	defer b.Release()
	b.Append(n)
	a := b.NewArray()
	defer a.Release()
	b.Append(1)
	o := b.NewArray()
	defer o.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "limit", Type: arrow.PrimitiveTypes.Int64}, {Name: "offset", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{a, o}, 1)
}
