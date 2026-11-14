// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "strings"

// Limit shared ownership: keeping one converted string may retain its column's
// arena. Larger columns keep the independent per-cell clone path.
const ingestStringArenaBytes = 1 << 20

type ingestStrings interface {
	IsNull(int) bool
	Value(int) string
}

// ingestStringArena is called only after full array validation and only for
// exact unbounded text. That conversion cannot fail or change the string bytes.
// Each pass is restricted to the actual conversion rows, not the parent batch.
// The final string owns copied Go bytes; no Arrow storage escapes the call.
func ingestStringArena(a ingestStrings, rows [][]Value, column int) bool {
	total := 0
	for i := range rows {
		if !a.IsNull(i) {
			n := len(a.Value(i))
			if n > ingestStringArenaBytes-total {
				return false
			}
			total += n
		}
	}
	var builder strings.Builder
	builder.Grow(total)
	for i := range rows {
		if !a.IsNull(i) {
			builder.WriteString(a.Value(i))
		}
	}
	owned := builder.String()
	at := 0
	for i := range rows {
		if a.IsNull(i) {
			rows[i][column] = nullValue(typeString)
			continue
		}
		n := len(a.Value(i))
		rows[i][column] = stringValue(owned[at : at+n])
		at += n
	}
	return true
}
