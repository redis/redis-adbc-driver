//go:build cgo

// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package cuda

/*
#cgo linux LDFLAGS: -ldl -lpthread
#include "runtime.h"
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type Filter struct{ handle *C.redis_cuda_filter }

func New(device int) (*Filter, error) {
	var message [256]C.char
	handle := C.redis_cuda_filter_new(C.int(device), &message[0], 256)
	if handle == nil {
		return nil, fmt.Errorf("%s", C.GoString(&message[0]))
	}
	return &Filter{handle}, nil
}
func (f *Filter) Close() {
	if f.handle != nil {
		C.redis_cuda_filter_free(f.handle)
		f.handle = nil
	}
}
func (f *Filter) Run(values []int64, valid []byte, rhs int64, op int) ([]byte, error) {
	if len(values) != len(valid) || len(values) > 1048576 {
		return nil, fmt.Errorf("invalid CUDA predicate batch")
	}
	out := make([]byte, len(values))
	if len(out) == 0 {
		return out, nil
	}
	var message [256]C.char
	rc := C.redis_cuda_filter_run(f.handle, (*C.int64_t)(unsafe.Pointer(&values[0])), (*C.uint8_t)(unsafe.Pointer(&valid[0])), (*C.uint8_t)(unsafe.Pointer(&out[0])), C.int64_t(len(out)), C.int64_t(rhs), C.int(op), &message[0], 256)
	if rc != 0 {
		return nil, fmt.Errorf("%s", C.GoString(&message[0]))
	}
	return out, nil
}
