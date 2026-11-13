// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// scanPredicate binds a small, pure integer/boolean predicate to decoded scalar
// slots. It never fetches data or changes index membership. All required HASH
// columns decode first, including projected fields of rows rejected here.
// Unsupported expressions retain evalEnv's complete row-map path.
type scanPredicate struct {
	fields []string
	values []Value
	root   func() (Value, error)
}

func (p *scanPredicate) matches() (bool, error) {
	v, err := p.root()
	if err != nil {
		return false, err
	}
	b, valid := truthy(v)
	return valid && b, nil
}

func bindScanPredicate(expr Expr, types map[string]ColType, params []Value, zone tzZone) *scanPredicate {
	p := &scanPredicate{}
	slots := map[string]int{}
	nodes := 0
	scalar := func(t ColType) bool { return t.Kind.isInteger() || t.Kind == KindBool || t.Kind == KindNull }
	var bind func(Expr) (func() (Value, error), bool)
	bind = func(expr Expr) (func() (Value, error), bool) {
		nodes++
		// Also keeps large literal IN lists on their established hash-set path.
		if nodes > 32 {
			return nil, false
		}
		switch x := expr.(type) {
		case *ColumnRef:
			typ, ok := types[x.Name]
			if !ok || !scalar(typ) || x.Outer != 0 || x.item != "" || x.outerAgg != nil || x.whole != nil {
				return nil, false
			}
			slot, ok := slots[x.Name]
			if !ok {
				slot = len(p.fields)
				slots[x.Name] = slot
				p.fields = append(p.fields, x.Name)
			}
			return func() (Value, error) { return p.values[slot], nil }, true
		case *Literal:
			if x.Unknown || !scalar(x.V.T) {
				return nil, false
			}
			return func() (Value, error) { return x.V, nil }, true
		case *Param:
			if x.Index < 0 || x.Index >= len(params) || !scalar(params[x.Index].T) {
				return nil, false
			}
			v := params[x.Index]
			return func() (Value, error) { return v, nil }, true
		case *IsNull:
			child, ok := bind(x.X)
			if !ok {
				return nil, false
			}
			return func() (Value, error) {
				v, err := child()
				return boolValue(v.Null != x.Not), err
			}, true
		case *Unary:
			if x.Op != "NOT" {
				return nil, false
			}
			child, ok := bind(x.X)
			if !ok {
				return nil, false
			}
			return func() (Value, error) {
				v, err := child()
				if err != nil {
					return Value{}, err
				}
				b, valid := truthy(v)
				if !valid {
					return nullValue(typeBool), nil
				}
				return boolValue(!b), nil
			}, true
		case *Binary:
			if x.nested || x.Op != "AND" && x.Op != "OR" && !isComparison(x.Op) && !isDistinctOp(x.Op) {
				return nil, false
			}
			left, ok := bind(x.L)
			if !ok {
				return nil, false
			}
			right, ok := bind(x.R)
			if !ok {
				return nil, false
			}
			return func() (Value, error) {
				l, err := left()
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
				r, err := right()
				if err != nil {
					return Value{}, err
				}
				rb, rok := truthy(r)
				switch x.Op {
				case "AND":
					if rok && !rb {
						return boolValue(false), nil
					}
					if lok && rok {
						return boolValue(true), nil
					}
					return nullValue(typeBool), nil
				case "OR":
					if rok && rb {
						return boolValue(true), nil
					}
					if lok && rok {
						return boolValue(false), nil
					}
					return nullValue(typeBool), nil
				default:
					return binaryOp(x.Op, l, r, zone)
				}
			}, true
		}
		return nil, false
	}
	var ok bool
	p.root, ok = bind(expr)
	if !ok || len(p.fields) == 0 {
		return nil
	}
	p.values = make([]Value, len(p.fields))
	return p
}
