// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// Private, bounded constant folding only. A failure refuses the optimization;
// it must never expose an error from a normally short-circuited residual arm.
func (e *executor) foldMemoryAliasIntegers(ctx context.Context, expr Expr) (Expr, bool) {
	nodes, operations := 0, 0
	env := e.newEnv(ctx, nil, nil)
	literal := func(x Expr) bool {
		v, ok := x.(*Literal)
		return ok && !v.Unknown && !v.V.Null && v.V.T.Kind.isInteger() && v.V.T.Alias == "" && v.V.T.Collation == ""
	}
	evaluate := func(x Expr) (Expr, bool) {
		operations++
		if operations > 16 {
			return nil, false
		}
		v, err := env.eval(x)
		if err != nil || v.Null || !v.T.Kind.isInteger() {
			return nil, false
		}
		return &Literal{V: v}, true
	}
	var fold func(Expr) (Expr, bool)
	fold = func(x Expr) (Expr, bool) {
		nodes++
		if nodes > 128 {
			return nil, false
		}
		switch x := x.(type) {
		case *Binary:
			left, ok := fold(x.L)
			if !ok {
				return nil, false
			}
			right, ok := fold(x.R)
			if !ok {
				return nil, false
			}
			copy := *x
			copy.L, copy.R = left, right
			if !x.nested && literal(left) && literal(right) {
				switch x.Op {
				case "+", "-", "*", "/", "%":
					return evaluate(&copy)
				}
			}
			return &copy, true
		case *Unary:
			child, ok := fold(x.X)
			if !ok {
				return nil, false
			}
			copy := *x
			copy.X = child
			if (x.Op == "+" || x.Op == "-") && literal(child) {
				return evaluate(&copy)
			}
			return &copy, true
		case *IsNull:
			child, ok := fold(x.X)
			if !ok {
				return nil, false
			}
			copy := *x
			copy.X = child
			return &copy, true
		default:
			// In particular functions/casts/params/columns are never evaluated here.
			// Remaining unsupported expressions fail the later whole-residual proof.
			return x, true
		}
	}
	return fold(expr)
}

func memoryAliasValueType(v Value, want ColType) bool {
	if !v.Null && v.T.Kind == KindDecimal && v.D == nil {
		return false
	}
	got := v.T
	if want.Kind == KindDecimal && want.Precision == 0 && got.Kind == KindDecimal {
		// Unbounded NUMERIC is a family of runtime scales/precisions. SUM and UNION
		// preserve each actual exact coefficient/scale; do not round or coerce it.
		got.Precision, got.Scale = want.Precision, want.Scale
	}
	return got == want && (got.Kind != KindNull || v.Null)
}
