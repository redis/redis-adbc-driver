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

import "math"

// integerArithmetic is shared by scalar and compiled aggregate evaluation.
// Keep the same checked operations and per-row error behavior in both paths.
func integerArithmetic(op byte, a, b int64) (int64, error) {
	switch op {
	case '+':
		s := a + b
		if (s > a) != (b > 0) {
			return 0, stateErr(stateNumericRange, "integer overflow")
		}
		return s, nil
	case '-':
		s := a - b
		if (s < a) != (b > 0) {
			return 0, stateErr(stateNumericRange, "integer overflow")
		}
		return s, nil
	case '*':
		if a != 0 && b != 0 {
			p := a * b
			if p/b != a || (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
				return 0, stateErr(stateNumericRange, "integer overflow")
			}
			return p, nil
		}
		return 0, nil
	case '/':
		if b == 0 {
			return 0, stateErr(stateDivisionByZero, "division by zero")
		}
		return a / b, nil
	}
	return 0, stateErr(stateUndefinedFunction, "unsupported integer operator %c", op)
}

type integerInputKey struct {
	kind        byte
	column      string
	left, right int
	literal     int64
	null        bool
	typ         Kind
}
type integerInputNode struct {
	key integerInputKey
	typ ColType
}
type integerInputValue struct {
	n          int64
	null       bool
	generation uint64
}

// aggregateInputs is a lazy, per-row DAG for pure, typed integer inputs.
// Filters, DISTINCT, extended aggregates and every unsupported expression
// keep addRow. Nothing is evaluated before its original aggregate position;
// an error stops evaluation before later aggregates, as in the scalar path.
type aggregateInputs struct {
	roots      []int
	nodes      []integerInputNode
	values     []integerInputValue
	generation uint64
	intern     map[integerInputKey]int
}

func compileAggregateInputs(aggs []*Func, types map[string]ColType) *aggregateInputs {
	p := &aggregateInputs{roots: make([]int, len(aggs)), intern: map[integerInputKey]int{}}
	for i, f := range aggs {
		p.roots[i] = -1
		if f.Filter != nil || f.Distinct || f.Star || len(f.Args) != 1 {
			continue
		}
		switch f.Name {
		case "SUM", "AVG", "MIN", "MAX", "COUNT":
		default:
			continue
		}
		if root, ok := p.compile(f.Args[0], types); ok {
			p.roots[i] = root
		}
	}
	p.values = make([]integerInputValue, len(p.nodes))
	p.intern = nil
	return p
}
func (p *aggregateInputs) compile(x Expr, types map[string]ColType) (int, bool) {
	var key integerInputKey
	var typ ColType
	switch x := x.(type) {
	case *ColumnRef:
		if x.Outer != 0 {
			return -1, false
		}
		var ok bool
		typ, ok = types[x.Name]
		if !ok || !typ.Kind.isInteger() {
			return -1, false
		}
		key = integerInputKey{kind: 'c', column: x.Name, typ: typ.Kind}
	case *Literal:
		if x.Unknown || !x.V.T.Kind.isInteger() {
			return -1, false
		}
		typ = x.V.T
		key = integerInputKey{kind: 'l', literal: x.V.I, null: x.V.Null, typ: typ.Kind}
	case *Binary:
		if x.Op != "+" && x.Op != "-" && x.Op != "*" {
			return -1, false
		}
		l, ok := p.compile(x.L, types)
		if !ok {
			return -1, false
		}
		r, ok := p.compile(x.R, types)
		if !ok {
			return -1, false
		}
		typ = typeInt64
		key = integerInputKey{kind: x.Op[0], left: l, right: r, typ: KindInt64}
	default:
		return -1, false
	}
	if id, ok := p.intern[key]; ok {
		return id, true
	}
	id := len(p.nodes)
	p.intern[key] = id
	p.nodes = append(p.nodes, integerInputNode{key: key, typ: typ})
	return id, true
}
func (p *aggregateInputs) nextRow() {
	p.generation++
	if p.generation == 0 {
		clear(p.values)
		p.generation = 1
	}
}
func (p *aggregateInputs) value(id int, row map[string]Value) (Value, error) {
	v, err := p.eval(id, row)
	if err != nil {
		return Value{}, err
	}
	typ := p.nodes[id].typ
	if v.null {
		return nullValue(typ), nil
	}
	return intValue(typ, v.n), nil
}
func (p *aggregateInputs) eval(id int, row map[string]Value) (integerInputValue, error) {
	if v := p.values[id]; v.generation == p.generation {
		return v, nil
	}
	node := p.nodes[id]
	key := node.key
	var v integerInputValue
	switch key.kind {
	case 'c':
		x, ok := row[key.column]
		v.n, v.null = x.I, !ok || x.Null
	case 'l':
		v.n, v.null = key.literal, key.null
	default:
		l, err := p.eval(key.left, row)
		if err != nil {
			return v, err
		}
		r, err := p.eval(key.right, row)
		if err != nil {
			return v, err
		}
		v.null = l.null || r.null
		if !v.null {
			v.n, err = integerArithmetic(key.kind, l.n, r.n)
			if err != nil {
				return v, err
			}
		}
	}
	v.generation = p.generation
	p.values[id] = v
	return v, nil
}
