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

// Registry revision 1: explicit normalization only, not SQL UUID/JSON types or
// arbitrary plugin registration. Unknown and nested extension semantics reject.
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
			// The normal field mapper rejects extensions nested inside containers.
			if _, err := colTypeFromField(f); err != nil {
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
	for i := range cols {
		a := src.Column(i)
		if ext, ok := a.(array.ExtensionArray); ok {
			a = ext.Storage()
		}
		if !arrow.TypeEqual(a.DataType(), r.schema.Field(i).Type) {
			r.err = invalidArg(fmt.Errorf("extension storage array/type mismatch"))
			return false
		}
		if err := validateInputArray(a); err != nil {
			r.err = invalidArg(err)
			return false
		}
		if r.names[i] == "arrow.json" {
			for j := 0; j < a.Len(); j++ {
				if a.IsNull(j) {
					continue
				}
				v, err := valueAt(a, j)
				if err != nil || !utf8.ValidString(v.S) || !json.Valid([]byte(v.S)) {
					r.err = invalidArg(fmt.Errorf("invalid arrow.json value in column %q at row %d", r.schema.Field(i).Name, j))
					return false
				}
			}
		}
		cols[i] = a
	}
	r.current = array.NewRecordBatch(r.schema, cols, src.NumRows())
	return true
}
