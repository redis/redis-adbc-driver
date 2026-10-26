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

// SweepReleased (redis-adbc sweep): the HASHes left under the key prefixes
// of dropped and truncated tables. Each test sweeps the whole database, so
// it only checks its own prefixes.

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func sweepOpts(uri string) map[string]string {
	return map[string]string{adbc.OptionKeyURI: uri}
}

func runSweep(t *testing.T, options map[string]string, apply bool) *Sweep {
	t.Helper()
	res, err := SweepReleased(context.Background(), options, SweepOptions{Apply: apply})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != apply {
		t.Errorf("applied %v, want %v", res.Applied, apply)
	}
	return res
}

// swept returns what a sweep found under a prefix (zero if nothing).
func swept(res *Sweep, prefix string) SweptPrefix {
	for _, p := range res.Prefixes {
		if p.Prefix == prefix {
			return p
		}
	}
	return SweptPrefix{Prefix: prefix}
}

// skipReason returns why a sweep left a prefix alone ("" if it didn't).
func skipReason(res *Sweep, prefix string) string {
	for _, s := range res.Skipped {
		if s.Prefix == prefix {
			return s.Reason
		}
	}
	return ""
}

// keysByNode counts the keys under a prefix on each primary.
func (h *sqlHarness) keysByNode(c goredis.UniversalClient, prefix string) map[string]int {
	h.t.Helper()
	var mu sync.Mutex
	out := map[string]int{}
	count := func(ctx context.Context, n *goredis.Client) error {
		keys, err := n.Keys(ctx, globEscape(prefix)+"*").Result()
		mu.Lock()
		defer mu.Unlock()
		if len(keys) > 0 {
			out[n.Options().Addr] = len(keys)
		}
		return err
	}
	var err error
	if cc, ok := c.(*goredis.ClusterClient); ok {
		err = cc.ForEachMaster(h.ctx, count)
	} else {
		err = count(h.ctx, c.(*goredis.Client))
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

// A truncated table's row that a late write left (as in
// TestSQLTruncateRestartLateWrite), and a dropped table's rows that a writer
// that crashed left: a dry run counts them, -apply deletes them, and the
// live tables keep their rows, one of them under a name that the dropped
// one's is a prefix of.
func TestSweepReleased(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_sw_t", "it_sw_t2", "it_sw_d")
	raw := h.rawClient()
	opts := sweepOpts(os.Getenv("REDIS_URI"))

	h.exec(`CREATE TABLE it_sw_t (id BIGINT, note VARCHAR)`)
	h.exec(`CREATE TABLE it_sw_t2 (id BIGINT)`)
	h.exec(`INSERT INTO it_sw_t VALUES (1, 'a'), (2, 'b')`)
	h.exec(`INSERT INTO it_sw_t2 VALUES (1), (2)`)
	st := &store{client: raw}
	stale, err := st.getTable(h.ctx, defaultSchema, "it_sw_t")
	if err != nil {
		t.Fatal(err)
	}
	rows := [][]Value{{intValue(typeInt64, 3), stringValue("old")}}
	a, err := st.allocRows(h.ctx, stale, rows)
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`TRUNCATE it_sw_t RESTART IDENTITY`)
	stale.readAt = time.Now().Add(time.Hour) // within rekeyFence: no check
	if _, err := st.writeRows(h.ctx, stale, a, rows); err != nil {
		t.Fatal(err)
	}
	h.exec(`INSERT INTO it_sw_t (id) VALUES (10), (11)`)
	oldT, newT := stale.prefix(), h.tablePrefix(raw, "public", "it_sw_t")
	t2 := h.tablePrefix(raw, "public", "it_sw_t2")

	h.exec(`CREATE TABLE it_sw_d (id BIGINT)`)
	h.exec(`INSERT INTO it_sw_d VALUES (1), (2), (3)`)
	oldD := h.tablePrefix(raw, "public", "it_sw_d")
	h.exec(`DROP TABLE it_sw_d`)
	// The rows of a writer that read the metadata before the DROP and
	// exited before it found out. A key that isn't a row id and a key that
	// isn't a HASH aren't rows, and stay.
	const left = 50
	for i := 1; i <= left; i++ {
		if err := raw.HSet(h.ctx, fmt.Sprintf("%s%d", oldD, 100+i), rowIDField, 100+i, "id", i).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.HSet(h.ctx, oldD+"x", "id", 1).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Set(h.ctx, oldD+"200", "1", 0).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Del(context.Background(), oldD+"x", oldD+"200") })
	if _, cluster := raw.(*goredis.ClusterClient); cluster {
		if n := len(h.keysByNode(raw, oldD)); n < 2 {
			t.Errorf("the dropped table's keys are on %d primaries, want several", n)
		}
	}

	expect := func(res *Sweep, prefix string, found, deleted int64) {
		t.Helper()
		if got := swept(res, prefix); got.Found != found || got.Deleted != deleted {
			t.Errorf("%s: found %d, deleted %d; want %d, %d", prefix, got.Found, got.Deleted, found, deleted)
		}
	}
	res := runSweep(t, opts, false)
	expect(res, oldT, 1, 0)
	expect(res, oldD, left, 0)
	for _, p := range []string{newT, t2} {
		if got := swept(res, p); got.Found > 0 || skipReason(res, p) != "" {
			t.Errorf("a live table's prefix %s: %+v, %q", p, got, skipReason(res, p))
		}
	}
	if res.Released < 2 || res.Deleted != 0 {
		t.Errorf("released %d, deleted %d", res.Released, res.Deleted)
	}
	if n := h.prefixKeyCount(raw, oldT); n != 1 {
		t.Errorf("the dry run left %d keys under %s, want 1", n, oldT)
	}

	res = runSweep(t, opts, true)
	expect(res, oldT, 1, 1)
	expect(res, oldD, left, left)
	if n := h.prefixKeyCount(raw, oldT); n != 0 {
		t.Errorf("%d keys left under %s", n, oldT)
	}
	if n := h.prefixKeyCount(raw, oldD); n != 2 {
		t.Errorf("%d keys left under %s, want the 2 that aren't rows", n, oldD)
	}
	h.expectRows(`SELECT __rowid, id, note FROM it_sw_t ORDER BY __rowid`, "1|10|NULL", "2|11|NULL")
	h.expectRows(`SELECT COUNT(*) FROM it_sw_t2`, "2")
	for _, p := range []string{newT, t2} {
		if n := h.prefixKeyCount(raw, p); n != 2 {
			t.Errorf("%d keys under the live prefix %s, want 2", n, p)
		}
	}
	// The releases stay counted, so no new table takes the prefixes.
	for _, p := range []string{oldT, oldD} {
		if ok, err := raw.HExists(h.ctx, releasedKey, p).Result(); err != nil || !ok {
			t.Errorf("%s is no longer counted as released (%v)", p, err)
		}
	}
	res = runSweep(t, opts, true)
	expect(res, oldT, 0, 0)
	expect(res, oldD, 0, 0)
}

// The released prefixes that sweep leaves alone.
func TestSweepSkips(t *testing.T) {
	h := newSQLHarness(t)
	raw := h.rawClient()
	st := &store{client: raw}
	opts := sweepOpts(os.Getenv("REDIS_URI"))
	hset := func(key string) {
		t.Helper()
		if err := raw.HSet(h.ctx, key, rowIDField, 1, "id", 1).Err(); err != nil {
			t.Fatal(err)
		}
	}

	// A table dropped while a re-keying rename moves its rows: until the
	// rename has settled, sweep leaves its prefix alone.
	h.dropTables("it_sw_r", "it_sw_r2")
	h.exec(`CREATE TABLE it_sw_r (id BIGINT)`)
	h.exec(`INSERT INTO it_sw_r VALUES (1)`)
	oldR := h.tablePrefix(raw, "public", "it_sw_r")
	job, _, err := st.beginRekey(h.ctx, "public", "it_sw_r", "it_sw_r2")
	if err != nil {
		t.Fatal(err)
	}
	defer job.lease.release()
	h.exec(`DROP TABLE it_sw_r`)
	hset(oldR + "7")

	// A dropped adopted table: the HASHes are the application's.
	f := newHashFixture(t, "it_sw_app:user:")
	f.dropAdopted("it_sw_app")
	t.Cleanup(func() { f.dropAdopted("it_sw_app") })
	f.users(3)
	f.adopt(HashAdoptOptions{Table: "it_sw_app", Apply: true})
	f.dropAdopted("it_sw_app")

	// An adopted table whose HASHes are under a dropped table's prefix.
	h.dropTables("it_sw_ov")
	h.exec(`CREATE TABLE it_sw_ov (id BIGINT)`)
	oldOv := h.tablePrefix(raw, "public", "it_sw_ov")
	h.exec(`DROP TABLE it_sw_ov`)
	g := newHashFixture(t, oldOv+"9")
	g.dropAdopted("it_sw_ovad")
	t.Cleanup(func() { g.dropAdopted("it_sw_ovad") })
	g.hset(oldOv+"91", "name", "x")
	g.adopt(HashAdoptOptions{Table: "it_sw_ovad", Apply: true})

	// A released prefix that isn't of a driver table's form, as v0.0.11 and
	// v0.0.12 left an adopted table's when it was dropped.
	const legacy = "it:sw:legacy:"
	l := newHashFixture(t, legacy)
	l.hset(legacy+"1", "name", "x")
	if err := raw.HIncrBy(h.ctx, releasedKey, legacy, 1).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.HDel(context.Background(), releasedKey, legacy) })

	res := runSweep(t, opts, true)
	for prefix, want := range map[string]string{
		oldR:              sweepMoving,
		"it_sw_app:user:": sweepAdopted,
		oldOv:             fmt.Sprintf("keys under it can be under %q, which a table has", oldOv+"9"),
		legacy:            sweepNotTable,
	} {
		if got := skipReason(res, prefix); got != want {
			t.Errorf("%s: skipped because %q, want %q", prefix, got, want)
		}
		if got := swept(res, prefix); got.Found > 0 {
			t.Errorf("%s: %+v", prefix, got)
		}
	}
	for prefix, n := range map[string]int{oldR: 1, "it_sw_app:user:": 3, oldOv + "9": 1, legacy: 1} {
		if got := h.prefixKeyCount(raw, prefix); got != n {
			t.Errorf("%d keys left under %s, want %d", got, prefix, n)
		}
	}

	// Once the rename has rolled back, sweep deletes the dropped table's
	// row.
	if _, err := st.settleRekey(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	res = runSweep(t, opts, true)
	if got := swept(res, oldR); got.Found != 1 || got.Deleted != 1 || skipReason(res, oldR) != "" {
		t.Errorf("%s after the rename: %+v, %q", oldR, got, skipReason(res, oldR))
	}
	if !slices.ContainsFunc(res.Skipped, func(s SkippedPrefix) bool { return s.Prefix == "it_sw_app:user:" }) {
		t.Errorf("the adopted table's prefix wasn't left alone: %+v", res.Skipped)
	}

	// A table that takes a released prefix while sweep runs, as versions up
	// to v0.0.7 do, keeps its rows.
	h.dropTables("it_sw_re")
	h.exec(`CREATE TABLE it_sw_re (id BIGINT)`)
	oldRe := h.tablePrefix(raw, "public", "it_sw_re")
	h.exec(`DROP TABLE it_sw_re`)
	hset(oldRe + "1")
	c, err := dialHashes(h.ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(h.ctx)
	s := &sweeper{c: c, res: &Sweep{}, cands: map[string]*SweptPrefix{oldRe: {Prefix: oldRe, Found: 1}}, apply: true,
		batch: []string{oldRe + "1"}}
	if err := raw.SAdd(h.ctx, prefixesKey, oldRe).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.SRem(context.Background(), prefixesKey, oldRe) })
	if err := s.flush(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := skipReason(s.res, oldRe); got != sweepRetaken || h.prefixKeyCount(raw, oldRe) != 1 {
		t.Errorf("a prefix taken meanwhile: %q, %d keys", got, h.prefixKeyCount(raw, oldRe))
	}
	raw.Del(h.ctx, oldRe+"1")
}

// driverPrefix tells a driver table's key prefix from others.
func TestSweepDriverPrefix(t *testing.T) {
	for p, want := range map[string]bool{
		rowPrefix("public", "t"):          true,
		namesFor("public", "t", 3).prefix: true,
		rowPrefix("a:b", "c{d} e%"):       true,
		"user:":                           false,
		"app:user:profile:":               false,
		"public:t":                        false,
		":t:":                             false,
		"public:a b:":                     false,
		"public:a%3:":                     false,
		"public:a%3a:":                    false,
	} {
		if got := driverPrefix(p); got != want {
			t.Errorf("driverPrefix(%q) = %v, want %v", p, got, want)
		}
	}
}

// The commands sweep needs besides the driver's (README).
var aclSweepCommands = []string{"+scan"}

// A user with the driver's commands and SCAN sweeps the schemas it may
// write to, and leaves the others alone.
func TestACLSweep(t *testing.T) {
	raw, err := os.ReadFile("../README.md")
	if err != nil {
		t.Skip(err)
	}
	if want := "`sweep` needs the driver's own rules, with `" + strings.Join(aclSweepCommands, " ") + "`"; !strings.Contains(string(raw), want) {
		t.Errorf("the README doesn't say %s", want)
	}
	a := newACLHarness(t)
	// Sweep what other tests left, so that the user only meets these keys.
	runSweep(t, sweepOpts(adminURI()), true)
	leave := func(schema, table string, n int) string {
		t.Helper()
		a.exec(fmt.Sprintf(`CREATE TABLE %s.%s (id BIGINT)`, schema, table))
		prefix := a.tablePrefix(a.raw, schema, table)
		a.exec(fmt.Sprintf(`DROP TABLE %s.%s`, schema, table))
		for i := 1; i <= n; i++ {
			if err := a.raw.HSet(a.ctx, fmt.Sprintf("%s%d", prefix, i), rowIDField, i).Err(); err != nil {
				t.Fatal(err)
			}
		}
		return prefix
	}
	a.dropTables("it_sw_acl")
	a.exec(`DROP SCHEMA IF EXISTS it_sw_acls CASCADE`)
	a.exec(`CREATE SCHEMA it_sw_acls`)
	t.Cleanup(func() { a.exec(`DROP SCHEMA IF EXISTS it_sw_acls CASCADE`) })
	public := leave("public", "it_sw_acl", 20)
	other := leave("it_sw_acls", "t", 1)
	t.Cleanup(func() { a.raw.Del(context.Background(), other+"1") })

	a.user("it_sw_acl", slices.Concat(a.driverCommands(false), aclSweepCommands, []string{"~adbc:*", "~public:*"})...)
	res := runSweep(t, sweepOpts(a.userURI("it_sw_acl")), true)
	if got := swept(res, public); got.Deleted != 20 {
		t.Errorf("%+v", got)
	}
	if got := skipReason(res, other); got != sweepDenied || a.prefixKeyCount(a.raw, other) != 1 {
		t.Errorf("%s: %q", other, got)
	}
	// Redis refused only the DEL of the other schema's key.
	if d := a.denials("it_sw_acl"); len(d) != 1 || d[0] != "key "+other+"1" {
		t.Errorf("ACL log: %v", d)
	}
}
