// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Managed TPC-H-shaped tables exercise the parallel exact-directory fact and
// dimension readers (orders has 2,500 rows), the broad-filter gate, duplicate
// keys, NULL keys and the derived-table path, against the eager plan on the
// same connection and snapshot rules.
func TestSQLSnowflakeJoinsManaged(t *testing.T) {
	h := newSQLHarness(t)
	tables := []string{"it_sf_lineitem", "it_sf_orders", "it_sf_customer", "it_sf_supplier", "it_sf_nation", "it_sf_region"}
	h.dropTables(tables...)
	t.Cleanup(func() { h.dropTables(tables...) })
	h.exec("CREATE TABLE it_sf_region (r_regionkey INTEGER, r_name VARCHAR)")
	h.exec("INSERT INTO it_sf_region VALUES (0,'AFRICA'),(1,'AMERICA'),(2,'ASIA')")
	h.exec("CREATE TABLE it_sf_nation (n_nationkey INTEGER, n_name VARCHAR, n_regionkey INTEGER)")
	h.exec("INSERT INTO it_sf_nation VALUES (0,'N0',0),(1,'N1',1),(2,'N2',2),(3,'N3',0),(4,'N4',1),(5,'N5',2),(6,'NOREGION',NULL)")
	insert := func(table string, n int, row func(int) string) {
		var rows []string
		for i := 1; i <= n; i++ {
			rows = append(rows, row(i))
		}
		h.exec("INSERT INTO " + table + " VALUES " + strings.Join(rows, ","))
	}
	h.exec("CREATE TABLE it_sf_supplier (s_suppkey BIGINT, s_nationkey INTEGER)")
	insert("it_sf_supplier", 60, func(s int) string {
		if s == 60 {
			return "(60,NULL)"
		}
		return fmt.Sprintf("(%d,%d)", s, s%7)
	})
	h.exec("CREATE TABLE it_sf_customer (c_custkey BIGINT, c_nationkey INTEGER)")
	insert("it_sf_customer", 300, func(c int) string {
		if c == 300 {
			return "(300,NULL)"
		}
		return fmt.Sprintf("(%d,%d)", c, c%7)
	})
	h.exec("CREATE TABLE it_sf_orders (o_orderkey BIGINT, o_custkey BIGINT, o_orderdate DATE)")
	insert("it_sf_orders", 2500, func(o int) string {
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
	h.exec("CREATE TABLE it_sf_lineitem (l_orderkey BIGINT, l_suppkey BIGINT, l_price DECIMAL(12,2), l_shipdate DATE)")
	insert("it_sf_lineitem", 6000, func(i int) string {
		order := fmt.Sprint(i%2600 + 1)
		if i%61 == 0 {
			order = "NULL"
		}
		return fmt.Sprintf("(%s,%d,%d.25,'1995-%02d-%02d')", order, i%62+1, i%113, i%12+1, i%28+1)
	})

	queries := map[string]string{
		"q5": `SELECT n_name, SUM(l_price), COUNT(*) FROM it_sf_customer, it_sf_orders, it_sf_lineitem, it_sf_supplier, it_sf_nation, it_sf_region
			WHERE c_custkey = o_custkey AND l_orderkey = o_orderkey AND l_suppkey = s_suppkey AND c_nationkey = s_nationkey
			AND s_nationkey = n_nationkey AND n_regionkey = r_regionkey AND r_name = 'ASIA' AND o_orderdate >= DATE '1994-03-01'
			GROUP BY n_name ORDER BY n_name`,
		"q3": `SELECT l_orderkey, SUM(l_price) AS revenue, o_orderdate FROM it_sf_customer, it_sf_orders, it_sf_lineitem
			WHERE c_nationkey < 6 AND c_custkey = o_custkey AND l_orderkey = o_orderkey AND o_orderdate < DATE '1994-12-01'
			AND l_shipdate > DATE '1995-02-01' GROUP BY l_orderkey, o_orderdate ORDER BY revenue DESC, l_orderkey LIMIT 9`,
		// More than maxUnionTerms orders: no fact key union, so the broad
		// ship-date filter alone selects the parallel exact-directory reader.
		"q7": `SELECT supp, cust, COUNT(*), SUM(v) FROM (SELECT n1.n_name AS supp, n2.n_name AS cust, l_price AS v
			FROM it_sf_supplier, it_sf_lineitem, it_sf_orders, it_sf_customer, it_sf_nation n1, it_sf_nation n2
			WHERE s_suppkey = l_suppkey AND o_orderkey = l_orderkey AND c_custkey = o_custkey
			AND s_nationkey = n1.n_nationkey AND c_nationkey = n2.n_nationkey
			AND ((n1.n_name = 'N1' AND n2.n_name = 'N2') OR (n1.n_name = 'N2' AND n2.n_name = 'N1'))) x
			GROUP BY supp, cust ORDER BY supp, cust`,
		// orders alone is a single-table branch: the parallel dimension reader.
		"dims": `SELECT l_orderkey, o_orderdate, l_price, s_nationkey FROM it_sf_lineitem, it_sf_orders, it_sf_supplier
			WHERE l_orderkey = o_orderkey AND l_suppkey = s_suppkey AND s_nationkey < 4 AND o_orderdate >= DATE '1994-02-01'`,
		"rows": `SELECT l_orderkey, o_orderkey, c_custkey, l_price FROM it_sf_lineitem, it_sf_orders, it_sf_customer
			WHERE l_orderkey = o_orderkey AND o_custkey = c_custkey AND c_nationkey < 2`,
	}
	run := func(sql string, snowflake bool) ([]string, string) {
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
		if err := st.SetSqlQuery(h.ctx, sql); err != nil {
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
		kind := ""
		if plan, ok := sample["plan"].(map[string]any); ok {
			kind, _ = plan["kind"].(string)
			if n, _ := plan["parallel_dimensions"].(float64); n > 0 {
				kind += "+parallel_dimensions"
			}
		}
		return rows, kind
	}
	for name, sql := range queries {
		t.Run(name, func(t *testing.T) {
			want, wantKind := run(sql, false)
			got, kind := run(sql, true)
			if strings.Contains(wantKind, "streaming_snowflake") {
				t.Fatalf("eager baseline planned %s", wantKind)
			}
			switch name {
			case "dims":
				if kind != "parallel_streaming_star_join+parallel_dimensions" {
					t.Fatalf("plan kind %q, want parallel fact and dimension readers", kind)
				}
			case "q5", "q3":
				// q3's broad ship-date filter also reads its fact in parallel.
				if !strings.HasPrefix(kind, "parallel_streaming_star_join") {
					t.Fatalf("plan kind %q, want the parallel fact reader", kind)
				}
			}
			if len(want) == 0 {
				t.Fatal("fixture query returned no rows")
			}
			if name == "rows" || name == "dims" {
				sort.Strings(got)
				sort.Strings(want)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v\nwant %v", got, want)
			}
		})
	}
	// #654: under a lowered branch cap the dimension that crosses it spills to
	// partitions (the parallel dimension reader re-reads it a page at a time),
	// and each query returns the eager join's rows.
	// Without spilling, a cap the parallel dimension reader reaches falls back
	// to the eager join, which must still be able to read: the reader stops
	// its workers without interrupting the execution's connections.
	t.Run("cap fallback", func(t *testing.T) {
		oldCap := snowflakeBranchRows
		t.Cleanup(func() { snowflakeBranchRows, testNoJoinSpill = oldCap, false })
		snowflakeBranchRows = oldCap
		want, _ := run(queries["dims"], false)
		sort.Strings(want)
		snowflakeBranchRows, testNoJoinSpill = 64, true
		got, kind := run(queries["dims"], true)
		sort.Strings(got)
		// The eager join records its own plan once the stream falls back.
		if strings.Contains(kind, "streaming") || !reflect.DeepEqual(got, want) {
			t.Fatalf("plan %q; got %d rows, want %d", kind, len(got), len(want))
		}
	})
	t.Run("spilled", func(t *testing.T) {
		oldCap, oldParts := snowflakeBranchRows, joinSpillPartitions
		t.Cleanup(func() { snowflakeBranchRows, joinSpillPartitions = oldCap, oldParts })
		joined := map[string]bool{}
		for name, sql := range queries {
			snowflakeBranchRows = oldCap
			want, _ := run(sql, false)
			for _, limit := range []int{64, 400, 1500} {
				snowflakeBranchRows, joinSpillPartitions = limit, 8
				before := joinSpillStats.joined.Load()
				got, _ := run(sql, true)
				if joinSpillStats.joined.Load() != before {
					joined[name] = true
				}
				g, w := slices.Clone(got), slices.Clone(want)
				if name == "rows" || name == "dims" {
					sort.Strings(g)
					sort.Strings(w)
				}
				if !reflect.DeepEqual(g, w) {
					t.Fatalf("%s, cap %d: got %v\nwant %v", name, limit, g, w)
				}
			}
		}
		for _, name := range []string{"dims", "rows", "q7"} {
			if !joined[name] {
				t.Errorf("%s never joined a spilled dimension (%v)", name, joined)
			}
		}
	})
}

// snowflakeReaderRows renders rows as sqlHarness.query does, plus each
// column's Arrow type, so a changed scale or type also fails a comparison.
func snowflakeReaderRows(rdr array.RecordReader) []string {
	defer rdr.Release()
	var rows []string
	for rdr.Next() {
		rec := rdr.RecordBatch()
		for r := 0; r < int(rec.NumRows()); r++ {
			cells := make([]string, rec.NumCols())
			for c := 0; c < int(rec.NumCols()); c++ {
				col := rec.Column(c)
				cell := "NULL"
				if !col.IsNull(r) {
					cell = col.ValueStr(r)
					if dec, ok := col.(*array.Decimal128); ok {
						cell = formatDecimal(dec.Value(r).BigInt(), dec.DataType().(*arrow.Decimal128Type).Scale)
					}
				}
				cells[c] = cell + "/" + col.DataType().String()
			}
			rows = append(rows, strings.Join(cells, "|"))
		}
	}
	return rows
}
