// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestUnmanagedReplaceScopeBytes(t *testing.T) {
	prefix := "app[*?:"
	for _, key := range []string{prefix + "one", prefix + string([]byte{'a', 0, 'b'})} {
		if !unmanagedReplaceScopeKey(prefix, key) {
			t.Fatal("rejected literal prefix/binary key")
		}
	}
	for _, key := range []string{prefix, prefix + "nested:one", "appz:one", "adbc:{meta}:one"} {
		if unmanagedReplaceScopeKey(prefix, key) {
			t.Fatal("unsafe key admitted")
		}
	}
}

func TestUnmanagedReplaceErrorPreservesStatus(t *testing.T) {
	initial := stateErrorf("40001", "registration changed")
	p := &unmanagedReplaceProgress{deleted: 7, phase: "registration", outcome: "unknown"}
	err := p.failure(3, initial)
	var a, b adbc.Error
	if !errors.As(initial, &a) || !errors.As(err, &b) || a.Code != b.Code || a.SqlState != b.SqlState || len(a.Details) != 0 {
		t.Fatal("status or original error changed")
	}
	assertReplaceDetail(t, err, "acknowledged_deletes", "7")
	assertReplaceDetail(t, err, "acknowledged_rows", "3")
	assertReplaceDetail(t, err, "phase", "registration")
	assertReplaceDetail(t, err, "current_outcome", "unknown")
}
