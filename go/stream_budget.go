// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "math"

// The optional batch budget accounts 256 bytes per projected Value plus twice
// its string/binary payload, 32 bytes plus decimal magnitude, and recursively
// stored array/record children. This conservative width-sensitive unit covers
// row slots plus output payload; it is NOT an allocator measurement or RSS cap.
// Nulls still count their slot. Duplicate output columns count independently.
// A batch stops BEFORE adding a row that exceeds its budget, except that one
// oversized row is always admitted to guarantee progress. The row ceiling also
// applies. Both APIs use this policy, including generic streamed expressions.
//
// Redis still fetches one bounded cursor page at a time. Its reply/raw rows and
// completed Arrow batches from that page can coexist with the partial batch.
// Page overshoot, allocator capacity/slack, builder offsets/validity, metadata,
// retained consumer batches and a single huge row are outside a process bound.
// No universal default changes: zero preserves the established row-only policy.
func batchRowBytes(row []Value) int64 {
	var n int64
	for _, v := range row {
		n = addBatchBytes(n, batchValueBytes(v))
	}
	return n
}

func batchValueBytes(v Value) int64 {
	n := int64(256)
	if v.Null {
		return n
	}
	n = addBatchBytes(n, addBatchBytes(int64(len(v.S)), int64(len(v.S))))
	if v.D != nil {
		n = addBatchBytes(n, 32+int64(v.D.BitLen()/8)+1)
	}
	for _, f := range v.fields() {
		n = addBatchBytes(n, batchValueBytes(f))
	}
	return n
}

func addBatchBytes(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}

// Fixed-width slots need no per-row inspection. Prebind only columns that
// can carry variable payload, once per reader, after output types are known.
type batchSizer struct {
	base     int64
	variable []int
}

func newBatchSizer(cols []resultColumn) *batchSizer {
	s := &batchSizer{base: 256 * int64(len(cols))}
	for i, c := range cols {
		switch c.Type.Kind {
		case KindString, KindBinary, KindDecimal, KindRecord, KindArray:
			s.variable = append(s.variable, i)
		}
	}
	return s
}

func (r *streamReader) rowBatchBytes(row []Value) int64 {
	if r.batchSizer == nil {
		r.batchSizer = newBatchSizer(r.cols)
	}
	n := r.batchSizer.base
	for _, i := range r.batchSizer.variable {
		n = addBatchBytes(n, batchValueBytes(row[i])-256)
	}
	return n
}
