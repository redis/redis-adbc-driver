// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestFactorial(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"factorial(0)", "1", "NUMERIC(76,0)"}, {"factorial(1::smallint)", "1", "NUMERIC(76,0)"},
		{"factorial(5)", "120", "NUMERIC(76,0)"}, {"factorial('20')", "2432902008176640000", "NUMERIC(76,0)"},
		{"factorial(56::bigint)", "710998587804863451854045647463724949736497978881168458687447040000000000000", "NUMERIC(76,0)"},
		{"factorial(NULL)", "NULL", "NUMERIC(76,0)"}, {"factorial(NULL::bigint)", "NULL", "NUMERIC(76,0)"},
		{"CASE WHEN FALSE THEN factorial(-1) ELSE 7 END", "7", "NUMERIC(76,0)"},
	})
}

func TestFactorialErrors(t *testing.T) {
	for _, tc := range []struct{ expr, state string }{
		{"factorial(-1)", stateNumericRange}, {"factorial(57)", stateNumericRange},
		{"factorial(32178)", stateNumericRange}, {"factorial(9223372036854775807)", stateNumericRange},
		{"factorial('bad')", stateInvalidText}, {"factorial('9223372036854775808')", stateNumericRange},
		{"factorial(1.0)", stateUndefinedFunction}, {"factorial(NULL::numeric)", stateUndefinedFunction},
		{"factorial(NULL::float8)", stateUndefinedFunction}, {"factorial('5'::text)", stateUndefinedFunction},
		{"factorial(true)", stateUndefinedFunction}, {"factorial()", stateUndefinedFunction},
		{"factorial(1,2)", stateUndefinedFunction},
	} {
		_, _, err := evalTestExpr(t, tc.expr)
		if sqlState(err) != tc.state {
			t.Errorf("%s: %v, want %s", tc.expr, err, tc.state)
		}
	}
}

func TestFactorialEmptyPlanning(t *testing.T) {
	for _, tc := range []struct{ expr, state string }{
		{"factorial(NULL::text) IS NULL", stateUndefinedFunction},
		{"factorial('bad')", stateInvalidText}, {"factorial(-1)", stateNumericRange},
	} {
		q := "SELECT " + tc.expr + " FROM generate_series(1,0)"
		stmts, err := ParseScript(q)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runInMemory(stmts[0].Stmt.(*SelectStmt))
		if sqlState(err) != tc.state {
			t.Errorf("%s: %v, want %s", q, err, tc.state)
		}
	}
}
