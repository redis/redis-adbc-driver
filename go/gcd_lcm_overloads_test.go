// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"testing"
)

func TestGCDLCMOverloads(t *testing.T) {
	for _, f := range []string{"gcd", "lcm"} {
		for _, tc := range []struct{ args, state string }{
			{"NULL,NULL", stateAmbiguousFunction}, {"'12','18'", stateAmbiguousFunction},
			{"1::smallint,2::smallint", stateAmbiguousFunction},
			{"NULL::smallint,NULL::smallint", stateAmbiguousFunction},
			{"NULL::text,1::integer", stateUndefinedFunction},
			{"NULL::numeric,true", stateUndefinedFunction},
			{"NULL::float8,1::integer", stateUndefinedFunction},
			{"NULL::integer,'bad'", stateInvalidText},
			{"1::integer,'2147483648'", stateNumericRange},
		} {
			_, _, err := evalTestExpr(t, f+"("+tc.args+")")
			if sqlState(err) != tc.state {
				t.Errorf("%s(%s): %v, want %s", f, tc.args, err, tc.state)
			}
		}
	}
	runScalarCases(t, []scalarCase{
		{"gcd(12::smallint,18::integer)", "6", "INTEGER"},
		{"lcm(12::integer,18::bigint)", "36", "BIGINT"},
		{"gcd(12::integer,'18')", "6", "INTEGER"},
		{"lcm('12',18::bigint)", "36", "BIGINT"},
		{"gcd(NULL::integer,18::integer)", "NULL", "INTEGER"},
		{"lcm(NULL::bigint,18::integer)", "NULL", "BIGINT"},
		{"gcd(1.2::numeric(5,1),'0.450')", "0.150", "NUMERIC(76,3)"},
		{"lcm('0.450',1.2::numeric(5,1))", "3.600", "NUMERIC(76,3)"},
	})
}

func TestGCDLCMWrappedPlanningErrors(t *testing.T) {
	for _, pattern := range []string{"%s IS NULL", "CAST(%s AS TEXT)", "CASE WHEN FALSE THEN %s ELSE 1 END"} {
		for _, f := range []string{"gcd", "lcm"} {
			expr := parseTestExpr(t, fmt.Sprintf(pattern, f+"(1::smallint,2::smallint)"))
			err := checkUnknowns(expr, func() map[string]ColType { return nil }, nil)
			if sqlState(err) != stateAmbiguousFunction {
				t.Errorf("%s: %v", pattern, err)
			}
			if err := checkCalls(expr); sqlState(err) != stateAmbiguousFunction {
				t.Errorf("unbound/default %s: %v", pattern, err)
			}
		}
	}
}
