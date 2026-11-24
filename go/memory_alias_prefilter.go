// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

type memoryAliasSlot struct {
	name string
	slot int
}
type memoryAliasFilter struct {
	predicate *scanPredicate
	slots     []memoryAliasSlot
}

// Prove the ENTIRE residual total before moving any conjunct. Inspecting a
// string/decimal comparison's types is not evaluating that comparison early.
// Unsupported coercions, functions/arithmetic and UUID-like aliases stay eager.
func memoryAliasTotal(expr Expr, types map[string]ColType, params []Value) (map[string]bool, bool) {
	refs := map[string]bool{}
	nodes := 0
	scalar := func(t ColType) bool {
		return t.Alias == "" && t.Collation == "" && (t.Kind.isInteger() || t.Kind == KindBool || t.Kind == KindDecimal || t.Kind == KindString || t.Kind == KindNull)
	}
	validLiteral := func(v Value) bool {
		return scalar(v.T) && (v.T.Kind != KindNull || v.Null) && (v.Null || v.T.Kind != KindDecimal || v.D != nil)
	}
	var valueType func(Expr) (ColType, bool)
	valueType = func(x Expr) (ColType, bool) {
		nodes++
		if nodes > 64 {
			return ColType{}, false
		}
		switch x := x.(type) {
		case *ColumnRef:
			t, ok := types[x.Name]
			if !ok || !scalar(t) || x.Outer != 0 || x.item != "" || x.outerAgg != nil || x.whole != nil {
				return ColType{}, false
			}
			refs[x.Name] = true
			return t, true
		case *Literal:
			return x.V.T, validLiteral(x.V)
		case *Param:
			if x.Index >= 0 && x.Index < len(params) {
				return params[x.Index].T, validLiteral(params[x.Index])
			}
		}
		return ColType{}, false
	}
	var boolean func(Expr) bool
	boolean = func(x Expr) bool {
		nodes++
		if nodes > 64 {
			return false
		}
		switch x := x.(type) {
		case *Literal:
			return validLiteral(x.V) && (x.V.T.Kind == KindBool || x.V.T.Kind == KindNull)
		case *ColumnRef:
			t, ok := valueType(x)
			return ok && t.Kind == KindBool
		case *Unary:
			return x.Op == "NOT" && boolean(x.X)
		case *IsNull:
			_, ok := valueType(x.X)
			return ok
		case *Binary:
			if x.nested {
				return false
			}
			if x.Op == "AND" || x.Op == "OR" {
				return boolean(x.L) && boolean(x.R)
			}
			if !isComparison(x.Op) && !isDistinctOp(x.Op) {
				return false
			}
			l, lok := valueType(x.L)
			r, rok := valueType(x.R)
			if !lok || !rok {
				return false
			}
			if l.Kind == KindNull || r.Kind == KindNull {
				return true
			}
			numeric := func(t ColType) bool { return t.Kind.isInteger() || t.Kind == KindDecimal }
			return numeric(l) && numeric(r) || l.Kind == KindBool && r.Kind == KindBool || l.Kind == KindString && r.Kind == KindString
		}
		return false
	}
	return refs, boolean(expr)
}

func (e *executor) bindMemoryAliasFilter(ctx context.Context, it *joinItem, req scanRequest, params []Value) (*memoryAliasFilter, error) {
	m := it.base
	if req.where.residual == nil || !m.isMem || m.view != nil || m.lazy != nil || m.join != nil || m.group != nil || m.sourceKeys != nil || req.meta.sourceKeys != nil || e.retained != nil || e.partitionDirectTable != nil || e.partitionForeignTable != "" || (ctx != nil && (ctx.Value(retainedAllocationKey{}) != nil || ctx.Value(subBudgetKey{}) != nil)) {
		return nil, nil
	}
	types := map[string]ColType{}
	repl := map[string]Expr{}
	for _, cm := range m.Columns {
		if _, exists := types[cm.Name]; exists {
			return nil, nil
		}
		types[cm.Name] = cm.Type
		repl[it.prefix+cm.Name] = &ColumnRef{Name: cm.Name}
	}
	direct := true
	walkExprPruned(req.where.residual, func(x Expr) bool {
		if r, ok := x.(*ColumnRef); ok && (r.Outer != 0 || r.item != "" || r.outerAgg != nil || r.whole != nil) {
			direct = false
		}
		if _, ok := x.(*Subquery); ok {
			direct = false
			return false
		}
		return true
	})
	if !direct {
		return nil, nil
	}
	rebound, ok := substitute(req.where.residual, repl)
	if !ok {
		return nil, nil
	}
	rebound, ok = e.foldMemoryAliasIntegers(ctx, rebound)
	if !ok {
		return nil, nil
	}
	refs, total := memoryAliasTotal(rebound, types, params)
	if !total {
		return nil, nil
	}
	var necessary Expr
	for _, part := range conjuncts(rebound, nil) {
		if bindScanPredicate(part, types, params, e.zone()) != nil {
			necessary = andAll([]Expr{necessary, part})
		}
	}
	predicate := bindScanPredicate(necessary, types, params, e.zone())
	if predicate == nil || len(predicate.fields) == 0 {
		return nil, nil
	}
	// Validate every referenced typed input before consuming/copying any row.
	// Missing/defaulted memory fields retain the established full-scan semantics.
	for _, row := range m.mem {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		for _, cm := range m.Columns {
			if !refs[cm.Name] {
				continue
			}
			v, exists := row[cm.Name]
			if !exists || !memoryAliasValueType(v, cm.Type) {
				return nil, nil
			}
		}
	}
	filter := &memoryAliasFilter{predicate: predicate}
	positions := map[string]int{}
	for i, name := range predicate.fields {
		positions[name] = i
	}
	for _, cm := range m.Columns {
		if at, ok := positions[cm.Name]; ok {
			filter.slots = append(filter.slots, memoryAliasSlot{name: cm.Name, slot: at})
		}
	}
	return filter, nil
}

// Return the actual qualified maps before the normal complete residual runs.
// A successful necessary filter saves map copies, never changes shared rows.
func (e *executor) qualifyMemoryJoinRows(ctx context.Context, it *joinItem, req scanRequest, params []Value) ([]map[string]Value, error) {
	filter, err := e.bindMemoryAliasFilter(ctx, it, req, params)
	if err != nil {
		return nil, err
	}
	var out []map[string]Value
	if filter == nil {
		out = make([]map[string]Value, len(it.base.mem))
	} else {
		out = make([]map[string]Value, 0, min(len(it.base.mem), wideFetchChunk))
	}
	for i, row := range it.base.mem {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		if filter != nil {
			for _, field := range filter.slots {
				filter.predicate.values[field.slot] = row[field.name]
			}
			match, err := filter.predicate.matches()
			if err != nil {
				return nil, invalidArg(err)
			}
			if !match {
				continue
			}
		}
		qualified := make(map[string]Value, len(row))
		for name, value := range row {
			qualified[it.prefix+name] = value
		}
		if filter == nil {
			out[i] = qualified
		} else {
			out = append(out, qualified)
		}
	}
	return out, nil
}
