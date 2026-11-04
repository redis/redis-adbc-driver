// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPartitionLayoutDescriptorRevisions(t *testing.T) {
	live := partitionID{Version: 3, Query: "SELECT v FROM t", Schema: "public", Table: "t", Target: strings.Repeat("a", 64), Zone: "UTC", Lo: 1, Hi: 2, Layouts: resultLayouts{"v": "string_view"}}
	retained := retainedPartitionID{Version: 4, Reference: retainedReference{Namespace: "tenant", JobID: strings.Repeat("a", 32), Manifest: strings.Repeat("b", 64), Expires: 12345678}, Start: 0, End: 1, Layouts: resultLayouts{"v": "string_view"}}
	for _, layout := range []string{"string_view", "dictionary", "unknown", "fixed_size_binary:0"} {
		live.Layouts["v"] = layout
		retained.Layouts["v"] = layout
		raw, _ := json.Marshal(live)
		_, e1 := decodePartition(raw)
		raw, _ = json.Marshal(retained)
		_, e2 := decodeRetainedPartition(raw)
		if (e1 == nil) != (layout == "string_view") || (e2 == nil) != (layout == "string_view") {
			t.Fatal(layout, e1, e2)
		}
	}
	live.Version = 1
	live.Layouts = resultLayouts{"v": "string_view"}
	raw, _ := json.Marshal(live)
	if _, err := decodePartition(raw); err == nil {
		t.Fatal("old revision accepted layout")
	}
	retained.Version = 2
	retained.Layouts = resultLayouts{"v": "string_view"}
	raw, _ = json.Marshal(retained)
	if _, err := decodeRetainedPartition(raw); err == nil {
		t.Fatal("old retained revision accepted layout")
	}
}
