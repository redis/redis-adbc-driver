// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestTypedArrowAgainstGeneric(t *testing.T) {
	for _, typ := range []ColType{typeInt16, typeInt32, typeInt64, typeBool, typeFloat32, typeFloat64, typeString,
		{Kind: KindString, Fixed: true, Length: 5}, typeBinary, typeDate, timeType(arrow.Microsecond), typeTimestamp, typeTimestampTZ, typeTimeTZ, typeInterval, decimalType(12, 3)} {
		t.Run(typ.SQLName(), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			cols := []resultColumn{{Name: "v", Type: typ}}
			a, b := array.NewRecordBuilder(mem, resultSchema(cols)), array.NewRecordBuilder(mem, resultSchema(cols))
			defer a.Release()
			defer b.Release()
			bound := bindArrowColumn(a.Field(0), typ)
			decode := bindStoredDecode(columnMeta{Type: typ})
			for _, raw := range []string{"0", "1", "-1", "32768", "2147483648", "9007199254740993", "9223372036854775807", "-9223372036854775808", "1.5", "NaN", "__NaN__", "a\x00b", "2024-01-01", "12:13:14+02", "bad", ""} {
				v, err := decode(raw)
				want, werr := (columnMeta{Type: typ}).decode(raw)
				if fmt.Sprint(err) != fmt.Sprint(werr) {
					t.Fatalf("decode %q: %v / %v", raw, err, werr)
				}
				if err != nil {
					continue
				}
				v, err = bound.coerce(v)
				want, werr = Coerce(want, typ)
				if fmt.Sprint(err) != fmt.Sprint(werr) {
					t.Fatalf("coerce %q: %v / %v", raw, err, werr)
				}
				if err != nil {
					continue
				}
				if err := bound.append(v); err != nil {
					t.Fatal(err)
				}
				if err := appendValue(b.Field(0), typ, want); err != nil {
					t.Fatal(err)
				}
			}
			if err := bound.append(nullValue(typ)); err != nil {
				t.Fatal(err)
			}
			b.Field(0).AppendNull()
			x, y := a.NewRecordBatch(), b.NewRecordBatch()
			defer x.Release()
			defer y.Release()
			if !array.RecordApproxEqual(x, y, array.WithNaNsEqual(true), array.WithAbsTolerance(0)) {
				t.Fatalf("typed/generic differ: %v / %v", x, y)
			}
		})
	}
}
