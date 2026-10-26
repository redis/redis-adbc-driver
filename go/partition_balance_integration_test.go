// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"github.com/apache/arrow-adbc/go/adbc"
	"slices"
	"testing"
)

func TestBalancedPartitionReads(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_balanced"
	h.setupStreamTable(table, 25000)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("DELETE FROM " + table + " WHERE id < 22500")
	sharedScanPlan(t, h, "SELECT id FROM "+table, nil) // wait for fixture readiness
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
	st.Close(h.ctx)
	h.exec("INSERT INTO " + table + " (id,label,qty) VALUES(25000,'new',0)")
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
	if _, _, _, err := st2.ExecutePartitions(canceled); err == nil {
		t.Fatal("ignored planning cancellation")
	}
}
