// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const (
	OptionStringIPCInput  = "adbc.redis.ipc.input"
	OptionStringIPCOutput = "adbc.redis.ipc.output"
	OptionStringIPCFormat = "adbc.redis.ipc.format"
)

type ipcSettings struct{ input, output, format string }

func (s ipcSettings) active() bool { return s.input != "" || s.output != "" }
func (s ipcSettings) get(key string) string {
	switch key {
	case OptionStringIPCInput:
		return s.input
	case OptionStringIPCOutput:
		return s.output
	default:
		if s.format == "" {
			return "stream"
		}
		return s.format
	}
}
func (s *ipcSettings) set(key, value string) error {
	if key == OptionStringIPCFormat {
		if value != "stream" && value != "file" {
			return errorf(adbc.StatusInvalidArgument, "%s must be stream or file", key)
		}
		s.format = value
		return nil
	}
	if value != "" && (!filepath.IsAbs(value) || strings.ContainsRune(value, 0)) {
		return errorf(adbc.StatusInvalidArgument, "%s requires an absolute local path or an empty string to disable", key)
	}
	if key == OptionStringIPCInput {
		s.input = value
	} else {
		s.output = value
	}
	return nil
}

// executeIPC uses ExecuteUpdate: ExecuteQuery with a NULL ArrowArrayStream at
// the C boundary. Serialization is entirely native, through standard ADBC.
func (s *statementImpl) executeIPC(ctx context.Context) (int64, error) {
	if s.ipc.input != "" && s.ipc.output != "" {
		return -1, errorf(adbc.StatusInvalidState, "IPC input and output cannot be enabled together")
	}
	if s.ipc.input != "" {
		if s.ingest.TableName == "" || s.params != nil {
			return -1, errorf(adbc.StatusInvalidState, "IPC input requires an ingest target and no bound data")
		}
		rdr, err := openIPCInput(ctx, s.ipc.input, s.ipc.get(OptionStringIPCFormat), s.conn.Alloc)
		if err != nil {
			return -1, err
		}
		s.params = rdr // executeIngest releases params, including on failure.
		n, err := s.executeIngest(ctx)
		// The shared ingest path wraps reader errors as IO. Preserve the
		// statement's cancellation/timeout status at this native IPC boundary.
		if cancelErr := cancelled(ctx); cancelErr != nil {
			return -1, cancelErr
		}
		return n, err
	}
	if s.ipc.get(OptionStringIPCFormat) == "file" {
		for _, layout := range s.resultLayouts {
			if layout == "dictionary" {
				return -1, errorf(adbc.StatusNotImplemented, "per-batch dictionary output requires IPC stream format")
			}
		}
	}
	if s.ingest.TableName != "" {
		return -1, errorf(adbc.StatusInvalidState, "IPC output requires a SELECT, not an ingest target")
	}
	parsed, err := s.parse()
	if err != nil {
		return -1, err
	}
	if len(parsed) != 1 {
		return -1, errorf(adbc.StatusInvalidArgument, "IPC output requires exactly one SELECT")
	}
	if _, ok := parsed[0].Stmt.(*SelectStmt); !ok {
		return -1, errorf(adbc.StatusInvalidArgument, "IPC output requires a SELECT")
	}
	return writeIPCOutput(ctx, s.ipc.output, s.ipc.get(OptionStringIPCFormat), s.conn.Alloc, func() (array.RecordReader, error) {
		rdr, _, err := s.executeQuery(ctx)
		return rdr, err
	})
}

// Require a new destination; remove partial output on failure. Consumers must
// wait for success. This is neither fsync nor an atomic publication protocol.
func writeIPCOutput(ctx context.Context, path, format string, mem memory.Allocator, query func() (array.RecordReader, error)) (n int64, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return -1, errorf(adbc.StatusIO, "create IPC output: %v", err)
	}
	defer func() {
		closeErr := f.Close()
		if err == nil && closeErr != nil {
			err = errorf(adbc.StatusIO, "close IPC output: %v", closeErr)
		}
		if err != nil {
			n = -1
			_ = os.Remove(path)
		}
	}()
	rdr, err := query()
	if err != nil {
		return -1, err
	}
	defer rdr.Release()
	var writer interface {
		Write(arrow.RecordBatch) error
		Close() error
	}
	opts := []ipc.Option{ipc.WithSchema(rdr.Schema()), ipc.WithAllocator(mem)}
	if format == "file" {
		writer, err = ipc.NewFileWriter(f, opts...)
		if err != nil {
			return -1, errorf(adbc.StatusIO, "create IPC writer: %v", err)
		}
	} else {
		writer = ipc.NewWriter(f, opts...)
	}
	defer func() {
		closeErr := writer.Close()
		if err == nil && closeErr != nil {
			err = errorf(adbc.StatusIO, "finish IPC output: %v", closeErr)
		}
	}()
	for {
		if err := cancelled(ctx); err != nil {
			return -1, err
		}
		if !rdr.Next() {
			break
		}
		if err := writer.Write(rdr.RecordBatch()); err != nil {
			return -1, errorf(adbc.StatusIO, "write IPC batch: %v", err)
		}
		n += rdr.RecordBatch().NumRows()
	}
	if err := rdr.Err(); err != nil {
		return -1, err
	}
	if err := cancelled(ctx); err != nil {
		return -1, err
	}
	return n, nil
}

type ipcBatchSource interface {
	Schema() *arrow.Schema
	Read() (arrow.RecordBatch, error)
}

// IPC readers own their current batch but do not close the underlying file.
// File format also retains footer metadata proportional to the batch count.
type ipcInputReader struct {
	refs        atomic.Int64
	source      ipcBatchSource
	closeSource func()
	file        *os.File
	ctx         context.Context
	rec         arrow.RecordBatch
	err         error
}

func openIPCInput(ctx context.Context, path, format string, mem memory.Allocator) (array.RecordReader, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	// Reject pipes/devices before opening: opening a FIFO could otherwise block
	// indefinitely before the regular-file check. Inputs must remain unchanged.
	info, err := os.Stat(path)
	if err != nil {
		return nil, errorf(adbc.StatusIO, "stat IPC input: %v", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errorf(adbc.StatusInvalidArgument, "IPC input must be a regular local file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errorf(adbc.StatusIO, "open IPC input: %v", err)
	}
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errorf(adbc.StatusInvalidArgument, "IPC input must be a regular local file")
	}
	r := &ipcInputReader{file: f, ctx: ctx}
	if format == "file" {
		source, e := ipc.NewFileReader(f, ipc.WithAllocator(mem))
		err = e
		if err == nil {
			r.source = source
			r.closeSource = func() { _ = source.Close() }
		}
	} else {
		source, e := ipc.NewReader(f, ipc.WithAllocator(mem))
		err = e
		if err == nil {
			r.source = source
			r.closeSource = source.Release
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, errorf(adbc.StatusInvalidData, "read IPC schema: %v", err)
	}
	r.refs.Store(1)
	return r, nil
}
func (r *ipcInputReader) Retain() { r.refs.Add(1) }
func (r *ipcInputReader) Release() {
	if r.refs.Add(-1) == 0 {
		r.closeSource()
		_ = r.file.Close()
		r.rec = nil
	}
}
func (r *ipcInputReader) Schema() *arrow.Schema          { return r.source.Schema() }
func (r *ipcInputReader) RecordBatch() arrow.RecordBatch { return r.rec }
func (r *ipcInputReader) Record() arrow.Record           { return r.rec }
func (r *ipcInputReader) Err() error                     { return r.err }
func (r *ipcInputReader) Next() bool {
	if r.err != nil || r.refs.Load() == 0 {
		return false
	}
	r.rec = nil
	if err := cancelled(r.ctx); err != nil {
		r.err = err
		return false
	}
	rec, err := r.source.Read()
	if errors.Is(err, io.EOF) {
		return false
	}
	if err != nil {
		r.err = errorf(adbc.StatusInvalidData, "read IPC batch: %v", err)
		return false
	}
	r.rec = rec
	return true
}
