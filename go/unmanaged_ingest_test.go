// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	"testing"
)

func TestUnmanagedIngestErrorPreservesStatus(t *testing.T) {
	original := stateErrorf(stateUnique, "duplicate input key")
	err := unmanagedIngestError(17, original)
	var before, after adbc.Error
	if !errors.As(original, &before) || !errors.As(err, &after) || before.Code != after.Code || before.SqlState != after.SqlState {
		t.Fatalf("lost original status: %v", err)
	}
	assertUnmanagedAcknowledged(t, err, 17)
	if len(before.Details) != 0 {
		t.Fatal("mutated original error details")
	}
}
