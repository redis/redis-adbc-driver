// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

// An adopted known-key lookup has already passed the ordinary planner's prefix
// and nested-key rules. Reuse the exact live projection reader instead of
// transferring every application HASH field. Foreign/filter membership and
// Flex lookups retain their existing Search/SCAN paths; this helper never
// establishes membership or replaces catalog/index/lease validation.
//
// The reader includes every selected/residual/hidden field in need. Its existing
// atomic native HGETALL fallback distinguishes an all-NULL projection from a
// missing HASH without combining observations, and preserves native ACL and
// unknown-reply behavior. Reader state belongs only to this scan.
func newUnmanagedPointRead(s *store, meta *tableMeta, need map[string]bool) *unmanagedProjectionRead {
	if s == nil || meta == nil || !meta.Unmanaged || !meta.KeyRows || meta.Foreign || meta.UnmanagedForeign || s.flexEnabled() {
		return nil
	}
	return newUnmanagedProjectionRead(s, meta, need)
}
