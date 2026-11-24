// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import "context"

// deferredUnionSafe deliberately admits less than the general view inliner.
// Filtering before a source has run must not suppress expression errors or
// volatile effects. Pairwise UNION conversions must be identity conversions;
// broader numeric/coercion rewrites need a separate proof.
func (e *executor) deferredUnionSafe(ctx context.Context, p *selectPlan) bool {
	if p == nil || p.sel == nil || p.aggregate || p.windowed() || p.distinct || p.grouping != nil || p.qualify != nil {
		return false
	}
	s := p.sel
	leftLeaf := e.deferredLeftJoinSafe(ctx, p)
	if len(s.With) != 0 || len(s.Joins) != 0 && !leftLeaf || len(s.OrderBy) != 0 || len(s.GroupBy) != 0 || s.GroupingSets != nil || len(s.Windows) != 0 || s.Distinct || s.Having != nil || s.Where != nil || s.Limit != nil || s.Offset != nil || s.LimitExpr != nil || s.OffsetExpr != nil {
		return false
	}
	if sp := p.setop; sp != nil {
		if sp.op != "UNION" || !sp.all || len(sp.order) != 0 || !e.deferredUnionSafe(ctx, sp.left) || !e.deferredUnionSafe(ctx, sp.right) {
			return false
		}
		for i, it := range p.items {
			if it.typ != sp.left.items[i].typ || it.typ != sp.right.items[i].typ {
				return false
			}
		}
		return true
	}
	if p.meta == nil || p.meta.lazy != nil || p.meta.join != nil && !leftLeaf || p.meta.group != nil {
		return false
	}
	if p.meta.view != nil && (!p.meta.view.unionDemand || !e.deferredUnionSafe(ctx, p.meta.view.plan)) {
		return false
	}
	for _, it := range p.items {
		switch x := it.expr.(type) {
		case *ColumnRef:
			if x.Outer != 0 {
				return false
			}
			cm, ok := p.meta.column(x.Name)
			if !ok || cm.Type != it.typ {
				return false
			}
		default:
			if !unionConstant(it.expr) {
				return false
			}
			v, err := e.newEnv(ctx, nil, nil).eval(it.expr)
			if err != nil {
				return false
			}
			if _, err = coerceIn(v, it.typ, e.zone()); err != nil {
				return false
			}
		}
	}
	return true
}

func unionConstant(x Expr) bool {
	switch x := x.(type) {
	case *Literal:
		return true
	case *Cast:
		return x.OnError == nil && unionConstant(x.X)
	}
	return false
}

func containsDeferredUnion(p *selectPlan) bool {
	return p.setop != nil || (p.meta != nil && p.meta.view != nil && p.meta.view.unionDemand)
}

// runDeferredUnion keeps the original pairwise conversion tree and branch
// sequence. Names are consumer names; branches are matched by position.
func (e *executor) runDeferredUnion(ctx context.Context, lv *lazyView, predicate Expr, need map[string]bool, params []Value) ([]map[string]Value, error) {
	var out []map[string]Value
	for _, p := range []*selectPlan{lv.plan.setop.left, lv.plan.setop.right} {
		rows, err := e.runView(ctx, &lazyView{plan: p, names: lv.names, unionDemand: true}, predicate, need, params)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := cancelled(ctx); err != nil {
				return nil, err
			}
			for i, name := range lv.names {
				v, ok := row[name]
				if !ok {
					continue
				}
				row[name], err = coerceIn(v, lv.plan.items[i].typ, e.zone())
				if err != nil {
					return nil, invalidArg(err)
				}
			}
		}
		out = append(out, rows...)
	}
	return out, nil
}

// deferredJoinDemand is a necessary matching-key condition, never a replacement
// for the original join. Restricting the input on the right is also valid for a
// LEFT join: unmatched left rows are still null-extended by joinRows.
func (e *executor) deferredJoinDemand(ctx context.Context, env *evalEnv, pairs []equiPair, left []map[string]Value, view *tableMeta) (Expr, error) {
	var combined Expr
	for _, pair := range pairs {
		l, lok := pair.left.(*ColumnRef)
		r, rok := pair.right.(*ColumnRef)
		if !lok || !rok || l.Outer != 0 || r.Outer != 0 {
			continue
		}
		cm, ok := view.column(r.Name)
		if !ok || !(cm.Type.Kind.isInteger() || cm.Type.Kind == KindDate) {
			continue
		}
		seen := map[string]bool{}
		var demand Expr
		safe := true
		for _, row := range left {
			if err := cancelled(ctx); err != nil {
				return nil, err
			}
			env.row = row
			v, err := env.eval(l)
			if err != nil {
				return nil, invalidArg(err)
			}
			if v.Null {
				continue
			}
			if v.T != cm.Type {
				safe = false
				break
			}
			key, ok := joinKey(v)
			if !ok {
				safe = false
				break
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(seen) > maxUnionTerms {
				safe = false
				break
			}
			term := &Binary{Op: "=", L: r, R: &Literal{V: v}}
			if demand == nil {
				demand = term
			} else {
				demand = &Binary{Op: "OR", L: demand, R: term}
			}
		}
		if !safe {
			continue
		}
		if demand == nil {
			// Every left key of this proven pair is NULL (or left is empty).
			// No right row can match the original equality; LEFT joins still
			// null-extend their unmodified left input through the real join.
			return &Literal{V: boolValue(false)}, nil
		}
		// Independent key sets can admit cross-combinations absent from the
		// left input. Their conjunction is still a necessary condition; the
		// original composite join and residual retain correlation/multiplicity.
		combined = andAll([]Expr{combined, demand})
	}
	return combined, nil
}

// Accept exact typed column/literal comparisons (including bounded OR key
// sets). Other outer conjuncts remain solely in the consumer's residual.
func deferredUnionPredicate(lv *lazyView, predicate Expr) Expr {
	types := make(map[string]ColType, len(lv.names))
	for i, name := range lv.names {
		types[name] = lv.plan.items[i].typ
	}
	var safe func(Expr) bool
	safe = func(x Expr) bool {
		switch x := x.(type) {
		case *Literal:
			return x.V.T.Kind == KindBool
		case *Binary:
			if x.Op == "AND" || x.Op == "OR" {
				return safe(x.L) && safe(x.R)
			}
			if x.Op != "=" && x.Op != "<" && x.Op != ">" && x.Op != "<=" && x.Op != ">=" {
				return false
			}
			c, cok := x.L.(*ColumnRef)
			v, vok := x.R.(*Literal)
			if !cok || !vok || c.Outer != 0 || v.Unknown {
				return false
			}
			t, ok := types[c.Name]
			// Numeric/date keys have total comparison and no collation or
			// user-defined comparison behavior.
			return ok && t == v.V.T && (t.Kind.isInteger() || t.Kind == KindDate)
		}
		return false
	}
	var out Expr
	for _, c := range conjuncts(predicate, nil) {
		if safe(c) {
			if out == nil {
				out = c
			} else {
				out = &Binary{Op: "AND", L: out, R: c}
			}
		}
	}
	return out
}
