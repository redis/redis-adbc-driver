// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

func TestOverlapsAndInputErrorInfo(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"(date '2024-01-01',date '2024-02-01') overlaps (date '2024-01-15',date '2024-03-01')", "true", "BOOLEAN"},
		{"(date '2024-02-01',date '2024-01-01') overlaps (date '2024-02-01',date '2024-03-01')", "false", "BOOLEAN"},
		{"(date '2024-01-01',date '2024-01-01') overlaps (date '2024-01-01',date '2024-02-01')", "true", "BOOLEAN"},
		{"(null,date '2024-02-01') overlaps (date '2024-01-01',date '2024-03-01')", "true", "BOOLEAN"},
		{"(null,date '2024-01-01') overlaps (date '2024-02-01',date '2024-03-01')", "NULL", "BOOLEAN"},
		{"true = (time '01:00',time '02:00') overlaps (time '01:30',time '03:00')", "true", "BOOLEAN"},
		{"(pg_input_error_info('1e4000','float8')).sql_error_code", "22003", "VARCHAR"},
		{"(pg_input_error_info('1e-4000','float8')).message", `"1e-4000" is out of range for type double precision`, "VARCHAR"},
		{"(pg_input_error_info('1','int4')).message", "NULL", "VARCHAR"},
		{"(pg_input_error_info('abc','varchar(1)')).message", "value too long for type character varying(1)", "VARCHAR"},
		{"(pg_input_error_info('1e100','numeric(2,0)')).detail", "A field with precision 2, scale 0 must round to an absolute value less than 10^2.", "VARCHAR"},
	})
	for q, want := range map[string]string{
		"(null,null) overlaps (null,null)":                                              "42725",
		"(timestamp '2024-01-01',timestamp '2024-01-02') overlaps ('bad','2024-01-03')": stateDatetimeFormat,
		"(1,2) overlaps (3,4)":                                                          stateUndefinedFunction,
		"pg_input_error_info(1,2)":                                                      stateUndefinedFunction,
		"pg_input_error_info('1','nosuch')":                                             stateUndefinedObject,
		"pg_input_error_info('NaN','numeric')":                                          stateFeatureNotSupported,
		"pg_input_error_info('NaN','numeric(3,1)')":                                     stateFeatureNotSupported,
	} {
		_, _, err := evalTestExpr(t, q)
		if sqlState(err) != want {
			t.Errorf("%s: %v, want %s", q, err, want)
		}
	}
}

func TestOverlapsNativeNanoseconds(t *testing.T) {
	typ := timestampType(arrow.Nanosecond, "")
	f := &Func{Name: "__OVERLAPS", Args: []Expr{&ColumnRef{Name: "a"}, &ColumnRef{Name: "b"}, &ColumnRef{Name: "c"}, &ColumnRef{Name: "d"}}}
	// Touching endpoints stay disjoint even within a single microsecond.
	v, err := pgOverlaps(f, []Value{intValue(typ, 0), intValue(typ, 1), intValue(typ, 1), intValue(typ, 2)}, utcZone)
	if err != nil || v.Null || v.I != 0 {
		t.Fatalf("nanosecond endpoints: %v, %v", v, err)
	}
}

func TestInputErrorInfoScalarAndFrom(t *testing.T) {
	for _, q := range []string{"pg_input_error_info('1','int4')", "pg_input_error_info(null,'int4')", "pg_input_error_info('bad','int4')"} {
		f := parseTestExpr(t, q).(*Func)
		ts, err := tableFunctionTypes(f, nil, nil)
		if err != nil || len(ts) != 4 {
			t.Fatalf("%s: %v, %v", q, ts, err)
		}
		for i, typ := range ts {
			if !typ.Text {
				t.Errorf("field %d lost logical text identity", i)
			}
		}
		fieldType, err := inferType(parseTestExpr(t, "("+q+").message"), nil, nil)
		if err != nil || !fieldType.Text {
			t.Fatalf("field selection lost text identity: %v, %v", fieldType, err)
		}
		env := &evalEnv{ctx: context.Background()}
		v, err := env.eval(f)
		if err != nil {
			t.Fatal(err)
		}
		if v.Null != (q == "pg_input_error_info(null,'int4')") {
			t.Fatalf("%s: wrong scalar NULL", q)
		}
		rows, _, err := srfRows(env, f)
		if err != nil || len(rows) != 1 || len(rows[0]) != 4 {
			t.Fatalf("%s: %v, %v", q, rows, err)
		}
	}
}
