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

// A user with the driver's commands and SCAN sweeps the schemas it may
// write to, and leaves the others alone.
func TestACLSweep(t *testing.T) {
	raw, err := os.ReadFile("../docs/driver-reference.md")
	if err != nil {
		t.Skip(err)
	}
	if want := "`sweep` needs the driver's own rules, with `" + strings.Join(aclSweepCommands, " ") + "`"; !strings.Contains(string(raw), want) {
		t.Errorf("the driver reference doesn't say %s", want)
	}
	a := newACLHarness(t)
	// Verify the existing principal-setup requirement before producing any
	// cleanup fixtures. The same SETUSER denial remains an explicit skip.
	a.user("it_sw_acl", slices.Concat(a.driverCommands(), aclSweepCommands, []string{"~adbc:*", "~public:*"})...)
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
