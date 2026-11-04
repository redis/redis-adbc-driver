//go:build cgo && (amd64 || arm64) && !purego

// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package intpredicate

/*
#include <stdint.h>
#include <stddef.h>
void integer_match(const int64_t*, const uint64_t*, uint64_t*, size_t, int64_t, int);
*/
import "C"

import (
	"golang.org/x/sys/cpu"
	"runtime"
	"unsafe"
)

func Backend() string {
	if runtime.GOARCH == "amd64" && cpu.X86.HasSSE42 {
		return "sse4.2"
	}
	if runtime.GOARCH == "arm64" && cpu.ARM64.HasASIMD {
		return "neon"
	}
	return "scalar"
}

func native(v []int64, valid, out []uint64, rhs int64, op Op) bool {
	if Backend() == "scalar" {
		return false
	}
	var bitmap *C.uint64_t
	if len(valid) != 0 {
		bitmap = (*C.uint64_t)(unsafe.Pointer(&valid[0]))
	}
	C.integer_match((*C.int64_t)(unsafe.Pointer(&v[0])), bitmap,
		(*C.uint64_t)(unsafe.Pointer(&out[0])), C.size_t(len(v)), C.int64_t(rhs), C.int(op))
	return true
}
