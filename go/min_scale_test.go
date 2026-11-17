// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"math/big"
	"testing"
)

func TestMinScale(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"min_scale(1.23000)", "2", "INTEGER"},
		{"min_scale(-1.23000)", "2", "INTEGER"},
		{"min_scale(0.000)", "0", "INTEGER"},
		{"min_scale(1200.000)", "0", "INTEGER"},
		{"min_scale(0.00000100)", "6", "INTEGER"},
		{"min_scale(42::smallint)", "0", "INTEGER"},
		{"min_scale(42::integer)", "0", "INTEGER"},
		{"min_scale(9223372036854775807::bigint)", "0", "INTEGER"},
		{"min_scale('1.23000')", "2", "INTEGER"},
		{"min_scale('1e-1000')", "1000", "INTEGER"},
		{"min_scale('1e1000')", "0", "INTEGER"},
		{"min_scale('1_000.001_00')", "3", "INTEGER"},
		{"min_scale(NULL)", "NULL", "INTEGER"},
		{"min_scale(NULL::numeric)", "NULL", "INTEGER"},
	})
}

func TestMinScaleResolutionBeforeRows(t *testing.T) {
	for _, expr := range []string{"min_scale('1'::text)", "min_scale(1::float8)", "min_scale(NULL::float4)", "min_scale(NULL::text)", "min_scale(true)", "min_scale(ARRAY[1])"} {
		_, _, err := evalTestExpr(t, expr)
		if sqlState(err) != stateUndefinedFunction {
			t.Errorf("%s: %v", expr, err)
		}
		stmts, err := ParseScript("SELECT " + expr + " FROM generate_series(1,0)")
		if err != nil {
			t.Fatal(err)
		}
		_, err = runInMemory(stmts[0].Stmt.(*SelectStmt))
		if sqlState(err) != stateUndefinedFunction {
			t.Errorf("empty %s: %v", expr, err)
		}
	}
	for _, q := range []string{"min_scale('bad')", "min_scale('1e-16384')"} {
		stmts, err := ParseScript("SELECT " + q + " FROM generate_series(1,0)")
		if err != nil {
			t.Fatal(err)
		}
		_, err = runInMemory(stmts[0].Stmt.(*SelectStmt))
		want := stateInvalidText
		if q == "min_scale('1e-16384')" {
			want = stateNumericRange
		}
		if sqlState(err) != want {
			t.Errorf("%s: %v, want %s", q, err, want)
		}
	}
}

func TestMinScaleBoundValues(t *testing.T) {
	expr := parseTestExpr(t, "min_scale($1)")
	for _, v := range []Value{decimalValue(big.NewInt(-123000), 76, 5), decimalValue(big.NewInt(123), 76, -2), decimalValue(big.NewInt(0), 76, 76), nullValue(decimalType(76, 6)), intValue(typeInt64, 42)} {
		before := ""
		if v.D != nil {
			before = v.D.String()
		}
		typ, err := inferType(expr, nil, []ColType{v.T})
		if err != nil || typ.Kind != KindInt32 {
			t.Fatalf("type: %v %v", typ, err)
		}
		out, err := (&evalEnv{params: []Value{v}}).eval(expr)
		want := int64(0)
		if v.D != nil && v.D.Int64() == -123000 {
			want = 2
		}
		if err != nil || out.Null != v.Null || !v.Null && out.I != want {
			t.Errorf("%v: %v %v", v, out, err)
		}
		if v.D != nil && v.D.String() != before {
			t.Fatal("coefficient changed")
		}
	}
	for _, typ := range []ColType{typeFloat64, typeString} {
		_, err := inferType(expr, nil, []ColType{typ})
		if sqlState(err) != stateUndefinedFunction {
			t.Errorf("bound %v: %v", typ, err)
		}
	}
}
