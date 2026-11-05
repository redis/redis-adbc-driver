// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"testing"
)

func TestStreamDistinctBoundsAndEquality(t *testing.T) {
	d := &streamDistinct{seen: map[string]bool{}}
	for _, row := range [][]Value{{nullValue(typeString)}, {stringValue("")}, {stringValue("a\x00é")}, {intValue(typeInt64, 9007199254740993)}} {
		if keep, err := d.keep(row); err != nil || !keep {
			t.Fatal(keep, err)
		}
		if keep, err := d.keep(row); err != nil || keep {
			t.Fatal(keep, err)
		}
	}
	for i := int64(0); len(d.seen) < maxStreamDistinctEntries; i++ {
		if _, err := d.keep([]Value{intValue(typeInt64, i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.keep([]Value{stringValue("new")}); err == nil {
		t.Fatal("cardinality cap not enforced")
	}
	d = &streamDistinct{seen: map[string]bool{}, rows: maxStreamDistinctRows}
	if _, err := d.keep(nil); err == nil {
		t.Fatal("work cap not enforced")
	}
	d = &streamDistinct{seen: map[string]bool{}}
	if _, err := d.keep([]Value{stringValue(strings.Repeat("x", maxStreamDistinctBytes))}); err == nil || len(d.seen) != 0 {
		t.Fatal("oversize key admitted", err)
	}
}
