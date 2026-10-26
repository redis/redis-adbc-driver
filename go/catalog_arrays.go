// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

func arrayOf(t ColType) ColType {
	r := recordOf([]ColType{t})
	return ColType{Kind: KindArray, Rec: r.Rec}
}

func (p *selectPlan) hasSetReturning() bool {
	for _, it := range p.items {
		if f, ok := it.expr.(*Func); ok && (f.Name == "UNNEST" || f.Name == "GENERATE_SUBSCRIPTS") {
			return true
		}
	}
	return false
}
func (t ColType) element() ColType {
	if t.Kind == KindArray && t.Rec != nil {
		return t.Rec.fields[0]
	}
	return typeNull
}
func arrayValue(v []Value, t ColType) Value { return Value{T: arrayOf(t), rec: &v} }

func arrayText(v Value, z tzZone) string {
	var parts []string
	for _, x := range v.fields() {
		s := "NULL"
		if !x.Null {
			s = x.textIn(z)
			if s == "" || strings.EqualFold(s, "NULL") || strings.ContainsAny(s, ",{}\"\\ \t\n") {
				s = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
			}
		}
		parts = append(parts, s)
	}
	return "{" + strings.Join(parts, ",") + "}"
}
func compareArrays(a, b Value, z tzZone) (int, bool) {
	af, bf := a.fields(), b.fields()
	for i := 0; i < min(len(af), len(bf)); i++ {
		x, y := af[i], bf[i]
		if x.Null || y.Null {
			if x.Null != y.Null {
				if x.Null {
					return 1, true
				}
				return -1, true
			}
			continue
		}
		n, ok := compareIn(x, y, z)
		if !ok || n != 0 {
			return n, ok
		}
	}
	if len(af) < len(bf) {
		return -1, true
	}
	if len(af) > len(bf) {
		return 1, true
	}
	return 0, true
}
func coerceArray(v Value, t ColType) (Value, error) {
	if v.Null {
		return nullValue(t), nil
	}
	if v.T.Kind != KindArray {
		return Value{}, fmt.Errorf("cannot convert %s to array", v.T.SQLName())
	}
	vals := make([]Value, len(v.fields()))
	for i, x := range v.fields() {
		c, err := Coerce(x, t.element())
		if err != nil {
			return Value{}, err
		}
		vals[i] = c
	}
	return arrayValue(vals, t.element()), nil
}

func (env *evalEnv) evalArrayFunc(f *Func) (Value, bool, error) {
	switch f.Name {
	case "__ARRAY", "__ARRAY_COMPARE", "UNNEST", "GENERATE_SUBSCRIPTS", "ARRAY_LENGTH", "CARDINALITY":
	default:
		return Value{}, false, nil
	}
	if err := checkArity(f); err != nil {
		return Value{}, true, err
	}
	a, err := env.evalItems(f.Args)
	if err != nil {
		return Value{}, true, err
	}
	if f.Name != "__ARRAY" {
		pos := 0
		if f.Name == "__ARRAY_COMPARE" {
			pos = 1
		}
		if a[pos].T.Kind != KindArray && !a[pos].Null {
			return Value{}, true, stateErrorf(stateDatatypeMismatch, "%s requires an array", f.Name)
		}
	}
	switch f.Name {
	case "__ARRAY":
		t := typeNull
		for _, v := range a {
			t = commonType(t, v.T)
		}
		v, err := coerceArray(arrayValue(a, t), arrayOf(t))
		return v, true, err
	case "UNNEST":
		return a[0], true, nil
	case "GENERATE_SUBSCRIPTS":
		var v []Value
		if !a[0].Null && a[1].I == 1 {
			for i := range a[0].fields() {
				v = append(v, intValue(typeInt32, int64(i+1)))
			}
			if len(a) > 2 && a[2].I != 0 {
				slices.Reverse(v)
			}
		}
		return arrayValue(v, typeInt32), true, nil
	case "ARRAY_LENGTH", "CARDINALITY":
		if a[0].Null || (f.Name == "ARRAY_LENGTH" && (len(a[0].fields()) == 0 || a[1].I != 1)) {
			return nullValue(typeInt32), true, nil
		}
		return intValue(typeInt32, int64(len(a[0].fields()))), true, nil
	case "__ARRAY_COMPARE":
		if a[1].Null {
			return nullValue(typeBool), true, nil
		}
		all, hasNull := a[3].I != 0, false
		for _, v := range a[1].fields() {
			l := a[0]
			if !l.Null && l.T.Kind == KindString && v.T.Kind != KindString {
				l, err = Coerce(l, v.T)
			}
			if !v.Null && v.T.Kind == KindString && l.T.Kind != KindString {
				v, err = Coerce(v, l.T)
			}
			if err != nil {
				return Value{}, true, err
			}
			r, err := binaryOp(a[2].S, l, v, env.zone())
			if err != nil {
				return Value{}, true, err
			}
			if r.Null {
				hasNull = true
				continue
			}
			if (r.I != 0) != all {
				return boolValue(!all), true, nil
			}
		}
		if hasNull {
			return nullValue(typeBool), true, nil
		}
		return boolValue(all), true, nil
	}
	return Value{}, false, nil
}

// SELECT-list set-returning functions run in lockstep, padding shorter
// results with NULL, as PostgreSQL does.
func evalExpandedItems(env *evalEnv, items []planItem) ([][]Value, error) {
	row, err := evalItems(env, items)
	if err != nil {
		return nil, err
	}
	n, has := 0, false
	for i, it := range items {
		if f, ok := it.expr.(*Func); ok && (f.Name == "UNNEST" || f.Name == "GENERATE_SUBSCRIPTS") {
			has = true
			n = max(n, len(row[i].fields()))
		}
	}
	if !has {
		return [][]Value{row}, nil
	}
	out := make([][]Value, n)
	for j := range out {
		out[j] = slices.Clone(row)
		for i, it := range items {
			if f, ok := it.expr.(*Func); ok && (f.Name == "UNNEST" || f.Name == "GENERATE_SUBSCRIPTS") {
				out[j][i] = nullValue(it.typ)
				if j < len(row[i].fields()) {
					out[j][i] = row[i].fields()[j]
				}
			}
		}
	}
	return out, nil
}

func expandedOrderKeys(env *evalEnv, p *selectPlan, rows [][]Value) ([][]Value, error) {
	keys := make([][]Value, len(rows))
	for i, row := range rows {
		for _, o := range p.order {
			found := false
			for j, it := range p.items {
				if exprEqual(it.expr, o.expr) {
					keys[i] = append(keys[i], row[j])
					found = true
					break
				}
			}
			if !found {
				v, err := env.eval(o.expr)
				if err != nil {
					return nil, err
				}
				keys[i] = append(keys[i], v)
			}
		}
	}
	return keys, nil
}
func sortExpandedRows(rows [][]Value, keys [][]Value, order []planOrder) [][]Value {
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return lessKeys(keys[idx[i]], keys[idx[j]], order) })
	out := make([][]Value, len(rows))
	for i, j := range idx {
		out[i] = rows[j]
	}
	return out
}

type arrayAgg struct{ jsonAgg }

func (a *arrayAgg) result(env *evalEnv, t ColType) (Value, error) {
	if len(a.rows) == 0 {
		return nullValue(t), nil
	}
	if len(a.fn.OrderBy) > 0 {
		order := make([]planOrder, len(a.fn.OrderBy))
		for i, o := range a.fn.OrderBy {
			order[i] = planOrder{desc: o.Desc, nullsFirst: o.Nulls == NullsFirst, nullsLast: o.Nulls == NullsLast}
		}
		sort.SliceStable(a.rows, func(i, j int) bool { return lessKeys(a.rows[i].keys, a.rows[j].keys, order) })
	}
	v := make([]Value, len(a.rows))
	for i, r := range a.rows {
		v[i] = r.args[0]
	}
	return coerceArray(arrayValue(v, t.element()), t)
}
