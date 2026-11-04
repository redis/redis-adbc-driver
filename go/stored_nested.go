// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Map is an ordered sequence of key/value pairs (duplicates are significant).
// Union is a tag plus one selected child, including a selected NULL child.
// Both carry an interned, immutable schema; no scalar coercion is permitted.
func (t ColType) taggedNested() bool { return t.Kind == KindMap || t.Kind == KindUnion }

func nestedType(kind Kind, fields []structField, codes []arrow.UnionTypeCode) (ColType, error) {
	if kind == KindMap {
		if len(fields) != 2 || fields[0].Name != "key" || fields[1].Name != "value" || fields[0].Nullable || !fields[1].Nullable || len(codes) != 0 {
			return ColType{}, fmt.Errorf("invalid Map schema")
		}
		k := fields[0].Type.Kind
		if k == KindNull || k == KindRecord || k == KindArray || k == KindMap || k == KindUnion {
			return ColType{}, fmt.Errorf("Map keys must have a supported scalar type")
		}
	} else if kind == KindUnion {
		if len(fields) == 0 || len(fields) > 128 || len(codes) != len(fields) {
			return ColType{}, fmt.Errorf("Union requires 1 through 128 tagged children")
		}
		seen := map[arrow.UnionTypeCode]bool{}
		for _, c := range codes {
			if c < 0 || seen[c] {
				return ColType{}, fmt.Errorf("Union type codes must be unique and nonnegative")
			}
			seen[c] = true
		}
	} else {
		return ColType{}, fmt.Errorf("invalid nested kind")
	}
	// Reuse the named-struct identifier and metadata-size validation.
	named, err := namedRecordOf(fields)
	if err != nil {
		return ColType{}, err
	}
	data, err := json.Marshal(struct {
		Kind   Kind
		Fields []structField
		Codes  []arrow.UnionTypeCode
	}{kind, fields, codes})
	if err != nil {
		return ColType{}, err
	}
	if len(data) > 1<<20 {
		return ColType{}, fmt.Errorf("nested schema exceeds 1 MiB")
	}
	r, _ := recordTypes.LoadOrStore("nested:"+string(data), &recordType{fields: named.Rec.fields, named: named.Rec.named, codes: append([]arrow.UnionTypeCode(nil), codes...)})
	return ColType{Kind: kind, Rec: r.(*recordType)}, nil
}

func mapFromArrow(t *arrow.MapType) (ColType, error) {
	if t.KeyType().ID() == arrow.NULL {
		return ColType{}, fmt.Errorf("Map keys cannot have Null type")
	}
	key, err := colTypeFromField(t.KeyField())
	if err != nil {
		return ColType{}, err
	}
	item, err := colTypeFromField(t.ItemField())
	if err != nil {
		return ColType{}, err
	}
	return nestedType(KindMap, []structField{{"key", key, false}, {"value", item, true}}, nil)
}
func unionFromArrow(t arrow.UnionType) (ColType, error) {
	fields := make([]structField, t.NumFields())
	for i, f := range t.Fields() {
		ct, err := colTypeFromField(f)
		if err != nil {
			return ColType{}, err
		}
		fields[i] = structField{f.Name, ct, f.Nullable}
	}
	return nestedType(KindUnion, fields, t.TypeCodes())
}
func (t ColType) nestedArrowType() arrow.DataType {
	fields := make([]arrow.Field, len(t.Rec.named))
	for i, f := range t.Rec.named {
		fields[i] = arrowField(f.Name, f.Type, f.Nullable)
	}
	if t.Kind == KindMap {
		return arrow.MapOfFields(fields[0], fields[1])
	}
	return arrow.DenseUnionOf(fields, t.Rec.codes)
}
func (t ColType) unionChild(code int64) int {
	for i, c := range t.Rec.codes {
		if int64(c) == code {
			return i
		}
	}
	return -1
}
func mapValueAt(a *array.Map, i int, ct ColType) (Value, error) {
	start, end := a.ValueOffsets(i)
	if start < 0 || end < start || end > int64(a.Keys().Len()) || end > int64(a.Items().Len()) {
		return Value{}, fmt.Errorf("invalid Arrow Map offsets")
	}
	fields := make([]Value, 0, 2*(end-start))
	for j := start; j < end; j++ {
		for _, child := range []arrow.Array{a.Keys(), a.Items()} {
			v, err := valueAt(child, int(j))
			if err != nil {
				return Value{}, err
			}
			fields = append(fields, v)
		}
	}
	return validateNestedValue(Value{T: ct, rec: &fields})
}
func unionValueAt(a array.Union, i, offset int, ct ColType) (Value, error) {
	code := int64(a.TypeCode(i))
	child := ct.unionChild(code)
	if child < 0 || offset < 0 || offset >= a.Field(child).Len() {
		return Value{}, fmt.Errorf("invalid Arrow Union tag or offset")
	}
	v, err := valueAt(a.Field(child), offset)
	if err != nil {
		return Value{}, err
	}
	fields := []Value{v}
	return validateNestedValue(Value{T: ct, I: code, rec: &fields})
}
func validateNestedValue(v Value) (Value, error) {
	fields := append([]Value(nil), v.fields()...)
	v.rec = &fields
	if v.T.Kind == KindMap {
		if v.I != 0 || len(fields)%2 != 0 {
			return Value{}, fmt.Errorf("invalid stored Map entry count")
		}
		for i, f := range fields {
			if f.Null && !v.T.Rec.named[i%2].Nullable {
				return Value{}, stateErr(stateNotNull, "Map %s must not be NULL", v.T.Rec.named[i%2].Name)
			}
			c, err := Coerce(f, v.T.Rec.fields[i%2])
			if err != nil {
				return Value{}, err
			}
			fields[i] = c
		}
	} else {
		child := v.T.unionChild(v.I)
		if child < 0 || len(fields) != 1 {
			return Value{}, fmt.Errorf("invalid stored Union tag or child count")
		}
		if fields[0].Null && !v.T.Rec.named[child].Nullable {
			return Value{}, stateErr(stateNotNull, "Union child must not be NULL")
		}
		c, err := Coerce(fields[0], v.T.Rec.fields[child])
		if err != nil {
			return Value{}, err
		}
		fields[0] = c
	}
	return v, nil
}

type storedNested struct {
	Version int       `json:"v"`
	Tag     int64     `json:"tag"`
	Fields  []*[]byte `json:"fields"`
}

func encodeNested(v Value) string {
	out := storedNested{Version: 1, Tag: v.I, Fields: make([]*[]byte, len(v.fields()))}
	for i, f := range v.fields() {
		if !f.Null {
			b := []byte(encodeStored(f))
			out.Fields[i] = &b
		}
	}
	data, _ := json.Marshal(out)
	return string(data)
}
func decodeNested(s string, t ColType) (Value, error) {
	if len(s) > 128<<20 {
		return Value{}, fmt.Errorf("stored Map/Union payload exceeds 128 MiB")
	}
	var in storedNested
	if err := json.Unmarshal([]byte(s), &in); err != nil {
		return Value{}, err
	}
	if in.Version != 1 || in.Fields == nil || len(in.Fields) > maxNormalizedInputValues {
		return Value{}, fmt.Errorf("invalid stored Map/Union envelope")
	}
	if t.Kind == KindMap && len(in.Fields)%2 != 0 || t.Kind == KindUnion && (len(in.Fields) != 1 || t.unionChild(in.Tag) < 0) {
		return Value{}, fmt.Errorf("invalid stored Map/Union shape")
	}
	fields := make([]Value, len(in.Fields))
	for i, raw := range in.Fields {
		child := i % 2
		if t.Kind == KindUnion {
			child = t.unionChild(in.Tag)
		}
		ct := t.Rec.fields[child]
		fields[i] = nullValue(ct)
		if raw != nil {
			v, err := decodeStored(string(*raw), ct)
			if err != nil {
				return Value{}, err
			}
			fields[i] = v
		}
	}
	return validateNestedValue(Value{T: t, I: in.Tag, rec: &fields})
}

func containsTaggedNested(dt arrow.DataType) bool {
	if dt.ID() == arrow.MAP || dt.ID() == arrow.DENSE_UNION || dt.ID() == arrow.SPARSE_UNION {
		return true
	}
	if n, ok := dt.(arrow.NestedType); ok {
		for _, f := range n.Fields() {
			if containsTaggedNested(f.Type) {
				return true
			}
		}
	}
	return false
}

// Check the serialized schema before recursive ColType decoding and interning.
// A schema level uses up to three JSON objects/arrays; the Arrow input depth
// limit remains 16. This also bounds corrupt metadata read from Redis.
func checkNestedMetadataJSON(data []byte) error {
	if len(data) > 1<<20 {
		return fmt.Errorf("column schema exceeds 1 MiB")
	}
	depth := 0
	quoted, escaped := false, false
	for _, c := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth > 64 {
				return fmt.Errorf("column schema nesting exceeds 64 JSON levels")
			}
		case '}', ']':
			depth--
		}
	}
	return nil
}

func hasStoredTaggedType(t ColType) bool {
	if t.taggedNested() {
		return true
	}
	if t.Rec != nil {
		for _, f := range t.Rec.fields {
			if hasStoredTaggedType(f) {
				return true
			}
		}
	}
	return false
}

// Reject scalar functions that would otherwise read an empty scalar payload
// from a Map/Union (including one inside an ordinary list or named struct).
func (e *executor) nestedFunctionError(f *Func) error {
	if !e.mayUseTagged(f) {
		return nil
	}
	types := inferArgTypes(f.Args, e.scopeTypes(), e.paramTypes)
	found := false
	for _, t := range types {
		found = found || hasStoredTaggedType(t)
	}
	if !found {
		return nil
	}
	d, ok := callee(f)
	if ok && (d.impl == implCoalesce || d.impl == implConditional) {
		var first ColType
		for _, t := range types {
			if t.Kind == KindNull {
				continue
			}
			if first.Kind == KindNull {
				first = t
			} else if first != t {
				return stateErr(stateDatatypeMismatch, "Map/Union conditional schemas must match exactly")
			}
		}
		return nil
	}
	switch f.Name {
	case "COUNT", "ANY_VALUE", "PG_TYPEOF":
		return nil
	}
	return stateErr(stateFeatureNotSupported, "function %s does not support Map/Union values", f.Name)
}

func (e *executor) mayUseTagged(expr Expr) bool {
	found := false
	walkExprPruned(expr, func(x Expr) bool {
		switch x := x.(type) {
		case *ColumnRef:
			if t, ok := e.refType(x); ok && hasStoredTaggedType(t) {
				found = true
			}
		case *Param:
			if x.Index >= 0 && x.Index < len(e.paramTypes) && hasStoredTaggedType(e.paramTypes[x.Index]) {
				found = true
			}
		case *Cast:
			found = found || hasStoredTaggedType(x.T)
		}
		return !found
	})
	return found
}
func (e *executor) nestedBinaryError(b *Binary) error {
	if !e.mayUseTagged(b) {
		return nil
	}
	types := inferArgTypes([]Expr{b.L, b.R}, e.scopeTypes(), e.paramTypes)
	if len(types) != 2 || !hasStoredTaggedType(types[0]) && !hasStoredTaggedType(types[1]) {
		return nil
	}
	switch b.Op {
	case "=", "!=", "<>", "<", ">", "<=", ">=":
		if types[0] == types[1] || types[0].Kind == KindNull || types[1].Kind == KindNull {
			return nil
		}
	}
	return stateErr(stateDatatypeMismatch, "operator %s requires matching Map/Union schemas and supports only comparisons", b.Op)
}
