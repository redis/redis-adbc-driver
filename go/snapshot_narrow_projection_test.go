// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"testing"
)

func TestSnapshotNarrowProjectionBindsAuthoritativeValues(t *testing.T) {
	fixture, items := projectionFixture()
	fixture.meta.KeyPrefix = snapshotRows(snapshotID(), defaultSchema)
	fixture.meta.SnapshotOverlay = true
	e := &executor{store: &store{}, sess: newSession(defaultSchema)}
	ctx := context.Background()
	it, err := e.openScan(ctx, scanRequest{meta: fixture.meta, need: fixture.need,
		where: wherePlan{query: "*"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	p := newScanProjection(it, items)
	if p == nil || p.hashFields == nil {
		t.Fatal("narrow snapshot projection did not bind authoritative positional HASH values")
	}
	row := make([]Value, len(items))
	// Missing qty at an older row ID retains its default. Embedded NULs,
	// duplicated projections and integers beyond 2^53 retain exact values.
	values := []any{"7", "9007199254740993", "a\x00b", nil, nil, "1234567890123456"}
	if err := p.decodeHashInto(values, row); err != nil {
		t.Fatal(err)
	}
	if row[0].S != "a\x00b" || row[2].S != row[0].S || row[1].I != 9007199254740993 || row[3].I != 7 || row[5].I != 7 {
		t.Fatal("narrow positional values/defaults/row identity differ", row)
	}
	values[4] = "1"
	if err := p.decodeHashInto(values, row); err != nil || !row[3].Null {
		t.Fatal("explicit NULL marker lost", row, err)
	}
}

func TestSnapshotNarrowProjectionKeepsMaterializingFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*scanRequest)
	}{
		{"residual", func(r *scanRequest) { r.where.residual = &Literal{V: boolValue(true)} }},
		{"ties", func(r *scanRequest) { r.ties = &tieSort{} }},
		{"late", func(r *scanRequest) { r.late = &lateProjection{} }},
		{"pushed_limit", func(r *scanRequest) { r.limit = &[2]int64{0, 1} }},
		{"descending", func(r *scanRequest) { r.sortBy = []sortKey{{field: rowIDField, desc: true}} }},
		{"other_sort", func(r *scanRequest) { r.sortBy = []sortKey{{field: "id"}} }},
		{"unmanaged", func(r *scanRequest) { r.meta.Adopted = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, _ := projectionFixture()
			fixture.meta.KeyPrefix = snapshotRows(snapshotID(), defaultSchema)
			fixture.meta.SnapshotOverlay = true
			req := scanRequest{meta: fixture.meta, need: fixture.need, where: wherePlan{query: "*"}}
			tc.change(&req)
			ctx := context.Background()
			it, err := (&executor{store: &store{}, sess: newSession(defaultSchema)}).openScan(ctx, req, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer it.close(ctx)
			if it.hashFetch {
				t.Fatal("unsupported narrow request entered positional fetch")
			}
		})
	}
}
