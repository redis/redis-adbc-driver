// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"testing"
)

func TestUnicodeFunctions(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"normalize('ä')", "ä", "VARCHAR"},
		{"normalize('ä', NFD)", "ä", "VARCHAR"},
		{"normalize('ﬃ', NFKC)", "ffi", "VARCHAR"},
		{"normalize('①', NFKD)", "1", "VARCHAR"},
		{"pg_catalog.normalize('ä','nfd')", "ä", "VARCHAR"},
		{`"normalize"('ä','NFD')`, "ä", "VARCHAR"},
		{"normalize('ä  '::char(5))", "ä", "VARCHAR"},
		{"normalize(NULL)", "NULL", "VARCHAR"},
		{"pg_catalog.normalize(NULL,'bad')", "NULL", "VARCHAR"},
		{`unistr('d\0061t\+000061')`, "data", "VARCHAR"},
		{`unistr('\uD83D\U0000DE00')`, "😀", "VARCHAR"},
		{`unistr('\\u0061')`, `\u0061`, "VARCHAR"},
		{"unistr('x '::char(4))", "x", "VARCHAR"},
		{"unistr(NULL)", "NULL", "VARCHAR"},
	})
}

func TestUnicodeErrors(t *testing.T) {
	for _, tc := range []struct{ expr, state string }{
		{"normalize(1)", stateUndefinedFunction},
		{"unistr()", stateUndefinedFunction}, {"unistr('a','b')", stateUndefinedFunction},
		{"pg_catalog.normalize()", stateUndefinedFunction}, {"pg_catalog.normalize('a','NFC','x')", stateUndefinedFunction}, {"unistr(NULL::integer)", stateUndefinedFunction},
		{"pg_catalog.normalize('x',NULL::integer)", stateUndefinedFunction},
		{`unistr('\xyz')`, stateSyntax}, {`unistr('\D800')`, stateSyntax},
		{`unistr('\DC00')`, stateSyntax}, {`unistr('\D800x\DC00')`, stateSyntax},
		{`unistr('\0000')`, stateInvalidParameter}, {`unistr('\+110000')`, stateInvalidParameter},
		{"pg_catalog.normalize('x','bad')", stateInvalidParameter},
	} {
		_, _, err := evalTestExpr(t, tc.expr)
		if sqlState(err) != tc.state {
			t.Errorf("%s: %v, want %s", tc.expr, err, tc.state)
		}
	}
	for _, q := range []string{"normalize('x','NFD')", "normalize('x',wrong)", "normalize('x',$1)"} {
		_, err := ParseScript("SELECT " + q)
		if sqlState(err) != stateSyntax {
			t.Errorf("%s: %v", q, err)
		}
	}
}

func TestUnicodeEmptyAndLazy(t *testing.T) {
	for _, tc := range []struct{ sql, state string }{
		{`SELECT unistr('\xyz') FROM generate_series(1,0)`, stateSyntax},
		{`SELECT unistr(NULL::integer) IS NULL FROM generate_series(1,0)`, stateUndefinedFunction},
		{`SELECT pg_catalog.normalize('x','bad') WHERE FALSE`, stateInvalidParameter},
	} {
		stmts, err := ParseScript(tc.sql)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runInMemory(stmts[0].Stmt.(*SelectStmt))
		if sqlState(err) != tc.state {
			t.Errorf("%s: %v", tc.sql, err)
		}
	}
	runScalarCases(t, []scalarCase{{`CASE WHEN FALSE THEN unistr('\xyz') ELSE 'ok' END`, "ok", "VARCHAR"}})
	// Beyond the stream-safe limit: PostgreSQL preserves/reorders all marks and
	// does not insert U+034F. Include an existing U+034F, which must be retained.
	s := "a" + strings.Repeat("\u0315", 40) + "\u0300"
	expr := "normalize('" + s + "')"
	v, _, err := evalTestExpr(t, expr)
	want := "à" + strings.Repeat("\u0315", 40)
	if err != nil || v != want {
		t.Fatalf("long sequence: %q %v", v, err)
	}
}

func TestUnicodeInvalidText(t *testing.T) {
	for _, name := range []string{"NORMALIZE", "UNISTR"} {
		for _, s := range []string{string([]byte{'a', 0, 'b'}), string([]byte{0xff})} {
			_, err := pgUnicode(&Func{Name: name}, []Value{stringValue(s)})
			if sqlState(err) != "22021" {
				t.Errorf("%s invalid UTF8/NUL: %v", name, err)
			}
		}
	}
}
