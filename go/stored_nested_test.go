// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestMapUnionInvalidInput(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	fields := []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "s", Type: arrow.BinaryTypes.String, Nullable: true}}
	dt := arrow.DenseUnionOf(fields, []arrow.UnionTypeCode{3, 7})
	valid := inventoryJSON(t, mem, dt, `[[3,9],[7,"s"],[3,null]]`)
	defer valid.Release()
	for _, bad := range []struct {
		name    string
		tags    []int8
		offsets []int32
	}{
		{"negative_tag", []int8{-1, 7, 3}, []int32{0, 0, 1}},
		{"unknown_tag", []int8{4, 7, 3}, []int32{0, 0, 1}},
		{"negative_offset", []int8{3, 7, 3}, []int32{-1, 0, 1}},
		{"past_child", []int8{3, 7, 3}, []int32{2, 0, 1}},
	} {
		t.Run(bad.name, func(t *testing.T) {
			tags := memory.NewBufferBytes(arrow.Int8Traits.CastToBytes(bad.tags))
			defer tags.Release()
			offsets := memory.NewBufferBytes(arrow.Int32Traits.CastToBytes(bad.offsets))
			defer offsets.Release()
			data := array.NewData(dt, 3, []*memory.Buffer{nil, tags, offsets}, valid.Data().Children(), 0, 0)
			defer data.Release()
			a := array.NewDenseUnionData(data)
			defer a.Release()
			if err := validateInputArray(a); err == nil {
				t.Fatal("accepted malformed union")
			}
		})
	}
	ct, err := colTypeFromArrow(arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int64))
	if err != nil {
		t.Fatal(err)
	}
	fieldsWithNullKey := []Value{nullValue(typeString), intValue(typeInt64, 1)}
	if _, err := validateNestedValue(Value{T: ct, rec: &fieldsWithNullKey}); err == nil {
		t.Fatal("accepted NULL key")
	}
	// Tiny buffers declare a huge map extent, which must fail before allocation.
	entries := inventoryJSON(t, mem, arrow.StructOf(arrow.Field{Name: "key", Type: arrow.BinaryTypes.String}, arrow.Field{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true}), `[{"key":"a","value":1}]`)
	defer entries.Release()
	offsets := memory.NewBufferBytes(arrow.Int32Traits.CastToBytes([]int32{0, 1 << 30}))
	defer offsets.Release()
	data := array.NewData(arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int64), 1, []*memory.Buffer{nil, offsets}, []arrow.ArrayData{entries.Data()}, 0, 0)
	defer data.Release()
	badMap := array.NewMapData(data)
	defer badMap.Release()
	if err := validateInputArray(badMap); err == nil {
		t.Fatal("accepted Map offset outside child")
	}
}

func TestMapUnionSchemaAndStoredPolicy(t *testing.T) {
	fields := []structField{{"n", typeInt64, true}, {"s", typeString, true}}
	union, err := nestedType(KindUnion, fields, []arrow.UnionTypeCode{3, 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, codes := range [][]arrow.UnionTypeCode{{3, 3}, {-1, 7}, {3}} {
		if _, err := nestedType(KindUnion, fields, codes); err == nil {
			t.Fatal("accepted bad type codes", codes)
		}
	}
	changed, _ := nestedType(KindUnion, fields, []arrow.UnionTypeCode{7, 3})
	if storedRecordAppendCompatible(changed, union) || storedRecordAppendCompatible(arrayOf(changed), arrayOf(union)) {
		t.Fatal("append allowed code evolution")
	}
	for _, raw := range []string{`{"v":1,"tag":-1,"fields":[null]}`, `{"v":1,"tag":3,"fields":[]}`, `{"v":2,"tag":3,"fields":[null]}`} {
		if _, err := decodeStored(raw, union); err == nil {
			t.Fatal("accepted corrupt union", raw)
		}
	}
	v, err := decodeStored(`{"v":1,"tag":7,"fields":[null]}`, union)
	if err != nil || v.Null || v.I != 7 || !v.fields()[0].Null {
		t.Fatal("lost selected NULL tag", v, err)
	}
	if _, err := Coerce(v, typeString); err == nil {
		t.Fatal("implicitly stringified Union")
	}
	if _, err := fieldSelectType(&FieldSelect{Name: "s"}, union); err == nil {
		t.Fatal("Union exposed inactive child as a field")
	}
	mapType, _ := nestedType(KindMap, []structField{{"key", typeString, false}, {"value", union, true}}, nil)
	if err := retainedType(mapType, 0); err == nil {
		t.Fatal("retained Map admitted")
	}
	for _, typ := range []ColType{union, mapType, arrayOf(mapType)} {
		data, _ := json.Marshal(typ)
		var reloaded ColType
		if err := json.Unmarshal(data, &reloaded); err != nil || reloaded != typ {
			t.Fatal("metadata changed", string(data), err)
		}
	}
	if _, err := decodeStored(`{"v":1,"fields":[null,null]}`, mapType); err == nil {
		t.Fatal("stored NULL key accepted")
	}
	// Order, duplicate keys, tags and selected-child NULLs are part of identity.
	a, _ := decodeStored(`{"v":1,"tag":3,"fields":[null]}`, union)
	if n, ok := compareIn(a, v, utcZone); !ok || n >= 0 {
		t.Fatal("tag order", n, ok)
	}
	ak, _ := joinKey(a)
	vk, _ := joinKey(v)
	if ak == vk {
		t.Fatal("hash key lost tag")
	}
}

func TestMapUnionNormalizationLimits(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	// A dictionary-backed Map repeats one payload without allocating it 65 times.
	b := array.NewMapBuilder(mem, arrow.BinaryTypes.String, arrow.BinaryTypes.String, false)
	b.Append(true)
	b.KeyBuilder().(*array.StringBuilder).Append("key")
	b.ItemBuilder().(*array.StringBuilder).Append(strings.Repeat("x", 1<<20))
	values := b.NewArray()
	b.Release()
	defer values.Release()
	for _, n := range []int{63, 65} {
		indices := array.NewInt8Builder(mem)
		for range n {
			indices.Append(0)
		}
		ix := indices.NewArray()
		indices.Release()
		dict := array.NewDictionaryArray(&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: values.DataType()}, ix, values)
		ix.Release()
		rec := inventoryRecord(dict)
		dict.Release()
		err := validateInputRecord(rec)
		rec.Release()
		if n == 63 && err != nil || n == 65 && (err == nil || !strings.Contains(err.Error(), "decoded string/binary bytes")) {
			t.Fatal(n, err)
		}
	}
	// Each selected union child counts, even if NULL.
	union := inventoryJSON(t, mem, arrow.DenseUnionOf([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, []arrow.UnionTypeCode{9}), `[[9,null],[9,null]]`)
	defer union.Release()
	budget := normalizationBudget{values: 3, payload: 10}
	if err := budget.visit(union, 0); err != nil {
		t.Fatal(err)
	}
	if err := budget.visit(union, 1); err == nil {
		t.Fatal("selected child not budgeted")
	}
}
