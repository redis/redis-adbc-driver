// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// PostgreSQL's one-dimensional text array representation. Explicit lower
// bounds and nested arrays are rejected rather than silently losing shape.
func parseArrayText(s string, t ColType, z tzZone) (Value, error) {
	bad := func() (Value, error) { return Value{}, stateErrorf(stateInvalidText, "malformed array literal: %q", s) }
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return bad()
	}
	body := s[1 : len(s)-1]
	var out []Value
	if strings.TrimSpace(body) == "" {
		return arrayValue(out, t.element()), nil
	}
	for i := 0; i < len(body); {
		for i < len(body) && unicode.IsSpace(rune(body[i])) {
			i++
		}
		if i == len(body) {
			return bad()
		}
		quoted := body[i] == '"'
		var b strings.Builder
		var protected []bool
		escaped := false
		if quoted {
			i++
		}
		closed := !quoted
		for i < len(body) {
			c := body[i]
			i++
			if c == '\\' {
				if i == len(body) {
					return bad()
				}
				b.WriteByte(body[i])
				protected = append(protected, true)
				i++
				escaped = true
				continue
			}
			if quoted {
				if c == '"' {
					closed = true
					break
				}
			} else {
				if c == ',' {
					i--
					break
				}
				if c == '{' || c == '}' || c == '"' {
					return bad()
				}
			}
			b.WriteByte(c)
			protected = append(protected, false)
		}
		if !closed {
			return bad()
		}
		vstr := b.String()
		if !quoted {
			lo, hi := 0, len(vstr)
			for lo < hi && !protected[lo] && strings.ContainsRune(" \t\r\n\v\f", rune(vstr[lo])) {
				lo++
			}
			for hi > lo && !protected[hi-1] && strings.ContainsRune(" \t\r\n\v\f", rune(vstr[hi-1])) {
				hi--
			}
			vstr = vstr[lo:hi]
			if vstr == "" {
				return bad()
			}
		}
		v := nullValue(t.element())
		var err error
		if quoted || escaped || !strings.EqualFold(vstr, "NULL") {
			if t.element().Kind == KindBinary && strings.HasPrefix(vstr, `\x`) {
				var data []byte
				data, err = hex.DecodeString(vstr[2:])
				v = binaryValue(string(data))
			} else {
				v, err = coerceIn(stringValue(vstr), t.element(), z)
			}
			if err != nil {
				return Value{}, err
			}
		}
		out = append(out, v)
		for i < len(body) && unicode.IsSpace(rune(body[i])) {
			i++
		}
		if i == len(body) {
			break
		}
		if body[i] != ',' {
			return bad()
		}
		i++
		if i == len(body) {
			return bad()
		}
	}
	return arrayValue(out, t.element()), nil
}
func extraArrayType(f *Func, a []ColType) (ColType, bool) {
	switch f.Name {
	case "__ARRAY_QUERY":
		if len(a) > 0 {
			return arrayOf(a[0]), true
		}
	case "__ARRAY_SUBSCRIPT":
		if len(a) > 0 {
			if l, ok := f.Args[3].(*Literal); ok && l.V.I != 0 {
				return a[0], true
			}
			return a[0].element(), true
		}
	case "STRING_TO_ARRAY", "REGEXP_SPLIT_TO_ARRAY":
		return arrayOf(typeString), true
	case "ARRAY_TO_STRING", "ARRAY_DIMS", "STRING_TO_TABLE", "REGEXP_SPLIT_TO_TABLE":
		return typeString, true
	case "REGEXP_MATCHES":
		return arrayOf(typeString), true
	case "ARRAY_LOWER", "ARRAY_UPPER", "ARRAY_POSITION":
		return typeInt32, true
	case "ARRAY_POSITIONS":
		return arrayOf(typeInt32), true
	case "ARRAY_APPEND":
		if len(a) > 1 {
			return arrayOf(commonType(a[0].element(), a[1])), true
		}
	case "ARRAY_PREPEND":
		if len(a) > 1 {
			return arrayOf(commonType(a[1].element(), a[0])), true
		}
	case "ARRAY_CAT":
		if len(a) > 1 {
			return commonType(a[0], a[1]), true
		}
	case "ARRAY_REMOVE", "ARRAY_REPLACE":
		if len(a) > 0 {
			return a[0], true
		}
	case "GENERATE_SERIES":
		t, err := seriesType(a)
		if err == nil {
			return t, true
		}
	case "@>", "<@", "&&":
		return typeBool, true
	}
	return ColType{}, false
}
func concatArrays(l, r Value, z tzZone) (Value, error) {
	t := l.T
	if t.Kind != KindArray {
		t = r.T
	}
	if l.T.Kind == KindArray && r.T.Kind == KindArray {
		t = commonType(l.T, r.T)
	} else {
		v := l
		if l.T.Kind == KindArray {
			v = r
		}
		t = arrayOf(commonType(t.element(), v.T))
	}
	var vals []Value
	for _, v := range []Value{l, r} {
		if v.T.Kind == KindArray {
			if !v.Null {
				vals = append(vals, v.fields()...)
			}
		} else {
			vals = append(vals, v)
		}
	}
	return coerceArrayIn(arrayValue(vals, t.element()), t, z)
}
func (env *evalEnv) evalExtraArray(f *Func) (Value, bool, error) {
	if f.Name == "__ARRAY_QUERY" {
		sq := f.Args[0].(*Subquery)
		rows, err := env.exec.subqueryRows(env.ctx, sq, env)
		if err != nil {
			return Value{}, true, err
		}
		var vals []Value
		for _, r := range rows {
			vals = append(vals, r[0])
		}
		t := sq.plan.columns()[0].Type
		return arrayValue(vals, t), true, nil
	}
	if isSRF(f.Name) && f.Name != "UNNEST" && f.Name != "GENERATE_SUBSCRIPTS" {
		rows, types, err := srfRows(env, f)
		var vals []Value
		for _, r := range rows {
			vals = append(vals, r[0])
		}
		t := typeNull
		if len(types) > 0 {
			t = types[0]
		}
		return arrayValue(vals, t), true, err
	}
	switch f.Name {
	case "__ARRAY_SUBSCRIPT", "STRING_TO_ARRAY", "REGEXP_SPLIT_TO_ARRAY", "ARRAY_TO_STRING", "ARRAY_APPEND", "ARRAY_PREPEND", "ARRAY_CAT", "ARRAY_REMOVE", "ARRAY_REPLACE", "ARRAY_POSITION", "ARRAY_POSITIONS", "ARRAY_UPPER", "ARRAY_LOWER", "ARRAY_DIMS", "@>", "<@", "&&":
	default:
		return Value{}, false, nil
	}
	a, err := env.evalItems(f.Args)
	if err != nil {
		return Value{}, true, err
	}
	types := make([]ColType, len(a))
	for i := range a {
		types[i] = a[i].T
	}
	t, _ := extraArrayType(f, types)
	ret := func(v Value) (Value, bool, error) { return v, true, nil }
	switch f.Name {
	case "STRING_TO_ARRAY":
		if a[0].Null {
			return ret(nullValue(t))
		}
		var parts []string
		if a[0].S != "" {
			if a[1].Null {
				for _, c := range a[0].S {
					parts = append(parts, string(c))
				}
			} else if a[1].S == "" {
				parts = []string{a[0].S}
			} else {
				parts = strings.Split(a[0].S, a[1].S)
			}
		}
		vals := make([]Value, len(parts))
		for i, s := range parts {
			vals[i] = stringValue(s)
			if len(a) > 2 && !a[2].Null && s == a[2].S {
				vals[i] = nullValue(typeString)
			}
		}
		return ret(arrayValue(vals, typeString))
	case "REGEXP_SPLIT_TO_ARRAY":
		rows, _, err := splitSRF(env, f, a)
		var vals []Value
		for _, r := range rows {
			vals = append(vals, r[0])
		}
		if a[0].Null || a[1].Null {
			return ret(nullValue(t))
		}
		return arrayValue(vals, typeString), true, err
	case "ARRAY_CAT":
		if a[0].Null {
			a[0] = nullValue(t)
		}
		if a[1].Null {
			a[1] = nullValue(t)
		}
		v, err := concatArrays(a[0], a[1], env.zone())
		return v, true, err
	case "ARRAY_APPEND", "ARRAY_PREPEND":
		l, r := a[0], a[1]
		v, err := concatArrays(l, r, env.zone())
		return v, true, err
	case "__ARRAY_SUBSCRIPT":
		for _, i := range []int{1, 2} {
			if a[i].Null {
				continue
			}
			a[i], err = coerceIn(a[i], typeInt64, env.zone())
			if err != nil {
				return Value{}, true, err
			}
		}
		if a[0].Null {
			return ret(nullValue(t))
		}
		v := a[0].fields()
		if a[1].Null || a[3].I != 0 && a[2].Null {
			return ret(nullValue(t))
		}
		if a[3].I == 0 {
			if a[1].Null || a[1].I < 1 || a[1].I > int64(len(v)) {
				return ret(nullValue(t))
			}
			return ret(v[a[1].I-1])
		}
		lo, hi := int64(1), int64(len(v))
		if !a[1].Null {
			lo = max(lo, a[1].I)
		}
		if !a[2].Null {
			hi = min(hi, a[2].I)
		}
		if lo > hi {
			return ret(arrayValue(nil, t.element()))
		}
		return ret(arrayValue(slices.Clone(v[lo-1:hi]), t.element()))
	case "ARRAY_UPPER", "ARRAY_LOWER", "ARRAY_DIMS":
		if a[0].Null || len(a[0].fields()) == 0 {
			return ret(nullValue(t))
		}
		if f.Name == "ARRAY_DIMS" {
			return ret(stringValue("[1:" + strconv.Itoa(len(a[0].fields())) + "]"))
		}
		if a[1].Null || a[1].I != 1 {
			return ret(nullValue(t))
		}
		if f.Name == "ARRAY_LOWER" {
			return ret(intValue(t, 1))
		}
		return ret(intValue(t, int64(len(a[0].fields()))))
	case "ARRAY_TO_STRING":
		if a[0].Null || a[1].Null {
			return ret(nullValue(t))
		}
		var parts []string
		for _, v := range a[0].fields() {
			if v.Null {
				if len(a) > 2 && !a[2].Null {
					parts = append(parts, a[2].textIn(env.zone()))
				}
			} else {
				parts = append(parts, v.textIn(env.zone()))
			}
		}
		return ret(stringValue(strings.Join(parts, a[1].S)))
	case "@>", "<@", "&&":
		if a[0].Null || a[1].Null {
			return ret(nullValue(t))
		}
		l, r := a[0].fields(), a[1].fields()
		if f.Name == "<@" {
			l, r = r, l
		}
		all := true
		for _, v := range r {
			found := false
			for _, x := range l {
				if !x.Null && !v.Null {
					c, ok := compareIn(x, v, env.zone())
					found = found || (ok && c == 0)
				}
			}
			if f.Name == "&&" && found {
				return ret(boolValue(true))
			}
			all = all && found
		}
		return ret(boolValue(f.Name != "&&" && all))
	default:
		if a[0].Null {
			return ret(nullValue(t))
		}
		needle := a[1]
		if !needle.Null {
			needle, err = coerceIn(needle, a[0].T.element(), env.zone())
			if err != nil {
				return Value{}, true, err
			}
		}
		var out []Value
		start := int64(1)
		if len(a) > 2 && f.Name == "ARRAY_POSITION" {
			if a[2].Null {
				return Value{}, true, stateErrorf(stateInvalidParameter, "initial position must not be null")
			}
			start = a[2].I
		}
		for i, v := range a[0].fields() {
			c, ok := compareIn(v, needle, env.zone())
			equal := v.Null && needle.Null || !v.Null && !needle.Null && ok && c == 0
			switch f.Name {
			case "ARRAY_POSITION", "ARRAY_POSITIONS":
				if equal && int64(i+1) >= start {
					if f.Name == "ARRAY_POSITION" {
						return ret(intValue(t, int64(i+1)))
					}
					out = append(out, intValue(typeInt32, int64(i+1)))
				}
			case "ARRAY_REMOVE":
				if !equal {
					out = append(out, v)
				}
			case "ARRAY_REPLACE":
				if equal {
					v, err = coerceIn(a[2], a[0].T.element(), env.zone())
					if err != nil {
						return Value{}, true, err
					}
				}
				out = append(out, v)
			}
		}
		if f.Name == "ARRAY_POSITION" {
			return ret(nullValue(t))
		}
		return ret(arrayValue(out, t.element()))
	}
}

func arrayElementType(exprs []Expr, types []ColType) (ColType, error) {
	common := typeNull
	for i, t := range types {
		if t.Kind == KindArray || t.Kind == KindRecord {
			return ColType{}, stateErrorf(stateFeatureNotSupported, "only one-dimensional scalar arrays are supported")
		}
		if unknownLiteral(exprs[i]) {
			continue
		}
		joined, ok := setOpType(common, t)
		if !ok {
			return ColType{}, &typeMatchError{what: "ARRAY", a: common, b: t}
		}
		common = joined
	}
	t, err := commonExprType("ARRAY", exprs, types, false)
	if err != nil {
		return ColType{}, err
	}
	if t.Kind == KindNull && len(exprs) > 0 {
		t = typeString
	}
	for _, x := range exprs {
		if unknownLiteral(x) {
			if err := checkLiteral(x, t); err != nil {
				return ColType{}, err
			}
		}
	}
	return t, nil
}
