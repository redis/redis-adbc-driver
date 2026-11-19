// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"reflect"
	"slices"
	"testing"
)

func TestUnmanagedClientRowFilterRefusesChangedPrefix(t *testing.T) {
	p := &rawCursorProcessor{t: t}
	it := &scanIter{meta: &tableMeta{Unmanaged: true}, unmanagedHashFetch: true, unmanagedRowPrefix: "app:", cur: &aggCursor{keyOnly: true, pending: true, firstKeys: []string{"other:row"}, cursor: 42, index: "idx", node: searchConn{p}}}
	_, _, err := it.nextPage(context.Background())
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusInvalidState {
		t.Fatal("unexpected prefix was not refused", err)
	}
	it.close(context.Background())
	if p.reads != 0 || p.dels != 1 {
		t.Fatal("unexpected native read or leaked cursor", p.reads, p.dels)
	}
}

func TestUnmanagedClientRowFilterMatchesSearch(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	for i, prefix := range []string{"filter:", "filter:λ:", "filter:" + string([]byte{0}) + ":"} {
		index := fmt.Sprintf("filter_membership_%d", i)
		if err := raw.Do(h.ctx, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "n", "NUMERIC").Err(); err != nil {
			t.Fatal(err)
		}
		// Include empty, Unicode, zero bytes and colons at every byte position.
		suffixes := []string{"", "a", "λ", ":", "a:", ":a", "a:b", "a" + string([]byte{0}) + ":b", string([]byte{0}) + ":", ":" + string([]byte{0})}
		for _, suffix := range suffixes {
			if err := raw.HSet(h.ctx, prefix+suffix, "n", 1).Err(); err != nil {
				t.Fatal(err)
			}
		}
		source := &store{client: raw}
		if err := source.awaitIndexReady(h.ctx, index); err != nil {
			t.Fatal(err)
		}
		all, err := source.aggregate(h.ctx, &aggRequest{index: index, query: "*", load: []string{keyField}, width: 1})
		if err != nil {
			t.Fatal(err)
		}
		filtered, err := source.aggregate(h.ctx, &aggRequest{index: index, query: "*", load: []string{keyField}, rowFilter: rowKeyFilter(prefix), width: 1})
		if err != nil {
			t.Fatal(err)
		}
		var got, want []string
		for _, row := range all {
			key := row[keyField]
			if unmanagedSearchRowKey(prefix, key) {
				got = append(got, key)
			}
		}
		for _, row := range filtered {
			want = append(want, row[keyField])
		}
		slices.Sort(got)
		slices.Sort(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("prefix %q: client=%q Search=%q", prefix, got, want)
		}
	}
}
