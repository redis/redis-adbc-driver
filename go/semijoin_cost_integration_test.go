// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"strings"
	"testing"
)

// keepSemiProbes turns wholeBuildByCost off for a test of the page probes
// themselves, whose small inner sides it would build once.
func keepSemiProbes(t *testing.T) {
	t.Helper()
	testSkipSemiCostBuild = true
	t.Cleanup(func() { testSkipSemiCostBuild = false })
}

// #662: a decorrelated EXISTS over an in-memory (joined) outer side builds
// its inner side once when probing the outer side's distinct keys would cost
// more (wholeBuildByCost), and keeps the page probes below that threshold.
// Both return the same rows.
func TestSQLSemiJoinWholeBuildByCost(t *testing.T) {
	h := newSQLHarnessWithOptions(t, map[string]string{OptionStringQueryWorkerBudget: "1", OptionStringQueryWorkers: "1"})
	drop := func() {
		h.exec("DROP TABLE IF EXISTS it_sjc_big")
		h.exec("DROP TABLE IF EXISTS it_sjc_o")
	}
	drop()
	t.Cleanup(drop)

	// 1,500 orders of one to three lines from suppliers 1 … 4.
	const orders = 1500
	type line struct{ ok, sk int }
	var lines []line
	var vals []string
	for ok := 1; ok <= orders; ok++ {
		for j := 0; j < 1+ok%3; j++ {
			l := line{ok: ok, sk: 1 + (ok+j*(1+ok%2))%4}
			lines = append(lines, l)
			vals = append(vals, fmt.Sprintf("(%d, %d)", l.ok, l.sk))
		}
	}
	if len(lines) <= semiJoinEagerRows {
		t.Fatalf("%d lines do not exceed the eager build size", len(lines))
	}
	// Distinct outer keys above which one serial worker builds the inner side.
	threshold := len(lines) * int(semiWholeBuildRowCost) / int(semiProbeKeyCost)
	small, large := threshold/4, min(orders, 4*threshold)
	if small < 1 || large <= threshold {
		t.Fatalf("fixture cannot straddle the threshold: %d lines, %d keys (costs %v / %v)", len(lines), threshold, semiWholeBuildRowCost, semiProbeKeyCost)
	}
	h.exec("CREATE TABLE it_sjc_big (ok BIGINT, sk BIGINT)")
	h.exec("INSERT INTO it_sjc_big VALUES " + strings.Join(vals, ", "))
	h.exec("CREATE TABLE it_sjc_o (ok BIGINT)")
	vals = vals[:0]
	for ok := 1; ok <= orders; ok++ {
		vals = append(vals, fmt.Sprintf("(%d)", ok))
	}
	h.exec("INSERT INTO it_sjc_o VALUES " + strings.Join(vals, ", ") + ", (NULL)")

	for _, c := range []struct {
		what  string
		keys  int
		build bool
	}{{"below the threshold", small, false}, {"above the threshold", large, true}} {
		// Lines of the first keys orders that have a line from another supplier.
		want := 0
		for _, l1 := range lines {
			if l1.ok > c.keys {
				continue
			}
			for _, l := range lines {
				if l.ok == l1.ok && l.sk != l1.sk {
					want++
					break
				}
			}
		}
		sql := fmt.Sprintf(`SELECT COUNT(*) FROM it_sjc_o o JOIN it_sjc_big l1 ON l1.ok = o.ok
			WHERE o.ok <= %d AND EXISTS (SELECT 1 FROM it_sjc_big l2 WHERE l2.ok = l1.ok AND l2.sk <> l1.sk)`, c.keys)
		before := subqueryStats.costBuilds.Load()
		w := h.expectWork(sql, fmt.Sprint(want))
		built := subqueryStats.costBuilds.Load() - before
		switch {
		case w.runs != 0:
			t.Errorf("%s (%d keys): %+v, want no per-row runs", c.what, c.keys, w)
		case c.build && (built != 1 || w.semiJoins != 1):
			t.Errorf("%s (%d keys, threshold %d): %d cost builds, %+v; want one whole build", c.what, c.keys, threshold, built, w)
		case !c.build && (built != 0 || w.semiJoins == 0):
			t.Errorf("%s (%d keys, threshold %d): %d cost builds, %+v; want page probes", c.what, c.keys, threshold, built, w)
		}
	}
}
