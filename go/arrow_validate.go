// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Arrow C run-end and Null arrays have no parent buffers. Arrow Go's generic
// validator expects its internal AlwaysNull placeholder instead. Adapt that slot,
// sharing/retaining the original buffers and still running full validation of
// every run, child, offset and length. Never modify caller-owned array data.
func validateInputArray(a arrow.Array) error {
	if !needsValidationPlaceholder(a.DataType()) {
		return array.ValidateFull(a)
	}
	data := validationData(a.Data())
	defer data.Release()
	adapted := array.MakeFromData(data)
	defer adapted.Release()
	return array.ValidateFull(adapted)
}
func needsValidationPlaceholder(dt arrow.DataType) bool {
	if dt.ID() == arrow.RUN_END_ENCODED || dt.ID() == arrow.NULL {
		return true
	}
	if d, ok := dt.(*arrow.DictionaryType); ok {
		return needsValidationPlaceholder(d.ValueType)
	}
	if n, ok := dt.(arrow.NestedType); ok {
		for _, f := range n.Fields() {
			if needsValidationPlaceholder(f.Type) {
				return true
			}
		}
	}
	return false
}
func validationData(data arrow.ArrayData) *array.Data {
	buffers := data.Buffers()
	if (data.DataType().ID() == arrow.RUN_END_ENCODED || data.DataType().ID() == arrow.NULL) && len(buffers) == 0 {
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
	return checkInputNormalizationBudget(rec)
}

// These are decoded-input limits, not allocator or process-memory guarantees.
// They apply to batches containing the layouts added in #208, whose logical
// expansion can be much larger than their physical buffers. Ordinary input
// schemas retain their existing limits. Check before allocating ingest rows.
const (
	maxNormalizedInputValues  = 1 << 20
	maxNormalizedInputPayload = 64 << 20
)

func hasNormalizingLayout(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.FIXED_SIZE_LIST, arrow.LIST_VIEW, arrow.LARGE_LIST_VIEW, arrow.RUN_END_ENCODED:
		return true
	}
	if d, ok := dt.(*arrow.DictionaryType); ok {
		return hasNormalizingLayout(d.ValueType)
	}
	if n, ok := dt.(arrow.NestedType); ok {
		for _, f := range n.Fields() {
			if hasNormalizingLayout(f.Type) {
				return true
			}
		}
	}
	return false
}

type normalizationBudget struct{ values, payload int64 }

func checkInputNormalizationBudget(rec arrow.RecordBatch) error {
	needed := false
	for _, f := range rec.Schema().Fields() {
		needed = needed || hasNormalizingLayout(f.Type)
	}
	if !needed {
		return nil
	}
	b := normalizationBudget{maxNormalizedInputValues, maxNormalizedInputPayload}
	// Reject a compressed huge logical row count without iterating it, including
	// narrow run-end indices that could otherwise overflow later arithmetic.
	if rec.NumRows() > b.values || rec.NumCols() > 0 && rec.NumRows() > b.values/rec.NumCols() {
		return fmt.Errorf("Arrow normalization exceeds %d decoded values per batch", maxNormalizedInputValues)
	}
	for _, a := range rec.Columns() {
		for i := 0; i < a.Len(); i++ {
			if err := b.visit(a, i); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *normalizationBudget) visit(a arrow.Array, i int) error {
	if encoded, ok := a.(*array.RunEndEncoded); ok {
		return b.visit(encoded.Values(), encoded.GetPhysicalIndex(i))
	}
	if d, ok := a.(*array.Dictionary); ok && !d.IsNull(i) {
		return b.visit(d.Dictionary(), d.GetValueIndex(i))
	}
	if b.values == 0 {
		return fmt.Errorf("Arrow normalization exceeds %d decoded values per batch", maxNormalizedInputValues)
	}
	b.values--
	if a.IsNull(i) {
		return nil
	}
	switch a := a.(type) {
	case *array.Struct:
		for j := 0; j < a.NumField(); j++ {
			if err := b.visit(a.Field(j), i); err != nil {
				return err
			}
		}
	case interface {
		ListValues() arrow.Array
		ValueOffsets(int) (int64, int64)
	}:
		start, end := a.ValueOffsets(i)
		if end-start > b.values {
			return fmt.Errorf("Arrow normalization exceeds %d decoded values per batch", maxNormalizedInputValues)
		}
		for j := start; j < end; j++ {
			if err := b.visit(a.ListValues(), int(j)); err != nil {
				return err
			}
		}
	case interface{ ValueLen(int) int }:
		n := int64(a.ValueLen(i))
		if n > b.payload {
			return fmt.Errorf("Arrow normalization exceeds %d decoded string/binary bytes per batch", maxNormalizedInputPayload)
		}
		b.payload -= n
	case *array.FixedSizeBinary:
		n := int64(a.DataType().(*arrow.FixedSizeBinaryType).ByteWidth)
		if n > b.payload {
			return fmt.Errorf("Arrow normalization exceeds %d decoded string/binary bytes per batch", maxNormalizedInputPayload)
		}
		b.payload -= n
	}
	return nil
}
