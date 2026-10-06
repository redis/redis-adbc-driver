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
	"strings"
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
func (f *hashFixture) indexUses(name string) string {
	f.h.t.Helper()
	reply, err := (&store{client: f.raw}).searchDo(f.h.ctx, name, "FT.INFO", name).Result()
	if err != nil {
		f.h.t.Fatal(err)
	}
	list, _ := reply.([]any)
	for i := 0; i+1 < len(list); i += 2 {
		if list[i] == "number_of_uses" {
			return fmt.Sprint(list[i+1])
		}
	}
	return ""
}

// The application's index: its NUMERIC attributes answer filters (and, if
// SORTABLE, sorts and aggregates), its TAGs find the rows that may match,
// which the driver checks, and its other attributes don't serve.
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
	if !cl.Ready || !res.AppIndex || res.Index != idx {
		t.Fatalf("dry run: %+v", res)
	}
	expectItem(t, cl, CheckOK, "adopt reads the rows through the application's index it_ad_app_idx, and creates no index of its own")
	expectItem(t, cl, CheckOK, "filters, sorts and aggregates on age run in it_ad_app_idx (SORTABLE NUMERIC)")
	expectItem(t, cl, CheckOK, "filters on score run in it_ad_app_idx (NUMERIC, not SORTABLE")
	expectItem(t, cl, CheckOK, "=, IN and LIKE 'x%' on city, code look values up in it_ad_app_idx (TAG)")
	expectItem(t, cl, CheckWarn, "name has no TAG or NUMERIC attribute in it_ad_app_idx")
	for _, s := range res.Steps {
		if strings.Contains(s, "FT.CREATE") {
			t.Errorf("step %q", s)
		}
	}
	r := f.inspect(HashInspectOptions{Table: "it_ad_app"})
	expectItem(t, checklist(t, r, ChecklistSQLInPlace), CheckOK, "adopt reads the rows through the application's index it_ad_app_idx")

	res = f.adopt(HashAdoptOptions{Table: "it_ad_app", Apply: true})
	if !res.Applied || !res.AppIndex {
		t.Fatalf("adopt: %+v", res)
	}
	st := &store{client: f.raw}
	if own := adoptIndexName("public", "it_ad_app"); st.searchDo(f.h.ctx, own, "FT.INFO", own).Err() == nil {
		t.Errorf("adopt created %s", own)
	}
	h := f.h
	before := f.indexUses(idx)
	h.expectRows(`SELECT COUNT(*) FROM it_ad_app`, "5")
	if f.indexUses(idx) == before {
		t.Errorf("the query didn't use %s", idx)
	}
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

	// A HASH the application writes is a row at once.
	f.hset("it:ad:app:6", "name", "Fay", "city", "Rome", "age", 33)
	h.expectRows(`SELECT string_agg(name, ',' ORDER BY name) FROM it_ad_app WHERE city = 'Rome'`, "Eve,Fay")

	// The application drops its index, or makes it so that it can't serve.
	if err := st.searchDo(h.ctx, idx, "FT.DROPINDEX", idx).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError(`SELECT COUNT(*) FROM it_ad_app`, `"public"."it_ad_app" reads the application's search index it_ad_app_idx, which is gone`)
	f.appIndex(idx, append([]any{"FILTER", "@age > 20"}, def...)...)
	h.expectError(`SELECT COUNT(*) FROM it_ad_app`, `which can't serve anymore (it has the FILTER "@age > 20")`)
	f.appIndex(idx, def...)
	h.expectRows(`SELECT COUNT(*) FROM it_ad_app WHERE age > 30`, "4")
	r = f.inspect(HashInspectOptions{})
	expectItem(t, checklist(t, r, ChecklistSQLInPlace), CheckOK, "it reads them through the application's index it_ad_app_idx")

	// DROP TABLE leaves the application's index, and its HASHes.
	h.exec(`DROP TABLE it_ad_app`)
	if err := st.searchDo(h.ctx, idx, "FT.INFO", idx).Err(); err != nil {
		t.Errorf("DROP TABLE dropped the application's index: %v", err)
	}
	if n := h.prefixKeyCount(f.raw, f.prefix); n != 7 {
		t.Errorf("DROP TABLE left %d of the 7 HASHes", n)
	}
}

// When the application's index can't serve, or isn't wanted, adopt creates
// its own; -index names one, which must serve.
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
	if res.Index != "it_ad_apc_ok" {
		t.Errorf("picked %q", res.Index)
	}
	res = f.adopt(HashAdoptOptions{Table: "it_ad_apc", OwnIndex: true})
	cl := res.Report.Checks[0]
	if res.AppIndex || !cl.Ready {
		t.Errorf("-own-index: %+v", res)
	}
	expectItem(t, cl, CheckWarn, `the application's index it_ad_apc_f can't serve: it has the FILTER "@n > 0"`)
	expectItem(t, cl, CheckMissing, "a search index in the driver's format on it:ad:apc:")
	res = f.adopt(HashAdoptOptions{Table: "it_ad_apc", Index: "it_ad_apc_f"})
	expectItem(t, res.Report.Checks[0], CheckBlocker, `the application's index it_ad_apc_f can't serve: it has the FILTER "@n > 0"`)
	res = f.adopt(HashAdoptOptions{Table: "it_ad_apc", Index: "nope"})
	expectItem(t, res.Report.Checks[0], CheckBlocker, "there is no search index named nope on the HASHes under it:ad:apc:")
	if _, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_apc", IndexColumns: []string{"n"}, Apply: true}); err == nil ||
		!strings.Contains(err.Error(), "-index-columns picks the columns of the index adopt creates") {
		t.Errorf("-index-columns with the application's index: %v", err)
	}

	// -own-index adopts with the driver's index.
	res = f.adopt(HashAdoptOptions{Table: "it_ad_apc", OwnIndex: true, Apply: true})
	if !res.Applied || res.AppIndex || res.Index != adoptIndexName("public", "it_ad_apc") {
		t.Fatalf("adopt -own-index: %+v", res)
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
