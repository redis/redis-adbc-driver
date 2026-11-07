// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"runtime"
	"testing"
	"weak"
)

//go:noinline
func hashCopyResultReference() (weak.Pointer[hashScan], *HashCopy) {
	scan := &hashScan{seen: map[string]struct{}{"application:key": {}}}
	scan.stats = HashCopy{Table: "public.copy", Rows: 1, UnknownFields: map[string]int64{"extra": 1}, Nulls: map[string]int64{"nullable": 1}}
	return weak.Make(scan), scan.copyResult()
}

func TestHashCopyResultDoesNotRetainInput(t *testing.T) {
	input, result := hashCopyResultReference()
	runtime.GC()
	if input.Value() != nil {
		t.Fatal("retaining copy statistics also retains the input scan and seen-key map")
	}
	if result.Table != "public.copy" || result.Rows != 1 || result.UnknownFields["extra"] != 1 || result.Nulls["nullable"] != 1 {
		t.Fatal("independent statistics lost copy results")
	}
	runtime.KeepAlive(result)
}
