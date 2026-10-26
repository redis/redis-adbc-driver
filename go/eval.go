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

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"
)

// evalEnv supplies column values (keyed by canonical column name) and bound
// parameters to expression evaluation.
type evalEnv struct {
	ctx   context.Context
	exec  *executor
	outer *evalEnv // enclosing query's row, for correlated references

	row    map[string]Value
	types  map[string]ColType
	params []Value
	// aggs holds the reduced value of each aggregate call when evaluating
	// over a group (SELECT items, HAVING and ORDER BY of grouped queries).
	aggs map[*Func]Value
	// win holds the value of each window function call for every row of
	// the window input; winRow is the current row's position in it.
	win    map[*WindowFunc][]Value
	winRow int
	// action is merge_action() while MERGE … RETURNING evaluates a row.
	action string
}

func isAggregate(e Expr) bool {
	found := false
	walkExpr(e, func(x Expr) {
		if f, ok := x.(*Func); ok && aggregateFuncs[f.Name] {
			found = true
		}
	})
	return found
}

func walkExpr(e Expr, fn func(Expr)) {
	walkExprPruned(e, func(x Expr) bool {
		fn(x)
		return true
	})
}

// walkExprPruned is walkExpr, where fn returns whether to visit the
// children of a node.
func walkExprPruned(e Expr, fn func(Expr) bool) {
	if e == nil || !fn(e) {
		return
	}
	switch x := e.(type) {
	case *Unary:
		walkExprPruned(x.X, fn)
	case *Binary:
		walkExprPruned(x.L, fn)
		walkExprPruned(x.R, fn)
	case *IsNull:
		walkExprPruned(x.X, fn)
	case *Cast:
		walkExprPruned(x.X, fn)
		walkExprPruned(x.OnError, fn)
	case *Func:
		for _, a := range x.Args {
			walkExprPruned(a, fn)
		}
		for _, o := range x.OrderBy {
			walkExprPruned(o.Expr, fn)
		}
		walkExprPruned(x.Filter, fn)
	case *Case:
		walkExprPruned(x.Operand, fn)
		for _, w := range x.Whens {
			walkExprPruned(w.When, fn)
			walkExprPruned(w.Then, fn)
		}
		walkExprPruned(x.Else, fn)
	case *Subquery:
		// The body is a separate scope; only the IN operand belongs here. A
		// row operand's items are visited, not the row: a *RowExpr is
		// visited only where it is an error.
		for _, it := range subqueryOperands(x) {
			walkExprPruned(it, fn)
		}
	case *RowColumn:
		walkExprPruned(x.Sub, fn)
	case *RowExpr:
		for _, it := range x.Items {
			walkExprPruned(it, fn)
		}
	case *FieldSelect:
		walkExprPruned(x.X, fn)
	case *WindowFunc:
		// The call itself is not visited as a *Func: SUM(x) OVER (…) is not
		// an aggregate, but aggregates in its arguments (SUM(COUNT(*))) are.
		for _, c := range x.children() {
			walkExprPruned(c, fn)
		}
	}
}

// walkOutsideAggregates visits the nodes of e that are not inside an
// aggregate call.
func walkOutsideAggregates(e Expr, fn func(Expr)) {
	if e == nil {
		return
	}
	if f, ok := e.(*Func); ok && aggregateFuncs[f.Name] {
		return
	}
	fn(e)
	switch x := e.(type) {
	case *Unary:
		walkOutsideAggregates(x.X, fn)
	case *Binary:
		walkOutsideAggregates(x.L, fn)
		walkOutsideAggregates(x.R, fn)
	case *IsNull:
		walkOutsideAggregates(x.X, fn)
	case *Cast:
		walkOutsideAggregates(x.X, fn)
		walkOutsideAggregates(x.OnError, fn)
	case *Func:
		for _, a := range x.Args {
			walkOutsideAggregates(a, fn)
		}
	case *Case:
		walkOutsideAggregates(x.Operand, fn)
		for _, w := range x.Whens {
			walkOutsideAggregates(w.When, fn)
			walkOutsideAggregates(w.Then, fn)
		}
		walkOutsideAggregates(x.Else, fn)
	case *Subquery:
		for _, it := range subqueryOperands(x) {
			walkOutsideAggregates(it, fn)
		}
	case *RowExpr:
		for _, it := range x.Items {
			walkOutsideAggregates(it, fn)
		}
	case *FieldSelect:
		walkOutsideAggregates(x.X, fn)
	case *WindowFunc:
		for _, c := range x.children() {
			walkOutsideAggregates(c, fn)
		}
	}
}

// columnRefs returns the distinct column names referenced by an expression.
func columnRefs(e Expr, out map[string]bool) {
	walkExpr(e, func(x Expr) {
		if c, ok := x.(*ColumnRef); ok && c.Outer == 0 {
			out[c.Name] = true
		}
	})
}

func truthy(v Value) (bool, bool) {
	if v.Null {
		return false, false
	}
	switch v.T.Kind {
	case KindBool, KindInt16, KindInt32, KindInt64:
		return v.I != 0, true
	}
	return false, false
}

func (env *evalEnv) eval(e Expr) (Value, error) {
	switch x := e.(type) {
	case *Literal:
		return x.V, nil
	case *Param:
		if x.Index >= len(env.params) {
			return Value{}, fmt.Errorf("parameter %d is not bound", x.Index+1)
		}
		return env.params[x.Index], nil
	case *ColumnRef:
		if x.Outer > 0 {
			return env.lookupUp(x.Name, x.Outer)
		}
		if v, ok := env.row[x.Name]; ok {
			return v, nil
		}
		if t, ok := env.types[x.Name]; ok {
			return nullValue(t), nil
		}
		return Value{}, fmt.Errorf("unknown column %q", x.Name)
	case *Cast:
		v, err := env.eval(x.X)
		if err != nil {
			return Value{}, err
		}
		c, err := castValue(v, x.T, env.zone())
		if err != nil && x.OnError != nil && castable(v.T, x.T) {
			// TRY_CAST and DEFAULT … ON CONVERSION ERROR: a value that
			// cannot be converted gives the fallback, but types that never
			// convert (DATE to BOOLEAN) are still an error.
			d, err := env.eval(x.OnError)
			if err != nil {
				return Value{}, err
			}
			return castValue(d, x.T, env.zone())
		}
		return c, err
	case *IsNull:
		v, err := env.eval(x.X)
		if err != nil {
			return Value{}, err
		}
		if v.T.Kind == KindRecord && !x.field {
			return boolValue(recordIsNull(v, x.Not)), nil
		}
		return boolValue(v.Null != x.Not), nil
	case *Unary:
		v, err := env.eval(x.X)
		if err != nil {
			return Value{}, err
		}
		if x.Op == "NOT" {
			b, ok := truthy(v)
			if !ok {
				return nullValue(typeBool), nil
			}
			return boolValue(!b), nil
		}
		if o, ok := operatorLiteral(x); ok {
			_, _, err := o.typeNext(typeNull) // - unknown
			return Value{}, err
		}
		return negate(v)
	case *Binary:
		switch x.Op {
		case "AND", "OR":
			if x.Op == "OR" {
				if v, ok, err := env.evalInList(x); ok {
					return v, err
				}
			}
			l, err := env.eval(x.L)
			if err != nil {
				return Value{}, err
			}
			lb, lok := truthy(l)
			if x.Op == "AND" && lok && !lb {
				return boolValue(false), nil
			}
			if x.Op == "OR" && lok && lb {
				return boolValue(true), nil
			}
			r, err := env.eval(x.R)
			if err != nil {
				return Value{}, err
			}
			rb, rok := truthy(r)
			if x.Op == "AND" {
				if rok && !rb {
					return boolValue(false), nil
				}
				if lok && rok {
					return boolValue(true), nil
				}
				return nullValue(typeBool), nil
			}
			if rok && rb {
				return boolValue(true), nil
			}
			if lok && rok {
				return boolValue(false), nil
			}
			return nullValue(typeBool), nil
		}
		l, err := env.eval(x.L)
		if err != nil {
			return Value{}, err
		}
		r, err := env.eval(x.R)
		if err != nil {
			return Value{}, err
		}
		if o, ok := operatorLiteral(x); ok {
			if l, r, err = env.literalOperands(x, o, l, r); err != nil {
				return Value{}, err
			}
		}
		return binaryOp(x.Op, l, r, env.zone())
	case *RowExpr:
		items, err := env.evalItems(x.Items)
		if err != nil {
			return Value{}, err
		}
		return recordValue(items), nil
	case *FieldSelect:
		return env.evalFieldSelect(x)
	case *Func:
		return env.evalFunc(x)
	case *Case:
		return env.evalCase(x)
	case *Subquery:
		return env.evalSubquery(x)
	case *RowColumn:
		return env.evalRowColumn(x)
	case *WindowFunc:
		vals, ok := env.win[x]
		if !ok {
			return Value{}, fmt.Errorf("window function %s is not allowed here", x.Func.Name)
		}
		return vals[env.winRow], nil
	}
	return Value{}, fmt.Errorf("unsupported expression %T", e)
}

func (env *evalEnv) evalCase(c *Case) (Value, error) {
	var operand Value
	if c.Operand != nil {
		v, err := env.eval(c.Operand)
		if err != nil {
			return Value{}, err
		}
		operand = v
	}
	for _, w := range c.Whens {
		v, err := env.eval(w.When)
		if err != nil {
			return Value{}, err
		}
		if c.Operand != nil {
			if v, err = binaryOp("=", operand, v, env.zone()); err != nil {
				return Value{}, err
			}
		}
		if b, ok := truthy(v); ok && b {
			return env.branchValue(c, w.Then)
		}
	}
	if c.Else != nil {
		return env.branchValue(c, c.Else)
	}
	return nullValue(typeNull), nil
}

// branchValue evaluates the chosen branch of a CASE or IIF (e). A quoted
// string literal is a value of the result's type (literalAs): '1' is the
// integer 1 in CASE WHEN … THEN '1' ELSE 2 END. A date, time or timestamp
// is converted to the result's type when that is another date or time
// type: a TIMESTAMP or DATE branch of a TIMESTAMP WITH TIME ZONE result is
// read as a local time in the session time zone, and a TIME branch of a
// TIME WITH TIME ZONE result takes the session time zone's offset, as
// Postgres converts a CASE's branches, so that sorting, grouping and text
// see the result's values. (Other types keep their branch's value, which
// the result's type holds.)
func (env *evalEnv) branchValue(e, branch Expr) (Value, error) {
	v, err := env.eval(branch)
	if err == nil && unknownLiteral(branch) {
		return env.literalAs(v, e)
	}
	k := v.T.Kind
	if err != nil || v.Null || (k != KindTimestamp && k != KindDate && k != KindTime) || env.exec == nil {
		return v, err
	}
	t, _ := env.resultType(e)
	if k == KindTime {
		if t.Kind != KindTimeTZ {
			return v, nil
		}
		return coerceIn(v, t.withFracDigits(max(t.fracDigits(), v.T.fracDigits())), env.zone())
	}
	if t.Kind != KindTimestamp || (v.T.Kind == KindTimestamp && v.T.TZ == t.TZ) {
		return v, nil
	}
	if v.T.Kind == KindTimestamp {
		t = finerTime(t, v.T) // nothing is rounded
	}
	return coerceIn(v, t, env.zone())
}

// resultType returns the type of e, a CASE, IIF or call whose value is one
// of its parts, as inferType gives it (cached for the statement); ok is
// false if it isn't known.
func (env *evalEnv) resultType(e Expr) (ColType, bool) {
	ex := env.exec
	if ex == nil {
		t, err := inferType(e, env.types, nil)
		return t, err == nil && t.Kind != KindNull
	}
	ex.ensureCache()
	t, ok := ex.cache.branchTypes[e]
	if !ok {
		t, _ = inferType(e, env.types, ex.paramTypes) // the zero type on error
		ex.cache.branchTypes[e] = t
	}
	return t, t.Kind != KindNull
}

// literalAs returns v, the text of a quoted string literal that is a value
// of e (see commonExprType), as a value of e's type, in the session time
// zone: a timestamp with time zone without an offset is a local time.
// Without a known type it stays text.
func (env *evalEnv) literalAs(v Value, e Expr) (Value, error) {
	t, ok := env.resultType(e)
	if v.Null || !ok || v.T == t {
		return v, nil
	}
	return coerceIn(v, t, env.zone())
}

// literalOperands returns l and r, the values of the operands of x, an
// arithmetic operator with the quoted string literal o (operatorLiteral),
// with the literal read as the type it takes there (literalOperandType), in
// the session time zone: in id + '1', '1' is an integer. Next to an operand
// whose type isn't known (a parameter without a value), it takes the type
// of the other value, and the result is NULL if that is NULL.
func (env *evalEnv) literalOperands(x Expr, o opLiteral, l, r Value) (Value, Value, error) {
	v, ov := &r, l
	if o.left {
		v, ov = &l, r
	}
	t, ok, err := env.literalOperandType(x, o)
	if err != nil {
		return Value{}, Value{}, err
	}
	if !ok {
		if ov.Null {
			*v = nullValue(typeNull)
			return l, r, nil
		}
		if t, _, err = o.typeNext(ov.T); err != nil {
			return Value{}, Value{}, err
		}
	}
	if *v, err = coerceIn(*v, t, env.zone()); err != nil {
		return Value{}, Value{}, err
	}
	return l, r, nil
}

// literalOperandType returns the type of o, the quoted string literal of
// x, an arithmetic operator, as inferType gives it (typeNext, cached for
// the statement); ok is false if it isn't known.
func (env *evalEnv) literalOperandType(x Expr, o opLiteral) (ColType, bool, error) {
	typed := func() (ColType, bool, error) {
		var params []ColType
		if env.exec != nil {
			params = env.exec.paramTypes
		}
		t, err := inferType(o.other, env.types, params)
		if err != nil {
			return ColType{}, false, nil // taken from the value
		}
		return o.typeNext(t)
	}
	ex := env.exec
	if ex == nil {
		return typed()
	}
	ex.ensureCache()
	if t, ok := ex.cache.literalTypes[x]; ok {
		return t, t.Kind != KindNull, nil
	}
	t, ok, err := typed()
	if err != nil {
		return ColType{}, false, err
	}
	if !ok {
		t = typeNull
	}
	ex.cache.literalTypes[x] = t
	return t, ok, nil
}

// concatValues joins the text of the non-NULL values with sep, in the
// session time zone z.
func concatValues(sep string, args []Value, z tzZone) Value {
	var b strings.Builder
	first := true
	for _, a := range args {
		if a.Null {
			continue
		}
		if !first {
			b.WriteString(sep)
		}
		b.WriteString(a.textIn(z))
		first = false
	}
	return stringValue(b.String())
}

// evalFunc evaluates a call. The registry (funcs.go) says which code
// evaluates it; binding has checked its name and arguments.
func (env *evalEnv) evalFunc(f *Func) (Value, error) {
	d, ok := callee(f)
	if !ok {
		return Value{}, fmt.Errorf("unsupported function %s", f.Name)
	}
	switch d.kind {
	case aggregateKind:
		if v, ok := env.aggs[f]; ok {
			return v, nil
		}
		return Value{}, fmt.Errorf("aggregate %s is not allowed here", f.Name)
	case windowKind:
		return Value{}, fmt.Errorf("window function %s requires an OVER clause", f.Name)
	case tableKind:
		return Value{}, fmt.Errorf("%s is only supported in FROM", strings.ToLower(f.Name))
	}
	if err := checkArity(f); err != nil {
		return Value{}, err
	}
	switch d.impl {
	case implMergeAction:
		return env.mergeAction()
	case implIIF:
		return env.evalIIF(f)
	case implJSON:
		return env.evalJSONFunc(f)
	case implGrouping:
		return Value{}, fmt.Errorf("GROUPING is only allowed with GROUP BY")
	}
	args := make([]Value, len(f.Args))
	for i, a := range f.Args {
		v, err := env.eval(a)
		if err != nil {
			return Value{}, err
		}
		args[i] = v
	}
	if o, ok := operatorLiteral(f); ok {
		// MOD (%) reads a quoted literal as an operator does.
		var err error
		if args[0], args[1], err = env.literalOperands(f, o, args[0], args[1]); err != nil {
			return Value{}, err
		}
	}
	if d.impl != implCoalesce && d.impl != implConditional && !seesPadding[f.Name] {
		// Functions take CHAR values as text, without their padding.
		for i, a := range args {
			if a.T.isChar() {
				args[i] = Value{T: typeString, Null: a.Null, S: a.Text()}
			}
		}
	}
	switch d.impl {
	case implCoalesce:
		// The result has the arguments' common type (quoted literals are
		// values of it), as typed: a NULL argument has no type.
		t, ok := env.resultType(f)
		if !ok {
			t = unifiedType(f, argTypes(args))
		}
		for _, a := range args {
			if !a.Null {
				return coerceIn(a, t, env.zone())
			}
		}
		return nullValue(t), nil
	case implConditional:
		t, ok := env.resultType(f)
		if !ok {
			t, _ = scalarFuncType(f, argTypes(args))
		}
		return evalConditional(f, args, t, env.zone())
	case implConcat:
		// NULL arguments are skipped, as in Postgres (|| propagates NULL).
		if f.Name == "CONCAT" {
			return concatValues("", args, env.zone()), nil
		}
		if args[0].Null {
			return nullValue(typeString), nil
		}
		return concatValues(args[0].textIn(env.zone()), args[1:], env.zone()), nil
	}
	for _, a := range args {
		if a.Null {
			return nullValue(inferFuncType(f, argTypes(args))), nil
		}
	}
	switch d.impl {
	case implDateTime:
		if v, ok, err := env.evalDateTimeFunc(f, args); ok {
			return v, err
		}
		return Value{}, fmt.Errorf("unsupported function %s", f.Name)
	case implScalar:
		return scalarFunc(f, args, env.zone())
	}
	switch f.Name {
	case "FROM_HEX", "UNHEX", "DECODE_HEX":
		b, err := hex.DecodeString(args[0].S)
		if err != nil {
			return Value{}, stateErr(stateInvalidParameter, "invalid hex string %q", args[0].S)
		}
		return binaryValue(string(b)), nil
	case "TO_HEX", "HEX":
		return stringValue(hex.EncodeToString([]byte(args[0].S))), nil
	case "LOWER", "LCASE":
		return stringValue(strings.ToLower(args[0].textIn(env.zone()))), nil
	case "UPPER", "UCASE":
		return stringValue(strings.ToUpper(args[0].textIn(env.zone()))), nil
	case "LENGTH", "CHAR_LENGTH", "CHARACTER_LENGTH", "LEN":
		if args[0].T.Kind == KindBinary {
			return intValue(typeInt64, int64(len(args[0].S))), nil
		}
		return intValue(typeInt64, int64(utf8.RuneCountInString(args[0].textIn(env.zone())))), nil
	case "LIKE", "ILIKE":
		esc := ""
		if len(args) == 3 {
			esc = args[2].Text()
			if utf8.RuneCountInString(esc) > 1 {
				return Value{}, stateErr(stateEscape, "ESCAPE must be a single character")
			}
		}
		s, pat := args[0].paddedIn(env.zone()), args[1].textIn(env.zone())
		if f.Name == "ILIKE" {
			s, pat, esc = strings.ToLower(s), strings.ToLower(pat), strings.ToLower(esc)
		}
		return boolValue(likeMatch(s, pat, esc)), nil
	case "ABS":
		v := args[0]
		switch {
		case v.T.Kind.isInteger():
			if v.I < 0 {
				return negate(v)
			}
			return v, nil
		case v.T.Kind.isFloat():
			return floatValue(v.T, math.Abs(v.F)), nil
		case v.T.Kind == KindDecimal:
			return decimalValue(new(big.Int).Abs(v.D), v.T.Precision, v.T.Scale), nil
		}
		return Value{}, fmt.Errorf("ABS of %s", v.T.Kind)
	}
	return Value{}, fmt.Errorf("unsupported function %s", f.Name)
}

// seesPadding are the functions (operators) that take a CHAR value with its
// padding, as Postgres's pattern matching does.
var seesPadding = map[string]bool{"LIKE": true, "ILIKE": true, "~": true, "~*": true, "SIMILAR TO": true}

// keepsTypmod are the functions whose result keeps the type modifier of
// their arguments, as in Postgres: the length of a string type they share,
// or the fractional digits of their times or timestamps (commonType).
var keepsTypmod = map[string]bool{"COALESCE": true, "IFNULL": true, "NVL": true, "NULLIF": true,
	"GREATEST": true, "LEAST": true, "IIF": true}

// padded is the text of a value, with a CHAR value's padding.
func (v Value) padded() string { return v.paddedIn(utcZone) }

// paddedIn is padded in the session time zone z.
func (v Value) paddedIn(z tzZone) string {
	if v.T.Kind == KindString {
		return v.S
	}
	return v.textIn(z)
}

func argTypes(args []Value) []ColType {
	out := make([]ColType, len(args))
	for i, a := range args {
		out[i] = a.T
	}
	return out
}

func negate(v Value) (Value, error) {
	if v.Null {
		return v, nil
	}
	switch {
	case v.T.Kind.isInteger():
		if v.I == math.MinInt64 {
			return Value{}, stateErr(stateNumericRange, "integer overflow")
		}
		return intValue(v.T, -v.I), nil
	case v.T.Kind.isFloat():
		return floatValue(v.T, -v.F), nil
	case v.T.Kind == KindDecimal:
		return decimalValue(new(big.Int).Neg(v.D), v.T.Precision, v.T.Scale), nil
	case v.T.Kind == KindInterval:
		return negateInterval(v)
	}
	return Value{}, fmt.Errorf("cannot negate %s", v.T.Kind)
}

func isComparison(op string) bool {
	switch op {
	case "=", "<>", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// binaryOp evaluates an operator in the session time zone z.
func binaryOp(op string, l, r Value, z tzZone) (Value, error) {
	if isDistinctOp(op) {
		return distinctFrom(op, l, r, z)
	}
	if isComparison(op) {
		if l.Null || r.Null {
			return nullValue(typeBool), nil
		}
		c, ok := compareIn(l, r, z)
		if !ok {
			if err := recordCompareError(l, r); err != nil {
				return Value{}, err
			}
			if err := textOperandError(l, r, z); err != nil {
				return Value{}, err
			}
			return Value{}, fmt.Errorf("cannot compare %s with %s", l.T.Kind, r.T.Kind)
		}
		switch op {
		case "=":
			return boolValue(c == 0), nil
		case "<>":
			return boolValue(c != 0), nil
		case "<":
			return boolValue(c < 0), nil
		case "<=":
			return boolValue(c <= 0), nil
		case ">":
			return boolValue(c > 0), nil
		default:
			return boolValue(c >= 0), nil
		}
	}
	if op == "||" {
		if l.Null || r.Null {
			return nullValue(typeString), nil
		}
		return stringValue(l.textIn(z) + r.textIn(z)), nil
	}
	if rt, ok, err := temporalType(op, l.T, r.T); ok {
		if err != nil {
			return Value{}, err
		}
		if l.Null || r.Null {
			return nullValue(rt), nil
		}
		return temporalOp(op, l, r, rt, z)
	}
	rt, err := arithmeticType(op, l.T, r.T)
	if err != nil {
		return Value{}, err
	}
	if l.Null || r.Null {
		return nullValue(rt), nil
	}
	switch rt.Kind {
	case KindInt64:
		switch op {
		case "+", "-", "*", "/":
			v, err := integerArithmetic(op[0], l.I, r.I)
			if err != nil {
				return Value{}, err
			}
			return intValue(rt, v), nil
		}
	case KindFloat64:
		a, _ := l.asFloat()
		b, _ := r.asFloat()
		switch op {
		case "+":
			return floatValue(rt, a+b), nil
		case "-":
			return floatValue(rt, a-b), nil
		case "*":
			return floatValue(rt, a*b), nil
		case "/":
			if b == 0 {
				return Value{}, stateErr(stateDivisionByZero, "division by zero")
			}
			return floatValue(rt, a/b), nil
		}
	case KindDecimal:
		ad, as, _ := asDecimal(l)
		bd, bs, _ := asDecimal(r)
		switch op {
		case "+", "-":
			s := max(as, bs)
			x, y := rescaleDecimal(ad, as, s), rescaleDecimal(bd, bs, s)
			if op == "+" {
				return decimalValue(x.Add(x, y), rt.Precision, s), nil
			}
			return decimalValue(x.Sub(x, y), rt.Precision, s), nil
		case "*":
			return decimalValue(new(big.Int).Mul(ad, bd), rt.Precision, as+bs), nil
		}
	}
	return Value{}, fmt.Errorf("unsupported operator %s for %s and %s", op, l.T.Kind, r.T.Kind)
}

// arithmeticType returns the result type of an arithmetic operator.
func arithmeticType(op string, a, b ColType) (ColType, error) {
	if rt, ok, err := temporalType(op, a, b); ok {
		return rt, err
	}
	if a.Kind == KindNull {
		a = b
	}
	if b.Kind == KindNull {
		b = a
	}
	if a.Kind == KindNull {
		return typeInt64, nil
	}
	num := func(k Kind) bool { return k.isNumeric() || k == KindBool }
	if a.Kind == KindRecord || b.Kind == KindRecord {
		return ColType{}, stateErr(stateUndefinedFunction, "operator does not exist: %s %s %s", sigTypeName(a), op, sigTypeName(b))
	}
	if !num(a.Kind) || !num(b.Kind) {
		return ColType{}, fmt.Errorf("operator %s is not supported for %s and %s", op, a.Kind, b.Kind)
	}
	switch {
	case a.Kind.isFloat() || b.Kind.isFloat():
		return typeFloat64, nil
	case a.Kind == KindDecimal || b.Kind == KindDecimal:
		if op == "/" {
			return typeFloat64, nil
		}
		as, bs := int32(0), int32(0)
		if a.Kind == KindDecimal {
			as = a.Scale
		}
		if b.Kind == KindDecimal {
			bs = b.Scale
		}
		if op == "*" {
			return decimalType(38, as+bs), nil
		}
		return decimalType(38, max(as, bs)), nil
	}
	return typeInt64, nil
}

// inferFuncType is the result type of a call: without a type modifier,
// except for the functions that keep their arguments' (keepsTypmod) and the
// current times, whose precision argument is one (CURRENT_TIMESTAMP(p)).
func inferFuncType(f *Func, args []ColType) ColType {
	t := funcType(f, args)
	if !keepsTypmod[f.Name] && !currentTimeFuncs[f.Name] {
		t = t.withoutTypmod()
	}
	return t
}

func funcType(f *Func, args []ColType) ColType {
	if t, ok := dateTimeFuncType(f, args); ok {
		return t
	}
	if t, ok := scalarFuncType(f, args); ok {
		return t
	}
	if t, ok := jsonFuncType(f); ok {
		return t
	}
	switch f.Name {
	case "COUNT", "LENGTH", "CHAR_LENGTH", "CHARACTER_LENGTH", "LEN":
		return typeInt64
	case "LIKE", "ILIKE":
		return typeBool
	case "FROM_HEX", "UNHEX", "DECODE_HEX":
		return typeBinary
	case "TO_HEX", "HEX", "LOWER", "LCASE", "UPPER", "UCASE", "CONCAT", "CONCAT_WS":
		return typeString
	case "MERGE_ACTION":
		return typeString
	case "AVG":
		if len(args) == 1 && args[0].Kind == KindInterval {
			return typeInterval
		}
		return typeFloat64
	case "SUM":
		if len(args) == 1 {
			switch {
			case args[0].Kind.isInteger() || args[0].Kind == KindBool:
				return typeInt64
			case args[0].Kind == KindDecimal:
				return decimalType(38, args[0].Scale)
			case args[0].Kind == KindInterval:
				return typeInterval
			}
		}
		return typeFloat64
	case "MIN", "MAX", "ABS":
		if len(args) == 1 {
			return args[0]
		}
	case "COALESCE", "IFNULL", "NVL":
		return unifiedType(f, args)
	}
	return typeNull
}

// inferType determines the static type of an expression.
func inferType(e Expr, cols map[string]ColType, params []ColType) (ColType, error) {
	switch x := e.(type) {
	case *Literal:
		return x.V.T, nil
	case *Param:
		if x.Index < len(params) {
			return params[x.Index], nil
		}
		return typeNull, nil
	case *ColumnRef:
		if x.Outer > 0 {
			return x.OuterType, nil
		}
		if t, ok := cols[x.Name]; ok {
			return t, nil
		}
		return ColType{}, fmt.Errorf("unknown column %q", x.Name)
	case *Cast:
		if x.T.Kind == KindRecord {
			// CAST(r AS RECORD) keeps r's fields.
			if t, err := inferType(x.X, cols, params); err == nil && t.Kind == KindRecord {
				return t, nil
			}
		}
		return x.T, nil
	case *IsNull:
		return typeBool, nil
	case *Unary:
		if x.Op == "NOT" {
			return typeBool, nil
		}
		if o, ok := operatorLiteral(x); ok {
			_, _, err := o.typeNext(typeNull) // - unknown
			return ColType{}, err
		}
		return inferType(x.X, cols, params)
	case *Binary:
		switch {
		case x.Op == "AND" || x.Op == "OR" || isComparison(x.Op) || isDistinctOp(x.Op):
			return typeBool, nil
		case x.Op == "||":
			return typeString, nil
		}
		l, err := inferType(x.L, cols, params)
		if err != nil {
			return ColType{}, err
		}
		r, err := inferType(x.R, cols, params)
		if err != nil {
			return ColType{}, err
		}
		if o, ok := operatorLiteral(x); ok {
			// A quoted string literal takes a type from the operator and
			// the other operand.
			other := r
			if !o.left {
				other = l
			}
			t, ok, err := o.typeNext(other)
			if err != nil {
				return ColType{}, err
			}
			if !ok {
				return typeNull, nil // next to a parameter without a value
			}
			if o.left {
				l = t
			} else {
				r = t
			}
		}
		return arithmeticType(x.Op, l, r)
	case *Func:
		if _, ok := callee(x); !ok {
			return ColType{}, noSuchFunction(x.Name, x.Star, inferArgTypes(x.Args, cols, params))
		}
		if err := checkArity(x); err != nil {
			return ColType{}, err
		}
		if t, ok, err := aggregateType(x, cols, params, false); ok {
			return t, err
		}
		args := make([]ColType, len(x.Args))
		for i, a := range x.Args {
			t, err := inferType(a, cols, params)
			if err != nil {
				return ColType{}, err
			}
			args[i] = t
		}
		if parts, numLits := matchedParts(x); parts != nil {
			// Values of types that don't match are an error.
			if _, err := commonExprType(x.Name, parts, args[len(args)-len(parts):], numLits); err != nil {
				return ColType{}, err
			}
		}
		if o, ok := operatorLiteral(x); ok {
			// MOD (%) of a quoted literal: as an operator's.
			i, j := 0, 1
			if !o.left {
				i, j = 1, 0
			}
			t, ok, err := o.typeNext(args[j])
			if err != nil {
				return ColType{}, err
			}
			if !ok {
				return typeNull, nil
			}
			args[i] = t
		}
		return inferFuncType(x, args), nil
	case *Subquery:
		if x.Kind == SubqueryScalar && x.plan != nil {
			return x.plan.items[0].typ, nil
		}
		return typeBool, nil
	case *WindowFunc:
		return windowType(x, cols, params)
	case *RowExpr:
		fields := make([]ColType, len(x.Items))
		for i, it := range x.Items {
			t, err := inferType(it, cols, params)
			if err != nil {
				return ColType{}, err
			}
			fields[i] = t
		}
		return recordOf(fields), nil
	case *FieldSelect:
		t, err := inferType(x.X, cols, params)
		if err != nil {
			return ColType{}, err
		}
		return fieldSelectType(x, t)
	case *Case:
		return exprsType("CASE", caseResults(x), cols, params, false)
	}
	return ColType{}, fmt.Errorf("unsupported expression %T", e)
}

// caseResults are the results of a CASE: its ELSE and its THEN values, in
// the order Postgres matches their types (for its errors).
func caseResults(c *Case) []Expr {
	results := make([]Expr, 0, len(c.Whens)+1)
	if c.Else != nil {
		results = append(results, c.Else)
	}
	for _, w := range c.Whens {
		results = append(results, w.Then)
	}
	return results
}

// commonType is the type that can hold values of both a and b, text for
// types that have none (see matchTypes).
func commonType(a, b ColType) ColType {
	if t, ok := matchTypes(a, b); ok {
		return t
	}
	return typeString
}

// matchTypes is the type that can hold values of both a and b (used for
// the results of CASE and the arguments of COALESCE, GREATEST and LEAST);
// ok is false for types that have none, such as an INTEGER and a VARCHAR,
// or a DATE and an INTERVAL, which Postgres doesn't match either.
func matchTypes(a, b ColType) (ColType, bool) {
	switch {
	case a.Kind == KindNull:
		return b, true
	case b.Kind == KindNull || a == b:
		return a, true
	case a.Kind == KindRecord && b.Kind == KindRecord:
		return commonRecord(a, b), true
	case (a.Kind.isNumeric() || a.Kind == KindBool) && (b.Kind.isNumeric() || b.Kind == KindBool):
		if a.Kind.isInteger() && b.Kind.isInteger() {
			if a.Kind > b.Kind {
				return a, true
			}
			return b, true
		}
		t, err := arithmeticType("+", a, b)
		if err != nil {
			return ColType{}, false
		}
		if t.Kind == KindDecimal {
			// Keep enough scale for both sides.
			t.Scale = max(a.Scale, b.Scale)
		}
		return t, true
	case a.Kind == KindTimestamp && b.Kind == KindTimestamp, a.Kind == KindTime && b.Kind == KindTime,
		a.Kind == KindTimeTZ && b.Kind == KindTimeTZ:
		// The more fractional digits, so that no value is rounded; with a
		// time zone if either has one, as in Postgres.
		t := finerTime(a, b)
		if b.TZ != "" {
			t.TZ = b.TZ
		}
		return t, true
	case a.isTimeOfDay() && b.isTimeOfDay():
		// A TIME and a time with time zone: a time with time zone (Postgres
		// converts the TIME).
		t := finerTime(a, b)
		t.Kind = KindTimeTZ
		return t.withFracDigits(t.fracDigits()), true
	case a.Kind == KindDate && b.Kind == KindTimestamp:
		return b, true
	case a.Kind == KindTimestamp && b.Kind == KindDate:
		return a, true
	case a.Kind == KindString && b.Kind == KindString:
		// Different lengths: no length; CHAR only if both are.
		return ColType{Kind: KindString, Fixed: a.Fixed && b.Fixed}, true
	case a.Kind == b.Kind:
		return a, true
	}
	return ColType{}, false
}

// likeMatch implements SQL LIKE: '%' matches any run of characters, '_' one
// character, and esc (if set) makes the next character literal.
func likeMatch(s, pat, esc string) bool {
	sr, pr := []rune(s), []rune(pat)
	var escR rune = -1
	if esc != "" {
		escR = []rune(esc)[0]
	}
	// Iterative matching with backtracking on the last '%'.
	si, pi, star, mark := 0, 0, -1, 0
	for si < len(sr) {
		if pi < len(pr) {
			c, literal := pr[pi], false
			if c == escR && pi+1 < len(pr) {
				c, literal = pr[pi+1], true
			}
			switch {
			case !literal && c == '%':
				star, mark = pi, si
				pi++
				continue
			case (!literal && c == '_') || c == sr[si]:
				si++
				if literal {
					pi += 2
				} else {
					pi++
				}
				continue
			}
		}
		if star < 0 {
			return false
		}
		mark++
		si, pi = mark, star+1
	}
	for pi < len(pr) && pr[pi] == '%' && rune('%') != escR {
		pi++
	}
	return pi == len(pr)
}
