// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"math/big"
	"strings"
	"testing"
)

func TestNumericGCDLCM(t *testing.T) {
	for _, tc := range []struct{ sql, text string }{
		{"gcd(1.20::numeric(5,2),0.450::numeric(5,3))", "0.150"},
		{"lcm(1.20::numeric(5,2),0.450::numeric(5,3))", "3.600"},
		{"gcd(-1.2::numeric(5,1),0.45::numeric(5,2))", "0.15"},
		{"lcm(-1.2::numeric(5,1),-0.45::numeric(5,2))", "3.60"},
		{"gcd(0.000::numeric(5,3),0.00::numeric(5,2))", "0.000"},
		{"lcm(0.000::numeric(5,3),7.5::numeric(5,1))", "0.000"},
		{"gcd(3::integer,1.2::numeric(5,1))", "0.6"},
		{"lcm(3::bigint,1.2::numeric(5,1))", "6.0"},
		{"gcd(1234567890123456789012345678901234567890::numeric(76,0),10::numeric(76,0))", "10"},
		{"gcd(1.2::numeric(5,1),NULL::numeric)", "NULL"},
		{"lcm(NULL::numeric,0::numeric)", "NULL"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			v, typ, err := evalTestExpr(t, tc.sql)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(typ, "NUMERIC") || v != tc.text {
				t.Fatalf("got %s (%s), want %s decimal", v, typ, tc.text)
			}
		})
	}
}

func TestNumericGCDLCMPreservesCoefficientsAndPrecision(t *testing.T) {
	typ := decimalType(76, 0)
	a := decimalValue(big.NewInt(-12), 76, 1)
	b := decimalValue(big.NewInt(45), 76, 2)
	f := &Func{Name: "LCM"}
	v, err := pg18Scalar(f, []Value{a, b}, utcZone)
	if err != nil || v.Text() != "3.60" {
		t.Fatalf("%s: %v", v.Text(), err)
	}
	if a.D.Int64() != -12 || b.D.Int64() != 45 {
		t.Fatal("input coefficients mutated")
	}
	huge := new(big.Int).Sub(pow10(76), big.NewInt(1))
	_, err = pg18Scalar(f, []Value{decimalValue(huge, typ.Precision, typ.Scale), decimalValue(big.NewInt(2), 76, 0)}, utcZone)
	if sqlState(err) != stateNumericRange {
		t.Fatalf("overflow: %v", err)
	}
	zero, err := pg18Scalar(f, []Value{decimalValue(huge, 76, 0), decimalValue(big.NewInt(0), 76, 0)}, utcZone)
	if err != nil || zero.D.Sign() != 0 {
		t.Fatalf("zero: %v %v", zero, err)
	}
}

func TestNumericGCDLCMUnsupportedTypes(t *testing.T) {
	for _, q := range []string{"gcd(1.2::numeric,true)", "lcm(false,1.2::numeric)",
		"gcd(1.2::numeric,'2'::text)", "lcm(1.2::numeric,2::float8)"} {
		_, _, err := evalTestExpr(t, q)
		if sqlState(err) != stateUndefinedFunction {
			t.Errorf("%s: %v", q, err)
		}
	}
}
