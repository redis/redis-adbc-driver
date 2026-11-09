// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"slices"
	"strings"
	"testing"
)

func TestExpressionTraversalScopes(t *testing.T) {
	g := &ColumnRef{Name: "g"}
	v := &ColumnRef{Name: "v"}
	order := &ColumnRef{Name: "order"}
	filter := &ColumnRef{Name: "filter"}
	fallback := &ColumnRef{Name: "fallback"}
	inner := &ColumnRef{Name: "body_only"}
	outer := &ColumnRef{Name: "outer", Outer: 1}
	agg := &Func{Name: "SUM", Args: []Expr{v}, OrderBy: []OrderItem{{Expr: order}}, Filter: filter}
	scalar := &Func{Name: "ABS", Args: []Expr{agg}}
	binary := &Binary{Op: "+", L: scalar, R: g}
	cast := &Cast{X: agg, T: typeInt64, OnError: fallback}
	unary := &Unary{Op: "-", X: g}
	null := &IsNull{X: unary}
	row := &RowExpr{Items: []Expr{g, outer}}
	field := &FieldSelect{X: row}
	conditional := &Case{Operand: g, Whens: []WhenClause{{When: null, Then: cast}}, Else: field}
	body := &SelectStmt{Items: []SelectItem{{Expr: &Func{Name: "SUM", Args: []Expr{inner}}}}}
	in := &Subquery{Kind: SubqueryIn, X: &RowExpr{Items: []Expr{g, agg}}, Select: body}
	sub := &Subquery{Kind: SubqueryScalar, Select: body}
	rc0, rc1 := &RowColumn{Sub: sub}, &RowColumn{Sub: sub, Index: 1}
	rowColumns := &RowExpr{Items: []Expr{rc0, rc1}}
	count := &Func{Name: "COUNT", Args: []Expr{v}}
	start, end := &Param{Index: 1}, &Param{Index: 2}
	window := &WindowFunc{Func: &Func{Name: "SUM", Args: []Expr{count}}, Over: &WindowSpec{
		PartitionBy: []Expr{g}, OrderBy: []OrderItem{{Expr: order}},
		Frame: &WindowFrame{Start: FrameBound{Kind: BoundPreceding, Offset: start}, End: FrameBound{Kind: BoundFollowing, Offset: end}},
	}}
	plainWindow := &WindowFunc{Func: &Func{Name: "SUM", Args: []Expr{v}, Filter: filter}, Over: &WindowSpec{PartitionBy: []Expr{g}}}
	nested := &Func{Name: "SUM", Args: []Expr{count}}
	for _, tc := range []struct {
		name         string
		expr         Expr
		all, outside []Expr
		refs         []string
	}{
		{"nil", nil, nil, nil, nil},
		{"aggregate_root", agg, []Expr{agg, v, order, filter}, nil, nil},
		{"scalar_around_aggregate", binary, []Expr{binary, scalar, agg, v, order, filter, g}, []Expr{binary, scalar, g}, []string{"g"}},
		{"cast_error_expression", cast, []Expr{cast, agg, v, order, filter, fallback}, []Expr{cast, fallback}, []string{"fallback"}},
		{"case_row_field_outer_reference", conditional,
			[]Expr{conditional, g, null, unary, g, cast, agg, v, order, filter, fallback, field, row, g, outer},
			[]Expr{conditional, g, null, unary, g, cast, fallback, field, row, g, outer}, []string{"fallback", "g"}},
		{"row_in_operand_without_body_or_row_wrapper", in, []Expr{in, g, agg, v, order, filter}, []Expr{in, g}, []string{"g"}},
		{"scalar_subquery_body_is_separate", sub, []Expr{sub}, []Expr{sub}, nil},
		{"row_columns_share_subquery_not_its_body", rowColumns, []Expr{rowColumns, rc0, sub, rc1, sub}, []Expr{rowColumns, rc0, sub, rc1, sub}, nil},
		{"window_nested_aggregate_and_frame", window, []Expr{window, count, v, g, order, start, end}, []Expr{window, g, order, start, end}, []string{"g", "order"}},
		{"window_own_call_is_not_group_aggregate", plainWindow, []Expr{plainWindow, v, filter, g}, []Expr{plainWindow, v, filter, g}, []string{"filter", "g", "v"}},
		{"nested_aggregate_root_prunes_entire_call", nested, []Expr{nested, count, v}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, walk := range []struct {
				name string
				fn   func(Expr, func(Expr))
				want []Expr
			}{{"all", walkExpr, tc.all}, {"outside", walkOutsideAggregates, tc.outside}} {
				var got []Expr
				walk.fn(tc.expr, func(x Expr) { got = append(got, x) })
				if !slices.Equal(got, walk.want) {
					t.Errorf("%s: visits=%v want=%v", walk.name, got, walk.want)
				}
			}
			refs := map[string]bool{}
			walkOutsideAggregates(tc.expr, func(x Expr) {
				if c, ok := x.(*ColumnRef); ok && c.Outer == 0 {
					refs[c.Name] = true
				}
			})
			var names []string
			for name := range refs {
				names = append(names, name)
			}
			slices.Sort(names)
			if !slices.Equal(names, tc.refs) {
				t.Errorf("current-scope references=%v want=%v", names, tc.refs)
			}
		})
	}
}

func TestExpressionTraversalScalarModifiersRejected(t *testing.T) {
	arg, order, filter := &ColumnRef{Name: "arg"}, &ColumnRef{Name: "order"}, &ColumnRef{Name: "filter"}
	for _, tc := range []struct {
		name string
		call *Func
		want []Expr
	}{
		{"ORDER BY", &Func{Name: "ABS", Args: []Expr{arg}, OrderBy: []OrderItem{{Expr: order}}}, []Expr{arg, order}},
		{"FILTER", &Func{Name: "ABS", Args: []Expr{arg}, Filter: filter}, []Expr{arg, filter}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := resolveCall(tc.call, false); err == nil || !strings.Contains(err.Error(), "is not an aggregate function") {
				t.Fatal("scalar aggregate modifier was not rejected", err)
			}
			// Structural enumeration is shared even for invalid ASTs. Binding
			// rejects these clauses before either aggregate pushdown consumer.
			var got []Expr
			walkOutsideAggregates(tc.call, func(x Expr) { got = append(got, x) })
			if !slices.Equal(got, append([]Expr{tc.call}, tc.want...)) {
				t.Fatalf("scalar modifier child omitted: visits=%v", got)
			}
		})
	}
}

func TestExpressionTraversalPruningPreservesSiblings(t *testing.T) {
	a, b, fallback := &ColumnRef{Name: "pruned"}, &ColumnRef{Name: "b"}, &ColumnRef{Name: "fallback"}
	left := &Unary{Op: "-", X: a}
	right := &Cast{X: b, T: typeInt64, OnError: fallback}
	root := &Binary{Op: "+", L: left, R: right}
	var got []Expr
	walkExprPruned(root, func(x Expr) bool {
		got = append(got, x)
		return x != left
	})
	if !slices.Equal(got, []Expr{root, left, right, b, fallback}) {
		t.Fatal("pruning changed preorder or skipped a sibling", got)
	}
}
