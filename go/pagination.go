// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "context"

// Resolve counts into a per-plan copy, never into the cached parsed statement.
func (e *executor) resolvePagination(ctx context.Context, sel *SelectStmt) error {
	for _, c := range []struct {
		name  string
		expr  Expr
		dst   **int64
		state string
	}{
		{"LIMIT", sel.LimitExpr, &sel.Limit, "2201W"},
		{"OFFSET", sel.OffsetExpr, &sel.Offset, "2201X"},
	} {
		if c.expr == nil {
			continue
		}
		var bad error
		walkExpr(c.expr, func(x Expr) {
			switch x.(type) {
			case *ColumnRef:
				bad = stateErrorf("42P10", "argument of %s must not contain variables", c.name)
			case *WindowFunc:
				bad = stateErrorf("42P20", "window functions are not allowed in %s", c.name)
			}
		})
		if bad != nil {
			return bad
		}
		if isAggregate(c.expr) {
			return stateErrorf("42803", "aggregate functions are not allowed in %s", c.name)
		}
		if err := e.bind(ctx, c.expr); err != nil {
			return err
		}
		e.cache.noteParamOf(c.expr, typeInt64)
		t, err := inferType(c.expr, e.scopeTypes(), e.paramTypes)
		if err != nil {
			return invalidArg(err)
		}
		if t.Kind != KindNull && !t.Kind.isNumeric() && !unknownLiteral(c.expr) {
			return stateErrorf(stateDatatypeMismatch, "argument of %s must be type bigint, not type %s", c.name, sigTypeName(t))
		}
		if e.planOnly {
			continue
		}
		v, err := e.newEnv(ctx, nil, e.params).eval(c.expr)
		if err != nil {
			return invalidArg(err)
		}
		*c.dst = nil
		if v.Null {
			continue
		}
		v, err = Coerce(v, typeInt64)
		if err != nil {
			return invalidArg(err)
		}
		if v.I < 0 {
			return stateErrorf(c.state, "%s must not be negative", c.name)
		}
		n := v.I
		*c.dst = &n
	}
	return nil
}
