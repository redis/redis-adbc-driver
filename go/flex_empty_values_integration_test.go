// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// Native empty-term postings are unnecessary for managed Flex reads. Exercise
// values whose native TAG representation is empty or lossy through the public
// Arrow/SQL lifecycle, including a compacted generation and replacement.
func TestSnapshotFlexEmptyValues(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	h.exec("CREATE TABLE t(id BIGINT,s TEXT)")
	nul := string([]byte{0, 't', 'a', 'i', 'l'})
	if strings.IndexByte(nul, 0) != 0 {
		t.Fatal("fixture must contain a real NUL byte")
	}
	values := []string{"", "", " ", tagSeparator, nul, "x", "x" + nul, "", "X"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "s", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	ingest := func(mode string) {
		t.Helper()
		builder := array.NewRecordBuilder(mem, schema)
		for i, value := range values {
			builder.Field(0).(*array.Int64Builder).Append(int64(i + 1))
			if i == 1 {
				builder.Field(1).(*array.StringBuilder).AppendNull()
			} else {
				builder.Field(1).(*array.StringBuilder).Append(value)
			}
		}
		batch := builder.NewRecordBatch()
		builder.Release()
		reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
		batch.Release()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Release()
		stmt, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close(h.ctx)
		if err := stmt.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, "t"); err != nil {
			t.Fatal(err)
		}
		if err := stmt.SetOption(h.ctx, adbc.OptionKeyIngestMode, mode); err != nil {
			t.Fatal(err)
		}
		if err := stmt.BindStream(h.ctx, reader); err != nil {
			t.Fatal(err)
		}
		if n, err := stmt.ExecuteUpdate(h.ctx); err != nil || n != int64(len(values)) {
			t.Fatalf("%s acknowledged %d rows: %v", mode, n, err)
		}
	}
	checkBase := func() {
		t.Helper()
		h.expectRows("SELECT id FROM t WHERE s='' ORDER BY id", "1", "8")
		h.expectRows("SELECT id FROM t WHERE s IS NULL", "2")
		h.expectRows("SELECT id FROM t WHERE s<>'' ORDER BY id", "3", "4", "5", "6", "7", "9")
		h.expectRows("SELECT id FROM t WHERE s IN ('','x') ORDER BY id", "1", "6", "8")
		h.expectRows("SELECT id FROM t WHERE s='' OR s IS NULL ORDER BY id", "1", "2", "8")
		h.expectRows("SELECT id FROM t WHERE s=' '", "3")
		h.expectRows("SELECT s FROM t WHERE id=5", nul)
		h.expectRows("SELECT s FROM t WHERE id=7", "x"+nul)
		h.expectRows("SELECT COUNT(*),COUNT(s),COUNT(DISTINCT s) FROM t", "9|8|7")
		h.expectRows("SELECT SUM(id) FROM t WHERE s=''", "9")
		h.expectRows("SELECT s,COUNT(*) FROM t GROUP BY s ORDER BY s NULLS FIRST",
			"NULL|1", "|2", nul+"|1", tagSeparator+"|1", " |1", "X|1", "x|1", "x"+nul+"|1")
		assertFlexEmptyValueIndexes(t, h, raw)
	}
	ingest(adbc.OptionValueIngestModeAppend)
	checkBase()
	h.exec("INSERT INTO t VALUES(10,'')")
	h.exec("UPDATE t SET s='' WHERE id=2")
	h.exec("DELETE FROM t WHERE id=6")
	h.expectRows("SELECT id FROM t WHERE s IN ('','x') ORDER BY id", "1", "2", "8", "10")
	h.expectRows("SELECT COUNT(*),COUNT(s) FROM t", "9|9")
	beforeCompaction := assertFlexEmptyValueIndexes(t, h, raw)
	store := snapshotTestStore(t, raw)
	store.flex = true
	snapshotDrainCompactionTest(t, store)
	h.expectRows("SELECT id FROM t WHERE s='' ORDER BY id", "1", "2", "8", "10")
	h.expectRows("SELECT s FROM t WHERE id=5", nul)
	afterCompaction := assertFlexEmptyValueIndexes(t, h, raw)
	compacted := false
	for index := range afterCompaction {
		if !beforeCompaction[index] {
			compacted = true
		}
	}
	if !compacted {
		t.Fatal("compaction did not publish a new physical index")
	}
	ingest(adbc.OptionValueIngestModeReplace)
	checkBase()
}

func assertFlexEmptyValueIndexes(t *testing.T, h *sqlHarness, raw goredis.UniversalClient) map[string]bool {
	t.Helper()
	store, ctx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	store.flex = true
	meta, err := store.getTable(ctx, defaultSchema, "t")
	if err != nil {
		t.Fatal(err)
	}
	indexes := make(map[string]bool)
	for _, segment := range meta.snapshotSegments() {
		indexes[segment.Index] = true
		reply, err := raw.Do(ctx, "FT.INFO", segment.Index).Result()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, attr := range indexInfoOf(reply).attrs {
			if attr.identifier == "s" {
				found = true
				if attr.typ != "TAG" || attr.hasOption("INDEXEMPTY") || attr.hasOption("SORTABLE") || attr.hasOption("UNF") {
					t.Fatalf("unexpected plain TAG attributes: %s", fmt.Sprint(attr))
				}
			}
		}
		if !found {
			t.Fatal("public TAG attribute was removed")
		}
	}
	return indexes
}
