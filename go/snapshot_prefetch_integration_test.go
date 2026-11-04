// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

// Prefetch resolves visibility before fetching immutable winning HASHes.
// Later SQL publications cannot change delivered or queued pages.
func TestSnapshotPrefetchUpdateDeleteAfterDiscovery(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidates_%t", indexed), func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_snapshot_prefetch"
			h.dropTables(table)
			defs, fields, exprs := []string{}, []string{}, []string{}
			need := map[string]bool{}
			for j := range 64 {
				name := fmt.Sprintf("c%d", j)
				defs = append(defs, name+" BIGINT")
				fields = append(fields, name)
				need[name] = true
				expr := fmt.Sprintf("i*100+%d", j)
				if j == 0 {
					expr = "i"
				}
				if j == 1 {
					expr = "9007199254740993+i"
				}
				if j == 2 {
					expr = "CASE WHEN i%7=0 THEN NULL ELSE i*100+2 END"
				}
				exprs = append(exprs, expr)
			}
			h.exec("CREATE TABLE " + table + " (" + strings.Join(defs, ",") + ")")
			h.exec("INSERT INTO " + table + " SELECT " + strings.Join(exprs, ",") + " FROM generate_series(1,600) AS g(i)")
			h.exec("UPDATE " + table + " SET c1=-9007199254740993 WHERE c0=10")
			h.exec("DELETE FROM " + table + " WHERE c0=11")
			raw := h.rawClient()
			st, ctx, pin := h.inspectionSnapshot(raw)
			defer pin.release()
			meta, err := st.getTable(ctx, defaultSchema, table)
			if err != nil {
				t.Fatal(err)
			}
			it := &scanIter{meta: meta, store: st, fetch: fields, need: need, hashFetch: true, allowPrefetch: true, env: &evalEnv{ctx: ctx}}
			if indexed {
				it.candidates = newSnapshotCandidateScan(ctx, st, meta, "@c0:[1 600]", fields)
			} else {
				it.delta = newSnapshotDeltaScan(st, meta, fields)
			}
			seen := map[int]bool{}
			it.projection = &scanProjection{consumeHash: func(values []any) error {
				id, err := strconv.Atoi(fmt.Sprint(values[1]))
				if err != nil || id < 1 || id > 600 || id == 11 || seen[id] {
					return fmt.Errorf("invalid or duplicate visible id %d: %v", id, err)
				}
				seen[id] = true
				for j := range 64 {
					var want any = fmt.Sprint(id*100 + j)
					if j == 0 {
						want = fmt.Sprint(id)
					}
					if j == 1 {
						want = strconv.FormatInt(9007199254740993+int64(id), 10)
						if id == 10 {
							want = "-9007199254740993"
						}
					}
					if j == 2 && id%7 == 0 {
						want = nil
					}
					if values[j+1] != want {
						return fmt.Errorf("id %d column %d: got %v want %v", id, j, values[j+1], want)
					}
				}
				return nil
			}}
			defer it.close(ctx)
			if _, _, err := it.next(ctx); err != nil || it.prefetch == nil {
				t.Fatal("prefetch did not start", err)
			}
			h.exec("UPDATE " + table + " SET c1=0 WHERE c0=1")
			h.exec("DELETE FROM " + table + " WHERE c0=2")
			h.exec("TRUNCATE " + table)
			h.exec("DROP TABLE " + table)
			snapshotReapTest(st, h.ctx)
			for {
				_, _, err := it.next(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(seen) != 599 {
				t.Fatalf("snapshot returned %d rows, want 599", len(seen))
			}
			it.close(ctx)
			pin.release()
			for _, seg := range meta.snapshotSegments() {
				h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
			}
		})
	}
}
