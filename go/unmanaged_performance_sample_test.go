// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Opt-in diagnostic, not a full table-mode/ClickBench rerun. All stored HASHes
// and complete results are independently checked outside the timed intervals.
func TestUnmanagedPerformanceSample(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_PERF") != "true" {
		t.Skip("set REDIS_UNMANAGED_PERF=true for timed diagnostic")
	}
	h, raw, db := newSnapshotHarness(t)
	const count = 4096
	const width = 64
	fields := []any{"n", "0"}
	types := map[string]string{"n": "BIGINT"}
	for c := 1; c < width; c++ {
		name := fmt.Sprintf("c%d", c)
		fields = append(fields, name, "0")
		types[name] = "BIGINT"
	}
	if err := raw.HSet(h.ctx, "sample:seed", fields...).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "sample_idx", "ON", "HASH", "PREFIX", 1, "sample:", "SCHEMA", "n", "NUMERIC", "SORTABLE").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "sample_idx"); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptHashes(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, HashAdoptOptions{Mode: "unmanaged", Prefix: "sample:", Table: "sample", Types: types, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Del(h.ctx, "sample:seed").Err(); err != nil {
		t.Fatal(err)
	}
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "sample")
	if err != nil {
		t.Fatal(err)
	}
	meta = insertMeta(meta)
	ctx = context.WithValue(ctx, unmanagedWriteKey{}, metaKey("public", "sample"))
	rows := make([][]Value, count)
	for i := range rows {
		rows[i] = make([]Value, len(meta.Columns))
		for c, col := range meta.Columns {
			if col.Name == keyField {
				rows[i][c] = stringValue(fmt.Sprintf("sample:%d", i+1))
				continue
			}
			value := int64(i + 1)
			if col.Name != "n" {
				var column int
				fmt.Sscanf(col.Name, "c%d", &column)
				value = value*100 + int64(column)
			}
			rows[i][c] = intValue(typeInt64, value)
		}
	}
	begin := time.Now()
	n, err := s.insertRows(ctx, meta, rows)
	writeTime := time.Since(begin)
	if err != nil || n != count {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := s.awaitIndexReady(h.ctx, "sample_idx"); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		got, err := raw.HGetAll(h.ctx, fmt.Sprintf("sample:%d", i+1)).Result()
		if err != nil || len(got) != width {
			t.Fatalf("stored row %d: width=%d err=%v", i, len(got), err)
		}
		for c, col := range meta.Columns {
			if col.Name != keyField && got[col.field()] != fmt.Sprint(row[c].I) {
				t.Fatalf("stored row %d field %s differs", i, col.Name)
			}
		}
	}
	expected := make([]string, count)
	for i, row := range rows {
		var cells []string
		for c, col := range meta.Columns {
			if col.Name != keyField {
				cells = append(cells, fmt.Sprint(row[c].I))
			}
		}
		expected[i] = strings.Join(cells, "|")
	}
	sort.Strings(expected)
	for _, workers := range []int{1, 4} {
		connections := make([]adbc.ConnectionWithContext, workers)
		for i := range connections {
			connections[i], err = db.Open(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer connections[i].Close(h.ctx)
		}
		for trial := 0; trial < 3; trial++ {
			tasks := 1
			if workers == 4 {
				tasks = 8
			}
			results := make(chan []string, workers)
			begin = time.Now()
			for w := range workers {
				go func(w int) {
					var found []string
					defer func() { results <- found }()
					worker := &sqlHarness{t: t, ctx: h.ctx, conn: connections[w]}
					for task := w; task < tasks; task += workers {
						q := "SELECT * FROM sample"
						if tasks > 1 {
							q += fmt.Sprintf(" WHERE n BETWEEN %d AND %d", task*count/tasks+1, (task+1)*count/tasks)
						}
						got, _ := worker.query(q)
						found = append(found, got...)
					}
				}(w)
			}
			var got []string
			for range workers {
				got = append(got, (<-results)...)
			}
			readTime := time.Since(begin)
			sort.Strings(got)
			if len(got) != len(expected) {
				t.Fatalf("read rows=%d want %d", len(got), len(expected))
			}
			for i := range got {
				if got[i] != expected[i] {
					t.Fatalf("read row %d differs", i)
				}
			}
			t.Logf("sample rows=%d columns=%d workers=%d trial=%d write=%s read=%s", count, width, workers, trial, writeTime, readTime)
		}
	}
}
