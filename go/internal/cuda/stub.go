//go:build !cgo

// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package cuda

import "fmt"

type Filter struct{}

func New(device int) (*Filter, error) {
	return nil, fmt.Errorf("CUDA execution requires a Linux cgo build")
}
func (f *Filter) Close() {}
func (f *Filter) Run([]int64, []byte, int64, int) ([]byte, error) {
	return nil, fmt.Errorf("CUDA execution requires a Linux cgo build")
}
