// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io"

	"github.com/apache/arrow-go/v18/arrow/array"
)

// Version 3 live descriptors retain one producer generation for every worker.
// Each reader owns a child lease; descriptor expiry or explicit release prevents
// new readers while active child leases continue to protect their generation.
func (e *executor) snapshotPartitionReader(ctx context.Context, plan *selectPlan, sel *SelectStmt, params []Value, d partitionID, c *connectionImpl) (array.RecordReader, error) {
	bound := &Binary{Op: "AND",
		L: &Binary{Op: ">=", L: &ColumnRef{Name: rowIDField}, R: &Literal{V: intValue(typeInt64, d.Lo)}},
		R: &Binary{Op: "<=", L: &ColumnRef{Name: rowIDField}, R: &Literal{V: intValue(typeInt64, d.Hi)}}}
	if sel.Where != nil {
		sel.Where = &Binary{Op: "AND", L: sel.Where, R: bound}
	} else {
		sel.Where = bound
	}
	plan, err := e.planSelect(ctx, sel, e.paramTypes)
	if err != nil {
		return nil, err
	}
	sc, err := e.prepareScan(ctx, plan, params)
	if err != nil {
		return nil, err
	}
	// Even a single physical segment uses the exact directory path here. This
	// avoids numeric Search range rounding and never reads only an overlay's tail.
	meta := *sc.req.meta
	meta.SnapshotOverlay = true
	sc.req.meta = &meta
	sc.req.ranges = &rowIDRangeScan{lo: d.Lo, hi: d.Hi}
	sc.req.where.query, sc.req.where.keys, sc.req.where.residual = "*", nil, sel.Where
	sc.req.sortBy, sc.req.ties, sc.req.limit, sc.req.late = nil, nil, nil, nil
	r, err := e.newScanReader(ctx, plan, sc.req, params, &streamOpts{mem: c.Alloc, batchRows: c.stream.rows(), batchBytes: c.stream.batchBytes})
	if err != nil {
		return nil, err
	}
	r.attachOwners([]*streams{&c.streams})
	return r, nil
}

func (c *retainedCapture) snapshotTable(ctx context.Context, meta *tableMeta) (*tableMeta, error) {
	if err := c.budget.reserve(int64(len(meta.Columns))*1024 + 4096); err != nil {
		return nil, err
	}
	scan := newSnapshotDeltaScan(c.store, meta, nil)
	var rows []map[string]Value
	for {
		_, batch, err := scan.next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, raw := range batch {
			c.keys++
			if c.rows >= c.maxRows || c.keys > c.maxKeys {
				return nil, c.budget.reserve(-1)
			}
			bytes := int64(len(raw))*256 + 512
			for k, v := range raw {
				bytes += int64(len(k)+len(v)) * 2
			}
			if err := c.budget.reserve(bytes); err != nil {
				return nil, err
			}
			row, err := decodeRow(meta, raw, nil)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
			c.rows++
		}
	}
	if err := snapshotOf(ctx).pin.valid(ctx); err != nil {
		return nil, err
	}
	snapshot := *meta
	snapshot.isMem, snapshot.retainedSource, snapshot.mem = true, true, rows
	return &snapshot, nil
}
