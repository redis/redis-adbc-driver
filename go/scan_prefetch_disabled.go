//go:build scanprefetchoff

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Build-only escape hatch for baseline measurements and troubleshooting.
const scanPrefetchEnabled = false
