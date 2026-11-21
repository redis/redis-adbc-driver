// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"strings"
	"testing"
)

func plainTagNULFixture(t *testing.T) string {
	t.Helper()
	nul := string([]byte{0, 't', 'a', 'i', 'l'})
	if strings.IndexByte(nul, 0) != 0 {
		t.Fatal("fixture must contain a real NUL byte")
	}
	return nul
}

func TestPlainTagEmptyLookup(t *testing.T) {
	nul := plainTagNULFixture(t)
	for _, policy := range []struct {
		name  string
		plain bool
		app   *appAttr
		empty bool
	}{
		{"managed RAM", false, nil, true},
		{"managed plain", true, nil, false},
		{"application without empty", false, &appAttr{sep: tagSeparator}, false},
		{"application with empty", false, &appAttr{sep: tagSeparator, indexEmpty: true}, true},
		{"plain application without empty", true, &appAttr{sep: tagSeparator}, false},
		{"plain application with empty", true, &appAttr{sep: tagSeparator, indexEmpty: true}, true},
	} {
		t.Run(policy.name, func(t *testing.T) {
			col := columnMeta{Name: "s", Type: typeString, Indexed: true, TagsChecked: true, PlainIndex: policy.plain, App: policy.app}
			// All of these require the empty native term, even when the SQL
			// literal itself is nonempty. A missing candidate cannot be repaired
			// by a residual predicate after index filtering.
			for _, value := range []string{"", " ", "\t\r\n", tagSeparator, " " + tagSeparator + " ", nul, " " + nul} {
				lit, exact, ok := tagLookup(col, value)
				if ok != policy.empty {
					t.Errorf("value %q: lookup=%q exact=%v ok=%v, want empty capability %v", value, lit, exact, ok, policy.empty)
				}
				if !ok && (lit != "" || exact) {
					t.Errorf("value %q: unavailable empty term retained a filter: %q/%v", value, lit, exact)
				}
			}
			for _, value := range []string{"hello", " hello ", tagSeparator + "hello", "hello" + nul} {
				lit, _, ok := tagLookup(col, value)
				if !ok || lit != "hello" {
					t.Errorf("nonempty term %q unnecessarily lost lookup: %q/%v", value, lit, ok)
				}
			}
		})
	}
}

func TestPlainTagUnionKeepsEmptyValuesForResidual(t *testing.T) {
	col := columnMeta{Name: "s", Type: typeString, Indexed: true, TagsChecked: true, PlainIndex: true}
	for _, empty := range []string{"", " ", tagSeparator, plainTagNULFixture(t)} {
		values := []Value{{T: typeString, S: "hello"}, {T: typeString, S: empty}}
		if query, ok := unionQuery(col, values, tzZone{}); ok {
			t.Errorf("union containing %q could lose rows through native filter %q", empty, query)
		}
	}
}

func TestPlainProvenanceTagSchema(t *testing.T) {
	for _, plain := range []bool{false, true} {
		meta := &tableMeta{PlainIndex: plain, SnapshotSourceKey: true,
			Columns: []columnMeta{{Name: "s", Type: typeString, Indexed: true, SearchAlias: "s_alias"}}}
		args := fmt.Sprint(indexCreateArgs(meta))
		for _, field := range []string{snapshotSourceKeyField, "s AS s_alias"} {
			if !strings.Contains(args, field+" TAG SEPARATOR "+tagSeparator+" CASESENSITIVE") {
				t.Errorf("plain=%v: missing provenance/public TAG options: %q", plain, args)
			}
		}
		for _, flag := range []string{"INDEXEMPTY", "SORTABLE", "UNF"} {
			if strings.Contains(args, flag) == plain {
				t.Errorf("plain=%v: unexpected %s policy: %q", plain, flag, args)
			}
		}
		if meta.Columns[0].PlainIndex || !meta.SnapshotSourceKey {
			t.Fatal("building physical arguments changed the source descriptor")
		}
	}
}
