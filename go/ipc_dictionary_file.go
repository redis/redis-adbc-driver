// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"os"
	"strings"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const maxDictionaryFileBytes = 256 << 20
const maxDictionaryFileCells = 16 << 20
const maxDictionaryFileBatches = 65536
const maxDictionaryFilePayload = 64 << 20

type dictionarySpoolWriter struct {
	file  *os.File
	bytes int64
}

func (w *dictionarySpoolWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > maxDictionaryFileBytes-w.bytes {
		return 0, errorf(adbc.StatusInvalidState, "dictionary IPC file exceeds 256 MiB staging budget")
	}
	n, err := w.file.Write(p)
	w.bytes += int64(n)
	return n, err
}

// Stage the finite result as a private IPC stream while collecting one bounded
// dictionary per column. Replay remaps indices without changing the int32 schema.
// Ownership of src is consumed on every path. No source batches remain in RAM.
func prepareDictionaryFile(ctx context.Context, mem memory.Allocator, src array.RecordReader) (out array.RecordReader, err error) {
	defer src.Release()
	f, err := os.CreateTemp("", "redis-adbc-dictionary-*.arrow")
	if err != nil {
		return nil, errorf(adbc.StatusIO, "create dictionary staging file: %v", err)
	}
	defer func() {
		if out == nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	schema := src.Schema()
	if schema.NumFields() > 4096 {
		return nil, errorf(adbc.StatusInvalidState, "dictionary IPC file exceeds 4096 fields")
	}
	r := &dictionaryFileReader{schema: schema, ctx: ctx, mem: mem, file: f, indices: make([]map[string]int32, schema.NumFields()), dicts: make([]arrow.Array, schema.NumFields())}
	defer func() {
		if out == nil {
			for _, a := range r.dicts {
				if a != nil {
					a.Release()
				}
			}
		}
	}()
	values := make([][]string, schema.NumFields())
	payloads := make([]int, schema.NumFields())
	for i, field := range schema.Fields() {
		if dt, ok := field.Type.(*arrow.DictionaryType); ok {
			if dt.IndexType.ID() != arrow.INT32 || (dt.ValueType.ID() != arrow.STRING && dt.ValueType.ID() != arrow.BINARY) {
				return nil, errorf(adbc.StatusNotImplemented, "dictionary IPC files require int32 string/binary dictionaries")
			}
			r.indices[i] = map[string]int32{}
		}
	}
	w := ipc.NewWriter(&dictionarySpoolWriter{file: f}, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	cells, batches, totalPayload, totalEntries := int64(0), 0, 0, 0
	dictionaryWork, dictionaryBytesWork := 0, int64(0)
	for src.Next() {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		rec := src.RecordBatch()
		batches++
		if !rec.Schema().Equal(schema) {
			return nil, errorf(adbc.StatusInvalidData, "dictionary file source schema changed")
		}
		if batches > maxDictionaryFileBatches || rec.NumRows() > (maxDictionaryFileCells-cells)/int64(max(1, schema.NumFields())) {
			return nil, errorf(adbc.StatusInvalidState, "dictionary IPC file work budget exhausted")
		}
		cells += rec.NumRows() * int64(schema.NumFields())
		for col, indices := range r.indices {
			if indices == nil {
				continue
			}
			a, ok := rec.Column(col).(*array.Dictionary)
			if !ok {
				return nil, errorf(adbc.StatusInvalidData, "dictionary file array/schema mismatch")
			}
			for entry := 0; entry < a.Dictionary().Len(); entry++ {
				dictionaryWork++
				if dictionaryWork > maxDictionaryFileCells {
					return nil, errorf(adbc.StatusInvalidState, "dictionary IPC file dictionary work budget exhausted")
				}
				if entry&1023 == 0 {
					if err := cancelled(ctx); err != nil {
						return nil, err
					}
				}
				if a.Dictionary().IsNull(entry) {
					continue
				}
				size := a.Dictionary().(interface{ ValueLen(int) int }).ValueLen(entry)
				dictionaryBytesWork += int64(size)
				if size > maxResultDictionaryPayload || dictionaryBytesWork > maxDictionaryFileBytes {
					return nil, errorf(adbc.StatusInvalidState, "dictionary IPC file dictionary payload work budget exhausted")
				}
				textValue, err := dictionaryText(a.Dictionary(), entry)
				if err != nil {
					return nil, invalidArg(err)
				}
				if _, ok := indices[textValue]; ok {
					continue
				}
				if totalEntries >= 262144 || len(indices) >= maxResultDictionaryEntries || len(textValue) > maxResultDictionaryPayload-payloads[col] || len(textValue) > maxDictionaryFilePayload-totalPayload {
					return nil, errorf(adbc.StatusInvalidState, "dictionary IPC file entry/payload budget exhausted")
				}
				text := strings.Clone(textValue)
				indices[text] = int32(len(values[col]))
				values[col] = append(values[col], text)
				payloads[col] += len(text)
				totalPayload += len(text)
				totalEntries++
			}
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	if err := src.Err(); err != nil {
		return nil, err
	}
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	closed = true
	if err := w.Close(); err != nil {
		return nil, err
	}
	for col, indices := range r.indices {
		if indices == nil {
			continue
		}
		dt := schema.Field(col).Type.(*arrow.DictionaryType)
		b := array.NewBuilder(mem, dt.ValueType)
		for _, v := range values[col] {
			switch b := b.(type) {
			case *array.StringBuilder:
				b.Append(v)
			case *array.BinaryBuilder:
				b.Append([]byte(v))
			}
		}
		r.dicts[col] = b.NewArray()
		b.Release()
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	r.source, err = ipc.NewReader(f, ipc.WithAllocator(mem))
	if err != nil {
		return nil, err
	}
	r.refs.Store(1)
	return r, nil
}

type dictionaryFileReader struct {
	refs    atomic.Int64
	schema  *arrow.Schema
	source  *ipc.Reader
	file    *os.File
	ctx     context.Context
	mem     memory.Allocator
	indices []map[string]int32
	dicts   []arrow.Array
	current arrow.RecordBatch
	err     error
}

func (r *dictionaryFileReader) Retain() { r.refs.Add(1) }
func (r *dictionaryFileReader) Release() {
	if r.refs.Add(-1) == 0 {
		if r.current != nil {
			r.current.Release()
			r.current = nil
		}
		r.source.Release()
		for _, a := range r.dicts {
			if a != nil {
				a.Release()
			}
		}
		_ = r.file.Close()
		_ = os.Remove(r.file.Name())
	}
}
func (r *dictionaryFileReader) Schema() *arrow.Schema          { return r.schema }
func (r *dictionaryFileReader) RecordBatch() arrow.RecordBatch { return r.current }
func (r *dictionaryFileReader) Record() arrow.Record           { return r.current }
func (r *dictionaryFileReader) Err() error {
	if r.err != nil {
		return r.err
	}
	return r.source.Err()
}
func (r *dictionaryFileReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.err != nil {
		return false
	}
	if r.err = cancelled(r.ctx); r.err != nil {
		return false
	}
	if !r.source.Next() {
		return false
	}
	rec := r.source.RecordBatch()
	cols := make([]arrow.Array, rec.NumCols())
	defer func() {
		for _, a := range cols {
			if a != nil {
				a.Release()
			}
		}
	}()
	for col := range cols {
		if r.indices[col] == nil {
			cols[col] = rec.Column(col)
			cols[col].Retain()
			continue
		}
		a := rec.Column(col).(*array.Dictionary)
		// Translate each dictionary entry once, then remap row indices without
		// copying a potentially large repeated payload for every row.
		remap := make([]int32, a.Dictionary().Len())
		for entry := range remap {
			if entry&1023 == 0 {
				if r.err = cancelled(r.ctx); r.err != nil {
					return false
				}
			}
			if a.Dictionary().IsNull(entry) {
				remap[entry] = -1
				continue
			}
			text, err := dictionaryText(a.Dictionary(), entry)
			if err != nil {
				r.err = err
				return false
			}
			index, ok := r.indices[col][text]
			if !ok {
				r.err = errorf(adbc.StatusInvalidData, "dictionary staging value missing")
				return false
			}
			remap[entry] = index
		}
		b := array.NewInt32Builder(r.mem)
		for row := 0; row < a.Len(); row++ {
			if row&1023 == 0 {
				if r.err = cancelled(r.ctx); r.err != nil {
					break
				}
			}
			if a.IsNull(row) {
				b.AppendNull()
				continue
			}
			index := remap[a.GetValueIndex(row)]
			if index < 0 {
				b.AppendNull()
				continue
			}
			b.Append(index)
		}
		if r.err != nil {
			b.Release()
			return false
		}
		indices := b.NewArray()
		b.Release()
		cols[col] = array.NewDictionaryArray(r.schema.Field(col).Type, indices, r.dicts[col])
		indices.Release()
	}
	r.current = array.NewRecordBatch(r.schema, cols, rec.NumRows())
	return true
}

func dictionaryText(a arrow.Array, index int) (string, error) {
	switch a := a.(type) {
	case *array.String:
		return a.Value(index), nil
	case *array.Binary:
		return string(a.Value(index)), nil
	default:
		return "", errorf(adbc.StatusInvalidData, "dictionary file requires string/binary values")
	}
}
