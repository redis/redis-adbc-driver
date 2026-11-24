// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "strings"

// pushdownConjuncts exposes conditions implied by every branch of a safe OR,
// without rewriting the predicate. In particular, TPC-H Q19 repeats its join
// equality and ship filters in three branches. Keeping the OR as a residual
// preserves its complete SQL semantics; planPushdown still checks null-supplying
// and isolated join boundaries for each additional condition.
func (jp *joinPlan) pushdownConjuncts(where Expr) []Expr {
	out := conjuncts(where, nil)
	for _, c := range out {
		b, ok := c.(*Binary)
		if !ok || b.Op != "OR" || !jp.safeCommonPredicate(c) {
			continue
		}
		for _, common := range impliedJoinConjuncts(c) {
			found := false
			for _, existing := range out {
				found = found || exprEqual(existing, common)
			}
			if !found {
				out = append(out, common)
			}
		}
	}
	return out
}

// impliedJoinConjuncts intersects OR branches and unions AND branches. It does
// not expand to DNF (which could grow exponentially), or mutate shared AST nodes.
func impliedJoinConjuncts(expr Expr) []Expr {
	if b, ok := expr.(*Binary); ok && (b.Op == "AND" || b.Op == "OR") {
		left, right := impliedJoinConjuncts(b.L), impliedJoinConjuncts(b.R)
		if b.Op == "AND" {
			return append(left, right...)
		}
		// The complete OR can itself be a repeated conjunct (e.g. IN's
		// equality expansion), even when its branches share no atomic term.
		common := []Expr{expr}
		for _, l := range left {
			for _, r := range right {
				if exprEqual(l, r) {
					common = append(common, l)
					break
				}
			}
		}
		return common
	}
	return []Expr{expr}
}

// Restrict the entire OR, not just extracted atoms: filtering rows early must
// not suppress errors or volatile/subquery evaluation in a different branch.
// Numeric/text comparisons over direct columns and checked literal arithmetic
// cover Q19, including the parser's AND/OR expansion of BETWEEN and IN lists.
func (jp *joinPlan) safeCommonPredicate(expr Expr) bool {
	b, ok := expr.(*Binary)
	if !ok {
		return false
	}
	switch b.Op {
	case "AND", "OR":
		return jp.safeCommonPredicate(b.L) && jp.safeCommonPredicate(b.R)
	case "=", "<>", "<", "<=", ">", ">=":
		if b.nested {
			return false
		}
		l, lok := jp.commonOperandType(b.L)
		r, rok := jp.commonOperandType(b.R)
		return lok && rok && l.Collation == "" && r.Collation == "" &&
			((l.Kind.isNumeric() && r.Kind.isNumeric()) || (l.Kind == KindString && r.Kind == KindString))
	}
	return false
}

func (jp *joinPlan) commonOperandType(expr Expr) (ColType, bool) {
	if c, ok := expr.(*ColumnRef); ok {
		if c.Outer != 0 || c.whole != nil {
			return ColType{}, false
		}
		for _, it := range jp.items {
			if name, ok := strings.CutPrefix(c.Name, it.prefix); ok {
				col, found := it.base.column(name)
				return col.Type, found
			}
		}
		return ColType{}, false
	}
	var literalArithmetic func(Expr) bool
	literalArithmetic = func(e Expr) bool {
		switch x := e.(type) {
		case *Literal:
			return true
		case *Binary:
			return (x.Op == "+" || x.Op == "-") && literalArithmetic(x.L) && literalArithmetic(x.R)
		}
		return false
	}
	if literalArithmetic(expr) {
		v, err := (&evalEnv{}).eval(expr)
		return v.T, err == nil
	}
	return ColType{}, false
}
