// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"reflect"
	"strings"
	"testing"
)

func TestManagedHashImportReportAndRefresh(t *testing.T) {
	f := newHashFixture(t, "it:managed-report:")
	const table = "it_managed_report"
	f.h.dropTables(table)
	t.Cleanup(func() { f.h.dropTables(table) })
	f.hset(f.prefix+"1", "v", "10")
	f.hset(f.prefix+"2", "v", "20")
	f.hset(f.prefix+"2:nested", "v", "999")
	before := f.raw.HGetAll(f.h.ctx, f.prefix+"1").Val()
	root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
	opts := HashAdoptOptions{Table: table, OwnIndex: true}
	dry := f.adopt(opts)
	if dry.Applied || !dry.ManagedCopy || len(dry.Report.Checks) != 1 || !dry.Report.Checks[0].Ready ||
		!strings.Contains(dry.Report.Checks[0].Title, "managed copy") ||
		!strings.Contains(dry.Report.Checks[0].Command, "-own-index") || len(dry.Steps) == 0 {
		t.Fatalf("incomplete dry-run report: %+v", dry)
	}
	expectItem(t, dry.Report.Checks[0], CheckOK, "2 HASHes under it:managed-report: are rows")
	expectItem(t, dry.Report.Checks[0], CheckWarn, "stays out of the table")
	if f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("dry run published metadata")
	}
	f.h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='"+table+"'", "0")
	opts.Apply = true
	applied := f.adopt(opts)
	if !applied.Applied || !applied.ManagedCopy || applied.AppIndex || len(applied.Report.Checks) != 1 || !applied.Report.Checks[0].Ready {
		t.Fatalf("incomplete apply report: %+v", applied)
	}
	f.h.expectRows("SELECT v FROM "+table+" ORDER BY v", "10", "20")
	if !reflect.DeepEqual(before, f.raw.HGetAll(f.h.ctx, f.prefix+"1").Val()) {
		t.Fatal("import changed source HASH")
	}
	f.hset(f.prefix+"1", "v", "99")
	f.h.expectRows("SELECT v FROM "+table+" ORDER BY v", "10", "20")
	opts.Apply = false
	root = f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
	refresh := f.adopt(opts)
	if len(refresh.Report.Checks) != 1 || !refresh.Report.Checks[0].Ready || refresh.AlreadyAdopted {
		t.Fatal("refresh was reported as an in-place no-op")
	}
	expectItem(t, refresh.Report.Checks[0], CheckOK, "explicitly refreshes")
	if f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("refresh dry run published")
	}
	f.h.expectRows("SELECT v FROM "+table+" ORDER BY v", "10", "20")
	opts.Apply = true
	if !f.adopt(opts).Applied {
		t.Fatal("explicit refresh was not applied")
	}
	f.h.expectRows("SELECT v FROM "+table+" ORDER BY v", "20", "99")
	if f.raw.HGet(f.h.ctx, f.prefix+"1", "v").Val() != "99" {
		t.Fatal("refresh changed source HASH")
	}
}

func TestManagedHashImportReportBlockers(t *testing.T) {
	f := newHashFixture(t, "it:managed-blockers:")
	f.hset(f.prefix+"1", "v", "10")
	const table = "it_managed_collision"
	f.h.dropTables(table)
	f.h.exec("CREATE TABLE " + table + "(v INTEGER)")
	f.h.exec("INSERT INTO " + table + " VALUES(999)")
	t.Cleanup(func() { f.h.dropTables(table) })
	root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
	for _, apply := range []bool{false, true} {
		result, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: table, OwnIndex: true, Apply: apply})
		if (err != nil) != apply || result == nil {
			t.Fatalf("blocked Apply must return its report and an error; dry run returns the report: %v", err)
		}
		if result.Applied || len(result.Report.Checks) != 1 || result.Report.Checks[0].Ready || result.Report.Checks[0].Command != "" {
			t.Fatalf("collision reported ready: %+v", result)
		}
		expectItem(t, result.Report.Checks[0], CheckBlocker, "not a managed copy of this source")
	}
	f.h.expectRows("SELECT v FROM "+table, "999")
	if f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("blocked import published")
	}
	f.hset(f.prefix+"1", "v", "not a boolean")
	bad, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_managed_invalid", OwnIndex: true, Apply: true, Types: map[string]string{"v": "BOOLEAN"}})
	if err == nil || bad == nil || bad.Applied || len(bad.Report.Checks) != 1 || bad.Report.Checks[0].Ready {
		t.Fatalf("invalid source type reported ready: %+v", bad)
	}
	expectItem(t, bad.Report.Checks[0], CheckBlocker, "do not fit the source encoding")
	if f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("invalid source type published")
	}
	f.clear()
	empty, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_managed_empty", OwnIndex: true, Apply: true})
	if err == nil || empty == nil || empty.Applied || len(empty.Report.Checks) != 1 || empty.Report.Checks[0].Ready {
		t.Fatalf("empty source reported ready: %+v", empty)
	}
	expectItem(t, empty.Report.Checks[0], CheckBlocker, "no HASH keys")
}
