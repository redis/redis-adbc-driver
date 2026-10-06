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

// Quoted string literals without a type.
//
// In Postgres a quoted string literal ('1') has no type of its own (it is
// of type unknown) until it meets a value that has one. Among the values
// that an expression's value is one of, it takes the type of the others
// (select_common_type): the results of a CASE (and the driver's IIF), the
// arguments of COALESCE, GREATEST and LEAST, the default of LAG and LEAD,
// and a column of the branches of UNION, INTERSECT and EXCEPT. So CASE WHEN
// … THEN '1' ELSE 1 END is an integer, and its '1' the integer 1, while
// CASE WHEN … THEN 'x' ELSE 1 END is an error, even where no row reads it.
// Literals are text if all the values are. NULLIF reads one as the type of
// its other argument, as = does.
//
// The parser marks these literals (Literal.Unknown); a cast of one is
// typed. inferType gives such an expression its type (commonExprType), and
// evaluation converts the literal's text to it (literalAs), so that the
// expression's values are all of its type: sorting, grouping, arithmetic
// and the index see integers, not the text '1' among them. Typed values of
// types that have no common type, such as an INTEGER and a VARCHAR, are an
// error, as in Postgres ("CASE types bigint and varchar cannot be
// matched"), which checkUnknowns reports when the expression is bound, as
// it does literals that don't convert.
//
// Comparisons already read text as the other side's type (textOperand),
// and INSERT, UPDATE and CAST convert it to theirs.

import (
	"errors"
	"slices"
)

// unknownLiteral reports whether e is a quoted string literal, which has no
// type of its own (see Literal).
func unknownLiteral(e Expr) bool {
	l, ok := e.(*Literal)
	return ok && l.Unknown && !l.V.Null
}

// typeMatchError is the error for values of a CASE, COALESCE, GREATEST or
// LEAST whose types have no common type, Postgres's datatype_mismatch.
type typeMatchError struct {
	what string
	a, b ColType
}

func (e *typeMatchError) Error() string {
	return e.what + " types " + sigTypeName(e.a) + " and " + sigTypeName(e.b) + " cannot be matched"
}

// Unwrap gives the error its SQLSTATE (sqlState).
func (e *typeMatchError) Unwrap() error {
	return &sqlError{msg: e.Error(), state: stateDatatypeMismatch}
}

// commonExprType is the type of a value that is one of exprs, of types ts:
// the results of a CASE or IIF, or the arguments of COALESCE, GREATEST and
// LEAST, which what names in the error for types that don't match. As in
// Postgres (select_common_type), the quoted string literals among them
// ('1', unknownLiteral) take the type of the others, without its length or
// fractional digits, so that a CASE of '1' and an INTEGER is an INTEGER,
// and its '1' the integer 1 (literalAs); they are text if all are. NULLs
// take any type. With numLits, a numeric literal that the others' type
// holds exactly takes it too: COALESCE(int_col, 0) is an INTEGER, as in
// Postgres, whose 0 is an INTEGER where the driver's is a BIGINT.
func commonExprType(what string, exprs []Expr, ts []ColType, numLits bool) (ColType, error) {
	unknown := func(i int) bool { return i < len(exprs) && unknownLiteral(exprs[i]) }
	numLit := func(i int) (Value, bool) {
		if i < len(exprs) && numLits {
			if l, ok := exprs[i].(*Literal); ok && !l.V.Null && l.V.T.Kind.isNumeric() {
				return l.V, true
			}
		}
		return Value{}, false
	}
	out := typeNull
	join := func(t ColType) error {
		c, ok := matchTypes(out, t)
		if !ok {
			return &typeMatchError{what: what, a: out, b: t}
		}
		out = c
		return nil
	}
	for i, t := range ts {
		if _, ok := numLit(i); !ok && !unknown(i) {
			if err := join(t); err != nil {
				return ColType{}, err
			}
		}
	}
	for i, t := range ts {
		v, ok := numLit(i)
		if !ok {
			continue
		}
		if out.Kind.isNumeric() {
			if c, err := Coerce(v, out); err == nil {
				if cmp, ok := compareValues(c, v); ok && cmp == 0 {
					continue
				}
			}
		}
		if err := join(t); err != nil {
			return ColType{}, err
		}
	}
	if out.Kind == KindNull {
		if slices.ContainsFunc(exprs[:min(len(exprs), len(ts))], unknownLiteral) {
			return typeString, nil
		}
		return out, nil
	}
	for i := range ts {
		if unknown(i) {
			out = literalType(exprs[i], out)
		}
	}
	return out, nil
}

// literalType is the type that the quoted string literal lit takes next to
// values of type t: t without its length or fractional digits, as in
// Postgres, and for a NUMERIC with the digits the literal's number needs, so
// that it isn't rounded: a NUMERIC(10,2) and '2.567' are a NUMERIC(38,3)
// (Postgres's NUMERIC has no scale).
func literalType(lit Expr, t ColType) ColType {
	t = t.withoutTypmod()
	if t.Kind != KindDecimal {
		return t
	}
	v, err := parseStringIn(lit.(*Literal).V.S, t, utcZone)
	if err != nil || v.Null || (v.T.Kind != KindDecimal && !v.T.Kind.isInteger()) {
		return t
	}
	if c, err := Coerce(v, t); err == nil {
		if cmp, ok := compareValues(c, v); ok && cmp == 0 {
			return t
		}
	}
	if w, ok := matchTypes(t, v.T); ok {
		return w
	}
	return t
}

// exprsType is commonExprType of exprs, whose types it infers.
func exprsType(what string, exprs []Expr, cols map[string]ColType, params []ColType, numLits bool) (ColType, error) {
	ts := make([]ColType, len(exprs))
	for i, x := range exprs {
		t, err := inferType(x, cols, params)
		if err != nil {
			return ColType{}, err
		}
		ts[i] = t
	}
	return commonExprType(what, exprs, ts, numLits)
}

// matchedParts returns the values that the value of x is one of, which
// commonExprType matches (with numLits): a CASE's results, and the
// arguments of COALESCE, GREATEST and LEAST, and IIF's results; nil for
// other expressions.
func matchedParts(x Expr) (parts []Expr, numLits bool) {
	switch v := x.(type) {
	case *Case:
		return caseResults(v), false
	case *Func:
		d, ok := callee(v)
		switch {
		case !ok:
		case d.impl == implCoalesce || (d.impl == implConditional && v.Name != "NULLIF"):
			return v.Args, true
		case d.impl == implIIF && len(v.Args) == 3:
			return v.Args[1:], false
		}
	}
	return nil, false
}

// literalParts returns the quoted string literals that take x's type: those
// of matchedParts, NULLIF's arguments, and LAG's and LEAD's default.
func literalParts(x Expr) []Expr {
	var out []Expr
	if parts, _ := matchedParts(x); parts != nil {
		for _, p := range parts {
			if unknownLiteral(p) {
				out = append(out, p)
			}
		}
		return out
	}
	switch v := x.(type) {
	case *Func:
		if v.Name == "NULLIF" {
			for _, a := range v.Args {
				if unknownLiteral(a) {
					out = append(out, a)
				}
			}
		}
	case *WindowFunc:
		if f := v.Func; (f.Name == "LAG" || f.Name == "LEAD") && len(f.Args) == 3 && unknownLiteral(f.Args[2]) {
			out = append(out, f.Args[2])
		}
	}
	return out
}

// checkLiteral returns the error, if any, of reading the quoted string
// literal lit as a value of type t, as Postgres reports when it parses it:
// CASE WHEN … THEN 'x' ELSE 1 END is an error, even if no row reads 'x'.
// (The value is read again in the session time zone when it is used.)
func checkLiteral(lit Expr, t ColType) error {
	l := lit.(*Literal)
	if t.Kind == KindNull || t.Kind == KindString {
		return nil
	}
	_, err := Coerce(l.V, t)
	return err
}

// checkUnknowns reports the error of an expression of expr (outside its
// subqueries) whose values' types have no common type, or one of whose
// quoted string literals isn't a value of the type it takes, as Postgres
// does when it parses the statement. cols gives the types of the columns
// it reads (called once, if it has such an expression).
func checkUnknowns(expr Expr, cols func() map[string]ColType, params []ColType) error {
	var types map[string]ColType
	var err error
	walkExprPruned(expr, func(x Expr) bool {
		if err != nil {
			return false
		}
		parts, _ := matchedParts(x)
		lits := literalParts(x)
		if parts == nil && lits == nil {
			return true
		}
		if types == nil {
			types = cols()
		}
		t, terr := inferType(x, types, params)
		var me *typeMatchError
		switch {
		case errors.As(terr, &me):
			err = terr
		case terr == nil:
			for _, l := range lits {
				if err = checkLiteral(l, t); err != nil {
					break
				}
			}
		}
		return err == nil
	})
	return err
}
