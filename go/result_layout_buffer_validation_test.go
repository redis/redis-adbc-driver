// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type guardedLayoutSource struct {
	array.RecordReader
	advances, proofs int
	failure          error
}

func (s *guardedLayoutSource) Next() bool                    { s.advances++; return s.RecordReader.Next() }
func (s *guardedLayoutSource) validateBufferedResult() error { s.proofs++; return s.failure }
func TestResultLayoutValidatesOnlyCachedSlices(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "lost_source"}[fail], func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
			b.AppendNull()
			b.AppendNull()
			b.AppendNull()
			a := b.NewArray()
			b.Release()
			schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.BinaryTypes.Binary, Nullable: true}}, nil)
			record := array.NewRecordBatch(schema, []arrow.Array{a}, 3)
			a.Release()
			native, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
			record.Release()
			if err != nil {
				t.Fatal(err)
			}
			source := &guardedLayoutSource{RecordReader: native}
			reader, err := resultLayouts{"v": "fixed_size_binary:10485760"}.wrap(context.Background(), mem, source)
			if err != nil {
				t.Fatal(err)
			}
			if !reader.Next() || reader.RecordBatch().NumRows() != 1 || source.proofs != 0 || source.advances != 1 {
				t.Fatal("fresh source batch duplicated validation", reader.Err(), source.proofs, source.advances)
			}
			if fail {
				source.failure = io.ErrUnexpectedEOF
				if reader.Next() || !errors.Is(reader.Err(), io.ErrUnexpectedEOF) || source.proofs != 1 || source.advances != 1 {
					t.Fatal("cached slice ignored source loss or replayed source", reader.Err(), source.proofs, source.advances)
				}
			} else {
				if !reader.Next() || !reader.Next() || reader.Next() || reader.Err() != nil || source.proofs != 2 || source.advances != 2 {
					t.Fatal("cached proof counts or source ownership changed", reader.Err(), source.proofs, source.advances)
				}
			}
			reader.Release()
			mem.AssertSize(t, 0)
		})
	}
}
