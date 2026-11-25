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
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// With Flex, a snowflake's selective fact filter (lineitem's ship dates) and
// its orders branch's sub-fact filter discover their complete Search
// candidates once and split them across parallel workers
// (sharedFlexCandidates). Results match the eager join over the same
// snapshot, with NULL and duplicate keys. After an UPDATE and a DELETE the
// overlay's filters are not pushed to Search, so it keeps its existing plan;
// both plans still agree.
func TestSQLFlexSnowflakeSharedCandidates(t *testing.T) {
	h := newSQLHarnessWithOptions(t, map[string]string{OptionStringFlex: "true", OptionStringQueryWorkers: "4", OptionStringQueryWorkerBudget: "4"})
	tables := []string{"it_fsc_lineitem", "it_fsc_orders", "it_fsc_customer"}
	h.dropTables(tables...)
	t.Cleanup(func() { h.dropTables(tables...) })
	// One statement per table: a single published segment, plus the newer
	// one the UPDATE below adds.
	insert := func(table string, n int, row func(int) string) {
		var rows []string
		for i := 1; i <= n; i++ {
			rows = append(rows, row(i))
		}
		h.exec("INSERT INTO " + table + " VALUES " + strings.Join(rows, ","))
	}
	h.exec("CREATE TABLE it_fsc_customer (c_custkey BIGINT, c_nationkey INTEGER)")
	insert("it_fsc_customer", 300, func(c int) string {
		if c == 300 {
			return "(300,NULL)"
		}
		return fmt.Sprintf("(%d,%d)", c, c%7)
	})
	h.exec("CREATE TABLE it_fsc_orders (o_orderkey BIGINT, o_custkey BIGINT, o_orderdate DATE)")
	insert("it_fsc_orders", 2500, func(o int) string {
		cust := fmt.Sprint(o%301 + 1)
		if o%37 == 0 {
			cust = "NULL"
		}
		key := o
		if o%500 == 0 {
			key = 7 // duplicate order keys
		}
		return fmt.Sprintf("(%d,%s,'1994-%02d-%02d')", key, cust, o%12+1, o%28+1)
	})
	h.exec("CREATE TABLE it_fsc_lineitem (l_orderkey BIGINT, l_suppkey BIGINT, l_price BIGINT, l_shipdate DATE)")
	insert("it_fsc_lineitem", 6000, func(i int) string {
		order := fmt.Sprint(i%2600 + 1)
		if i%61 == 0 {
			order = "NULL"
		}
		return fmt.Sprintf("(%s,%d,%d,'1995-%02d-%02d')", order, i%62+1, i%113, i%12+1, i%28+1)
	})
	const query = `SELECT c_nationkey, COUNT(*), SUM(l_price) FROM it_fsc_lineitem, it_fsc_orders, it_fsc_customer
		WHERE l_orderkey = o_orderkey AND o_custkey = c_custkey
		AND l_shipdate BETWEEN DATE '1995-03-01' AND DATE '1995-06-30' AND o_orderdate >= DATE '1994-04-01'
		GROUP BY c_nationkey ORDER BY c_nationkey`
	run := func(snowflake bool) ([]string, map[string]any) {
		t.Helper()
		oldFact, oldDim := snowflakeMinFactRows, snowflakeParallelDimensionRows
		snowflakeMinFactRows, snowflakeParallelDimensionRows = math.MaxInt64, math.MaxInt64
		if snowflake {
			snowflakeMinFactRows, snowflakeParallelDimensionRows = 1, 1
		}
		defer func() { snowflakeMinFactRows, snowflakeParallelDimensionRows = oldFact, oldDim }()
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
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var sample map[string]any
		if err := json.Unmarshal(b, &sample); err != nil {
			t.Fatal(err)
		}
		plan, _ := sample["plan"].(map[string]any)
		return rows, plan
	}
	same := func(label string) map[string]any {
		t.Helper()
		eager, eagerPlan := run(false)
		streamed, plan := run(true)
		if eagerPlan["complete_flex_candidates"] == true {
			t.Fatalf("%s: eager baseline used shared candidates: %v", label, eagerPlan)
		}
		sort.Strings(eager)
		sort.Strings(streamed)
		if len(eager) == 0 || strings.Join(eager, "\n") != strings.Join(streamed, "\n") {
			t.Fatalf("%s: snowflake differs from eager join:\n%s\nwant:\n%s", label, strings.Join(streamed, "\n"), strings.Join(eager, "\n"))
		}
		return plan
	}
	if plan := same("published"); plan["kind"] != "parallel_streaming_star_join" || plan["complete_flex_candidates"] != true || plan["snowflake"] != true {
		t.Fatalf("selective Flex snowflake fact did not split shared candidates: %v", plan)
	}
	h.exec("UPDATE it_fsc_lineitem SET l_price = l_price + 1000 WHERE l_suppkey % 10 = 0")
	h.exec("DELETE FROM it_fsc_lineitem WHERE l_suppkey % 10 = 1")
	same("overlay")
}
