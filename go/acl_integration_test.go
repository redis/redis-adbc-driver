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

// Integration tests with Redis ACL users: the command lists that the driver reference
// documents ("ACL permissions"), a read-only user, the errors of commands
// refused inside MULTI, and what key patterns don't cover. The tests create
// users it_acl_* (on every master of a cluster) and delete them afterwards,
// so the user at adminURI needs the ACL command.
//
// To run the whole suite as a user with only the driver's commands, set
// REDIS_URI to that user and REDIS_ADMIN_URI to one that can do everything
// (the tests' own checks use KEYS, ACL, …).

import (
	"context"
	"errors"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// The ACL rules the driver reference documents, by feature.
var (
	aclBaseCommands = []string{
		"+ping", "+info",
		"+ft._list", "+ft.create", "+ft.alter", "+ft.info", "+ft.dropindex", "+ft.aggregate", "+ft.cursor", "+ft.search", "+ft.profile",
		"+get", "+set", "+del", "+exists", "+incrby",
		"+hset", "+hget", "+hmget", "+hgetall", "+hdel", "+hincrby", "+hexists",
		"+sadd", "+srem", "+smembers", "+sismember",
		"+multi", "+exec", "+watch", "+unwatch",
		"+eval", "+copy", "+dbsize", "+time", "+type", "+unlink", "+pexpireat",
		"+hlen", "+hstrlen", "+hscan", "+scard", "+memory|usage",
		"+zadd", "+zrem", "+zcard", "+zrange", "+zrangebylex", "+zrangebyscore", "+zremrangebyscore", "+zscore", "+zmscore", "+zlexcount",
	}
	aclClusterCommands = []string{"+cluster|slots", "+command"}
	aclReadOnly        = []string{
		"+ping", "+info",
		"+ft._list", "+ft.info", "+ft.aggregate", "+ft.cursor", "+ft.search", "+ft.profile",
		"+get", "+exists", "+hget", "+hmget", "+hgetall", "+smembers", "+sismember", "+zmscore",
	}
)

const aclPassword = "it-acl-password"

// aclHarness creates ACL users and connects as them.
type aclHarness struct {
	*sqlHarness
	raw     goredis.UniversalClient
	cluster bool
}

func newACLHarness(t *testing.T) *aclHarness {
	t.Helper()
	h := newSQLHarness(t)
	raw := h.rawClient()
	_, cluster := raw.(*goredis.ClusterClient)
	a := &aclHarness{sqlHarness: h, raw: raw, cluster: cluster}
	if err := a.eachNode(func(n *goredis.Client) error { return n.Do(h.ctx, "ACL", "WHOAMI").Err() }); err != nil {
		t.Skipf("the user at adminURI can't manage ACL users: %v", err)
	}
	return a
}

// eachNode runs fn on the server, or on every master of a cluster (ACL
// users are per node).
func (a *aclHarness) eachNode(fn func(*goredis.Client) error) error {
	if cc, ok := a.raw.(*goredis.ClusterClient); ok {
		return cc.ForEachMaster(a.ctx, func(_ context.Context, n *goredis.Client) error { return fn(n) })
	}
	return fn(a.raw.(*goredis.Client))
}

// user creates an ACL user with the given rules (on top of reset), resets
// the ACL log, and deletes the user when the test ends.
func (a *aclHarness) user(name string, rules ...string) {
	a.t.Helper()
	rules, namespace := aclSnapshotRules(name, rules)
	args := append([]any{"ACL", "SETUSER", name, "reset", "on", ">" + aclPassword, "resetchannels"}, anySlice(rules)...)
	// Also clean partial node setup or a failed domain registration.
	a.t.Cleanup(func() {
		_ = a.eachNode(func(n *goredis.Client) error { return n.Do(context.Background(), "ACL", "DELUSER", name).Err() })
		if namespace != "" {
			_ = a.raw.SRem(context.Background(), snapshotReaderDomains, namespace).Err()
			_ = a.raw.SRem(context.Background(), snapshotReaderDirectory, namespace).Err()
			_ = a.raw.Del(context.Background(), snapshotReaderPrefix+namespace).Err()
		}
	})
	err := a.eachNode(func(n *goredis.Client) error {
		if err := n.Do(a.ctx, args...).Err(); err != nil {
			return err
		}
		return n.Do(a.ctx, "ACL", "LOG", "RESET").Err()
	})
	if err != nil {
		// Redis Software manages users through its management API. WHOAMI
		// is available there, but SETUSER is explicitly blocked by the proxy.
		// Keep other permission errors as failures: they need REDIS_ADMIN_URI.
		if strings.Contains(err.Error(), "command 'acl|setuser' is not allowed") {
			a.t.Skipf("server manages ACL users outside the Redis protocol: %v", err)
		}
		a.t.Fatal(err)
	}
	if namespace != "" {
		if err := a.raw.SAdd(a.ctx, snapshotReaderDomains, namespace).Err(); err != nil {
			a.t.Fatal(err)
		}
	}
}

// Preserve each fixture's explicit command denials and external HASH grants.
// Only logical SQL schema patterns gain the format-16 physical equivalent.
// Restricted clients use the production lease selectors, never catalog writes.
func aclSnapshotRules(name string, rules []string) ([]string, string) {
	writer := slices.Contains(rules, "+hset") && (slices.Contains(rules, "~adbc:*") || slices.Contains(rules, "~*"))
	metadata := slices.Contains(rules, "~adbc:*") || slices.Contains(rules, "%R~adbc:*")
	var out []string
	namespace := ""
	if writer {
		out = append(out, "~adbc_snapshot16_{meta}_*")
	}
	if metadata && !writer {
		policy := SnapshotReaderPermissions("acl-fixture:" + name)
		namespace = policy.Namespace
		// Base query commands remain exactly those requested by the fixture.
		// Lease-control commands and positive metadata keys are canonical.
		base := strings.Fields(rbacBaseACL)
		for _, token := range regexp.MustCompile(`\([^)]*\)|\S+`).FindAllString(policy.ACL, -1) {
			if strings.HasPrefix(token, "+") && slices.Contains(base, token) {
				continue
			}
			out = append(out, token)
		}
	}
	for _, rule := range rules {
		if namespace != "" && (rule == "~adbc:*" || rule == "%R~adbc:*") {
			continue
		}
		out = append(out, rule)
		for _, mode := range []string{"~", "%R~", "%W~"} {
			if !strings.HasPrefix(rule, mode) {
				continue
			}
			pattern := strings.TrimPrefix(rule, mode)
			if pattern == "pg_temp_*" {
				out = append(out, mode+"*~pg_temp_*"+snapshotRowMarker+"*")
			} else if schema, ok := strings.CutSuffix(pattern, ":*"); ok && !strings.ContainsAny(schema, ":*?[]{}") && schema != "adbc" {
				out = append(out, mode+"*~"+snapshotSchemaPart(schema)+snapshotRowMarker+"*")
			}
		}
	}
	return out, namespace
}

func (a *aclHarness) expectSnapshotWriteDenied(reader *sqlHarness, sql string) {
	a.t.Helper()
	before, err := a.raw.HGet(a.ctx, snapshotCatalogKey, "root").Result()
	if err != nil {
		a.t.Fatal(err)
	}
	err = reader.execErr(sql)
	if !isStatus(err, adbc.StatusUnauthorized) || !strings.Contains(err.Error(), "NOPERM restricted snapshot reader cannot acquire a writer lease") {
		a.t.Fatalf("%s: expected the snapshot writer-lease denial, got %v", sql, err)
	}
	after, err := a.raw.HGet(a.ctx, snapshotCatalogKey, "root").Result()
	if err != nil || after != before {
		a.t.Fatal("denied statement changed the published catalog", sql, err)
	}
}

func anySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// driverCommands are the commands of the driver on this server.
func (a *aclHarness) driverCommands() []string {
	cmds := slices.Clone(aclBaseCommands)
	if a.cluster {
		cmds = append(cmds, aclClusterCommands...)
	}
	return cmds
}

func (a *aclHarness) readOnlyCommands() []string {
	cmds := slices.Clone(aclReadOnly)
	if a.cluster {
		cmds = append(cmds, aclClusterCommands...)
	}
	return cmds
}

// userURI is adminURI as an ACL user.
func (a *aclHarness) userURI(name string) string {
	a.t.Helper()
	u, err := url.Parse(adminURI())
	if err != nil {
		a.t.Fatal(err)
	}
	u.User = url.UserPassword(name, aclPassword)
	return u.String()
}

// connect opens a driver connection as an ACL user.
func (a *aclHarness) connect(name string, opts map[string]string) *sqlHarness {
	a.t.Helper()
	return openWith(a.t, a.userURI(name), opts)
}

// denials returns what the ACL log recorded for a user: "reason object".
func (a *aclHarness) denials(name string) []string {
	a.t.Helper()
	var out []string
	err := a.eachNode(func(n *goredis.Client) error {
		entries, err := n.ACLLog(a.ctx, 1000).Result()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Username == name {
				out = append(out, entry.Reason+" "+entry.Object)
			}
		}
		return nil
	})
	if err != nil {
		a.t.Fatal(err)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (a *aclHarness) expectNoDenials(name string) {
	a.t.Helper()
	if d := a.denials(name); len(d) > 0 {
		a.t.Errorf("ACL log for %s: %v", name, d)
	}
}

// The driver reference's recipes are the rules tested here.
func TestACLReadme(t *testing.T) {
	raw, err := os.ReadFile("../docs/driver-reference.md")
	if err != nil {
		t.Skip(err)
	}
	readme := string(raw)
	for what, rules := range map[string][]string{
		"base":      aclBaseCommands,
		"cluster":   aclClusterCommands,
		"read-only": aclReadOnly,
	} {
		if line := strings.Join(rules, " "); !strings.Contains(readme, line) {
			t.Errorf("the driver reference doesn't list the %s commands as tested:\n%s", what, line)
		}
	}
}

// A user with exactly the documented commands and keys runs a workload of
// every kind of statement, without a single command refused.
func TestACLDriverUser(t *testing.T) {
	a := newACLHarness(t)
	a.exec(`DROP SCHEMA IF EXISTS it_acl_s CASCADE`)
	t.Cleanup(func() { a.exec(`DROP SCHEMA IF EXISTS it_acl_s CASCADE`) })
	a.dropTables("it_acl_d", "it_acl_d2", "it_acl_d3", "it_acl_ing")
	keys := []string{"~adbc:*", "~public:*", "~it_acl_s:*", "~pg_temp_*"}
	a.user("it_acl_base", append(a.driverCommands(), keys...)...)
	a.user("it_acl_rekey", append(a.driverCommands(), keys...)...)

	u := a.connect("it_acl_base", map[string]string{OptionStringReadTimeout: "60s"})
	for _, sql := range []string{
		`CREATE SCHEMA it_acl_s`,
		`CREATE TABLE it_acl_s.t (id INTEGER NOT NULL, label VARCHAR, amount NUMERIC(10,2), note VARCHAR NOINDEX)`,
		`INSERT INTO it_acl_s.t VALUES (1, 'apple', 1.50, 'n1'), (2, 'banana', NULL, NULL), (3, 'avocado', 3.25, 'n3')`,
		`UPDATE it_acl_s.t SET amount = 2.00 WHERE id = 2`,
		`DELETE FROM it_acl_s.t WHERE id = 3`,
		`INSERT INTO it_acl_s.t (id, label) VALUES (4, 'apricot')`,
		`ALTER TABLE it_acl_s.t ADD COLUMN qty INTEGER DEFAULT 7`,
		`ALTER TABLE it_acl_s.t DROP COLUMN note`,
		`ALTER TABLE it_acl_s.t RENAME COLUMN qty TO quantity`,
		`ALTER TABLE it_acl_s.t ADD CONSTRAINT pos CHECK (id > 0)`,
		`COMMENT ON TABLE it_acl_s.t IS 'fruit'`,
		`CREATE VIEW it_acl_s.v AS SELECT id, label FROM it_acl_s.t WHERE label LIKE 'a%'`,
		// Rewrites the rows to new keys, with the base commands.
		`ALTER TABLE it_acl_s.t ALTER COLUMN amount TYPE NUMERIC(12,2), ALTER COLUMN quantity TYPE BIGINT`,
		`CREATE TABLE it_acl_d AS SELECT id, label FROM it_acl_s.t`,
		`MERGE INTO it_acl_d d USING it_acl_s.t s ON d.id = s.id WHEN MATCHED THEN UPDATE SET label = s.label || '!'`,
		`ALTER TABLE it_acl_d RENAME TO it_acl_d2`,
		`CREATE TEMP TABLE it_acl_tmp (id INTEGER, label VARCHAR)`,
		`INSERT INTO it_acl_tmp SELECT id, label FROM it_acl_s.t`,
	} {
		u.exec(sql)
	}
	u.expectRows(`SELECT id, label, amount, quantity FROM it_acl_s.t ORDER BY id`, "1|apple|1.50|7", "2|banana|2.00|7", "4|apricot|NULL|7")
	u.expectRows(`SELECT label FROM it_acl_s.t WHERE label LIKE 'ap%' ORDER BY label`, "apple", "apricot")
	u.expectRows(`SELECT COUNT(*), SUM(amount) FROM it_acl_s.t WHERE id >= 2`, "2|2.00")
	u.expectRows(`SELECT id FROM it_acl_s.v ORDER BY id`, "1", "4")
	u.expectRows(`SELECT id, label FROM it_acl_d2 ORDER BY id`, "1|apple!", "2|banana!", "4|apricot!")
	u.expectRows(`SELECT COUNT(*) FROM it_acl_tmp`, "3")
	u.expectRows(`SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = 'it_acl_s' ORDER BY table_name`,
		"t|BASE TABLE", "v|VIEW")
	if _, err := u.conn.GetTableSchema(u.ctx, nil, ptr("it_acl_s"), "t"); err != nil {
		t.Error(err)
	}
	rdr, err := u.conn.GetObjects(u.ctx, adbc.ObjectDepthAll, nil, ptr("it_acl_s"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for rdr.Next() {
	}
	rdr.Release()

	// Bulk ingest, into a temporary table too.
	if err := u.ingest("it_acl_ing", adbc.OptionValueIngestModeCreate, false, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := u.ingest("it_acl_ing", adbc.OptionValueIngestModeAppend, false, 3); err != nil {
		t.Fatal(err)
	}
	if err := u.ingest("it_acl_ing", adbc.OptionValueIngestModeReplace, false, 4, 5); err != nil {
		t.Fatal(err)
	}
	if err := u.ingest("it_acl_ingt", adbc.OptionValueIngestModeCreate, true, 6); err != nil {
		t.Fatal(err)
	}
	u.expectRows(`SELECT id FROM it_acl_ing ORDER BY id`, "4", "5")
	u.expectRows(`SELECT id FROM it_acl_ingt`, "6")

	for _, sql := range []string{
		`TRUNCATE it_acl_d2`,
		`DROP VIEW it_acl_s.v`,
		`DROP TABLE it_acl_d2`,
		`DROP TABLE it_acl_ing`,
	} {
		u.exec(sql)
	}
	// Snapshot rekey copies into a private generation with the same writer
	// profile; the former mutable protocol's INCR/DUMP/RESTORE recipe is gone.
	if err := u.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	u.exec(`ALTER TABLE it_acl_s.t RENAME TO t2`)
	u.expectRows(`SELECT COUNT(*) FROM it_acl_s.t2`, "3")
	u.rekeyNativeLayout(a.raw, "it_acl_s", "t2")
	if err := u.conn.Close(u.ctx); err != nil {
		t.Error(err)
	}
	a.expectNoDenials("it_acl_base")

	r := a.connect("it_acl_rekey", map[string]string{OptionStringRenameRekey: "true", OptionStringReadTimeout: "60s"})
	r.exec(`ALTER TABLE it_acl_s.t2 RENAME TO t3`)
	r.expectRows(`SELECT COUNT(*) FROM it_acl_s.t3`, "3")
	r.rekeyNativeLayout(a.raw, "it_acl_s", "t3")
	a.expectNoDenials("it_acl_rekey")
}

func ptr[T any](v T) *T { return &v }

// With the URI's client_name, go-redis names each connection with CLIENT
// SETNAME, which the user then needs (the driver reference says so).
func TestACLClientName(t *testing.T) {
	a := newACLHarness(t)
	keys := []string{"~adbc:*", "~public:*", "~pg_temp_*"}
	a.user("it_acl_noname", append(a.driverCommands(), keys...)...)
	a.user("it_acl_name", append(append(a.driverCommands(), "+client|setname"), keys...)...)
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(a.ctx, map[string]string{
		adbc.OptionKeyURI: withParams(t, a.userURI("it_acl_noname"), "client_name=it_acl_app")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Open(a.ctx)
	want := "I/O: [redis] failed to connect to Redis: NOPERM User it_acl_noname has no permissions to run the 'client|setname' command"
	if err == nil || err.Error() != want {
		t.Errorf("without +client|setname: %v\nwant: %s", err, want)
	}
	u := openWith(t, withParams(t, a.userURI("it_acl_name"), "client_name=it_acl_app"), nil)
	u.expectRows(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'it_acl_none'`, "0")
	a.expectNoDenials("it_acl_name")
	if readme, err := os.ReadFile("../docs/driver-reference.md"); err == nil &&
		!strings.Contains(strings.Join(strings.Fields(string(readme)), " "), "`+client|setname` if it sets `client_name`") {
		t.Error("the driver reference doesn't say that client_name needs +client|setname")
	}
}

// The documented read-only user: it reads tables, views and metadata, and
// every change is refused, with Redis's reason, before anything changed.
func TestACLReadOnlyUser(t *testing.T) {
	a := newACLHarness(t)
	a.dropTables("it_acl_ro", "it_acl_ro2")
	a.exec(`DROP VIEW IF EXISTS it_acl_rov`)
	t.Cleanup(func() { a.exec(`DROP VIEW IF EXISTS it_acl_rov`) })
	a.exec(`CREATE TABLE it_acl_ro (id INTEGER, label VARCHAR)`)
	a.exec(`INSERT INTO it_acl_ro VALUES (1, 'a'), (2, 'ab'), (3, 'b')`)
	a.exec(`CREATE VIEW it_acl_rov AS SELECT id FROM it_acl_ro WHERE label LIKE 'a%'`)
	a.user("it_acl_ro", append(a.readOnlyCommands(), "%R~adbc:*", "%R~public:*")...)

	u := a.connect("it_acl_ro", map[string]string{OptionStringReadTimeout: "60s"})
	u.expectRows(`SELECT id, label FROM it_acl_ro WHERE label LIKE 'a%' ORDER BY id`, "1|a", "2|ab")
	u.expectRows(`SELECT COUNT(*), SUM(id) FROM it_acl_ro WHERE id > 1`, "2|5")
	u.expectRows(`SELECT label, COUNT(*) FROM it_acl_ro GROUP BY label ORDER BY label`, "a|1", "ab|1", "b|1")
	u.expectRows(`SELECT id FROM it_acl_rov ORDER BY id`, "1", "2")
	u.expectRows(`SELECT column_name FROM information_schema.columns WHERE table_name = 'it_acl_ro' ORDER BY ordinal_position`, "id", "label")
	if _, err := u.conn.GetTableSchema(u.ctx, nil, nil, "it_acl_ro"); err != nil {
		t.Error(err)
	}
	a.expectNoDenials("it_acl_ro")

	for _, sql := range []string{
		`INSERT INTO it_acl_ro VALUES (4, 'c')`,
		`UPDATE it_acl_ro SET label = 'x'`,
		`DELETE FROM it_acl_ro`,
		`TRUNCATE it_acl_ro`,
		`DROP TABLE it_acl_ro`,
		`CREATE TABLE it_acl_ro2 (id INTEGER)`,
		`CREATE VIEW it_acl_rov2 AS SELECT 1 AS x`,
		`ALTER TABLE it_acl_ro ADD COLUMN z INTEGER`,
		`COMMENT ON TABLE it_acl_ro IS 'x'`,
		`CREATE TEMP TABLE it_acl_t (id INTEGER)`,
	} {
		a.expectSnapshotWriteDenied(u, sql)
	}
	a.expectRows(`SELECT id, label FROM it_acl_ro ORDER BY id`, "1|a", "2|ab", "3|b")
	a.expectRows(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name LIKE 'it_acl_ro%'`, "2")
}

// A command refused inside MULTI fails the transaction with EXECABORT; the
// error reports the command Redis refused and its key instead.
func TestACLExecAbort(t *testing.T) {
	a := newACLHarness(t)
	a.dropTables("it_acl_x")
	a.exec(`DROP VIEW IF EXISTS it_acl_xv`)
	t.Cleanup(func() { a.exec(`DROP VIEW IF EXISTS it_acl_xv`) })
	a.exec(`CREATE TABLE it_acl_x (id INTEGER)`)
	a.exec(`INSERT INTO it_acl_x VALUES (1)`)
	a.exec(`CREATE VIEW it_acl_xv AS SELECT id FROM it_acl_x`)
	// Every command the driver needs, but read-only keys.
	a.user("it_acl_keys", append(a.driverCommands(), "%R~adbc:*", "%R~public:*", "~adbc:{meta}:oid:next")...)
	u := a.connect("it_acl_keys", map[string]string{OptionStringReadTimeout: "60s"})
	const denied = "NOPERM No permissions to access a key"
	for _, sql := range []string{
		`CREATE VIEW it_acl_xv2 AS SELECT id FROM it_acl_x`,
		`COMMENT ON TABLE it_acl_x IS 'x'`,
		`INSERT INTO it_acl_x VALUES (2)`,
		`DROP VIEW it_acl_xv`,
	} {
		a.expectSnapshotWriteDenied(u, sql)
	}
	a.expectRows(`SELECT id FROM it_acl_xv`, "1")

	// go-redis still sees EXECABORT through the rewritten error (it then
	// knows that EXEC ended the WATCH).
	opts, err := goredis.ParseURL(a.userURI("it_acl_keys"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Protocol, opts.DisableIdentity = 3, true // the driver's default
	client := newClient(newClientConfig(opts), a.cluster, timeouts{read: time.Minute, write: time.Minute})
	defer client.Close()
	_, inspectCtx, inspectPin := a.inspectionSnapshot(a.raw)
	defer inspectPin.release()
	key, err := resolveSnapshotMetadata(inspectCtx, strings.TrimPrefix(metaKey("public", "it_acl_x"), metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Watch(a.ctx, func(tx *goredis.Tx) error {
		_, err := tx.TxPipelined(a.ctx, func(p goredis.Pipeliner) error {
			p.Get(a.ctx, key)
			p.Set(a.ctx, key, "x", 0)
			return nil
		})
		return err
	}, key)
	if !goredis.IsExecAbortError(err) || err.Error() != denied+" ('set' on "+key+", in a MULTI transaction that Redis discarded)" {
		t.Errorf("raw transaction: %v (EXECABORT: %v)", err, goredis.IsExecAbortError(err))
	}
	var q *queuedError
	if !errors.As(err, &q) {
		t.Errorf("%T", err)
	}
}

// What key patterns do for the search commands (Redis 8.6): queries are
// refused on an index whose key prefix the user can't read, but the
// commands that change indexes declare no keys, so key patterns don't
// restrict them, and FT.DROPINDEX … DD deletes rows the user may only read.
// If this test fails, Redis changed that, and the driver reference's ACL section
// needs updating.
func TestACLSearchCommandsAndKeys(t *testing.T) {
	a := newACLHarness(t)
	a.exec(`DROP SCHEMA IF EXISTS it_acl_other CASCADE`)
	t.Cleanup(func() { a.exec(`DROP SCHEMA IF EXISTS it_acl_other CASCADE`) })
	a.dropTables("it_acl_victim")
	a.exec(`CREATE TABLE it_acl_victim (id INTEGER)`)
	a.exec(`INSERT INTO it_acl_victim VALUES (1), (2), (3)`)
	a.exec(`CREATE SCHEMA it_acl_other`)
	a.exec(`CREATE TABLE it_acl_other.t (id INTEGER, secret VARCHAR)`)
	a.exec(`INSERT INTO it_acl_other.t VALUES (1, 's1'), (2, 's2')`)
	_, otherIndex := a.tableNames(a.raw, "it_acl_other", "t")

	rawAs := func(name string) *store {
		opts, err := goredis.ParseURL(a.userURI(name))
		if err != nil {
			t.Fatal(err)
		}
		opts.Protocol, opts.DisableIdentity = 3, true // the driver's default
		c := newClient(newClientConfig(opts), a.cluster, timeouts{read: time.Minute, write: time.Minute})
		t.Cleanup(func() { _ = c.Close() })
		return &store{client: c}
	}
	// A user who may read only the public schema's keys can't query another
	// schema's index, through the driver or directly.
	a.user("it_acl_schema", append(a.readOnlyCommands(), "%R~adbc:*", "%R~public:*")...)
	const refused = "NOPERM User does not have the required permissions to query the index"
	u := a.connect("it_acl_schema", map[string]string{OptionStringReadTimeout: "60s"})
	u.expectError(`SELECT id FROM it_acl_other.t`, "NOPERM")
	if err := rawAs("it_acl_schema").searchDo(a.ctx, otherIndex, "FT.SEARCH", otherIndex, "*", "NOCONTENT").Err(); err == nil || err.Error() != refused {
		t.Fatalf("FT.SEARCH on another schema's index: %v", err)
	}
	u.expectRows(`SELECT COUNT(*) FROM it_acl_victim`, "3")
	_, err := rawAs("it_acl_schema").aggregate(a.ctx, &aggRequest{index: otherIndex, query: "*", load: []string{"secret"}})
	if err == nil || err.Error() != "I/O: [redis] FT.AGGREGATE failed: "+refused {
		t.Errorf("FT.AGGREGATE … LOAD on another schema's index: %v", err)
	}
	// Exercise destructive module DDL on an independent application index.
	// Deleting a published managed index would intentionally corrupt the
	// snapshot catalog and poison unrelated later inspection/cleanup tests.
	rawFixture := newHashFixture(t, "it:acl:dropper:{meta}:")
	for i := 1; i <= 3; i++ {
		rawFixture.hset(rawFixture.prefix+strconv.Itoa(i), "id", i)
	}
	index := "it_acl_dropper_{meta}_idx"
	rawFixture.appIndex(index, "SCHEMA", "id", "NUMERIC")
	prefix := rawFixture.prefix
	dropper := []string{"+ping", "+ft.dropindex", "%R~*"}
	if a.cluster {
		dropper = append(dropper, aclClusterCommands...)
	}
	a.user("it_acl_dropper", dropper...)
	if err := rawAs("it_acl_dropper").searchDo(a.ctx, index, "FT.DROPINDEX", index, "DD").Err(); err != nil {
		t.Fatalf("FT.DROPINDEX … DD as a read-only user: %v", err)
	}
	if n := a.prefixKeyCount(a.raw, prefix); n != 0 {
		t.Errorf("%d rows left after FT.DROPINDEX … DD", n)
	}
	a.expectNoDenials("it_acl_dropper")
	a.expectRows(`SELECT COUNT(*) FROM it_acl_victim`, "3")
	a.exec(`DROP TABLE it_acl_victim`)
}

// Snapshot DDL retires private generations rather than rolling mutable renames
// back. A refused index cleanup must preserve its rows and remain immediately
// recoverable by another collector, without waiting for a failed owner's lease.
func TestSQLSnapshotCleanupDeniedRecovery(t *testing.T) {
	// This test observes the exact work done by two explicit collectors. A
	// third automatic collector could legitimately remove the three rows after
	// the denial, racing the successor's immediate-recovery assertion.
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run SQL integration tests")
	}
	ctx := context.Background()
	impl, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, retainedTestDatabaseOptions(map[string]string{adbc.OptionKeyURI: uri}))
	if err != nil {
		t.Fatal(err)
	}
	impl.snapshotCompactionDisabled = true
	db := driverbase.NewDatabase(impl)
	conn, err := db.Open(ctx)
	if err != nil {
		_ = db.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx); _ = db.Close(ctx) })
	h := &sqlHarness{t: t, ctx: ctx, conn: conn}
	h.dropTables("it_cleanup_live")
	h.exec("CREATE TABLE it_cleanup_live(v BIGINT)")
	h.exec("INSERT INTO it_cleanup_live VALUES(9007199254740993)")
	raw := h.rawClient()
	s := &store{client: raw, snapshots: true}
	meta, _ := snapshotSweepGarbage(t, h, s, false)
	denied := &snapshotCleanupDenied{index: meta.index()}
	limited := &store{client: h.rawClient(denied), snapshots: true}
	snapshotCheckCleanupRecovery(t, h, s, limited, raw, meta)
	if denied.calls.Load() != 1 {
		t.Fatalf("denied cleanup replayed: %d calls", denied.calls.Load())
	}
}
