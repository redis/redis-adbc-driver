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

// Integration tests for the client's options from the URI (database.go)
// and its timeouts (timeout.go).

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// uriWithoutTimeouts is REDIS_URI without its timeout parameters, so that
// the driver's defaults apply.
func uriWithoutTimeouts(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(os.Getenv("REDIS_URI"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Del("read_timeout")
	q.Del("write_timeout")
	u.RawQuery = q.Encode()
	return u.String()
}

// openWith opens a connection to a URI with database options.
func openWith(t *testing.T, uri string, opts map[string]string) *sqlHarness {
	t.Helper()
	ctx := context.Background()
	all := map[string]string{adbc.OptionKeyURI: uri}
	for k, v := range opts {
		all[k] = v
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return &sqlHarness{t: t, ctx: ctx, conn: conn}
}

func (h *sqlHarness) getConnOption(key string) string {
	h.t.Helper()
	v, err := h.conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, key)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

// queryErr runs a query and returns its error.
func (h *sqlHarness) queryErr(sql string) error {
	h.t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, sql); err != nil {
		h.t.Fatal(err)
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err == nil {
		for rdr.Next() {
		}
		err = rdr.Err()
		rdr.Release()
	}
	return err
}

// The options on the database and the connection, and that a connection
// keeps working (temporary tables included) when its timeouts change.
func TestTimeoutOptions(t *testing.T) {
	newSQLHarness(t) // skips without REDIS_URI
	uri := uriWithoutTimeouts(t)
	def := openWith(t, uri, nil)
	if r, w := def.getConnOption(OptionStringReadTimeout), def.getConnOption(OptionStringWriteTimeout); r != "5m0s" || w != "5m0s" {
		t.Errorf("defaults: read %s, write %s", r, w)
	}
	h := openWith(t, uri, map[string]string{OptionStringReadTimeout: "45s"})
	if r, w := h.getConnOption(OptionStringReadTimeout), h.getConnOption(OptionStringWriteTimeout); r != "45s" || w != "45s" {
		t.Errorf("database option: read %s, write %s", r, w)
	}
	h.exec(`CREATE TEMP TABLE it_to_tmp (id INTEGER)`)
	h.exec(`INSERT INTO it_to_tmp VALUES (1)`)

	steps := []struct{ key, value, read, write string }{
		{OptionStringReadTimeout, "2m", "2m0s", "2m0s"},
		{OptionStringWriteTimeout, "10", "2m0s", "10s"},
		{OptionStringReadTimeout, "0", "0s", "10s"},
		{OptionStringReadTimeout, "0", "0s", "10s"},
	}
	for _, s := range steps {
		if err := h.setConnOption(s.key, s.value); err != nil {
			t.Fatalf("%s=%s: %v", s.key, s.value, err)
		}
		if r, w := h.getConnOption(OptionStringReadTimeout), h.getConnOption(OptionStringWriteTimeout); r != s.read || w != s.write {
			t.Errorf("after %s=%s: read %s, write %s; want %s, %s", s.key, s.value, r, w, s.read, s.write)
		}
		h.exec(`INSERT INTO it_to_tmp VALUES (2)`)
	}
	err := h.setConnOption(OptionStringReadTimeout, "-3s")
	want := `Invalid Argument: [redis] invalid adbc.redis.read_timeout "-3s" (want a duration such as 30s or 10m, a number of seconds, or 0 for no timeout)`
	if err == nil || err.Error() != want {
		t.Errorf("invalid value: %v", err)
	}
	h.expectRows(`SELECT COUNT(*) FROM it_to_tmp`, "5")
	h.expectRows(`SELECT table_schema, table_type FROM information_schema.tables WHERE table_name = 'it_to_tmp'`,
		"pg_temp|LOCAL TEMPORARY")
}

// withParams is a URI with more query parameters ("name=value").
func withParams(t *testing.T, uri string, params ...string) string {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for _, p := range params {
		name, value, _ := strings.Cut(p, "=")
		q.Add(name, value)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// eachPrimary runs fn with a client, or each primary's node client.
func eachPrimary(ctx context.Context, client goredis.UniversalClient, fn func(*goredis.Client) error) error {
	if cc, ok := client.(*goredis.ClusterClient); ok {
		var mu sync.Mutex
		return cc.ForEachMaster(ctx, func(_ context.Context, n *goredis.Client) error {
			mu.Lock()
			defer mu.Unlock()
			return fn(n)
		})
	}
	return fn(client.(*goredis.Client))
}

// namedConns returns the connections with a client name, on every primary:
// "address id=…".
func namedConns(t *testing.T, ctx context.Context, raw goredis.UniversalClient, name string) []string {
	t.Helper()
	var conns []string
	err := eachPrimary(ctx, raw, func(n *goredis.Client) error {
		list, err := n.ClientList(ctx).Result()
		if err != nil {
			return err
		}
		for _, line := range strings.Split(list, "\n") {
			if fields := strings.Fields(line); slices.Contains(fields, "name="+name) {
				conns = append(conns, n.Options().Addr+" "+fields[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return conns
}

// The URI's go-redis parameters reach the client, on a cluster too (up to
// v0.0.12 its client took only the credentials, TLS and timeouts): every
// node's client has them, client_name names its connections, and
// min_idle_conns opens connections before they're needed, up to pool_size.
func TestClientURIOptionsTakeEffect(t *testing.T) {
	h := newSQLHarness(t)
	name := fmt.Sprintf("it_curi_%d", time.Now().UnixNano())
	uri := withParams(t, uriWithoutTimeouts(t), "client_name="+name, "pool_size=2", "min_idle_conns=5", "max_retries=4")
	hc, err := dialHashes(h.ctx, map[string]string{adbc.OptionKeyURI: uri})
	if err != nil {
		t.Fatal(err)
	}
	err = eachPrimary(h.ctx, hc.client, func(n *goredis.Client) error {
		addr := n.Options().Addr
		if got, err := n.ClientGetName(h.ctx).Result(); err != nil || got != name {
			return fmt.Errorf("%s: CLIENT GETNAME = %q, %v", addr, got, err)
		}
		o := n.Options()
		if o.PoolSize != 2 || o.MinIdleConns != 5 || o.MaxRetries != 4 || o.ReadTimeout != defaultReadTimeout || o.Protocol != 2 {
			return fmt.Errorf("%s: pool_size %d, min_idle_conns %d, max_retries %d, read timeout %v, protocol %d",
				addr, o.PoolSize, o.MinIdleConns, o.MaxRetries, o.ReadTimeout, o.Protocol)
		}
		for deadline := time.Now().Add(5 * time.Second); n.PoolStats().TotalConns != 2; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				return fmt.Errorf("%s: %d connections, want 2 (min_idle_conns, up to pool_size)", addr, n.PoolStats().TotalConns)
			}
		}
		return nil
	})
	hc.close(h.ctx)
	if err != nil {
		t.Error(err)
	}

	// A connection's client for other timeouts has them too, and closing
	// the connection closes both.
	raw := h.rawClient()
	u := openWith(t, uri, nil)
	before := namedConns(t, h.ctx, raw, name)
	if err := u.setConnOption(OptionStringReadTimeout, "90s"); err != nil {
		t.Fatal(err)
	}
	u.exec(`DROP TABLE IF EXISTS it_curi_none`)
	after := namedConns(t, h.ctx, raw, name)
	if len(slices.DeleteFunc(after, func(c string) bool { return slices.Contains(before, c) })) == 0 {
		t.Errorf("no new connection named %s after the timeout changed (before: %v)", name, before)
	}
	if err := u.conn.Close(u.ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "no connection named "+name, func() bool { return len(namedConns(t, h.ctx, raw, name)) == 0 })
}

// The URI parameters that go-redis takes for a cluster only. On a cluster,
// the client finds the nodes through addr when the URI's own address
// doesn't answer. On a single endpoint, or with adbc.redis.cluster=false,
// they're an error.
func TestClientURIClusterParamsConnect(t *testing.T) {
	h := newSQLHarness(t)
	_, cluster := h.rawClient().(*goredis.ClusterClient)
	uri := os.Getenv("REDIS_URI")
	openErr := func(uri string, opts map[string]string) error {
		t.Helper()
		all := map[string]string{adbc.OptionKeyURI: uri}
		for k, v := range opts {
			all[k] = v
		}
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Open(h.ctx)
		if err == nil {
			_ = conn.Close(h.ctx)
		}
		return err
	}
	err := openErr(withParams(t, uri, "max_redirects=5"), map[string]string{OptionStringCluster: "false"})
	want := "Invalid Argument: [redis] invalid URI: max_redirects applies only to a Redis Cluster (OSS Cluster API), and adbc.redis.cluster is false"
	if err == nil || err.Error() != want {
		t.Errorf("cluster=false: %v\nwant: %s", err, want)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if !cluster {
		err := openErr(withParams(t, uri, "addr="+u.Host, "dialer_retries=2"), nil)
		want := "Invalid Argument: [redis] invalid URI: addr, dialer_retries apply only to a Redis Cluster (OSS Cluster API), and the server is a single endpoint"
		if err == nil || err.Error() != want {
			t.Errorf("single endpoint: %v\nwant: %s", err, want)
		}
		return
	}
	if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Skip("a seed that doesn't answer needs a REDIS_URI on this machine")
	}
	l, err := net.Listen("tcp", net.JoinHostPort(u.Hostname(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close()
	dead := *u
	dead.Host = net.JoinHostPort(u.Hostname(), closedPort)
	c := openWith(t, withParams(t, dead.String(), "addr="+u.Host, "dialer_retries=1", "max_redirects=2"),
		map[string]string{OptionStringCluster: "true"})
	c.dropTables("it_curi_seed")
	c.exec(`CREATE TABLE it_curi_seed (id INTEGER)`)
	c.exec(`INSERT INTO it_curi_seed VALUES (1), (2), (3)`)
	c.expectRows(`SELECT COUNT(*) FROM it_curi_seed`, "3")
	c.exec(`DROP TABLE it_curi_seed`)
}

// pauseTarget returns the node to pause: the server, or on a cluster a
// shard that doesn't hold the driver's metadata, so that the query reads
// its table's metadata and then waits for that shard's part of the search.
func pauseTarget(t *testing.T, ctx context.Context, raw goredis.UniversalClient) (*goredis.Client, bool) {
	t.Helper()
	cc, ok := raw.(*goredis.ClusterClient)
	if !ok {
		return raw.(*goredis.Client), false
	}
	meta, err := cc.MasterForKey(ctx, metaPrefix)
	if err != nil {
		t.Fatal(err)
	}
	var target *goredis.Client
	err = cc.ForEachMaster(ctx, func(ctx context.Context, n *goredis.Client) error {
		if n.Options().Addr != meta.Options().Addr && target == nil {
			target = n
		}
		return nil
	})
	if err != nil || target == nil {
		t.Fatalf("no shard to pause (%v)", err)
	}
	return target, true
}

// A server (or one shard of a cluster) that doesn't answer for a while:
// statements wait with the default timeouts, and fail with a message that
// names the setting with a short one. This pauses the server at REDIS_URI
// for about half a minute, so it only runs with REDIS_PAUSE_TESTS=1.
func TestTimeoutServerPaused(t *testing.T) {
	if os.Getenv("REDIS_PAUSE_TESTS") == "" {
		t.Skip("set REDIS_PAUSE_TESTS=1 to run tests that pause the Redis server at REDIS_URI (CLIENT PAUSE)")
	}
	h := newSQLHarness(t)
	h.dropTables("it_to_paused")
	h.exec(`CREATE TABLE it_to_paused (id INTEGER, label VARCHAR)`)
	h.exec(`INSERT INTO it_to_paused VALUES (1, 'a'), (2, 'b'), (3, 'c')`)
	raw := h.rawClient()
	node, cluster := pauseTarget(t, h.ctx, raw)
	var pausedUntil time.Time
	waitPause := func() { time.Sleep(time.Until(pausedUntil) + 100*time.Millisecond) }
	t.Cleanup(waitPause)
	pause := func(d time.Duration) {
		t.Helper()
		waitPause()
		if err := node.Do(h.ctx, "CLIENT", "PAUSE", d.Milliseconds(), "ALL").Err(); err != nil {
			t.Fatal(err)
		}
		pausedUntil = time.Now().Add(d)
	}
	const q = `SELECT COUNT(*), SUM(id) FROM it_to_paused WHERE id >= 1`
	long := 12 * time.Second
	if cluster {
		long = 8 * time.Second
	}
	// With a short timeout, the error depends on which command waited.
	failed := regexp.MustCompile(`^I/O: \[redis\] failed to read table metadata: read tcp \S+->\S+: i/o timeout ` +
		`\(Redis did not reply within the read timeout, 1s: raise adbc\.redis\.read_timeout, or set it to 0 for no timeout\)$`)
	if cluster {
		failed = regexp.MustCompile(`^I/O: \[redis\] FT\.AGGREGATE failed: read tcp \S+->\S+: i/o timeout ` +
			`\(Redis did not reply within the read timeout, 1s: raise adbc\.redis\.read_timeout, or set it to 0 for no timeout\)$`)
	}
	uri := uriWithoutTimeouts(t)

	// The defaults wait.
	def := openWith(t, uri, nil)
	pause(long)
	start := time.Now()
	def.expectRows(q, "3|6")
	if took := time.Since(start); took < long-2*time.Second {
		t.Errorf("the query took %v: was the server paused?", took)
	} else {
		t.Logf("default timeouts: the query took %v", took.Round(10*time.Millisecond))
	}

	// A database option of 1s fails, and the connection works again once
	// the server answers.
	short := openWith(t, uri, map[string]string{OptionStringReadTimeout: "1s"})
	pause(8 * time.Second)
	start = time.Now()
	err := short.queryErr(q)
	if err == nil || !failed.MatchString(err.Error()) {
		t.Errorf("read_timeout=1s: %v", err)
	}
	t.Logf("read_timeout=1s: the query failed after %v", time.Since(start).Round(10*time.Millisecond))
	waitPause()
	short.expectRows(q, "3|6")

	// The same on the connection, and 0 (no timeout) waits.
	if err := def.setConnOption(OptionStringReadTimeout, "1s"); err != nil {
		t.Fatal(err)
	}
	pause(8 * time.Second)
	if err := def.queryErr(q); err == nil || !failed.MatchString(err.Error()) {
		t.Errorf("connection read_timeout=1s: %v", err)
	}
	waitPause()
	if err := def.setConnOption(OptionStringReadTimeout, "0"); err != nil {
		t.Fatal(err)
	}
	pause(3 * time.Second)
	start = time.Now()
	def.expectRows(q, "3|6")
	if took := time.Since(start); took < time.Second {
		t.Errorf("read_timeout=0: the query took %v: was the server paused?", took)
	}
}
