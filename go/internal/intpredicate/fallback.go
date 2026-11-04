//go:build !cgo || (!amd64 && !arm64) || purego

// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package intpredicate

func Backend() string                                    { return "scalar" }
func native([]int64, []uint64, []uint64, int64, Op) bool { return false }
