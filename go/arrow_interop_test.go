// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestUnsignedNormalization(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	b := array.NewUint64Builder(mem)
	b.AppendValues([]uint64{0, 255, 65535, math.MaxUint32, math.MaxInt64, 1 << 63, math.MaxUint64}, nil)
	b.AppendNull()
	a := b.NewArray()
	b.Release()
	defer a.Release()
	ct, err := colTypeFromArrow(a.DataType())
	if err != nil || ct != decimalType(20, 0) {
		t.Fatal(ct, err)
	}
	data, err := json.Marshal(ct)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded ColType
	if err = json.Unmarshal(data, &reloaded); err != nil || reloaded != ct {
		t.Fatal(reloaded, err)
	}
	for i := 0; i < a.Len(); i++ {
		v, err := valueAt(a, i)
		if err != nil {
			t.Fatal(err)
		}
		if v.Null {
			if i != 7 || v.T != ct {
				t.Fatal(v)
			}
			continue
		}
		want := new(big.Int).SetUint64(a.(*array.Uint64).Value(i))
		if v.D.Cmp(want) != 0 || v.T != ct {
			t.Fatal(i, v, want)
		}
		stored := encodeStored(v)
		decoded, err := decodeStored(stored, reloaded)
		if err != nil || decoded.D.Cmp(want) != 0 {
			t.Fatal(stored, decoded, err)
		}
		rec, err := buildRecord(mem, []resultColumn{{Name: "u", Type: ct}}, [][]Value{{v}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := valueAt(rec.Column(0), 0)
		rec.Release()
		if err != nil || got.D.Cmp(want) != 0 {
			t.Fatal(got, err)
		}
	}
}

func TestListLayoutNormalization(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	fixed := array.NewFixedSizeListBuilder(mem, 2, arrow.PrimitiveTypes.Int32)
	fixed.Append(true)
	fixed.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{91, 92}, nil)
	fixed.Append(true)
	fixed.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{7, 0}, []bool{true, false})
	fixed.AppendNull()
	f := fixed.NewArray()
	fixed.Release()
	defer f.Release()
	view := array.NewListViewBuilder(mem, arrow.PrimitiveTypes.Int32)
	view.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{7, 0, 91}, []bool{true, false, true})
	view.AppendDimensions(2, 1)
	view.AppendDimensions(0, 2)
	view.AppendNull()
	view.AppendDimensions(1, 0)
	va := view.NewArray()
	view.Release()
	defer va.Release()
	large := array.NewLargeListViewBuilder(mem, arrow.PrimitiveTypes.Int32)
	large.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{7, 0}, []bool{true, false})
	large.AppendDimensions(0, 2)
	large.AppendNull()
	large.AppendDimensions(0, 0)
	la := large.NewArray()
	large.Release()
	defer la.Release()
	for _, a := range []arrow.Array{f, va, la} {
		end := int64(a.Len())
		sliced := array.NewSlice(a, 1, end)
		defer sliced.Release()
		for i := 0; i < sliced.Len(); i++ {
			v, err := valueAt(sliced, i)
			if err != nil {
				t.Fatal(err)
			}
			if v.Null {
				continue
			}
			text := encodeStored(v)
			round, err := decodeStored(text, v.T)
			if err != nil {
				t.Fatal(err)
			}
			if n, ok := compareArrays(v, round, utcZone); !ok || n != 0 {
				t.Fatal(text, v, round)
			}
		}
	}
}

func TestRunEndNormalization(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, dt := range []arrow.DataType{arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64} {
		b := array.NewBuilder(mem, dt)
		for _, n := range []int64{2, 5, 8} {
			if err := appendValue(b, typeInt64, intValue(typeInt64, n)); err != nil {
				// appendValue's canonical builders exclude Int8/unsigned, but these
				// run-end types are precisely the supported signed integer widths.
				t.Fatal(err)
			}
		}
		ends := b.NewArray()
		b.Release()
		values := array.NewStringBuilder(mem)
		values.Append("a")
		values.AppendNull()
		values.Append("c")
		vals := values.NewArray()
		values.Release()
		encoded := array.NewRunEndEncodedArray(ends, vals, 8, 0)
		ends.Release()
		vals.Release()
		sliced := array.NewSlice(encoded, 1, 7)
		encoded.Release()
		ct, err := colTypeFromArrow(sliced.DataType())
		if err != nil || ct != typeString {
			t.Fatal(ct, err)
		}
		for i, want := range []string{"a", "NULL", "NULL", "NULL", "c", "c"} {
			v, err := valueAt(sliced, i)
			if err != nil || (want == "NULL" && !v.Null || want != "NULL" && (v.Null || v.Text() != want)) {
				t.Fatal(i, v.Text(), want, err)
			}
		}
		sliced.Release()
	}
}

func TestExtensionMetadataRejected(t *testing.T) {
	for _, key := range []string{"ARROW:extension:name", "ARROW:extension:metadata", "ARROW:extension:future"} {
		f := arrow.Field{Name: "x", Type: arrow.BinaryTypes.String, Metadata: arrow.NewMetadata([]string{key}, []string{"opaque"})}
		if _, err := colTypeFromField(f); err == nil {
			t.Fatal("accepted", key)
		}
		nested := arrow.Field{Name: "l", Type: arrow.ListOfField(f)}
		if _, err := colTypeFromField(nested); err == nil {
			t.Fatal("accepted nested extension", key)
		}
	}
	for _, dt := range []arrow.DataType{arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int64)} {
		if _, err := colTypeFromArrow(dt); err == nil || !strings.Contains(err.Error(), "unsupported Arrow type") {
			t.Fatal(dt, err)
		}
	}
}

func TestResultLayoutOwnership(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, layout := range []string{"large_string", "string_view", "dictionary"} {
		cols := []resultColumn{{Name: "s", Type: typeString}}
		first, err := buildRecord(mem, cols, [][]Value{{stringValue("one")}, {nullValue(typeString)}, {stringValue("one")}})
		if err != nil {
			t.Fatal(err)
		}
		second, err := buildRecord(mem, cols, [][]Value{{stringValue("two")}})
		if err != nil {
			t.Fatal(err)
		}
		src, _ := array.NewRecordReader(resultSchema(cols), []arrow.RecordBatch{first, second})
		first.Release()
		second.Release()
		rdr, err := resultLayouts{"s": layout}.wrap(context.Background(), mem, src)
		if err != nil {
			t.Fatal(err)
		}
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		retained := rdr.RecordBatch()
		retained.Retain()
		if !rdr.Next() {
			t.Fatal(rdr.Err())
		}
		rdr.Release()
		v, err := valueAt(retained.Column(0), 0)
		if err != nil || v.S != "one" || !retained.Column(0).IsNull(1) {
			t.Fatal(v, err)
		}
		retained.Release()
	}
}

func TestStoredStructAndNestedLists(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	nested := arrow.ListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64))
	st := arrow.StructOf(
		arrow.Field{Name: "number", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		arrow.Field{Name: "lists", Type: nested, Nullable: true},
		arrow.Field{Name: "child", Type: arrow.StructOf(arrow.Field{Name: "text", Type: arrow.BinaryTypes.String, Nullable: true}), Nullable: true},
	)
	ct, err := colTypeFromArrow(st)
	if err != nil {
		t.Fatal(err)
	}
	b := array.NewBuilder(mem, st)
	if err := b.UnmarshalJSON([]byte(`[{"number":null,"lists":[[1,null],[],null,[2]],"child":{"text":null}},null,{"number":3,"lists":[],"child":null}]`)); err != nil {
		t.Fatal(err)
	}
	a := b.NewArray()
	b.Release()
	defer a.Release()
	data, err := json.Marshal(ct)
	if err != nil {
		t.Fatal(err)
	}
	var loaded ColType
	if err := json.Unmarshal(data, &loaded); err != nil || loaded != ct {
		t.Fatal(loaded, ct, err)
	}
	var rows [][]Value
	for i := 0; i < a.Len(); i++ {
		v, err := valueAt(a, i)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Null {
			v, err = decodeStored(encodeStored(v), loaded)
			if err != nil {
				t.Fatal(err)
			}
		}
		rows = append(rows, []Value{v})
	}
	rec, err := buildRecord(mem, []resultColumn{{Name: "record", Type: loaded}}, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Release()
	if !array.Equal(a, rec.Column(0)) || !arrow.TypeEqual(st, rec.Column(0).DataType()) {
		t.Fatal(a, rec.Column(0), rec.Schema())
	}
	// Parent-null is distinct from a present struct with all-null children.
	if rec.Column(0).IsNull(0) || !rec.Column(0).IsNull(1) {
		t.Fatal(rec.Column(0))
	}
	for _, fields := range [][]structField{{{Name: "", Type: typeInt64}}, {{Name: "x", Type: typeInt64}, {Name: "x", Type: typeString}}} {
		if _, err := namedRecordOf(fields); err == nil {
			t.Fatal("invalid names accepted")
		}
	}
	required, _ := colTypeFromArrow(arrow.StructOf(arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64, Nullable: false}))
	fields := []Value{nullValue(typeInt64)}
	if _, err := coerceRecord(Value{T: required, rec: &fields}, required, utcZone); err == nil {
		t.Fatal("NULL required child accepted")
	}
}

func TestPhysicalResultConversions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	iv, err := intervalValue(0, 0, -2000000)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		layout string
		v      Value
		ok     bool
	}{
		{"decimal32", decimalValue(big.NewInt(-999999999), 9, 2), true},
		{"decimal64", decimalValue(big.NewInt(999999999999999999), 18, -2), true},
		{"decimal32", decimalValue(big.NewInt(1), 10, 0), false},
		{"date64", intValue(typeDate, -1), true},
		{"timestamp:America/New_York", intValue(timestampType(arrow.Nanosecond, "UTC"), -1), true},
		{"large_binary", binaryValue("a\x00b"), true},
		{"binary_view", binaryValue("a\x00b"), true},
		{"fixed_size_binary:3", binaryValue("a\x00b"), true},
		{"fixed_size_binary:4", binaryValue("abc"), false},
		{"duration:ms", iv, true},
		{"duration:s", iv, false},
		{"day_time_interval", iv, true},
		{"month_interval", iv, false},
	}
	for _, c := range cases {
		t.Run(c.layout+"/"+c.v.T.SQLName(), func(t *testing.T) {
			src, err := recordsReader(mem, []resultColumn{{Name: "v", Type: c.v.T}}, [][]Value{{c.v}, {nullValue(c.v.T)}})
			if err != nil {
				t.Fatal(err)
			}
			rdr, err := resultLayouts{"v": c.layout}.wrap(context.Background(), mem, src)
			if err != nil {
				if c.ok {
					t.Fatal(err)
				}
				return
			}
			defer rdr.Release()
			if !rdr.Next() {
				if c.ok {
					t.Fatal(rdr.Err())
				}
				return
			}
			if !c.ok {
				t.Fatal("lossy layout accepted")
			}
			if !rdr.RecordBatch().Column(0).IsNull(1) {
				t.Fatal("lost NULL")
			}
			got, err := valueAt(rdr.RecordBatch().Column(0), 0)
			if err != nil {
				t.Fatal(err)
			}
			equal, ok := compareIn(c.v, got, utcZone)
			if !ok || equal != 0 {
				t.Fatal(c.v, got)
			}
		})
	}
	for _, value := range []string{`null`, `[]`, `{"v":"unknown"}`, `{"v":"fixed_size_binary:0"}`, `{"v":"duration:days"}`, `{"v":"timestamp:Local"}`} {
		if _, err := parseResultLayouts(value); err == nil {
			t.Fatal("invalid layouts accepted", value)
		}
	}
}

func TestStoredStructBinaryOwnership(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	ct, err := namedRecordOf([]structField{{Name: "bytes", Type: typeBinary, Nullable: true}})
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{binaryValue(string([]byte{0, 255, 128, 0}))}
	original := Value{T: ct, rec: &values}
	decoded, err := decodeStored(encodeStored(original), ct)
	if err != nil || decoded.fields()[0].S != values[0].S {
		t.Fatal(decoded, err)
	}
	b := array.NewBuilder(poisonAllocator{mem}, ct.ArrowType())
	if err := appendValue(b, ct, original); err != nil {
		t.Fatal(err)
	}
	a := b.NewArray()
	b.Release()
	copied, err := valueAt(a, 0)
	a.Release()
	if err != nil || copied.fields()[0].S != values[0].S {
		t.Fatal(copied, err)
	}
}

func TestResultDictionaryLimit(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	b := array.NewStringBuilder(mem)
	for i := 0; i <= maxResultDictionaryEntries; i++ {
		b.Append(fmt.Sprint(i))
	}
	a := b.NewArray()
	b.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.BinaryTypes.String}}, nil)
	batch := array.NewRecordBatch(schema, []arrow.Array{a}, int64(a.Len()))
	a.Release()
	source, _ := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	batch.Release()
	r, err := resultLayouts{"v": "dictionary"}.wrap(context.Background(), mem, source)
	if err != nil {
		t.Fatal(err)
	}
	if r.Next() || r.Err() == nil || !strings.Contains(r.Err().Error(), "dictionary exceeds") {
		t.Fatal("unbounded dictionary accepted", r.Err())
	}
	r.Release()
}

func TestArrowSchemaBounds(t *testing.T) {
	nested := arrow.DataType(arrow.PrimitiveTypes.Int64)
	for i := 0; i < 17; i++ {
		nested = arrow.ListOf(nested)
	}
	for _, dt := range []arrow.DataType{nested, &arrow.Decimal32Type{Precision: 10}, &arrow.TimestampType{Unit: arrow.Second, TimeZone: "not/a/zone"}, &arrow.Time32Type{Unit: arrow.Nanosecond}} {
		if _, err := colTypeFromArrow(dt); err == nil {
			t.Fatal("invalid type accepted", dt)
		}
	}
}

func TestCImportedRunEndValidation(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	ends := array.NewInt16Builder(mem)
	ends.AppendValues([]int16{2, 5}, nil)
	e := ends.NewArray()
	ends.Release()
	defer e.Release()
	b := array.NewStringBuilder(mem)
	b.AppendValues([]string{"a", "b"}, nil)
	v := b.NewArray()
	b.Release()
	defer v.Release()
	data := array.NewData(arrow.RunEndEncodedOf(e.DataType(), v.DataType()), 4, nil, []arrow.ArrayData{e.Data(), v.Data()}, 0, 1)
	defer data.Release()
	imported := array.NewRunEndEncodedData(data)
	defer imported.Release()
	if err := validateInputArray(imported); err != nil {
		t.Fatal(err)
	}
	if len(imported.Data().Buffers()) != 0 {
		t.Fatal("modified caller data")
	}
	badEnds := array.NewInt16Builder(mem)
	badEnds.AppendValues([]int16{3, 2}, nil)
	be := badEnds.NewArray()
	badEnds.Release()
	defer be.Release()
	badData := array.NewData(arrow.RunEndEncodedOf(be.DataType(), v.DataType()), 2, nil, []arrow.ArrayData{be.Data(), v.Data()}, 0, 0)
	defer badData.Release()
	bad := array.NewRunEndEncodedData(badData)
	defer bad.Release()
	if err := validateInputArray(bad); err == nil {
		t.Fatal("invalid run boundaries accepted")
	}
}

func TestFixedBinaryLayoutBounds(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	source, err := recordsReader(mem, []resultColumn{{Name: "v", Type: typeBinary}}, [][]Value{{nullValue(typeBinary)}, {nullValue(typeBinary)}, {nullValue(typeBinary)}})
	if err != nil {
		t.Fatal(err)
	}
	rdr, err := resultLayouts{"v": "fixed_size_binary:10485760"}.wrap(context.Background(), mem, source)
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Release()
	var first arrow.RecordBatch
	n := 0
	for rdr.Next() {
		if rdr.RecordBatch().NumRows() != 1 {
			t.Fatal("fixed binary amplification exceeded budget")
		}
		n++
		if first == nil {
			first = rdr.RecordBatch()
			first.Retain()
			defer first.Release()
		}
	}
	if n != 3 || rdr.Err() != nil || !first.Column(0).IsNull(0) {
		t.Fatal(n, rdr.Err())
	}
}
