// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestSQLPG18QueryGaps(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_gap_t", "it_gap_text")
	t.Cleanup(func() { h.dropTables("it_gap_t", "it_gap_text") })
	h.exec("CREATE SEQUENCE it_gap_seq")
	t.Cleanup(func() { h.exec("DROP SEQUENCE IF EXISTS it_gap_seq") })
	h.exec("CREATE TABLE it_gap_t(i INTEGER)")
	h.exec("INSERT INTO it_gap_t VALUES(1),(2)")
	h.expectRows("SELECT CASE WHEN i>100 THEN nextval('it_gap_seq') ELSE 0 END FROM it_gap_t", "0", "0")
	h.expectRows("SELECT nextval('it_gap_seq')", "1")
	h.expectError("SELECT CASE WHEN i>100 THEN 1/0 ELSE 0 END FROM it_gap_t", "division by zero")
	h.expectRows("SELECT CASE WHEN i>100 THEN 1/i ELSE 0 END FROM it_gap_t", "0", "0")
	h.expectRows("SELECT CASE WHEN false THEN 1/0 ELSE 1 END FROM it_gap_t", "1", "1")
	h.exec("CREATE TABLE it_gap_text AS SELECT MD5('abc') sk")
	h.expectRows("SELECT data_type FROM information_schema.columns WHERE table_name='it_gap_text' AND column_name='sk'", "text")
	h.expectRows("SELECT pg_typeof(sk) FROM it_gap_text", "text")
	h.expectRows("SELECT EXTRACT(epoch FROM TIMESTAMP '250000-01-01 01:02:03.123456')", "7827070784523.123456")
	h.expectRows("SELECT MIN(EXTRACT(year FROM DATE '2020-01-01')) OVER() FROM it_gap_t", "2020", "2020")
}
