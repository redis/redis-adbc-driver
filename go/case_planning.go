// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// Inspect immutable constant expressions while planning CASE. The AST stays
// unchanged: row-dependent expressions and volatile functions still run per row.
// Constant facts come from literal query inputs, never from observed table rows.
type casePlanner struct {
	env   *evalEnv
	known map[string]Value
}

func (e *executor) checkCaseConstants(ctx context.Context, expr Expr) error {
	hasCase := false
	walkExpr(expr, func(x Expr) {
		if _, ok := x.(*Case); ok {
			hasCase = true
		}
	})
	if !hasCase {
		return nil
	}
	known := map[string]Value{}
	if len(e.scopes) > 0 {
		for _, r := range e.scopes[len(e.scopes)-1].rels {
			if r.meta == nil {
				continue
			}
			for name, v := range r.meta.planConstants {
				known[r.prefix+name] = v
			}
		}
	}
	env := e.newEnv(ctx, e.scopeTypes(), nil)
	env.row = known
	_, _, err := (&casePlanner{env: env, known: known}).fold(expr)
	return invalidArg(err)
}

func (p *casePlanner) fold(expr Expr) (Value, bool, error) {
	if expr == nil {
		return Value{}, false, nil
	}
	if err := cancelled(p.env.ctx); err != nil {
		return Value{}, false, err
	}
	eval := func() (Value, bool, error) { v, err := p.env.eval(expr); return v, err == nil, err }
	switch x := expr.(type) {
	case *Literal:
		return x.V, true, nil
	case *ColumnRef:
		v, ok := p.known[x.Name]
		return v, ok && x.Outer == 0, nil
	case *Case:
		operand, operandKnown, err := p.fold(x.Operand)
		if err != nil {
			return Value{}, false, err
		}
		uncertain := false
		for _, w := range x.Whens {
			cond, known, err := p.fold(w.When)
			if err != nil {
				return Value{}, false, err
			}
			if x.Operand != nil {
				known = known && operandKnown
				if known {
					cond, err = binaryOp("=", operand, cond, p.env.zone())
					if err != nil {
						return Value{}, false, err
					}
				}
			}
			if known {
				yes, _ := truthy(cond)
				if !yes {
					continue
				}
				if _, _, err = p.fold(w.Then); err != nil {
					return Value{}, false, err
				}
				if !uncertain {
					return eval()
				}
				return Value{}, false, nil
			}
			uncertain = true
			if _, _, err = p.fold(w.Then); err != nil {
				return Value{}, false, err
			}
		}
		if _, _, err = p.fold(x.Else); err != nil {
			return Value{}, false, err
		}
		if !uncertain && (x.Operand == nil || operandKnown) {
			return eval()
		}
		return Value{}, false, nil
	case *Func:
		if x.Name == "COALESCE" {
			uncertain := false
			for _, a := range x.Args {
				v, known, err := p.fold(a)
				if err != nil {
					return Value{}, false, err
				}
				if !known {
					uncertain = true
					continue
				}
				if !v.Null {
					if !uncertain {
						return eval()
					}
					return Value{}, false, nil
				}
			}
			if !uncertain {
				return eval()
			}
			return Value{}, false, nil
		}
	case *Binary:
		if x.Op == "AND" || x.Op == "OR" {
			v, known, err := p.fold(x.L)
			if err != nil {
				return Value{}, false, err
			}
			if known {
				b, nonnull := truthy(v)
				if nonnull && (x.Op == "AND" && !b || x.Op == "OR" && b) {
					return boolValue(b), true, nil
				}
			}
			v, rightKnown, err := p.fold(x.R)
			if err != nil {
				return Value{}, false, err
			}
			if rightKnown {
				b, nonnull := truthy(v)
				if nonnull && (x.Op == "AND" && !b || x.Op == "OR" && b) {
					return boolValue(b), true, nil
				}
			}
			if known && rightKnown {
				return eval()
			}
			return Value{}, false, nil
		}
	case *Cast:
		if x.OnError != nil { // Runtime fallback is evaluated only after conversion fails.
			_, _, err := p.fold(x.X)
			return Value{}, false, err
		}
	}
	all := true
	var err error
	walkExprResolved(expr, func(child Expr) bool {
		if child == expr {
			return true
		}
		if err != nil {
			return false
		}
		_, known, e := p.fold(child)
		all = all && known
		err = e
		return false
	}, false)
	if err != nil {
		return Value{}, false, err
	}
	pure := false
	switch x := expr.(type) {
	case *Unary, *Binary, *IsNull, *Cast, *RowExpr:
		pure = true
	case *Func:
		switch x.Name {
		case "ABS", "MOD", "NULLIF", "GREATEST", "LEAST", "__ARRAY":
			pure = true
		}
	}
	if all && pure {
		return eval()
	}
	return Value{}, false, nil
}

// Propagate only provable immutable query constants through a materialized
// relation. Actual row contents, parameters and outer references are not facts.
func (e *executor) projectedConstants(ctx context.Context, plan *selectPlan, meta *tableMeta) {
	if plan.setop != nil || plan.aggregate || plan.grouping != nil || len(plan.windows) > 0 || len(plan.sel.Joins) > 0 || plan.sel.FromFunc != nil || plan.sel.FromGroup != nil {
		return
	}
	if plan.sel.FromSelect != nil && (plan.meta == nil || len(plan.meta.planConstants) == 0) {
		return
	}
	volatile := false
	inspect := func(expr Expr) {
		walkExpr(expr, func(x Expr) {
			if f, ok := x.(*Func); ok && volatileFuncs[f.Name] {
				volatile = true
			}
		})
	}
	for _, it := range plan.items {
		inspect(it.expr)
	}
	inspect(plan.sel.Where)
	for _, o := range plan.order {
		inspect(o.expr)
	}
	if volatile {
		return
	}
	known := map[string]Value{}
	types := map[string]ColType{}
	if plan.meta != nil {
		known = plan.meta.planConstants
		types = plan.meta.types()
	}
	env := e.newEnv(ctx, types, nil)
	env.row = known
	p := casePlanner{env: env, known: known}
	for i, it := range plan.items {
		v, ok, err := p.fold(it.expr)
		if err == nil && ok && i < len(meta.Columns) {
			if meta.planConstants == nil {
				meta.planConstants = map[string]Value{}
			}
			meta.planConstants[meta.Columns[i].Name] = v
		}
	}
}

// PostgreSQL normally inlines a non-recursive CTE referenced once. A shared
// definition remains a planning boundary even when its rows look constant.
func (e *executor) noteInlineFacts(sel *SelectStmt) {
	if e.cache.inlineFacts == nil {
		e.cache.inlineFacts = map[*CTE]bool{}
	}
	for i := range sel.With {
		def := &sel.With[i]
		probe := recursionCheck{cte: def, probe: true}
		body := *sel
		body.With = nil
		probe.query(&body, nil)
		for j := range sel.With {
			if j != i {
				probe.query(sel.With[j].Select, nil)
			}
		}
		e.cache.inlineFacts[def] = probe.refs == 1 && def.Statement == nil && !def.Recursive
	}
}
func (e *executor) cteAllowsFacts(key any) bool {
	if def, ok := key.(*CTE); ok {
		return e.cache.inlineFacts[def]
	}
	return true
}
