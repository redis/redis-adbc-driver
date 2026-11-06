// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io"
	"maps"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

type snapshotOrderedSource struct {
	prefix string
	iter   *scanIter
	rows   []map[string]Value
	at     int
	done   bool
}
type snapshotOrderedScan struct {
	store      *store
	fields     []string
	meta       *tableMeta
	sources    []snapshotOrderedSource
	columns    []string
	order      []planOrder
	skip, left int64
}

func (e *executor) openSnapshotOrdered(ctx context.Context, req scanRequest, params []Value) (*snapshotOrderedScan, error) {
	d := &snapshotOrderedScan{meta: req.meta, store: e.store, left: -1}
	_, fields, _ := e.newScanIter(ctx, req, params)
	if fields == nil {
		fields = []string{}
	}
	d.fields = fields
	sortNeed := map[string]bool{}
	columnRefs(req.where.residual, sortNeed)
	for _, key := range req.sortBy {
		name := rowIDField
		for _, col := range req.meta.Columns {
			if col.field() == key.field {
				name = col.Name
				break
			}
		}
		sortNeed[name] = true
		d.columns = append(d.columns, name)
		d.order = append(d.order, planOrder{desc: key.desc, nullsLast: true})
	}
	if req.limit != nil {
		d.skip, d.left = req.limit[0], req.limit[1]
	}
	if req.late != nil {
		d.fields = req.late.fields
		d.skip, d.left = req.late.offset, req.late.limit
	}
	for _, query := range req.visibility {
		meta := *req.meta
		meta.KeyPrefix, meta.IndexName = query.segment.Prefix, query.segment.Index
		meta.SnapshotOverlay = false
		meta.SnapshotSegments = nil
		meta.SnapshotRows = &query.segment.Rows
		source := req
		source.meta = &meta
		source.visibility = nil
		// Residuals and visibility run per source; late output and SQL limits
		// belong to the global merge, never independently to each segment.
		source.late = nil
		source.need = maps.Clone(sortNeed)
		for name := range req.dependencies {
			source.need[name] = true
		}
		source.where.query = andSnapshotQuery(req.where.query, query.query)
		if req.limit != nil {
			source.limit = &[2]int64{0, req.limit[0] + req.limit[1]}
		}
		it, err := e.openScan(ctx, source, params)
		if err != nil {
			d.close(ctx)
			return nil, err
		}
		d.sources = append(d.sources, snapshotOrderedSource{iter: it, prefix: query.segment.Prefix})
	}
	return d, nil
}
func (d *snapshotOrderedScan) head(ctx context.Context, i int) (map[string]Value, error) {
	source := &d.sources[i]
	for source.at == len(source.rows) && !source.done {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		_, rows, err := source.iter.next(ctx)
		if errors.Is(err, io.EOF) {
			source.done = true
			break
		}
		if err != nil {
			return nil, err
		}
		source.rows, source.at = rows, 0
	}
	if source.at == len(source.rows) {
		return nil, nil
	}
	return source.rows[source.at], nil
}
func (d *snapshotOrderedScan) keys(row map[string]Value) []Value {
	values := make([]Value, len(d.columns))
	for i, col := range d.columns {
		values[i] = row[col]
	}
	return values
}
func (d *snapshotOrderedScan) next(ctx context.Context) ([]string, []aggRow, error) {
	if d.left == 0 {
		return nil, nil, io.EOF
	}
	var keys []string
	var physical []string
	for len(keys) < snapshotDeltaPage && d.left != 0 {
		best := -1
		var row map[string]Value
		var order []Value
		for i := range d.sources {
			head, err := d.head(ctx, i)
			if err != nil {
				return nil, nil, err
			}
			if head == nil {
				continue
			}
			candidate := d.keys(head)
			if best < 0 || lessKeys(candidate, order, d.order) {
				best, row, order = i, head, candidate
			}
		}
		if best < 0 {
			break
		}
		d.sources[best].at++
		if d.skip > 0 {
			d.skip--
			continue
		}
		id := strconv.FormatInt(row[rowIDField].I, 10)
		keys = append(keys, d.meta.prefix()+id)
		physical = append(physical, d.sources[best].prefix+id)
		if d.left > 0 {
			d.left--
		}
	}
	if len(keys) == 0 {
		return nil, nil, io.EOF
	}
	rows, err := d.store.fetchSnapshotPhysical(ctx, physical, d.fields)
	if err != nil {
		return nil, nil, err
	}
	for i, row := range rows {
		if row == nil || row[rowIDField] != strings.TrimPrefix(keys[i], d.meta.prefix()) || row[snapshotDeletedField] == "1" {
			return nil, nil, errorf(adbc.StatusIO, "ordered snapshot index references a missing or invalid visible row")
		}
	}
	return keys, rows, nil
}
func (d *snapshotOrderedScan) close(ctx context.Context) {
	for _, source := range d.sources {
		source.iter.close(ctx)
	}
}
