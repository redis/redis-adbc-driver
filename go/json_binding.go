// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

// PostgreSQL json preserves text but has no equality/ordering operators. Reject
// these uses at binding rather than letting an unsupported hash key merge rows.
func containsPlainJSON(t ColType) bool {
	if t.Kind == KindJSON {
		return true
	}
	if t.Kind == KindArray {
		return containsPlainJSON(t.element())
	}
	if t.Rec != nil {
		for _, f := range t.Rec.fieldTypes() {
			if containsPlainJSON(f) {
				return true
			}
		}
	}
	return false
}
func jsonEqualityError() error {
	return stateErrorf(stateUndefinedFunction, "could not identify an equality operator for type json")
}
func (e *executor) checkJSONComparisons(expr Expr) error {
	var err error
	types := e.scopeTypes()
	walkExpr(expr, func(x Expr) {
		if err != nil {
			return
		}
		var operands []Expr
		switch x := x.(type) {
		case *Binary:
			if isComparison(x.Op) || isDistinctOp(x.Op) {
				operands = []Expr{x.L, x.R}
			}
		case *Func:
			if x.Distinct || x.Name == "MIN" || x.Name == "MAX" || x.Name == "GREATEST" || x.Name == "LEAST" || x.Name == "NULLIF" {
				operands = append(operands, x.Args...)
			}
			for _, o := range x.OrderBy {
				operands = append(operands, o.Expr)
			}
		case *Subquery:
			if x.Kind == SubqueryIn || x.Kind == SubqueryAny || x.Kind == SubqueryAll {
				operands = []Expr{x.X}
				if x.plan != nil {
					for _, it := range x.plan.items {
						if containsPlainJSON(it.typ) {
							err = jsonEqualityError()
							return
						}
					}
				}
			}
		}
		for _, operand := range operands {
			t, eerr := inferType(operand, types, e.paramTypes)
			if eerr == nil && containsPlainJSON(t) {
				err = jsonEqualityError()
				return
			}
		}
	})
	return err
}
func (e *executor) checkJSONPlanComparisons(plan *selectPlan, types map[string]ColType) error {
	var keys []Expr
	keys = append(keys, plan.sel.GroupBy...)
	for _, o := range plan.order {
		keys = append(keys, o.expr)
	}
	for _, o := range plan.distinctOn {
		keys = append(keys, o.expr)
	}
	if plan.sel.Distinct && len(plan.distinctOn) == 0 {
		for _, it := range plan.items {
			if containsPlainJSON(it.typ) {
				return jsonEqualityError()
			}
		}
	}
	for _, w := range plan.windows {
		if w.fn.Over == nil {
			continue
		}
		keys = append(keys, w.fn.Over.PartitionBy...)
		for _, o := range w.fn.Over.OrderBy {
			keys = append(keys, o.Expr)
		}
	}
	for _, key := range keys {
		t, err := inferType(key, types, e.paramTypes)
		if err == nil && containsPlainJSON(t) {
			return jsonEqualityError()
		}
	}
	return nil
}
