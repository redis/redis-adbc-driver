// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"cmp"
	"strconv"
	"strings"
)

func jsonbNumberCompare(a, b string) (int, bool) {
	x, xs, err := parseDecimal(a)
	if err != nil {
		return 0, false
	}
	y, ys, err := parseDecimal(b)
	if err != nil {
		return 0, false
	}
	scale := max(xs, ys)
	return rescaleDecimal(x, xs, scale).Cmp(rescaleDecimal(y, ys, scale)), true
}
func jsonbRank(n *jsonNode, top bool) int {
	if top && n.kind == jsonArray && len(n.items) == 0 {
		return -1
	}
	switch n.kind {
	case jsonNull:
		return 0
	case jsonString:
		return 1
	case jsonNumber:
		return 2
	case jsonBool:
		return 3
	case jsonArray:
		return 4
	case jsonObject:
		return 5
	}
	return -2
}
func compareJSONB(a, b Value) (int, bool) {
	x, err := parseJSON(a.S, true)
	if err != nil {
		return 0, false
	}
	y, err := parseJSON(b.S, true)
	if err != nil {
		return 0, false
	}
	toJSONB(x)
	toJSONB(y)
	var compare func(*jsonNode, *jsonNode, bool) (int, bool)
	compare = func(x, y *jsonNode, top bool) (int, bool) {
		if c := cmp.Compare(jsonbRank(x, top), jsonbRank(y, top)); c != 0 {
			return c, true
		}
		switch x.kind {
		case jsonNull:
			return 0, true
		case jsonBool:
			return strings.Compare(x.raw, y.raw), true
		case jsonNumber:
			return jsonbNumberCompare(x.raw, y.raw)
		case jsonString:
			xs, e := jsonUnescape(x.raw)
			if e != nil {
				return 0, false
			}
			ys, e := jsonUnescape(y.raw)
			return strings.Compare(xs, ys), e == nil
		case jsonArray, jsonObject:
			if c := cmp.Compare(len(x.items), len(y.items)); c != 0 {
				return c, true
			}
			for i := range x.items {
				if x.kind == jsonObject {
					if c := strings.Compare(x.keys[i], y.keys[i]); c != 0 {
						return c, true
					}
				}
				if c, ok := compare(x.items[i], y.items[i], false); !ok || c != 0 {
					return c, ok
				}
			}
			return 0, true
		}
		return 0, false
	}
	return compare(x, y, true)
}

// Keys use decoded strings, canonical object order and scaled-integer numeric
// identity. Thus 1, 1.0 and 1e0 share a key without rounding through float64.
func jsonbEqualityKey(v Value) (string, bool) {
	n, err := parseJSON(v.S, true)
	if err != nil {
		return "", false
	}
	toJSONB(n)
	var b strings.Builder
	text := func(s string) { b.WriteString(strconv.Itoa(len(s))); b.WriteByte(':'); b.WriteString(s) }
	var emit func(*jsonNode) bool
	emit = func(n *jsonNode) bool {
		b.WriteByte(byte('A' + n.kind))
		switch n.kind {
		case jsonObject:
			b.WriteString(strconv.Itoa(len(n.items)))
			b.WriteByte(':')
			for i, x := range n.items {
				text(n.keys[i])
				if !emit(x) {
					return false
				}
			}
		case jsonArray:
			b.WriteString(strconv.Itoa(len(n.items)))
			b.WriteByte(':')
			for _, x := range n.items {
				if !emit(x) {
					return false
				}
			}
		case jsonString:
			s, e := jsonUnescape(n.raw)
			if e != nil {
				return false
			}
			text(s)
		case jsonNumber:
			d, scale, e := parseDecimal(n.raw)
			if e != nil {
				return false
			}
			if d.Sign() == 0 {
				text("0")
				break
			}
			digits := d.String()
			trimmed := strings.TrimRight(digits, "0")
			scale -= int32(len(digits) - len(trimmed))
			text(trimmed + "/" + strconv.FormatInt(int64(scale), 10))
		default:
			text(n.raw)
		}
		return true
	}
	if !emit(n) {
		return "", false
	}
	return "j" + b.String(), true
}
