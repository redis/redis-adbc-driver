// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"testing"
)

func TestSQLNumericNotEqualIndex(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_numeric_neq")
	h.exec("CREATE TABLE it_numeric_neq (id INTEGER, s SMALLINT, b BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_numeric_neq") })
	h.exec("INSERT INTO it_numeric_neq VALUES (1,-32768,-9223372036854775808),(2,-1,-9007199254740993),(3,0,0),(4,1,9007199254740993),(5,32767,9223372036854775807),(6,NULL,NULL)")
	for _, pred := range []string{"s <> 0", "0 <> s", "s != 0"} {
		q := "SELECT COUNT(*) FROM it_numeric_neq WHERE " + pred
		h.expectRows(q, "4")
		query, residual := h.pushedWhere(h.rawClient(), q)
		if residual || !strings.Contains(query, "|") {
			t.Fatalf("missing exact range union: %s residual=%v", query, residual)
		}
	}
	for _, pred := range []string{"b <> 9007199254740993", "b <> -9223372036854775808", "b != 9223372036854775807"} {
		h.expectRows("SELECT COUNT(*) FROM it_numeric_neq WHERE "+pred, "4")
		_, residual := h.pushedWhere(h.rawClient(), "SELECT COUNT(*) FROM it_numeric_neq WHERE "+pred)
		if !residual {
			t.Fatal("lost precision recheck", pred)
		}
	}
	for _, pred := range []string{"s <> 0.5", "s <> 32767.1", "s <> 32768", "b <> 9007199254740992"} {
		h.expectRows("SELECT COUNT(*) FROM it_numeric_neq WHERE "+pred, "5")
	}
	h.exec("ALTER TABLE it_numeric_neq ADD COLUMN d SMALLINT DEFAULT 3")
	h.exec("UPDATE it_numeric_neq SET d=NULL WHERE id=2")
	h.exec("UPDATE it_numeric_neq SET d=0 WHERE id=3")
	h.expectRows("SELECT COUNT(*) FROM it_numeric_neq WHERE d <> 0", "4")
	_, residual := h.pushedWhere(h.rawClient(), "SELECT COUNT(*) FROM it_numeric_neq WHERE d <> 0")
	if !residual {
		t.Fatal("missing-default recheck lost")
	}
}

func TestSQLNotEqualTimestampRetainsFallback(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_neq_timestamp")
	h.exec("SET TIME ZONE 'America/New_York'")
	h.exec("CREATE TABLE it_neq_timestamp (t TIMESTAMP)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_neq_timestamp") })
	h.exec("INSERT INTO it_neq_timestamp VALUES ('2026-01-01'),('2026-01-02'),('2026-01-03'),(NULL)")
	for _, pred := range []string{"t <> TIMESTAMPTZ '2026-01-02 05:00:00+00'", "TIMESTAMPTZ '2026-01-02 05:00:00+00' != t"} {
		sql := "SELECT COUNT(*) FROM it_neq_timestamp WHERE " + pred
		h.expectRows(sql, "2")
		query, residual := h.pushedWhere(h.rawClient(), sql)
		if query != "*" || !residual {
			t.Fatal("noninteger not-equal lost fallback", query, residual)
		}
	}
}
