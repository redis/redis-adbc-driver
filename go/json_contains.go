// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// Containment walks parsed values and compares numbers exactly. Array
// membership applies only at the top level, without flattening nested arrays.
func jsonbContainment(ctx context.Context, op string, l, r Value) (Value, error) {
	if op == "&&" {
		return Value{}, stateErr(stateUndefinedFunction, "operator does not exist: jsonb && jsonb")
	}
	if l.Null || r.Null {
		return nullValue(typeBool), nil
	}
	if l.T.Kind == KindJSON || r.T.Kind == KindJSON {
		return Value{}, stateErr(stateUndefinedFunction, "containment requires jsonb")
	}
	a, err := parseJSON(l.Text(), true)
	if err != nil {
		return Value{}, err
	}
	b, err := parseJSON(r.Text(), true)
	if err != nil {
		return Value{}, err
	}
	toJSONB(a)
	toJSONB(b)
	if op == "<@" {
		a, b = b, a
	}
	var contains func(*jsonNode, *jsonNode, bool) (bool, error)
	contains = func(a, b *jsonNode, top bool) (bool, error) {
		if err := cancelled(ctx); err != nil {
			return false, err
		}
		if a.kind == jsonArray && b.kind != jsonArray && top {
			for _, x := range a.items {
				yes, err := contains(x, b, false)
				if err != nil || yes {
					return yes, err
				}
			}
			return false, nil
		}
		if a.kind != b.kind {
			return false, nil
		}
		switch a.kind {
		case jsonObject:
			for i, key := range b.keys {
				x := a.member(key)
				if x == nil {
					return false, nil
				}
				yes, err := contains(x, b.items[i], false)
				if err != nil || !yes {
					return yes, err
				}
			}
			return true, nil
		case jsonArray:
			for _, y := range b.items {
				found := false
				for _, x := range a.items {
					yes, err := contains(x, y, false)
					if err != nil {
						return false, err
					}
					if yes {
						found = true
						break
					}
				}
				if !found {
					return false, nil
				}
			}
			return true, nil
		case jsonString:
			x, err := jsonUnescape(a.raw)
			if err != nil {
				return false, err
			}
			y, err := jsonUnescape(b.raw)
			return x == y, err
		case jsonNumber:
			x, xs, err := parseDecimal(a.raw)
			if err != nil {
				return false, err
			}
			y, ys, err := parseDecimal(b.raw)
			if err != nil {
				return false, err
			}
			scale := max(xs, ys)
			return rescaleDecimal(x, xs, scale).Cmp(rescaleDecimal(y, ys, scale)) == 0, nil
		default:
			return a.raw == b.raw, nil
		}
	}
	yes, err := contains(a, b, true)
	return boolValue(yes), err
}
