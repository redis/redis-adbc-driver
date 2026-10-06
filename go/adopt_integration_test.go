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
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// adopt runs AdoptHashes on the fixture's prefix.
func (f *hashFixture) adopt(o HashAdoptOptions) *HashAdoption {
	f.h.t.Helper()
	o.Prefix = f.prefix
	res, err := AdoptHashes(f.h.ctx, f.opts, o)
	if err != nil {
		f.h.t.Fatal(err)
	}
	return res
}

// dropAdopted drops a table (an adopted one keeps the HASHes).
func (f *hashFixture) dropAdopted(table string) {
	f.h.exec(`DROP TABLE IF EXISTS ` + table)
}

func TestAdoptHashes(t *testing.T) {
	f := newHashFixture(t, "it:ad:user:")
	f.dropAdopted("it_ad_users")
	f.dropAdopted("it_ad_users_r")
	t.Cleanup(func() {
		f.dropAdopted("it_ad_users")
		f.dropAdopted("it_ad_users_r")
	})
	f.users(30)
	f.hset("it:ad:user:alice", "name", "Alice", "age", "33")
	// Another collection's HASH under the prefix.
	f.hset("it:ad:user:7:prefs", "theme", "dark")

	// A dry run changes nothing.
	res := f.adopt(HashAdoptOptions{Table: "it_ad_users"})
	cl := res.Report.Checks[0]
	if res.Applied || !cl.Ready || len(res.Steps) == 0 {
		t.Fatalf("dry run: %+v", res)
	}
	expectItem(t, cl, CheckOK, "31 HASHes under it:ad:user: are rows")
	expectItem(t, cl, CheckWarn, "1 key (such as it:ad:user:7:prefs) has another ':' after it:ad:user:, and stays out of the table")
	expectItem(t, cl, CheckWarn, "active is VARCHAR in place (BOOLEAN when copied)")
	if n, _ := f.raw.Exists(f.h.ctx, metaKey("public", "it_ad_users")).Result(); n != 0 {
		t.Fatal("the dry run wrote the metadata")
	}

	res = f.adopt(HashAdoptOptions{Table: "it_ad_users", Apply: true})
	if !res.Applied {
		t.Fatalf("adopt: %+v", res)
	}
	h := f.h
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "31")
	// Adopting writes nothing into the HASHes.
	for _, k := range []string{"it:ad:user:1", "it:ad:user:alice"} {
		if ok, _ := f.raw.HExists(h.ctx, k, rowIDField).Result(); ok {
			t.Errorf("adopt wrote %s into %s", rowIDField, k)
		}
	}
	h.expectRows(`SELECT name, age, balance, zip FROM it_ad_users WHERE __key = 'it:ad:user:12'`, "User 12|32|12.12|00084")
	h.expectRows(`SELECT __key, age FROM it_ad_users WHERE name = 'Alice'`, "it:ad:user:alice|33")
	for _, k := range []string{"it:ad:user:7:prefs", "it:ad:user:nobody", "other:1"} {
		h.expectRows(`SELECT COUNT(*) FROM it_ad_users WHERE __key = '`+k+`'`, "0")
	}
	if schema := h.expectRows(`SELECT * FROM it_ad_users WHERE __key = 'it:ad:user:alice'`,
		"Alice|33|NULL|NULL|NULL|NULL|NULL"); schema.NumFields() != 7 {
		t.Errorf("SELECT * has %d columns, with __key: %v", schema.NumFields(), schema)
	}
	h.expectError(`SELECT __rowid FROM it_ad_users`, "__rowid")
	// age holds "n/a" (user 5), so it is VARCHAR; balance is NUMERIC in the
	// index, so this runs there.
	h.expectRows(`SELECT COUNT(*), SUM(balance) FROM it_ad_users WHERE balance >= 25`, "6|166.65")
	h.expectRows(`SELECT name FROM it_ad_users WHERE zip = '00070' AND active = 'true'`, "User 10")
	h.expectRows(`SELECT data_type FROM information_schema.columns WHERE table_name = 'it_ad_users' AND column_name = 'balance'`,
		"NUMERIC(38,2)")

	// UPDATE and DELETE change the application's HASHes.
	h.exec(`UPDATE it_ad_users SET name = 'Renamed' WHERE __key = 'it:ad:user:3'`)
	if v, _ := f.raw.HGet(h.ctx, "it:ad:user:3", "name").Result(); v != "Renamed" {
		t.Errorf("UPDATE wrote %q", v)
	}
	h.exec(`UPDATE it_ad_users SET zip = '99999' WHERE name = 'Alice'`)
	if v, _ := f.raw.HGet(h.ctx, "it:ad:user:alice", "zip").Result(); v != "99999" {
		t.Errorf("UPDATE wrote %q", v)
	}
	h.exec(`DELETE FROM it_ad_users WHERE __key = 'it:ad:user:4'`)
	if n, _ := f.raw.Exists(h.ctx, "it:ad:user:4").Result(); n != 0 {
		t.Error("DELETE left it:ad:user:4")
	}
	// Redis deletes a HASH without fields, so an UPDATE doesn't delete the
	// last ones.
	h.expectError(`UPDATE it_ad_users SET name = NULL, age = NULL, zip = NULL WHERE __key = 'it:ad:user:alice'`,
		`UPDATE would leave the HASH it:ad:user:alice of "public"."it_ad_users" without fields`)
	if v, _ := f.raw.HGet(h.ctx, "it:ad:user:alice", "name").Result(); v != "Alice" {
		t.Errorf("the refused UPDATE changed the HASH: name %q", v)
	}
	h.exec(`UPDATE it_ad_users SET zip = NULL WHERE __key = 'it:ad:user:alice'`)
	if ok, _ := f.raw.HExists(h.ctx, "it:ad:user:alice", "zip").Result(); ok {
		t.Error("UPDATE … SET zip = NULL kept the field")
	}

	// A HASH the application writes is a row at once; one with a value its
	// column can't hold isn't (the index can't hold it).
	f.hset("it:ad:user:bob", "name", "Bob", "balance", "1.00", "extra", "x")
	f.hset("it:ad:user:32", "name", "User 32", "balance", "lots")
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "31")
	h.expectRows(`SELECT balance FROM it_ad_users WHERE __key = 'it:ad:user:bob'`, "1.00")
	h.expectRows(`SELECT name FROM it_ad_users WHERE balance = 1`, "Bob")
	r := f.inspect(HashInspectOptions{})
	ip := checklist(t, r, ChecklistSQLInPlace)
	expectItem(t, ip, CheckOK, "these HASHes are the adopted table public.it_ad_users")
	expectItem(t, ip, CheckWarn, "(the last: it:ad:user:32: Invalid numeric value: 'lots')")
	res = f.adopt(HashAdoptOptions{})
	if res.Applied || !res.AlreadyAdopted || !res.Report.Checks[0].Ready {
		t.Errorf("adopt again: %+v", res)
	}

	// What an adopted table refuses, and what DROP TABLE keeps.
	for _, c := range []struct{ sql, want string }{
		{`INSERT INTO it_ad_users (name) VALUES ('x')`, "adding rows (INSERT, MERGE … INSERT, bulk ingest) isn't supported"},
		{`TRUNCATE it_ad_users`, "TRUNCATE isn't supported"},
		{`ALTER TABLE it_ad_users ADD COLUMN extra VARCHAR`, "ADD COLUMN isn't supported"},
	} {
		h.expectError(c.sql, c.want)
	}
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectError(`ALTER TABLE it_ad_users RENAME TO it_ad_users2`, "RENAME TO with adbc.redis.rename_rekey")
	if err := h.setConnOption(OptionStringRenameRekey, "false"); err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_ad_users RENAME TO it_ad_users_r`)
	h.expectRows(`SELECT name FROM it_ad_users_r WHERE __key = 'it:ad:user:bob'`, "Bob")
	h.exec(`ALTER TABLE it_ad_users_r RENAME TO it_ad_users`)
	h.exec(`ALTER TABLE it_ad_users DROP COLUMN zip`)
	if v, _ := f.raw.HGet(h.ctx, "it:ad:user:1", "zip").Result(); v != "00007" {
		t.Errorf("DROP COLUMN removed the application's field: %q", v)
	}
	before, _ := f.raw.HGetAll(h.ctx, "it:ad:user:1").Result()
	h.exec(`DROP TABLE it_ad_users`)
	// 30 users without user:4, alice, bob, user:32 and the prefs.
	if n := f.h.prefixKeyCount(f.raw, f.prefix); n != 33 {
		t.Errorf("DROP TABLE left %d of the 33 HASHes", n)
	}
	if after, _ := f.raw.HGetAll(h.ctx, "it:ad:user:1").Result(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("DROP TABLE changed it:ad:user:1: %v, was %v", after, before)
	}

	// Adopted again after the DROP.
	if err := f.raw.Del(h.ctx, "it:ad:user:32").Err(); err != nil {
		t.Fatal(err)
	}
	res = f.adopt(HashAdoptOptions{Table: "it_ad_users", Apply: true})
	if !res.Applied {
		t.Errorf("adopt again: %+v", res)
	}
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "31")
}

// An UPDATE neither makes again a HASH that the application deleted after
// the scan found it, nor deletes a HASH's last fields that the application
// removed the others of meanwhile.
func TestAdoptedUpdateRaces(t *testing.T) {
	f := newHashFixture(t, "it:ad:race:")
	f.hset(f.prefix+"1", "n", "1", "s", "a")
	meta := &tableMeta{Schema: "public", Name: "it_ad_race", KeyPrefix: f.prefix, Adopted: true, KeyRows: true}
	st := &store{client: f.raw}
	ctx := f.h.ctx
	if err := keyRowUpdateScript.Load(ctx, f.raw).Err(); err != nil {
		t.Fatal(err)
	}
	gone := rowChange{key: f.prefix + "2", set: []any{"n", "2"}}
	if err := st.updateKeyRows(ctx, meta, []rowChange{gone}); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.raw.Exists(ctx, gone.key).Result(); n != 0 {
		t.Error("the UPDATE made a deleted HASH again")
	}
	last := rowChange{key: f.prefix + "1", del: []string{"n", "s"}}
	if err := st.updateKeyRows(ctx, meta, []rowChange{last}); err == nil || !strings.Contains(err.Error(), "without fields") {
		t.Errorf("deleting the last fields: %v", err)
	}
	if err := st.checkKeyRowsKept(ctx, meta, []rowChange{last}); err == nil || !strings.Contains(err.Error(), "without fields") {
		t.Errorf("check before deleting the last fields: %v", err)
	}
	if v, _ := f.raw.HGetAll(ctx, f.prefix+"1").Result(); len(v) != 2 {
		t.Errorf("the refused change changed the HASH: %v", v)
	}
	if err := st.updateKeyRows(ctx, meta, []rowChange{{key: f.prefix + "1", set: []any{"n", "5"}, del: []string{"s"}}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.raw.HGetAll(ctx, f.prefix+"1").Result(); fmt.Sprint(v) != "map[n:5]" {
		t.Errorf("after the update: %v", v)
	}
}

// A table that redis-adbc v0.0.11 adopted (rows identified by __rowid) is
// refused, until it is dropped and adopted again.
func TestAdoptV0011Table(t *testing.T) {
	f := newHashFixture(t, "it:ad:old:")
	f.dropAdopted("it_ad_old")
	t.Cleanup(func() { f.dropAdopted("it_ad_old") })
	for i := 1; i <= 3; i++ {
		f.hset(fmt.Sprintf("%s%d", f.prefix, i), "n", i, rowIDField, i)
	}
	f.adopt(HashAdoptOptions{Table: "it_ad_old", Apply: true})
	// Its metadata as v0.0.11 wrote it.
	ctx, key := f.h.ctx, metaKey("public", "it_ad_old")
	raw, err := f.raw.Get(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"key_rows":true`) {
		t.Fatalf("metadata: %s", raw)
	}
	if err := f.raw.Set(ctx, key, strings.Replace(raw, `,"key_rows":true`, "", 1), 0).Err(); err != nil {
		t.Fatal(err)
	}
	want := `"public"."it_ad_old" was adopted by redis-adbc v0.0.11, whose adopted tables this driver no longer reads: DROP TABLE "public"."it_ad_old" (its HASHes stay) and run redis-adbc adopt again`
	f.h.expectError(`SELECT COUNT(*) FROM it_ad_old`, want)
	f.h.expectError(`UPDATE it_ad_old SET n = 0`, want)
	res := f.adopt(HashAdoptOptions{})
	if res.Applied || res.Report.Checks[0].Ready {
		t.Errorf("adopt: %+v", res)
	}
	expectItem(t, res.Report.Checks[0], CheckBlocker, "these HASHes are the table public.it_ad_old, which redis-adbc v0.0.11 adopted")
	f.h.exec(`DROP TABLE it_ad_old`)
	if n := f.h.prefixKeyCount(f.raw, f.prefix); n != 3 {
		t.Errorf("DROP TABLE left %d of the 3 HASHes", n)
	}
	f.adopt(HashAdoptOptions{Table: "it_ad_old", Apply: true})
	f.h.expectRows(`SELECT SUM(n) FROM it_ad_old`, "6")
}

// A schema named after the adopted prefix can't get tables: their rows
// would be among the application's HASHes.
func TestAdoptedPrefixOverlap(t *testing.T) {
	f := newHashFixture(t, "it_ad_s:")
	f.dropAdopted("it_ad_ov")
	f.h.exec(`DROP SCHEMA IF EXISTS it_ad_s CASCADE`)
	t.Cleanup(func() {
		f.dropAdopted("it_ad_ov")
		f.h.exec(`DROP SCHEMA IF EXISTS it_ad_s CASCADE`)
	})
	f.hset("it_ad_s:1", "n", "1")
	f.adopt(HashAdoptOptions{Table: "it_ad_ov", Apply: true})
	f.h.exec(`CREATE SCHEMA it_ad_s`)
	f.h.expectError(`CREATE TABLE it_ad_s.t (a INTEGER)`, `under the key prefix "it_ad_s:" of an adopted table's HASHes`)
}

// What adopt refuses before it writes anything.
func TestAdoptBlockers(t *testing.T) {
	f := newHashFixture(t, "it:ad:blk:")
	f.dropAdopted("it_ad_blk")
	t.Cleanup(func() { f.dropAdopted("it_ad_blk") })
	// A __rowid field (left by redis-adbc v0.0.11, say) doesn't matter.
	f.hset("it:ad:blk:1", "n", "1", rowIDField, "9")
	f.hset("it:ad:blk:x", "n", "2")
	f.hset("it:ad:blk:3", "n", "3", "flag", "true")
	res, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_blk", Apply: true,
		Types: map[string]string{"flag": "BOOLEAN"}})
	if err != nil {
		t.Fatal(err)
	}
	cl := res.Report.Checks[0]
	if res.Applied || cl.Ready {
		t.Fatalf("adopted: %+v", res)
	}
	for _, it := range cl.Items {
		if it.Status == CheckBlocker && strings.Contains(it.Text, rowIDField) {
			t.Errorf("blocker: %s", it.Text)
		}
	}
	expectItem(t, cl, CheckBlocker, `-type flag=BOOLEAN: 1 value doesn't fit (such as "true" in it:ad:blk:3: the driver stores booleans as 0 and 1)`)
	if n, _ := f.raw.Exists(f.h.ctx, metaKey("public", "it_ad_blk")).Result(); n != 0 {
		t.Error("a refused adopt wrote the metadata")
	}
	if ok, _ := f.raw.SIsMember(f.h.ctx, adoptedKey, f.prefix).Result(); ok {
		t.Error("a refused adopt reserved the prefix")
	}

	// A prefix that a driver table's rows are under.
	f.h.exec(`DROP TABLE IF EXISTS it_ad_tbl`)
	f.h.exec(`CREATE TABLE it_ad_tbl (a INTEGER)`)
	t.Cleanup(func() { f.h.exec(`DROP TABLE IF EXISTS it_ad_tbl`) })
	res, err = AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: "public:", Table: "it_ad_pub"})
	if err != nil {
		t.Fatal(err)
	}
	expectItem(t, res.Report.Checks[0], CheckBlocker, "can be rows of the driver table")

	// A prefix whose keys are all another collection's.
	n := newHashFixture(t, "it:ad:nest:")
	n.hset("it:ad:nest:1:a", "n", "1")
	res = n.adopt(HashAdoptOptions{Table: "it_ad_nest"})
	expectItem(t, res.Report.Checks[0], CheckBlocker, "every HASH key under it:ad:nest: has another ':' after it (such as it:ad:nest:1:a)")
}

// adopt stops and undoes what it did if the index can't index a HASH
// (here a value written between its read and the index).
func TestAdoptIndexFailure(t *testing.T) {
	f := newHashFixture(t, "it:ad:fail:")
	f.dropAdopted("it_ad_fail")
	t.Cleanup(func() { f.dropAdopted("it_ad_fail") })
	for i := 1; i <= 5; i++ {
		f.hset(fmt.Sprintf("%s%d", f.prefix, i), "n", i)
	}
	o := HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_fail", Apply: true}
	o.Progress = func(msg string) {
		if strings.HasPrefix(msg, "created the index") {
			f.hset(f.prefix+"2", "n", "two")
		}
	}
	_, err := AdoptHashes(f.h.ctx, f.opts, o)
	// One HASH, but it may fail twice: when it is written, and when the
	// index's scan of the existing keys reaches it.
	if err == nil || !strings.Contains(err.Error(), "indexing failure") ||
		!strings.Contains(err.Error(), "(the last: it:ad:fail:2: Invalid numeric value: 'two')") {
		t.Fatalf("got %v", err)
	}
	st := &store{client: f.raw}
	idx := adoptIndexName("public", "it_ad_fail")
	if err := st.searchDo(f.h.ctx, idx, "FT.INFO", idx).Err(); err == nil || !isUnknownIndex(err) {
		t.Errorf("the index is still there: %v", err)
	}
	for _, k := range []string{adoptedKey, prefixesKey} {
		if ok, _ := f.raw.SIsMember(f.h.ctx, k, f.prefix).Result(); ok {
			t.Errorf("%s still has the prefix", k)
		}
	}
	if n := f.h.prefixKeyCount(f.raw, f.prefix); n != 5 {
		t.Errorf("%d HASHes left", n)
	}
	// Fixed, it adopts.
	f.hset(f.prefix+"2", "n", "2")
	o.Progress = nil
	if res, err := AdoptHashes(f.h.ctx, f.opts, o); err != nil || !res.Applied {
		t.Fatalf("%+v, %v", res, err)
	}
	f.h.expectRows(`SELECT SUM(n) FROM it_ad_fail`, "15")
}

// The commands adopt needs besides the driver's, and those an UPDATE of an
// adopted table needs (README).
var (
	aclAdoptCommands = []string{"+scan", "+hkeys"}
	aclUpdateAdopted = []string{"+hlen", "+eval", "+evalsha", "+script|load"}
)

// A user with the driver's rules and adopt's adopts, and then queries; one
// with the update commands updates.
func TestACLAdopt(t *testing.T) {
	raw, err := os.ReadFile("../README.md")
	if err != nil {
		t.Skip(err)
	}
	for _, cmds := range [][]string{aclAdoptCommands, aclUpdateAdopted} {
		if line := strings.Join(cmds, " "); !strings.Contains(string(raw), line) {
			t.Errorf("the README doesn't list the commands as tested: %s", line)
		}
	}
	a := newACLHarness(t)
	f := newHashFixture(t, "it:ad:acl:")
	f.dropAdopted("it_ad_acl")
	t.Cleanup(func() { f.dropAdopted("it_ad_acl") })
	f.users(5)
	// Adopting only reads the HASHes.
	a.user("it_ad_acl", slices.Concat(a.driverCommands(false), aclAdoptCommands, []string{"~adbc:*", "~public:*", "%R~it:ad:acl:*"})...)
	res, err := AdoptHashes(a.ctx, map[string]string{adbc.OptionKeyURI: a.userURI("it_ad_acl")},
		HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_acl", Apply: true})
	if err != nil || !res.Applied {
		t.Fatalf("%+v, %v", res, err)
	}
	u := a.connect("it_ad_acl", nil)
	u.expectRows(`SELECT COUNT(*) FROM it_ad_acl`, "5")
	a.expectNoDenials("it_ad_acl")

	// A reader needs the read-only rules and read access to the HASHes.
	a.user("it_ad_reader", slices.Concat(a.readOnlyCommands(), []string{"%R~adbc:*", "%R~it:ad:acl:*"})...)
	reader := a.connect("it_ad_reader", nil)
	reader.expectRows(`SELECT name FROM it_ad_acl WHERE __key = 'it:ad:acl:2'`, "User 2")
	reader.expectRows(`SELECT COUNT(*) FROM it_ad_acl WHERE active = 'true'`, "2")
	a.expectNoDenials("it_ad_reader")

	// A writer needs write access to the HASHes and the update commands.
	a.user("it_ad_writer", slices.Concat(a.driverCommands(false), aclUpdateAdopted, []string{"~adbc:*", "~it:ad:acl:*"})...)
	writer := a.connect("it_ad_writer", nil)
	writer.exec(`UPDATE it_ad_acl SET name = 'Two' WHERE __key = 'it:ad:acl:2'`)
	writer.exec(`UPDATE it_ad_acl SET age = NULL WHERE name = 'Two'`)
	writer.expectRows(`SELECT name, age FROM it_ad_acl WHERE __key = 'it:ad:acl:2'`, "Two|NULL")
	a.expectNoDenials("it_ad_writer")
}

// An adopt that stopped after reserving the names (its process killed)
// is finished by running it again; another table can't take the prefix
// meanwhile.
func TestAdoptResume(t *testing.T) {
	f := newHashFixture(t, "it:ad:res:")
	f.dropAdopted("it_ad_res")
	t.Cleanup(func() {
		f.dropAdopted("it_ad_res")
		_ = f.raw.HDel(f.h.ctx, adoptingKey, f.prefix).Err()
		for _, k := range []string{prefixesKey, adoptedKey} {
			_ = f.raw.SRem(f.h.ctx, k, f.prefix).Err()
		}
	})
	for i := 1; i <= 3; i++ {
		f.hset(fmt.Sprintf("%s%d", f.prefix, i), "n", i)
	}
	idx := adoptIndexName("public", "it_ad_res")
	rec := `{"schema":"public","table":"it_ad_res","index":"` + idx + `"}`
	pipe := f.raw.TxPipeline()
	pipe.SAdd(f.h.ctx, prefixesKey, f.prefix)
	pipe.SAdd(f.h.ctx, adoptedKey, f.prefix)
	pipe.SAdd(f.h.ctx, indexesKey, idx)
	pipe.HSet(f.h.ctx, adoptingKey, f.prefix, rec)
	if _, err := pipe.Exec(f.h.ctx); err != nil {
		t.Fatal(err)
	}
	res := f.adopt(HashAdoptOptions{Table: "it_ad_other"})
	expectItem(t, res.Report.Checks[0], CheckBlocker, "an adopt of it:ad:res: into public.it_ad_res stopped before it finished")
	res = f.adopt(HashAdoptOptions{Table: "it_ad_res"})
	expectItem(t, res.Report.Checks[0], CheckWarn, "an adopt of it:ad:res: into public.it_ad_res stopped before it finished")
	if !res.Report.Checks[0].Ready {
		t.Fatalf("not ready: %+v", res.Report.Checks[0])
	}
	if res = f.adopt(HashAdoptOptions{Table: "it_ad_res", Apply: true}); !res.Applied {
		t.Fatalf("%+v", res)
	}
	f.h.expectRows(`SELECT SUM(n) FROM it_ad_res`, "6")
	if n, _ := f.raw.HExists(f.h.ctx, adoptingKey, f.prefix).Result(); n {
		t.Error("the adopting record is still there")
	}
}
