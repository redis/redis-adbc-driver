// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"
)

// appIndex makes the application's index on the fixture's prefix, and waits
// until it has indexed the existing HASHes.
func (f *hashFixture) appIndex(name string, def ...any) {
	f.h.t.Helper()
	st := &store{client: f.raw}
	_ = st.searchDo(f.h.ctx, name, "FT.DROPINDEX", name).Err()
	f.h.t.Cleanup(func() { _ = st.searchDo(f.h.ctx, name, "FT.DROPINDEX", name).Err() })
	args := append([]any{"FT.CREATE", name, "ON", "HASH", "PREFIX", 1, f.prefix}, def...)
	if err := st.searchDo(f.h.ctx, name, args...).Err(); err != nil {
		f.h.t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		reply, err := st.searchDo(f.h.ctx, name, "FT.INFO", name).Result()
		if err != nil {
			f.h.t.Fatal(err)
		}
		if info := indexInfoOf(reply); !info.indexing && info.percent >= 1 {
			return
		}
		if time.Now().After(deadline) {
			f.h.t.Fatalf("%s is still indexing", name)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// indexUses returns how often an index was queried (FT.INFO number_of_uses).
func (f *hashFixture) indexUses(name string) int64 {
	f.h.t.Helper()
	reply, err := (&store{client: f.raw}).searchDo(f.h.ctx, name, "FT.INFO", name).Result()
	if err != nil {
		f.h.t.Fatal(err)
	}
	fields, ok := redisReplyFields(reply)
	if !ok || fields["number_of_uses"] == nil {
		f.h.t.Fatal("FT.INFO has no number_of_uses counter")
	}
	count, err := strconv.ParseInt(fmt.Sprint(fields["number_of_uses"]), 10, 64)
	if err != nil {
		f.h.t.Fatal(err)
	}
	return count
}

// Application index capture preserves exact source values in a private copy.
// SQL predicates and reducers use that copy independently of later source changes.
func TestAdoptAppIndex(t *testing.T) {
	f := newHashFixture(t, "it:ad:app:")
	f.dropAdopted("it_ad_app")
	t.Cleanup(func() { f.dropAdopted("it_ad_app") })
	f.hset("it:ad:app:1", "name", "Ann", "city", "New York", "age", 31, "score", "1.5", "code", "a|b")
	f.hset("it:ad:app:2", "name", "Ben", "city", "new york", "age", 42, "score", "2.5", "code", "A")
	f.hset("it:ad:app:3", "name", "Cid", "city", "Paris, France", "age", 25, "score", "0.5", "code", "")
	f.hset("it:ad:app:4", "name", "Dee", "city", " Rome ", "age", 50, "score", "3.5", "code", "a")
	f.hset("it:ad:app:5", "name", "Eve", "city", "Rome", "age", 19)
	// Another collection's HASH, which the index holds too.
	f.hset("it:ad:app:1:prefs", "city", "Rome", "age", 99)
	const idx = "it_ad_app_idx"
	def := []any{"SCHEMA", "name", "TEXT", "city", "TAG", "age", "NUMERIC", "SORTABLE", "score", "NUMERIC",
		"code", "TAG", "SEPARATOR", "|", "CASESENSITIVE"}
	f.appIndex(idx, def...)

	res := f.adopt(HashAdoptOptions{Table: "it_ad_app"})
	cl := res.Report.Checks[0]
	if !cl.Ready || !res.ManagedCopy || res.AppIndex || res.SourceIndex != idx {
		t.Fatalf("dry run: %+v", res)
	}
	expectItem(t, cl, CheckOK, "capture source rows through the application's index "+idx+"; SQL uses a private managed index")
	expectItem(t, cl, CheckMissing, "a private managed copy and its own Search index")
	res = f.adopt(HashAdoptOptions{Table: "it_ad_app", Apply: true})
	if !res.Applied || !res.ManagedCopy || res.AppIndex || res.SourceIndex != idx {
		t.Fatalf("import: %+v", res)
	}
	h := f.h
	indexes, _ := h.query(`SELECT index_name FROM information_schema.tables WHERE table_name='it_ad_app'`)
	if len(indexes) != 1 || indexes[0] == idx {
		t.Fatal("managed copy has no independent index", indexes)
	}
	privateIndex := indexes[0]
	// FT.INFO itself increments this Software counter. Calibrate that cost
	// independently for each index rather than treating a probe as a SQL read.
	probe := f.indexUses(idx)
	before := f.indexUses(idx)
	infoCost := before - probe
	privateProbe := f.indexUses(privateIndex)
	privateBefore := f.indexUses(privateIndex)
	privateInfoCost := privateBefore - privateProbe
	h.expectRows(`SELECT COUNT(*) FROM it_ad_app`, "5")
	for _, c := range []struct{ where, want string }{
		{`city = 'New York'`, "Ann"},
		{`city = 'new york'`, "Ben"},
		{`city = 'Paris, France'`, "Cid"},
		{`city = ' Rome '`, "Dee"},
		{`city = 'Rome'`, "Eve"},
		{`city IN ('Rome', 'New York')`, "Ann,Eve"},
		{`city LIKE 'New%'`, "Ann"},
		{`code = 'a|b'`, "Ann"},
		{`code = 'A'`, "Ben"},
		{`code = 'a'`, "Dee"},
		{`code = ''`, "Cid"},
		{`age >= 30`, "Ann,Ben,Dee"},
		{`age BETWEEN 20 AND 31`, "Ann,Cid"},
		{`score > 1`, "Ann,Ben,Dee"},
		{`name = 'Ann'`, "Ann"},
		{`__key = 'it:ad:app:4'`, "Dee"},
	} {
		h.expectRows(`SELECT string_agg(name, ',' ORDER BY name) FROM it_ad_app WHERE `+c.where, c.want)
	}
	h.expectRows(`SELECT COUNT(*), SUM(age), MAX(age), COUNT(score), SUM(score) FROM it_ad_app WHERE age >= 20`, "4|148|50|4|8.0")
	h.expectRows(`SELECT name FROM it_ad_app ORDER BY age DESC LIMIT 2`, "Dee", "Ben")
	h.expectRows(`SELECT name FROM it_ad_app WHERE score IS NOT NULL ORDER BY score LIMIT 2`, "Cid", "Ann")
	h.expectRows(`SELECT age, COUNT(*) FROM it_ad_app WHERE age < 30 GROUP BY age ORDER BY age`, "19|1", "25|1")

	if f.indexUses(idx)-before != infoCost {
		t.Fatal("SQL read the live application index")
	}
	if f.indexUses(privateIndex)-privateBefore <= privateInfoCost {
		t.Fatal("SQL did not use the private managed index")
	}
	// Source writes are invisible until an explicit refresh.
	f.hset("it:ad:app:6", "name", "Fay", "city", "Rome", "age", 33)
	h.expectRows(`SELECT string_agg(name, ',' ORDER BY name) FROM it_ad_app WHERE city = 'Rome'`, "Eve")
	if !f.adopt(HashAdoptOptions{Table: "it_ad_app", Apply: true, Index: idx}).Applied {
		t.Fatal("refresh did not publish")
	}
	h.expectRows(`SELECT string_agg(name, ',' ORDER BY name) FROM it_ad_app WHERE city = 'Rome'`, "Eve,Fay")
	// Dropping/changing the application's index cannot invalidate a published copy.
	st := &store{client: f.raw}
	if err := st.searchDo(h.ctx, idx, "FT.DROPINDEX", idx).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT COUNT(*) FROM it_ad_app`, "6")
	root := f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	blocked, err := AdoptHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_app", Apply: true, Index: idx})
	if err == nil || blocked == nil || blocked.Applied || blocked.Report.Checks[0].Ready {
		t.Fatal("missing source index refresh was not blocked")
	}
	f.appIndex(idx, append([]any{"FILTER", "@age > 20"}, def...)...)
	h.expectRows(`SELECT COUNT(*) FROM it_ad_app`, "6")
	blocked, err = AdoptHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_app", Apply: true, Index: idx})
	if err == nil || blocked == nil || blocked.Applied || blocked.Report.Checks[0].Ready {
		t.Fatal("incompatible source index refresh was not blocked")
	}
	if root != f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("blocked refresh published")
	}
	f.appIndex(idx, def...)
	h.expectRows(`SELECT COUNT(*) FROM it_ad_app WHERE age > 30`, "4")

	// DROP TABLE leaves the application's index, and its HASHes.
	h.exec(`DROP TABLE it_ad_app`)
	if err := st.searchDo(h.ctx, idx, "FT.INFO", idx).Err(); err != nil {
		t.Errorf("DROP TABLE dropped the application's index: %v", err)
	}
	if n := h.prefixKeyCount(f.raw, f.prefix); n != 7 {
		t.Errorf("DROP TABLE left %d of the 7 HASHes", n)
	}
}

// Source selection and private index policy are reported independently.
func TestAdoptAppIndexChoice(t *testing.T) {
	f := newHashFixture(t, "it:ad:apc:")
	f.dropAdopted("it_ad_apc")
	t.Cleanup(func() { f.dropAdopted("it_ad_apc") })
	for i, c := range []string{"Rome", "Oslo", "Lima"} {
		f.hset(f.prefix+string(rune('1'+i)), "city", c, "n", i)
	}
	f.appIndex("it_ad_apc_f", "FILTER", "@n > 0", "SCHEMA", "city", "TAG", "n", "NUMERIC")
	f.appIndex("it_ad_apc_ok", "SCHEMA", "city", "TAG", "n", "NUMERIC")
	res := f.adopt(HashAdoptOptions{Table: "it_ad_apc"})
	if !res.ManagedCopy || res.AppIndex || res.SourceIndex != "it_ad_apc_ok" {
		t.Fatalf("source choice: %+v", res)
	}
	expectItem(t, res.Report.Checks[0], CheckWarn, `source index it_ad_apc_f cannot capture this HASH collection: it has the FILTER "@n > 0"`)
	res = f.adopt(HashAdoptOptions{Table: "it_ad_apc", OwnIndex: true})
	if !res.ManagedCopy || res.AppIndex || res.SourceIndex != "" || !res.Report.Checks[0].Ready {
		t.Fatalf("direct capture: %+v", res)
	}
	expectItem(t, res.Report.Checks[0], CheckOK, "capture source HASHes directly; SQL uses a private managed index")
	for _, index := range []string{"it_ad_apc_f", "nope"} {
		for _, apply := range []bool{false, true} {
			root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
			blocked, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_apc", Index: index, Apply: apply})
			if (err != nil) != apply || blocked == nil || blocked.Applied || blocked.Report.Checks[0].Ready || blocked.Report.Checks[0].Command != "" {
				t.Fatalf("invalid source report: %+v %v", blocked, err)
			}
			want := `the application's index it_ad_apc_f can't serve: it has the FILTER "@n > 0"`
			if index == "nope" {
				want = "there is no search index named nope on the HASHes under it:ad:apc:"
			}
			expectItem(t, blocked.Report.Checks[0], CheckBlocker, want)
			if root != f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() {
				t.Fatal("invalid source plan published")
			}
		}
	}
	res = f.adopt(HashAdoptOptions{Table: "it_ad_apc", Index: "it_ad_apc_ok", IndexColumns: []string{"n"}, Apply: true})
	if !res.Applied || !res.ManagedCopy || res.AppIndex || res.SourceIndex != "it_ad_apc_ok" {
		t.Fatalf("independent private index: %+v", res)
	}
	f.h.expectRows(`SELECT city FROM it_ad_apc WHERE n = 2`, "Lima")
}

// A reader of a table that reads the application's index needs FT.INFO
// besides the read-only rules (README).
func TestACLAppIndexReader(t *testing.T) {
	a := newACLHarness(t)
	f := newHashFixture(t, "it:ad:apr:")
	f.dropAdopted("it_ad_apr")
	t.Cleanup(func() { f.dropAdopted("it_ad_apr") })
	f.users(5)
	f.appIndex("it_ad_apr_idx", "SCHEMA", "name", "TAG", "balance", "NUMERIC", "SORTABLE")
	if res := f.adopt(HashAdoptOptions{Table: "it_ad_apr", Apply: true}); !res.AppIndex {
		t.Fatalf("%+v", res)
	}
	keys := []string{"%R~adbc:*", "%R~it:ad:apr:*"}
	a.user("it_ad_apr_r", slices.Concat(a.readOnlyCommands(), []string{"+ft.info"}, keys)...)
	reader := a.connect("it_ad_apr_r", nil)
	reader.expectRows(`SELECT name FROM it_ad_apr WHERE balance > 4.5`, "User 5")
	a.expectNoDenials("it_ad_apr_r")
	a.user("it_ad_apr_n", slices.Concat(a.readOnlyCommands(), keys)...)
	a.connect("it_ad_apr_n", nil).expectError(`SELECT COUNT(*) FROM it_ad_apr`, "NOPERM")
}
