// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"errors"
	"io"
	"math"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Streamed results.
//
// A query result is normally built whole before ExecuteQuery returns. A
// query that reads one table through its index, row by row, is streamed
// instead: its rows are read from the FT.AGGREGATE cursor a page at a time
// as the result is read, so the driver holds about one batch of them.
// These queries stream:
//   - a single SELECT (not a script), run with no parameters or one row of
//     them;
//   - on a table (not a view, CTE, derived table or join), matched through
//     its index rather than looked up by key;
//   - without aggregates, GROUP BY, DISTINCT, set operations, window
//     functions, or correlated subqueries;
//   - in insertion order, or ordered by indexed columns the index sorts
//     exactly (see prepareScan), but for rows that tie on values it rounds,
//     which the scan sorts again as they stream (see ties.go).
//
// UNION ALL trees without ORDER BY also stream when their managed-table
// leaves qualify for insertion-order index scans (see union_stream.go).
// They preserve pairwise coercions and open one child cursor at a time.
//
// Other queries are read whole, as before. LIMIT and OFFSET apply as the
// rows stream when they can't run in the index.
//
// ExecuteQuery reads the first batch before it returns, so errors in it are
// reported there. If that is all of the result, it returns it whole with its
// row count; otherwise it returns a stream, and -1 rows. An error later on
// ends the stream: Next returns false and Err reports it.
//
// Releasing the stream deletes its cursor, as does closing the connection
// (after which reading the stream fails). So does cancelling the context
// given to ExecuteQuery, which the stream reads with, or the statement
// (AdbcStatementCancel), after which reading it fails with CANCELLED. Redis
// deletes a cursor that isn't read for its idle time (300 seconds by
// default).

const defaultStreamBatchRows = 65536

// streamSettings are adbc.redis.stream_results and stream_batch_rows.
type streamSettings struct {
	off        bool  // stream_results=false
	batchRows  int   // 0: defaultStreamBatchRows
	batchBytes int64 // 0: disabled
}

func (s streamSettings) rows() int {
	if s.batchRows > 0 {
		return s.batchRows
	}
	return defaultStreamBatchRows
}

func (s streamSettings) get(key string) string {
	if key == OptionStringStreamResults {
		return strconv.FormatBool(!s.off)
	}
	if key == OptionIntStreamBatchBytes {
		return strconv.FormatInt(s.batchBytes, 10)
	}
	return strconv.Itoa(s.rows())
}

func (s *streamSettings) set(key, value string) error {
	if key == OptionStringStreamResults {
		on, err := parseStreamResults(value)
		if err == nil {
			s.off = !on
		}
		return err
	}
	if key == OptionIntStreamBatchBytes {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			return errorf(adbc.StatusInvalidArgument, "invalid %s %q (want nonnegative bytes)", key, value)
		}
		s.batchBytes = n
		return nil
	}
	n, err := parseStreamBatchRows(value)
	if err == nil {
		s.batchRows = n
	}
	return err
}

// streamOpts asks execute to stream a query's rows if it can.
type streamOpts struct {
	// cols are the result's columns, or nil for the plan's.
	cols       []resultColumn
	batchRows  int
	batchBytes int64
	mem        memory.Allocator
	// owners are the open streams of the connection and the statement,
	// which the stream joins.
	owners []*streams
}

// streamable reports whether a query's plan allows streaming its rows,
// before prepareScan decides how they are read.
func (p *selectPlan) streamable() bool {
	return !p.noRows && !p.distinct && p.setop == nil && p.grouping == nil && p.meta != nil && !p.meta.isMem &&
		!p.aggregate && p.having == nil && len(p.outerAggs) == 0 && !p.windowed() && len(p.extraNeed) == 0 &&
		!p.hasCorrelated() && !p.hasSetReturning()
}

// hasCorrelated reports whether the query holds a correlated subquery,
// which runs once per distinct outer row and remembers each result.
func (p *selectPlan) hasCorrelated() bool {
	found := false
	visit := func(x Expr) {
		walkExprPruned(x, func(x Expr) bool {
			if sq, ok := x.(*Subquery); ok && sq.correlated {
				found = true
			}
			return !found
		})
	}
	for _, it := range p.items {
		visit(it.expr)
	}
	visit(p.sel.Where)
	for _, o := range p.order {
		visit(o.expr)
	}
	return found
}

// incrementalScan permits a prepared key list without a global sort. The
// existing scan iterator fetches those HASHes in bounded pipeline chunks.
// General HASH discovery still needs a global insertion-order sort.
func incrementalScan(plan *selectPlan, sc selectScan) bool {
	return !sc.req.hashScan && (sc.indexSort || sc.req.where.keys != nil && len(plan.order) == 0)
}

// streamSelect runs a query whose plan is streamable. It reads the first
// batch of rows, and returns the result whole if that was all of it, or
// else a stream of it.
func (e *executor) streamSelect(ctx context.Context, plan *selectPlan, params []Value, so *streamOpts) (execResult, error) {
	sc, err := e.prepareScan(ctx, plan, params)
	if err != nil {
		return execResult{}, err
	}
	// A residual can prevent LIMIT from running in an otherwise exact index
	// sort. Redis would then sort the complete match set before this stream saw
	// its first page. Prefer the bounded partitioned heap when it is eligible.
	if rows, handled, err := e.parallelSelectTopK(ctx, plan, sc, params); handled {
		if err != nil {
			return execResult{}, err
		}
		return e.fitResult(execResult{isQuery: true, cols: plan.columns(), rows: rows, affected: int64(len(rows))}, nil)
	}
	if sc.req.where.none || !incrementalScan(plan, sc) {
		rows, handled, err := e.selectTopK(ctx, plan, sc, params)
		if !handled {
			rows, err = e.selectScanned(ctx, plan, sc, params)
		}
		if err != nil {
			return execResult{}, err
		}
		return e.fitResult(execResult{isQuery: true, cols: plan.columns(), rows: rows, affected: int64(len(rows))}, nil)
	}
	r, err := e.newScanReader(ctx, plan, sc.req, params, so)
	if err != nil {
		return execResult{}, err
	}
	cols := r.cols
	if !sc.pushLimit && sc.req.late == nil {
		if off := plan.sel.Offset; off != nil {
			r.skip = *off
		}
		if lim := plan.sel.Limit; lim != nil {
			r.left = *lim
		}
	}
	if err := r.fill(); err != nil {
		r.closeLocked()
		return execResult{}, err
	}
	if r.exhausted && (r.arrow != nil || r.batchBytes == 0 || r.pendingBytes <= r.batchBytes || len(r.pending) <= 1) {
		if r.arrow != nil {
			// As for the materialized small-result path, finish metadata checks
			// before returning a detached reader and its known row count.
			if err := e.store.checkReads(ctx, e.cache.loaded); err != nil {
				r.closeLocked()
				return execResult{}, err
			}
			var n int64
			for _, rec := range r.arrow.ready {
				n += rec.NumRows()
			}
			if n == 0 {
				r.closeLocked()
				return execResult{isQuery: true, cols: cols, affected: 0}, nil
			}
			rdr, err := array.NewRecordReader(r.schema, r.arrow.ready)
			r.closeLocked() // NewRecordReader retained the Arrow buffers.
			if err != nil {
				return execResult{}, err
			}
			return execResult{isQuery: true, cols: cols, stream: rdr, affected: n}, nil
		}
		rows := r.pending
		r.pending = nil
		r.closeLocked()
		return execResult{isQuery: true, cols: cols, rows: rows, affected: int64(len(rows))}, nil
	}
	r.attachOwners(so.owners)
	return execResult{isQuery: true, cols: cols, stream: r, affected: -1}, nil
}

// streamReader is a streamed result (see the top of this file). Next and
// Release may be called on another goroutine than ExecuteQuery's, but not
// at the same time as each other. closeWith (Connection.Close, statement
// Cancel) may be called on any goroutine at any time, while Next runs too.
type streamReader struct {
	refs      atomic.Int64
	transport *executionTransport

	// mu guards everything below. closeWith takes it to close the stream.
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelCauseFunc
	exec   *executor
	cache  *execCache // the statement's, for checkReads
	owners []*streams
	it     *scanIter
	union  *unionRows
	items  []planItem
	env    *evalEnv
	zone   tzZone

	cols       []resultColumn
	schema     *arrow.Schema
	mem        memory.Allocator
	batchRows  int
	batchBytes int64
	// skip is the OFFSET still to skip and left the LIMIT still to return
	// (-1: none), when they don't run in the index.
	skip, left int64

	// pending are rows read and evaluated but not returned yet, and
	// exhausted is set once the scan has no more.
	batchSizer   *batchSizer
	pendingBytes int64
	pending      [][]Value
	arrow        *scanArrow // direct simple-projection batches instead of pending rows
	exhausted    bool
	cur          arrow.RecordBatch
	err          error
	closed       bool
}

// fill reads pages until a batch of rows is pending, or the rows end.
func (r *streamReader) fill() error {
	if r.union != nil {
		return r.fillUnion()
	}
	if r.arrow != nil {
		return r.fillArrow()
	}
	for !r.exhausted && len(r.pending) < r.batchRows && (r.batchBytes == 0 || r.pendingBytes < r.batchBytes) {
		projection := r.it.projection
		if projection != nil {
			clear(projection.rows)
			projection.rows = projection.rows[:0]
		}
		_, rows, err := r.it.next(r.ctx)
		if errors.Is(err, io.EOF) {
			r.exhausted = true
			break
		}
		if err != nil {
			return err
		}
		count := len(rows)
		if projection != nil {
			count = len(projection.rows)
		}
		for i := 0; i < count; i++ {
			if r.skip > 0 {
				r.skip--
				continue
			}
			if r.left == 0 {
				break
			}
			var row []Value
			if projection != nil {
				row = projection.rows[i]
			} else {
				r.env.row = rows[i]
				row, err = evalItems(r.env, r.items)
				if err != nil {
					return err
				}
			}
			if err := fitRow(row, r.cols, r.zone); err != nil {
				return err
			}
			if r.batchBytes > 0 {
				for j, c := range r.cols {
					row[j], err = Coerce(row[j], c.Type)
					if err != nil {
						return invalidArg(err)
					}
				}
			}
			r.pending = append(r.pending, row)
			if r.batchBytes > 0 {
				r.pendingBytes = addBatchBytes(r.pendingBytes, r.rowBatchBytes(row))
			}
			if r.left > 0 {
				r.left--
			}
		}
		if projection != nil {
			clear(projection.rows)
			projection.rows = projection.rows[:0]
		}
		if r.left == 0 {
			// The LIMIT is reached: the rest of the cursor isn't needed.
			r.exhausted = true
			r.it.close(r.ctx)
		}
	}
	return nil
}

func (r *streamReader) Retain() { r.refs.Add(1) }

func (r *streamReader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() { _ = recover() }() // a panic must not cross a cgo callback
	r.closeLocked()
	if r.cur != nil {
		r.cur.Release()
		r.cur = nil
	}
}

func (r *streamReader) Schema() *arrow.Schema { return r.schema }

func (r *streamReader) Next() (ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			r.err = errorf(adbc.StatusInternal, "failed to read the result: %v", p)
			ok = false
			func() {
				defer func() { _ = recover() }()
				r.closeLocked()
			}()
		}
	}()
	if r.cur != nil {
		r.cur.Release()
		r.cur = nil
	}
	if r.closed {
		return false
	}
	if err := r.ctx.Err(); err != nil {
		r.fail(err)
		return false
	}
	if r.arrow != nil {
		return r.nextArrow()
	}
	if len(r.pending) < r.batchRows && (r.batchBytes == 0 || r.pendingBytes < r.batchBytes) {
		if err := r.fill(); err != nil {
			r.fail(err)
			return false
		}
	}
	if len(r.pending) == 0 {
		// The end: check that the tables weren't re-keyed meanwhile (see
		// rekey.go), as a statement does once it is done.
		if err := r.exec.store.checkReads(r.ctx, r.cache.loaded); err != nil {
			r.err = err
		}
		r.closeLocked()
		return false
	}
	n := min(len(r.pending), r.batchRows)
	var used int64
	if r.batchBytes > 0 {
		for i, row := range r.pending[:n] {
			cost := r.rowBatchBytes(row)
			if i > 0 && cost > r.batchBytes-used {
				n = i
				break
			}
			used = addBatchBytes(used, cost)
		}
	}
	rec, err := buildRecord(r.mem, r.cols, r.pending[:n])
	if err != nil {
		r.fail(invalidArg(err))
		return false
	}
	clear(r.pending[:n])
	r.pending = r.pending[n:]
	if r.batchBytes > 0 {
		if r.pendingBytes == math.MaxInt64 {
			// Saturation loses information, so only this exceptional case
			// needs a rescan; ordinary small budgets remain linear in rows.
			r.pendingBytes = 0
			for _, row := range r.pending {
				r.pendingBytes = addBatchBytes(r.pendingBytes, r.rowBatchBytes(row))
			}
		} else {
			r.pendingBytes -= used
		}
	}
	r.cur = rec
	return true
}

func (r *streamReader) RecordBatch() arrow.RecordBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur
}

// Record is RecordBatch.
//
// Deprecated: Use RecordBatch instead.
func (r *streamReader) Record() arrow.RecordBatch { return r.RecordBatch() }

func (r *streamReader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// fail ends the stream with err, or why its context ended.
func (r *streamReader) fail(err error) {
	if r.ctx.Err() != nil {
		var ae adbc.Error
		if cause := context.Cause(r.ctx); errors.As(cause, &ae) {
			err = cause // closeWith's
		} else {
			err = cancelled(r.ctx)
		}
	}
	r.err = err
	r.closeLocked()
}

// closeLocked deletes the cursor and forgets the pending rows. The current
// batch stays until Next or Release.
func (r *streamReader) closeLocked() {
	if r.closed {
		return
	}
	r.closed = true
	if r.union != nil {
		r.union.close()
	} else {
		r.it.close(r.ctx)
	}
	r.pending = nil
	if r.arrow != nil {
		r.arrow.release()
	}
	if r.it != nil && r.it.projection != nil {
		p := r.it.projection
		clear(p.rows)
		p.rows = nil
	}
	r.cancel(nil)
	for _, o := range r.owners {
		o.remove(r)
	}
	if r.transport != nil {
		r.transport.finish()
	}
}

// closeWith closes the stream for a reason other than its end, which a
// later Next reports. It first cancels a Next under way.
func (r *streamReader) closeWith(err error) {
	r.cancel(err)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.err = err
		r.closeLocked()
	}
}

// streams are the open streams of a connection, closed with it, or of a
// statement, closed when it is cancelled.
type streams struct {
	mu   sync.Mutex
	open map[ownedStream]bool
}

type ownedStream interface{ closeWith(error) }

func (s *streams) add(r ownedStream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		s.open = map[ownedStream]bool{}
	}
	s.open[r] = true
}

func (s *streams) remove(r ownedStream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.open, r)
}

// closeAll closes the open streams with err, deleting their cursors, and
// returns how many there were. A stream's lock is taken after s's is
// released, as a closing stream takes them in the other order.
func (s *streams) closeAll(err error) int {
	s.mu.Lock()
	open := make([]ownedStream, 0, len(s.open))
	for r := range s.open {
		open = append(open, r)
	}
	s.mu.Unlock()
	for _, r := range open {
		r.closeWith(err)
	}
	return len(open)
}
