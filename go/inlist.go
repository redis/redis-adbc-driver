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

// Literal IN lists.
//
// `x IN (v1, …, vN)` is parsed as `x = v1 OR … OR x = vN`, all sharing one x
// node, and a row list `(x, y) IN ((v1, w1), …)` as `(x = v1 AND y = w1) OR
// …`, sharing the x and y nodes (see compareExpr). Pushdown turns that into
// an index union when it can (up to maxUnionTerms values on an indexed
// column). When the driver evaluates it, each row used to compare x with
// every value. A long list of constants now probes a hash set instead, built
// once per statement: the inSet of IN subqueries, or for a row list the
// rowSet of row IN subqueries, which has the chain's NULL rules.
//
// The hash set only answers rows whose items can't fail to compare with any
// of the values in their position: `=` raises an error for types it can't
// compare, and the chain stops at the first match (and each AND at the first
// unequal item), so a row whose types might hit an error is evaluated by the
// chain itself, exactly as before. So is a row whose second or later item
// fails to evaluate: the chain evaluates it only where the items before it
// can match, as Postgres does.

// inListMin is the list length from which a hash set pays off.
const inListMin = 16

type inList struct {
	// xs are the items compared: one for a scalar list.
	xs []Expr
	// set holds a scalar list's values, rows a row list's.
	set  *inSet
	rows *rowSet
	// types are, per item, the distinct types of the non-NULL values in its
	// position; safe caches, per item and by the item's type, whether it
	// compares with all of them without an error.
	types [][]ColType
	safe  []map[ColType]bool
}

// literalInList recognizes the OR chain of a literal IN list with at least
// inListMin rows of constants, returning the items compared and each row's
// values (in list order).
func literalInList(or *Binary) ([]Expr, [][]Expr, bool) {
	var rev []Expr
	var node Expr = or
	for {
		b, ok := node.(*Binary)
		if !ok || b.Op != "OR" {
			rev = append(rev, node)
			break
		}
		rev = append(rev, b.R)
		node = b.L
	}
	if len(rev) < inListMin {
		return nil, nil, false
	}
	var xs []Expr
	values := make([][]Expr, len(rev))
	for i, leaf := range rev {
		eqs := conjuncts(leaf, nil)
		if i == 0 {
			xs = make([]Expr, len(eqs))
		} else if len(eqs) != len(xs) {
			return nil, nil, false
		}
		row := make([]Expr, len(eqs))
		for k, c := range eqs {
			eq, ok := c.(*Binary)
			if !ok || eq.Op != "=" || !isConstant(eq.R) {
				return nil, nil, false
			}
			if i == 0 {
				xs[k] = eq.L
			} else if eq.L != xs[k] {
				return nil, nil, false // not one IN list
			}
			row[k] = eq.R
		}
		values[len(rev)-1-i] = row
	}
	for _, x := range xs {
		if hasVolatile(x) || hasSubquery(x) {
			return nil, nil, false // x is evaluated once here, once per row before
		}
	}
	return xs, values, true
}

// evalInList evaluates an IN list's OR chain with its hash set; ok is false
// when the chain must be evaluated as written.
func (env *evalEnv) evalInList(or *Binary) (Value, bool, error) {
	e := env.exec
	if e == nil {
		return Value{}, false, nil
	}
	e.ensureCache()
	l, seen := e.cache.inLists[or]
	if !seen {
		l = env.buildInList(or)
		e.cache.inLists[or] = l
	}
	if l == nil {
		return Value{}, false, nil
	}
	// The chain evaluates the first item first too.
	x, err := env.eval(l.xs[0])
	if err != nil {
		return Value{}, true, err
	}
	if !x.Null && !l.safeFor(0, x.T) {
		return Value{}, false, nil
	}
	if l.set != nil {
		subqueryStats.inLists.Add(1)
		return l.set.eval(x, false), true, nil
	}
	xs := make([]Value, len(l.xs))
	xs[0] = x
	for i := 1; i < len(xs); i++ {
		if xs[i], err = env.eval(l.xs[i]); err != nil {
			return Value{}, false, nil // the chain may not evaluate it
		}
		if !xs[i].Null && !l.safeFor(i, xs[i].T) {
			return Value{}, false, nil
		}
	}
	subqueryStats.inLists.Add(1)
	return l.rows.eval(xs, false), true, nil
}

// buildInList evaluates the list's values once. It returns nil (evaluate
// the chain) if or isn't such a list or a value fails to evaluate.
func (env *evalEnv) buildInList(or *Binary) *inList {
	xs, exprs, ok := literalInList(or)
	if !ok {
		// Don't look at the chain's inner OR nodes again either.
		for node, _ := or.L.(*Binary); node != nil && node.Op == "OR"; node, _ = node.L.(*Binary) {
			env.exec.cache.inLists[node] = nil
		}
		return nil
	}
	n := len(xs)
	l := &inList{xs: xs, types: make([][]ColType, n), safe: make([]map[ColType]bool, n)}
	seen := make([]map[ColType]bool, n)
	for k := range xs {
		l.safe[k], seen[k] = map[ColType]bool{}, map[ColType]bool{}
	}
	rows := make([][]Value, len(exprs))
	for i, row := range exprs {
		rows[i] = make([]Value, n)
		for k, v := range row {
			val, err := env.eval(v)
			if err != nil {
				return nil
			}
			rows[i][k] = val
			if !val.Null && !seen[k][val.T] {
				seen[k][val.T] = true
				l.types[k] = append(l.types[k], val.T)
			}
		}
	}
	if n == 1 {
		l.set = &inSet{rows: rows, zone: env.zone()}
	} else {
		l.rows = &rowSet{rows: rows, n: n, zone: env.zone()}
	}
	// The chain's inner OR nodes are only reached through this one.
	for node, _ := or.L.(*Binary); node != nil && node.Op == "OR"; node, _ = node.L.(*Binary) {
		env.exec.cache.inLists[node] = nil
	}
	return l
}

// safeFor reports whether a value of type t as item i compares with every
// value in that position without an error.
func (l *inList) safeFor(i int, t ColType) bool {
	if ok, seen := l.safe[i][t]; seen {
		return ok
	}
	ok := true
	for _, vt := range l.types[i] {
		if !compareNeverFails(t, vt) {
			ok = false
			break
		}
	}
	l.safe[i][t] = ok
	return ok
}

// compareNeverFails reports whether compareValues succeeds for every pair of
// non-NULL values of types a and b (string-to-number conversions, mixed
// time units and date/timestamp mixes can fail, so they are excluded).
func compareNeverFails(a, b ColType) bool {
	ak, bk := a.Kind, b.Kind
	intOrBool := func(k Kind) bool { return k.isInteger() || k == KindBool }
	switch {
	case ak == KindInterval && bk == KindInterval:
		return true
	case (ak == KindString || ak == KindBinary) && (bk == KindString || bk == KindBinary):
		return true
	case intOrBool(ak) && intOrBool(bk):
		return true
	case ak == KindDecimal && (bk.isNumeric() || bk == KindBool), bk == KindDecimal && (ak.isNumeric() || ak == KindBool):
		return true
	case ak.isNumeric() && bk.isNumeric():
		return true
	case ak == KindDate && bk == KindDate:
		return true
	case (ak == KindTime || ak == KindTimestamp) && ak == bk && a.Unit == b.Unit && a.TZ == b.TZ:
		return true
	case ak == KindTimeTZ && bk == KindTimeTZ:
		return true
	}
	return false
}
