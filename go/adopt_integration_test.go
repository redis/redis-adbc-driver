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
	for _, c := range res.Report.Columns {
		if c.Name == "active" && c.InPlaceType != "BOOLEAN" {
			t.Fatalf("active type: %s", c.InPlaceType)
		}
	}
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
	h.expectRows(`SELECT COUNT(DISTINCT __rowid), COUNT(__key) FROM it_ad_users`, "31|31")
	// age holds "n/a" (user 5), so it is VARCHAR; balance is NUMERIC in the
	// index, so this runs there.
	h.expectRows(`SELECT COUNT(*), SUM(balance) FROM it_ad_users WHERE balance >= 25`, "6|166.65")
	h.expectRows(`SELECT name FROM it_ad_users WHERE zip = '00070' AND active = 'true'`, "User 10")
	h.expectRows(`SELECT driver_data_type FROM information_schema.columns WHERE table_name = 'it_ad_users' AND column_name = 'balance'`,
		"NUMERIC(38,2)")

	// SQL modifies the managed copy; the application's HASHes stay unchanged.
	h.exec(`UPDATE it_ad_users SET name = 'Renamed' WHERE __key = 'it:ad:user:3'`)
	h.expectRows(`SELECT name FROM it_ad_users WHERE __key = 'it:ad:user:3'`, "Renamed")
	if v := f.raw.HGet(h.ctx, "it:ad:user:3", "name").Val(); v != "User 3" {
		t.Errorf("UPDATE changed source name: %q", v)
	}
	h.exec(`UPDATE it_ad_users SET zip = '99999' WHERE name = 'Alice'`)
	h.expectRows(`SELECT zip FROM it_ad_users WHERE __key = 'it:ad:user:alice'`, "99999")
	h.exec(`DELETE FROM it_ad_users WHERE __key = 'it:ad:user:4'`)
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users WHERE __key = 'it:ad:user:4'`, "0")
	if n := f.raw.Exists(h.ctx, "it:ad:user:4").Val(); n != 1 {
		t.Error("DELETE changed the source HASH")
	}
	// A managed all-NULL row retains its internal ID and original key.
	h.exec(`UPDATE it_ad_users SET name = NULL, age = NULL, zip = NULL WHERE __key = 'it:ad:user:alice'`)
	h.expectRows(`SELECT __key,name,age,zip FROM it_ad_users WHERE __key = 'it:ad:user:alice'`,
		"it:ad:user:alice|NULL|NULL|NULL")
	if v := f.raw.HGet(h.ctx, "it:ad:user:alice", "name").Val(); v != "Alice" {
		t.Errorf("managed NULL update changed source name: %q", v)
	}
	h.exec(`UPDATE it_ad_users SET zip = NULL WHERE __key = 'it:ad:user:1'`)
	if v := f.raw.HGet(h.ctx, "it:ad:user:1", "zip").Val(); v != "00007" {
		t.Error("NULL update changed source zip")
	}

	// External changes stay invisible until an explicit refresh. A failed
	// typed refresh must retain the complete published copy.
	f.hset("it:ad:user:bob", "name", "Bob", "balance", "1.00", "extra", "x")
	f.hset("it:ad:user:32", "name", "User 32", "balance", "lots")
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "30")
	h.expectRows(`SELECT balance FROM it_ad_users WHERE __key = 'it:ad:user:bob'`)
	root := f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	blocked, err := AdoptHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_users", Apply: true,
		Types: map[string]string{"balance": "NUMERIC(38,2)"}})
	if err == nil || blocked == nil || len(blocked.Report.Checks) != 1 || blocked.Report.Checks[0].Ready || blocked.Applied {
		t.Fatal("invalid typed refresh was not blocked with a report")
	}
	if f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("failed refresh published")
	}
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "30")
	if err := f.raw.Del(h.ctx, "it:ad:user:32").Err(); err != nil {
		t.Fatal(err)
	}
	res = f.adopt(HashAdoptOptions{Table: "it_ad_users"})
	if res.Applied || res.AlreadyAdopted || !res.ManagedCopy || !res.Report.Checks[0].Ready {
		t.Errorf("refresh plan: %+v", res)
	}
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "30")
	res = f.adopt(HashAdoptOptions{Table: "it_ad_users", Apply: true})
	if !res.Applied || !res.ManagedCopy {
		t.Fatal("refresh did not publish a managed copy")
	}
	h.expectRows(`SELECT COUNT(*) FROM it_ad_users`, "32")
	h.expectRows(`SELECT balance FROM it_ad_users WHERE __key = 'it:ad:user:bob'`, "1.00")
	h.expectRows(`SELECT name FROM it_ad_users WHERE balance = 1`, "Bob")
	h.expectRows(`SELECT name FROM it_ad_users WHERE __key = 'it:ad:user:3'`, "User 3")
	h.expectRows(`SELECT name FROM it_ad_users WHERE __key = 'it:ad:user:4'`, "User 4")

	// Managed copies support normal writes/DDL while preserving source data.
	h.exec(`INSERT INTO it_ad_users (name) VALUES ('private only')`)
	h.expectRows(`SELECT name FROM it_ad_users WHERE __key IS NULL`, "private only")
	h.exec(`ALTER TABLE it_ad_users ADD COLUMN private_note VARCHAR`)
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_ad_users RENAME TO it_ad_users_r`)
	h.expectRows(`SELECT name FROM it_ad_users_r WHERE __key = 'it:ad:user:bob'`, "Bob")
	if err := h.setConnOption(OptionStringRenameRekey, "false"); err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_ad_users_r RENAME TO it_ad_users`)
	h.exec(`ALTER TABLE it_ad_users DROP COLUMN zip`)
	if v := f.raw.HGet(h.ctx, "it:ad:user:1", "zip").Val(); v != "00007" {
		t.Errorf("DROP COLUMN changed source zip: %q", v)
	}
	before := f.raw.HGetAll(h.ctx, "it:ad:user:1").Val()
	h.exec(`TRUNCATE it_ad_users`)
	h.expectRows(`SELECT COUNT(__key) FROM it_ad_users`, "0")
	h.exec(`DROP TABLE it_ad_users`)
	// Thirty original users, Alice, Bob, and the nested prefs HASH.
	if n := f.h.prefixKeyCount(f.raw, f.prefix); n != 33 {
		t.Errorf("DROP TABLE left %d of the 33 source HASHes", n)
	}
	if after := f.raw.HGetAll(h.ctx, "it:ad:user:1").Val(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("DROP TABLE changed source HASH: %v, was %v", after, before)
	}
	res = f.adopt(HashAdoptOptions{Table: "it_ad_users", Apply: true})
	if !res.Applied {
		t.Fatal("re-import after DROP failed")
	}
	h.expectRows(`SELECT COUNT(*), COUNT(__key) FROM it_ad_users`, "32|32")
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

// Managed rows have an independent physical namespace even when a schema
// has the same name as an application prefix.
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
	f.h.exec(`CREATE TABLE it_ad_s.t (a INTEGER)`)
	f.h.exec(`INSERT INTO it_ad_s.t VALUES (7)`)
	f.h.expectRows(`SELECT a FROM it_ad_s.t`, "7")
	f.h.expectRows(`SELECT __key,n FROM it_ad_ov`, "it_ad_s:1|1")
	f.h.exec(`DROP SCHEMA it_ad_s CASCADE`)
	f.h.expectRows(`SELECT __key,n FROM it_ad_ov`, "it_ad_s:1|1")
	if fields := f.raw.HGetAll(f.h.ctx, "it_ad_s:1").Val(); len(fields) != 1 || fields["n"] != "1" {
		t.Fatal("schema DDL changed the application HASH", fields)
	}
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
	root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
	res, err := AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_blk", Apply: true,
		Types: map[string]string{"flag": "BOOLEAN"}, Encodings: map[string]string{"flag": "native"}})
	if err == nil || res == nil || len(res.Report.Checks) != 1 {
		t.Fatalf("blocked Apply must return its report and an error: %v", err)
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
	expectItem(t, cl, CheckBlocker, `-type flag=BOOLEAN: 1 value do not fit the source encoding`)
	if root != f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("a refused import published metadata")
	}
	f.h.expectRows(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name='it_ad_blk'`, "0")
	if ok, _ := f.raw.SIsMember(f.h.ctx, adoptedKey, f.prefix).Result(); ok {
		t.Error("a refused adopt reserved the prefix")
	}

	// Driver control storage cannot be treated as application HASHes.
	_, err = AdoptHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: metaPrefix, Table: "it_ad_control", Apply: true})
	if err == nil || !strings.Contains(err.Error(), "source prefix overlaps driver control storage") {
		t.Fatalf("driver control storage accepted as a source: %v", err)
	}
	if root != f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("rejected control-storage import published")
	}

	// A prefix whose keys are all another collection's.
	n := newHashFixture(t, "it:ad:nest:")
	n.hset("it:ad:nest:1:a", "n", "1")
	res = n.adopt(HashAdoptOptions{Table: "it_ad_nest"})
	expectItem(t, res.Report.Checks[0], CheckBlocker, "no source HASHes qualify as rows")
}

// The commands adopt needs besides the driver's, and those an UPDATE of an
// adopted table needs (driver reference).
var (
	aclAdoptCommands = []string{"+scan", "+hkeys"}
	aclUpdateAdopted = []string{"+hlen", "+eval", "+evalsha", "+script|load"}
)

// A user with the driver's rules and adopt's adopts, and then queries; one
// with the update commands updates.
func TestACLAdopt(t *testing.T) {
	raw, err := os.ReadFile("../docs/driver-reference.md")
	if err != nil {
		t.Skip(err)
	}
	for _, cmds := range [][]string{aclAdoptCommands, aclUpdateAdopted} {
		if line := strings.Join(cmds, " "); !strings.Contains(string(raw), line) {
			t.Errorf("the driver reference doesn't list the commands as tested: %s", line)
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
