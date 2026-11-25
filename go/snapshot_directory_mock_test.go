// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"strings"
)

// mockDirectorySeek recognizes a seek's first directory page,
// ZRANGE key [from + BYLEX LIMIT 0 count, and returns its zero-padded bound.
func mockDirectorySeek(args []any) (from string, count int64, ok bool) {
	if len(args) != 8 || !strings.EqualFold(fmt.Sprint(args[4]), "bylex") || !strings.EqualFold(fmt.Sprint(args[5]), "limit") {
		return "", 0, false
	}
	start, stop := fmt.Sprint(args[2]), fmt.Sprint(args[3])
	offset, _ := args[6].(int64)
	count, _ = args[7].(int64)
	if !strings.HasPrefix(start, "[") || stop != "+" || offset != 0 || count < 1 {
		return "", 0, false
	}
	return strings.TrimPrefix(start, "["), count, true
}
