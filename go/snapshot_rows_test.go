// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"strings"
	"testing"
)

func TestSnapshotRowSchemaNamespace(t *testing.T) {
	id := strings.Repeat("a", 32)
	seen := map[string]bool{}
	for _, schema := range []string{"public", "private", "adbc", "%61dbc", "public%3A", "{meta}", "with:colon", "pg_temp_123", "tenant:{evil}*雪"} {
		prefix := snapshotRows(id, schema)
		if seen[prefix] {
			t.Fatalf("different schema names share a physical prefix: %q", schema)
		}
		seen[prefix] = true
		if strings.HasPrefix(prefix, "adbc:") || snapshotRowPrefix(prefix+"9007199254740993") != prefix || snapshotDataID(prefix+"1") != id {
			t.Fatalf("invalid schema-qualified identity for %q: %q", schema, prefix)
		}
		first := strings.IndexByte(prefix, '{')
		if first < 0 || !strings.HasPrefix(prefix[first:], "{meta}:") || strings.Count(prefix, "{") != 1 {
			t.Fatalf("schema changed the cluster hash tag: %q", prefix)
		}
		if snapshotDirectoryMember(prefix+"9007199254740993") != "00009007199254740993" {
			t.Fatal("schema changed exact directory membership")
		}
	}
	for _, key := range []string{
		snapshotPrefix + "rows:" + id + ":1",
		"%70ublic" + snapshotRowMarker + id + ":1",
		"adbc" + snapshotRowMarker + id + ":1",
		"wrong:{evil}" + snapshotRowMarker + id + ":1",
		"%GG" + snapshotRowMarker + id + ":1",
		"public" + snapshotRowMarker + strings.Repeat("z", 32) + ":1",
	} {
		if snapshotRowPrefix(key) != "" || snapshotDataID(key) != "" {
			t.Fatalf("invalid/legacy row identity accepted: %q", key)
		}
	}
}
