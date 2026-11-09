// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	"strings"
	"testing"
)

func TestSnapshotOperationErrorPreservesCStatusAndHandle(t *testing.T) {
	original := adbc.Error{Code: adbc.StatusInvalidState, Msg: "dependency changed", SqlState: [5]byte{'4', '0', '0', '0', '1'}}
	wrapped := operationError("0123456789abcdef0123456789abcdef", original)
	var mapped adbc.Error
	if !errors.As(wrapped, &mapped) || mapped.Code != original.Code || mapped.SqlState != original.SqlState || !strings.Contains(mapped.Msg, "0123456789abcdef0123456789abcdef") {
		t.Fatal("C adapter lost operation identity or error status", mapped)
	}
	if original.Msg != "dependency changed" {
		t.Fatal("wrapping mutated the original error")
	}
}
