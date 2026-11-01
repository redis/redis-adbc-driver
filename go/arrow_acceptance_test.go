// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func acceptanceTicks(t *testing.T, mem memory.Allocator, dt arrow.DataType, values []int64) arrow.Array {
	t.Helper()
	b := array.NewBuilder(mem, dt)
	defer b.Release()
	for _, v := range values {
		switch b := b.(type) {
		case *array.Date64Builder:
			b.Append(arrow.Date64(v))
		case *array.Date32Builder:
			b.Append(arrow.Date32(v))
		case *array.Time32Builder:
			b.Append(arrow.Time32(v))
		case *array.Time64Builder:
			b.Append(arrow.Time64(v))
		case *array.TimestampBuilder:
			b.Append(arrow.Timestamp(v))
		case *array.DurationBuilder:
			b.Append(arrow.Duration(v))
		default:
			t.Fatal("unsupported tick fixture", dt)
		}
	}
	b.AppendNull()
	return b.NewArray()
}

func acceptanceDecimals(t *testing.T, mem memory.Allocator, dt arrow.DataType, values []*big.Int) arrow.Array {
	t.Helper()
	b := array.NewBuilder(mem, dt)
	defer b.Release()
	for _, v := range values {
		switch b := b.(type) {
		case *array.Decimal32Builder:
			b.Append(decimal.Decimal32(v.Int64()))
		case *array.Decimal64Builder:
			b.Append(decimal.Decimal64(v.Int64()))
		case *array.Decimal128Builder:
			b.Append(decimal128.FromBigInt(v))
		case *array.Decimal256Builder:
			b.Append(decimal256.FromBigInt(v))
		default:
			t.Fatal(dt)
		}
	}
	b.AppendNull()
	return b.NewArray()
}

func acceptanceCases(t *testing.T, mem memory.Allocator) []layoutInventoryCase {
	t.Helper()
	var cases []layoutInventoryCase
	add := func(name string, a, want arrow.Array) {
		cases = append(cases, layoutInventoryCase{name: name, input: a, want: want})
	}
	for _, c := range []struct {
		name, json     string
		source, target arrow.DataType
	}{
		{"uint8", `[0,127,128,255,null]`, arrow.PrimitiveTypes.Uint8, arrow.PrimitiveTypes.Int16},
		{"uint16", `[0,255,256,32767,32768,65535,null]`, arrow.PrimitiveTypes.Uint16, arrow.PrimitiveTypes.Int32},
		{"uint32", `[0,65535,65536,2147483647,2147483648,4294967295,null]`, arrow.PrimitiveTypes.Uint32, arrow.PrimitiveTypes.Int64},
	} {
		add(c.name, inventoryJSON(t, mem, c.source, c.json), inventoryJSON(t, mem, c.target, c.json))
	}
	b := array.NewUint64Builder(mem)
	ints := []uint64{0, math.MaxUint32, math.MaxInt64, 1 << 63, math.MaxUint64}
	b.AppendValues(ints, nil)
	b.AppendNull()
	coeffs := make([]*big.Int, len(ints))
	for i, v := range ints {
		coeffs[i] = new(big.Int).SetUint64(v)
	}
	add("uint64", b.NewArray(), acceptanceDecimals(t, mem, &arrow.Decimal128Type{Precision: 20, Scale: 0}, coeffs))
	b.Release()
	for _, width := range []int{32, 64, 128, 256} {
		p := map[int]int32{32: 9, 64: 18, 128: 38, 256: 76}[width]
		max := new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(p)), nil), big.NewInt(1))
		values := []*big.Int{new(big.Int).Neg(max), big.NewInt(0), new(big.Int).Set(max)}
		for _, scale := range []int32{-2, 0, 2} {
			var dt arrow.DataType
			switch width {
			case 32:
				dt = &arrow.Decimal32Type{Precision: p, Scale: scale}
			case 64:
				dt = &arrow.Decimal64Type{Precision: p, Scale: scale}
			case 128:
				dt = &arrow.Decimal128Type{Precision: p, Scale: scale}
			default:
				dt = &arrow.Decimal256Type{Precision: p, Scale: scale}
			}
			canonical := arrow.DataType(&arrow.Decimal128Type{Precision: p, Scale: scale})
			if p > 38 {
				canonical = &arrow.Decimal256Type{Precision: p, Scale: scale}
			}
			add(fmt.Sprintf("decimal%d_scale%d", width, scale), acceptanceDecimals(t, mem, dt, values), acceptanceDecimals(t, mem, canonical, values))
		}
	}
	add("date64_floor", acceptanceTicks(t, mem, arrow.FixedWidthTypes.Date64, []int64{-86400001, -86400000, -1, 0, 1, 86400001}), acceptanceTicks(t, mem, arrow.FixedWidthTypes.Date32, []int64{-2, -1, -1, 0, 0, 1}))
	for _, unit := range []arrow.TimeUnit{arrow.Second, arrow.Millisecond, arrow.Microsecond, arrow.Nanosecond} {
		factor := map[arrow.TimeUnit]int64{arrow.Second: 1, arrow.Millisecond: 1000, arrow.Microsecond: 1000000, arrow.Nanosecond: 1000000000}[unit]
		var timeType arrow.DataType = &arrow.Time64Type{Unit: unit}
		if unit == arrow.Second || unit == arrow.Millisecond {
			timeType = &arrow.Time32Type{Unit: unit}
		}
		values := []int64{0, 1, 86400*factor - 1}
		add("time_"+unit.String(), acceptanceTicks(t, mem, timeType, values), acceptanceTicks(t, mem, timeType, values))
		for _, zone := range []string{"", "America/New_York", "+05:30"} {
			// Both sides of a named-zone DST fold are distinct exact instants.
			values := []int64{-1, 0, 1730611800 * factor, 1730615400 * factor}
			canonical := zone
			if zone != "" {
				canonical = "UTC"
			}
			add("timestamp_"+unit.String()+zone, acceptanceTicks(t, mem, &arrow.TimestampType{Unit: unit, TimeZone: zone}, values), acceptanceTicks(t, mem, &arrow.TimestampType{Unit: unit, TimeZone: canonical}, values))
		}
		values = []int64{-9223372036854775807 / (1000000000 / factor), -1, 0, 1, 9223372036854775807 / (1000000000 / factor)}
		duration := acceptanceTicks(t, mem, &arrow.DurationType{Unit: unit}, values)
		iv := array.NewMonthDayNanoIntervalBuilder(mem)
		for _, v := range values {
			iv.Append(arrow.MonthDayNanoInterval{Nanoseconds: v * (1000000000 / factor)})
		}
		iv.AppendNull()
		add("duration_"+unit.String(), duration, iv.NewArray())
		iv.Release()
	}
	mb := array.NewMonthIntervalBuilder(mem)
	iv := array.NewMonthDayNanoIntervalBuilder(mem)
	for _, v := range []int32{math.MinInt32, -1, 0, 1, math.MaxInt32} {
		mb.Append(arrow.MonthInterval(v))
		iv.Append(arrow.MonthDayNanoInterval{Months: v})
	}
	mb.AppendNull()
	iv.AppendNull()
	add("month_interval", mb.NewArray(), iv.NewArray())
	mb.Release()
	iv.Release()
	db := array.NewDayTimeIntervalBuilder(mem)
	iv = array.NewMonthDayNanoIntervalBuilder(mem)
	for _, v := range []arrow.DayTimeInterval{{Days: math.MinInt32, Milliseconds: math.MaxInt32}, {Days: 1, Milliseconds: -1}, {Days: math.MaxInt32, Milliseconds: math.MinInt32}} {
		db.Append(v)
		iv.Append(arrow.MonthDayNanoInterval{Days: v.Days, Nanoseconds: int64(v.Milliseconds) * 1000000})
	}
	db.AppendNull()
	iv.AppendNull()
	add("day_time_interval", db.NewArray(), iv.NewArray())
	db.Release()
	iv.Release()
	for _, dt := range []arrow.DataType{arrow.BinaryTypes.String, arrow.BinaryTypes.LargeString, arrow.BinaryTypes.StringView} {
		add(dt.Name(), inventoryJSON(t, mem, dt, `["",null,"a\u0000b","a long view payload beyond twelve bytes"]`), inventoryJSON(t, mem, arrow.BinaryTypes.String, `["",null,"a\u0000b","a long view payload beyond twelve bytes"]`))
	}
	for _, dt := range []arrow.DataType{arrow.BinaryTypes.Binary, arrow.BinaryTypes.LargeBinary, arrow.BinaryTypes.BinaryView, &arrow.FixedSizeBinaryType{ByteWidth: 3}} {
		makeBytes := func(typ arrow.DataType) arrow.Array {
			b := array.NewBuilder(mem, typ)
			defer b.Release()
			for _, v := range [][]byte{{0, 255, 0}, nil, {'a', 0, 'b'}} {
				if v == nil {
					b.AppendNull()
					continue
				}
				switch b := b.(type) {
				case *array.BinaryBuilder:
					b.Append(v)
				case *array.BinaryViewBuilder:
					b.Append(v)
				case *array.FixedSizeBinaryBuilder:
					b.Append(v)
				}
			}
			return b.NewArray()
		}
		add(dt.Name(), makeBytes(dt), makeBytes(arrow.BinaryTypes.Binary))
	}
	for _, dt := range []arrow.DataType{arrow.ListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64)), arrow.LargeListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64))} {
		const j = `[[[1,null],[],null],null,[],[[2],[3,4]]]`
		add(dt.Name()+"_ragged", inventoryJSON(t, mem, dt, j), inventoryJSON(t, mem, arrow.ListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64)), j))
	}
	st := arrow.StructOf(arrow.Field{Name: "Required", Type: arrow.PrimitiveTypes.Int64, Nullable: false}, arrow.Field{Name: "nested", Type: arrow.ListOf(arrow.StructOf(arrow.Field{Name: "text", Type: arrow.BinaryTypes.String, Nullable: true})), Nullable: true})
	const sj = `[{"Required":1,"nested":[{"text":"a"},null,{"text":null}]},null,{"Required":2,"nested":[]},{"Required":3,"nested":null}]`
	add("named_struct_nested", inventoryJSON(t, mem, st, sj), inventoryJSON(t, mem, st, sj))
	indicesBuilder := array.NewInt16Builder(mem)
	indicesBuilder.AppendValues([]int16{1, 0, 0, 1}, []bool{true, true, false, true})
	indices := indicesBuilder.NewArray()
	indicesBuilder.Release()
	values := inventoryJSON(t, mem, arrow.BinaryTypes.String, `[null,"x","unused"]`)
	dictionary := array.NewDictionaryArray(&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int16, ValueType: arrow.BinaryTypes.String}, indices, values)
	indices.Release()
	values.Release()
	add("dictionary_null_values", dictionary, inventoryJSON(t, mem, arrow.BinaryTypes.String, `["x",null,null,"x"]`))
	return cases
}

func TestArrowAcceptanceRoundTrips(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	cases := acceptanceCases(t, mem)
	defer releaseInventory(cases)
	checkCanonicalRoundTrips(t, mem, cases)
}
func TestArrowAcceptanceStorage(t *testing.T) {
	cases := acceptanceCases(t, memory.DefaultAllocator)
	defer releaseInventory(cases)
	checkCanonicalStorage(t, cases, "it_arrow_acceptance")
}
