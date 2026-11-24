// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

// Additional independent review cases: these execute only the in-memory unit
// fixture, with no driver endpoint, native library, or Redis process.
func TestDeferredUnionReviewNestedAliases(t *testing.T) {
	q := `WITH a(k,v) AS (VALUES (1,10),(1,11),(2,20),(NULL,30)) SELECT x.key,x.amount FROM (SELECT k,v FROM a UNION ALL SELECT k,v FROM a UNION ALL SELECT k,v FROM a) AS x(key,amount) WHERE x.key=1 ORDER BY x.amount`
	if got := memQuery(q); got != "1|10\n1|10\n1|10\n1|11\n1|11\n1|11" {
		t.Fatal(got)
	}
	q = `WITH a(k,v) AS (VALUES (1,10),(2,20)), u AS (SELECT k,v FROM a UNION ALL SELECT k,v FROM a), p AS (SELECT k AS key,v AS amount FROM u) SELECT p.key,SUM(p.amount) FROM p JOIN (VALUES (1),(1),(3)) AS d(key) ON p.key=d.key GROUP BY p.key`
	if got := memQuery(q); got != "1|40" {
		t.Fatal(got)
	}
}

func TestDeferredUnionReviewDateDemandAndOuterSides(t *testing.T) {
	prefix := `WITH a(d,v) AS (VALUES (DATE '2000-01-01',10),(DATE '2000-01-02',20),(CAST(NULL AS DATE),30)), dims(d) AS (VALUES (DATE '2000-01-01'),(DATE '2000-01-03'),(CAST(NULL AS DATE))) `
	for _, tt := range []struct{ tail, want string }{
		{`SELECT COUNT(*) FROM dims LEFT JOIN (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u ON dims.d=u.d`, "4"},
		{`SELECT COUNT(*) FROM dims RIGHT JOIN (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u ON dims.d=u.d`, "6"},
		{`SELECT COUNT(*) FROM dims FULL JOIN (SELECT d,v FROM a UNION ALL SELECT d,v FROM a) u ON dims.d=u.d`, "8"},
	} {
		if got := memQuery(prefix + tt.tail); got != tt.want {
			t.Fatalf("%s: %s", tt.tail, got)
		}
	}
}

func TestDeferredUnionReviewNullConstantsAndResidual(t *testing.T) {
	for _, tt := range []struct{ q, want string }{
		{`WITH a(k) AS (VALUES (1),(2)) SELECT COUNT(*) FROM (SELECT k,NULL AS n FROM a UNION ALL SELECT k,NULL FROM a) u WHERE k=1 AND n IS NULL`, "2"},
		{`WITH a(k,v) AS (VALUES (1,10),(1,20),(2,30)) SELECT SUM(v) FROM (SELECT k,v FROM a UNION ALL SELECT k,v FROM a) u WHERE k=1 AND v+1=21`, "40"},
		{`WITH a(k,v) AS (VALUES (1,10),(2,20)) SELECT COUNT(*) FROM (SELECT k,v FROM a UNION ALL SELECT k,v FROM a) u WHERE k=1 OR v+1=21`, "4"},
	} {
		if got := memQuery(tt.q); got != tt.want {
			t.Fatalf("%s: %s", tt.q, got)
		}
	}
}

func TestDeferredUnionReviewRenameDoesNotMutateOriginal(t *testing.T) {
	_, p := deferredTestPlan(t, `WITH a(k,v) AS (VALUES (1,10)) SELECT * FROM (SELECT k,v FROM a UNION ALL SELECT k,v FROM a) u`)
	original := p.meta
	renamed, err := renameColumns(original, "u", []string{"newkey"})
	if err != nil {
		t.Fatal(err)
	}
	if original.view.names[0] != "k" || renamed.view.names[0] != "newkey" || renamed.view == original.view {
		t.Fatal("alias mutated or reused shared deferred output mapping")
	}
}
