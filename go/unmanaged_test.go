// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "testing"

func TestUnmanagedModeValidation(t *testing.T) {
	for _, mode := range []string{"", "managed", "unmanaged"} {
		if _, err := parseTableMode(mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"MANAGED", "true", "live"} {
		if _, err := parseTableMode(mode); err == nil {
			t.Fatalf("accepted invalid mode %q", mode)
		}
	}
	for _, key := range []string{metaPrefix + "table:public:t", snapshotRows(snapshotID()) + "1", "adbc:meta:legacy"} {
		if unmanagedSourceKey(key) {
			t.Fatalf("accepted driver-owned source key %q", key)
		}
	}
}
