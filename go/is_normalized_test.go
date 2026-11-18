// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"testing"
)

func TestIsNormalized(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"'ä' IS NORMALIZED", "false", "BOOLEAN"},
		{"'ä' IS NOT NORMALIZED", "true", "BOOLEAN"},
		{"'ä' IS NFD NORMALIZED", "true", "BOOLEAN"},
		{"'ä' IS NOT NFD NORMALIZED", "true", "BOOLEAN"},
		{"'ﬃ' IS NFC NORMALIZED", "true", "BOOLEAN"},
		{"'ﬃ' IS NFKC NORMALIZED", "false", "BOOLEAN"},
		{"NULL IS NORMALIZED", "NULL", "BOOLEAN"},
		{"NULL IS NOT NFKD NORMALIZED", "NULL", "BOOLEAN"},
		{"is_normalized('ä','nfd')", "true", "BOOLEAN"},
		{"pg_catalog.is_normalized('ä')", "true", "BOOLEAN"},
		{"is_normalized(NULL,'bad')", "NULL", "BOOLEAN"},
		{"is_normalized('a',NULL)", "NULL", "BOOLEAN"},
		{"'ä  '::char(5) IS NFD NORMALIZED", "true", "BOOLEAN"},
		{"'a' || '̈' IS NOT NORMALIZED AND true", "true", "BOOLEAN"},
		{"('ä' IS NORMALIZED) IS TRUE", "true", "BOOLEAN"},
	})
	for _, s := range []string{"a" + strings.Repeat("\u0315", 40), "a\u1acf\u0300", "\U00011383"} {
		value, typ, err := evalTestExpr(t, "'"+s+"' IS NORMALIZED")
		if err != nil || value != "true" || typ != "BOOLEAN" {
			t.Errorf("long/version boundary: %q %s %v", value, typ, err)
		}
	}
}

func TestIsNormalizedErrors(t *testing.T) {
	for _, tc := range []struct{ expr, state string }{
		{"1 IS NORMALIZED", stateUndefinedFunction},
		{"NULL::integer IS NOT NORMALIZED", stateUndefinedFunction},
		{"is_normalized()", stateUndefinedFunction},
		{"is_normalized('x','NFC','extra')", stateUndefinedFunction},
		{"is_normalized('x','bad')", stateInvalidParameter},
		{"is_normalized('x',NULL::integer)", stateUndefinedFunction},
	} {
		_, _, err := evalTestExpr(t, tc.expr)
		if sqlState(err) != tc.state {
			t.Errorf("%s: %v, want %s", tc.expr, err, tc.state)
		}
	}
	for _, sql := range []string{"SELECT 'x' IS 'NFC' NORMALIZED", "SELECT 'x' IS WRONG NORMALIZED", "SELECT 'x' IS NFC", "SELECT 'x' IS NOT NORMALIZED NFC NFD"} {
		_, err := ParseScript(sql)
		if sqlState(err) != stateSyntax {
			t.Errorf("%s: %v", sql, err)
		}
	}
	for _, sql := range []string{"SELECT NULL::integer IS NORMALIZED WHERE FALSE", "SELECT (1 IS NORMALIZED) IS NULL FROM generate_series(1,0)", "SELECT CASE WHEN FALSE THEN 1 IS NORMALIZED ELSE TRUE END"} {
		stmts, err := ParseScript(sql)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runInMemory(stmts[0].Stmt.(*SelectStmt))
		if sqlState(err) != stateUndefinedFunction {
			t.Errorf("%s: %v", sql, err)
		}
	}
	runScalarCases(t, []scalarCase{{"CASE WHEN FALSE THEN is_normalized('x','bad') ELSE TRUE END", "true", "BOOLEAN"}})
}
