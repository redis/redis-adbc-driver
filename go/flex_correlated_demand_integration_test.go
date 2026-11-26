// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// wideUnionHook counts FT.SEARCH calls, and those whose query unions more
// than maxUnionTerms terms.
type wideUnionHook struct{ searches, wide atomic.Int64 }

func (h *wideUnionHook) see(c goredis.Cmder) {
	if !strings.EqualFold(c.Name(), "ft.search") || len(c.Args()) < 3 {
		return
	}
	h.searches.Add(1)
	if q, ok := c.Args()[2].(string); ok && strings.Count(q, "|") >= maxUnionTerms {
		h.wide.Add(1)
	}
}
func (h *wideUnionHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *wideUnionHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		h.see(c)
		return next(ctx, c)
	}
}
func (h *wideUnionHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, c := range cmds {
			h.see(c)
		}
		return next(ctx, cmds)
	}
}

// A correlated subquery's streamed join runs once per distinct set of outer
// values, so with Flex its fact key demand keeps maxUnionTerms (TPC-H Q2's
// minimum supply cost per part: at SF1 its 1,987-supplier demand was pushed
// for each of 747 parts, about 80 s of Search calls). Here 1,360 of 2,000
// suppliers are in the region, and partsupp (6,000 rows) is the streamed
// fact, as at SF1: the outer, uncorrelated join may push their union once,
// but the 30 correlated runs push none. Rows equal the eager join's, with
// NULL costs and suppliers outside the region.
func TestSQLFlexCorrelatedJoinKeepsDemandLimit(t *testing.T) {
	h := newSQLHarnessWithOptions(t, map[string]string{OptionStringFlex: "true", OptionStringQueryWorkers: "4", OptionStringQueryWorkerBudget: "4"})
	tables := []string{"it_cfd_part", "it_cfd_supplier", "it_cfd_partsupp", "it_cfd_nation", "it_cfd_region"}
	h.dropTables(tables...)
	t.Cleanup(func() { h.dropTables(tables...) })
	insert := func(table string, from, to int, row func(int) string) {
		var rows []string
		for i := from; i <= to; i++ {
			rows = append(rows, row(i))
		}
		h.exec("INSERT INTO " + table + " VALUES " + strings.Join(rows, ","))
	}
	h.exec("CREATE TABLE it_cfd_region (r_regionkey INTEGER, r_name VARCHAR)")
	insert("it_cfd_region", 0, 4, func(r int) string {
		return fmt.Sprintf("(%d,'%s')", r, []string{"AFRICA", "AMERICA", "ASIA", "EUROPE", "MIDDLE EAST"}[r])
	})
	// EUROPE (3) holds nations 0-14, 18 and 23: 17 of 25.
	europe := func(n int) bool { return n < 15 || n%5 == 3 }
	h.exec("CREATE TABLE it_cfd_nation (n_nationkey INTEGER, n_regionkey INTEGER)")
	insert("it_cfd_nation", 0, 24, func(n int) string {
		r := n % 5
		if europe(n) {
			r = 3
		}
		return fmt.Sprintf("(%d,%d)", n, r)
	})
	h.exec("CREATE TABLE it_cfd_supplier (s_suppkey BIGINT, s_nationkey INTEGER)")
	insert("it_cfd_supplier", 1, 2000, func(s int) string { return fmt.Sprintf("(%d,%d)", s, s%25) })
	if n := func() (n int) {
		for s := 1; s <= 2000; s++ {
			if europe(s % 25) {
				n++
			}
		}
		return n
	}(); n <= maxUnionTerms || n > flexDemandTerms {
		t.Fatalf("fixture has %d suppliers in the region", n)
	}
	// 300 parts, 30 of size 5.
	h.exec("CREATE TABLE it_cfd_part (p_partkey BIGINT, p_size INTEGER)")
	insert("it_cfd_part", 1, 300, func(p int) string { return fmt.Sprintf("(%d,%d)", p, p%10) })
	// Twenty distinct suppliers per part; some costs are NULL or tie.
	h.exec("CREATE TABLE it_cfd_partsupp (ps_partkey BIGINT, ps_suppkey BIGINT, ps_supplycost INTEGER)")
	insert("it_cfd_partsupp", 0, 5999, func(i int) string {
		p, j := i/20+1, i%20
		cost := fmt.Sprint((p*13 + j*17) % 7)
		if (p+j)%11 == 0 {
			cost = "NULL"
		}
		return fmt.Sprintf("(%d,%d,%s)", p, (p*600+j*97)%2000+1, cost)
	})
	const query = `SELECT p_partkey, s_suppkey, ps_supplycost
		FROM it_cfd_part, it_cfd_supplier, it_cfd_partsupp, it_cfd_nation, it_cfd_region
		WHERE p_partkey = ps_partkey AND s_suppkey = ps_suppkey AND p_size = 5
		AND s_nationkey = n_nationkey AND n_regionkey = r_regionkey AND r_name = 'EUROPE'
		AND ps_supplycost = (SELECT min(ps_supplycost) FROM it_cfd_partsupp, it_cfd_supplier, it_cfd_nation, it_cfd_region
			WHERE p_partkey = ps_partkey AND s_suppkey = ps_suppkey AND s_nationkey = n_nationkey
			AND n_regionkey = r_regionkey AND r_name = 'EUROPE')
		ORDER BY p_partkey, s_suppkey`
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Statements run on the connection's execution pool: hook a fresh one and
	// leave it idle, so every execution below reuses it.
	impl := st.(*statementImpl).conn
	hook := &wideUnionHook{}
	impl.store.client.AddHook(hook)
	pool := impl.newExecutionPool(impl.timeouts)
	pool.client.AddHook(hook)
	impl.store.idleExecution = pool
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	run := func(snowflake bool) ([]string, int64, int64, subqueryWork) {
		t.Helper()
		oldFact, oldDim := snowflakeMinFactRows, snowflakeParallelDimensionRows
		snowflakeMinFactRows, snowflakeParallelDimensionRows = math.MaxInt64, math.MaxInt64
		if snowflake {
			snowflakeMinFactRows = 1
		}
		defer func() { snowflakeMinFactRows, snowflakeParallelDimensionRows = oldFact, oldDim }()
		before, searches := hook.wide.Load(), hook.searches.Load()
		r, s, i := subqueryStats.runs.Load(), subqueryStats.semiJoins.Load(), subqueryStats.inSets.Load()
		rows, _ := h.query(query)
		work := subqueryWork{subqueryStats.runs.Load() - r, subqueryStats.semiJoins.Load() - s, subqueryStats.inSets.Load() - i}
		return rows, hook.wide.Load() - before, hook.searches.Load() - searches, work
	}
	eager, _, _, _ := run(false)
	streamed, wide, searches, work := run(true)
	if len(eager) == 0 || strings.Join(streamed, "\n") != strings.Join(eager, "\n") {
		t.Fatalf("streamed rows differ from the eager join:\n%s\nwant:\n%s", strings.Join(streamed, "\n"), strings.Join(eager, "\n"))
	}
	if work.runs < 10 || searches < work.runs {
		t.Fatalf("the subquery ran %d times with %d Search calls; the fixture must run it per part", work.runs, searches)
	}
	// Only the outer join's reader pushes the region's union, a few times in
	// all (4 here); without the limit each correlated run pushes it about four
	// times (124 for 30 runs).
	if wide >= work.runs {
		t.Fatalf("%d Search calls pushed a union over %d terms for %d correlated runs", wide, maxUnionTerms, work.runs)
	}
}
