// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"os"
	"strconv"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestSnapshotImportedSourceKeyIdentity(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	keys := []string{"app:alice", "app:bob", "app:nul\x00A", "app:nul\x00B", "app:separator\x1f雪"}
	for i, key := range keys {
		if err := raw.HSet(h.ctx, key, "v", 10+i).Err(); err != nil {
			t.Fatal(err)
		}
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	adopt := func(table string) {
		t.Helper()
		result, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "app:", Table: table, Apply: true, OwnIndex: true})
		if err != nil || result == nil || !result.Applied {
			t.Fatalf("import: %v", err)
		}
	}
	adopt("copied")
	for i, key := range keys {
		h.expectRows("SELECT v FROM copied WHERE __key='"+key+"'", strconv.Itoa(10+i))
	}
	h.expectRows("SELECT COUNT(DISTINCT __key),COUNT(*) FROM copied", "5|5")
	h.expectRows("SELECT __key FROM copied WHERE __key LIKE 'app:nul%' ORDER BY __key", "app:nul\x00A", "app:nul\x00B")
	if schema := h.expectRows("SELECT * FROM copied WHERE __key='app:alice'", "10"); schema.NumFields() != 1 {
		t.Fatal("source provenance leaked into SELECT *")
	}
	schema, err := h.conn.GetTableSchema(h.ctx, nil, nil, "copied")
	if err != nil || schema.NumFields() != 1 {
		t.Fatalf("public schema exposed provenance: %v", err)
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.columns WHERE table_name='copied'", "1")
	_, partitions := planPartitions(t, h, "SELECT __key,v FROM copied", 2)
	if len(partitions.PartitionIDs) == 0 {
		t.Fatal("no source-key partitions")
	}
	descriptor, err := decodePartition(partitions.PartitionIDs[0])
	if err != nil || descriptor.Snapshot == nil {
		t.Fatalf("source-key partition has no shared snapshot: %v", err)
	}
	defer h.exec("DROP PARTITION SNAPSHOT '" + descriptor.Snapshot.Token + "'")
	r, _ := h.openStream(h.ctx, h.conn, "SELECT __key,v FROM copied", nil, map[string]string{OptionIntStreamBatchRows: "1"})
	defer r.Release()
	h.expectRows("UPDATE copied SET v=110 WHERE __key='app:alice' RETURNING __key,v", "app:alice|110")
	h.expectRows("DELETE FROM copied WHERE __key='app:bob' RETURNING __key", "app:bob")
	h.expectRows("SELECT COUNT(*) FROM copied WHERE __key='app:bob'", "0")
	h.expectRows("INSERT INTO copied(v) VALUES(99) RETURNING __key,v", "NULL|99")
	snapshotDrainCompactionTest(t, snapshotTestStore(t, raw))
	h.expectRows("SELECT v FROM copied WHERE __key IS NULL", "99")
	h.expectRows("SELECT COUNT(DISTINCT __key),COUNT(*) FROM copied", "4|5")
	seen := map[string]int64{}
	for r.Next() {
		record := r.RecordBatch()
		for i := 0; i < int(record.NumRows()); i++ {
			seen[record.Column(0).(*array.String).Value(i)] = record.Column(1).(*array.Int64).Value(i)
		}
	}
	if r.Err() != nil || len(seen) != len(keys) {
		t.Fatalf("old source-key stream changed: %v %v", seen, r.Err())
	}
	for i, key := range keys {
		if seen[key] != int64(10+i) || raw.HGet(h.ctx, key, "v").Val() != strconv.Itoa(10+i) {
			t.Fatal("SQL changed a pinned value or application HASH")
		}
	}
	r.Release()
	h.exec("ALTER TABLE copied ADD COLUMN n BIGINT DEFAULT 9007199254740993")
	h.exec("ALTER TABLE copied ALTER COLUMN v TYPE BIGINT")
	h.expectRows("SELECT __key,v,n FROM copied WHERE __key='app:alice'", "app:alice|110|9007199254740993")
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE copied RENAME TO renamed")
	h.expectRows("SELECT __key,v FROM renamed WHERE __key='app:alice'", "app:alice|110")
	if err := raw.HSet(h.ctx, "app:alice", "v", "42").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM renamed WHERE __key='app:alice'", "110")
	adopt("renamed")
	h.expectRows("SELECT __key,v FROM renamed WHERE __key='app:alice'", "app:alice|42")
	h.expectRows("SELECT COUNT(*) FROM renamed WHERE __key IS NULL", "0")
	h.exec("TRUNCATE renamed")
	h.expectRows("SELECT COUNT(__key) FROM renamed", "0")
	partitionRows := map[string]int64{}
	for _, descriptor := range partitions.PartitionIDs {
		reader, err := h.conn.ReadPartition(h.ctx, descriptor)
		if err != nil {
			t.Fatal(err)
		}
		for reader.Next() {
			record := reader.RecordBatch()
			for i := 0; i < int(record.NumRows()); i++ {
				key := record.Column(0).(*array.String).Value(i)
				if _, duplicate := partitionRows[key]; duplicate {
					t.Fatal("partition duplicated a source key")
				}
				partitionRows[key] = record.Column(1).(*array.Int64).Value(i)
			}
		}
		err = reader.Err()
		reader.Release()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(partitionRows) != len(keys) {
		t.Fatal("partition source-key coverage changed after refresh/TRUNCATE")
	}
	for i, key := range keys {
		if partitionRows[key] != int64(10+i) {
			t.Fatal("partition did not retain the original source key/value")
		}
	}
}

func TestSnapshotImportedSourceKeyLateProjection(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	if err := raw.HSet(h.ctx, "app:origin", "v", "1", "a", "a", "b", "b", "c", "c").Err(); err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "app:", Table: "copied", Apply: true, OwnIndex: true}); err != nil {
		t.Fatal(err)
	}
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	e := &executor{store: s, schema: "public", sess: newSession("public"), pushdown: PushdownExact}
	// Force the unrestricted-cap planner branch on a one-row input that fits
	// the real fixture cap, retaining coverage of late projection deployments.
	s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
	parsed, _ := ParseScript("SELECT __key FROM copied WHERE v%2=1 ORDER BY a LIMIT 1")
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := e.prepareScan(ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scan.req.late == nil {
		t.Fatal("test did not select late projection")
	}
	rows, err := e.selectScanned(ctx, plan, scan, nil)
	if err != nil || len(rows) != 1 || rows[0][0].Null || rows[0][0].S != "app:origin" {
		t.Fatalf("late projection lost the original key: %v %v", rows, err)
	}
	h.expectRows("SELECT __key FROM copied WHERE v%2=1 ORDER BY a LIMIT 1", "app:origin")
}
