// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"os"
	"strings"
	"testing"
)

func TestSQLJoinCommonQ19(t *testing.T) {
	h := newSQLHarness(t)
	for _, table := range []string{"it_common_q19_lineitem", "it_common_q19_part"} {
		h.exec("DROP TABLE IF EXISTS " + table)
		t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	}
	h.exec(`CREATE TABLE it_common_q19_part (
		p_partkey BIGINT, p_brand VARCHAR, p_container VARCHAR, p_size INTEGER)`)
	h.exec(`CREATE TABLE it_common_q19_lineitem (
		l_partkey BIGINT, l_quantity DECIMAL(15,2), l_shipmode VARCHAR,
		l_shipinstruct VARCHAR, l_extendedprice DECIMAL(15,2), l_discount DECIMAL(15,2))`)
	h.exec(`INSERT INTO it_common_q19_part VALUES
		(1,'Brand#12','SM CASE',3),(1,'Brand#12','SM CASE',3),
		(2,'Brand#23','MED BAG',8),(3,'Brand#34','LG BOX',12),
		(NULL,'Brand#12','SM CASE',3),(4,NULL,'SM CASE',3)`)
	h.exec(`INSERT INTO it_common_q19_lineitem VALUES
		(1,5,'AIR','DELIVER IN PERSON',100,0.05),
		(2,12,'AIR REG','DELIVER IN PERSON',200,0.10),
		(3,25,'AIR','DELIVER IN PERSON',80,0.25),
		(1,5,'SHIP','DELIVER IN PERSON',900,0),
		(1,5,'AIR','OTHER',900,0),
		(1,50,'AIR','DELIVER IN PERSON',900,0),
		(1,5,NULL,'DELIVER IN PERSON',900,0),
		(NULL,5,'AIR','DELIVER IN PERSON',900,0),
		(4,5,'AIR','DELIVER IN PERSON',900,0)`)
	sql, err := os.ReadFile("testdata/olap/tpch-q19.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Only fixture table names differ from the upstream query. All predicates,
	// arithmetic and the aggregate are the unchanged SF1 SQL shape.
	query := strings.NewReplacer("    lineitem,", "    it_common_q19_lineitem,",
		"    part\n", "    it_common_q19_part\n").Replace(string(sql))
	h.expectRows(query, "430.0000")
}
