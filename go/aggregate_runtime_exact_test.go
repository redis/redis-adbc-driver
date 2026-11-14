// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestRuntimeExactBigIntAggregateScope(t *testing.T) {
	base := tableMeta{Schema: defaultSchema, KeyPrefix: "adbc:{meta}:rows:proof:"}
	column := columnMeta{Name: "v", Type: typeInt64, Indexed: true}
	if !runtimeExactBigIntAggregate(&base, column, PushdownExact) {
		t.Fatal("immutable managed BIGINT lost runtime proof")
	}
	for _, change := range []func(*tableMeta){
		func(m *tableMeta) { m.Unmanaged = true },
		func(m *tableMeta) { m.KeyRows = true },
		func(m *tableMeta) { m.AppIndex = true },
		func(m *tableMeta) { m.Adopted = true },
		func(m *tableMeta) { m.Rewriting = true },
	} {
		m := base
		change(&m)
		if runtimeExactBigIntAggregate(&m, column, PushdownExact) {
			t.Fatal("non-immutable layout acquired runtime proof", m)
		}
	}
	for _, change := range []func(*columnMeta){
		func(c *columnMeta) { c.Type = typeFloat64 },
		func(c *columnMeta) { c.Type = decimalType(38, 0) },
		func(c *columnMeta) { c.Indexed = false },
		func(c *columnMeta) { c.PlainIndex = true },
		func(c *columnMeta) { c.MissingThrough = 1 },
		func(c *columnMeta) { c.NaNs = true },
	} {
		c := column
		change(&c)
		if runtimeExactBigIntAggregate(&base, c, PushdownExact) {
			t.Fatal("unsupported column acquired runtime proof", c)
		}
	}
	if runtimeExactBigIntAggregate(&base, column, PushdownNone) || column.groupable(PushdownExact) || column.pushable(PushdownExact) {
		t.Fatal("runtime proof leaked into general pushdown eligibility")
	}
}

func TestAggregateInputCountUsesApplicationKey(t *testing.T) {
	f := []*Func{{Name: "COUNT", Star: true}, {Name: "SUM", Args: []Expr{&ColumnRef{Name: "v"}}}}
	for _, unmanaged := range []bool{false, true} {
		items := aggregateInputItems(&tableMeta{Unmanaged: unmanaged}, f)
		field := rowIDField
		if unmanaged {
			field = keyField
		}
		if items[0].expr.(*ColumnRef).Name != field || items[1].expr != f[1].Args[0] {
			t.Fatal("COUNT(*) assumed an internal ID or SUM binding changed", unmanaged)
		}
	}
}

func TestGroupedAggregateInputFallbackScope(t *testing.T) {
	e := &executor{}
	col := &ColumnRef{Name: "v"}
	agg := &Func{Name: "SUM", Args: []Expr{col}}
	for _, tc := range []struct {
		groups []Expr
		aggs   []*Func
		need   map[string]bool
	}{
		{nil, []*Func{agg}, nil},
		{[]Expr{&Binary{Op: "+", L: col, R: &Literal{V: intValue(typeInt64, 1)}}}, []*Func{agg}, nil},
		{[]Expr{col}, []*Func{{Name: "SUM", Args: []Expr{col}, Filter: &Literal{V: boolValue(true)}}}, nil},
		{[]Expr{col}, []*Func{agg}, map[string]bool{"unrelated": true}},
	} {
		handled, err := e.scanGroupedAggregateValues(nil, scanRequest{}, nil, tc.groups, tc.aggs, tc.need, func([]Value) error { t.Fatal("unsupported input was consumed"); return nil })
		if handled || err != nil {
			t.Fatal("unsupported group/input escaped fallback", handled, err)
		}
	}
}
