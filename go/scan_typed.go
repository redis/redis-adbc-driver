// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Bind the common managed HASH encodings once; preserve the generic decoder's
// legacy float-form integers, errors, text encodings and uncommon type behavior.
func bindStoredDecode(c columnMeta) func(string) (Value, error) {
	if c.Encoding == "text" {
		return c.decode
	}
	t := c.Type
	switch t.Kind {
	case KindBool, KindInt16, KindInt32, KindInt64, KindDate, KindTime, KindTimestamp:
		return func(s string) (Value, error) {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return intValue(t, n), nil
			}
			return c.decode(s)
		}
	case KindString:
		if !t.Fixed {
			return func(s string) (Value, error) { return stringValue(s), nil }
		}
	case KindBinary:
		return func(s string) (Value, error) { return binaryValue(s), nil }
	}
	return c.decode
}

type typedArrowColumn struct {
	coerce func(Value) (Value, error)
	append func(Value) error
}

func bindArrowColumn(b array.Builder, t ColType) typedArrowColumn {
	c := typedArrowColumn{coerce: func(v Value) (Value, error) { return Coerce(v, t) }}
	if t.Kind.isInteger() {
		lo, hi := intRange(t.Kind)
		c.coerce = func(v Value) (Value, error) {
			if v.Null {
				return nullValue(t), nil
			}
			if v.T.Kind.isInteger() && v.T.Alias == "" {
				if v.I < lo || v.I > hi {
					return Value{}, intOutOfRange(t.Kind)
				}
				return intValue(t, v.I), nil
			}
			return Coerce(v, t)
		}
	} else if t.Kind == KindString && !t.Fixed {
		c.coerce = func(v Value) (Value, error) {
			if v.Null {
				return nullValue(t), nil
			}
			if v.T.Kind == KindString && !v.T.Fixed {
				return v, nil
			}
			return Coerce(v, t)
		}
	}
	var appendNonNull func(Value)
	switch b := b.(type) {
	case *array.Int16Builder:
		appendNonNull = func(v Value) { b.Append(int16(v.I)) }
	case *array.Int32Builder:
		appendNonNull = func(v Value) { b.Append(int32(v.I)) }
	case *array.Int64Builder:
		appendNonNull = func(v Value) { b.Append(v.I) }
	case *array.BooleanBuilder:
		appendNonNull = func(v Value) { b.Append(v.I != 0) }
	case *array.Float32Builder:
		appendNonNull = func(v Value) { b.Append(float32(v.F)) }
	case *array.Float64Builder:
		appendNonNull = func(v Value) { b.Append(v.F) }
	case *array.StringBuilder:
		if t.Kind != KindTimeTZ {
			appendNonNull = func(v Value) { b.Append(v.S) }
		}
	case *array.BinaryBuilder:
		appendNonNull = func(v Value) { b.Append([]byte(v.S)) }
	case *array.Date32Builder:
		appendNonNull = func(v Value) { b.Append(arrow.Date32(v.I)) }
	case *array.TimestampBuilder:
		appendNonNull = func(v Value) { b.Append(arrow.Timestamp(v.I)) }
	}
	if appendNonNull == nil {
		c.append = func(v Value) error { return appendValue(b, t, v) }
	} else {
		c.append = func(v Value) error {
			if v.Null {
				b.AppendNull()
			} else {
				appendNonNull(v)
			}
			return nil
		}
	}
	return c
}
