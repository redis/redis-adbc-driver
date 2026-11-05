//go:build !scanprefetchoff

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Eligible multi-page wide HASH streams overlap one owned lookahead page.
const scanPrefetchEnabled = true
