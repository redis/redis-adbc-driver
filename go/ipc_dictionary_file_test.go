// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestIPCDictionaryFileRemapsBatches(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(fmt.Sprint(binary), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			var valueType arrow.DataType = arrow.BinaryTypes.String
			if binary {
				valueType = arrow.BinaryTypes.Binary
			}
			dt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: valueType}
			schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: dt, Nullable: true}}, nil)
			texts := [][]string{{"one", "two", ""}, {}, {"two", "three", "one"}, {"three", "", "two"}}
			var records []arrow.RecordBatch
			for _, vs := range texts {
				b := array.NewDictionaryBuilder(mem, dt).(*array.BinaryDictionaryBuilder)
				for i, v := range vs {
					if i == 2 {
						b.AppendNull()
					} else if err := b.AppendString(v); err != nil {
						t.Fatal(err)
					}
				}
				a := b.NewArray()
				b.Release()
				rec := array.NewRecordBatch(schema, []arrow.Array{a}, int64(len(vs)))
				a.Release()
				records = append(records, rec)
			}
			defer func() {
				for _, rec := range records {
					rec.Release()
				}
			}()
			path := filepath.Join(t.TempDir(), "dictionary.arrow")
			n, err := writeIPCOutput(context.Background(), path, "file", mem, func() (array.RecordReader, error) { return array.NewRecordReader(schema, records) })
			if err != nil || n != 9 {
				t.Fatal(n, err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := ipc.NewFileReader(f, ipc.WithAllocator(mem))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if !r.Schema().Equal(schema) || r.NumRecords() != len(records) {
				t.Fatal(r.Schema(), r.NumRecords())
			}
			for batch, want := range records {
				got, err := r.RecordAt(batch)
				if err != nil {
					t.Fatal(err)
				}
				a := got.Column(0).(*array.Dictionary)
				w := want.Column(0).(*array.Dictionary)
				for row := 0; row < a.Len(); row++ {
					if a.IsNull(row) != w.IsNull(row) {
						t.Fatal("null changed")
					}
					if !a.IsNull(row) {
						v, _ := valueAt(a.Dictionary(), a.GetValueIndex(row))
						wv, _ := valueAt(w.Dictionary(), w.GetValueIndex(row))
						if v.S != wv.S {
							t.Fatal(v, wv)
						}
					}
				}
				if a.Dictionary().Len() != 4 {
					t.Fatal("dictionary was not file-wide", a.Dictionary())
				}
				// Retained batches remain valid across reader closure.
				got.Release()
			}
		})
	}
}

func TestIPCDictionaryFileGlobalLimitCleanup(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	dt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: dt}}, nil)
	var records []arrow.RecordBatch
	for batch := 0; batch < 2; batch++ {
		b := array.NewDictionaryBuilder(mem, dt).(*array.BinaryDictionaryBuilder)
		for row := 0; row < 40000; row++ {
			if err := b.AppendString(fmt.Sprint(batch*40000 + row)); err != nil {
				t.Fatal(err)
			}
		}
		a := b.NewArray()
		b.Release()
		rec := array.NewRecordBatch(schema, []arrow.Array{a}, 40000)
		a.Release()
		records = append(records, rec)
	}
	defer func() {
		for _, rec := range records {
			rec.Release()
		}
	}()
	path := filepath.Join(t.TempDir(), "partial.arrow")
	if _, err := writeIPCOutput(context.Background(), path, "file", mem, func() (array.RecordReader, error) { return array.NewRecordReader(schema, records) }); err == nil {
		t.Fatal("global dictionary limit not enforced")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("partial output remained", err)
	}
}

func TestIPCDictionaryFileOwnershipAndStagingCleanup(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	outputDir := t.TempDir()
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	dt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: dt}}, nil)
	b := array.NewDictionaryBuilder(mem, dt).(*array.BinaryDictionaryBuilder)
	_ = b.AppendString("retained")
	b.AppendNull()
	a := b.NewArray()
	b.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 2)
	a.Release()
	defer rec.Release()
	src, _ := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
	replay, err := prepareDictionaryFile(context.Background(), mem, src)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Next() {
		t.Fatal(replay.Err())
	}
	retained := replay.RecordBatch()
	retained.Retain()
	replay.Release()
	if retained.Column(0).(*array.Dictionary).ValueStr(0) != "retained" || !retained.Column(0).IsNull(1) {
		t.Fatal("consumer batch invalidated")
	}
	retained.Release()
	files, _ := os.ReadDir(staging)
	if len(files) != 0 {
		t.Fatal("staging file leaked")
	}
	for _, mode := range []string{"cancel", "late", "empty"} {
		path := filepath.Join(outputDir, "output-"+mode+".arrow")
		ctx, cancel := context.WithCancel(context.Background())
		_, err := writeIPCOutput(ctx, path, "file", mem, func() (array.RecordReader, error) {
			batches := []arrow.RecordBatch{rec}
			if mode == "empty" {
				batches = nil
			}
			r, _ := array.NewRecordReader(schema, batches)
			if mode == "cancel" {
				cancel()
			}
			if mode == "late" {
				return &failingIPCReader{RecordReader: r, failure: fmt.Errorf("late dictionary source error")}, nil
			}
			return r, nil
		})
		cancel()
		if mode == "empty" {
			if err != nil {
				t.Fatal(err)
			}
			f, _ := os.Open(path)
			r, e := ipc.NewFileReader(f)
			if e != nil {
				f.Close()
				t.Fatal(e)
			}
			if r.NumRecords() != 0 || !r.Schema().Equal(schema) {
				t.Fatal("empty file schema changed")
			}
			r.Close()
			f.Close()
		} else {
			if err == nil {
				t.Fatal("failure lost", mode)
			}
			if _, e := os.Stat(path); !os.IsNotExist(e) {
				t.Fatal("partial dictionary file leaked", e)
			}
		}
		files, _ := os.ReadDir(staging)
		if len(files) != 0 {
			t.Fatal("staging file leaked", mode)
		}
	}
}

// Repeated dictionaries can serialize only once in an IPC stream. Limit their
// visitation work independently of the compact staging bytes and row count.
func TestIPCDictionaryFileRepeatedPayloadWorkLimit(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	dt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: dt}}, nil)
	b := array.NewDictionaryBuilder(mem, dt).(*array.BinaryDictionaryBuilder)
	if err := b.AppendString(strings.Repeat("x", 1<<20)); err != nil {
		t.Fatal(err)
	}
	a := b.NewArray()
	b.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	a.Release()
	defer rec.Release()
	batches := make([]arrow.RecordBatch, 257)
	for i := range batches {
		batches[i] = rec
	}
	path := filepath.Join(t.TempDir(), "partial.arrow")
	_, err := writeIPCOutput(context.Background(), path, "file", mem, func() (array.RecordReader, error) { return array.NewRecordReader(schema, batches) })
	if err == nil || !strings.Contains(err.Error(), "payload work budget") {
		t.Fatal("repeated dictionary work not bounded", err)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("partial file leaked", e)
	}
}
