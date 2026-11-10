// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Only explicit SQL JSON identity opts into native JSON interpretation. The
// existing reject/storage policy for arbitrary incoming extensions is unchanged.
func nativeJSONField(f arrow.Field) (ColType, bool, error) {
	identity, _ := f.Metadata.GetValue(typeNameKey)
	if !strings.EqualFold(identity, "JSON") && !strings.EqualFold(identity, "JSONB") {
		return ColType{}, false, nil
	}
	t := typeJSON
	if strings.EqualFold(identity, "JSONB") {
		t = typeJSONB
	}
	name, serialization := "", ""
	seen := map[string]bool{}
	for i, k := range f.Metadata.Keys() {
		if strings.HasPrefix(k, "ARROW:extension:") || k == typeNameKey {
			if seen[k] {
				return ColType{}, true, fmt.Errorf("duplicate JSON metadata key %s", k)
			}
			seen[k] = true
		}
		switch k {
		case "ARROW:extension:name":
			name = f.Metadata.Values()[i]
		case "ARROW:extension:metadata":
			serialization = f.Metadata.Values()[i]
		default:
			if strings.HasPrefix(k, "ARROW:extension:") {
				return ColType{}, true, fmt.Errorf("unknown JSON extension metadata key")
			}
		}
	}
	storage := f.Type
	if ext, ok := f.Type.(arrow.ExtensionType); ok {
		if name != "" && name != ext.ExtensionName() || seen["ARROW:extension:metadata"] && serialization != ext.Serialize() {
			return ColType{}, true, fmt.Errorf("JSON extension type/metadata mismatch")
		}
		name, serialization, storage = ext.ExtensionName(), ext.Serialize(), ext.StorageType()
	}
	if name != "arrow.json" || storage.ID() != arrow.STRING && storage.ID() != arrow.LARGE_STRING && storage.ID() != arrow.STRING_VIEW {
		return ColType{}, true, fmt.Errorf("native JSON requires arrow.json UTF-8 storage")
	}
	if len(serialization) > maxExtensionMetadata {
		return ColType{}, true, fmt.Errorf("JSON metadata exceeds limit")
	}
	if serialization != "" {
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(serialization), &object) != nil || object == nil {
			return ColType{}, true, fmt.Errorf("invalid JSON extension metadata")
		}
	}
	return t, true, nil
}

func nativeJSONInputReader(source array.RecordReader) (array.RecordReader, error) {
	fieldsLeft, metadataLeft := 4096, 64<<10
	fields := source.Schema().Fields()
	changed := false
	var normalize func(arrow.Field) (arrow.Field, error)
	normalize = func(f arrow.Field) (arrow.Field, error) {
		if _, owned, err := nativeJSONField(f); owned {
			if err != nil {
				return f, err
			}
			if ext, ok := f.Type.(arrow.ExtensionType); ok {
				f.Type = ext.StorageType()
			}
			changed = true
			return f, nil
		}
		if st, ok := f.Type.(*arrow.StructType); ok {
			children := st.Fields()
			for i, c := range children {
				n, err := normalize(c)
				if err != nil {
					return f, err
				}
				children[i] = n
			}
			f.Type = arrow.StructOf(children...)
		}
		return f, nil
	}
	for i, f := range fields {
		if err := extensionSchemaBounds(f, 0, &fieldsLeft, &metadataLeft); err != nil {
			return nil, err
		}
		n, err := normalize(f)
		if err != nil {
			return nil, err
		}
		fields[i] = n
	}
	if !changed {
		return source, nil
	}
	metadata := source.Schema().Metadata()
	r := &extensionInputReader{source: source, schema: arrow.NewSchema(fields, &metadata)}
	r.refs.Store(1)
	return r, nil
}
