// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"slices"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestSnapshotPartitionLayoutOwnsUnderlyingPin(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v TEXT)")
	h.exec("INSERT INTO t SELECT 'old-' || i::TEXT FROM generate_series(1,121) AS g(i)")
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(h.ctx, "SELECT v FROM t"); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{OptionIntPartitionCount: "1", OptionStringResultLayouts: `{"v":"string_view"}`} {
		if err := stmt.SetOption(h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	schema, parts, _, err := stmt.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if schema.Field(0).Type.ID() != arrow.STRING_VIEW {
		t.Fatal("producer schema lost layout", schema)
	}
	descriptor, err := decodePartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Snapshot == nil || descriptor.Layouts["v"] != "string_view" {
		t.Fatal("descriptor lost a required extension")
	}
	if err := stmt.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.conn.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionIntStreamBatchRows, "1"); err != nil {
		t.Fatal(err)
	}
	worker, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(h.ctx)
	other := &sqlHarness{t: t, ctx: h.ctx, conn: worker}
	other.exec("UPDATE t SET v='new'")
	other.exec("DROP TABLE t")
	reader, err := worker.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if !reader.Schema().Equal(schema) || !reader.Next() {
		t.Fatal("worker lost schema or first batch", reader.Err())
	}
	var seen []string
	appendBatch := func() {
		col := reader.RecordBatch().Column(0).(*array.StringView)
		for i := range col.Len() {
			if col.IsNull(i) {
				t.Fatal("unexpected NULL")
			}
			seen = append(seen, col.Value(i))
		}
	}
	appendBatch()
	if len(seen) != 1 {
		t.Fatal("test requires an unfinished one-row stream", len(seen))
	}
	other.exec("DROP PARTITION SNAPSHOT '" + descriptor.Snapshot.Token + "'")
	snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
	if extra, err := worker.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		extra.Release()
		t.Fatal("released group admitted a reader")
	}
	for reader.Next() {
		appendBatch()
	}
	if reader.Err() != nil {
		t.Fatal("layout wrapper released the child pin early", reader.Err())
	}
	var want []string
	for i := 1; i <= 121; i++ {
		want = append(want, fmt.Sprintf("old-%d", i))
	}
	slices.Sort(want)
	slices.Sort(seen)
	if !slices.Equal(seen, want) {
		t.Fatal("layout reader mixed or lost generations", len(seen))
	}
	for field := range raw.HGetAll(h.ctx, snapshotCatalogKey).Val() {
		if len(field) > 2 && field[:2] == "p:" {
			t.Fatal("layout reader leaked a snapshot pin")
		}
	}
}
