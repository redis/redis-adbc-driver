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

// Preserve the legacy deletion-time reservation guard on an independent raw
// fixture. Snapshot prefixes are never reused and use reference ownership instead.
func TestSweepRetakenPrefix(t *testing.T) {
	f := newHashFixture(t, "it:sw:retaken:")
	f.hset(f.prefix+"1", "v", "keep")
	// Clone connection options, not hooks. This unmanaged primitive has no SQL
	// descriptor or snapshot catalog; rawClient's metadata admission must stay on.
	var raw goredis.UniversalClient
	switch client := f.raw.(type) {
	case *goredis.Client:
		opts := *client.Options()
		raw = goredis.NewClient(&opts)
	case *goredis.ClusterClient:
		opts := *client.Options()
		raw = goredis.NewClusterClient(&opts)
	default:
		t.Fatalf("unsupported fixture client %T", f.raw)
	}
	defer raw.Close()
	c := &hashConn{client: raw, st: &store{client: raw}}
	s := &sweeper{c: c, res: &Sweep{}, cands: map[string]*SweptPrefix{f.prefix: {Prefix: f.prefix, Found: 1}}, apply: true, batch: []string{f.prefix + "1"}}
	if err := raw.SAdd(f.h.ctx, prefixesKey, f.prefix).Err(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := raw.SRem(context.Background(), prefixesKey, f.prefix).Err(); err != nil {
			t.Error("primitive reservation cleanup", err)
		}
	}()
	if err := s.flush(f.h.ctx); err != nil {
		t.Fatal(err)
	}
	if skipReason(s.res, f.prefix) != sweepRetaken || f.raw.HGet(f.h.ctx, f.prefix+"1", "v").Val() != "keep" {
		t.Fatal("legacy reservation guard deleted a retaken prefix")
	}
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

// The commands sweep needs besides the driver's (driver reference).
var aclSweepCommands = []string{"+scan"}

// A restricted operator reclaims authorized registered data and reports a
// partial error without deleting another schema's rows. An admin can resume.
func TestACLSweep(t *testing.T) {
	raw, err := os.ReadFile("../docs/driver-reference.md")
	if err != nil {
		t.Skip(err)
	}
	if want := "`sweep` needs the driver's own rules, with `" + strings.Join(aclSweepCommands, " ") + "`"; !strings.Contains(string(raw), want) {
		t.Errorf("the driver reference doesn't say %s", want)
	}
	a := newACLHarness(t)
	// Read access lets index validation reach the actual deletion permission
	// boundary; the other schema deliberately has no write grant.
	a.user("it_sw_acl", slices.Concat(a.driverCommands(), aclSweepCommands, []string{"~adbc:*", "~public:*", "%R~it_sw_acls:*", "%R~*~it_sw_acls:{meta}:snapshot:16:rows:*"})...)
	stopMatviewTestMaintenance(t, a.sqlHarness)
	runSweep(t, sweepOpts(adminURI()), true)
	a.dropTables("it_sw_acl")
	a.exec(`DROP SCHEMA IF EXISTS it_sw_acls CASCADE`)
	a.exec(`CREATE SCHEMA it_sw_acls`)
	t.Cleanup(func() { a.exec(`DROP SCHEMA IF EXISTS it_sw_acls CASCADE`) })
	a.exec(`CREATE TABLE it_sw_acl (id BIGINT)`)
	a.exec(`INSERT INTO it_sw_acl SELECT generate_series FROM generate_series(1,20)`)
	a.exec(`CREATE TABLE it_sw_acls.t (id BIGINT)`)
	a.exec(`INSERT INTO it_sw_acls.t VALUES (1)`)
	public, publicIndex := a.tableNames(a.raw, defaultSchema, "it_sw_acl")
	other, otherIndex := a.tableNames(a.raw, "it_sw_acls", "t")
	// Keep both real data generations registered and alive through both DROP
	// statements, then make them eligible together immediately before sweeping.
	_, _, pin := a.inspectionSnapshot(a.raw)
	defer pin.release()
	a.exec(`DROP TABLE it_sw_acl`)
	a.exec(`DROP TABLE it_sw_acls.t`)
	if a.prefixKeyCount(a.raw, public) != 20 || a.prefixKeyCount(a.raw, other) != 1 {
		t.Fatal("pinned sweep fixture lost rows")
	}
	pin.release()
	res, err := SweepReleased(a.ctx, sweepOpts(a.userURI("it_sw_acl")), SweepOptions{Apply: true})
	if err == nil || !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "NOPERM") || res == nil || !res.ManagedSnapshots || !res.Applied || !res.More {
		t.Fatalf("expected explicit partial permission failure: %+v %v", res, err)
	}
	remaining := a.prefixKeyCount(a.raw, public)
	// GC ordering may encounter the forbidden segment first. Every confirmed
	// authorized deletion must still be accounted for by the partial report.
	if res.Deleted != int64(20-remaining) || res.Deleted < 0 || res.Deleted > 20 || a.prefixKeyCount(a.raw, other) != 1 {
		t.Fatalf("partial sweep lost or misreported rows: public=%d report=%+v", remaining, res)
	}
	// Lua's acl_check_cmd preflight can refuse without writing ACL LOG.
	// Verify the same row-key boundary through a direct backend command,
	// which must also be denied and produces the auditable key refusal.
	opts, err := goredis.ParseURL(a.userURI("it_sw_acl"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Protocol, opts.DisableIdentity = 3, true // match the driver's handshake
	probe := newClient(newClientConfig(opts), a.cluster, timeouts{read: time.Minute, write: time.Minute})
	defer probe.Close()
	forbidden := other + "1"
	if a.raw.Exists(a.ctx, forbidden).Val() != 1 {
		t.Fatal("missing forbidden row before direct permission probe")
	}
	if err := probe.Del(a.ctx, forbidden).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("direct row deletion permission was not denied", err)
	}
	if a.prefixKeyCount(a.raw, other) != 1 {
		t.Fatal("direct permission probe changed forbidden rows")
	}
	if d := a.denials("it_sw_acl"); len(d) != 1 || d[0] != "key "+forbidden {
		t.Errorf("ACL log: %v", d)
	}
	runSweep(t, sweepOpts(adminURI()), true)
	a.expectSnapshotReleased(a.raw, public, publicIndex)
	a.expectSnapshotReleased(a.raw, other, otherIndex)
}
