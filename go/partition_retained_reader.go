// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Releasing or cancelling this local reader never changes shared job state.
type retainedReader struct {
	transport            *executionTransport
	refs                 atomic.Int64
	mu                   sync.Mutex
	ctx                  context.Context
	cancel               context.CancelCauseFunc
	store                *retainedStore
	ref                  retainedReference
	manifest             *retainedManifest
	schema               *arrow.Schema
	next, end, batchRows int
	batchBytes           int64
	offset               int64
	pending, current     arrow.RecordBatch
	owners               []*streams
	closed               bool
	err                  error
}

var _ array.RecordReader = (*retainedReader)(nil)

func newRetainedReader(ctx context.Context, store *retainedStore, ref retainedReference, start, end, batchRows int, owners ...*streams) (*retainedReader, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if batchRows < 1 || batchRows > retainedIPCRows {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained Arrow batch size")
	}
	m, err := store.manifest(ctx, ref)
	if err != nil {
		return nil, err
	}
	if start < 0 || start >= end || end > len(m.Chunks) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained partition chunk range")
	}
	ctx, cancel := context.WithCancelCause(ctx)
	r := &retainedReader{ctx: ctx, cancel: cancel, store: store, ref: ref, manifest: m, next: start, end: end, batchRows: batchRows, owners: owners}
	r.refs.Store(1)
	if err := r.load(); err != nil {
		r.closeLocked()
		return nil, err
	}
	r.schema = r.pending.Schema()
	for _, owner := range owners {
		owner.add(r)
	}
	return r, nil
}

func (r *retainedReader) load() error {
	if err := cancelled(r.ctx); err != nil {
		return err
	}
	payload, err := r.store.chunk(r.ctx, r.ref, r.manifest, r.next)
	if err != nil {
		return err
	}
	record, err := decodeRetainedIPC(payload, r.manifest.Schema, r.manifest.Chunks[r.next].Rows)
	if err != nil {
		return err
	}
	if r.schema != nil && !record.Schema().Equal(r.schema) {
		record.Release()
		return errorf(adbc.StatusInvalidData, "retained Arrow schema changed between chunks")
	}
	r.pending = record
	r.offset = 0
	r.next++
	return nil
}

func (r *retainedReader) Retain() { r.refs.Add(1) }
func (r *retainedReader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
}
func (r *retainedReader) Schema() *arrow.Schema { return r.schema }
func (r *retainedReader) RecordBatch() arrow.RecordBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}
func (r *retainedReader) Record() arrow.RecordBatch { return r.RecordBatch() }
func (r *retainedReader) Err() error                { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *retainedReader) Next() (ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if recover() != nil {
			r.err = errorf(adbc.StatusInvalidData, "invalid retained Arrow result")
			r.closeLocked()
			ok = false
		}
	}()
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.closed {
		return false
	}
	for {
		if err := cancelled(r.ctx); err != nil {
			r.err = err
			r.closeLocked()
			return false
		}
		if r.pending != nil && r.offset < r.pending.NumRows() {
			end := min(r.offset+int64(r.batchRows), r.pending.NumRows())
			if r.batchBytes > 0 {
				var used int64
				for row := r.offset; row < end; row++ {
					cost := retainedArrowRowBytes(r.pending, row)
					if row > r.offset && cost > r.batchBytes-used {
						end = row
						break
					}
					used = addBatchBytes(used, cost)
				}
			}
			r.current = r.pending.NewSlice(r.offset, end)
			r.offset = end
			return true
		}
		if r.pending != nil {
			r.pending.Release()
			r.pending = nil
		}
		if r.next == r.end {
			r.closeLocked()
			return false
		}
		if err := r.load(); err != nil {
			r.err = err
			r.closeLocked()
			return false
		}
	}
}
func (r *retainedReader) closeLocked() {
	if r.closed {
		return
	}
	r.closed = true
	r.cancel(nil)
	if r.pending != nil {
		r.pending.Release()
		r.pending = nil
	}
	for _, owner := range r.owners {
		owner.remove(r)
	}
	r.manifest = nil
	r.store = nil
	if r.transport != nil {
		r.transport.finish()
	}
}
func (r *retainedReader) closeWith(err error) {
	r.cancel(err)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.err = err
		r.closeLocked()
	}
}
