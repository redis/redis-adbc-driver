// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

// Approximate statistics on Flex count rows with the exact snapshot
// visibility queries (#627): Search counts of the NUMERIC row-ID and deletion
// marker, not a read of every row ID. The DuckDB ADBC scanner requests them
// for every table reference while binding. Shadowed versions, tombstones and
// appended rows must still give COUNT(*), before and after compaction.
func TestSnapshotFlexApproximateStatisticsSearchCounts(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	h.exec("CREATE TABLE fs(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO fs SELECT i,i FROM generate_series(1,300) AS g(i)")
	h.exec("UPDATE fs SET v=v+1 WHERE id<=40")
	h.exec("UPDATE fs SET v=v+1 WHERE id<=10")
	h.exec("DELETE FROM fs WHERE id BETWEEN 50 AND 69")
	h.exec("INSERT INTO fs VALUES (1001,1),(1002,2)")
	h.expectRows("SELECT COUNT(*) FROM fs", "282")
	s := snapshotTestStore(t, raw)
	s.flex = true
	// Each check is a subtest so that its hooked transport finishes before
	// the next one needs an idle connection.
	check := func(when string) {
		t.Helper()
		t.Run(when, func(t *testing.T) { checkFlexStatistics(t, h, s) })
	}
	check("with update and delete overlays")
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*) FROM fs", "282")
	check("after compaction")
}

func checkFlexStatistics(t *testing.T, h *sqlHarness, s *store) {
	// statisticsHookedContext inspects through a non-Flex store, which refuses
	// Flex plain-index tables; find the segment indexes here instead.
	pinned, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(pinned, defaultSchema, "fs")
	pin.release()
	if err != nil {
		t.Fatal(err)
	}
	hook := &statisticsReadHook{approximate: true, indexes: map[string]bool{}, prefixes: map[string]bool{}}
	for _, seg := range meta.snapshotSegments() {
		hook.indexes[seg.Index] = true
	}
	ctx, impl := statisticsHookedTransport(t, h, hook)
	filter := "fs"
	r, err := impl.GetStatistics(ctx, nil, nil, &filter, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := readStatisticCounts(t, r); got["fs"] != 282 {
		t.Fatalf("approximate row count %v, want 282", got)
	}
	if !hook.fired {
		t.Fatal("approximate statistics read every row ID instead of counting with Search")
	}
}
