// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "strings"

func jsonSRFTypes(name string) ([]ColType, bool) {
	if !strings.HasPrefix(name, "JSON_") && !strings.HasPrefix(name, "JSONB_") {
		return nil, false
	}
	base := strings.TrimPrefix(name, "JSONB_")
	if base == name {
		base = strings.TrimPrefix(name, "JSON_")
	}
	t := typeJSON
	if strings.HasPrefix(name, "JSONB_") {
		t = typeJSONB
	}
	switch base {
	case "EACH":
		return []ColType{typeString, t}, true
	case "EACH_TEXT":
		return []ColType{typeString, typeString}, true
	case "ARRAY_ELEMENTS":
		return []ColType{t}, true
	case "ARRAY_ELEMENTS_TEXT", "OBJECT_KEYS":
		return []ColType{typeString}, true
	}
	return nil, false
}
func (env *evalEnv) jsonSRFRows(f *Func, a []Value) ([][]Value, []ColType, error) {
	ts, _ := jsonSRFTypes(f.Name)
	if len(a) != 1 {
		return nil, nil, noSuchFunction(f.Name, false, nil)
	}
	if a[0].Null {
		return nil, ts, nil
	}
	if a[0].T.Kind != KindString && !a[0].T.Kind.isJSON() {
		return nil, nil, noSuchFunction(f.Name, false, []ColType{a[0].T})
	}
	n, err := parseJSON(a[0].S, true)
	if err != nil {
		return nil, nil, err
	}
	binary := strings.HasPrefix(f.Name, "JSONB_")
	if binary {
		toJSONB(n)
	}
	object := strings.Contains(f.Name, "EACH") || strings.HasSuffix(f.Name, "OBJECT_KEYS")
	if object && n.kind != jsonObject {
		return nil, nil, stateErr(stateInvalidParameter, "cannot call %s on a non-object", strings.ToLower(f.Name))
	}
	if !object && n.kind != jsonArray {
		return nil, nil, stateErr(stateInvalidParameter, "cannot extract elements from a scalar or object")
	}
	if len(n.items) > maxSeriesRows {
		return nil, nil, errTooManySeriesRows()
	}
	if err := retainedAllocate(env.ctx, int64(len(n.items)), int64(len(ts))*128); err != nil {
		return nil, nil, err
	}
	rows := make([][]Value, 0, len(n.items))
	for i, x := range n.items {
		if err := cancelled(env.ctx); err != nil {
			return nil, nil, err
		}
		if strings.HasSuffix(f.Name, "OBJECT_KEYS") {
			rows = append(rows, []Value{stringValue(n.keys[i])})
			continue
		}
		var v Value
		if strings.HasSuffix(f.Name, "_TEXT") {
			if binary && x.kind != jsonNull && x.kind != jsonString {
				j, e := jsonbValue(x)
				v, err = stringValue(j.S), e
			} else {
				v, err = jsonScalarText(x)
			}
		} else if binary {
			v, err = jsonbValue(x)
		} else {
			v = Value{T: typeJSON, S: x.raw}
		}
		if err != nil {
			return nil, nil, err
		}
		if object {
			rows = append(rows, []Value{stringValue(n.keys[i]), v})
		} else {
			rows = append(rows, []Value{v})
		}
	}
	return rows, ts, nil
}
