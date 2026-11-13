// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestUnmanagedFilteredProjectionLiveMutation(t *testing.T) {
	for _, change := range []string{"changed", "deleted", "wrongtype"} {
		t.Run(change, func(t *testing.T) {
			h, raw, _ := unmanagedIngestFixture(t, false)
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			meta, err := s.getTable(ctx, "public", "live")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.loadUnmanagedIndex(ctx, meta); err != nil {
				t.Fatal(err)
			}
			e := &executor{store: s, diagnostics: &executionDiagnostics{}}
			predicate := filteredComparison(">=", "n", 9)
			wp, err := e.planWhere(ctx, predicate, meta, nil)
			if err != nil {
				t.Fatal(err)
			}
			it, err := e.openScan(ctx, scanRequest{meta: meta, where: wp, need: map[string]bool{"n": true}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer it.close(ctx)
			it.projection = newScanProjection(it, []planItem{{expr: &ColumnRef{Name: "n"}}})
			if !it.unmanagedHashFetch || it.projection == nil || it.projection.predicate == nil {
				t.Fatal("live residual was not bound before source mutation")
			}
			switch change {
			case "changed":
				err = raw.HSet(h.ctx, "app:seed", "n", "1").Err()
			case "deleted":
				err = raw.Del(h.ctx, "app:seed").Err()
			case "wrongtype":
				if err = raw.Del(h.ctx, "app:seed").Err(); err == nil {
					err = raw.Set(h.ctx, "app:seed", "string", 0).Err()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			keys, rows, err := it.nextPage(ctx)
			if err != nil || len(keys) != 0 || len(rows) != 0 || len(it.projection.rows) != 0 || e.diagnostics.matched.Load() != 0 {
				t.Fatalf("stale/non-HASH candidate survived filtered projection: keys=%v rows=%v err=%v", keys, rows, err)
			}
		})
	}
}

func TestUnmanagedFilteredArrowLifetime(t *testing.T) {
	h, query, _ := unmanagedWideProjectionFixture(t, 9001)
	query += " WHERE id >= 2 AND id <= 9000"
	for _, action := range []string{"finish", "release", "cancel"} {
		t.Run(action, func(t *testing.T) {
			st, reader, count := h.openStmtStream(h.ctx, h.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "127"})
			released := false
			defer func() {
				if !released {
					reader.Release()
				}
			}()
			if count != -1 || !reader.Next() {
				t.Fatal("filtered fixture did not stream", count, reader.Err())
			}
			stream, ok := inspectionReader(reader).(*streamReader)
			if !ok || stream.it.projection == nil || stream.it.projection.predicate == nil || stream.arrow == nil || !stream.it.unmanagedHashFetch || len(stream.pending) != 0 {
				t.Fatal("filtered HGETALL did not choose direct Arrow projection")
			}
			retained := reader.RecordBatch()
			retained.Retain()
			defer retained.Release()
			seen := make([]bool, 9001)
			validateMemoryRecord(t, retained, seen)
			if !reader.Next() {
				t.Fatal("missing second filtered batch", reader.Err())
			}
			validateMemoryRecord(t, reader.RecordBatch(), seen)
			switch action {
			case "finish":
				for reader.Next() {
					validateMemoryRecord(t, reader.RecordBatch(), seen)
				}
				if reader.Err() != nil {
					t.Fatal(reader.Err())
				}
				for i, actual := range seen {
					if actual != (i >= 1 && i <= 8999) {
						t.Fatal("filtered membership changed", i+1, actual)
					}
				}
			case "cancel":
				if err := canceler(t, st).Cancel(h.ctx); err != nil {
					t.Fatal(err)
				}
				if reader.Next() || !isStatus(reader.Err(), adbc.StatusCancelled) {
					t.Fatal("cancel delivered buffered filtered output", reader.Err())
				}
			}
			reader.Release()
			released = true
			validateMemoryRecord(t, retained, make([]bool, 9001))
		})
	}
}
