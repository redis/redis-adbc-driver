// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"slices"
	"testing"
)

func TestSnapshotOrderedVisibilityAndGlobalLimit(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT,g VARCHAR)")
	h.exec("INSERT INTO t VALUES (1,9007199254740993,'a'),(2,9007199254740992,'a'),(3,10,'b'),(4,NULL,'b'),(5,11,'c')")
	h.exec("UPDATE t SET v=12,g='b' WHERE id=3")
	h.exec("DELETE FROM t WHERE id=5")
	h.exec("INSERT INTO t VALUES (6,9,'a'),(7,NULL,'c'),(8,9007199254740994,'c')")
	s := snapshotTestStore(t, raw)
	cases := map[string][]string{
		"SELECT id,v FROM t ORDER BY v ASC NULLS LAST,id":                          {"6|9", "3|12", "2|9007199254740992", "1|9007199254740993", "8|9007199254740994", "4|NULL", "7|NULL"},
		"SELECT id FROM t ORDER BY v DESC NULLS LAST,id LIMIT 3 OFFSET 1":          {"1", "2", "3"},
		"SELECT id FROM t WHERE g='b' ORDER BY v LIMIT 1":                          {"3"},
		"SELECT id FROM t WHERE id%2=0 ORDER BY v ASC NULLS LAST LIMIT 2 OFFSET 1": {"2", "8"},
		"SELECT id FROM t ORDER BY g,id LIMIT 4 OFFSET 2":                          {"6", "3", "4", "7"},
		"SELECT id FROM t LIMIT 3 OFFSET 2":                                        {"3", "4", "6"},
	}
	for query, want := range cases {
		result := snapshotExecuteTest(t, s, h.ctx, query)
		if got := snapshotResultStrings(result.rows); !slices.Equal(got, want) {
			t.Fatalf("%s: got %v want %v", query, got, want)
		}
	}
	h.expectRows("SELECT id FROM t ORDER BY v DESC NULLS FIRST,id LIMIT 3", "4", "7", "8")
}
