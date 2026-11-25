// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
//go:build cgo

package redis

import (
	"errors"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/cdata"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// cDataRoundTrip exports a through the Arrow C Data interface and imports it
// again, giving the buffer layout that bound and ingested C input has.
func cDataRoundTrip(t *testing.T, a arrow.Array) arrow.Array {
	t.Helper()
	var carr cdata.CArrowArray
	var csch cdata.CArrowSchema
	cdata.ExportArrowArray(a, &carr, &csch)
	_, imported, err := cdata.ImportCArray(&carr, &csch)
	if err != nil {
		t.Fatal(err)
	}
	return imported
}

// bufferlessRunEnds counts run-end-encoded nodes that have no parent buffer
// slot, the C Data shape that once needed a validation placeholder.
func bufferlessRunEnds(data arrow.ArrayData) int {
	n := 0
	if data.DataType().ID() == arrow.RUN_END_ENCODED && len(data.Buffers()) == 0 {
		n++
	}
	for _, child := range data.Children() {
		n += bufferlessRunEnds(child)
	}
	return n
}

// Upstream apache/arrow-go#1421 (in v18.9.0) validates run-end-encoded arrays
// without their nil buffer slot. These C imports must reach ValidateFull with
// no driver adaptation, at the top level and nested, and keep the driver's
// accept/reject outcome and status. NULL values children still need it.
func TestCImportedRunEndNestedValidation(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	ends := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int16, `[2,5,7]`)
	defer ends.Release()
	values := inventoryJSON(t, mem, arrow.BinaryTypes.String, `["a",null,"c"]`)
	defer values.Release()
	ree := array.NewRunEndEncodedArray(ends, values, 7, 0)
	defer ree.Release()
	nullValues := array.NewNull(3)
	defer nullValues.Release()
	reeNull := array.NewRunEndEncodedArray(ends, nullValues, 7, 0)
	defer reeNull.Release()

	valid := map[string]func() arrow.Array{
		"top":         func() arrow.Array { return array.NewSlice(ree, 1, 6) },
		"null_values": func() arrow.Array { return array.NewSlice(reeNull, 1, 6) },
		"struct": func() arrow.Array {
			a, err := array.NewStructArrayWithNulls([]arrow.Array{ree}, []string{"r"}, nil, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			return a
		},
		// The struct itself is not sliced: that C round trip is #460.
		"struct_of_sliced_null_values": func() arrow.Array {
			child := array.NewSlice(reeNull, 1, 6)
			defer child.Release()
			a, err := array.NewStructArrayWithNulls([]arrow.Array{child}, []string{"r"}, nil, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			return a
		},
		"list": func() arrow.Array {
			offsets := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int32, `[0,2,5,7]`)
			defer offsets.Release()
			data := array.NewData(arrow.ListOf(ree.DataType()), 3, []*memory.Buffer{nil, offsets.Data().Buffers()[1]}, []arrow.ArrayData{ree.Data()}, 0, 0)
			defer data.Release()
			return array.NewListData(data)
		},
		"map_values": func() arrow.Array {
			keys := inventoryJSON(t, mem, arrow.BinaryTypes.String, `["k1","k2","k3","k4","k5","k6","k7"]`)
			defer keys.Release()
			offsets := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int32, `[0,3,7]`)
			defer offsets.Release()
			dt := arrow.MapOf(keys.DataType(), ree.DataType())
			entries := array.NewData(dt.Elem(), 7, []*memory.Buffer{nil}, []arrow.ArrayData{keys.Data(), ree.Data()}, 0, 0)
			defer entries.Release()
			data := array.NewData(dt, 2, []*memory.Buffer{nil, offsets.Data().Buffers()[1]}, []arrow.ArrayData{entries}, 0, 0)
			defer data.Release()
			return array.NewMapData(data)
		},
		"sparse_union": func() arrow.Array {
			codes := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int8, `[0,0,0,0,0,0,0]`)
			defer codes.Release()
			a, err := array.NewSparseUnionFromArraysWithFields(codes, []arrow.Array{ree}, []string{"r"})
			if err != nil {
				t.Fatal(err)
			}
			return a
		},
		"dense_union": func() arrow.Array {
			codes := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int8, `[0,0,0]`)
			defer codes.Release()
			offsets := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int32, `[0,3,6]`)
			defer offsets.Release()
			a, err := array.NewDenseUnionFromArraysWithFields(codes, offsets, []arrow.Array{ree}, []string{"r"})
			if err != nil {
				t.Fatal(err)
			}
			return a
		},
	}
	for name, build := range valid {
		t.Run("valid_"+name, func(t *testing.T) {
			source := build()
			imported := cDataRoundTrip(t, source)
			source.Release()
			defer imported.Release()
			if bufferlessRunEnds(imported.Data()) == 0 {
				t.Fatal("C import no longer omits the run-end buffer slot; this case tests nothing")
			}
			if err := validateInputArray(imported); err != nil {
				t.Fatal(err)
			}
			if bufferlessRunEnds(imported.Data()) == 0 {
				t.Fatal("validation modified caller-owned buffers")
			}
		})
	}

	// Each invalid run-end child is rejected by Arrow's full validation, with
	// the same message path and an InvalidArgument status with no SQLSTATE from
	// ingestion, alone and inside a struct or list.
	for _, c := range []struct{ name, ends, values, msg string }{
		{"zero_run_end", `[0,4]`, `["x","y"]`, "first run end must be greater than 0, got 0"},
		{"decreasing", `[3,2,4]`, `["x","y","z"]`, "run end at position 1 (2) must be strictly greater than previous run end (3)"},
		{"short", `[1,3]`, `["x","y"]`, "last run end is 3 but it should cover 4"},
	} {
		badEnds := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int16, c.ends)
		badValues := inventoryJSON(t, mem, arrow.BinaryTypes.String, c.values)
		data := array.NewData(arrow.RunEndEncodedOf(badEnds.DataType(), badValues.DataType()), 4, nil, []arrow.ArrayData{badEnds.Data(), badValues.Data()}, 0, 0)
		badEnds.Release()
		badValues.Release()
		bad := array.NewRunEndEncodedData(data)
		data.Release()
		structData := array.NewData(arrow.StructOf(arrow.Field{Name: "bad", Type: bad.DataType(), Nullable: true}), 4, []*memory.Buffer{nil}, []arrow.ArrayData{bad.Data()}, 0, 0)
		inStruct := array.NewStructData(structData)
		structData.Release()
		offsets := inventoryJSON(t, mem, arrow.PrimitiveTypes.Int32, `[0,4]`)
		listData := array.NewData(arrow.ListOf(bad.DataType()), 1, []*memory.Buffer{nil, offsets.Data().Buffers()[1]}, []arrow.ArrayData{bad.Data()}, 0, 0)
		offsets.Release()
		inList := array.NewListData(listData)
		listData.Release()
		for _, shape := range []struct {
			name, path string
			a          arrow.Array
		}{{"top", "", bad}, {"struct", `field "bad": `, inStruct}, {"list", "list values: ", inList}} {
			t.Run("invalid_"+c.name+"_"+shape.name, func(t *testing.T) {
				imported := cDataRoundTrip(t, shape.a)
				defer imported.Release()
				if bufferlessRunEnds(imported.Data()) == 0 {
					t.Fatal("C import no longer omits the run-end buffer slot; this case tests nothing")
				}
				want := shape.path + "arrow/array: " + c.msg
				err := validateInputArray(imported)
				if err == nil || err.Error() != want {
					t.Fatalf("invalid run ends %s: got %v, want %q", c.ends, err, want)
				}
				rows := make([][]Value, imported.Len())
				for i := range rows {
					rows[i] = make([]Value, 1)
				}
				err = ingestColumn(imported, typeString, utcZone, rows, 0)
				var ae adbc.Error
				if !errors.As(err, &ae) || ae.Code != adbc.StatusInvalidArgument || ae.Msg != "[redis] invalid Arrow input: "+want || ae.SqlState != [5]byte{} {
					t.Fatalf("ingest: %#v", err)
				}
			})
		}
		bad.Release()
		inStruct.Release()
		inList.Release()
	}
}
