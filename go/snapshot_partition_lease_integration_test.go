// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestSnapshotPartitionsShareProducerGeneration(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,100) AS g(i)")
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetOption(h.ctx, OptionIntPartitionCount, "4"); err != nil {
		t.Fatal(err)
	}
	_, parts, _, err := stmt.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := stmt.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringPartitionSnapshotID)
	if err != nil || id == "" {
		t.Fatal("missing release ID", err)
	}
	if err := stmt.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	worker, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(h.ctx)
	other := &sqlHarness{t: t, ctx: h.ctx, conn: worker}
	var seen []int64
	read := func(data []byte) {
		r, err := worker.ReadPartition(h.ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		for r.Next() {
			rec := r.RecordBatch()
			ids := rec.Column(0).(*array.Int64)
			vs := rec.Column(1).(*array.Int64)
			for i := 0; i < ids.Len(); i++ {
				if vs.Value(i) != 9007199254740993 {
					t.Fatal("partition mixed generations")
				}
				seen = append(seen, ids.Value(i))
			}
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
	first, err := decodePartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.Token != id {
		t.Fatal("release ID differs from descriptor")
	}
	read(parts.PartitionIDs[0])
	// Preserve schema and rows across producer close, writes, compaction, a
	// physical rewrite and recreation of the logical table under a new lineage.
	other.exec("UPDATE t SET v=42")
	other.exec("ALTER TABLE t ADD COLUMN extra INTEGER DEFAULT 7")
	if err := h.conn.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	other.exec("TRUNCATE t")
	other.exec("DROP TABLE t")
	other.exec("CREATE TABLE t(id VARCHAR)")
	other.exec("INSERT INTO t VALUES ('replacement')")
	for _, data := range parts.PartitionIDs[1:] {
		d, err := decodePartition(data)
		if err != nil {
			t.Fatal(err)
		}
		if *d.Snapshot != *first.Snapshot {
			t.Fatal("partition set contains multiple snapshots")
		}
		read(data)
	}
	slices.Sort(seen)
	if len(seen) != 100 {
		t.Fatalf("got %d rows", len(seen))
	}
	for i, v := range seen {
		if v != int64(i+1) {
			t.Fatal("missing or duplicate ID", v)
		}
	}
	other.exec("DROP PARTITION SNAPSHOT '" + id + "'")
	if r, err := worker.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		r.Release()
		t.Fatal("released descriptor remained readable")
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+id).Val() {
		t.Fatal("released group pin remains")
	}
	other.expectRows("SELECT id FROM t", "replacement")
}

func TestSnapshotPartitionReleasePreservesActiveReader(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id BIGINT)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,1201) AS g(i)")
	_, parts := planPartitions(t, h, "SELECT id FROM t", 1)
	d, err := decodePartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if !r.Next() {
		t.Fatal(r.Err())
	}
	count := int(r.RecordBatch().NumRows())
	h.exec("DROP PARTITION SNAPSHOT '" + d.Snapshot.Token + "'")
	h.exec("TRUNCATE t")
	if extra, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		extra.Release()
		t.Fatal("release admitted a new reader")
	}
	for r.Next() {
		count += int(r.RecordBatch().NumRows())
	}
	if r.Err() != nil || count != 1201 {
		t.Fatal("active reader lost its snapshot", r.Err(), count)
	}
	for field := range raw.HGetAll(h.ctx, snapshotCatalogKey).Val() {
		if len(field) > 2 && field[:2] == "p:" {
			t.Fatal("released group/child pin leaked")
		}
	}
}

func TestSnapshotPartitionExpiryAndTampering(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id BIGINT)")
	h.exec("INSERT INTO t VALUES (1),(2)")
	_, parts := planPartitions(t, h, "SELECT id FROM t", 1)
	d, err := decodePartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	bad := d
	copyRef := *d.Snapshot
	copyRef.Root = snapshotID()
	bad.Snapshot = &copyRef
	data, _ := json.Marshal(bad)
	if r, err := h.conn.ReadPartition(h.ctx, data); err == nil {
		r.Release()
		t.Fatal("forged root acquired a snapshot")
	}
	legacy := d
	legacy.Version = 1
	legacy.Snapshot = nil
	data, _ = json.Marshal(legacy)
	if r, err := h.conn.ReadPartition(h.ctx, data); !isStatus(err, adbc.StatusInvalidArgument) {
		if r != nil {
			r.Release()
		}
		t.Fatal("legacy descriptor did not fail closed", err)
	}
	var group map[string]any
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "p:"+d.Snapshot.Token).Val()), &group); err != nil {
		t.Fatal(err)
	}
	group["expires"] = 0
	data, _ = json.Marshal(group)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+d.Snapshot.Token, data).Err(); err != nil {
		t.Fatal(err)
	}
	if r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		r.Release()
		t.Fatal("expired partition acquired a new child lease")
	}
	for field := range raw.HGetAll(h.ctx, snapshotCatalogKey).Val() {
		if len(field) > 2 && field[:2] == "p:" {
			t.Fatal("expiry or rejected acquisition leaked pin")
		}
	}
}
