// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func retainedTestAdmission() *retainedExecution {
	return &retainedExecution{working: newRetainedBudget(256 << 20), vetted: map[*SelectStmt]bool{}, views: map[string]string{}}
}
func TestRetainedExecutionWorkContextAndStickyAllocation(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	ctx := &retainedWorkContext{Context: parent, work: newRetainedBudget(2), cancel: cancel}
	if cancelled(ctx) != nil || cancelled(ctx) != nil {
		t.Fatal("work credits failed early")
	}
	if err := cancelled(ctx); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("exhaustion became ordinary cancellation", err)
	}
	if err := cancelled(ctx); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("exhausted work budget revived", err)
	}
	budget := newRetainedBudget(1024)
	allocation := context.WithValue(context.Background(), retainedAllocationKey{}, budget)
	if err := retainedAllocate(allocation, math.MaxInt64, 2); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("allocation overflow accepted", err)
	}
	if err := retainedAllocate(allocation, 1, 1); err == nil {
		t.Fatal("overflow did not poison admission")
	}
	if err := retainedAllocate(context.Background(), math.MaxInt64, math.MaxInt64); err != nil {
		t.Fatal("ordinary context was governed", err)
	}
}

func TestRetainedExecutionExpressionPolicy(t *testing.T) {
	for _, query := range []string{
		"SELECT REPEAT('x',2147483647)",
		"SELECT 1 LIMIT length(REPEAT('x',2147483647))",
		"VALUES (REPEAT('x',2147483647))", "SELECT LPAD('x',2147483647,'y')", "SELECT REPLACE('x','','large')",
		"SELECT SET_CONFIG('timezone','UTC',false)", "SELECT RANDOM()", "SELECT ROUND(1.0,2147483647)",
		"SELECT ROUND(1.0,1+1)", "SELECT CAST('x' AS CHAR(65537))",
		"SELECT * FROM generate_series(1,1000000000)",
		"WITH RECURSIVE n AS (SELECT 1 UNION ALL SELECT 1 FROM n) SELECT * FROM n",
		"SELECT * FROM information_schema.tables",
	} {
		t.Run(query, func(t *testing.T) {
			parsed, err := ParseScript(query)
			if err != nil {
				t.Fatal(err)
			}
			r := retainedTestAdmission()
			for range 2 {
				if err := r.vet(parsed[0].Stmt.(*SelectStmt), 0); err == nil {
					t.Fatal("unsafe query admitted (including after earlier rejection)")
				}
			}
		})
	}
	for _, query := range []string{"SELECT 1+2, LOWER('ABC'), COALESCE(NULL,3), ROUND(1.23,2)", "SELECT ROW_NUMBER() OVER (ORDER BY id) FROM t", "WITH a AS (SELECT id FROM t) SELECT DISTINCT id FROM a ORDER BY id LIMIT 2 OFFSET 1"} {
		parsed, err := ParseScript(query)
		if err != nil {
			t.Fatal(err)
		}
		if err := retainedTestAdmission().vet(parsed[0].Stmt.(*SelectStmt), 0); err != nil {
			t.Fatal(query, err)
		}
	}
}

func TestRetainedExecutionRejectsExpandingValuesAndViewChange(t *testing.T) {
	r := retainedTestAdmission()
	if err := r.value(stringValue(strings.Repeat("x", 64<<10+1))); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal(err)
	}
	if err := r.working.reserve(1); err == nil {
		t.Fatal("value admission revived after exhaustion")
	}
	r = retainedTestAdmission()
	view := &viewMeta{Schema: defaultSchema, Name: "owned_test", SQL: "SELECT 1"}
	if err := r.view(view); err != nil {
		t.Fatal(err)
	}
	view.SQL = "SELECT 2"
	if err := r.view(view); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("view changed during planning", err)
	}
}
