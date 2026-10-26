// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func retainedExecuteTest(t *testing.T, h *sqlHarness, query string, limits retainedExecutionLimits) (execResult, error) {
	t.Helper()
	statement, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	_, _, cfg, timeouts, _, _ := retainedSourceFixture(t)
	ctx, e, execution, close, err := newRetainedExecutor(h.ctx, statement.(*statementImpl).executor(), cfg, timeouts, limits)
	if err != nil {
		return execResult{}, err
	}
	defer close()
	parsed, err := ParseScript(query)
	if err != nil {
		return execResult{}, err
	}
	if len(parsed) != 1 {
		t.Fatal("test expects one SELECT")
	}
	result, err := e.execute(ctx, parsed[0], nil, nil, nil)
	if err == nil {
		err = execution.check(ctx)
	}
	return result, err
}
func assertRetainedEqualsQuery(t *testing.T, h *sqlHarness, query string) {
	t.Helper()
	result, err := retainedExecuteTest(t, h, query, defaultRetainedExecutionLimits())
	if err != nil {
		t.Fatal(query, err)
	}
	actual, err := buildRecord(memory.DefaultAllocator, result.cols, result.rows)
	if err != nil {
		t.Fatal(err)
	}
	defer actual.Release()
	// Compare fully fetched physical Arrow values/types, including NULL and
	// nested layouts, rather than rendered SQL text or only the execute call.
	st, err := h.conn.NewStatement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, query); err != nil {
		t.Fatal(err)
	}
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	var records []arrow.RecordBatch
	for reader.Next() {
		record := reader.RecordBatch()
		record.Retain()
		records = append(records, record)
	}
	defer func() {
		for _, record := range records {
			record.Release()
		}
	}()
	if reader.Err() != nil {
		t.Fatal(reader.Err())
	}
	expected := array.NewTableFromRecords(reader.Schema(), records)
	defer expected.Release()
	table := array.NewTableFromRecords(actual.Schema(), []arrow.RecordBatch{actual})
	defer table.Release()
	if strings.HasPrefix(query, "SELECT ARRAY_AGG(id) FROM ") {
		// SQL leaves aggregate element order unspecified without ORDER BY.
		// Cluster cursor arrival can differ across two independent executions;
		// check the exact typed element multiset, including duplicates.
		if !actual.Schema().Equal(reader.Schema()) || len(records) != 1 || records[0].NumRows() != 1 {
			t.Fatal("unordered aggregate schema or row count differs")
		}
		left := slices.Clone(actual.Column(0).(*array.List).ListValues().(*array.Int64).Int64Values())
		right := slices.Clone(records[0].Column(0).(*array.List).ListValues().(*array.Int64).Int64Values())
		slices.Sort(left)
		slices.Sort(right)
		if !slices.Equal(left, right) {
			t.Fatal("unordered aggregate exact element multiset differs", left, right)
		}
		return
	}
	if !actual.Schema().Equal(reader.Schema()) || !array.TableEqual(table, expected) {
		t.Fatalf("retained and ordinary Arrow results differ for %s: %v != %v", query, table, expected)
	}
}

func TestRetainedExecutionGlobalSQLMatchesExecuteQuery(t *testing.T) {
	h := newSQLHarness(t)
	const name = "it_retained_execution"
	h.exec("DROP VIEW IF EXISTS it_retained_execution_view")
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (id BIGINT, grp INTEGER, label TEXT, amount DECIMAL(20,2))")
	t.Cleanup(func() {
		h.exec("DROP VIEW IF EXISTS it_retained_execution_view")
		h.exec("DROP TABLE IF EXISTS " + name)
	})
	h.exec("INSERT INTO " + name + " VALUES (9007199254740993,1,'a',10.25),(9223372036854775807,1,NULL,20.50),(-1,2,'b',NULL),(7,2,'b',5.00),(9,3,'c',4.50)")
	h.exec("CREATE VIEW it_retained_execution_view AS SELECT grp, SUM(amount) AS total FROM " + name + " GROUP BY grp")
	queries := []string{
		"SELECT id FROM " + name + " LIMIT 3 OFFSET 1",
		"SELECT ARRAY_AGG(id) FROM " + name,
		"SELECT id,label,amount FROM " + name + " ORDER BY id DESC LIMIT 3 OFFSET 1",
		"SELECT DISTINCT label FROM " + name + " ORDER BY label NULLS FIRST",
		"SELECT DISTINCT ON (grp) grp,id FROM " + name + " ORDER BY grp,id DESC",
		"SELECT grp, COUNT(*), SUM(amount), MIN(id), MAX(id) FROM " + name + " GROUP BY grp HAVING COUNT(*)>=2 ORDER BY grp",
		"SELECT id, ROW_NUMBER() OVER (PARTITION BY grp ORDER BY id) AS pos, SUM(amount) OVER (PARTITION BY grp) AS total FROM " + name + " QUALIFY pos<=1 ORDER BY id",
		"SELECT grp FROM " + name + " WHERE grp<3 UNION SELECT grp FROM " + name + " WHERE grp>1 ORDER BY grp",
		"SELECT grp FROM " + name + " INTERSECT SELECT grp FROM " + name + " WHERE grp=2",
		"SELECT grp FROM " + name + " EXCEPT SELECT grp FROM " + name + " WHERE grp<>2",
		"SELECT a.id,b.label FROM " + name + " a JOIN " + name + " b ON a.grp=b.grp WHERE a.id<10 ORDER BY a.id,b.id",
		"WITH a AS (SELECT grp,COUNT(*) AS n FROM " + name + " GROUP BY grp) SELECT grp,n FROM a WHERE n>1 ORDER BY grp",
		"SELECT id FROM " + name + " WHERE grp IN (SELECT grp FROM " + name + " GROUP BY grp HAVING COUNT(*)=2) ORDER BY id",
		"SELECT a.id,(SELECT COUNT(*) FROM " + name + " b WHERE b.grp=a.grp) AS n FROM " + name + " a ORDER BY id",
		"SELECT grp,total FROM it_retained_execution_view ORDER BY grp",
		"SELECT ARRAY_AGG(id ORDER BY id), (COUNT(*),MIN(id)) FROM " + name,
		"SELECT id FROM " + name + " WHERE false",
		"SELECT id FROM " + name + " LIMIT 0",
		"SELECT 9007199254740993::BIGINT AS exact, NULL::TEXT AS missing",
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) { assertRetainedEqualsQuery(t, h, query) })
	}
}

func TestRetainedExecutionBudgetFailureAndSideEffectPolicy(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_retained_execution_limits"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " SELECT i FROM generate_series(1,20) AS i")
	for _, budget := range []string{"transport", "source", "working", "work", "join", "window"} {
		t.Run(budget, func(t *testing.T) {
			limits := defaultRetainedExecutionLimits()
			query := "SELECT id FROM " + table + " ORDER BY id"
			switch budget {
			case "transport":
				limits.TransportBytes = 1
			case "source":
				limits.SourceBytes = 1
			case "working":
				limits.WorkingBytes = 1
			case "work":
				limits.Work = 1
			case "join":
				limits.WorkingBytes = 2 << 20
				query = "SELECT a.id,b.id FROM " + table + " a CROSS JOIN " + table + " b CROSS JOIN " + table + " c"
			case "window":
				limits.WorkingBytes = 1<<20 + 60000
				query = "SELECT id,SUM(id) OVER (ORDER BY id) FROM " + table
			}
			result, err := retainedExecuteTest(t, h, query, limits)
			if !isStatus(err, adbc.StatusInvalidState) || result.stream != nil {
				t.Fatal("unbounded coordinator work completed", err)
			}
		})
	}
	before, _ := h.query("SHOW timezone")
	if _, err := retainedExecuteTest(t, h, "SELECT SET_CONFIG('timezone','Pacific/Honolulu',false)", defaultRetainedExecutionLimits()); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	after, _ := h.query("SHOW timezone")
	if !slices.Equal(before, after) {
		t.Fatal("retained SELECT changed session state")
	}
	rows, _ := h.query("SELECT COUNT(*) FROM " + table)
	if !slices.Equal(rows, []string{"20"}) {
		t.Fatal("budget failure modified source rows", rows)
	}
}
