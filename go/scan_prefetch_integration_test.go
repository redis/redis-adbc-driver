// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"strings"
	"testing"
)

// Discovery is deliberately complete before mutation. HASHes remain
// authoritative at fetch time; this pipeline does not provide a snapshot.
func TestPrefetchUpdateDeleteAfterDiscovery(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_prefetch_mutation"
	h.dropTables(table)
	defs := []string{}
	for i := range 64 {
		defs = append(defs, fmt.Sprintf("c%d BIGINT", i))
	}
	h.exec("CREATE TABLE " + table + " (" + strings.Join(defs, ",") + ")")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " (c0) VALUES (1),(2),(NULL)")
	e, plan := sharedScanPlan(t, h, "SELECT * FROM "+table, nil)
	sc, err := e.prepareScan(h.ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	it, err := e.openScan(h.ctx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(h.ctx)
	if !it.hashFetch || !it.cur.pending {
		t.Fatal("expected discovered wide HASH page")
	}
	// Commands run after FT.AGGREGATE's first reply and before its HMGETs.
	key := it.meta.KeyPrefix
	if err := e.store.redis(h.ctx).HSet(h.ctx, key+"1", "c0", "9007199254740993").Err(); err != nil {
		t.Fatal(err)
	}
	if err := e.store.redis(h.ctx).Del(h.ctx, key+"2").Err(); err != nil {
		t.Fatal(err)
	}
	page := it.fetchHashPage(h.ctx)
	if page.err != nil {
		t.Fatal(page.err)
	}
	seen := map[string][]any{}
	for i, row := range page.values {
		if row != nil {
			seen[page.keys[i]] = row
		}
	}
	if len(seen) != 2 || seen[key+"2"] != nil {
		t.Fatalf("wrong surviving rows: %v", seen)
	}
	if got := seen[key+"1"][1]; got != "9007199254740993" {
		t.Fatalf("stale update: %v", got)
	}
	if seen[key+"3"] == nil || seen[key+"3"][1] != nil {
		t.Fatal("lost all-NULL row")
	}
}
