// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

func TestPrunedPartitionReads(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_pruned"
	h.setupStreamTable(table, 5000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("DELETE FROM " + table + " WHERE id=2")
	worker := newSQLHarness(t)
	raw := h.rawClient()
	produce := func(t *testing.T, query string, count int) (*arrow.Schema, adbc.Partitions, string) {
		t.Helper()
		schema, parts := planPartitions(t, h, query, count)
		descriptor, err := decodePartition(parts.PartitionIDs[0])
		if err != nil || descriptor.Snapshot == nil {
			t.Fatal("missing shared generation", err)
		}
		token := descriptor.Snapshot.Token
		t.Cleanup(func() { h.exec("DROP PARTITION SNAPSHOT '" + token + "'") })
		return schema, parts, token
	}
	for _, tc := range []struct {
		where string
		count int
	}{
		{"__rowid=2", 1}, {"__rowid IN (1,2,2,3,4999)", 4},
		{"__rowid BETWEEN 8 AND 10", 3}, {"__rowid>=4999", 2},
		{"__rowid<0", 1}, {"__rowid=3 AND __rowid=4", 1},
		{"__rowid=3 AND qty IS NULL", 1}, {"__rowid=3 OR id=4", 8},
		{"__rowid=2.1", 8}, {"__rowid=CAST(2 AS BIGINT)", 8},
		{"__rowid=NULL", 8}, {"20>__rowid", 8},
		{"__rowid BETWEEN 1000 AND 4000", 8},
	} {
		t.Run(tc.where, func(t *testing.T) {
			q := "SELECT id FROM " + table + " WHERE " + tc.where
			schema, parts, token := produce(t, q, 8)
			defer h.exec("DROP PARTITION SNAPSHOT '" + token + "'")
			if int(parts.NumPartitions) != tc.count {
				t.Fatalf("descriptors: got %d want %d", parts.NumPartitions, tc.count)
			}
			if tc.where == "__rowid BETWEEN 1000 AND 4000" {
				first, _ := decodePartition(parts.PartitionIDs[0])
				last, _ := decodePartition(parts.PartitionIDs[len(parts.PartitionIDs)-1])
				if first.Lo != 1000 || last.Hi != 4000 {
					t.Fatal(first.Lo, last.Hi)
				}
			}
			var actual []int64
			for _, part := range parts.PartitionIDs {
				r, err := worker.conn.ReadPartition(h.ctx, part)
				if err != nil {
					t.Fatal(err)
				}
				if !r.Schema().Equal(schema) {
					t.Fatal("schema changed")
				}
				vals, _, err := readInts(r)
				r.Release()
				if err != nil {
					t.Fatal(err)
				}
				actual = append(actual, vals...)
			}
			r, _ := h.openStream(h.ctx, h.conn, q, nil, nil)
			expected, _, err := readInts(r)
			r.Release()
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(actual)
			slices.Sort(expected)
			if !slices.Equal(actual, expected) {
				t.Fatalf("got %v want %v", actual, expected)
			}
		})
	}
	// A pruned capability retains its producer generation through later writes.
	_, parts, oldToken := produce(t, "SELECT id FROM "+table+" WHERE __rowid>=4999", 1)
	old := h.inspectTable(raw, defaultSchema, table)
	h.exec("UPDATE " + table + " SET id=9000 WHERE __rowid=4999")
	h.exec("DELETE FROM " + table + " WHERE __rowid=5000")
	h.exec("INSERT INTO " + table + " (id,label,qty) VALUES(5001,'later',0)")
	r, err := worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err := readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, []int64{4998, 4999}) {
		t.Fatal(vals, err)
	}
	h.expectRows("SELECT id FROM "+table+" WHERE __rowid>=4999 ORDER BY __rowid", "9000", "5001")
	// Small Arrow batches exercise pending-page cancellation and owner closure.
	_, parts, token := produce(t, "SELECT id FROM "+table+" WHERE __rowid BETWEEN 1 AND 1024", 1)
	expectedReader, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM "+table+" WHERE __rowid BETWEEN 1 AND 1024 ORDER BY __rowid", nil, nil)
	expected, _, err := readInts(expectedReader)
	expectedReader.Release()
	if err != nil {
		t.Fatal(err)
	}
	pinned := h.inspectTable(raw, defaultSchema, table)
	worker.conn.Close(h.ctx)
	worker = openWith(t, os.Getenv("REDIS_URI"), map[string]string{OptionIntStreamBatchRows: "10"})
	ctx, cancel := context.WithCancel(h.ctx)
	r, err = worker.conn.ReadPartition(ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next() || r.RecordBatch().NumRows() != 10 {
		t.Fatal("bounded batch")
	}
	cancel()
	if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
		t.Fatal(r.Err())
	}
	r.Release()
	r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	worker.conn.Close(h.ctx)
	if r.Next() || !isStatus(r.Err(), adbc.StatusInvalidState) {
		t.Fatal(r.Err())
	}
	r.Release()
	worker = newSQLHarness(t)
	r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	h.exec("TRUNCATE " + table)
	h.exec("DROP TABLE " + table)
	snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
	vals, _, err = readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, expected) {
		t.Fatal("pinned reader changed after retirement", vals, err)
	}
	r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err = readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, expected) {
		t.Fatal("pinned descriptor changed after retirement", vals, err)
	}
	h.exec("DROP PARTITION SNAPSHOT '" + oldToken + "'")
	h.exec("DROP PARTITION SNAPSHOT '" + token + "'")
	for _, descriptor := range []*tableMeta{old, pinned} {
		for _, segment := range descriptor.snapshotSegments() {
			h.expectSnapshotReleased(raw, segment.Prefix, segment.Index)
		}
	}
	if r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "partition snapshot expired or released") {
		if r != nil {
			r.Release()
		}
		t.Fatal("released generation remained available", err)
	}
}

func TestPrunedPartitionPermissions(t *testing.T) {
	a := newACLHarness(t)
	const table = "it_partition_pruned_acl"
	a.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { a.exec("DROP TABLE IF EXISTS " + table) })
	a.exec("INSERT INTO " + table + " VALUES(1),(2),(3)")
	_, parts := planPartitions(t, a.sqlHarness, "SELECT id FROM "+table+" WHERE __rowid IN(1,1,3)", 1)
	const user = "it_acl_pruned_partitions"
	a.user(user, append(a.readOnlyCommands(), "%R~adbc:*", "%R~public:*", "-ft.info", "-ft.search", "-ft.aggregate", "-ft.cursor", "-ft.profile")...)
	worker := a.connect(user, nil)
	r, err := worker.conn.ReadPartition(a.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err := readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, []int64{1, 3}) {
		t.Fatal(vals, err)
	}
	if err := a.eachNode(func(n *goredis.Client) error { return n.Do(a.ctx, "ACL", "SETUSER", user, "-hmget").Err() }); err != nil {
		t.Fatal(err)
	}
	r, err = worker.conn.ReadPartition(a.ctx, parts.PartitionIDs[0])
	if err == nil {
		if r == nil {
			t.Fatal("missing both partition reader and permission error")
		}
		if r.Next() {
			r.Release()
			t.Fatal("denied HMGET returned partition values")
		}
		err = r.Err()
		r.Release()
	} else if r != nil {
		r.Release()
	}
	// HMGET may be required by immutable metadata admission before row
	// iteration. Either phase must reject this exact command with no values.
	if !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "NOPERM") || !strings.Contains(strings.ToLower(err.Error()), "hmget") {
		t.Fatal("denied HMGET permission bypassed", err)
	}
}
