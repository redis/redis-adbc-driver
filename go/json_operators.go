// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strconv"
	"strings"
)

func jsonbValue(n *jsonNode) (Value, error) {
	toJSONB(n)
	var b strings.Builder
	if err := writeJSONB(&b, n); err != nil {
		return Value{}, err
	}
	return Value{T: typeJSONB, S: b.String()}, nil
}
func jsonbDelete(ctx context.Context, doc, key Value) (Value, error) {
	if doc.Null || key.Null {
		return nullValue(typeJSONB), nil
	}
	n, err := parseJSON(doc.S, true)
	if err != nil {
		return Value{}, err
	}
	toJSONB(n)
	if n.kind != jsonObject && n.kind != jsonArray {
		return Value{}, stateErr(stateInvalidParameter, "cannot delete from scalar")
	}
	if key.T.Kind.isInteger() {
		if n.kind != jsonArray {
			return Value{}, stateErr(stateInvalidParameter, "cannot delete from object using integer index")
		}
		i := key.I
		if i < 0 {
			i += int64(len(n.items))
		}
		if i >= 0 && i < int64(len(n.items)) {
			n.items = append(n.items[:i], n.items[i+1:]...)
		}
	} else if key.T.Kind == KindString {
		for i := len(n.items) - 1; i >= 0; i-- {
			if i%cancelEvery == 0 {
				if err := cancelled(ctx); err != nil {
					return Value{}, err
				}
			}
			match := n.kind == jsonObject && n.keys[i] == key.S
			if n.kind == jsonArray && n.items[i].kind == jsonString {
				s, e := jsonUnescape(n.items[i].raw)
				if e != nil {
					return Value{}, e
				}
				match = s == key.S
			}
			if match {
				n.items = append(n.items[:i], n.items[i+1:]...)
				if n.kind == jsonObject {
					n.keys = append(n.keys[:i], n.keys[i+1:]...)
				}
			}
		}
	} else {
		return Value{}, stateErr(stateUndefinedFunction, "operator does not exist: jsonb - %s", sigTypeName(key.T))
	}
	return jsonbValue(n)
}
func jsonExists(n *jsonNode, key string) bool {
	if n.kind == jsonObject {
		return n.member(key) != nil
	}
	if n.kind == jsonString {
		s, err := jsonUnescape(n.raw)
		return err == nil && s == key
	}
	if n.kind == jsonArray {
		for _, v := range n.items {
			if v.kind == jsonString {
				s, err := jsonUnescape(v.raw)
				if err == nil && s == key {
					return true
				}
			}
		}
	}
	return false
}
func jsonPathParts(v Value) ([]string, error) {
	if v.T.Kind == KindArray {
		if v.T.element().Kind != KindString {
			return nil, stateErr(stateDatatypeMismatch, "JSON path requires text[]")
		}
		out := make([]string, len(v.fields()))
		for i, x := range v.fields() {
			if x.Null {
				return nil, stateErr(stateNullNotAllowed, "path element at position %d is null", i+1)
			}
			out[i] = x.Text()
		}
		return out, nil
	}
	steps, ok, err := jsonSteps(&Func{Name: "#>"}, []Value{stringValue("{}"), v})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, stateErr(stateNullNotAllowed, "path must not contain null elements")
	}
	out := make([]string, len(steps))
	for i, x := range steps {
		out[i] = x.key
	}
	return out, nil
}
func (env *evalEnv) evalJSONMutation(f *Func, a []Value) (Value, error) {
	n, err := parseJSON(a[0].Text(), true)
	if err != nil {
		return Value{}, err
	}
	toJSONB(n)
	switch f.Name {
	case "?":
		return boolValue(jsonExists(n, a[1].Text())), nil
	case "?|", "?&":
		keysArray, err := coerceIn(a[1], arrayOf(typeString), env.zone())
		if err != nil {
			return Value{}, err
		}
		keys := keysArray.fields()
		all := f.Name == "?&"
		for _, k := range keys {
			if k.Null {
				continue
			}
			found := jsonExists(n, k.Text())
			if found != all {
				return boolValue(found), nil
			}
		}
		return boolValue(all), nil
	}
	path, err := jsonPathParts(a[1])
	if err != nil {
		return Value{}, err
	}
	if n.kind != jsonObject && n.kind != jsonArray {
		return Value{}, stateErr(stateInvalidParameter, "cannot set or delete path in scalar")
	}
	if len(path) == 0 {
		return jsonbValue(n)
	}
	var replacement *jsonNode
	create := true
	if f.Name == "JSONB_SET" {
		replacement, err = parseJSON(a[2].Text(), true)
		if err != nil {
			return Value{}, err
		}
		toJSONB(replacement)
		if len(a) == 4 {
			flag, e := coerceIn(a[3], typeBool, env.zone())
			if e != nil {
				return Value{}, e
			}
			create = flag.I != 0
		}
	}
	var mutate func(*jsonNode, int) error
	mutate = func(node *jsonNode, depth int) error {
		if err := cancelled(env.ctx); err != nil {
			return err
		}
		key, last := path[depth], depth == len(path)-1
		switch node.kind {
		case jsonObject:
			i := -1
			for j, k := range node.keys {
				if k == key {
					i = j
					break
				}
			}
			if !last {
				if i >= 0 {
					return mutate(node.items[i], depth+1)
				}
				return nil
			}
			if f.Name == "#-" {
				if i >= 0 {
					node.keys = append(node.keys[:i], node.keys[i+1:]...)
					node.items = append(node.items[:i], node.items[i+1:]...)
				}
			} else if i >= 0 {
				node.items[i] = replacement
			} else if create {
				node.keys = append(node.keys, key)
				node.items = append(node.items, replacement)
			}
		case jsonArray:
			raw, e := strconv.ParseInt(strings.TrimLeft(key, " \t\r\n"), 10, 32)
			if e != nil {
				return stateErr(stateInvalidText, "path element at position %d is not an integer: %q", depth+1, key)
			}
			i := raw
			if i < 0 {
				i += int64(len(node.items))
			}
			if i >= 0 && i < int64(len(node.items)) {
				if !last {
					return mutate(node.items[i], depth+1)
				}
				if f.Name == "#-" {
					node.items = append(node.items[:i], node.items[i+1:]...)
				} else {
					node.items[i] = replacement
				}
			} else if last && create && f.Name == "JSONB_SET" {
				if raw < 0 {
					node.items = append([]*jsonNode{replacement}, node.items...)
				} else {
					node.items = append(node.items, replacement)
				}
			}
		}
		return nil
	}
	if err = mutate(n, 0); err != nil {
		return Value{}, err
	}
	return jsonbValue(n)
}

func jsonScalarText(n *jsonNode) (Value, error) {
	if n.kind == jsonNull {
		return nullValue(typeString), nil
	}
	if n.kind == jsonString {
		s, err := jsonUnescape(n.raw)
		return stringValue(s), err
	}
	if n.raw != "" {
		return stringValue(n.raw), nil
	}
	v, err := jsonbValue(n)
	if err != nil {
		return Value{}, err
	}
	return stringValue(v.S), nil
}
