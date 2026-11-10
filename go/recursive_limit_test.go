// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"
	"testing"
)

func TestRecursiveConsumerLimit(t *testing.T) {
	old := maxRecursion
	maxRecursion = 12
	defer func() { maxRecursion = old }()
	for _, op := range []string{"UNION", "UNION ALL"} {
		q := `WITH RECURSIVE t(n) AS (SELECT 1 ` + op + ` SELECT n+1 FROM t) SELECT n FROM t LIMIT 3 OFFSET 2`
		if got := memQuery(q); got != "3\n4\n5" {
			t.Fatalf("%s: %s", op, got)
		}
	}
}

func TestRecursiveConsumerBoundaries(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t) SELECT * FROM t LIMIT 0`, ""},
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION SELECT n FROM t) SELECT * FROM t LIMIT 4`, "1"},
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n FROM t) SELECT t.n FROM t LIMIT 4`, "1\n1\n1\n1"},
		{`WITH RECURSIVE t(n) AS (SELECT 1 WHERE false UNION ALL SELECT n+1 FROM t) SELECT * FROM t LIMIT 4`, ""},
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<3) SELECT * FROM t LIMIT 8 OFFSET 1`, "2\n3"},
		{`WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n/0 FROM t) SELECT * FROM t LIMIT 1`, "1"},
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<8) SELECT * FROM t ORDER BY n DESC LIMIT 2`, "8\n7"},
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<8) SELECT COUNT(*) FROM t LIMIT 1`, "8"},
		{`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<8) SELECT * FROM t WHERE n>6 LIMIT 2`, "7\n8"},
	} {
		if got := memQuery(tc.sql); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.sql, got, tc.want)
		}
	}
}

func TestRecursiveConsumerKeepsFullCache(t *testing.T) {
	stmts, err := ParseScript(`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<8) SELECT * FROM t LIMIT 2`)
	if err != nil {
		t.Fatal(err)
	}
	sel := stmts[0].Stmt.(*SelectStmt)
	e := &executor{cache: newExecCache()}
	ctx := context.Background()
	p, err := e.planSelect(ctx, sel, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.runSelect(ctx, p, nil)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	if _, ok := e.cache.ctes[&sel.With[0]]; ok {
		t.Fatal("bounded result entered shared CTE cache")
	}
	sel.Limit, sel.LimitExpr = nil, nil
	p, err = e.planSelect(ctx, sel, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = e.runSelect(ctx, p, nil)
	if err != nil || len(rows) != 8 {
		t.Fatal("subsequent full consumer", rows, err)
	}
}

func TestRecursiveConsumerPreservesCapsAndCancellation(t *testing.T) {
	old := maxRecursion
	maxRecursion = 4
	defer func() { maxRecursion = old }()
	prefix := `WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t) `
	for _, consumer := range []string{"SELECT * FROM t ORDER BY n LIMIT 1", "SELECT * FROM t WHERE n>0 LIMIT 1", "SELECT COUNT(*) FROM t LIMIT 1", "SELECT * FROM t a JOIN t b USING(n) LIMIT 1", "SELECT n,(SELECT MAX(n) FROM t) FROM t LIMIT 1", "SELECT * FROM t LIMIT 20"} {
		if got := memQuery(prefix + consumer); !strings.Contains(got, "did not finish after 4 iterations") {
			t.Fatalf("%s: %s", consumer, got)
		}
	}
	stmts, err := ParseScript(prefix + "SELECT * FROM t LIMIT 3")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := &executor{cache: newExecCache()}
	if _, err = e.planSelect(ctx, stmts[0].Stmt.(*SelectStmt), nil); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestRecursiveConsumerParameters(t *testing.T) {
	stmts, err := ParseScript(`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t) SELECT * FROM t LIMIT ? OFFSET ?`)
	if err != nil {
		t.Fatal(err)
	}
	sel := stmts[0].Stmt.(*SelectStmt)
	e := &executor{cache: newExecCache(), paramTypes: []ColType{typeInt64, typeInt64}}
	for _, counts := range [][2]int64{{1, 0}, {3, 2}, {0, 0}} {
		e.params = []Value{intValue(typeInt64, counts[0]), intValue(typeInt64, counts[1])}
		p, err := e.planSelect(context.Background(), sel, e.paramTypes)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := e.runSelect(context.Background(), p, e.params)
		if err != nil || int64(len(rows)) != counts[0] {
			t.Fatal(counts, rows, err)
		}
		for i, row := range rows {
			if row[0].I != counts[1]+int64(i)+1 {
				t.Fatal(counts, rows)
			}
		}
	}
}

func TestRecursiveConsumerRowCap(t *testing.T) {
	old := maxRecursiveRows
	maxRecursiveRows = 4
	defer func() { maxRecursiveRows = old }()
	prefix := `WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t) SELECT * FROM t `
	if got := memQuery(prefix + "LIMIT 4"); got != "1\n2\n3\n4" {
		t.Fatal(got)
	}
	if got := memQuery(prefix + "LIMIT 5"); !strings.Contains(got, "returned more than 4 rows") {
		t.Fatal(got)
	}
	if got := memQuery(prefix + "LIMIT 1 OFFSET 4"); !strings.Contains(got, "returned more than 4 rows") {
		t.Fatal(got)
	}
}
