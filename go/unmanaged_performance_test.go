// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"testing"
)

func TestUnmanagedCandidateAttributeSafety(t *testing.T) {
	for _, tc := range []struct {
		name    string
		column  columnMeta
		attr    indexAttr
		indexed bool
	}{
		{"integer", columnMeta{Name: "v", Type: typeInt64}, indexAttr{identifier: "v", name: "v", typ: "NUMERIC", options: []string{"SORTABLE"}}, true},
		{"renamed field", columnMeta{Name: "v", Field: "stored", Type: typeInt64}, indexAttr{identifier: "stored", name: "stored", typ: "NUMERIC"}, true},
		{"text encoding", columnMeta{Name: "v", Type: typeInt64, Encoding: "text"}, indexAttr{identifier: "v", name: "v", typ: "NUMERIC"}, false},
		{"float", columnMeta{Name: "v", Type: typeFloat64}, indexAttr{identifier: "v", name: "v", typ: "NUMERIC"}, false},
		{"tag", columnMeta{Name: "v", Type: typeString}, indexAttr{identifier: "v", name: "v", typ: "TAG"}, false},
		{"aliased attribute", columnMeta{Name: "v", Type: typeInt64}, indexAttr{identifier: "v", name: "other", typ: "NUMERIC"}, false},
		{"noindex", columnMeta{Name: "v", Type: typeInt64}, indexAttr{identifier: "v", name: "v", typ: "NUMERIC", options: []string{"NOINDEX"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := &tableMeta{Unmanaged: true, Columns: []columnMeta{tc.column}}
			applyUnmanagedAttrs(meta, []indexAttr{tc.attr})
			c := meta.Columns[0]
			if c.Indexed != tc.indexed {
				t.Fatalf("candidate indexing=%v", c.Indexed)
			}
			if c.App != nil && c.App.sortable || c.pushable(PushdownExact) {
				t.Fatal("mutable source permits index-value sorting/reduction")
			}
			applyUnmanagedAttrs(meta, nil)
			if meta.Columns[0].Indexed || meta.Columns[0].App != nil {
				t.Fatal("retained stale source attribute")
			}
		})
	}
}

func TestUnmanagedVanishedCursorRowIsSkipped(t *testing.T) {
	it := &scanIter{meta: &tableMeta{Unmanaged: true}, cur: &aggCursor{}, env: &evalEnv{ctx: context.Background()}}
	keys, rows, err := it.keep([]string{"app:gone"}, []aggRow{nil})
	if err != nil || len(keys) != 0 || len(rows) != 0 {
		t.Fatalf("vanished live row became snapshot corruption: %v %v %v", keys, rows, err)
	}
}
