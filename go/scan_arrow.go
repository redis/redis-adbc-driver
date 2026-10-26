// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"errors"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// scanArrow retains one reusable row, one partial Arrow batch and complete
// batches from at most one cursor-page overshoot. No projected row collection
// is materialized. Completed buffers belong to their records, never to scratch.
type scanArrow struct {
	builder *array.RecordBuilder
	scratch []Value
	rows    int
	bytes   int64
	ready   []arrow.RecordBatch
}

func (a *scanArrow) release() {
	if a.builder != nil {
		a.builder.Release()
		a.builder = nil
	}
	for _, rec := range a.ready {
		rec.Release()
	}
	a.ready = nil
	a.scratch = nil
}

func (r *streamReader) fillArrow() error {
	a := r.arrow
	if a.builder == nil && !r.exhausted {
		a.builder = array.NewRecordBuilder(r.mem, r.schema)
	}
	p := r.it.projection
	p.consume = r.appendArrow
	if p.hashFields != nil {
		p.consumeHash = r.appendArrowHash
	}
	defer func() { p.consume = nil; p.consumeHash = nil }()
	for !r.exhausted && len(a.ready) == 0 {
		_, _, err := r.it.next(r.ctx)
		if errors.Is(err, io.EOF) {
			r.exhausted = true
			break
		}
		if err != nil {
			return err
		}
		if r.left == 0 {
			r.exhausted = true
			r.it.close(r.ctx)
		}
	}
	if r.exhausted && a.rows > 0 {
		a.ready = append(a.ready, a.builder.NewRecordBatch())
		a.rows = 0
		a.bytes = 0
	}
	return nil
}

func (r *streamReader) appendArrow(raw aggRow) error {
	p := r.it.projection
	// Like keep's row path, validate even skipped rows and the tail of
	// the final cursor page after LIMIT. Never append a partial row.
	if err := p.decodeInto(raw, r.arrow.scratch); err != nil {
		return err
	}
	return r.appendDecodedArrow()
}

func (r *streamReader) appendArrowHash(vals []any) error {
	if err := r.it.projection.decodeHashInto(vals, r.arrow.scratch); err != nil {
		return err
	}
	return r.appendDecodedArrow()
}

func (r *streamReader) appendDecodedArrow() error {
	a := r.arrow
	if r.skip > 0 {
		r.skip--
		return nil
	}
	if r.left == 0 {
		return nil
	}
	if err := fitRow(a.scratch, r.cols, r.zone); err != nil {
		return err
	}
	var cost int64
	if r.batchBytes > 0 {
		for j, c := range r.cols {
			v, err := Coerce(a.scratch[j], c.Type)
			if err != nil {
				return invalidArg(err)
			}
			a.scratch[j] = v
		}
		cost = r.rowBatchBytes(a.scratch)
		if a.rows > 0 && cost > r.batchBytes-a.bytes {
			a.ready = append(a.ready, a.builder.NewRecordBatch())
			a.rows, a.bytes = 0, 0
		}
	}
	if a.rows == 0 {
		// Reserve a modest initial capacity, not 65K variable-width
		// slots for every narrow/short partition. Grow as needed.
		reserve := min(r.batchRows, 1024)
		if r.batchBytes > 0 {
			// A small byte budget should not reserve 1,024 wide rows for
			// every tiny record completed inside the cursor page.
			reserve = int(min(int64(reserve), max(int64(1), r.batchBytes/max(int64(1), r.batchSizer.base))))
		}
		a.builder.Reserve(reserve)
	}
	if r.batchBytes > 0 {
		for j, c := range r.cols {
			if err := appendValue(a.builder.Field(j), c.Type, a.scratch[j]); err != nil {
				return invalidArg(err)
			}
		}
	} else {
		// Preserve the row-only hot loop: no extra Value copy or budget
		// branch per column when the optional policy is disabled.
		for j, c := range r.cols {
			v, err := Coerce(a.scratch[j], c.Type)
			if err != nil {
				return invalidArg(err)
			}
			if err := appendValue(a.builder.Field(j), c.Type, v); err != nil {
				return invalidArg(err)
			}
		}
	}
	a.rows++
	a.bytes = addBatchBytes(a.bytes, cost)
	if a.rows == r.batchRows || r.batchBytes > 0 && a.bytes >= r.batchBytes {
		a.ready = append(a.ready, a.builder.NewRecordBatch())
		a.rows = 0
		a.bytes = 0
	}
	if r.left > 0 {
		r.left--
	}
	return nil
}

func (r *streamReader) nextArrow() bool {
	a := r.arrow
	if len(a.ready) == 0 {
		if err := r.fillArrow(); err != nil {
			r.fail(err)
			return false
		}
	}
	if len(a.ready) == 0 {
		if err := r.exec.store.checkReads(r.ctx, r.cache.loaded); err != nil {
			r.err = err
		}
		r.closeLocked()
		return false
	}
	r.cur = a.ready[0] // transfer this reference to the public current batch
	a.ready[0] = nil
	a.ready = a.ready[1:]
	return true
}
