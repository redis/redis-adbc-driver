// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func memoryAliasFixture(t *testing.T) (*executor, *joinItem, scanRequest) {
	t.Helper()
	var values []string
	for year := 2000; year < 2005; year++ {
		for _, sale := range []string{"s", "c", "w"} {
			for customer := 1; customer <= 2; customer++ {
				price := "1.25"
				if year == 2001 && sale == "s" && customer == 2 {
					price = "-2.00"
				}
				values = append(values, fmt.Sprintf("(%d,%d,CAST(%s AS DECIMAL(18,2)),'%s')", customer, year, price, sale))
			}
		}
	}
	aliases := []string{"a", "b", "c", "d", "e", "f"}
	sales := []string{"s", "s", "c", "c", "w", "w"}
	var from, where []string
	for i, alias := range aliases {
		from = append(from, "totals "+alias)
		year := 2001 + i%2
		where = append(where, fmt.Sprintf("%s.sale_type='%s' AND %s.dyear=%d AND %s.year_total>0", alias, sales[i], alias, year, alias))
		if i > 0 {
			where = append(where, "a.customer_id="+alias+".customer_id")
		}
	}
	q := `WITH totals(customer_id,dyear,year_total,sale_type) AS (VALUES ` + strings.Join(values, ",") + `) SELECT a.customer_id FROM ` + strings.Join(from, ",") + ` WHERE ` + strings.Join(where, " AND ")
	e, p := deferredTestPlan(t, q)
	e.sess = newSession(defaultSchema)
	if p.meta.join == nil || len(p.meta.join.items) != 6 {
		t.Fatal("fixture needs six parsed shared aliases")
	}
	it := p.meta.join.items[0]
	for _, other := range p.meta.join.items {
		if other.base != it.base {
			t.Fatal("fixture did not share the CTE source")
		}
	}
	view := it.view()
	wp, err := e.planWhere(context.Background(), andAll(it.pushed), view, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, it, scanRequest{meta: view, where: wp, need: map[string]bool{it.prefix + "customer_id": true}}
}

func TestMemoryAliasPrefilterCopiesOnlyNecessaryYearAndOwnsMaps(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	ctx := context.Background()
	filter, err := e.bindMemoryAliasFilter(ctx, it, req, nil)
	if err != nil || filter == nil || len(filter.slots) != 1 || filter.slots[0].name != "dyear" {
		t.Fatalf("not a year-only filter: %+v %v", filter, err)
	}
	copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 6 || len(it.base.mem) != 30 {
		t.Fatalf("qualified map copies%d source%d err%v", len(copied), len(it.base.mem), err)
	}
	// String/decimal predicates have not run: all three sale types and the
	// negative metric remain among these six actual allocated qualified maps.
	types := map[string]bool{}
	negative := false
	for _, row := range copied {
		types[row[it.prefix+"sale_type"].S] = true
		negative = negative || row[it.prefix+"year_total"].D.Sign() < 0
	}
	if len(types) != 3 || !negative {
		t.Fatal("string/decimal clause evaluated early")
	}
	copied[0][it.prefix+"customer_id"] = intValue(it.base.Columns[0].Type, 99)
	if it.base.mem[6]["customer_id"].I != 1 {
		t.Fatal("qualified copy mutated shared source")
	}
	original := req.where.residual
	_, rows, err := e.scanJoinItem(ctx, it, req, nil)
	if err != nil || len(rows) != 1 || req.meta.mem != nil || req.where.residual != original {
		t.Fatalf("full residual/cleanup changed: rows%d %v", len(rows), err)
	}
	for _, row := range it.base.mem {
		if _, ok := row[it.prefix+"dyear"]; ok {
			t.Fatal("shared CTE acquired alias keys")
		}
	}
}

func TestMemoryAliasPrefilterNullMissingAndDefaultFallback(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	ctx := context.Background()
	it.base.mem[6]["year_total"] = nullValue(it.base.Columns[2].Type)
	copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 6 {
		t.Fatal("NULL decimal was filtered early")
	}
	_, rows, err := e.scanJoinItem(ctx, it, req, nil)
	if err != nil || len(rows) != 0 {
		t.Fatalf("full residual NULL semantics changed: %v %v", rows, err)
	}
	it.base.mem[0]["dyear"] = nullValue(it.base.Columns[1].Type)
	copied, err = e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 6 {
		t.Fatal("NULL year fabricated a match")
	}
	delete(it.base.mem[29], "year_total")
	it.base.Columns[2].Missing = "9.99"
	it.base.Columns[2].MissingThrough = 100
	copied, err = e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 30 {
		t.Fatal("missing/defaulted field did not refuse before copies")
	}
	if _, ok := copied[29][it.prefix+"year_total"]; ok {
		t.Fatal("prefilter invented a missing memory default")
	}
}

func TestMemoryAliasPrefilterDoesNotCrossErrorsOrVolatility(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	ctx := context.Background()
	year := &Binary{Op: "=", L: &ColumnRef{Name: it.prefix + "dyear"}, R: &Literal{V: intValue(typeInt32, 2099)}}
	divide := &Binary{Op: "=", L: &Binary{Op: "/", L: &Literal{V: intValue(typeInt32, 1)}, R: &Literal{V: intValue(typeInt32, 0)}}, R: &Literal{V: intValue(typeInt32, 0)}}
	req.where.residual = &Binary{Op: "AND", L: divide, R: year}
	copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 30 {
		t.Fatal("later year filter crossed an earlier error")
	}
	if _, _, err = e.scanJoinItem(ctx, it, req, nil); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("earlier error suppressed: %v", err)
	}
	call := &Func{Name: "SET_CONFIG", Args: []Expr{&Literal{V: stringValue("application_name")}, &Literal{V: stringValue("changed")}, &Literal{V: boolValue(false)}}}
	req.where.residual = &Binary{Op: "AND", L: &Binary{Op: "=", L: call, R: &Literal{V: stringValue("changed")}}, R: year}
	before := e.sess.value("application_name").text
	copied, err = e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 30 || e.sess.value("application_name").text != before {
		t.Fatal("volatile clause was skipped or evaluated early")
	}
}

func TestMemoryAliasPrefilterRejectsTypeErrorsBeforeAnyCopy(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	ctx := context.Background()
	it.base.mem[0]["dyear"] = stringValue("not an integer")
	filter, err := e.bindMemoryAliasFilter(ctx, it, req, nil)
	if err != nil || filter != nil {
		t.Fatal("wrong typed source admitted")
	}
	copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 30 {
		t.Fatal("type mismatch did not use original copy path")
	}
	if _, _, err = e.scanJoinItem(ctx, it, req, nil); err == nil {
		t.Fatal("original typed comparison error suppressed")
	}
	// bindScanPredicate is deliberately not the total-comparison proof.
	bad := &Binary{Op: "AND", L: &Binary{Op: "=", L: &ColumnRef{Name: "b"}, R: &Literal{V: intValue(typeInt32, 1)}}, R: &Literal{V: boolValue(false)}}
	if _, ok := memoryAliasTotal(bad, map[string]ColType{"b": typeBool}, nil); ok {
		t.Fatal("unsafe mixed bool/integer comparison proved total")
	}
}

type aliasCancelContext struct {
	context.Context
	calls, after int
}

func (c *aliasCancelContext) Err() error {
	c.calls++
	if c.calls >= c.after {
		return context.Canceled
	}
	return nil
}
func TestMemoryAliasPrefilterCancellationAndRetainedFallback(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	ctx := &aliasCancelContext{Context: context.Background(), after: 38}
	copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err == nil || copied != nil {
		t.Fatal("cancellation exposed partial qualified maps")
	}
	if len(it.base.mem) != 30 || req.meta.mem != nil {
		t.Fatal("cancellation mutated shared state")
	}
	retained := context.WithValue(context.Background(), retainedAllocationKey{}, newRetainedBudget(0))
	filter, err := e.bindMemoryAliasFilter(retained, it, req, nil)
	if err != nil || filter != nil {
		t.Fatal("retained execution changed admission path")
	}
	copied, err = e.qualifyMemoryJoinRows(nil, it, req, nil)
	if err != nil || len(copied) != 6 {
		t.Fatalf("nil expression context compatibility: %d %v", len(copied), err)
	}
}

func TestMemoryAliasPrefilterSlotsFollowMetadataOrder(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	req.where.residual = &Binary{Op: "AND", L: &Binary{Op: "=", L: &ColumnRef{Name: it.prefix + "dyear"}, R: &Literal{V: intValue(typeInt32, 2001)}}, R: &Binary{Op: ">", L: &ColumnRef{Name: it.prefix + "customer_id"}, R: &Literal{V: intValue(typeInt32, 0)}}}
	filter, err := e.bindMemoryAliasFilter(context.Background(), it, req, nil)
	if err != nil || filter == nil {
		t.Fatal(err)
	}
	var names []string
	for _, slot := range filter.slots {
		names = append(names, slot.name)
	}
	if !reflect.DeepEqual(names, []string{"customer_id", "dyear"}) {
		t.Fatalf("typed reads not in metadata order: %v", names)
	}
}

func TestMemoryAliasParsedDynamicSumUnionSixAliasSelection(t *testing.T) {
	var values []string
	for year := 2000; year < 2005; year++ {
		for customer := 1; customer <= 2; customer++ {
			amount := "1.2"
			if year == 2001 {
				amount = "2.3456"
			}
			if customer == 2 {
				amount = "2.00"
			}
			value := "CAST('" + amount + "' AS NUMERIC)"
			if year == 2001 && customer == 2 {
				value = "CAST(NULL AS NUMERIC)"
			}
			values = append(values, fmt.Sprintf("(%d,%d,%s)", customer, year, value))
		}
	}
	values = append(values, "(1,2001,CAST('3.10' AS NUMERIC))")
	aliases := []string{"a", "b", "c", "d", "e", "f"}
	sales := []string{"s", "s", "c", "c", "w", "w"}
	var from, where []string
	for i, alias := range aliases {
		from = append(from, "year_total "+alias)
		year := "2001"
		if i%2 == 1 {
			year = "2001+1"
		}
		where = append(where, fmt.Sprintf("%s.sale_type='%s' AND %s.dyear=%s AND %s.year_total>0", alias, sales[i], alias, year, alias))
		if i > 0 {
			where = append(where, "a.customer_id="+alias+".customer_id")
		}
	}
	var branches []string
	for _, sale := range []string{"s", "c", "w"} {
		branches = append(branches, `SELECT customer_id,dyear,SUM((amount-CAST(0 AS NUMERIC)+amount)/2) AS year_total,'`+sale+`' AS sale_type FROM raw_sales GROUP BY customer_id,dyear`)
	}
	q := `WITH raw_sales(customer_id,dyear,amount) AS (VALUES ` + strings.Join(values, ",") + `),year_total AS (` + strings.Join(branches, " UNION ALL ") + `) SELECT a.customer_id FROM ` + strings.Join(from, ",") + ` WHERE ` + strings.Join(where, " AND ")
	e, p := deferredTestPlan(t, q)
	ctx := context.Background()
	if p.meta.join == nil || len(p.meta.join.items) != 6 {
		t.Fatal("realistic fixture needs six shared SUM/UNION aliases")
	}
	base := p.meta.join.items[0].base
	metric, ok := base.column("year_total")
	if !ok || metric.Type.Kind != KindDecimal || metric.Type.Precision != 0 {
		t.Fatalf("fixture did not produce dynamic NUMERIC SUM metadata: %+v", metric)
	}
	dynamic, typedNull := false, false
	for _, row := range base.mem {
		v := row["year_total"]
		dynamic = dynamic || !v.Null && v.T.Scale != metric.Type.Scale
		typedNull = typedNull || v.Null && v.T.Kind == KindDecimal
	}
	if !dynamic || !typedNull {
		t.Fatal("fixture lacks runtime NUMERIC scales or typed NULL SUM")
	}
	for _, it := range p.meta.join.items {
		if it.base != base {
			t.Fatal("UNION CTE was recomputed per alias")
		}
		view := it.view()
		wp, err := e.planWhere(ctx, andAll(it.pushed), view, nil)
		if err != nil {
			t.Fatal(err)
		}
		req := scanRequest{meta: view, where: wp, need: map[string]bool{it.prefix + "customer_id": true}}
		filter, err := e.bindMemoryAliasFilter(ctx, it, req, nil)
		if err != nil || filter == nil {
			t.Fatalf("real parsed alias %s did not select year prefilter: %v", it.alias, err)
		}
		copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
		if err != nil || len(copied) != 6 || len(base.mem) != 30 {
			t.Fatalf("real parsed alias %s copied%d rows from%d: %v", it.alias, len(copied), len(base.mem), err)
		}
		for _, row := range copied {
			v := row[it.prefix+"year_total"]
			if !v.Null && (v.D == nil || v.T.Scale == 0) {
				t.Fatal("dynamic decimal was rounded/coerced while qualifying")
			}
		}
	}
	rows, err := e.runSelect(ctx, p, nil)
	if err != nil || len(rows) != 1 || rows[0][0].I != 1 {
		t.Fatalf("complete SUM/UNION residual output changed: %v %v", rows, err)
	}
}

func TestMemoryAliasLiteralFoldKeepsLazyErrorsAndOriginalResidual(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	ctx := context.Background()
	miss := &Binary{Op: "=", L: &ColumnRef{Name: it.prefix + "dyear"}, R: &Literal{V: intValue(typeInt32, 2099)}}
	for _, bad := range []Expr{
		&Binary{Op: "/", L: &Literal{V: intValue(typeInt32, 1)}, R: &Literal{V: intValue(typeInt32, 0)}},
		&Binary{Op: "+", L: &Literal{V: intValue(typeInt64, 9223372036854775807)}, R: &Literal{V: intValue(typeInt64, 1)}},
	} {
		req.where.residual = &Binary{Op: "AND", L: miss, R: &Binary{Op: "=", L: bad, R: &Literal{V: intValue(typeInt32, 0)}}}
		original := req.where.residual
		copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
		if err != nil || len(copied) != 30 {
			t.Fatalf("literal fold error escaped or caused partial filtering: %d %v", len(copied), err)
		}
		_, rows, err := e.scanJoinItem(ctx, it, req, nil)
		if err != nil || len(rows) != 0 || req.where.residual != original {
			t.Fatalf("short-circuited division/overflow changed semantics: %v %v", rows, err)
		}
	}
	rhs := &Binary{Op: "+", L: &Literal{V: intValue(typeInt32, 2001)}, R: &Literal{V: intValue(typeInt32, 1)}}
	req.where.residual = &Binary{Op: "=", L: &ColumnRef{Name: it.prefix + "dyear"}, R: rhs}
	original := req.where.residual
	copied, err := e.qualifyMemoryJoinRows(ctx, it, req, nil)
	if err != nil || len(copied) != 6 || req.where.residual != original || req.where.residual.(*Binary).R != rhs {
		t.Fatal("safe private fold did not preserve original residual")
	}
}

func TestMemoryAliasDynamicNumericCompatibilityAndLiteralBounds(t *testing.T) {
	want := decimalType(0, 0)
	value, err := decodeStored("5.4456", decimalType(18, 4))
	if err != nil {
		t.Fatal(err)
	}
	beforeType, beforeCoefficient := value.T, value.D.String()
	if !memoryAliasValueType(value, want) || value.T != beforeType || value.D.String() != beforeCoefficient {
		t.Fatal("unbounded NUMERIC value was rejected or coerced")
	}
	if !memoryAliasValueType(nullValue(decimalType(38, 16)), want) {
		t.Fatal("typed NULL NUMERIC scale rejected")
	}
	if memoryAliasValueType(value, decimalType(18, 2)) || memoryAliasValueType(floatValue(typeFloat64, 1), want) || memoryAliasValueType(Value{T: want}, want) {
		t.Fatal("fixed/malformed/wrong-kind numeric accepted")
	}
	e := &executor{}
	for _, leaf := range []Expr{&Param{Index: 0}, &ColumnRef{Name: "x"}, &Func{Name: "NEXTVAL"}, &Literal{V: intValue(typeInt32, 1), Unknown: true}, &Literal{V: nullValue(typeInt32)}} {
		expr := &Binary{Op: "+", L: leaf, R: &Literal{V: intValue(typeInt32, 1)}}
		result, ok := e.foldMemoryAliasIntegers(context.Background(), expr)
		if !ok {
			t.Fatal("unsupported operands should remain for totality fallback")
		}
		if _, folded := result.(*Literal); folded {
			t.Fatal("nonliteral/unknown/NULL operand was evaluated")
		}
	}
	var many Expr = &Literal{V: intValue(typeInt32, 1)}
	for range 20 {
		many = &Binary{Op: "+", L: many, R: &Literal{V: intValue(typeInt32, 1)}}
	}
	if _, ok := e.foldMemoryAliasIntegers(context.Background(), many); ok {
		t.Fatal("unbounded literal operation chain admitted")
	}
}

func TestMemoryAliasDoesNotRebindSpecialColumnRoles(t *testing.T) {
	e, it, req := memoryAliasFixture(t)
	req.where.residual = &Binary{Op: "=", L: &ColumnRef{Name: it.prefix + "dyear", item: "field"}, R: &Literal{V: intValue(typeInt32, 2001)}}
	copied, err := e.qualifyMemoryJoinRows(context.Background(), it, req, nil)
	if err != nil || len(copied) != 30 {
		t.Fatal("special column role was erased by alias substitution")
	}
}
