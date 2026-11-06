// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"reflect"
	"strings"
	"testing"
)

func TestManagedHashInspectionDescribesImportAndRefresh(t *testing.T) {
	f := newHashFixture(t, "it:managed-inspection:")
	const target = "it_managed_inspection"
	f.h.dropTables(target)
	t.Cleanup(func() { f.h.dropTables(target) })
	f.hset(f.prefix+"1", "v", "10")
	f.hset(f.prefix+"2", "v", "20")
	// Nested HASHes belong to neither the managed copy nor its type inference.
	f.hset(f.prefix+"2:nested", "v", "not an integer")
	before := f.raw.HGetAll(f.h.ctx, f.prefix+"1").Val()
	inspect := func(refresh bool) {
		t.Helper()
		r := f.inspect(HashInspectOptions{Table: target, Sample: -1, KeyColumn: "_key"})
		cl := checklist(t, r, ChecklistSQLInPlace)
		if !cl.Ready || !strings.Contains(cl.Title, "managed copy") || strings.Contains(cl.Title, "in place") {
			t.Fatalf("inspection describes the obsolete adoption protocol: %+v", cl)
		}
		expectItem(t, cl, CheckOK, "2 HASHes")
		expectItem(t, cl, CheckOK, "source HASHes and application indexes remain unchanged")
		expectItem(t, cl, CheckWarn, "not an atomic snapshot of application writers")
		for _, col := range r.Columns {
			if col.Name == "v" && (col.Type != "VARCHAR" || col.InPlaceType != "BIGINT") {
				t.Fatal("nested HASH changed the managed import type", col)
			}
			if col.Name == "_key" && col.InPlaceType != "" {
				t.Fatal("copy's synthetic key became an import column", col)
			}
		}
		if refresh {
			expectItem(t, cl, CheckOK, "explicitly refreshes")
		}
		if !reflect.DeepEqual(before, f.raw.HGetAll(f.h.ctx, f.prefix+"1").Val()) {
			t.Fatal("inspection changed source HASH")
		}
	}
	inspect(false)
	f.h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='"+target+"'", "0")
	f.adopt(HashAdoptOptions{Table: target, OwnIndex: true, Apply: true})
	f.h.expectRows("SELECT v FROM "+target+" ORDER BY v", "10", "20")
	inspect(true)
	f.h.expectRows("SELECT v FROM "+target+" ORDER BY v", "10", "20")
}
