// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotUtilitySource = "app:utility:"

func snapshotUtilitySeed(t *testing.T, h *sqlHarness, raw *goredis.Client, rows int, payload string) {
	t.Helper()
	for start := 1; start <= rows; start += 256 {
		pipe := raw.Pipeline()
		for i := start; i <= rows && i < start+256; i++ {
			fields := []any{"id", i, "exact", "9007199254740993", "payload", payload, rowIDField, "external-owner"}
			if i != 1 {
				fields = append(fields, "optional", "raw雪")
			}
			pipe.HSet(h.ctx, snapshotUtilitySource+strconv.Itoa(i), fields...)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshotUtilitySourceUnchanged(t *testing.T, h *sqlHarness, raw *goredis.Client, rows int, payload string) {
	t.Helper()
	for start := 1; start <= rows; start += 128 {
		pipe := raw.Pipeline()
		var commands []*goredis.MapStringStringCmd
		for i := start; i <= rows && i < start+128; i++ {
			commands = append(commands, pipe.HGetAll(h.ctx, snapshotUtilitySource+strconv.Itoa(i)))
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
		for j, cmd := range commands {
			i := start + j
			fields := cmd.Val()
			wantFields := 5
			if i == 1 {
				wantFields = 4
			}
			if len(fields) != wantFields || fields["id"] != strconv.Itoa(i) || fields["exact"] != "9007199254740993" ||
				fields["payload"] != payload || fields[rowIDField] != "external-owner" ||
				(i != 1 && fields["optional"] != "raw雪") {
				t.Fatalf("copy/cleanup changed application HASH %d", i)
			}
		}
	}
}

func snapshotUtilityCopyOptions() (map[string]string, HashCopyOptions) {
	return testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}), HashCopyOptions{
		Prefix: snapshotUtilitySource, Sample: 2, Table: "utility_copy",
		Types:        map[string]string{"id": "BIGINT", "exact": "BIGINT", "payload": "VARCHAR", "optional": "VARCHAR"},
		IndexColumns: []string{"id", "payload"},
	}
}

func TestSnapshotUtilityCopyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows int
		wide bool
	}{
		{"row_boundary", 100000, false},
		{"beyond_row_boundary", 100001, false},
		{"beyond_total_bytes", 33, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			payload := "raw\x00雪"
			if tc.wide {
				payload = strings.Repeat("x", (2<<20)-len(payload)) + payload
			}
			snapshotUtilitySeed(t, h, raw, tc.rows, payload)
			s := snapshotTestStore(t, raw)
			base, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			options, copyOptions := snapshotUtilityCopyOptions()
			if tc.wide {
				copyOptions.IndexColumns = []string{"id"}
			}
			runtime.GC()
			var before, after, retained runtime.MemStats
			runtime.ReadMemStats(&before)
			peak := before.HeapAlloc
			batches, total, last, maximum := 0, 0, 0, 0
			var prefix string
			previous := testHookRowsWritten
			t.Cleanup(func() { testHookRowsWritten = previous })
			testHookRowsWritten = func(meta *tableMeta, written int) {
				if meta.Name != copyOptions.Table {
					return
				}
				if written <= last {
					last = 0
				}
				delta := written - last
				last, total, maximum = written, total+delta, max(maximum, delta)
				batches++
				prefix = meta.prefix()
				if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
					t.Fatal("copy published before all input finished")
				}
				var sample runtime.MemStats
				runtime.ReadMemStats(&sample)
				peak = max(peak, sample.HeapAlloc)
			}
			started := time.Now()
			result, err := CopyHashes(h.ctx, options, copyOptions)
			elapsed := time.Since(started)
			runtime.ReadMemStats(&after)
			peak = max(peak, after.HeapAlloc)
			runtime.GC()
			runtime.ReadMemStats(&retained)
			testHookRowsWritten = previous
			if err != nil || result == nil || result.Rows != int64(tc.rows) {
				t.Fatalf("complete copy: result=%+v error=%v", result, err)
			}
			if total != tc.rows || maximum > snapshotWriteBatchRows || batches < 2 {
				t.Fatalf("copy was not bounded: rows=%d max_batch=%d batches=%d", total, maximum, batches)
			}
			h.expectRows("SELECT COUNT(*),MIN(id),MAX(id),MIN(exact),MAX(exact),COUNT(optional) FROM utility_copy",
				fmt.Sprintf("%d|1|%d|9007199254740993|9007199254740993|%d", tc.rows, tc.rows, tc.rows-1))
			h.expectRows("SELECT payload,optional FROM utility_copy WHERE id=1", payload+"|NULL")
			h.expectRows("SELECT payload,optional FROM utility_copy WHERE id=2", payload+"|raw雪")
			if exists, err := s.tableExists(base, "public", copyOptions.Table); err != nil || exists {
				t.Fatal("old reader observed the utility publication", err)
			}
			if raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(prefix))).Val() != int64(tc.rows) {
				t.Fatal("published output directory is incomplete")
			}
			snapshotUtilitySourceUnchanged(t, h, raw, tc.rows, payload)
			t.Logf("utility_profile rows=%d payload_bytes=%d elapsed_ms=%.3f batches=%d max_batch_rows=%d sampled_heap_growth=%d total_alloc_bytes=%d post_gc_heap_delta=%d directory_bytes=%d sample_row_bytes=%d",
				tc.rows, len(payload), float64(elapsed)/float64(time.Millisecond), batches, maximum,
				int64(peak)-int64(before.HeapAlloc), after.TotalAlloc-before.TotalAlloc, int64(retained.HeapAlloc)-int64(before.HeapAlloc),
				raw.MemoryUsage(h.ctx, snapshotMembers(snapshotDataID(prefix))).Val(), raw.MemoryUsage(h.ctx, prefix+"1").Val())
		})
	}
}

func TestSnapshotUtilityCopyCancellationKeepsPriorTable(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE utility_copy(id BIGINT)")
	h.exec("INSERT INTO utility_copy VALUES(42)")
	const rows = 1025
	const payload = "unchanged\x00雪"
	snapshotUtilitySeed(t, h, raw, rows, payload)
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(base, "public", "utility_copy")
	if err != nil {
		t.Fatal(err)
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	var private string
	previous := testHookRowsWritten
	t.Cleanup(func() { testHookRowsWritten = previous })
	testHookRowsWritten = func(meta *tableMeta, _ int) {
		if meta.Name == "utility_copy" {
			private = meta.prefix()
			cancel() // The first bounded row batch has been acknowledged privately.
		}
	}
	options, copyOptions := snapshotUtilityCopyOptions()
	copyOptions.Mode = adbc.OptionValueIngestModeReplace
	result, err := CopyHashes(ctx, options, copyOptions)
	testHookRowsWritten = previous
	if err == nil || result != nil || private == "" || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatalf("canceled partial copy published: result=%+v error=%v", result, err)
	}
	h.expectRows("SELECT id FROM utility_copy", "42")
	if _, err := SweepReleased(h.ctx, options, SweepOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	if raw.Exists(h.ctx, private+"1", snapshotMembers(snapshotDataID(private))).Val() != 0 {
		t.Fatal("canceled private output was not reclaimed")
	}
	if _, err := s.getTable(base, "public", "utility_copy"); err != nil || raw.HGet(h.ctx, old.prefix()+"1", "id").Val() != "42" {
		t.Fatal("cleanup removed referenced prior rows", err)
	}
	snapshotUtilitySourceUnchanged(t, h, raw, rows, payload)
}

func TestSnapshotUtilityCopyBinaryValues(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const payload = "binary\x00雪\xff"
	snapshotUtilitySeed(t, h, raw, 2, payload)
	options, copyOptions := snapshotUtilityCopyOptions()
	copyOptions.Types["payload"] = "BYTEA"
	copyOptions.IndexColumns = []string{"id"}
	if result, err := CopyHashes(h.ctx, options, copyOptions); err != nil || result.Rows != 2 {
		t.Fatalf("binary copy: result=%+v error=%v", result, err)
	}
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	if err := stmt.SetSqlQuery(h.ctx, "SELECT payload FROM utility_copy ORDER BY id"); err != nil {
		t.Fatal(err)
	}
	reader, _, err := stmt.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	rows := 0
	for reader.Next() {
		record := reader.RecordBatch()
		column, ok := record.Column(0).(*array.Binary)
		if !ok {
			t.Fatalf("unexpected BYTEA Arrow type: %T", record.Column(0))
		}
		for i := 0; i < column.Len(); i++ {
			if column.IsNull(i) || !bytes.Equal(column.Value(i), []byte(payload)) {
				t.Fatal("binary source value changed")
			}
			rows++
		}
	}
	if err := reader.Err(); err != nil || rows != 2 {
		t.Fatal("binary copy was not completely read", rows, err)
	}
	snapshotUtilitySourceUnchanged(t, h, raw, 2, payload)
}
