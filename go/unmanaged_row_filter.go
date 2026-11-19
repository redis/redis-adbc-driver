// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "strings"

// Preserve the existing Search rowKeyFilter expression exactly, including its
// historical string-function edges. substr(key, prefixBytes, -1) omits the final
// byte; contains uses C-string matching and stops at a zero byte. Do not replace
// this with the stronger canonical isRowKey rule or change existing membership.
// The caller verifies the registered single prefix before this predicate.
func unmanagedSearchRowKey(prefix, key string) bool {
	rest := key[min(len(prefix), len(key)):]
	if len(rest) > 0 {
		rest = rest[:len(rest)-1]
	}
	if at := strings.IndexByte(rest, 0); at >= 0 {
		rest = rest[:at]
	}
	return !strings.Contains(rest, ":")
}
