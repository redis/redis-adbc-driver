// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// flexPlan runs query and returns its rows and execution diagnostics.
func flexPlan(t *testing.T, h *sqlHarness, query string) ([]string, map[string]any) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringExecutionDiagnosticsFile, path); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, query); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows := snowflakeReaderRows(r)
	for i, row := range rows {
		// Drop the reader's /type suffixes.
		cells := strings.Split(row, "|")
		for c, cell := range cells {
			if k := strings.LastIndex(cell, "/"); k >= 0 {
				cells[c] = cell[:k]
			}
		}
		rows[i] = strings.Join(cells, "|")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var diag map[string]any
	if err := json.Unmarshal(b, &diag); err != nil {
		t.Fatal(err)
	}
	return rows, diag
}

// With Flex, an eager join's large input (TPC-H Q19's lineitem) was read whole
// and serially (#636). It is now read by the parallel chunk readers, and the
// join's rows are unchanged, with NULL and duplicate keys.
func TestSQLFlexEagerJoinParallelInputs(t *testing.T) {
	h := newSQLHarnessWithOptions(t, map[string]string{OptionStringFlex: "true", OptionStringQueryWorkers: "4", OptionStringQueryWorkerBudget: "4"})
	tables := []string{"it_fej_lineitem", "it_fej_part"}
	h.dropTables(tables...)
	t.Cleanup(func() { h.dropTables(tables...) })
	var parts []string
	for p := 1; p <= 500; p++ {
		key := strconv.Itoa(p)
		if p == 500 {
			key = "NULL"
		}
		parts = append(parts, fmt.Sprintf("(%s,'B%d','C%d',%d)", key, p%5, p%4, p%50))
	}
	h.exec("CREATE TABLE it_fej_part (p_partkey BIGINT, p_brand VARCHAR, p_container VARCHAR, p_size INTEGER)")
	h.exec("INSERT INTO it_fej_part VALUES " + strings.Join(parts, ","))
	var lines []string
	for i := 1; i <= 6000; i++ {
		part := strconv.Itoa(i%520 + 1) // some keys have no part
		if i%97 == 0 {
			part = "NULL"
		}
		lines = append(lines, fmt.Sprintf("(%s,%d,%d,'%s')", part, i%50+1, i%113, []string{"AIR", "MAIL", "SHIP", "AIR REG"}[i%4]))
	}
	h.exec("CREATE TABLE it_fej_lineitem (l_partkey BIGINT, l_quantity INTEGER, l_price BIGINT, l_shipmode VARCHAR)")
	h.exec("INSERT INTO it_fej_lineitem VALUES " + strings.Join(lines, ","))
	// TPC-H Q19's shape: the join equality inside each OR branch keeps the
	// eager join, with per-table literal lists pushed into each scan.
	const query = `SELECT COUNT(*), SUM(l_price) FROM it_fej_lineitem, it_fej_part WHERE
		(p_partkey = l_partkey AND p_brand = 'B1' AND p_container IN ('C1','C2') AND l_quantity BETWEEN 1 AND 30 AND p_size BETWEEN 1 AND 40 AND l_shipmode IN ('AIR','AIR REG'))
		OR (p_partkey = l_partkey AND p_brand = 'B2' AND p_container IN ('C2','C3') AND l_quantity BETWEEN 10 AND 45 AND p_size BETWEEN 1 AND 45 AND l_shipmode IN ('AIR','AIR REG'))`
	run := func(parallel bool) ([]string, map[string]any) {
		t.Helper()
		old := snowflakeParallelDimensionRows
		snowflakeParallelDimensionRows = math.MaxInt64
		if parallel {
			snowflakeParallelDimensionRows = 1
		}
		defer func() { snowflakeParallelDimensionRows = old }()
		return flexPlan(t, h, query)
	}
	serial, serialDiag := run(false)
	parallel, diag := run(true)
	sort.Strings(serial)
	sort.Strings(parallel)
	if len(serial) != 1 || strings.HasPrefix(serial[0], "0|") || strings.Join(serial, "\n") != strings.Join(parallel, "\n") {
		t.Fatalf("parallel Flex eager join %v differs from the serial read %v", parallel, serial)
	}
	plan, _ := diag["plan"].(map[string]any)
	if kind, _ := plan["kind"].(string); kind == "parallel_streaming_star_join" || kind == "streaming_snowflake_join" {
		t.Fatalf("fixture no longer uses the eager join: %v", plan)
	}
	if g, _ := serialDiag["worker_grant"].(float64); g != 0 {
		t.Fatalf("serial baseline used %v workers", g)
	}
	if g, _ := diag["worker_grant"].(float64); g < 2 {
		t.Fatalf("eager Flex join inputs were not read in parallel: %v", diag)
	}
}
