// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSQLCompactIntegerTuples(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_compact_integer"
	h.dropTables(table)
	defs := []string{"g INTEGER", "u BIGINT", "a SMALLINT", "w SMALLINT DEFAULT 11"}
	for i := range 61 {
		defs = append(defs, fmt.Sprintf("p%d INTEGER DEFAULT 0", i))
	}
	h.exec("CREATE TABLE " + table + " (" + strings.Join(defs, ",") + ")")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	// More than 4096 unique complete tuples per range; the second half repeats
	// users from the first. NULL/zero, signed extremes and adjacent >2^53 users
	// must survive compaction, flush boundaries and the exact global merge.
	for start := 0; start < 10000; start += 250 {
		var rows []string
		for i := start; i < start+250; i++ {
			u := fmt.Sprint(int64(9007199254740992) + int64(i%5000))
			if i%13 == 0 {
				u = "NULL"
			}
			if i%17 == 0 {
				u = fmt.Sprint(-int64(9007199254740992) - int64(i%5000))
			}
			if i == 1 {
				u = "9223372036854775807"
			}
			if i == 2 {
				u = "-9223372036854775808"
			}
			g := fmt.Sprint((i % 5000) % 3)
			if i%19 == 0 {
				g = "NULL"
			}
			a := fmt.Sprint(i%7 - 3)
			if i%11 == 0 {
				a = "NULL"
			}
			rows = append(rows, "("+g+","+u+","+a+")")
		}
		h.exec("INSERT INTO " + table + " (g,u,a) VALUES " + strings.Join(rows, ","))
	}
	h.exec("ALTER TABLE " + table + " ADD COLUMN d SMALLINT DEFAULT 11")
	h.exec("UPDATE " + table + " SET w=NULL WHERE __rowid IN (3,5003)")
	h.exec("DELETE FROM " + table + " WHERE __rowid IN (4,5004)")
	_, defaultPlan, _ := distinctTestPlan(t, h, "SELECT g,AVG(d),COUNT(DISTINCT u) FROM "+table+" GROUP BY g")
	if _, ok := tupleAggregateShape(defaultPlan, defaultPlan.aggregateCalls()); ok {
		t.Fatal("missing-default column lost fallback")
	}
	for _, sql := range []string{
		"SELECT g,COUNT(DISTINCT u) FROM " + table + " GROUP BY g",
		"SELECT g,SUM(a),COUNT(*),AVG(w),COUNT(DISTINCT u) FROM " + table + " GROUP BY g",
		"SELECT COUNT(DISTINCT u) FROM " + table,
	} {
		t.Run(sql, func(t *testing.T) {
			e, plan, wp := distinctTestPlan(t, h, sql)
			p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
			if !ok || !compactIntegerTupleShape(plan.meta, p, wp) {
				t.Fatal("shape not eligible")
			}
			want, err := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, compact := range []bool{false, true} {
				p.compactRows = compact
				spill := tupleSpill{dir: t.TempDir(), prefix: "compact"}
				for _, r := range [][2]int64{{1, 5000}, {5001, 10000}} {
					if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, r[0], r[1], false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
						t.Fatal(err)
					}
				}
				got, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill)
				if err != nil {
					t.Fatal(err)
				}
				if a, b := aggregateGroupsText(got, plan), aggregateGroupsText(want, plan); !reflect.DeepEqual(a, b) {
					t.Fatalf("compact=%v got %v want %v", compact, a, b)
				}
			}
			p.compactRows = true
			sentinel := errors.New("tuple consumer failed")
			if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, 1, 10000, false, func(spillTuple) error { return sentinel }); !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(h.ctx)
			cancel()
			if err := e.aggregateTuplePartition(ctx, plan, p, wp, nil, 1, 10000, false, func(spillTuple) error { return nil }); err == nil {
				t.Fatal("cancel ignored")
			}
			for _, where := range []string{" WHERE g=1", " WHERE g%2=0"} {
				_, _, filtered := distinctTestPlan(t, h, "SELECT * FROM "+table+where)
				if compactIntegerTupleShape(plan.meta, p, filtered) {
					t.Fatal("filtered source escaped guard")
				}
			}
		})
	}
	// Overflow is checked on the final exact weighted sum, after all tuples.
	e, plan, wp := distinctTestPlan(t, h, "SELECT SUM(u),COUNT(DISTINCT u) FROM "+table)
	p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
	if !ok || !compactIntegerTupleShape(plan.meta, p, wp) {
		t.Fatal("overflow shape unavailable")
	}
	_, wantErr := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
	if wantErr == nil {
		t.Fatal("overflow reference unexpectedly succeeded")
	}
	for _, compact := range []bool{false, true} {
		p.compactRows = compact
		spill := tupleSpill{dir: t.TempDir(), prefix: "overflow"}
		if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, 1, 10000, false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
			t.Fatal(err)
		}
		if _, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill); err == nil || err.Error() != wantErr.Error() {
			t.Fatalf("overflow compact=%v got %v want %v", compact, err, wantErr)
		}
	}
	// An empty source still produces SQL's scalar COUNT=0 and SUM=NULL.
	var emptyResults []string
	for _, compact := range []bool{false, true} {
		p.compactRows = compact
		spill := tupleSpill{dir: t.TempDir(), prefix: "empty"}
		if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, 20000, 30000, false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
			t.Fatal(err)
		}
		groups, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill)
		if err != nil {
			t.Fatal(err)
		}
		got := aggregateGroupsText(groups, plan)
		if !compact {
			emptyResults = got
		} else if !reflect.DeepEqual(got, emptyResults) {
			t.Fatal("empty source differs", got, emptyResults)
		}
	}

}

// Slow/failed/cancelled latency samples retain the old source.
func TestCompactTupleLatencySamples(t *testing.T) {
	ctx := context.Background()
	if _, err := sampleCompactTupleRTT(ctx, func(context.Context) error { time.Sleep(4 * time.Millisecond); return nil }); err == nil {
		t.Fatal("slow link accepted")
	}
	sentinel := errors.New("probe failed")
	if _, err := sampleCompactTupleRTT(ctx, func(context.Context) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := sampleCompactTupleRTT(ctx, func(context.Context) error { t.Fatal("cancelled probe called"); return nil }); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestSQLCompactTupleProbeOwnership(t *testing.T) {
	h := newSQLHarness(t)
	client := h.rawClient()
	e := &executor{store: &store{client: client}}
	e.compactIntegerTupleRTT(h.ctx)
	if err := client.Ping(h.ctx).Err(); err != nil {
		t.Fatal("probe closed shared pool", err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if selected, _ := e.compactIntegerTupleRTT(ctx); selected {
		t.Fatal("cancelled probe selected HASH source")
	}
}
