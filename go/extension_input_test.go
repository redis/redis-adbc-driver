// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestExtensionRegistryPolicy(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, registered := range []bool{false, true} {
		b := array.NewFixedSizeBinaryBuilder(poisonAllocator{mem}, &arrow.FixedSizeBinaryType{ByteWidth: 16})
		b.Append([]byte("0123456789abcdef"))
		b.AppendNull()
		b.Append([]byte{0, 255, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13})
		storage := b.NewArray()
		b.Release()
		var a arrow.Array = storage
		md := arrow.NewMetadata([]string{"ARROW:extension:name", "ARROW:extension:metadata"}, []string{"arrow.uuid", ""})
		if registered {
			a = array.NewExtensionArrayWithStorage(extensions.NewUUIDType(), storage)
			storage.Release()
			md = arrow.Metadata{}
		}
		rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "u", Type: a.DataType(), Nullable: true, Metadata: md}}, nil), []arrow.Array{a}, int64(a.Len()))
		a.Release()
		source, _ := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
		rec.Release()
		r, err := extensionStorageReader(source)
		if err != nil {
			source.Release()
			t.Fatal(err)
		}
		if !r.Next() {
			t.Fatal(r.Err())
		}
		retained := r.RecordBatch()
		retained.Retain()
		if r.Next() || r.Err() != nil {
			t.Fatal(r.Err())
		}
		r.Release()
		if !arrow.TypeEqual(retained.Column(0).DataType(), &arrow.FixedSizeBinaryType{ByteWidth: 16}) || retained.Schema().Field(0).Metadata.FindKey("ARROW:extension:name") >= 0 {
			t.Fatal(retained.Schema())
		}
		if string(retained.Column(0).(*array.FixedSizeBinary).Value(0)) != "0123456789abcdef" || !retained.Column(0).IsNull(1) {
			t.Fatal(retained)
		}
		retained.Release()
	}
	for _, c := range []struct {
		name, metadata string
		dt             arrow.DataType
	}{
		{"custom.semantic", "", arrow.BinaryTypes.String},
		{"arrow.uuid", "", &arrow.FixedSizeBinaryType{ByteWidth: 15}},
		{"arrow.uuid", `{"version":2}`, &arrow.FixedSizeBinaryType{ByteWidth: 16}},
		{"arrow.json", "", arrow.BinaryTypes.Binary},
		{"arrow.json", "null", arrow.BinaryTypes.String},
		{"arrow.json", "[", arrow.BinaryTypes.String},
		{"arrow.json", strings.Repeat("x", maxExtensionMetadata+1), arrow.BinaryTypes.String},
	} {
		schema := arrow.NewSchema([]arrow.Field{{Name: "x", Type: c.dt, Metadata: arrow.NewMetadata([]string{"ARROW:extension:name", "ARROW:extension:metadata"}, []string{c.name, c.metadata})}}, nil)
		source, _ := array.NewRecordReader(schema, nil)
		if r, err := extensionStorageReader(source); err == nil {
			r.Release()
			t.Fatal("unsafe extension accepted", c.name)
		} else {
			source.Release()
		}
	}
	st := &statementImpl{}
	if got, _ := st.GetOption(context.Background(), OptionStringExtensionPolicy); got != "reject" {
		t.Fatal(got)
	}
	if err := st.SetOption(context.Background(), OptionStringExtensionPolicy, "auto"); !isStatus(err, adbc.StatusInvalidArgument) {
		t.Fatal(err)
	}
}

func TestExtensionJSONValues(t *testing.T) {
	for _, valid := range []bool{false, true} {
		jsonText := `{"a":[1,null],"text":"hello"}`
		if !valid {
			jsonText = `{"broken":`
		}
		b := array.NewStringBuilder(memory.DefaultAllocator)
		b.Append(jsonText)
		b.AppendNull()
		a := b.NewArray()
		b.Release()
		dt, err := extensions.NewJSONType(arrow.BinaryTypes.String)
		if err != nil {
			t.Fatal(err)
		}
		ext := array.NewExtensionArrayWithStorage(dt, a)
		a.Release()
		rec := inventoryRecord(ext)
		ext.Release()
		source, _ := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
		rec.Release()
		r, err := extensionStorageReader(source)
		if err != nil {
			source.Release()
			t.Fatal(err)
		}
		if r.Next() != valid {
			t.Fatal(valid, r.Err())
		}
		if valid {
			if r.RecordBatch().Column(0).(*array.String).Value(0) != jsonText || !r.RecordBatch().Column(0).IsNull(1) {
				t.Fatal(r.RecordBatch())
			}
		} else if !isStatus(r.Err(), adbc.StatusInvalidArgument) {
			t.Fatal(r.Err())
		}
		r.Release()
	}
}

func TestExtensionPolicyRecheckedAfterSchema(t *testing.T) {
	a := inventoryJSON(t, memory.DefaultAllocator, arrow.BinaryTypes.String, `["{}"]`)
	defer a.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "x", Type: a.DataType(), Metadata: arrow.NewMetadata([]string{"ARROW:extension:name"}, []string{"arrow.json"})}}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	defer rec.Release()
	source, _ := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
	st := &statementImpl{params: source, extensionPolicy: "storage"}
	defer st.clearParams()
	if err := st.prepareExtensionInput(); err != nil {
		t.Fatal(err)
	}
	if st.params.Schema().Field(0).Metadata.FindKey("ARROW:extension:name") >= 0 {
		t.Fatal("normalization not applied")
	}
	if err := st.SetOption(context.Background(), OptionStringExtensionPolicy, "reject"); err != nil {
		t.Fatal(err)
	}
	if _, err := colTypeFromField(st.params.Schema().Field(0)); err == nil {
		t.Fatal("policy change reused a normalized extension schema")
	}
}

func TestExtensionMalformedMetadata(t *testing.T) {
	for _, keys := range [][]string{
		{"ARROW:extension:metadata"},
		{"ARROW:extension:name", "ARROW:extension:name"},
		{"ARROW:extension:name", "ARROW:extension:metadata", "ARROW:extension:metadata"},
		{"ARROW:extension:name", "ARROW:extension:future"},
	} {
		values := make([]string, len(keys))
		for i, key := range keys {
			if key == "ARROW:extension:name" {
				values[i] = "arrow.json"
			}
		}
		schema := arrow.NewSchema([]arrow.Field{{Name: "x", Type: arrow.BinaryTypes.String, Metadata: arrow.NewMetadata(keys, values)}}, nil)
		source, _ := array.NewRecordReader(schema, nil)
		if r, err := extensionStorageReader(source); err == nil {
			r.Release()
			t.Fatal("malformed extension metadata accepted", keys)
		} else {
			source.Release()
		}
	}
}
