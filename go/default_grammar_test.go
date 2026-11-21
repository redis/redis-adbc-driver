// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestDefaultRestrictedGrammar(t *testing.T) {
	for _, expr := range []string{"1 IS NULL", "NULL::integer IS NORMALIZED", "true AND false", "true OR false", "NOT true", "1 IN (1)", "1 BETWEEN 0 AND 2", "'a' LIKE 'a'", "true IS TRUE", "1 ISNULL", "1 NOTNULL", "1 = ANY(ARRAY[1])", "1 = SOME(ARRAY[1])", "1 = ALL(ARRAY[1])", "TIMESTAMP '2024-01-01' AT TIME ZONE 'UTC'"} {
		for _, sql := range []string{"CREATE TABLE dg(v boolean DEFAULT " + expr + ")", "ALTER TABLE dg ADD v boolean DEFAULT " + expr} {
			if _, err := ParseScript(sql); sqlState(err) != stateSyntax {
				t.Errorf("%s: %v, want syntax error", sql, err)
			}
		}
	}
	for _, expr := range []string{"(1 IS NULL)", "(true AND false)", "(NOT true)", "(1 IN (1))", "1 IS DISTINCT FROM 2", "1 IS NOT DISTINCT FROM 2", "1 < 2", "1+2*3", "coalesce((1 IS NULL),false)", "CASE WHEN 1 IS NULL THEN true ELSE false END", "('a' LIKE 'a')"} {
		if _, err := ParseScript("CREATE TABLE dg(v boolean DEFAULT " + expr + ")"); err != nil {
			t.Errorf("%s: %v", expr, err)
		}
	}
	for _, suffix := range []string{"NOT NULL", "CHECK(v>0)", "REFERENCES parent(v)", "UNIQUE", `COLLATE "C"`} {
		stmts, err := ParseScript("CREATE TABLE dg(v integer DEFAULT 1 " + suffix + ")")
		if err != nil {
			t.Fatal(err)
		}
		if got := stmts[0].Stmt.(*CreateTableStmt).Columns[0].DefaultText; got != "1" {
			t.Errorf("constraint consumed: %q", got)
		}
	}
	for _, expr := range []string{"1 IS NULL", "true AND false", "NOT true"} {
		if _, err := ParseScript("ALTER TABLE dg ALTER v SET DEFAULT " + expr); err != nil {
			t.Fatal(err)
		}
	}
}
