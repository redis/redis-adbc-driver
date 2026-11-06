// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"slices"
	"testing"
)

func TestSnapshotSortCapacityPrivateOutputHasNoIndex(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
	work, _, _ := snapshotStageSQL(t, s, h.ctx, "", "CREATE TABLE private_rows(id INTEGER NOT NULL)")
	snapshotExecuteTest(t, s, work, "INSERT INTO private_rows VALUES(3),(1),(2)")
	m := snapshotTableTest(t, s, work, "private_rows")
	if _, pending := snapshotDeferredIndex(work, m); !pending {
		t.Fatal("private output index was not deferred")
	}
	guard := &snapshotSortCapHook{native: true}
	s.client.AddHook(guard)
	res := snapshotExecuteTest(t, s, work, "SELECT id FROM private_rows ORDER BY id DESC")
	if got := snapshotResultStrings(res.rows); !slices.Equal(got, []string{"3", "2", "1"}) {
		t.Fatal(got)
	}
	if guard.probes != 0 || guard.data != 0 {
		t.Fatalf("unindexed private output opened Search queries: probes=%d data=%d", guard.probes, guard.data)
	}
}
