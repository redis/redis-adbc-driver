// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Match batchValueBytes without materializing or copying the decoded payload.
// This limits each returned slice, not the resident IPC chunk or consumer-held
// batches. Preflight restricts nested output to List and Struct arrays.
func retainedArrowRowBytes(record arrow.RecordBatch, row int64) int64 {
	var n int64
	for _, column := range record.Columns() {
		n = addBatchBytes(n, retainedArrowValueBytes(column, int(row)))
	}
	return n
}

func retainedArrowValueBytes(column arrow.Array, row int) int64 {
	n := int64(256)
	if column.IsNull(row) {
		return n
	}
	switch a := column.(type) {
	case *array.String:
		n = addBatchBytes(n, 2*int64(len(a.Value(row))))
	case *array.Binary:
		n = addBatchBytes(n, 2*int64(len(a.Value(row))))
	case *array.Decimal128:
		n = addBatchBytes(n, 32+int64(a.Value(row).BigInt().BitLen()/8)+1)
	case *array.Decimal256:
		n = addBatchBytes(n, 32+int64(a.Value(row).BigInt().BitLen()/8)+1)
	case *array.List:
		start, end := a.ValueOffsets(row)
		for i := start; i < end; i++ {
			n = addBatchBytes(n, retainedArrowValueBytes(a.ListValues(), int(i)))
		}
	case *array.Struct:
		for i := 0; i < a.NumField(); i++ {
			n = addBatchBytes(n, retainedArrowValueBytes(a.Field(i), row))
		}
	}
	return n
}
