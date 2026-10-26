// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strconv"
)

func (p *parser) parseValuesQuery() (*SelectStmt, error) {
	sel := &SelectStmt{Items: []SelectItem{{Star: true, Text: "*"}}}
	for {
		if err := p.expectOp("("); err != nil {
			return nil, err
		}
		var row []Expr
		for {
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			row = append(row, x)
			if !p.acceptOp(",") {
				break
			}
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		if len(sel.Values) > 0 && len(row) != len(sel.Values[0]) {
			return nil, syntaxErr("VALUES lists must all be the same length")
		}
		sel.Values = append(sel.Values, row)
		if !p.acceptOp(",") {
			return sel, nil
		}
	}
}

// Resolve each column across every row at once (not pairwise UNION boundaries).
func (e *executor) valuesRelation(ctx context.Context, rows [][]Expr) (*tableMeta, error) {
	cols := make([]resultColumn, len(rows[0]))
	for j := range cols {
		xs := make([]Expr, len(rows))
		for i, row := range rows {
			xs[i] = row[j]
			if isAggregate(row[j]) || containsWindow(row[j]) {
				return nil, stateErrorf("42803", "aggregate and window functions are not allowed in VALUES")
			}
			if err := e.bind(ctx, row[j]); err != nil {
				return nil, err
			}
		}
		// VALUES uses SQL common-type categories; unlike permissive scalar
		// coercion, BOOLEAN and numeric inputs cannot be combined.
		common := typeNull
		for _, x := range xs {
			if unknownLiteral(x) {
				continue
			}
			t, err := inferType(x, e.scopeTypes(), e.paramTypes)
			if err != nil {
				return nil, invalidArg(err)
			}
			joined, ok := setOpType(common, t)
			if !ok {
				return nil, invalidArg(&typeMatchError{what: "VALUES", a: common, b: t})
			}
			common = joined
		}
		t, err := exprsType("VALUES", xs, e.scopeTypes(), e.paramTypes, false)
		if err != nil {
			return nil, invalidArg(err)
		}
		if t.Kind == KindNull {
			t = typeString
		}
		for _, x := range xs {
			if unknownLiteral(x) {
				if e.localLits != nil && readsLocalTime(t) {
					e.localLits[x.(*Literal)] = true
				}
				if err := checkLiteral(x, t); err != nil {
					return nil, invalidArg(err)
				}
			}
			e.cache.noteParamOf(x, t)
		}
		cols[j] = resultColumn{Name: "column" + strconv.Itoa(j+1), Type: t}
	}
	var values [][]Value
	if !e.planOnly {
		env := e.newEnv(ctx, nil, e.params)
		for _, row := range rows {
			if err := cancelled(ctx); err != nil {
				return nil, err
			}
			out := make([]Value, len(cols))
			for j, x := range row {
				v, err := env.eval(x)
				if err != nil {
					return nil, invalidArg(err)
				}
				out[j], err = coerceIn(v, cols[j].Type, e.zone())
				if err != nil {
					return nil, invalidArg(err)
				}
			}
			values = append(values, out)
		}
	}
	return e.retainedMemTable(ctx, "*VALUES*", cols, values, nil)
}
