// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Arrow C run-end arrays have no parent buffers. Arrow Go's generic validator
// expects its internal AlwaysNull placeholder instead. Adapt only that slot,
// sharing/retaining the original buffers and still running full validation of
// every run, child, offset and length. Never modify caller-owned array data.
func validateInputArray(a arrow.Array) error {
	if !containsRunEnd(a.DataType()) {
		return array.ValidateFull(a)
	}
	data := validationData(a.Data())
	defer data.Release()
	adapted := array.MakeFromData(data)
	defer adapted.Release()
	return array.ValidateFull(adapted)
}
func containsRunEnd(dt arrow.DataType) bool {
	if dt.ID() == arrow.RUN_END_ENCODED {
		return true
	}
	if d, ok := dt.(*arrow.DictionaryType); ok {
		return containsRunEnd(d.ValueType)
	}
	if n, ok := dt.(arrow.NestedType); ok {
		for _, f := range n.Fields() {
			if containsRunEnd(f.Type) {
				return true
			}
		}
	}
	return false
}
func validationData(data arrow.ArrayData) *array.Data {
	buffers := data.Buffers()
	if data.DataType().ID() == arrow.RUN_END_ENCODED && len(buffers) == 0 {
		buffers = []*memory.Buffer{nil}
	}
	children := make([]arrow.ArrayData, len(data.Children()))
	for i, child := range data.Children() {
		children[i] = validationData(child)
		defer children[i].Release()
	}
	out := array.NewData(data.DataType(), data.Len(), buffers, children, data.NullN(), data.Offset())
	if data.DataType().ID() == arrow.DICTIONARY {
		dict := validationData(data.Dictionary())
		defer dict.Release()
		out.SetDictionary(dict)
	}
	return out
}
func validateInputRecord(rec arrow.RecordBatch) error {
	if int(rec.NumCols()) != rec.Schema().NumFields() {
		return fmt.Errorf("record column/schema count mismatch")
	}
	for i := 0; i < int(rec.NumCols()); i++ {
		a := rec.Column(i)
		if int64(a.Len()) != rec.NumRows() || !arrow.TypeEqual(a.DataType(), rec.Schema().Field(i).Type) {
			return fmt.Errorf("record column %d length/type mismatch", i)
		}
		if err := validateInputArray(a); err != nil {
			return fmt.Errorf("column %d: %w", i, err)
		}
	}
	return nil
}
