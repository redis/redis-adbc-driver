// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"testing"
)

func TestGroupedJoinAliases(t *testing.T) {
	const tables = "WITH a(i,t) AS (VALUES (1,'one'),(2,'two')), b(i,k) AS (VALUES (1,10),(3,30)) "
	for _, tc := range []struct{ sql, want string }{
		{"SELECT ii,tt,kk FROM (a CROSS JOIN b) AS tx(ii,tt,ii2,kk) ORDER BY ii,kk", "1|one|10\n1|one|30\n2|two|10\n2|two|30"},
		{"SELECT tx.ii,tx.tt,tx.kk FROM (a t1(x,y) CROSS JOIN b t2(z,w)) tx(ii,tt,ii2,kk) ORDER BY ii,kk", "1|one|10\n1|one|30\n2|two|10\n2|two|30"},
		{"SELECT * FROM a JOIN b USING(i) AS x WHERE a.t='one'", "1|one|10"},
		{"SELECT * FROM a JOIN b USING(i) AS x WHERE x.i=1", "1|one|10"},
		{"SELECT x.* FROM a FULL JOIN b USING(i) AS x ORDER BY x.i", "1\n2\n3"},
		{"SELECT a.i,b.i FROM (a FULL JOIN b USING(i)) ORDER BY COALESCE(a.i,b.i)", "1|1\n2|NULL\nNULL|3"},
		{"SELECT * FROM (a LEFT JOIN b USING(i)) x ORDER BY i", "1|one|10\n2|two|NULL"},
		{"SELECT a.i FROM a JOIN b USING(i) AS a", "error: table name \"a\" specified more than once"},
		{"SELECT a.i FROM (a JOIN b USING(i)) x", "error: missing FROM-clause entry for table \"a\""},
		{"SELECT x.t FROM a JOIN b USING(i) AS x", "error: column \"x.t\" does not exist"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			if got := memQuery(tables + tc.sql); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWholeRowReferences(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT foo FROM (SELECT 1 OFFSET 0) foo", "(1)"},
		{"SELECT foo FROM (SELECT NULL OFFSET 0) foo", "()"},
		{"SELECT foo FROM (SELECT 'xyzzy',1,NULL OFFSET 0) foo", "(xyzzy,1,)"},
		{"SELECT to_json(t), (t).n FROM (SELECT 1 n, 'x' v) t", `{"n":1,"v":"x"}|1`},
		{"SELECT foo FROM (SELECT 1 foo) foo", "1"},
		{"SELECT row_to_json(1) WHERE false", "error: function row_to_json(integer) does not exist"},
		{"SELECT (SELECT t) FROM (VALUES (1),(2)) t(i) ORDER BY i", "(1)\n(2)"},
		{"SELECT a,b,b IS NULL FROM (VALUES (1),(2)) a(i) LEFT JOIN (VALUES (1,NULL)) b(i,v) USING(i) ORDER BY i", "(1)|(1,)|false\n(2)|NULL|true"},
		{"SELECT b, to_json(b) FROM (VALUES(1)) a(i) LEFT JOIN (VALUES(NULL::int)) b(v) ON true", `()|{"v":null}`},
		{"SELECT x.*,ROW(x.*),row_to_json(x.*) FROM (VALUES(1,'one')) a(i,t) JOIN (VALUES(1,10)) b(i,k) USING(i) AS x", `1|(1)|{"i":1}`},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			if got := memQuery(tc.sql); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGroupedJoinAssociation(t *testing.T) {
	const cte = "WITH a(i) AS(VALUES(1),(2)), b(i) AS(VALUES(1)), c(i) AS(VALUES(3)) "
	for _, tc := range []struct{ sql, want string }{
		{"SELECT * FROM ((a LEFT JOIN b USING(i)) FULL JOIN c USING(i)) ORDER BY i", "1\n2\n3"},
		{"SELECT a.i,b.i,c.i FROM a LEFT JOIN (b JOIN c USING(i)) ON a.i=b.i ORDER BY a.i", "1|NULL|NULL\n2|NULL|NULL"},
		{"SELECT * FROM (a LEFT JOIN b USING(i)) JOIN c ON a.i=c.i", ""},
		{"SELECT * FROM (a LEFT JOIN b USING(i)) x JOIN (VALUES(2)) d(i) USING(i)", "2"},
		{"SELECT (SELECT a.i FROM (b JOIN c ON b.i=a.i)) FROM a ORDER BY i", "1\nNULL"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			if got := memQuery(cte + tc.sql); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestWholeRowNullPresenceAndGrouping(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT (SELECT b) FROM (VALUES(1),(2)) a(i) LEFT JOIN (VALUES(1,NULL::int)) b(i,v) USING(i) ORDER BY i", "(1,)\nNULL"},
		{"SELECT x FROM (VALUES(1),(2)) a(i) LEFT JOIN LATERAL (SELECT a.i) x ON a.i=1 ORDER BY a.i", "(1)\nNULL"},
		{"SELECT ROW(x.*) FROM (VALUES(1)) a(i) CROSS JOIN generate_series(1,2) x", "(1)\n(2)"},
		{"SELECT to_json(b) FROM ((VALUES(1),(2)) a(i) LEFT JOIN (VALUES(1,NULL::int)) b(i,v) USING(i)) ORDER BY i", `{"i":1,"v":null}` + "\nNULL"},
		{"SELECT to_json(x) FROM (VALUES(1),(2)) a(i) FULL JOIN (VALUES(1),(3)) b(i) USING(i) AS x ORDER BY x.i", "{\"i\":1}\n{\"i\":2}\n{\"i\":3}"},
		{"SELECT t,count(*) FROM (VALUES(1),(1),(2)) t(i) GROUP BY t ORDER BY t", "(1)|2\n(2)|1"},
		{"SELECT t,count(*) FROM (VALUES(1),(1),(2)) t(i) GROUP BY i ORDER BY i", "(1)|2\n(2)|1"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			if got := memQuery(tc.sql); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestQualifiedStarsRetainPublicNames(t *testing.T) {
	ctx := context.Background()
	for _, sql := range []string{
		"SELECT a.* FROM (VALUES(1,'x')) a(i,v) CROSS JOIN (VALUES(2)) b(j)",
		"SELECT x.* FROM (VALUES(1)) a(i) FULL JOIN (VALUES(2)) b(i) USING(i) AS x",
	} {
		parsed, err := ParseScript(sql)
		if err != nil {
			t.Fatal(err)
		}
		e := &executor{cache: newExecCache()}
		plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
		if err != nil {
			t.Fatal(err)
		}
		for i, c := range plan.columns() {
			want := []string{"i", "v"}[i]
			if c.Name != want {
				t.Fatalf("%s column %d = %q, want %q", sql, i, c.Name, want)
			}
		}
	}
}

func TestWholeRowArrowAndReplanning(t *testing.T) {
	ctx := context.Background()
	parsed, err := ParseScript("SELECT t FROM (SELECT NULL v) t")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{cache: newExecCache()}
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	typ := plan.columns()[0].Type
	if typ.namedRecord() || !typ.Rec.query {
		t.Fatal("whole row became a stored struct")
	}
	fields := typ.ArrowType().(*arrow.StructType).Fields()
	if len(fields) != 1 || fields[0].Name != "v" || fields[0].Type.ID() != arrow.STRING {
		t.Fatalf("wrong fields: %v", fields)
	}
	rows, err := e.runSelect(ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := buildRecord(memory.DefaultAllocator, plan.columns(), rows)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Release()
	if rec.Column(0).IsNull(0) || !rec.Column(0).(*array.Struct).Field(0).IsNull(0) {
		t.Fatal("present NULL field became a NULL record")
	}

	parsed, err = ParseScript("WITH t(a,b) AS(VALUES(1,NULL::int)) SELECT ROW(t.*),ROW(t.*)=ROW(1,NULL::int),ROW(t.*) IS NULL,ROW(t.*) IS NOT DISTINCT FROM ROW(1,NULL::int) FROM t")
	if err != nil {
		t.Fatal(err)
	}
	sel := parsed[0].Stmt.(*SelectStmt)
	for i := 0; i < 2; i++ {
		if got := memRun(sel); got != "(1,)|NULL|false|true" {
			t.Fatalf("execution %d: %s", i, got)
		}
	}
	parsed, err = ParseScript("WITH t(a,b) AS(VALUES(1,2)) SELECT ROW(t.*) FROM t")
	if err != nil {
		t.Fatal(err)
	}
	sel = parsed[0].Stmt.(*SelectStmt)
	if got := memRun(sel); got != "(1,2)" {
		t.Fatal(got)
	}
	replacement, err := ParseScript("VALUES(3,4,5)")
	if err != nil {
		t.Fatal(err)
	}
	sel.With[0].Columns = []string{"x", "y", "z"}
	sel.With[0].Select = replacement[0].Stmt.(*SelectStmt)
	if got := memRun(sel); got != "(3,4,5)" {
		t.Fatalf("star did not rebind: %s", got)
	}
	if got := memQuery("SELECT ROW(t.*) FROM (SELECT 1 a,2 a) t"); got != "(1,2)" {
		t.Fatalf("duplicate fields: %s", got)
	}
}

// Scalar partition reducers read physical column keys. A whole-row value
// requires the ordinary expression evaluator, including its presence semantics.
func TestWholeRowAggregateAdmission(t *testing.T) {
	parsed, err := ParseScript("WITH t(a) AS(VALUES(1),(2)) SELECT COUNT(t) FROM t")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{cache: newExecCache()}
	plan, err := e.planSelect(context.Background(), parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	if parallelAggregates(plan.aggregateCalls(), plan.meta.types()) {
		t.Fatal("whole-row aggregate entered the scalar partition reducer")
	}
	rows, err := e.runSelect(context.Background(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0][0].I != 2 {
		t.Fatalf("COUNT(t)=%v", rows)
	}
}
