// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"slices"
	"strings"
)

func isSRF(name string) bool {
	switch name {
	case "UNNEST", "GENERATE_SUBSCRIPTS", "GENERATE_SERIES", "STRING_TO_TABLE", "REGEXP_SPLIT_TO_TABLE", "REGEXP_MATCHES":
		return true
	}
	return false
}
func tableFunctionName(f *Func) string {
	if f.Name == "__ORDINALITY" || f.Name == "__ROWS_FROM" {
		return tableFunctionName(f.Args[0].(*Func))
	}
	return strings.ToLower(f.Name)
}
func tableFunctionTypes(f *Func, types map[string]ColType, params []ColType) ([]ColType, error) {
	if f.Name == "__ORDINALITY" {
		ts, err := tableFunctionTypes(f.Args[0].(*Func), types, params)
		return append(ts, typeInt64), err
	}
	if f.Name == "__ROWS_FROM" {
		var ts []ColType
		for _, a := range f.Args {
			t, err := tableFunctionTypes(a.(*Func), types, params)
			if err != nil {
				return nil, err
			}
			ts = append(ts, t...)
		}
		return ts, nil
	}
	if !isSRF(f.Name) || f.quotedCase {
		return nil, noSuchFunction(f.Name, f.Star, inferArgTypes(f.Args, types, params))
	}
	if f.Star || f.Distinct {
		return nil, stateErrorf(stateInvalidParameter, "%s does not accept * or DISTINCT", f.Name)
	}
	if f.Name == "UNNEST" {
		if len(f.Args) == 0 {
			return nil, noSuchFunction(f.Name, false, nil)
		}
		var ts []ColType
		for _, a := range f.Args {
			t, err := inferType(a, types, params)
			if err != nil {
				return nil, err
			}
			if t.Kind != KindArray {
				return nil, stateErrorf(stateDatatypeMismatch, "unnest requires an array")
			}
			ts = append(ts, t.element())
		}
		return ts, nil
	}
	if d, ok := lookupFunc(f.Name); ok {
		if err := d.argsError(f); err != nil {
			return nil, err
		}
	}
	args := inferArgTypes(f.Args, types, params)
	if f.Name == "GENERATE_SERIES" {
		t, err := seriesType(args)
		return []ColType{t}, err
	}
	if err := checkArity(f); err != nil {
		return nil, err
	}
	return []ColType{funcType(f, args)}, nil
}
func (e *executor) planGeneralTableFunc(ctx context.Context, f *Func, alias string, cols []string, sc *scope) (*tableMeta, ColType, error) {
	var bindFunc func(*Func) error
	bindFunc = func(fn *Func) error {
		if fn.Name == "__ORDINALITY" || fn.Name == "__ROWS_FROM" {
			for _, a := range fn.Args {
				if err := bindFunc(a.(*Func)); err != nil {
					return err
				}
			}
			return nil
		}
		for _, a := range fn.Args {
			if isAggregate(a) {
				return stateErrorf(stateFeatureNotSupported, "aggregate functions are not allowed in functions in FROM")
			}
			if containsWindow(a) || containsSRF(a) {
				return stateErrorf(stateFeatureNotSupported, "set-returning function arguments may not contain aggregate, window or set-returning functions")
			}
			if err := e.bind(ctx, a); err != nil {
				return err
			}
			columnRefs(a, sc.needs)
		}
		return nil
	}
	if err := bindFunc(f); err != nil {
		return nil, ColType{}, err
	}
	ts, err := tableFunctionTypes(f, e.scopeTypes(), e.paramTypes)
	if err != nil {
		return nil, ColType{}, err
	}
	if len(cols) > len(ts) {
		return nil, ColType{}, stateErrorf(stateInvalidParameter, "table %q has %d columns available but %d columns specified", alias, len(ts), len(cols))
	}
	var names []string
	var namesFor func(*Func)
	namesFor = func(fn *Func) {
		switch fn.Name {
		case "__ORDINALITY":
			namesFor(fn.Args[0].(*Func))
			names = append(names, "ordinality")
		case "__ROWS_FROM":
			for _, a := range fn.Args {
				namesFor(a.(*Func))
			}
		default:
			n := 1
			if fn.Name == "UNNEST" {
				n = len(fn.Args)
			}
			for range n {
				names = append(names, strings.ToLower(fn.Name))
			}
		}
	}
	namesFor(f)
	if len(ts) == 1 && f.Name != "__ROWS_FROM" {
		names[0] = alias
	}
	for i, n := range cols {
		names[i] = n
	}
	m := &tableMeta{Name: alias, isMem: true}
	for i, t := range ts {
		m.Columns = append(m.Columns, columnMeta{Name: names[i], Type: t, Nullable: true})
	}
	m.nameColumns(names)
	return m, ts[0], nil
}
func containsSRF(x Expr) bool {
	has := false
	walkExpr(x, func(e Expr) {
		if f, ok := e.(*Func); ok && isSRF(f.Name) {
			has = true
		}
	})
	return has
}

// srfRows is shared by FROM and ProjectSet. Each call is bounded and checks
// cancellation; ROWS FROM and multi-argument UNNEST zip rather than cross join.
func srfRows(env *evalEnv, f *Func) ([][]Value, []ColType, error) {
	if err := cancelled(env.ctx); err != nil {
		return nil, nil, err
	}
	if f.Name == "__ORDINALITY" {
		rows, ts, err := srfRows(env, f.Args[0].(*Func))
		for i := range rows {
			rows[i] = append(rows[i], intValue(typeInt64, int64(i+1)))
		}
		return rows, append(ts, typeInt64), err
	}
	if f.Name == "__ROWS_FROM" {
		var sets [][][]Value
		var types [][]ColType
		for _, a := range f.Args {
			r, t, err := srfRows(env, a.(*Func))
			if err != nil {
				return nil, nil, err
			}
			sets = append(sets, r)
			types = append(types, t)
		}
		return zipSRFs(env.ctx, sets, types)
	}
	a, err := env.evalItems(f.Args)
	if err != nil {
		return nil, nil, err
	}
	ts := make([]ColType, len(a))
	for i, v := range a {
		ts[i] = v.T
	}
	t := funcType(f, ts)
	var vals []Value
	switch f.Name {
	case "GENERATE_SERIES":
		t, err = seriesType(ts)
		if err == nil {
			vals, err = generateSeries(env.ctx, t, a, env.zone())
		}
	case "UNNEST":
		var sets [][][]Value
		var types [][]ColType
		for _, v := range a {
			if v.T.Kind != KindArray {
				return nil, nil, stateErrorf(stateDatatypeMismatch, "unnest requires an array")
			}
			var rows [][]Value
			if !v.Null {
				if len(v.fields()) > maxSeriesRows {
					return nil, nil, errTooManySeriesRows()
				}
				for i, x := range v.fields() {
					if i%cancelEvery == 0 {
						if err := cancelled(env.ctx); err != nil {
							return nil, nil, err
						}
					}
					rows = append(rows, []Value{x})
				}
			}
			sets = append(sets, rows)
			types = append(types, []ColType{v.T.element()})
		}
		return zipSRFs(env.ctx, sets, types)
	case "GENERATE_SUBSCRIPTS":
		v, _, e := env.evalArrayFunc(f)
		err = e
		vals = v.fields()
		t = typeInt32
	case "STRING_TO_TABLE":
		v, _, e := env.evalExtraArray(&Func{Name: "STRING_TO_ARRAY", Args: f.Args})
		err = e
		if !v.Null {
			vals = v.fields()
		}
		t = typeString
	case "REGEXP_SPLIT_TO_TABLE", "REGEXP_MATCHES":
		return splitSRF(env, f, a)
	default:
		return nil, nil, stateErrorf(stateFeatureNotSupported, "unsupported set-returning function %s", f.Name)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(vals) > maxSeriesRows {
		return nil, nil, errTooManySeriesRows()
	}
	rows := make([][]Value, len(vals))
	for i, v := range vals {
		if i%cancelEvery == 0 {
			if err := cancelled(env.ctx); err != nil {
				return nil, nil, err
			}
		}
		rows[i] = []Value{v}
	}
	return rows, []ColType{t}, nil
}
func zipSRFs(ctx context.Context, sets [][][]Value, types [][]ColType) ([][]Value, []ColType, error) {
	n := 0
	var ts []ColType
	for i, r := range sets {
		n = max(n, len(r))
		ts = append(ts, types[i]...)
	}
	if n > maxSeriesRows {
		return nil, nil, errTooManySeriesRows()
	}
	out := make([][]Value, n)
	for i := range out {
		if i%cancelEvery == 0 {
			if err := cancelled(ctx); err != nil {
				return nil, nil, err
			}
		}
		for j, r := range sets {
			if i < len(r) {
				out[i] = append(out[i], r[i]...)
			} else {
				for _, t := range types[j] {
					out[i] = append(out[i], nullValue(t))
				}
			}
		}
	}
	return out, ts, nil
}
func splitSRF(env *evalEnv, f *Func, a []Value) ([][]Value, []ColType, error) {
	t := typeString
	if f.Name == "REGEXP_MATCHES" {
		t = arrayOf(typeString)
	}
	ts := []ColType{t}
	if a[0].Null || a[1].Null {
		return nil, ts, nil
	}
	flags := ""
	if len(a) > 2 {
		if a[2].Null {
			return nil, ts, nil
		}
		flags = a[2].S
	}
	if f.Name != "REGEXP_MATCHES" && strings.Contains(flags, "g") {
		return nil, nil, stateErrorf(stateInvalidParameter, "%s does not support the global option", f.Name)
	}
	fl, err := parseRegexpFlags(f.Name, flags)
	if err != nil {
		return nil, nil, err
	}
	rs, err := compilePattern(f.Name, a[1].S, fl)
	if err != nil {
		return nil, nil, err
	}
	limit := maxSeriesRows + 1
	if f.Name == "REGEXP_MATCHES" && !strings.Contains(flags, "g") {
		limit = 1
	}
	matches, err := rs.allContext(env.ctx, a[0].S, 0, limit)
	if err != nil {
		return nil, nil, err
	}
	var rows [][]Value
	s := a[0].S
	pos := 0
	for _, m := range matches {
		if len(rows) >= maxSeriesRows {
			return nil, nil, errTooManySeriesRows()
		}
		if err := cancelled(env.ctx); err != nil {
			return nil, nil, err
		}
		if f.Name == "REGEXP_MATCHES" {
			var vals []Value
			groups := m
			if len(m) > 2 {
				groups = m[2:]
			}
			for i := 0; i < len(groups); i += 2 {
				v := nullValue(typeString)
				if groups[i] >= 0 {
					v = stringValue(s[groups[i]:groups[i+1]])
				}
				vals = append(vals, v)
			}
			rows = append(rows, []Value{arrayValue(vals, typeString)})
		} else {
			if m[0] == m[1] && (m[0] == 0 || m[0] == len(s) || m[0] == pos) {
				continue
			}
			rows = append(rows, []Value{stringValue(s[pos:m[0]])})
			pos = m[1]
		}
	}
	if f.Name != "REGEXP_MATCHES" {
		if len(rows) >= maxSeriesRows {
			return nil, nil, errTooManySeriesRows()
		}
		rows = append(rows, []Value{stringValue(s[pos:])})
	}
	return rows, ts, nil
}

func sortedRowNames(row map[string]Value) []string {
	names := make([]string, 0, len(row))
	for k := range row {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

func validateSetReturning(x Expr) error {
	var err error
	walkExprPruned(x, func(v Expr) bool {
		if err != nil {
			return false
		}
		switch f := v.(type) {
		case *WindowFunc:
			for _, a := range callArgs(f.Func, f) {
				if containsSRF(a) {
					err = stateErrorf(stateFeatureNotSupported, "set-returning functions are not allowed in window function arguments")
				}
			}
		case *Case:
			if containsSRF(f) {
				err = stateErrorf(stateFeatureNotSupported, "set-returning functions are not allowed in CASE")
			}
		case *Func:
			if f.Name == "COALESCE" || aggregateFuncs[f.Name] {
				for _, a := range f.Args {
					if containsSRF(a) {
						err = stateErrorf(stateFeatureNotSupported, "set-returning functions are not allowed in %s", f.Name)
					}
				}
			}
		}
		return true
	})
	return err
}
