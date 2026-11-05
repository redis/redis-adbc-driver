// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Registry revision 2: explicit normalization only, not SQL UUID/JSON types or
// arbitrary plugin registration. Unknown semantics reject; nested STRUCT UUID/JSON fields normalize explicitly.
const OptionStringExtensionPolicy = "adbc.redis.extension_policy"
const maxExtensionMetadata = 16 << 10

type extensionInputReader struct {
	source  array.RecordReader
	schema  *arrow.Schema
	names   []string
	current arrow.RecordBatch
	refs    atomic.Int64
	err     error
}

func (s *statementImpl) prepareExtensionInput() error {
	if s.extensionPolicy != "storage" || s.params == nil {
		return nil
	}
	if _, ok := s.params.(*extensionInputReader); ok {
		return nil
	}
	r, err := extensionStorageReader(s.params)
	if err != nil {
		return err
	}
	s.params = r
	return nil
}

func extensionStorageReader(source array.RecordReader) (array.RecordReader, error) {
	fieldsLeft, metadataLeft := 4096, 64<<10
	for _, field := range source.Schema().Fields() {
		if err := extensionSchemaBounds(field, 0, &fieldsLeft, &metadataLeft); err != nil {
			return nil, invalidArg(err)
		}
	}
	return extensionStorageReaderDepth(source, 0)
}
func extensionStorageReaderDepth(source array.RecordReader, depth int) (array.RecordReader, error) {
	if depth > 16 {
		return nil, invalidArg(fmt.Errorf("extension STRUCT nesting exceeds 16"))
	}
	fields := source.Schema().Fields()
	names := make([]string, len(fields))
	changed := false
	for i, f := range fields {
		name, serialized := "", ""
		hasName, hasMetadata := false, false
		keys, values := []string{}, []string{}
		for j, key := range f.Metadata.Keys() {
			v := f.Metadata.Values()[j]
			switch key {
			case "ARROW:extension:name":
				if hasName {
					return nil, invalidArg(fmt.Errorf("duplicate extension name metadata"))
				}
				hasName = true
				name = v
			case "ARROW:extension:metadata":
				if hasMetadata {
					return nil, invalidArg(fmt.Errorf("duplicate extension serialization metadata"))
				}
				hasMetadata = true
				serialized = v
			default:
				if strings.HasPrefix(key, "ARROW:extension:") {
					return nil, invalidArg(fmt.Errorf("unknown extension metadata key %q", key))
				}
				keys = append(keys, key)
				values = append(values, v)
			}
		}
		storage := f.Type
		if ext, ok := f.Type.(arrow.ExtensionType); ok {
			if hasName && name != ext.ExtensionName() || hasMetadata && serialized != ext.Serialize() {
				return nil, invalidArg(fmt.Errorf("extension type/field metadata mismatch"))
			}
			name, serialized, hasName = ext.ExtensionName(), ext.Serialize(), true
			storage = ext.StorageType()
		}
		if !hasName {
			if hasMetadata {
				return nil, invalidArg(fmt.Errorf("extension metadata requires a name"))
			}
			if dt, ok := f.Type.(*arrow.StructType); ok {
				source, _ := array.NewRecordReader(arrow.NewSchema(dt.Fields(), nil), nil)
				child, err := extensionStorageReaderDepth(source, depth+1)
				if err != nil {
					source.Release()
					return nil, err
				}
				normalized := arrow.StructOf(child.Schema().Fields()...)
				child.Release()
				if !arrow.TypeEqual(normalized, f.Type, arrow.CheckMetadata()) {
					fields[i].Type = normalized
					changed = true
				}
			}
			// The normal field mapper rejects extensions nested inside containers.
			if _, err := colTypeFromField(fields[i]); err != nil {
				return nil, errorf(adbc.StatusNotImplemented, "column %q: %v", f.Name, err)
			}
			continue
		}
		if len(name) == 0 || len(name) > 256 || len(serialized) > maxExtensionMetadata || !utf8.ValidString(name) || !utf8.ValidString(serialized) {
			return nil, invalidArg(fmt.Errorf("extension name/metadata exceeds bounds or is invalid UTF-8"))
		}
		if f.Metadata.FindKey(typeNameKey) >= 0 {
			return nil, invalidArg(fmt.Errorf("extension storage fallback conflicts with SQL type metadata"))
		}
		switch name {
		case "arrow.uuid":
			if !arrow.TypeEqual(storage, &arrow.FixedSizeBinaryType{ByteWidth: 16}) || serialized != "" {
				return nil, invalidArg(fmt.Errorf("arrow.uuid requires fixed_size_binary[16] and empty metadata"))
			}
		case "arrow.json":
			if storage.ID() != arrow.STRING && storage.ID() != arrow.LARGE_STRING && storage.ID() != arrow.STRING_VIEW {
				return nil, invalidArg(fmt.Errorf("arrow.json requires UTF-8 string storage"))
			}
			if serialized != "" {
				var object map[string]json.RawMessage
				if json.Unmarshal([]byte(serialized), &object) != nil || object == nil {
					return nil, invalidArg(fmt.Errorf("arrow.json metadata must be empty or a JSON object"))
				}
			}
		default:
			return nil, errorf(adbc.StatusNotImplemented, "extension %q is not allowlisted for storage fallback", name)
		}
		fields[i].Type = storage
		fields[i].Metadata = arrow.NewMetadata(keys, values)
		names[i] = name
		changed = true
	}
	if !changed {
		return source, nil
	}
	md := source.Schema().Metadata()
	r := &extensionInputReader{source: source, schema: arrow.NewSchema(fields, &md), names: names}
	r.refs.Store(1)
	return r, nil
}

func (r *extensionInputReader) Retain() { r.refs.Add(1) }
func (r *extensionInputReader) Release() {
	if r.refs.Add(-1) == 0 {
		if r.current != nil {
			r.current.Release()
			r.current = nil
		}
		r.source.Release()
	}
}
func (r *extensionInputReader) Schema() *arrow.Schema          { return r.schema }
func (r *extensionInputReader) RecordBatch() arrow.RecordBatch { return r.current }
func (r *extensionInputReader) Record() arrow.Record           { return r.current }
func (r *extensionInputReader) Err() error {
	if r.err != nil {
		return r.err
	}
	return r.source.Err()
}
func (r *extensionInputReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.err != nil || !r.source.Next() {
		return false
	}
	src := r.source.RecordBatch()
	if !src.Schema().Equal(r.source.Schema()) {
		r.err = invalidArg(fmt.Errorf("extension stream schema changed between batches"))
		return false
	}
	cols := make([]arrow.Array, int(src.NumCols()))
	budget := normalizationBudget{maxNormalizedInputValues, maxNormalizedInputPayload}
	for i := range cols {
		a := src.Column(i)
		if ext, ok := a.(array.ExtensionArray); ok {
			a = ext.Storage()
		}
		if !arrow.TypeEqual(a.DataType(), r.schema.Field(i).Type, arrow.CheckMetadata()) {
			data := extensionStorageData(a.Data(), r.schema.Field(i).Type)
			a = array.MakeFromData(data)
			data.Release()
			defer a.Release()
		}
		if !arrow.TypeEqual(a.DataType(), r.schema.Field(i).Type, arrow.CheckMetadata()) {
			r.err = invalidArg(fmt.Errorf("extension storage array/type mismatch"))
			return false
		}
		if err := validateInputArray(a); err != nil {
			r.err = invalidArg(err)
			return false
		}
		if int64(a.Len()) > budget.values {
			r.err = invalidArg(fmt.Errorf("extension input exceeds decoded value budget"))
			return false
		}
		for row := 0; row < a.Len(); row++ {
			if err := budget.visit(a, row); err != nil {
				r.err = invalidArg(err)
				return false
			}
			if err := validateExtensionJSON(a, r.source.Schema().Field(i), row); err != nil {
				r.err = invalidArg(err)
				return false
			}
		}

		cols[i] = a
	}
	r.current = array.NewRecordBatch(r.schema, cols, src.NumRows())
	return true
}

// Preserve offsets/null bitmaps and retain every buffer while replacing only
// approved logical types/metadata. The resulting arrays own their references.
func extensionStorageData(src arrow.ArrayData, target arrow.DataType) *array.Data {
	children := make([]arrow.ArrayData, len(src.Children()))
	for i, child := range src.Children() {
		ct := child.DataType()
		if st, ok := target.(*arrow.StructType); ok {
			ct = st.Field(i).Type
		}
		children[i] = extensionStorageData(child, ct)
		defer children[i].Release()
	}
	out := array.NewData(target, src.Len(), src.Buffers(), children, src.NullN(), src.Offset())
	if src.Dictionary() != nil {
		out.SetDictionary(src.Dictionary())
	}
	return out
}
func validateExtensionJSON(a arrow.Array, field arrow.Field, row int) error {
	if a.IsNull(row) {
		return nil
	}
	name, _ := field.Metadata.GetValue("ARROW:extension:name")
	if ext, ok := field.Type.(arrow.ExtensionType); ok {
		name = ext.ExtensionName()
	}
	if name == "arrow.json" {
		v, err := valueAt(a, row)
		if err != nil || !utf8.ValidString(v.S) || !json.Valid([]byte(v.S)) {
			return fmt.Errorf("invalid arrow.json value in field %q at row %d", field.Name, row)
		}
	}
	if st, ok := field.Type.(*arrow.StructType); ok {
		values := a.(*array.Struct)
		for i, child := range st.Fields() {
			if err := validateExtensionJSON(values.Field(i), child, row); err != nil {
				return err
			}
		}
	}
	return nil
}

// Check every nested schema shape before invoking the ordinary recursive mapper,
// including containers whose extension children remain deliberately unsupported.
func extensionSchemaBounds(f arrow.Field, depth int, fields, metadata *int) error {
	if depth > 16 {
		return fmt.Errorf("extension schema nesting exceeds 16")
	}
	*fields--
	if *fields < 0 {
		return fmt.Errorf("extension schema exceeds 4096 fields")
	}
	for i, k := range f.Metadata.Keys() {
		*metadata -= len(k) + len(f.Metadata.Values()[i])
	}
	dt := f.Type
	if ext, ok := dt.(arrow.ExtensionType); ok {
		*metadata -= len(ext.ExtensionName()) + len(ext.Serialize())
		dt = ext.StorageType()
	}
	if *metadata < 0 {
		return fmt.Errorf("extension schema metadata exceeds 64 KiB")
	}
	if dict, ok := dt.(*arrow.DictionaryType); ok {
		return extensionSchemaBounds(arrow.Field{Type: dict.ValueType}, depth+1, fields, metadata)
	}
	if nested, ok := dt.(arrow.NestedType); ok {
		for _, child := range nested.Fields() {
			if err := extensionSchemaBounds(child, depth+1, fields, metadata); err != nil {
				return err
			}
		}
	}
	return nil
}
