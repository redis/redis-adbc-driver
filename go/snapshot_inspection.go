// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
)

// Inspection reuses its bounded sample, with the same row membership and
// native decoding policy as managed adoption. Copy's synthetic key column and
// nested HASHes must not change the import's inferred schema. No second source
// scan, catalog initialization or application mutation is performed here.
func (c *hashConn) snapshotInspectionChecklist(ctx context.Context, r *HashReport, rows []hashRow, stats keyStats, indexes []searchIndex, o HashInspectOptions) (Checklist, error) {
	gopts := guessOptions{types: o.Types, renames: o.Renames}
	gs, err := newGuesserFor(gopts)
	if err != nil {
		return Checklist{}, err
	}
	for _, row := range rows {
		if isRowKey(o.Prefix, row.key) {
			gs.add(row)
		}
	}
	cols, _, err := gs.columns(gopts)
	if err != nil {
		return Checklist{}, err
	}
	importReport := *r
	importReport.Sampled = gs.rows
	a := &adopter{
		c: c, o: HashAdoptOptions{Prefix: o.Prefix, Schema: r.Schema, Table: r.Table, Types: o.Types, Renames: o.Renames},
		res:    &HashAdoption{Report: &importReport, Table: displaySchema(r.Schema) + "." + r.Table, ManagedCopy: true},
		schema: r.Schema, table: r.Table, cols: cols, stats: stats,
	}
	choice := chooseAppIndex(indexes, cols, o.Prefix, stats.keys, "", false)
	cl, err := a.snapshotAdoptionChecklist(ctx, &tableMeta{KeyPrefix: o.Prefix}, choice)
	if err != nil {
		return cl, err
	}
	if int64(gs.rows) < stats.rows {
		cl.add(CheckWarn, fmt.Sprintf("managed import types are guessed from %d sampled qualifying HASHes; Apply validates every captured row", gs.rows), "use check -sample -1 to inspect all source rows")
	}
	// Keep the public JSON field for compatibility, while reporting the native
	// source type of the managed import rather than an obsolete live table.
	types := make(map[string]string, len(cols))
	for _, col := range cols {
		types[col.field] = col.inPlace.SQLName()
	}
	for i := range r.Columns {
		r.Columns[i].InPlaceType = types[r.Columns[i].Field]
	}
	return cl, nil
}
