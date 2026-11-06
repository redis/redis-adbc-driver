// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "testing"

func TestManagedHashImportIndependentIndexPolicy(t *testing.T) {
	for _, mode := range []string{"automatic_source", "explicit_source", "direct_hashes"} {
		for _, policy := range []string{"default", "selected", "none"} {
			t.Run(mode+"/"+policy, func(t *testing.T) {
				f := newHashFixture(t, "it:import-policy:")
				const table = "it_import_policy"
				f.h.dropTables(table)
				t.Cleanup(func() { f.h.dropTables(table) })
				f.hset(f.prefix+"1", "text flag", "YES", "n", "1", "city", "Rome")
				const sourceIndex = "it_import_policy_source"
				f.appIndex(sourceIndex, "SCHEMA", "n", "NUMERIC", "city", "TAG")
				o := HashAdoptOptions{Prefix: f.prefix, Table: table, Apply: true, Renames: map[string]string{"text flag": "flag"}}
				if policy == "selected" {
					o.IndexColumns = []string{"flag"}
				}
				if policy == "none" {
					o.IndexColumns = []string{}
				}
				wantIndexed := func(name string) bool { return policy == "default" || policy == "selected" && name == "flag" }
				if mode == "explicit_source" {
					o.Index = sourceIndex
				}
				if mode == "direct_hashes" {
					o.OwnIndex = true
				}
				result, err := AdoptHashes(f.h.ctx, f.opts, o)
				if err != nil || result == nil || !result.Applied {
					t.Fatalf("managed copy must own its index policy independently of capture: %v", err)
				}
				wantSource := sourceIndex
				if mode == "direct_hashes" {
					wantSource = ""
				}
				if result.SourceIndex != wantSource || result.AppIndex {
					t.Fatal("source capture and SQL index report were conflated")
				}
				c, err := dialHashes(f.h.ctx, f.opts)
				if err != nil {
					t.Fatal(err)
				}
				defer c.close(f.h.ctx)
				meta, err := c.st.getTable(c.context(f.h.ctx), "public", table)
				if err != nil {
					t.Fatal(err)
				}
				for _, col := range meta.Columns {
					if col.Indexed != wantIndexed(col.Name) || col.Encoding != "" || col.Field != "" {
						t.Fatalf("private native index policy not honored: %+v", col)
					}
				}
				reply, err := f.raw.Do(f.h.ctx, "FT.INFO", meta.index()).Result()
				if err != nil {
					t.Fatal(err)
				}
				attrs := map[string]string{}
				for _, attr := range indexInfoOf(reply).attrs {
					attrs[attr.name] = attr.typ
				}
				for _, name := range []string{"flag", "n", "city"} {
					_, present := attrs[name]
					if present != wantIndexed(name) {
						t.Fatal("private Search attributes differ from requested public columns", name, attrs)
					}
				}
				if wantIndexed("flag") && attrs["flag"] != "NUMERIC" {
					t.Fatal("converted text Boolean was not indexed natively")
				}
				f.h.expectRows("SELECT __key,flag,n,city FROM "+table+" WHERE flag", "it:import-policy:1|true|1|Rome")
				if f.raw.HGet(f.h.ctx, f.prefix+"1", "text flag").Val() != "YES" {
					t.Fatal("managed conversion changed source encoding")
				}
			})
		}
	}
}

func TestManagedForeignImportNativeIndexPolicy(t *testing.T) {
	f := newHashFixture(t, "it:foreign-policy:")
	f.hset(f.prefix+"1", "flag", "YES", "n", "1", "city", "Rome")
	const index = "it_foreign_policy_source"
	f.appIndex(index, "SCHEMA", "flag", "TAG", "n", "NUMERIC", "city", "TAG")
	if err := f.h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	indexes, _ := f.h.query("SELECT index_name FROM information_schema.tables WHERE table_schema='redis_foreign' AND table_name='" + index + "'")
	if len(indexes) != 1 || indexes[0] == index {
		t.Fatal("foreign copy has no private index")
	}
	reply, err := f.raw.Do(f.h.ctx, "FT.INFO", indexes[0]).Result()
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]string{}
	for _, attr := range indexInfoOf(reply).attrs {
		attrs[attr.name] = attr.typ
	}
	if attrs["flag"] != "NUMERIC" || attrs["n"] != "NUMERIC" || attrs["city"] != "TAG" {
		t.Fatal("foreign private index retained source storage restrictions", attrs)
	}
	f.h.expectRows("SELECT __key,flag,n,city FROM redis_foreign."+index+" WHERE flag", "it:foreign-policy:1|true|1|Rome")
	if f.raw.HGet(f.h.ctx, f.prefix+"1", "flag").Val() != "YES" {
		t.Fatal("foreign conversion changed application encoding")
	}
}
