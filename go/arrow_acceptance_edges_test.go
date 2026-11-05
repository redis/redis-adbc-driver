// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestArrowAcceptancePhysicalOutputs(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, c := range []struct {
		layout string
		v      Value
		ok     bool
	}{
		{"decimal32", decimalValue(big.NewInt(999999999), 9, -2), true},
		{"decimal64", decimalValue(big.NewInt(-999999999999999999), 18, 2), true},
		{"decimal128", decimalValue(new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil), big.NewInt(1)), 38, 2), true},
		{"decimal256", decimalValue(new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(76), nil), big.NewInt(1)), 76, -2), true},
		{"timestamp:+05:30", intValue(timestampType(arrow.Nanosecond, "UTC"), math.MaxInt64), true},
		{"timestamp:America/New_York", intValue(timestampType(arrow.Nanosecond, "UTC"), math.MinInt64), true},
		{"decimal32", decimalValue(big.NewInt(1), 10, 0), false},
		{"decimal64", decimalValue(big.NewInt(1), 19, 0), false},
		{"decimal128", decimalValue(big.NewInt(1), 39, 0), false},
	} {
		source, err := recordsReader(mem, []resultColumn{{Name: "v", Type: c.v.T}}, [][]Value{{c.v}, {nullValue(c.v.T)}})
		if err != nil {
			t.Fatal(err)
		}
		r, err := resultLayouts{"v": c.layout}.wrap(context.Background(), mem, source)
		if err != nil {
			if c.ok {
				t.Fatal(c.layout, err)
			}
			continue
		}
		if !c.ok {
			r.Release()
			t.Fatal("narrow layout accepted", c.layout)
		}
		if !r.Next() {
			t.Fatal(r.Err())
		}
		actual, err := valueAt(r.RecordBatch().Column(0), 0)
		if err != nil {
			t.Fatal(err)
		}
		if eq, ok := compareIn(c.v, actual, utcZone); !ok || eq != 0 || !r.RecordBatch().Column(0).IsNull(1) {
			t.Fatal(c.layout, c.v, actual)
		}
		r.Release()
	}
	for _, dt := range []arrow.DataType{&arrow.Decimal32Type{Precision: 9, Scale: math.MaxInt32}, &arrow.Decimal256Type{Precision: 76, Scale: math.MinInt32}} {
		if _, err := colTypeFromArrow(dt); err == nil {
			t.Fatal("unbounded scale accepted", dt)
		}
	}
	for _, unit := range []arrow.TimeUnit{arrow.Second, arrow.Millisecond, arrow.Microsecond, arrow.Nanosecond} {
		dt := arrow.DataType(&arrow.Time64Type{Unit: unit})
		if unit == arrow.Second || unit == arrow.Millisecond {
			dt = &arrow.Time32Type{Unit: unit}
		}
		for _, v := range []int64{-1, 86400*unitsPerSecond[unit] + 1} {
			a := acceptanceTicks(t, mem, dt, []int64{v})
			if _, err := valueAt(a, 0); err == nil {
				t.Fatal("out-of-range Arrow time accepted", unit, v)
			}
			a.Release()
		}
	}
	a := acceptanceTicks(t, mem, arrow.FixedWidthTypes.Date64, []int64{math.MinInt64, math.MaxInt64})
	for i := 0; i < 2; i++ {
		if _, err := valueAt(a, i); err == nil {
			t.Fatal("out-of-range Date64 accepted")
		}
	}
	a.Release()
}

func TestArrowAcceptanceLargeOffsets(t *testing.T) {
	for _, dt := range []arrow.DataType{arrow.BinaryTypes.LargeString, arrow.BinaryTypes.LargeBinary} {
		for _, offset := range []int64{-1, 1 << 31, math.MaxInt64} {
			offsets := memory.NewBufferBytes(arrow.Int64Traits.CastToBytes([]int64{0, 1}))
			payload := memory.NewBufferBytes([]byte{'x'})
			data := array.NewData(dt, 1, []*memory.Buffer{nil, offsets, payload}, nil, 0, 0)
			a := array.MakeFromData(data)
			// Construct a safe tiny array first: Arrow Go's constructor already
			// panics on impossible extents. Corrupt only the test-owned offset
			// buffer to exercise the driver's validation without huge buffers.
			arrow.Int64Traits.CastFromBytes(offsets.Bytes())[1] = offset
			if err := validateInputArray(a); err == nil {
				t.Fatal("invalid large offset accepted", offset)
			}
			a.Release()
			data.Release()
			offsets.Release()
			payload.Release()
		}
	}
}

func TestArrowAcceptanceDictionaryPayload(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, size := range []int{maxResultDictionaryPayload, maxResultDictionaryPayload + 1} {
		b := array.NewStringBuilder(mem)
		b.Append(strings.Repeat("x", size))
		a := b.NewArray()
		b.Release()
		rec := inventoryRecord(a)
		a.Release()
		source, _ := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
		rec.Release()
		r, err := resultLayouts{"v": "dictionary"}.wrap(context.Background(), mem, source)
		if err != nil {
			t.Fatal(err)
		}
		if r.Next() != (size <= maxResultDictionaryPayload) {
			t.Fatal(size, r.Err())
		}
		if size > maxResultDictionaryPayload && (r.Err() == nil || !strings.Contains(r.Err().Error(), "payload bytes")) {
			t.Fatal(r.Err())
		}
		r.Release()
	}
	// Independent dictionaries include NULL dictionary entries, NULL indices
	// and unused entries. Empty batches and replacements preserve decoded data.
	for _, binary := range []bool{false, true} {
		valueType := arrow.DataType(arrow.BinaryTypes.String)
		if binary {
			valueType = arrow.BinaryTypes.Binary
		}
		var batches []arrow.RecordBatch
		var wantRows [][]Value
		for _, text := range []string{"first", "second"} {
			var values arrow.Array
			if binary {
				b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
				b.AppendNull()
				b.Append([]byte(text))
				b.Append([]byte("unused"))
				values = b.NewArray()
				b.Release()
			} else {
				b := array.NewStringBuilder(mem)
				b.AppendNull()
				b.Append(text)
				b.Append("unused")
				values = b.NewArray()
				b.Release()
			}
			ib := array.NewInt16Builder(mem)
			ib.AppendValues([]int16{1, 0, 0, 1}, []bool{true, true, false, true})
			indices := ib.NewArray()
			ib.Release()
			a := array.NewDictionaryArray(&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int16, ValueType: valueType}, indices, values)
			indices.Release()
			values.Release()
			ct, err := colTypeFromArrow(a.DataType())
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < a.Len(); i++ {
				v, err := valueAt(a, i)
				if err != nil {
					t.Fatal(err)
				}
				wantRows = append(wantRows, []Value{v})
			}
			r, err := buildRecord(mem, []resultColumn{{Name: "v", Type: ct}}, wantRows[len(wantRows)-4:])
			if err != nil {
				t.Fatal(err)
			}
			batches = append(batches, r.NewSlice(0, 0), r)
			a.Release()
		}
		source, _ := array.NewRecordReader(batches[0].Schema(), batches)
		for _, b := range batches {
			b.Release()
		}
		r, err := resultLayouts{"v": "dictionary"}.wrap(context.Background(), mem, source)
		if err != nil {
			t.Fatal(err)
		}
		index := 0
		for r.Next() {
			for i := 0; i < int(r.RecordBatch().NumRows()); i++ {
				v, e := valueAt(r.RecordBatch().Column(0), i)
				want := wantRows[index][0]
				if e != nil || v.Null != want.Null || v.S != want.S {
					t.Fatal(index, v, want, e)
				}
				index++
			}
		}
		if r.Err() != nil || index != len(wantRows) {
			t.Fatal(index, r.Err())
		}
		r.Release()
	}
}

func TestArrowAcceptanceUnsignedSQL(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_arrow_unsigned_semantics"
	h.exec("DROP TABLE IF EXISTS " + table)
	defer h.exec("DROP TABLE IF EXISTS " + table)
	b := array.NewUint64Builder(memory.DefaultAllocator)
	b.AppendValues([]uint64{math.MaxInt64, 1 << 63, math.MaxUint64}, nil)
	b.AppendNull()
	a := b.NewArray()
	b.Release()
	rec := inventoryRecord(a)
	a.Release()
	defer rec.Release()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, table); err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v + CAST(-1 AS BIGINT), v * 2 FROM "+table+" WHERE v IS NOT NULL ORDER BY __rowid", "9223372036854775806|18446744073709551614", "9223372036854775807|18446744073709551616", "18446744073709551614|36893488147419103230")
	h.expectRows("SELECT SUM(v),MIN(v),MAX(v),COUNT(v) FROM "+table, "36893488147419103230|9223372036854775807|18446744073709551615|3")
	h.expectRows("SELECT v FROM "+table+" WHERE v > 9223372036854775807 ORDER BY v", "9223372036854775808", "18446744073709551615")
	h.expectRows("SELECT CAST(1.235 AS NUMERIC(5,2)),CAST(-1.235 AS NUMERIC(5,2)),CAST(149 AS NUMERIC(3,-1))", "1.24|-1.24|150")
	h.expectRows("SELECT CAST(1.25 AS NUMERIC(9,2)) + CAST(123400 AS NUMERIC(18,-2))", "123401.25")
	h.expectRows("SELECT CAST("+strings.Repeat("9", 38)+" AS NUMERIC(38,0)) + 1", "1"+strings.Repeat("0", 38))
	wide := strings.Repeat("9", 38)
	sum := new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil), big.NewInt(1))
	sum.Mul(sum, big.NewInt(2))
	h.expectRows("SELECT SUM(v) FROM (VALUES (CAST("+wide+" AS NUMERIC(38,0))),(CAST("+wide+" AS NUMERIC(38,0)))) t(v)", sum.String())
	h.expectError("SELECT CAST("+strings.Repeat("9", 76)+" AS NUMERIC(76,0)) + 1", "numeric field overflow")
	const signed = "it_arrow_unsigned_append"
	h.exec("DROP TABLE IF EXISTS " + signed)
	h.exec("CREATE TABLE " + signed + " (v BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + signed)
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, signed); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestMode, adbc.OptionValueIngestModeAppend); err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err == nil {
		t.Fatal("uint64 overflow appended to signed BIGINT")
	}
	h.expectRows("SELECT COUNT(*) FROM "+signed, "0")
}

func TestArrowAcceptanceLayoutCancellation(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, layout := range []string{"large_string", "string_view", "dictionary"} {
		ctx, cancel := context.WithCancel(context.Background())
		a, err := buildRecord(mem, []resultColumn{{Name: "v", Type: typeString}}, [][]Value{{stringValue("retained long string payload")}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := buildRecord(mem, []resultColumn{{Name: "v", Type: typeString}}, [][]Value{{stringValue("next")}})
		if err != nil {
			t.Fatal(err)
		}
		source, _ := array.NewRecordReader(a.Schema(), []arrow.RecordBatch{a, b})
		a.Release()
		b.Release()
		r, err := resultLayouts{"v": layout}.wrap(ctx, mem, source)
		if err != nil {
			t.Fatal(err)
		}
		if !r.Next() {
			t.Fatal(r.Err())
		}
		retained := r.RecordBatch()
		retained.Retain()
		cancel()
		if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
			t.Fatal(layout, r.Err())
		}
		r.Release()
		v, err := valueAt(retained.Column(0), 0)
		if err != nil || v.S != "retained long string payload" {
			t.Fatal(layout, v, err)
		}
		retained.Release()
	}
}
