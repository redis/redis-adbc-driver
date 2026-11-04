//go:build !scanprefetch

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Experimental builds only, pending the complete-fetch performance gate.
const scanPrefetchEnabled = false
