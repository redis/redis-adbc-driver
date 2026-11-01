// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type layoutInventoryCase struct {
	name  string
	input arrow.Array
	want  arrow.Array // nil means unsupported, even for an empty input
}

func inventoryJSON(t *testing.T, mem memory.Allocator, dt arrow.DataType, json string) arrow.Array {
	t.Helper()
	b := array.NewBuilder(mem, dt)
	defer b.Release()
	if err := b.UnmarshalJSON([]byte(json)); err != nil {
		t.Fatal(dt, err)
	}
	return b.NewArray()
}

// Expectations use independent, ordinary Arrow builders, not valueAt or the
// normalization implementation. Every caller owns and releases both arrays.
func layoutInventoryCases(t *testing.T, mem memory.Allocator) []layoutInventoryCase {
	t.Helper()
	var cases []layoutInventoryCase
	add := func(name string, input arrow.Array, dt arrow.DataType, json string) {
		cases = append(cases, layoutInventoryCase{name, input, inventoryJSON(t, mem, dt, json)})
	}
	list := arrow.ListOf(arrow.PrimitiveTypes.Int64)
	const fixedJSON = `[[91,92],[7,null],null,[-3,4]]`
	add("fixed_size_list", inventoryJSON(t, mem, arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Int64), fixedJSON), list, fixedJSON)
	nested := arrow.ListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64))
	const nestedJSON = `[[[1],[]],null,[null,[2,null]]]`
	add("nested_fixed_size_list", inventoryJSON(t, mem, arrow.FixedSizeListOf(2, list), nestedJSON), nested, nestedJSON)
	for _, large := range []bool{false, true} {
		// The two view builders expose different offset argument widths.
		name := "list_view"
		var a arrow.Array
		if !large {
			b := array.NewListViewBuilder(mem, arrow.PrimitiveTypes.Int64)
			b.ValueBuilder().(*array.Int64Builder).AppendValues([]int64{7, 0, 8, 9}, []bool{true, false, true, true})
			b.AppendDimensions(3, 1)
			b.AppendDimensions(0, 2)
			b.AppendNull()
			b.AppendDimensions(0, 0)
			b.AppendDimensions(1, 2)
			a = b.NewArray()
			b.Release()
		} else {
			name = "large_list_view"
			b := array.NewLargeListViewBuilder(mem, arrow.PrimitiveTypes.Int64)
			b.ValueBuilder().(*array.Int64Builder).AppendValues([]int64{7, 0, 8, 9}, []bool{true, false, true, true})
			b.AppendDimensions(3, 1)
			b.AppendDimensions(0, 2)
			b.AppendNull()
			b.AppendDimensions(0, 0)
			b.AppendDimensions(1, 2)
			a = b.NewArray()
			b.Release()
		}
		add(name, a, list, `[[9],[7,null],null,[],[null,8]]`)
	}
	for _, dt := range []arrow.DataType{arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32, arrow.PrimitiveTypes.Int64} {
		ends := inventoryJSON(t, mem, dt, `[2,5,7]`)
		values := inventoryJSON(t, mem, arrow.BinaryTypes.String, `["long string that must own its bytes",null,"last"]`)
		a := array.NewRunEndEncodedArray(ends, values, 7, 0)
		ends.Release()
		values.Release()
		add("run_end_"+dt.Name(), a, arrow.BinaryTypes.String, `["long string that must own its bytes","long string that must own its bytes",null,null,null,"last","last"]`)
	}
	ends := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int16, `[2,3,5]`)
	values := inventoryJSON(t, mem, list, `[[1,null],null,[]]`)
	a := array.NewRunEndEncodedArray(ends, values, 5, 0)
	ends.Release()
	values.Release()
	add("run_end_list", a, list, `[[1,null],[1,null],null,[],[]]`)
	ends = inventoryJSON(t, mem, arrow.PrimitiveTypes.Int16, `[2,5,7]`)
	values = inventoryJSON(t, mem, arrow.BinaryTypes.String, `["long string that must own its bytes",null,"last"]`)
	encodedData := array.NewData(arrow.RunEndEncodedOf(ends.DataType(), values.DataType()), 7, nil, []arrow.ArrayData{ends.Data(), values.Data()}, 0, 0)
	offsets := memory.NewBufferBytes(arrow.Int32Traits.CastToBytes([]int32{0, 2, 5, 7}))
	listData := array.NewData(arrow.ListOf(encodedData.DataType()), 3, []*memory.Buffer{nil, offsets}, []arrow.ArrayData{encodedData}, 0, 0)
	add("list_of_c_run_end", array.NewListData(listData), arrow.ListOf(arrow.BinaryTypes.String), `[["long string that must own its bytes","long string that must own its bytes"],[null,null,null],["last","last"]]`)
	ends.Release()
	values.Release()
	encodedData.Release()
	offsets.Release()
	listData.Release()
	mb := array.NewMapBuilder(mem, arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int64, false)
	mb.Append(true)
	mb.KeyBuilder().(*array.StringBuilder).AppendValues([]string{"duplicate", "duplicate"}, nil)
	mb.ItemBuilder().(*array.Int64Builder).AppendValues([]int64{1, 0}, []bool{true, false})
	mb.AppendNull()
	mb.AppendEmptyValue()
	cases = append(cases, layoutInventoryCase{name: "map", input: mb.NewArray()})
	mb.Release()
	fields := []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "s", Type: arrow.BinaryTypes.String, Nullable: true}}
	codes := []arrow.UnionTypeCode{3, 7}
	for _, dt := range []arrow.DataType{arrow.SparseUnionOf(fields, codes), arrow.DenseUnionOf(fields, codes)} {
		cases = append(cases, layoutInventoryCase{name: dt.Name(), input: inventoryJSON(t, mem, dt, `[[3,9],[7,"text"],[3,null]]`)})
	}
	return cases
}

func releaseInventory(cases []layoutInventoryCase) {
	for _, c := range cases {
		c.input.Release()
		if c.want != nil {
			c.want.Release()
		}
	}
}

func inventoryRecord(a arrow.Array) arrow.RecordBatch {
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "v", Type: a.DataType(), Nullable: true}}, nil), []arrow.Array{a}, int64(a.Len()))
}

func assertInventoryReader(t *testing.T, reader array.RecordReader, want arrow.RecordBatch) {
	t.Helper()
	if !reader.Schema().Equal(want.Schema()) {
		t.Fatal("schema changed", reader.Schema(), want.Schema())
	}
	var offset int64
	for reader.Next() {
		r := reader.RecordBatch()
		if offset+r.NumRows() > want.NumRows() {
			t.Fatal("extra rows")
		}
		expected := want.NewSlice(offset, offset+r.NumRows())
		equal := array.RecordEqual(expected, r)
		expected.Release()
		if !equal {
			t.Fatal("values changed", offset, r)
		}
		offset += r.NumRows()
	}
	if reader.Err() != nil || offset != want.NumRows() {
		t.Fatal(offset, want.NumRows(), reader.Err())
	}
}

func TestArrowLayoutInventory(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	cases := layoutInventoryCases(t, poisonAllocator{mem})
	defer releaseInventory(cases)
	checkCanonicalRoundTrips(t, mem, cases)
}

func checkCanonicalRoundTrips(t *testing.T, mem memory.Allocator, cases []layoutInventoryCase) {
	t.Helper()
	for _, c := range cases {
		for _, bounds := range [][2]int64{{0, int64(c.input.Len())}, {1, int64(c.input.Len())}, {0, 0}} {
			t.Run(fmt.Sprintf("%s/%d:%d", c.name, bounds[0], bounds[1]), func(t *testing.T) {
				a := array.NewSlice(c.input, bounds[0], bounds[1])
				defer a.Release()
				if err := validateInputArray(a); err != nil {
					t.Fatal("invalid fixture", err)
				}
				rec := inventoryRecord(a)
				defer rec.Release()
				meta, err := metaFromArrow(defaultSchema, "inventory", rec.Schema())
				if c.want == nil {
					if !isStatus(err, adbc.StatusNotImplemented) || !strings.Contains(err.Error(), "unsupported Arrow type") {
						t.Fatal("unsupported layout accepted", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				rows := make([][]Value, a.Len())
				for i := range rows {
					rows[i] = make([]Value, 1)
				}
				if err := ingestColumn(a, meta.Columns[0].Type, utcZone, rows, 0); err != nil {
					t.Fatal(err)
				}
				for i := range rows {
					// SQL NULL is an absent HASH field, not an encoded empty cell.
					if rows[i][0].Null {
						continue
					}
					v, err := decodeStored(encodeStored(rows[i][0]), meta.Columns[0].Type)
					if err != nil {
						t.Fatal(err)
					}
					rows[i][0] = v
				}
				out, err := buildRecord(mem, []resultColumn{{Name: "v", Type: meta.Columns[0].Type}}, rows)
				if err != nil {
					t.Fatal(err)
				}
				defer out.Release()
				want := array.NewSlice(c.want, bounds[0], bounds[1])
				defer want.Release()
				expected := inventoryRecord(want)
				defer expected.Release()
				if !array.RecordEqual(out, expected) {
					t.Fatal("normalization changed values/schema", out, expected)
				}
				for _, format := range []string{"stream", "file"} {
					path := filepath.Join(t.TempDir(), "out.arrow")
					n, err := writeIPCOutput(context.Background(), path, format, mem, func() (array.RecordReader, error) {
						return array.NewRecordReader(out.Schema(), []arrow.RecordBatch{out})
					})
					if err != nil || n != out.NumRows() {
						t.Fatal(n, err)
					}
					reader, err := openIPCInput(context.Background(), path, format, mem)
					if err != nil {
						t.Fatal(err)
					}
					assertInventoryReader(t, reader, expected)
					reader.Release()
				}
			})
		}
	}
}

// Tiny buffers declare impossible dimensions: validation must reject them
// before offset arithmetic, child access, or allocation from their dimensions.
func TestArrowLayoutInvalidDimensions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	child := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int64, `[1,2]`)
	defer child.Release()
	for _, c := range []struct {
		name         string
		offset, size int64
	}{
		{"negative_offset", -1, 1}, {"negative_size", 0, -1}, {"past_child", 2, 1},
		{"offset_overflow", math.MaxInt64, 1}, {"size_overflow", 1, math.MaxInt64},
	} {
		t.Run(c.name, func(t *testing.T) {
			offsets := memory.NewBufferBytes(arrow.Int64Traits.CastToBytes([]int64{c.offset}))
			defer offsets.Release()
			sizes := memory.NewBufferBytes(arrow.Int64Traits.CastToBytes([]int64{c.size}))
			defer sizes.Release()
			data := array.NewData(arrow.LargeListViewOf(arrow.PrimitiveTypes.Int64), 1, []*memory.Buffer{nil, offsets, sizes}, []arrow.ArrayData{child.Data()}, 0, 0)
			defer data.Release()
			a := array.NewLargeListViewData(data)
			defer a.Release()
			rows := [][]Value{{}}
			if err := ingestColumn(a, arrayOf(typeInt64), utcZone, rows, 0); !isStatus(err, adbc.StatusInvalidArgument) {
				t.Fatal("invalid dimensions accepted", err)
			}
		})
	}
	t.Run("fixed_width", func(t *testing.T) {
		data := array.NewData(arrow.FixedSizeListOf(math.MaxInt32, arrow.PrimitiveTypes.Int64), 1, []*memory.Buffer{nil}, []arrow.ArrayData{child.Data()}, 0, 0)
		defer data.Release()
		a := array.NewFixedSizeListData(data)
		defer a.Release()
		if err := validateInputArray(a); err == nil {
			t.Fatal("oversized fixed-width child accepted")
		}
	})
	for _, endsJSON := range []string{`[0,5]`, `[3,2]`, `[1,3]`} {
		t.Run("run_ends_"+endsJSON, func(t *testing.T) {
			ends := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int16, endsJSON)
			defer ends.Release()
			data := array.NewData(arrow.RunEndEncodedOf(ends.DataType(), child.DataType()), 4, nil, []arrow.ArrayData{ends.Data(), child.Data()}, 0, 0)
			defer data.Release()
			a := array.NewRunEndEncodedData(data)
			defer a.Release()
			// The adapter must also validate invalid C run-end data inside a struct.
			st := arrow.StructOf(arrow.Field{Name: "bad", Type: a.DataType(), Nullable: true})
			nestedData := array.NewData(st, 4, []*memory.Buffer{nil}, []arrow.ArrayData{data}, 0, 0)
			defer nestedData.Release()
			nested := array.NewStructData(nestedData)
			defer nested.Release()
			if err := validateInputArray(nested); err == nil {
				t.Fatal("invalid nested run ends accepted")
			}
		})
	}
}

func TestArrowLayoutCopiedValues(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	cases := layoutInventoryCases(t, poisonAllocator{mem})
	for _, c := range cases {
		func() {
			if c.want == nil {
				c.input.Release()
				return
			}
			defer c.want.Release()
			ct, err := colTypeFromArrow(c.input.DataType())
			if err != nil {
				t.Fatal(err)
			}
			rows := make([][]Value, c.input.Len())
			for i := range rows {
				rows[i] = make([]Value, 1)
			}
			err = ingestColumn(c.input, ct, utcZone, rows, 0)
			c.input.Release() // overwrite all original buffers before building output
			if err != nil {
				t.Fatal(err)
			}
			out, err := buildRecord(mem, []resultColumn{{Name: "v", Type: ct}}, rows)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Release()
			expected := inventoryRecord(c.want)
			defer expected.Release()
			if !array.RecordEqual(out, expected) {
				t.Fatal("released source changed values", c.name, out, expected)
			}
		}()
	}
}

func TestArrowLayoutExpansionBudget(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	// One physical run describes over a million logical rows. The rejected
	// case must never allocate a million Value rows just to discover the limit.
	for _, n := range []int{maxNormalizedInputValues, maxNormalizedInputValues + 1} {
		ends := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int64, fmt.Sprintf("[%d]", n))
		values := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int64, `[7]`)
		a := array.NewRunEndEncodedArray(ends, values, n, 0)
		ends.Release()
		values.Release()
		rec := inventoryRecord(a)
		a.Release()
		err := validateInputRecord(rec)
		rec.Release()
		if n == maxNormalizedInputValues && err != nil || n > maxNormalizedInputValues && (err == nil || !strings.Contains(err.Error(), "decoded values")) {
			t.Fatal(n, err)
		}
	}
	// Payload cost counts each decoded repetition, not only the stored string.
	for _, n := range []int{64, 65} {
		ends := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int16, fmt.Sprintf("[%d]", n))
		b := array.NewStringBuilder(mem)
		b.Append(strings.Repeat("x", 1<<20))
		values := b.NewArray()
		b.Release()
		a := array.NewRunEndEncodedArray(ends, values, n, 0)
		ends.Release()
		values.Release()
		rec := inventoryRecord(a)
		a.Release()
		err := validateInputRecord(rec)
		rec.Release()
		if n == 64 && err != nil || n == 65 && (err == nil || !strings.Contains(err.Error(), "decoded string/binary bytes")) {
			t.Fatal(n, err)
		}
	}
	// Overlapping views reference a small child buffer many times.
	b := array.NewListViewBuilder(mem, arrow.PrimitiveTypes.Int64)
	for range 1024 {
		b.ValueBuilder().(*array.Int64Builder).Append(1)
	}
	for range 1024 {
		b.AppendDimensions(0, 1024)
	}
	a := b.NewArray()
	b.Release()
	rec := inventoryRecord(a)
	a.Release()
	defer rec.Release()
	if err := validateInputRecord(rec); err == nil || !strings.Contains(err.Error(), "decoded values") {
		t.Fatal("shared-view expansion accepted", err)
	}
}

func TestCImportedNullValidation(t *testing.T) {
	data := array.NewData(arrow.Null, 3, nil, nil, 3, 0)
	defer data.Release()
	a := array.NewNullData(data)
	defer a.Release()
	for _, bounds := range [][2]int64{{0, 3}, {1, 3}, {0, 0}} {
		sliced := array.NewSlice(a, bounds[0], bounds[1])
		if err := validateInputArray(sliced); err != nil {
			t.Fatal(err)
		}
		sliced.Release()
	}
	if len(a.Data().Buffers()) != 0 {
		t.Fatal("mutated caller's Null buffers")
	}
	st := arrow.StructOf(arrow.Field{Name: "null", Type: arrow.Null, Nullable: true})
	nestedData := array.NewData(st, 3, []*memory.Buffer{nil}, []arrow.ArrayData{data}, 0, 0)
	defer nestedData.Release()
	nested := array.NewStructData(nestedData)
	defer nested.Release()
	if err := validateInputArray(nested); err != nil {
		t.Fatal(err)
	}
}
