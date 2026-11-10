// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"testing"
)

func TestPG18RemainingQueryGaps(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`WITH t(i) AS(VALUES(1),(2)) SELECT CASE WHEN i>100 THEN 1/0 ELSE 0 END FROM t`, "error: division by zero"},
		{`SELECT pg_typeof(EXTRACT(year FROM DATE '2024-01-01')),pg_typeof(DATE_PART('year',DATE '2024-01-01'))`, "numeric|double precision"},
		{`SELECT pg_typeof(MD5('abc'))`, "text"},
	} {
		if got := memQuery(tc.sql); !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q want %q", tc.sql, got, tc.want)
		}
	}
}

func TestPG18CasePruning(t *testing.T) {
	for _, q := range []string{
		`SELECT CASE WHEN false THEN 1/0 ELSE 1 END`,
		`SELECT CASE WHEN true THEN 1 ELSE 1/0 END`,
		`WITH t(i)AS(VALUES(1)) SELECT CASE WHEN i>100 THEN 1/0 ELSE 1 END FROM t`,
		`WITH t(i)AS(VALUES(1),(2)) SELECT CASE WHEN i>100 AND false THEN 1/0 ELSE 1 END FROM t LIMIT 1`,
		`WITH t(i)AS(VALUES(1),(2)) SELECT CASE WHEN i>100 THEN COALESCE(1,1/0) ELSE 1 END FROM t LIMIT 1`,
	} {
		if got := memQuery(q); got != "1" {
			t.Errorf("%s: %s", q, got)
		}
	}
}

func TestPG18ExactExtract(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`SELECT EXTRACT(epoch FROM TIMESTAMP '250000-01-01 01:02:03.123456')`, "7827070784523.123456"},
		{`SELECT EXTRACT(epoch FROM TIMESTAMP '1969-12-31 23:59:59.123456')`, "-0.876544"},
		{`WITH t(i)AS(VALUES(1),(2)) SELECT SUM(EXTRACT(second FROM TIME '00:00:01.123456')) OVER() FROM t LIMIT 1`, "2.246912"},
		{`SELECT COALESCE(1,1/0)`, "1"},
		{`SELECT COALESCE(NULL,1,1/0)`, "1"},
	} {
		if got := memQuery(tc.sql); got != tc.want {
			t.Errorf("%s: %s", tc.sql, got)
		}
	}
}
