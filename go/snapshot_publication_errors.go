// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

// Only exact direct native Search refusals allow polling private publication
// readiness. A wrapped/joined error can also contain reply loss and is terminal.
func snapshotPublicationTransient(err error) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return false
	}
	return native.Error() == "Inconsistent index state" || native.Error() == "SEARCH_VALUE_BAD Inconsistent index state"
}
