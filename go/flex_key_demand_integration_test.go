// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// searchQueryHook records the query of every FT.SEARCH and FT.AGGREGATE.
type searchQueryHook struct {
	mu      sync.Mutex
	queries []string
}

func (h *searchQueryHook) see(c goredis.Cmder) {
	name := strings.ToLower(c.Name())
	if name != "ft.search" && name != "ft.aggregate" || len(c.Args()) < 3 {
		return
	}
	if q, ok := c.Args()[2].(string); ok {
		h.mu.Lock()
		h.queries = append(h.queries, q)
		h.mu.Unlock()
	}
}
func (h *searchQueryHook) take() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.queries
	h.queries = nil
	return q
}
func (h *searchQueryHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *searchQueryHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		h.see(c)
		return next(ctx, c)
	}
}
func (h *searchQueryHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, c := range cmds {
			h.see(c)
		}
		return next(ctx, cmds)
	}
}

// With Flex, an SSB-shaped streamed star join pushes its date demand as
// monthly ranges: 1993's 365 yyyymmdd keys become 12 terms, and neither the
// orphan key 19930132 between two months nor a NULL key is admitted
// (integerRunUnionQuery). Its 1,500 suppliers have no filter of their own, so
// their demand, over maxUnionTerms, is not pushed; a region filter makes it
// a filtered dimension's demand again (starDimensionUnfiltered). The same
// holds for a snowflake branch of suppliers and their nations: without a
// filter in the branch the supplier demand is dropped, and a nation filter
// pushes it. Every result equals the rows computed here.
func TestSQLFlexStarKeyDemandRangesAndUnfilteredDimensions(t *testing.T) {
	h := newSQLHarnessWithOptions(t, map[string]string{OptionStringFlex: "true", OptionStringQueryWorkers: "4", OptionStringQueryWorkerBudget: "4"})
	tables := []string{"it_fkd_lineorder", "it_fkd_date", "it_fkd_supplier", "it_fkd_nation"}
	h.dropTables(tables...)
	t.Cleanup(func() { h.dropTables(tables...) })
	insert := func(table string, rows []string) {
		h.exec("INSERT INTO " + table + " VALUES " + strings.Join(rows, ","))
	}
	type day struct{ key, year int }
	var days []day
	var rows []string
	for d := time.Date(1992, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() < 1999; d = d.AddDate(0, 0, 1) {
		key := d.Year()*10000 + int(d.Month())*100 + d.Day()
		days = append(days, day{key, d.Year()})
		rows = append(rows, fmt.Sprintf("(%d,%d)", key, d.Year()))
	}
	h.exec("CREATE TABLE it_fkd_date (d_datekey INTEGER, d_year INTEGER)")
	insert("it_fkd_date", rows)
	if len(days) <= maxUnionTerms || len(days) > flexDemandTerms {
		t.Fatalf("fixture has %d dates", len(days))
	}
	const suppliers = 1500
	regions := []string{"AFRICA", "AMERICA", "ASIA", "EUROPE", "MIDDLE EAST"}
	rows = nil
	for s := 1; s <= suppliers; s++ {
		rows = append(rows, fmt.Sprintf("(%d,'%s',%d)", s, regions[s%5], s%25))
	}
	h.exec("CREATE TABLE it_fkd_supplier (s_suppkey BIGINT, s_region VARCHAR, s_nationkey INTEGER)")
	insert("it_fkd_supplier", rows)
	rows = nil
	for n := 0; n < 25; n++ {
		rows = append(rows, fmt.Sprintf("(%d,'N%d')", n, n))
	}
	h.exec("CREATE TABLE it_fkd_nation (n_nationkey INTEGER, n_name VARCHAR)")
	insert("it_fkd_nation", rows)
	type fact struct {
		date, supp *int
		revenue    int
	}
	var facts []fact
	rows = nil
	for i := 0; i < 8000; i++ {
		f := fact{revenue: i % 1000}
		date := days[(i*7)%len(days)].key
		if i%97 == 0 {
			date = 19930132 // not a date: between two of 1993's monthly ranges
		}
		supp := i%1600 + 1 // suppliers 1501-1600 do not exist
		if i%53 != 0 {
			f.date = &date
		}
		if i%89 != 0 {
			f.supp = &supp
		}
		facts = append(facts, f)
		text := func(p *int) string {
			if p == nil {
				return "NULL"
			}
			return fmt.Sprint(*p)
		}
		rows = append(rows, fmt.Sprintf("(%s,%s,%d)", text(f.date), text(f.supp), f.revenue))
	}
	h.exec("CREATE TABLE it_fkd_lineorder (lo_orderdate INTEGER, lo_suppkey BIGINT, lo_revenue BIGINT)")
	insert("it_fkd_lineorder", rows)
	year := map[int]int{}
	for _, d := range days {
		year[d.key] = d.year
	}
	// expectYears is the grouped result, by year, of the facts whose date is
	// in years and whose supplier is a supplier that passes.
	expectYears := func(years []int, supplier func(int) bool) string {
		var out []string
		for _, y := range years {
			count, sum := 0, 0
			for _, f := range facts {
				if f.date == nil || f.supp == nil || year[*f.date] != y || *f.supp > suppliers || !supplier(*f.supp) {
					continue
				}
				count++
				sum += f.revenue
			}
			out = append(out, fmt.Sprintf("%d/int32|%d/int64|%d/decimal256(76, 0)", y, count, sum))
		}
		return strings.Join(out, "\n")
	}
	expect := func(supplier func(int) bool) string { return expectYears([]int{1993}, supplier) }

	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Statements run on the connection's execution pool: hook a fresh one and
	// leave it idle, so every execution below reuses it.
	impl := st.(*statementImpl).conn
	hook := &searchQueryHook{}
	impl.store.client.AddHook(hook)
	pool := impl.newExecutionPool(impl.timeouts)
	pool.client.AddHook(hook)
	impl.store.idleExecution = pool
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	run := func(query string) (string, map[string]any, []string) {
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
		hook.take()
		r, _, err := st.ExecuteQuery(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(snowflakeReaderRows(r), "\n")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var sample map[string]any
		if err := json.Unmarshal(b, &sample); err != nil {
			t.Fatal(err)
		}
		plan, _ := sample["plan"].(map[string]any)
		return got, plan, hook.take()
	}
	const join = `SELECT d_year, COUNT(*), SUM(lo_revenue) FROM it_fkd_lineorder, it_fkd_date, it_fkd_supplier
		WHERE lo_orderdate = d_datekey AND lo_suppkey = s_suppkey AND d_year = 1993`
	dateTerm := regexp.MustCompile(`@lo_orderdate:\[(-?\d+) (-?\d+)\]`)
	monthly := func(queries []string) []string {
		t.Helper()
		var demand string
		for _, q := range queries {
			if strings.Contains(q, "@lo_orderdate:") {
				if demand != "" && q != demand {
					t.Fatalf("fact queries differ:\n%s\n%s", demand, q)
				}
				demand = q
			}
		}
		var terms []string
		for _, m := range dateTerm.FindAllStringSubmatch(demand, -1) {
			terms = append(terms, m[1]+"-"+m[2])
		}
		slices.Sort(terms)
		return terms
	}
	var months []string
	for m := 1; m <= 12; m++ {
		last := time.Date(1993, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		months = append(months, fmt.Sprintf("1993%02d01-1993%02d%02d", m, m, last))
	}
	everySupplier := func(int) bool { return true }
	got, plan, queries := run(join + ` GROUP BY d_year ORDER BY d_year`)
	if want := expect(everySupplier); got != want {
		t.Fatalf("unfiltered suppliers: got %q, want %q", got, want)
	}
	if kind, _ := plan["kind"].(string); !strings.Contains(kind, "star_join") {
		t.Fatalf("not a streamed star join: %v", plan)
	}
	if terms := monthly(queries); !slices.Equal(terms, months) {
		t.Fatalf("date demand terms %v, want %v; queries:\n%s", terms, months, strings.Join(queries, "\n"))
	}
	for _, q := range queries {
		if strings.Contains(q, "@lo_suppkey:") {
			t.Fatalf("unfiltered supplier demand pushed: %.300s", q)
		}
	}
	got, _, queries = run(join + ` AND s_region = 'ASIA' GROUP BY d_year ORDER BY d_year`)
	if want := expect(func(s int) bool { return regions[s%5] == "ASIA" }); got != want {
		t.Fatalf("filtered suppliers: got %q, want %q", got, want)
	}
	if terms := monthly(queries); !slices.Equal(terms, months) {
		t.Fatalf("date demand terms with a supplier filter %v, want %v", terms, months)
	}
	supplierDemand := func(queries []string) bool {
		for _, q := range queries {
			if strings.Contains(q, "@lo_suppkey:") {
				return true
			}
		}
		return false
	}
	if !supplierDemand(queries) {
		t.Fatalf("filtered supplier demand not pushed; queries:\n%s", strings.Join(queries, "\n"))
	}

	// Snowflake: suppliers and their nations form one branch.
	oldFact := snowflakeMinFactRows
	snowflakeMinFactRows = 1
	defer func() { snowflakeMinFactRows = oldFact }()
	const branch = `SELECT d_year, COUNT(*), SUM(lo_revenue) FROM it_fkd_lineorder, it_fkd_date, it_fkd_supplier, it_fkd_nation
		WHERE lo_orderdate = d_datekey AND lo_suppkey = s_suppkey AND s_nationkey = n_nationkey AND d_year = 1993`
	got, plan, queries = run(branch + ` GROUP BY d_year ORDER BY d_year`)
	if want := expect(everySupplier); got != want {
		t.Fatalf("unfiltered branch: got %q, want %q", got, want)
	}
	if plan["snowflake"] != true {
		t.Fatalf("not a streamed snowflake join: %v", plan)
	}
	if terms := monthly(queries); !slices.Equal(terms, months) || supplierDemand(queries) {
		t.Fatalf("unfiltered branch: date terms %v, supplier demand %v; queries:\n%.2000s", terms, supplierDemand(queries), strings.Join(queries, "\n"))
	}
	// A nation filter in the branch makes its supplier demand a filtered
	// dimension's. All years: the date dimension is unfiltered here, so its
	// 2,557 keys are not pushed, and the fact's filter is the supplier demand
	// alone, broad enough for the parallel reader to send it to Search (a
	// selective one would take the serial exact-directory scan).
	const branchAll = `SELECT d_year, COUNT(*), SUM(lo_revenue) FROM it_fkd_lineorder, it_fkd_date, it_fkd_supplier, it_fkd_nation
		WHERE lo_orderdate = d_datekey AND lo_suppkey = s_suppkey AND s_nationkey = n_nationkey AND n_nationkey < 13
		GROUP BY d_year ORDER BY d_year`
	got, plan, queries = run(branchAll)
	if want := expectYears([]int{1992, 1993, 1994, 1995, 1996, 1997, 1998}, func(s int) bool { return s%25 < 13 }); got != want {
		t.Fatalf("filtered branch: got %q, want %q", got, want)
	}
	dateDemand := false
	for _, q := range queries {
		dateDemand = dateDemand || strings.Contains(q, "@lo_orderdate:")
	}
	if plan["snowflake"] != true || !supplierDemand(queries) || dateDemand {
		t.Fatalf("filtered branch: supplier demand %v, unfiltered date demand %v (plan %v); queries:\n%.2000s", supplierDemand(queries), dateDemand, plan, strings.Join(queries, "\n"))
	}
}
