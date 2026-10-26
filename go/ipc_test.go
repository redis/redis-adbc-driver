// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func ipcTestBatch(mem memory.Allocator) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	b.Field(0).(*array.Int64Builder).AppendValues([]int64{9007199254740993, 0, -3}, []bool{true, false, true})
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"a\x00b", "", ""}, []bool{true, true, false})
	return b.NewRecordBatch()
}

func TestIPCRoundTripAndEarlyRelease(t *testing.T) {
	for _, format := range []string{"stream", "file"} {
		for _, count := range []int{0, 1, 3} {
			t.Run(format+string(rune('0'+count)), func(t *testing.T) {
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)
				rec := ipcTestBatch(mem)
				defer rec.Release()
				batches := make([]arrow.RecordBatch, count)
				for i := range batches {
					batches[i] = rec
				}
				path := filepath.Join(t.TempDir(), "result.arrow")
				n, err := writeIPCOutput(context.Background(), path, format, mem, func() (array.RecordReader, error) { return array.NewRecordReader(rec.Schema(), batches) })
				if err != nil || n != int64(3*count) {
					t.Fatal(n, err)
				}
				rdr, err := openIPCInput(context.Background(), path, format, mem)
				if err != nil {
					t.Fatal(err)
				}
				if !rdr.Schema().Equal(rec.Schema()) {
					t.Fatal(rdr.Schema())
				}
				got := 0
				for rdr.Next() {
					if !array.RecordEqual(rec, rdr.RecordBatch()) {
						t.Fatal("changed values")
					}
					got++
				}
				if rdr.Err() != nil || got != count {
					t.Fatal(got, rdr.Err())
				}
				rdr.Release()
				rdr, err = openIPCInput(context.Background(), path, format, mem)
				if err != nil {
					t.Fatal(err)
				}
				rdr.Next()
				file := rdr.(*ipcInputReader).file
				rdr.Release()
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("input file leaked", err)
				}
			})
		}
	}
}

type failingIPCReader struct {
	array.RecordReader
	failure error
}

func (r *failingIPCReader) Err() error { return r.failure }

func TestIPCFailureCleanup(t *testing.T) {
	for _, format := range []string{"stream", "file"} {
		for _, failure := range []string{"query", "late", "cancel"} {
			t.Run(format+failure, func(t *testing.T) {
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)
				rec := ipcTestBatch(mem)
				defer rec.Release()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				path := filepath.Join(t.TempDir(), "partial.arrow")
				want := errors.New("failure")
				_, err := writeIPCOutput(ctx, path, format, mem, func() (array.RecordReader, error) {
					if failure == "query" {
						return nil, want
					}
					rdr, e := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
					if e != nil {
						return nil, e
					}
					if failure == "cancel" {
						cancel()
						return rdr, nil
					}
					return &failingIPCReader{rdr, want}, nil
				})
				if failure == "cancel" {
					if !isStatus(err, adbc.StatusCancelled) {
						t.Fatal(err)
					}
				} else if !errors.Is(err, want) {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("partial output remains", err)
				}
			})
		}
	}
	path := filepath.Join(t.TempDir(), "existing.arrow")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := writeIPCOutput(context.Background(), path, "stream", memory.DefaultAllocator, func() (array.RecordReader, error) {
		t.Fatal("query ran before destination validation")
		return nil, nil
	})
	if !isStatus(err, adbc.StatusIO) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep" {
		t.Fatal("overwrote existing output")
	}
}

func TestIPCOptionsAndInvalidInput(t *testing.T) {
	s := ipcSettings{}
	if s.get(OptionStringIPCFormat) != "stream" {
		t.Fatal("default format")
	}
	for _, key := range []string{OptionStringIPCInput, OptionStringIPCOutput} {
		for _, value := range []string{"relative.arrow", "https://example.test/file", "/tmp/bad\x00file"} {
			if err := s.set(key, value); !isStatus(err, adbc.StatusInvalidArgument) {
				t.Fatal(key, value, err)
			}
		}
	}
	if err := s.set(OptionStringIPCFormat, "auto"); !isStatus(err, adbc.StatusInvalidArgument) {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bad.arrow")
	if err := os.WriteFile(path, []byte("not IPC"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"stream", "file"} {
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		r, err := openIPCInput(context.Background(), path, format, mem)
		if r != nil || !isStatus(err, adbc.StatusInvalidData) {
			t.Fatal(r, err)
		}
		mem.AssertSize(t, 0)
	}
}

func TestIPCInputCancellation(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	rec := ipcTestBatch(mem)
	defer rec.Release()
	for _, format := range []string{"stream", "file"} {
		path := filepath.Join(t.TempDir(), "input.arrow")
		_, err := writeIPCOutput(context.Background(), path, format, mem, func() (array.RecordReader, error) {
			return array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec, rec})
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		r, err := openIPCInput(ctx, path, format, mem)
		if err != nil {
			t.Fatal(err)
		}
		if !r.Next() {
			t.Fatal(r.Err())
		}
		cancel()
		if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
			t.Fatal(r.Err())
		}
		r.Release()
	}
}
