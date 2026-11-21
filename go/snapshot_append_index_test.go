// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestSnapshotAppendIndexEligibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta tableMeta
		mode string
		want bool
	}{
		{"noindex", tableMeta{Columns: []columnMeta{{Name: "f", Type: typeFloat64}, {Name: "s", Type: typeString}}}, adbc.OptionValueIngestModeAppend, true},
		{"flex", tableMeta{PlainIndex: true}, adbc.OptionValueIngestModeCreateAppend, true},
		{"indexed-float", tableMeta{Columns: []columnMeta{{Indexed: true, Type: typeFloat64}}}, adbc.OptionValueIngestModeAppend, false},
		{"indexed-tag", tableMeta{Columns: []columnMeta{{Indexed: true, Type: typeString}}}, adbc.OptionValueIngestModeAppend, false},
		{"provenance-tag", tableMeta{SnapshotSourceKey: true}, adbc.OptionValueIngestModeAppend, false},
		{"unmanaged", tableMeta{Unmanaged: true}, adbc.OptionValueIngestModeAppend, false},
		{"live", tableMeta{Live: true}, adbc.OptionValueIngestModeAppend, false},
		{"replace", tableMeta{}, adbc.OptionValueIngestModeReplace, false},
		{"create", tableMeta{}, adbc.OptionValueIngestModeCreate, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotAppendIndexEligible(&tc.meta, tc.mode); got != tc.want {
				t.Fatalf("eligible=%v want %v", got, tc.want)
			}
		})
	}
	meta := &tableMeta{PlainIndex: true, KeyPrefix: snapshotRows(strings.Repeat("a", 32))}
	args := fmt.Sprint(indexCreateArgs(meta))
	if !strings.Contains(args, rowIDField+" NUMERIC") || !strings.Contains(args, snapshotDeletedField+" NUMERIC") || strings.Contains(args, "SORTABLE") {
		t.Fatal("Flex hidden numeric index must retain its membership proof without SORTABLE:", args)
	}
}
