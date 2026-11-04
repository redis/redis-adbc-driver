// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

// Package intpredicate is an isolated experiment, not a SQL execution backend.
// It compares already decoded signed integers without arithmetic or coercion.
package intpredicate

import (
	"context"
	"fmt"
)

type Op uint8

const (
	Equal Op = iota
	NotEqual
	Less
	LessEqual
	Greater
	GreaterEqual
)

// Match returns an owned, little-bit-order bitmap. A nil validity bitmap means
// all rows are valid; NULL comparisons never match. Padding bits are zero.
// scalar forces the portable reference for measurements and parity checks.
// Input and output storage must not alias. No pointers are retained by C.
func Match(ctx context.Context, values []int64, valid []uint64, rhs int64, op Op, scalar bool) ([]uint64, error) {
	words := (len(values) + 63) / 64
	if op > GreaterEqual || (valid != nil && len(valid) != words) {
		return nil, fmt.Errorf("invalid integer predicate shape or operation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]uint64, words)
	const batch = 65536 // bounded work and cancellation latency, like CUDA
	for start := 0; start < len(values); start += batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+batch, len(values))
		dst := out[start/64 : (end+63)/64]
		var validity []uint64
		if valid != nil {
			validity = valid[start/64 : (end+63)/64]
		}
		if scalar || !native(values[start:end], validity, dst, rhs, op) {
			reference(values[start:end], validity, dst, rhs, op)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func reference(values []int64, valid, out []uint64, rhs int64, op Op) {
	for i, v := range values {
		if valid != nil && valid[i/64]&(uint64(1)<<uint(i%64)) == 0 {
			continue
		}
		var match bool
		switch op {
		case Equal:
			match = v == rhs
		case NotEqual:
			match = v != rhs
		case Less:
			match = v < rhs
		case LessEqual:
			match = v <= rhs
		case Greater:
			match = v > rhs
		case GreaterEqual:
			match = v >= rhs
		}
		if match {
			out[i/64] |= uint64(1) << uint(i%64)
		}
	}
}
