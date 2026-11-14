// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "github.com/apache/arrow-go/v18/arrow/array"

// bindUnmanagedArrowInput fuses the common already-compatible conversion and append.
// Input points into the reader's existing scratch row and is never retained.
// All required metadata is decoded and fitRow has run before this is called.
func bindUnmanagedArrowInput(b array.Builder, t ColType, c typedArrowColumn) func(*Value) error {
	switch out := b.(type) {
	case *array.Int16Builder:
		if !t.Kind.isInteger() {
			return nil
		}
		lo, hi := intRange(t.Kind)
		return func(v *Value) error {
			if v.Null {
				out.AppendNull()
				return nil
			}
			if !v.T.Kind.isInteger() || v.T.Alias != "" {
				return appendArrowInputFallback(c, v)
			}
			if v.I < lo || v.I > hi {
				return intOutOfRange(t.Kind)
			}
			out.Append(int16(v.I))
			return nil
		}
	case *array.Int32Builder:
		if !t.Kind.isInteger() {
			return nil
		}
		lo, hi := intRange(t.Kind)
		return func(v *Value) error {
			if v.Null {
				out.AppendNull()
				return nil
			}
			if !v.T.Kind.isInteger() || v.T.Alias != "" {
				return appendArrowInputFallback(c, v)
			}
			if v.I < lo || v.I > hi {
				return intOutOfRange(t.Kind)
			}
			out.Append(int32(v.I))
			return nil
		}
	case *array.Int64Builder:
		if !t.Kind.isInteger() {
			return nil
		}
		lo, hi := intRange(t.Kind)
		return func(v *Value) error {
			if v.Null {
				out.AppendNull()
				return nil
			}
			if !v.T.Kind.isInteger() || v.T.Alias != "" {
				return appendArrowInputFallback(c, v)
			}
			if v.I < lo || v.I > hi {
				return intOutOfRange(t.Kind)
			}
			out.Append(int64(v.I))
			return nil
		}
	case *array.StringBuilder:
		if t.Kind != KindString || t.Fixed {
			return nil
		}
		return func(v *Value) error {
			if v.Null {
				out.AppendNull()
				return nil
			}
			if v.T.Kind != KindString || v.T.Fixed {
				return appendArrowInputFallback(c, v)
			}
			out.Append(v.S)
			return nil
		}
	default:
		return nil
	}
}

func appendArrowInputFallback(c typedArrowColumn, v *Value) error {
	converted, err := c.coerce(*v)
	if err != nil {
		return err
	}
	return c.append(converted)
}
