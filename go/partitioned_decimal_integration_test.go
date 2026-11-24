// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Exact decimal SUM/AVG (TPC-H Q1 and the Q18 subquery shape) use client
// partial groups. Two forced row-ID ranges must merge to the serial result,
// including value types/scales, NULL-only groups, zero sums and expressions.
func TestSQLPartitionedDecimalSums(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_partition_decimal")
	h.exec("CREATE TABLE it_partition_decimal (flag VARCHAR, k BIGINT, qty DECIMAL(15,2), price DECIMAL(15,2), disc DECIMAL(15,2), tax DECIMAL(10,4), n INTEGER)")
	h.exec(`INSERT INTO it_partition_decimal VALUES
 ('A',1,17.00,21168.23,0.04,0.0200,3),
 ('A',1,36.00,45983.16,0.09,0.0600,-2),
 ('A',2,-8.00,13309.60,0.10,0.0200,NULL),
 ('N',2,28.00,28955.64,0.09,0.0600,7),
 ('N',3,24.00,22824.48,0.10,0.0400,1),
 ('N',3,-24.00,-22824.48,0.00,0.0000,1),
 ('R',4,NULL,NULL,NULL,NULL,NULL),
 ('R',4,NULL,NULL,0.05,NULL,NULL),
 (NULL,5,0.01,0.03,0.99,9.9999,0)`)
	queries := []string{
		`SELECT flag, SUM(qty), SUM(price), SUM(price * (1 - disc)), SUM(price * (1 - disc) * (1 + tax)),
		 AVG(qty), AVG(price), AVG(disc), COUNT(*) FROM it_partition_decimal GROUP BY flag`,
		"SELECT k FROM it_partition_decimal GROUP BY k HAVING SUM(qty) > 20",
		"SELECT k, SUM(qty), AVG(tax), SUM(n), SUM(qty * n) FROM it_partition_decimal GROUP BY k",
		"SELECT flag, k, SUM(price - disc), AVG(qty * 2) FROM it_partition_decimal WHERE k < 5 GROUP BY flag, k",
	}
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			ctx, e, plan, wp := distinctSnapshotTestPlan(t, h, sql)
			p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
			if !ok {
				t.Fatal("exact decimal sums were not admitted to partitioned aggregation")
			}
			if !p.partialGroups || p.server {
				t.Fatalf("decimal sums require client partial groups, got partial=%v server=%v", p.partialGroups, p.server)
			}
			spill := tupleSpill{dir: t.TempDir(), prefix: "fixture", combine: p.combine, fanIn: aggregateSpillFanIn}
			for _, r := range [][2]int64{{1, 3}, {4, 9}} {
				if err := e.aggregateTuplePartition(ctx, plan, p, wp, nil, r[0], r[1], false, func(row spillTuple) error { return spill.add(ctx, row) }); err != nil {
					t.Fatal(err)
				}
			}
			if err := spill.flush(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := e.reduceTupleRuns(ctx, plan, p, plan.aggregateCalls(), nil, &spill)
			if err != nil {
				t.Fatal(err)
			}
			want, err := e.serialDriverAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if plan.having != nil {
				// selectAggregate applies HAVING after the serial reduction;
				// partitioned reduction applies it while merging runs.
				env := e.newEnv(ctx, plan.meta.types(), nil)
				kept := want[:0]
				for _, g := range want {
					env.row, env.aggs = g.rep, g.results
					v, err := env.eval(plan.having)
					if err != nil {
						t.Fatal(err)
					}
					if b, ok := truthy(v); ok && b {
						kept = append(kept, g)
					}
				}
				want = kept
			}
			if a, b := decimalGroupsText(got, plan), decimalGroupsText(want, plan); !reflect.DeepEqual(a, b) {
				t.Fatalf("got %v\nwant %v", a, b)
			}
		})
	}
	// Decimal state cannot use server tuples or exact-tuple DISTINCT merges.
	for _, sql := range []string{
		"SELECT flag, SUM(qty), COUNT(DISTINCT k) FROM it_partition_decimal GROUP BY flag",
		"SELECT flag, SUM(qty), MIN(price) FROM it_partition_decimal GROUP BY flag",
		"SELECT SUM(qty) FROM it_partition_decimal",
		"SELECT flag, SUM(qty / 3) FROM it_partition_decimal GROUP BY flag",
	} {
		_, _, plan, _ := distinctSnapshotTestPlan(t, h, sql)
		if _, ok := tupleAggregateShape(plan, plan.aggregateCalls()); ok {
			t.Fatalf("%s: unsupported decimal shape admitted", sql)
		}
	}
}

// decimalGroupsText retains result types, so a different decimal scale fails.
func decimalGroupsText(groups []aggGroup, plan *selectPlan) []string {
	var out []string
	for _, g := range groups {
		var fields []string
		for _, x := range plan.sel.GroupBy {
			fields = append(fields, partitionValueString(g.rep[x.(*ColumnRef).Name]))
		}
		for _, f := range plan.aggregateCalls() {
			v := g.results[f]
			fields = append(fields, fmt.Sprintf("%s(%d,%d,%d)", partitionValueString(v), v.T.Kind, v.T.Precision, v.T.Scale))
		}
		out = append(out, strings.Join(fields, "|"))
	}
	sort.Strings(out)
	return out
}
