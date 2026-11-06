// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"os"
	"testing"
)

// Read-only plan/selection proof on the actual restored benchmark table. This
// is outside timings and does not replace the independent full-value screen.
func TestPrefetchScreenSelection(t *testing.T) {
	query := os.Getenv("ADBC_PREFETCH_SCREEN_QUERY")
	if query == "" {
		t.Skip("set ADBC_PREFETCH_SCREEN_QUERY for read-only restored-fixture selection proof")
	}
	h := newSQLHarness(t)
	r, _ := h.openStream(h.ctx, h.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "65536"})
	defer r.Release()
	regular, ok := inspectionReader(r).(*streamReader)
	if !ok || regular.it == nil || !regular.it.hashFetch || len(regular.cols) < 64 || regular.it.projection == nil {
		t.Fatal("restored wide fixture did not select managed positional HASH streaming")
	}
	started := regular.it.prefetch != nil
	if regular.it.allowPrefetch != scanPrefetchEnabled || started != scanPrefetchEnabled {
		t.Fatal("restored wide fixture did not select the build's expected prefetch path")
	}
	_, parts := planPartitions(t, h, query, 4)
	if len(parts.PartitionIDs) == 0 {
		t.Fatal("no public partitions")
	}
	partition, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer partition.Release()
	p, ok := inspectionReader(partition).(*streamReader)
	if !ok || p.it == nil || !p.it.hashFetch || p.it.projection == nil || p.it.allowPrefetch != scanPrefetchEnabled {
		t.Fatal("restored public partition did not select eligible positional HASH streaming")
	}
	if !partition.Next() {
		t.Fatal(partition.Err())
	}
	proof := map[string]any{"prefetch_enabled": scanPrefetchEnabled, "regular_hash_fetch": true,
		"regular_prefetch_started": started, "partition_hash_fetch": true,
		"partition_prefetch_eligible": p.it.allowPrefetch, "projected_columns": len(regular.cols)}
	if path := os.Getenv("ADBC_PREFETCH_SCREEN_PROOF"); path != "" {
		data, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
