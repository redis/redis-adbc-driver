// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type subBudgetKey struct{}
type subWork struct{ rows, bytes int64 }

const subMaxRows int64 = 1000000
const subMaxBytes int64 = 256 << 20

func (b *subWork) charge(rows, bytes int64) error {
	if b == nil {
		return nil
	}
	if rows < 0 || rows > subMaxRows-b.rows {
		return subBudget("scan_rows")
	}
	if bytes < 0 || bytes > subMaxBytes-b.bytes {
		return subBudget("decoded_bytes")
	}
	b.rows += rows
	b.bytes += bytes
	return nil
}

// Charge authoritative input before decoding or applying residual predicates.
// This includes rows discarded by the filter, and bounds raw payload as well
// as fixed-width value overhead. SQL executions have no subWork in context.
func (b *subWork) input(raw aggRow, width int) error {
	if b == nil {
		return nil
	}
	n := int64(width) * 64
	for k, v := range raw {
		n += int64(len(k)) + int64(len(v))
	}
	return b.charge(1, n)
}
func (b *subWork) output(row []Value) error {
	n := int64(len(row)) * 64
	for _, v := range row {
		n += int64(len(v.S))
		if v.D != nil {
			n += int64(len(v.D.Bytes()))
		}
	}
	return b.charge(0, n)
}

type subSource struct {
	exec   *executor
	req    scanRequest
	it     *scanIter
	keys   *keyScan
	fields []string
	seen   map[string]bool
	done   bool
	work   int64
	budget *subWork
}

func newSubSource(ctx context.Context, e *executor, plan *selectPlan) (*subSource, error) {
	sc, err := e.prepareScan(ctx, plan, nil)
	if err != nil {
		return nil, err
	}
	// Keep an exact residual even when Search claims to answer the predicate.
	// There are no sort/limit operators in this subset.
	sc.req.where.residual = plan.sel.Where
	sc.req.authoritative = !plan.meta.KeyRows
	src := &subSource{exec: e, req: sc.req, budget: ctx.Value(subBudgetKey{}).(*subWork), done: sc.req.where.none}
	if src.done {
		return src, nil
	}
	if !sc.req.hashScan || plan.meta.SnapshotOverlay {
		src.it, err = e.openScan(ctx, sc.req, nil)
	} else {
		src.it, src.fields, _ = e.newScanIter(ctx, sc.req, nil)
		src.keys, err = (&hashConn{client: e.store.redis(ctx)}).scanKeys(ctx, plan.meta.prefix(), "hash")
		src.seen = map[string]bool{}
	}
	if err != nil {
		return nil, err
	}
	return src, nil
}
func (s *subSource) next(ctx context.Context) ([]map[string]Value, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if s.done {
		return nil, io.EOF
	}
	if err := s.exec.store.checkKeys(ctx, s.req.meta, false); err != nil {
		return nil, err
	}
	if s.keys == nil {
		_, rows, err := s.it.next(ctx)
		return rows, err
	}
	keys, done, err := s.keys.next(ctx)
	if err != nil {
		return nil, err
	}
	s.done = done
	unique := keys[:0]
	for _, k := range keys {
		s.work++
		if s.work > subMaxRows {
			return nil, subBudget("discovery_work")
		}
		if s.seen[k] || s.req.meta.KeyRows && !isRowKey(s.req.meta.prefix(), k) {
			continue
		}
		if err := s.budget.charge(0, int64(len(k))+64); err != nil {
			return nil, err
		}
		s.seen[k] = true
		unique = append(unique, k)
	}
	fetch := s.exec.store.fetchRows
	if s.req.meta.KeyRows {
		fetch = s.exec.store.fetchKeyRows
	}
	raw, err := fetch(ctx, unique, s.fields)
	if err != nil {
		return nil, err
	}
	if err := s.exec.store.checkKeys(ctx, s.req.meta, false); err != nil {
		return nil, err
	}
	_, rows, err := s.it.keep(unique, raw)
	return rows, err
}
func (s *subSource) close(ctx context.Context) {
	if s.it != nil {
		s.it.close(ctx)
	}
}

// subReader uses the existing scan/expression/Arrow conversion machinery, with
// its own typed schema and work accounting. No detached goroutine or unbounded
// channel holds resources after release. Both result modes consume this reader.
type subReader struct {
	refs         atomic.Int64
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	cancelCause  context.CancelCauseFunc
	transport    *executionTransport
	snapshot     *snapshotPin
	snapshotStop func() bool
	source       *subSource
	plan         *subPlan
	mem          memory.Allocator
	settings     streamSettings
	env          *evalEnv
	owners       []*streams
	input        []map[string]Value
	current      arrow.RecordBatch
	err          error
	closed       bool
}

func (r *subReader) Retain() { r.refs.Add(1) }
func (r *subReader) Release() {
	if r.refs.Add(-1) == 0 {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.close()
		if r.current != nil {
			r.current.Release()
			r.current = nil
		}
	}
}
func (r *subReader) Schema() *arrow.Schema          { return r.plan.schema }
func (r *subReader) RecordBatch() arrow.RecordBatch { return r.current }
func (r *subReader) Record() arrow.RecordBatch      { return r.current }
func (r *subReader) Err() error                     { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *subReader) close() {
	if r.closed {
		return
	}
	r.closed = true
	if r.snapshotStop != nil {
		r.snapshotStop()
	}
	r.source.close(r.ctx)
	r.snapshot.release()
	r.cancelCause(nil)
	r.cancel()
	r.input = nil
	for _, o := range r.owners {
		o.remove(r)
	}
	if r.transport != nil {
		r.transport.finish()
	}
}
func (r *subReader) closeWith(err error) {
	r.cancelCause(err)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.err = err
		r.close()
	}
}
func (r *subReader) fail(err error) bool {
	if r.ctx.Err() != nil {
		var ae adbc.Error
		if errors.As(context.Cause(r.ctx), &ae) {
			err = context.Cause(r.ctx)
		} else {
			err = cancelled(r.ctx)
		}
	}
	r.err = subError(err, "execution")
	r.close()
	return false
}
func (r *subReader) Next() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.closed {
		return false
	}
	if err := cancelled(r.ctx); err != nil {
		return r.fail(err)
	}
	if err := r.snapshot.valid(r.ctx); err != nil {
		return r.fail(err)
	}
	limit := min(r.settings.rows(), 4096)
	rows := make([][]Value, 0, limit)
	var batchBytes int64
	for len(rows) < limit {
		if len(r.input) == 0 {
			page, err := r.source.next(r.ctx)
			if errors.Is(err, io.EOF) {
				if err := r.snapshot.valid(r.ctx); err != nil {
					return r.fail(err)
				}
				if err := r.source.exec.store.checkReads(r.ctx, r.source.exec.cache.loaded); err != nil {
					return r.fail(err)
				}
				r.close()
				break
			}
			if err != nil {
				return r.fail(err)
			}
			r.input = page
			if len(page) == 0 {
				continue
			}
		}
		r.env.row = r.input[0]
		r.input[0] = nil
		r.input = r.input[1:]
		row := make([]Value, len(r.plan.output))
		for i, x := range r.plan.output {
			v, err := r.env.eval(x.expr)
			if err != nil {
				return r.fail(subInvalid("expression evaluation"))
			}
			if v.Null && !x.t.nullable {
				return r.fail(subInvalid("required output is null"))
			}
			row[i] = v
		}
		if err := r.source.budget.output(row); err != nil {
			return r.fail(err)
		}
		rows = append(rows, row)
		for _, v := range row {
			batchBytes += 64 + int64(len(v.S))
		}
		if r.settings.batchBytes > 0 && batchBytes >= r.settings.batchBytes {
			break
		}
	}
	if len(rows) == 0 {
		return false
	}
	cols := make([]resultColumn, len(r.plan.output))
	for i, x := range r.plan.output {
		cols[i] = resultColumn{Name: r.plan.schema.Field(i).Name, Type: x.t.typ}
	}
	rec, err := buildRecord(r.mem, cols, rows)
	if err != nil {
		return r.fail(subInvalid("Arrow conversion"))
	}
	r.current = array.NewRecordBatch(r.plan.schema, rec.Columns(), rec.NumRows())
	rec.Release()
	return true
}

// Metadata-only operations bind one committed generation without retaining it
// in the installed plan. Execution rebinds its declared schema under a fresh pin.
func (s *statementImpl) bindSubstraitMetadata(ctx context.Context, plan *subPlan) (*selectPlan, error) {
	ctx, pin, err := s.conn.acquireReadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer pin.release()
	if err := s.conn.store.refreshSnapshotTemp(ctx); err != nil {
		return nil, err
	}
	bound, err := plan.bind(ctx, s.executor())
	if err == nil {
		err = pin.valid(ctx)
	}
	return bound, err
}
func (s *statementImpl) subSchema(ctx context.Context) (*arrow.Schema, error) {
	if _, err := s.bindSubstraitMetadata(ctx, s.substrait); err != nil {
		return nil, subError(err, "metadata")
	}
	return s.substrait.schema, nil
}
func (s *statementImpl) subQuery(ctx context.Context) (reader array.RecordReader, count int64, resultErr error) {
	ctx, transport := s.conn.executionContext(ctx, &s.streams, &s.subResults, &s.conn.streams)
	if transport != nil {
		defer func() {
			if resultErr == nil && reader != nil {
				reader = transport.reader(reader)
			} else {
				transport.finish()
			}
		}()
	}
	if s.executionBackend == "cuda" || len(s.resultLayouts) > 0 || s.ipc.active() {
		return nil, -1, subUnsupported("execution option")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	ctx, cancelCause := context.WithCancelCause(ctx)
	ctx, pin, err := s.conn.acquireReadSnapshot(ctx)
	if err != nil {
		cancelCause(nil)
		cancel()
		return nil, -1, subError(err, "snapshot admission")
	}
	defer func() {
		if pin != nil {
			pin.release()
		}
	}()
	if err := s.conn.store.refreshSnapshotTemp(ctx); err != nil {
		cancelCause(nil)
		cancel()
		return nil, -1, subError(err, "temporary metadata")
	}
	work := new(subWork)
	ctx = context.WithValue(ctx, subBudgetKey{}, work)
	e := s.executor()
	e.cache = newExecCache()
	plan, err := s.substrait.bind(ctx, e)
	if err != nil {
		cancelCause(nil)
		cancel()
		return nil, -1, subError(err, "metadata")
	}
	src, err := newSubSource(ctx, e, plan)
	if err != nil {
		cancelCause(nil)
		cancel()
		return nil, -1, subError(err, "scan admission")
	}
	r := &subReader{ctx: ctx, cancel: cancel, cancelCause: cancelCause, source: src, plan: s.substrait, mem: s.conn.Alloc, settings: s.stream, env: e.newEnv(ctx, plan.meta.types(), nil), owners: []*streams{&s.streams, &s.subResults, &s.conn.streams}}
	r.refs.Store(1)
	// Register every owner before cancellation can close the reader. Close may
	// already have interrupted its execution transport during source admission.
	r.mu.Lock()
	for _, o := range r.owners {
		o.add(r)
	}
	r.snapshot, pin = pin, nil
	if r.snapshot != nil {
		r.snapshotStop = context.AfterFunc(ctx, func() { r.closeWith(cancelled(ctx)) })
	}
	r.mu.Unlock()
	if !s.stream.off {
		return r, -1, nil
	}
	defer r.Release()
	var records []arrow.RecordBatch
	var total int64
	defer func() {
		for _, rec := range records {
			rec.Release()
		}
	}()
	for r.Next() {
		rec := r.RecordBatch()
		rec.Retain()
		records = append(records, rec)
		total += rec.NumRows()
	}
	if err := r.Err(); err != nil {
		return nil, -1, err
	}
	out, err := array.NewRecordReader(r.Schema(), records)
	return out, total, err
}
