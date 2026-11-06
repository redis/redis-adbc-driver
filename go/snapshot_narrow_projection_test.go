// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"testing"

	goredis "github.com/redis/go-redis/v9"
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

// An in-process allocation/decoding comparison, not Redis latency evidence.
// The real production fetch helpers consume identical HMGET reply slices.
func BenchmarkSnapshotNarrowProjectionValues(b *testing.B) {
	for _, tc := range []struct {
		name                string
		positional, missing bool
	}{
		{"present/maps", false, false},
		{"present/positional", true, false},
		{"missing_defaults/maps", false, true},
		{"missing_defaults/positional", true, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			it, projection := hashProjectionFixture()
			client := goredis.NewClient(&goredis.Options{})
			b.Cleanup(func() { _ = client.Close() })
			hook := &hashSliceHook{t: b, fields: it.fetch, rows: map[string][]any{}}
			keys := make([]string, 256)
			for i := range keys {
				keys[i] = fmt.Sprintf("projection:%d", i+1)
				var qty any = "7"
				if tc.missing {
					qty = nil
				}
				hook.rows[keys[i]] = []any{fmt.Sprint(i + 1), "9007199254740993", "a\x00b", qty, nil, nil}
			}
			client.AddHook(hook)
			s := &store{client: client}
			ctx, scratch := context.Background(), make([]Value, projection.width)
			check := func(i int, err error) {
				if err != nil || scratch[0].S != "a\x00b" || scratch[1].I != 9007199254740993 ||
					scratch[2].S != scratch[0].S || scratch[5].I != int64(i+1) || !scratch[4].Null ||
					((!tc.missing || i < 10) && (scratch[3].Null || scratch[3].I != 7)) || (tc.missing && i >= 10 && !scratch[3].Null) {
					b.Fatal("incorrect complete positional/map projection", i, err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if tc.positional {
					rows, err := s.fetchRowSlices(ctx, keys, it.fetch, wideFetchChunk)
					if err != nil {
						b.Fatal(err)
					}
					for i, row := range rows {
						check(i, projection.decodeHashInto(row, scratch))
					}
				} else {
					rows, err := s.fetchRows(ctx, keys, it.fetch)
					if err != nil {
						b.Fatal(err)
					}
					for i, row := range rows {
						check(i, projection.decodeInto(row, scratch))
					}
				}
			}
		})
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
