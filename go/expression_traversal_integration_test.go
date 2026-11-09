// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestSQLExpressionTraversalAggregateScopes(t *testing.T) {
	hs := pushdownHarnesses(t)
	h := hs["default"]
	h.dropTables("it_expr_scope")
	h.exec("CREATE TABLE it_expr_scope(g INTEGER,v INTEGER)")
	h.exec("INSERT INTO it_expr_scope VALUES(1,10),(1,20),(2,5),(2,NULL)")
	queries := []struct {
		sql  string
		want []string
	}{
		{"SELECT g,COALESCE(SUM(v),0)+ABS(g) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|31", "2|7"}},
		{"SELECT g,COUNT(DISTINCT v)+ABS(g) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|3", "2|3"}},
		{"SELECT g,SUM(v) FILTER(WHERE v>5) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|30", "2|NULL"}},
		{"SELECT g,COUNT(*),SUM(COUNT(*)) OVER(ORDER BY g ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|2|2", "2|2|4"}},
		{"SELECT g,COUNT(DISTINCT v),SUM(COUNT(DISTINCT v)) OVER(ORDER BY g) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|2|2", "2|1|3"}},
		{"SELECT g,(g,COUNT(*))=(SELECT 1,2) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|true", "2|false"}},
		{"SELECT g,CAST('bad' AS INTEGER DEFAULT g ON CONVERSION ERROR)+COUNT(*) FROM it_expr_scope GROUP BY g ORDER BY g", []string{"1|3", "2|4"}},
		{"SELECT g,COUNT(*) FROM it_expr_scope GROUP BY g HAVING (g,COUNT(*)) IN(SELECT 1,2) ORDER BY g", []string{"1|2"}},
		{"SELECT g,COUNT(*) FROM it_expr_scope GROUP BY g HAVING EXISTS(SELECT SUM(v) FROM it_expr_scope) ORDER BY g", []string{"1|2", "2|2"}},
	}
	forModes(t, hs, func(mode string, h *sqlHarness) {
		for _, q := range queries {
			h.expectRows(q.sql, q.want...)
		}
		h.expectQueryError("SELECT ABS(g ORDER BY v) FROM it_expr_scope", adbc.StatusInvalidArgument, "ORDER BY specified, but ABS is not an aggregate function")
		h.expectQueryError("SELECT ABS(g) FILTER(WHERE v>0) FROM it_expr_scope", adbc.StatusInvalidArgument, "FILTER specified, but ABS is not an aggregate function")
		h.expectQueryError("SELECT SUM(SUM(v)) FROM it_expr_scope", adbc.StatusInvalidArgument, "aggregate function calls cannot be nested")
		if mode == "default" {
			mode = PushdownExact
		}
		if _, _, indexed := h.planIn(queries[0].sql, mode); indexed != (mode != PushdownNone) {
			t.Errorf("%s: ordinary aggregate pushdown eligibility changed: %t", mode, indexed)
		}
	})
	// Exercise the other outside-column consumer directly; result equality alone
	// would not catch accidentally disabling COUNT DISTINCT pushdown.
	for _, i := range []int{1, 4} {
		ctx, e, plan, wp := distinctSnapshotTestPlan(t, h, queries[i].sql)
		if _, ok, err := e.indexCountDistinct(ctx, plan, wp, plan.aggregateCalls(), nil); err != nil || !ok {
			t.Fatalf("distinct aggregate pushdown rejected scoped expression: ok=%t error=%v", ok, err)
		}
	}
}
