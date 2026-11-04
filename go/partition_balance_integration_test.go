// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"reflect"
	"slices"
	"testing"
)

func TestBalancedPartitionReads(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_balanced"
	h.setupStreamTable(table, 25000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("DELETE FROM " + table + " WHERE id < 22500")
	h.inspectTable(h.rawClient(), defaultSchema, table) // validate every pinned segment/index
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	value, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringPartitionStrategy)
	if err != nil || value != "rowid" {
		t.Fatal(value, err)
	}
	if err := st.SetOption(h.ctx, OptionStringPartitionStrategy, "auto"); err == nil {
		t.Fatal("accepted unknown strategy")
	}
	if err := st.SetOption(h.ctx, OptionStringPartitionStrategy, "balanced"); err != nil {
		t.Fatal(err)
	}
	if err := st.(adbc.GetSetOptionsWithContext).SetOptionInt(h.ctx, OptionIntPartitionCount, 4); err != nil {
		t.Fatal(err)
	}
	st.SetOption(h.ctx, OptionStringIncremental, "true")
	st.SetSqlQuery(h.ctx, "SELECT id FROM "+table)
	schema, parts, _, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if parts.NumPartitions != 4 {
		t.Fatal(parts.NumPartitions)
	}
	retainRelease := func(parts adbc.Partitions) *partitionSnapshot {
		t.Helper()
		d, err := decodePartition(parts.PartitionIDs[0])
		if err != nil || d.Snapshot == nil {
			t.Fatal("balanced descriptors lack a shared generation", err)
		}
		t.Cleanup(func() { h.exec("DROP PARTITION SNAPSHOT '" + d.Snapshot.Token + "'") })
		for _, data := range parts.PartitionIDs {
			part, err := decodePartition(data)
			if err != nil || part.Snapshot == nil || *part.Snapshot != *d.Snapshot {
				t.Fatal("balanced descriptors use different generations", err)
			}
		}
		return d.Snapshot
	}
	first := retainRelease(parts)
	_, empty, _, err := st.ExecutePartitions(h.ctx)
	if err != nil || empty.NumPartitions != 0 {
		t.Fatal("nonterminal discovery", err)
	}
	// Setting strategy resets discovery without revoking the earlier descriptors.
	if err := st.SetOption(h.ctx, OptionStringPartitionStrategy, "balanced"); err != nil {
		t.Fatal(err)
	}
	_, again, _, err := st.ExecutePartitions(h.ctx)
	if err != nil || again.NumPartitions != 4 {
		t.Fatal("strategy did not reset discovery", err)
	}
	second := retainRelease(again)
	if first.Root != second.Root || first.Token == second.Token {
		t.Fatal("reset did not independently retain the same committed generation")
	}
	st.Close(h.ctx)
	h.exec("INSERT INTO " + table + " (id,label,qty) VALUES(25000,'new',0)")
	h.exec("UPDATE " + table + " SET id=-1 WHERE id=23000")
	h.exec("DELETE FROM " + table + " WHERE id=22500")
	h.expectRows("SELECT COUNT(*) FROM "+table, "2500")
	h.expectRows("SELECT id FROM "+table+" WHERE id IN (-1,25000) ORDER BY id", "-1", "25000")
	h.exec("TRUNCATE " + table)
	h.exec("DROP TABLE " + table)
	snapshotReapTest(&store{client: h.rawClient(), snapshots: true}, h.ctx)
	worker := newSQLHarness(t)
	var actual []int64
	var previous int64
	for i, data := range parts.PartitionIDs {
		d, err := decodePartition(data)
		if err != nil {
			t.Fatal(err)
		}
		if d.Lo != previous+1 || i == 3 && d.Hi != 25000 {
			t.Fatalf("coverage changed: %+v", d)
		}
		previous = d.Hi
		r, err := worker.conn.ReadPartition(h.ctx, data)
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
		if len(vals) != 625 {
			t.Fatalf("descriptor %d has %d rows, expected 625", i, len(vals))
		}
		actual = append(actual, vals...)
	}
	slices.Sort(actual)
	if len(actual) != 2500 {
		t.Fatal(len(actual))
	}
	for i, v := range actual {
		if v != int64(22500+i) {
			t.Fatalf("coverage at %d: %d", i, v)
		}
	}
	canceled, cancel := context.WithCancel(h.ctx)
	cancel()
	st2, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close(h.ctx)
	st2.SetSqlQuery(h.ctx, "SELECT id FROM "+table)
	st2.SetOption(h.ctx, OptionStringPartitionStrategy, "balanced")
	if _, _, _, err := st2.ExecutePartitions(canceled); !isStatus(err, adbc.StatusCancelled) {
		t.Fatal("ignored planning cancellation", err)
	}
}

func TestSnapshotBalancedPartitionCountBudget(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER)")
	for start := 1; start <= 6000; start += 1000 {
		h.exec(fmt.Sprintf("INSERT INTO t SELECT i FROM generate_series(%d,%d) AS g(i)", start, start+999))
	}
	const last = int64(1_000_000_000)
	const partitions = 128
	h.sparseSnapshotTable("t", last, nil)
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, defaultSchema, "t")
	if err != nil {
		t.Fatal(err)
	}
	spans, err := (&executor{store: s}).balancedPartitionSpans(ctx, meta, 1, last, partitions, 0)
	if err != nil || !reflect.DeepEqual(spans, equalPartitionSpans(1, last, partitions)) {
		t.Fatalf("partial count budget did not discard quantiles: %v %v", spans, err)
	}
	if requests := guard.search.Load(); requests < 1 || requests > partitionBalanceRequests {
		t.Fatalf("segmented counts escaped the shared request budget: %d", requests)
	}
	if guard.rows.Load() != 0 || guard.masks.Load() != 0 {
		t.Fatal("balancing transferred row images or enumerated contiguous IDs")
	}
	h.expectRows("SELECT COUNT(*),MIN(id),MAX(id) FROM t", "6000|1|6000")
}
