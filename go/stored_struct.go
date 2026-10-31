// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
)

type structField struct {
	Name     string  `json:"name"`
	Type     ColType `json:"type"`
	Nullable bool    `json:"nullable"`
}

func namedRecordOf(fields []structField) (ColType, error) {
	if len(fields) == 0 || len(fields) > 1024 {
		return ColType{}, fmt.Errorf("stored structs require 1 through 1024 fields")
	}
	seen := map[string]bool{}
	types := make([]ColType, len(fields))
	for i, f := range fields {
		if f.Name == "" || len(f.Name) > 1024 || strings.ContainsRune(f.Name, 0) || seen[f.Name] {
			return ColType{}, fmt.Errorf("struct field names must be unique, 1 through 1024 bytes, and contain no NUL")
		}
		seen[f.Name] = true
		types[i] = f.Type
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return ColType{}, err
	}
	if len(data) > 1<<20 {
		return ColType{}, fmt.Errorf("stored struct schema exceeds 1 MiB")
	}
	key := "named:" + string(data)
	r, _ := recordTypes.LoadOrStore(key, &recordType{fields: types, named: append([]structField(nil), fields...)})
	return ColType{Kind: KindRecord, Rec: r.(*recordType)}, nil
}
func (t ColType) namedRecord() bool {
	return t.Kind == KindRecord && t.Rec != nil && t.Rec.named != nil
}

func structFromArrow(dt *arrow.StructType) (ColType, error) {
	fields := make([]structField, dt.NumFields())
	for i, f := range dt.Fields() {
		ct, err := colTypeFromField(f)
		if err != nil {
			return ColType{}, fmt.Errorf("struct field %q: %w", f.Name, err)
		}
		fields[i] = structField{Name: f.Name, Type: ct, Nullable: f.Nullable}
	}
	return namedRecordOf(fields)
}

// Composite HASH values have an explicit version and recursively encoded
// cells. A nil cell is SQL NULL; a pointer to empty bytes is an empty, non-NULL value. Binary-safe cells
// use JSON base64 encoding rather than treating arbitrary bytes as UTF-8.
// Existing scalar SQL arrays continue to use their original text encoding.
type storedComposite struct {
	Version int       `json:"v"`
	Fields  []*[]byte `json:"fields"`
}

func compositeStorage(t ColType) bool {
	return t.namedRecord() || t.Kind == KindArray && (t.element().Kind == KindArray || t.element().namedRecord())
}
func encodeComposite(v Value) string {
	out := storedComposite{Version: 1, Fields: make([]*[]byte, len(v.fields()))}
	for i, x := range v.fields() {
		if !x.Null {
			s := []byte(encodeStored(x))
			out.Fields[i] = &s
		}
	}
	data, _ := json.Marshal(out) // only strings, booleans and integers; cannot fail
	return string(data)
}
func decodeComposite(s string, t ColType) (Value, error) {
	var data storedComposite
	if err := json.Unmarshal([]byte(s), &data); err != nil {
		return Value{}, fmt.Errorf("invalid stored composite: %w", err)
	}
	if data.Version != 1 || data.Fields == nil {
		return Value{}, fmt.Errorf("invalid stored composite version or fields")
	}
	if t.namedRecord() && len(data.Fields) != len(t.Rec.fields) {
		return Value{}, fmt.Errorf("stored struct field count mismatch")
	}
	values := make([]Value, len(data.Fields))
	for i, raw := range data.Fields {
		ct := t.element()
		if t.namedRecord() {
			ct = t.Rec.fields[i]
		}
		values[i] = nullValue(ct)
		if raw != nil {
			v, err := decodeStored(string(*raw), ct)
			if err != nil {
				return Value{}, err
			}
			values[i] = v
		}
	}
	v := Value{T: t, rec: &values}
	if t.namedRecord() {
		return coerceRecord(v, t, utcZone)
	}
	return v, nil
}
