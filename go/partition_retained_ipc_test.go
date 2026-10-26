// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestRetainedPartitionIPCBoundedReplay(t *testing.T) {
	payload := retainedIPCFixture(t)
	frame, err := preflightRetainedIPC(payload)
	if err != nil {
		t.Fatal(err)
	}
	record, err := decodeRetainedIPC(payload, frame.schema, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()
	if got := record.Column(0).(*array.Int64).Int64Values(); got[0] != 1<<53+1 || got[1] != 1<<63-1 || got[2] != -1 {
		t.Fatal(got)
	}
	b := record.Column(1).(*array.Binary)
	if !bytes.Equal(b.Value(0), []byte{0, 255}) || !b.IsNull(1) || string(b.Value(2)) != "exact\x00" {
		t.Fatal("binary/NULL mismatch")
	}
	for _, test := range []struct {
		schema string
		rows   int64
	}{{retainedSHA(nil), 3}, {frame.schema, 4}} {
		if r, err := decodeRetainedIPC(payload, test.schema, test.rows); r != nil || !isStatus(err, adbc.StatusInvalidData) {
			t.Fatal("invalid manifest accepted", err)
		}
	}
}

func retainedTestEncode(t *testing.T, record arrow.RecordBatch, options ...ipc.Option) []byte {
	t.Helper()
	var output bytes.Buffer
	options = append(options, ipc.WithSchema(record.Schema()))
	w := ipc.NewWriter(&output, options...)
	if err := w.Write(record); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestRetainedPartitionIPCOutputTypesAndEmpty(t *testing.T) {
	types := []ColType{typeNull, typeBool, typeInt16, typeInt32, typeInt64, typeFloat32, typeFloat64, typeString, typeBinary, typeDate, timeType(arrow.Microsecond), timestampType(arrow.Microsecond, ""), timestampType(arrow.Microsecond, "UTC"), typeInterval, decimalType(38, 4)}
	cols := make([]resultColumn, len(types))
	row := make([]Value, len(types))
	for i, typ := range types {
		cols[i] = resultColumn{Name: "value", Type: typ}
		row[i] = nullValue(typ)
	}
	for _, rows := range [][][]Value{nil, {row}} {
		record, err := buildRecord(memory.DefaultAllocator, cols, rows)
		if err != nil {
			t.Fatal(err)
		}
		payload := retainedTestEncode(t, record)
		record.Release()
		frame, err := preflightRetainedIPC(payload)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeRetainedIPC(payload, frame.schema, int64(len(rows)))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.NumCols() != int64(len(types)) || decoded.NumRows() != int64(len(rows)) {
			t.Fatal("shape changed")
		}
		decoded.Release()
	}
	schema := arrow.NewSchema([]arrow.Field{{Name: "nested", Type: arrow.ListOf(arrow.StructOf(arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true})), Nullable: true}}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	list := builder.Field(0).(*array.ListBuilder)
	list.Append(true)
	child := list.ValueBuilder().(*array.StructBuilder)
	child.Append(true)
	child.FieldBuilder(0).(*array.Int64Builder).Append(1<<53 + 1)
	list.AppendNull()
	record := builder.NewRecordBatch()
	defer record.Release()
	payload := retainedTestEncode(t, record)
	frame, err := preflightRetainedIPC(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRetainedIPC(payload, frame.schema, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Release()
	if !array.RecordEqual(record, decoded) {
		t.Fatal("nested data changed")
	}
}

func TestRetainedPartitionIPCRejectsCompressionAndFraming(t *testing.T) {
	b := array.NewInt64Builder(memory.DefaultAllocator)
	b.Append(42)
	a := b.NewArray()
	b.Release()
	defer a.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	defer rec.Release()
	for _, codec := range []ipc.Option{ipc.WithZstd(), ipc.WithLZ4()} {
		if _, err := preflightRetainedIPC(retainedTestEncode(t, rec, codec)); !isStatus(err, adbc.StatusInvalidData) {
			t.Fatal("compression accepted", err)
		}
	}
	payload := retainedIPCFixture(t)
	for i := 0; i < len(payload); i++ {
		if _, err := preflightRetainedIPC(payload[:i]); err == nil {
			t.Fatalf("truncation accepted at %d", i)
		}
	}
	for _, mutate := range []func([]byte){
		func(p []byte) { binary.LittleEndian.PutUint32(p[4:], ^uint32(0)) },
		func(p []byte) { binary.LittleEndian.PutUint32(p[8:], ^uint32(0)) },
		func(p []byte) { p[len(p)-1] = 1 },
	} {
		bad := bytes.Clone(payload)
		mutate(bad)
		if _, err := preflightRetainedIPC(bad); err == nil {
			t.Fatal("bad framing accepted")
		}
	}
	if _, err := preflightRetainedIPC(append(bytes.Clone(payload), 0)); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestRetainedPartitionIPCRejectsOversizedMetadataBeforeDecode(t *testing.T) {
	payload := retainedIPCFixture(t)
	frame, err := preflightRetainedIPC(payload)
	if err != nil {
		t.Fatal(err)
	}
	_ = frame
	cases := map[string]func([]byte){
		"schema-vector": func(p []byte) {
			m := p[8 : 8+retainedU32(p, 4)]
			s := retainedTable(m, retainedU32(m, 0)).table(2)
			at := retainedIndirect(m, s.field(1, 4))
			binary.LittleEndian.PutUint32(m[at:], ^uint32(0))
		},
		"row-count": func(p []byte) {
			offset := 8 + retainedU32(p, 4)
			m := p[offset+8 : offset+8+retainedU32(p, offset+4)]
			r := retainedTable(m, retainedU32(m, 0)).table(2)
			binary.LittleEndian.PutUint64(m[r.field(0, 8):], 1<<62)
		},
		"body-length": func(p []byte) {
			offset := 8 + retainedU32(p, 4)
			m := p[offset+8 : offset+8+retainedU32(p, offset+4)]
			msg := retainedTable(m, retainedU32(m, 0))
			binary.LittleEndian.PutUint64(m[msg.field(3, 8):], 1<<62)
		},
		"buffer-offset": func(p []byte) {
			offset := 8 + retainedU32(p, 4)
			m := p[offset+8 : offset+8+retainedU32(p, offset+4)]
			r := retainedTable(m, retainedU32(m, 0)).table(2)
			at, _ := r.vector(2, 16, 100)
			binary.LittleEndian.PutUint64(m[at:], 1<<62)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := bytes.Clone(payload)
			mutate(p)
			if _, err := preflightRetainedIPC(p); err == nil {
				t.Fatal("excessive metadata accepted")
			}
		})
	}
}

func FuzzRetainedPartitionIPC(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{255, 255, 255, 255, 0, 0, 0, 0})
	// A valid stream seeds structural mutations beyond the framing header.
	b := array.NewInt64Builder(memory.DefaultAllocator)
	b.Append(1<<53 + 1)
	a := b.NewArray()
	b.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	a.Release()
	var out bytes.Buffer
	w := ipc.NewWriter(&out, ipc.WithSchema(schema))
	_ = w.Write(rec)
	_ = w.Close()
	rec.Release()
	f.Add(out.Bytes())
	f.Fuzz(func(t *testing.T, p []byte) {
		frame, err := preflightRetainedIPC(p)
		if err != nil {
			return
		}
		r, err := decodeRetainedIPC(p, frame.schema, frame.rows)
		if err == nil {
			r.Release()
		}
	})
}
