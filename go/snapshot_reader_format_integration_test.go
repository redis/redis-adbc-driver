// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	_ "embed"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/snapshot-protocol15-row-collector.lua
var snapshotProtocol15RowCollector string

func TestSnapshotReaderFormatFencesPreviousStorage(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(42)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	data := snapshotDataID(meta.prefix())
	// Old collectors' literal graph/queue checks cannot admit a new-format
	// row batch, even if handed the current physical keys by a stale caller.
	err = once(h.ctx, raw, "EVAL", snapshotProtocol15RowCollector, 4, snapshotMetadataGraph, snapshotDataQueue, snapshotMembers(data), meta.prefix()+"1", data, meta.prefix(), "00000000000000000001").Err()
	if err == nil || !strings.Contains(err.Error(), "invalid snapshot reclamation batch") {
		t.Fatal("old collector admitted new storage", err)
	}
	if raw.HGet(h.ctx, meta.prefix()+"1", "v").Val() != "42" {
		t.Fatal("old collector modified new storage")
	}
	pin.release()
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "format", "16").Err(); err != nil {
		t.Fatal(err)
	}
	defer raw.HSet(h.ctx, snapshotCatalogKey, "format", "17")
	before, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	fresh := &store{client: raw}
	if err := fresh.openSnapshotCatalog(h.ctx, true); err == nil || !strings.Contains(err.Error(), "unsupported snapshot catalog format") {
		t.Fatal("old storage was not refused", err)
	}
	after, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("refusing old storage mutated its catalog", err)
	}
}
