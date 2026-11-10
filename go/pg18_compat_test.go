// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestPG18TypedNumericLiterals(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"int2 '2' + int4 '3'", "5", "INTEGER"},
		{"int8 '9223372036854775807'", "9223372036854775807", "BIGINT"},
		{"int4 '2147483647' + int4 '0'", "2147483647", "INTEGER"},
	})
	for _, q := range []string{"SELECT int2 '32768'", "SELECT int4 '2147483648'"} {
		_, err := ParseScript(q)
		if sqlState(err) != stateNumericRange {
			t.Errorf("%s: %v", q, err)
		}
	}
}

func TestPG18ScalarCompatibility(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"pg_input_is_valid('34', 'int4')", "true", "BOOLEAN"},
		{"pg_input_is_valid('2147483648', 'int4')", "false", "BOOLEAN"},
		{"pg_input_is_valid('bad', 'int8')", "false", "BOOLEAN"},
		{"translate('abcé', 'abé', 'xy')", "xyc", "VARCHAR"},
		{"encode('abc'::bytea, 'base64')", "YWJj", "VARCHAR"},
		{"quote_literal('it''s')", "'it''s'", "VARCHAR"},
		{"chr(128512)", "😀", "VARCHAR"},
		{"ascii('😀')", "128512", "INTEGER"},
		{"num_nonnulls(1, null, 'x')", "2", "INTEGER"},
		{"num_nulls(1, null, 'x')", "1", "INTEGER"},
		{"sinh(0)", "0", "DOUBLE PRECISION"},
		{"width_bucket(5, 0, 10, 5)", "3", "INTEGER"},
		{"width_bucket(10, 10, 0, 5)", "1", "INTEGER"},
		{"isfinite(date '2024-01-01')", "true", "BOOLEAN"},
		{"justify_hours(interval '25 hours')", "1 day 01:00:00", "INTERVAL"},
	})
}

func TestPG18DateInputs(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"CAST(date 'Jan 5 2024' AS VARCHAR)", "2024-01-05", "VARCHAR"},
		{"CAST(date '20240105' AS VARCHAR)", "2024-01-05", "VARCHAR"},
		{"CAST(date '2024-1-5' AS VARCHAR)", "2024-01-05", "VARCHAR"},
		{"CAST(date '01/02/03' AS VARCHAR)", "2003-01-02", "VARCHAR"},
		{"CAST(date '1999.008' AS VARCHAR)", "1999-01-08", "VARCHAR"},
		{"CAST(date 'J2451187' AS VARCHAR)", "1999-01-08", "VARCHAR"},
		{"CAST(date 'January 8, 99 BC' AS VARCHAR)", "0099-01-08 BC", "VARCHAR"},
		{"CAST(time '10:00 PM' AS VARCHAR)", "22:00:00", "VARCHAR"},
		{"CAST(timestamp '2024-01-05 10:00 PM' AS VARCHAR)", "2024-01-05 22:00:00", "VARCHAR"},
		{"CAST(timestamptz '20500710 173201 Europe/Helsinki' AS VARCHAR)", "2050-07-10 14:32:01+00", "VARCHAR"},
		{"CAST(timestamptz 'Wed Jul 11 10:51:14 America/New_York 2001' AS VARCHAR)", "2001-07-11 14:51:14+00", "VARCHAR"},
	})
}

func TestPG18OperatorsAndStrings(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"2 ^ 3", "8", "DOUBLE PRECISION"}, {"2 ^ 3 ^ 2", "64", "DOUBLE PRECISION"},
		{"5 & 3", "1", "INTEGER"}, {"5 | 3", "7", "INTEGER"}, {"1 << 2", "4", "INTEGER"},
		{"5 BETWEEN SYMMETRIC 10 AND 1", "true", "BOOLEAN"},
		{"'first'\n' second'", "first second", "VARCHAR"},
	})
	for q, want := range map[string]string{`SELECT E'bad \u061'`: stateEscape, `SELECT E'bad \U002FFFFF'`: stateSyntax, `SELECT U&'bad \+2FFFFF'`: stateSyntax} {
		_, err := ParseScript(q)
		if sqlState(err) != want {
			t.Errorf("%s: %v", q, err)
		}
	}
}

func TestPG18SpecialTemporalInputs(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"date 'today' = current_date", "true", "BOOLEAN"},
		{"CAST(date 'epoch' AS VARCHAR)", "1970-01-01", "VARCHAR"},
	})
}

func TestPG18NumericAndFormatEdges(t *testing.T) {
	runScalarCases(t, []scalarCase{
		{"width_bucket('infinity'::float8, 0, 10, 5)", "6", "INTEGER"},
		{"width_bucket('-infinity'::float8, 10, 0, 5)", "6", "INTEGER"},
		{"format('%2$s/%1$L/%3$I', 'it''s', 'é', 'A b')", "é/'it''s'/\"A b\"", "VARCHAR"},
		{"quote_ident('simple')", "simple", "VARCHAR"},
		{"quote_ident('select')", "\"select\"", "VARCHAR"},
		{"format('%s/%I/%L', true, true, true)", "t/t/'t'", "VARCHAR"},
		{"format('%I/%I', 'simple', 'A b')", "simple/\"A b\"", "VARCHAR"},
		{"format('%*s', -4, 'é')", "é   ", "VARCHAR"},
		{"make_interval()", "00:00:00", "INTERVAL"},
		{"div(-7, 2)", "-3", "NUMERIC(76,0)"},
		{"gcd(-12, 18)", "6", "INTEGER"},
		{"lcm(-4, 6)", "12", "INTEGER"},
		{"to_char(-1, '99')", " -1", "VARCHAR"},
		{"encode(E'\\n'::bytea, 'escape')", "\n", "VARCHAR"},
		{"to_char(1234.5, '9G999D99')", " 1,234.50", "VARCHAR"},
	})
	for q, state := range map[string]string{
		"SELECT date '99 Jan 08'":     stateDatetimeFormat,
		"SELECT date '5874898-01-01'": stateDatetimeRange,
	} {
		statements, err := ParseScript(q)
		if err == nil {
			_, err = runInMemory(statements[0].Stmt.(*SelectStmt))
		}
		if sqlState(err) != state {
			t.Errorf("%s: %v, want %s", q, err, state)
		}
	}
}

func TestPG18RegressionAndBitWindows(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want []string
	}{
		{"SELECT regr_count(y,x) OVER (ORDER BY x ROWS 1 PRECEDING), bit_or(x) OVER (ORDER BY x ROWS 1 PRECEDING) FROM (VALUES (1,2),(2,NULL),(3,6)) t(x,y) ORDER BY x", []string{"1|1", "1|3", "1|3"}},
		{"SELECT regr_slope(y,x) OVER (ORDER BY x ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING EXCLUDE CURRENT ROW) FROM (VALUES (1,2),(2,4),(3,6)) t(x,y) ORDER BY x", []string{"2", "2", "2"}},
	} {
		stmts, err := ParseScript(tc.sql)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := runInMemory(stmts[0].Stmt.(*SelectStmt))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(tc.want) {
			t.Fatalf("rows=%d", len(rows))
		}
		for i, row := range rows {
			got := ""
			for j, v := range row {
				if j > 0 {
					got += "|"
				}
				if v.Null {
					got += "NULL"
				} else {
					got += v.Text()
				}
			}
			if got != tc.want[i] {
				t.Errorf("row %d: %q, want %q", i, got, tc.want[i])
			}
		}
	}
}

func TestPG18RegressionSpecialInputs(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT sum(x) FROM (VALUES (9223372036854775807::bigint),(9223372036854775807::bigint)) t(x)", "18446744073709551614"},
		{"SELECT avg(x) FROM (VALUES (0.00000000000000000001::numeric(30,20))) t(x)", "0.00000000000000000001"},
		{"SELECT regr_avgx(y,x), regr_avgy(y,x), regr_sxx(y,x), regr_syy(y,x) FROM (VALUES (1.0::float8,1.0::float8),('Infinity'::float8,2.0::float8),(3.0::float8,3.0::float8)) t(y,x)", "2|Infinity|2|NaN"},
		{"SELECT regr_avgx(y,x), regr_sxx(y,x), regr_slope(y,x), corr(y,x), covar_samp(y,x) FROM (VALUES ('Infinity'::float8,1.0::float8)) t(y,x)", "1|0|NULL|NULL|NULL"},
		{"SELECT regr_count(y,x), corr(y,x) FROM (VALUES (NULL::float8,1.0::float8),(2.0::float8,NULL::float8)) t(y,x)", "0|NULL"},
	} {
		stmts, err := ParseScript(tc.sql)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := runInMemory(stmts[0].Stmt.(*SelectStmt))
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for i, v := range rows[0] {
			if i > 0 {
				got += "|"
			}
			if v.Null {
				got += "NULL"
			} else {
				got += v.Text()
			}
		}
		if got != tc.want {
			t.Errorf("%s: %s, want %s", tc.sql, got, tc.want)
		}
	}
}
