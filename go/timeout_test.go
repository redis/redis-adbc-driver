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

// Tests of the client's options from the URI (database.go), its timeouts
// and errorHook's messages (timeout.go) that need no Redis server.

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func TestTimeoutParse(t *testing.T) {
	for _, c := range []struct {
		in   string
		want time.Duration
	}{
		{"30s", 30 * time.Second},
		{"10m", 10 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"250ms", 250 * time.Millisecond},
		{"45", 45 * time.Second},
		{"1.5", 1500 * time.Millisecond},
		{" 2m ", 2 * time.Minute},
		{"0", 0},
		{"0s", 0},
	} {
		got, err := parseTimeout(OptionStringReadTimeout, c.in)
		if err != nil || got != c.want {
			t.Errorf("parseTimeout(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"", "abc", "-1", "-5s", "10 minutes", "1e12"} {
		_, err := parseTimeout(OptionStringWriteTimeout, in)
		want := `Invalid Argument: [redis] invalid adbc.redis.write_timeout "` + in +
			`" (want a duration such as 30s or 10m, a number of seconds, or 0 for no timeout)`
		if err == nil || err.Error() != want {
			t.Errorf("parseTimeout(%q): %v\nwant: %s", in, err, want)
		}
	}
}

// The timeouts a client gets, from the options, the URI and the defaults.
func TestTimeoutResolve(t *testing.T) {
	d := func(v time.Duration) *time.Duration { return &v }
	for _, c := range []struct {
		uri         string
		set         timeoutSettings
		read, write time.Duration
		follows     bool
	}{
		{"redis://localhost:6379/0", timeoutSettings{}, 5 * time.Minute, 5 * time.Minute, true},
		{"redis://localhost:6379/0?read_timeout=30s", timeoutSettings{}, 30 * time.Second, 30 * time.Second, true},
		{"redis://localhost:6379/0?read_timeout=20", timeoutSettings{}, 20 * time.Second, 20 * time.Second, true},
		{"redis://localhost:6379/0?read_timeout=-1", timeoutSettings{}, 0, 0, true},
		{"redis://localhost:6379/0?read_timeout=0", timeoutSettings{}, 0, 0, true},
		{"redis://localhost:6379/0?write_timeout=10s", timeoutSettings{}, 5 * time.Minute, 10 * time.Second, false},
		{"redis://localhost:6379/0?read_timeout=30s", timeoutSettings{read: d(time.Second)}, time.Second, time.Second, true},
		{"redis://localhost:6379/0?read_timeout=30s&write_timeout=40s", timeoutSettings{read: d(time.Second)}, time.Second, 40 * time.Second, false},
		{"redis://localhost:6379/0", timeoutSettings{read: d(0), write: d(time.Minute)}, 0, time.Minute, false},
	} {
		opts, err := goredis.ParseURL(c.uri)
		if err != nil {
			t.Fatal(err)
		}
		got, follows := c.set.resolve(opts)
		if got.read != c.read || got.write != c.write || follows != c.follows || got.dial != 5*time.Second {
			t.Errorf("%s %+v: got %+v (follows %v), want read %v write %v (follows %v)", c.uri, c.set, got, follows, c.read, c.write, c.follows)
		}
		// go-redis's own encoding: -1 is none.
		none := func(v time.Duration) time.Duration {
			if v == 0 {
				return -1
			}
			return v
		}
		if r, w := got.goRedis(); r != none(c.read) || w != none(c.write) {
			t.Errorf("%s: go-redis timeouts %v / %v", c.uri, r, w)
		}
	}

	// The database options, before any connection.
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		adbc.OptionKeyURI: "redis://localhost:1/0?write_timeout=7s"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(key, want string) {
		t.Helper()
		if v, err := db.(adbc.GetSetOptionsWithContext).GetOption(ctx, key); err != nil || v != want {
			t.Errorf("GetOption(%s) = %q, %v; want %q", key, v, err, want)
		}
	}
	set := func(key, value, wantErr string) {
		t.Helper()
		err := db.(adbc.GetSetOptionsWithContext).SetOption(ctx, key, value)
		if (err == nil) != (wantErr == "") || (err != nil && err.Error() != wantErr) {
			t.Errorf("SetOption(%s, %q) = %v; want %q", key, value, err, wantErr)
		}
	}
	get(OptionStringReadTimeout, "5m0s")
	get(OptionStringWriteTimeout, "7s")
	set(OptionStringReadTimeout, "90s", "")
	set(OptionStringWriteTimeout, "0", "")
	get(OptionStringReadTimeout, "1m30s")
	get(OptionStringWriteTimeout, "0s")
	set(OptionStringReadTimeout, "soon", `Invalid Argument: [redis] invalid adbc.redis.read_timeout "soon" (want a duration such as 30s or 10m, a number of seconds, or 0 for no timeout)`)
	get(OptionStringReadTimeout, "1m30s")
}

// Every URI parameter that go-redis parses reaches the cluster client, as
// it does the single-endpoint client, under the driver's own settings
// (credentials options, RESP2, no CLIENT SETINFO, the timeouts).
func TestClientURIOptions(t *testing.T) {
	fields := func(t *testing.T, what string, opts any, want map[string]any) {
		t.Helper()
		v := reflect.ValueOf(opts).Elem()
		for name, w := range want {
			if g := v.FieldByName(name).Interface(); g != w {
				t.Errorf("%s %s = %v, want %v", what, name, g, w)
			}
		}
	}
	d := &databaseImpl{password: "from-option", uri: "rediss://app:s%40cret@h1:7000/0?" + strings.Join([]string{
		"client_name=etl", "protocol=3", "max_retries=2", "min_retry_backoff=20ms", "max_retry_backoff=2s",
		"dial_timeout=3", "read_timeout=30s", "write_timeout=40s",
		"pool_fifo=true", "pool_size=7", "pool_timeout=9s", "min_idle_conns=1", "max_idle_conns=4",
		"max_active_conns=8", "max_concurrent_dials=3",
		"conn_max_idle_time=1m", "conn_max_lifetime=1h", "conn_max_lifetime_jitter=5m", "skip_verify=true",
	}, "&")}
	cfg, err := d.clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"Username": "app", "Password": "from-option", "ClientName": "etl", "Protocol": 2, "DisableIdentity": true,
		"MaxRetries": 2, "MinRetryBackoff": 20 * time.Millisecond, "MaxRetryBackoff": 2 * time.Second,
		"DialTimeout": 3 * time.Second, "ReadTimeout": 30 * time.Second, "WriteTimeout": 40 * time.Second,
		"PoolFIFO": true, "PoolSize": 7, "PoolTimeout": 9 * time.Second, "MinIdleConns": 1, "MaxIdleConns": 4,
		"MaxActiveConns": 8, "MaxConcurrentDials": 3,
		"ConnMaxIdleTime": time.Minute, "ConnMaxLifetime": time.Hour, "ConnMaxLifetimeJitter": 5 * time.Minute,
	}
	fields(t, "single", cfg.single, want)
	fields(t, "cluster", cfg.cluster, want)
	if !slices.Equal(cfg.cluster.Addrs, []string{"h1:7000"}) || len(cfg.clusterParams) != 0 {
		t.Errorf("cluster seeds %v, cluster parameters %v", cfg.cluster.Addrs, cfg.clusterParams)
	}
	if tc := cfg.cluster.TLSConfig; tc == nil || tc.ServerName != "h1" || !tc.InsecureSkipVerify || tc.MinVersion != tls.VersionTLS12 {
		t.Errorf("cluster TLS: %+v", tc)
	}

	// The cluster client, with the timeouts that the options resolve to
	// (go-redis's defaults filled in: no write timeout is 0).
	c := newClient(cfg, true, timeouts{read: time.Minute, write: 0, dial: 3 * time.Second}).(*goredis.ClusterClient)
	defer c.Close()
	want["ReadTimeout"], want["WriteTimeout"] = time.Minute, time.Duration(0)
	fields(t, "cluster client", c.Options(), want)
	if got := c.Options().Addrs; !slices.Equal(got, []string{"h1:7000"}) {
		t.Errorf("cluster client seeds %v", got)
	}

	// go-redis's old names for two of them.
	cfg, err = (&databaseImpl{uri: "redis://h1:7000?idle_timeout=2m&max_conn_age=3h"}).clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	fields(t, "cluster", cfg.cluster, map[string]any{"ConnMaxIdleTime": 2 * time.Minute, "ConnMaxLifetime": 3 * time.Hour})

	// No URI: the address.
	cfg, err = (&databaseImpl{address: "h9:6380", username: "u"}).clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.cluster.Addrs, []string{"h9:6380"}) || cfg.cluster.Username != "u" || cfg.cluster.Protocol != 2 {
		t.Errorf("from the address: %+v", cfg.cluster)
	}
}

// The parameters that go-redis parses for a cluster only (ParseClusterURL):
// the cluster client gets them, the single-endpoint client doesn't, and on
// a single endpoint they're an error. Those that read from replicas are
// refused.
func TestClientURIClusterParams(t *testing.T) {
	d := &databaseImpl{uri: "redis://app:p%40ss%2Fw%3Ard@h1:7000/0?pool_size=7&addr=h2:7001&addr=[::1]:7002&" +
		"max_redirects=5&dialer_retries=2&dialer_retry_timeout=50ms&failing_timeout_seconds=30"}
	cfg, err := d.clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	co := cfg.cluster
	if !slices.Equal(co.Addrs, []string{"h1:7000", "h2:7001", "[::1]:7002"}) || co.MaxRedirects != 5 || co.DialerRetries != 2 ||
		co.DialerRetryTimeout != 50*time.Millisecond || co.FailingTimeoutSeconds != 30 || co.PoolSize != 7 || co.Password != "p@ss/w:rd" {
		t.Errorf("cluster options: %+v", co)
	}
	s := cfg.single
	if s.Addr != "h1:7000" || s.DialerRetries != 0 || s.FailingTimeoutSeconds != 0 || s.PoolSize != 7 || s.Password != "p@ss/w:rd" {
		t.Errorf("single-endpoint options: %+v", s)
	}
	if want := []string{"addr", "max_redirects", "dialer_retries", "dialer_retry_timeout", "failing_timeout_seconds"}; !slices.Equal(cfg.clusterParams, want) {
		t.Errorf("cluster parameters %v", cfg.clusterParams)
	}
	// Reading from primaries, the default, is fine.
	cfg, err = (&databaseImpl{uri: "redis://h1:7000?read_only=false&route_randomly=0"}).clientConfig()
	if err != nil || cfg.cluster.ReadOnly || !slices.Equal(cfg.clusterParams, []string{"read_only", "route_randomly"}) {
		t.Errorf("read_only=false: %v, %+v", err, cfg)
	}

	for uri, want := range map[string]string{
		"redis://h1:7000?read_only=true":        `read_only is not supported: the driver reads from primaries only, so that it sees its own writes`,
		"redis://h1:7000?route_by_latency=true": `route_by_latency is not supported: the driver reads from primaries only, so that it sees its own writes`,
		"redis://h1:7000?route_randomly=1":      `route_randomly is not supported: the driver reads from primaries only, so that it sees its own writes`,
		"redis://h1:7000?read_only=maybe":       `redis: invalid read_only boolean: expected true/false/1/0 or an empty string, got "maybe"`,
		"redis://h1:7000?addr=h2":               `redis: unable to parse addr param: h2`,
		"redis://h1:7000?max_redirects=x":       `redis: invalid max_redirects number: strconv.Atoi: parsing "x": invalid syntax`,
		"redis://h1:7000?addr=h2:1&pool_size=x": `redis: invalid pool_size number: strconv.Atoi: parsing "x": invalid syntax`,
		"redis://h1:7000?addr=h2:1&bogus=1":     `redis: unexpected option: bogus`,
	} {
		_, err := (&databaseImpl{uri: uri}).clientConfig()
		if want = "Invalid Argument: [redis] invalid URI: " + want; err == nil || err.Error() != want {
			t.Errorf("%s: %v\nwant: %s", uri, err, want)
		}
	}

	// With adbc.redis.cluster=false, before connecting.
	ctx := context.Background()
	for uri, want := range map[string]string{
		"redis://localhost:1/0?max_redirects=5":                  `max_redirects applies`,
		"redis://localhost:1/0?addr=localhost:2&max_redirects=5": `addr, max_redirects apply`,
	} {
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
			adbc.OptionKeyURI: uri, OptionStringCluster: "false"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Open(ctx)
		want = "Invalid Argument: [redis] invalid URI: " + want + " only to a Redis Cluster (OSS Cluster API), and adbc.redis.cluster is false"
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v\nwant: %s", uri, err, want)
		}
	}
}

// clusterOptions copies every setting of go-redis's Options that
// ClusterOptions has too, so that a go-redis upgrade that adds one fails
// here until the driver copies it.
func TestClusterOptionsCopiesEverySetting(t *testing.T) {
	var o goredis.Options
	src := reflect.ValueOf(&o).Elem()
	for i := 0; i < src.NumField(); i++ {
		f := src.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int64:
			f.SetInt(int64(i + 1))
		case reflect.String:
			f.SetString(src.Type().Field(i).Name)
		case reflect.Pointer:
			f.Set(reflect.New(f.Type().Elem()))
		case reflect.Func:
			f.Set(reflect.MakeFunc(f.Type(), func([]reflect.Value) []reflect.Value { panic("not called") }))
		}
	}
	c := clusterOptions(&o)
	if !slices.Equal(c.Addrs, []string{o.Addr}) {
		t.Errorf("Addrs = %v", c.Addrs)
	}
	dst := reflect.ValueOf(c).Elem()
	for i := 0; i < dst.NumField(); i++ {
		name := dst.Type().Field(i).Name
		got, from := dst.Field(i), src.FieldByName(name)
		if !got.CanSet() || !from.IsValid() || from.Type() != got.Type() {
			continue // a cluster's own setting
		}
		switch got.Kind() {
		case reflect.Interface:
			// Nothing to set them to here; clusterOptions copies these.
			if name != "StreamingCredentialsProvider" && name != "PushNotificationProcessor" {
				t.Errorf("%s: not checked", name)
			}
		case reflect.Func:
			if got.IsNil() {
				t.Errorf("%s not copied", name)
			}
		default:
			if !got.Equal(from) {
				t.Errorf("%s = %v, want %v", name, got, from)
			}
		}
	}
}

// errorHook's messages for timeouts, and the errors it leaves alone.
func TestTimeoutMessages(t *testing.T) {
	h := errorHook{t: timeouts{read: time.Second, write: 0, dial: 5 * time.Second}}
	opErr := func(op string) error {
		return &net.OpError{Op: op, Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6379}, Err: os.ErrDeadlineExceeded}
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{opErr("read"), "read tcp 127.0.0.1:6379: i/o timeout (Redis did not reply within the read timeout, 1s: raise adbc.redis.read_timeout, or set it to 0 for no timeout)"},
		{opErr("write"), "write tcp 127.0.0.1:6379: i/o timeout (could not send the command within the write timeout, 0s: raise adbc.redis.write_timeout, or set it to 0 for no timeout)"},
		{opErr("dial"), "dial tcp 127.0.0.1:6379: i/o timeout (could not connect within the dial timeout, 5s: raise the URI's dial_timeout parameter)"},
		{context.DeadlineExceeded, "context deadline exceeded"},
		{errors.New("ERR unknown command"), "ERR unknown command"},
		{&net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}, "read tcp: connection reset by peer"},
	} {
		got := h.explainTimeout(c.err)
		if got.Error() != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
		if !errors.Is(got, c.err) {
			t.Errorf("%v does not wrap %v", got, c.err)
		}
		// Rewriting is idempotent (cluster commands pass two hooks).
		if again := h.explainTimeout(got); again != got {
			t.Errorf("rewritten twice: %v", again)
		}
	}
	if h.explainTimeout(nil) != nil {
		t.Error("nil error rewritten")
	}
}

// redisErr is an error reply from Redis.
type redisErr string

func (e redisErr) Error() string { return string(e) }
func (redisErr) RedisError()     {}

// EXECABORT is replaced by the error of the command Redis refused to queue.
func TestTimeoutExecAbortMessage(t *testing.T) {
	ctx := context.Background()
	abort := redisErr("EXECABORT Transaction discarded because of previous errors.")
	noperm := redisErr("NOPERM No permissions to access a key")
	cmds := func() []goredis.Cmder {
		multi := goredis.NewStatusCmd(ctx, "multi")
		get := goredis.NewStringCmd(ctx, "get", "adbc:{meta}:view:public:v")
		set := goredis.NewStatusCmd(ctx, "set", "adbc:{meta}:view:public:v", `{"secret":"value"}`, 0)
		sadd := goredis.NewIntCmd(ctx, "sadd", "adbc:{meta}:views:public", "v")
		exec := goredis.NewSliceCmd(ctx, "exec")
		get.SetErr(goredis.Nil)
		set.SetErr(noperm)
		sadd.SetErr(abort)
		return []goredis.Cmder{multi, get, set, sadd, exec}
	}
	run := func(cmds []goredis.Cmder, returned error) error {
		return errorHook{}.ProcessPipelineHook(func(context.Context, []goredis.Cmder) error { return returned })(ctx, cmds)
	}
	want := "NOPERM No permissions to access a key ('set' on adbc:{meta}:view:public:v, in a MULTI transaction that Redis discarded)"
	// A single-endpoint client returns EXECABORT, a cluster client the
	// command's error.
	for _, returned := range []error{abort, noperm} {
		err := run(cmds(), returned)
		if err == nil || err.Error() != want {
			t.Errorf("got  %v\nwant %s", err, want)
		}
		// (That go-redis still sees EXECABORT through it needs the typed
		// error a server returns: see TestACLExecAbort.)
		if wrapped := wrapRedis(err, "failed to create view"); wrapped.Error() != "I/O: [redis] failed to create view: "+want {
			t.Errorf("wrapped: %v", wrapped)
		}
	}
	// Other pipelines and errors stay as they are.
	plain := cmds()[1:4]
	if err := run(plain, noperm); err != noperm {
		t.Errorf("pipeline: %v", err)
	}
	other := errors.New("redis: client is closed")
	if err := run(cmds(), other); err != other {
		t.Errorf("client error: %v", err)
	}
	// MULTI refused: every command has its error, and nothing was queued.
	multi := redisErr("NOPERM User u has no permissions to run the 'multi' command")
	refused := cmds()
	for _, c := range refused {
		c.SetErr(multi)
	}
	if err := run(refused, multi); err != multi {
		t.Errorf("MULTI refused: %v", err)
	}
}

// A server that accepts connections but never replies: the read timeout
// fires, and the message names the setting.
func TestTimeoutNoReply(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't listen on localhost: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { // read and drop everything
				buf := make([]byte, 4096)
				for {
					if _, err := conn.Read(buf); err != nil {
						_ = conn.Close()
						return
					}
				}
			}()
		}
	}()
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		adbc.OptionKeyURI:       "redis://" + ln.Addr().String() + "/0",
		OptionStringReadTimeout: "200ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = db.Open(ctx)
	want := regexp.MustCompile(`^I/O: \[redis\] failed to connect to Redis: read tcp 127\.0\.0\.1:\d+->127\.0\.0\.1:\d+: i/o timeout ` +
		`\(Redis did not reply within the read timeout, 200ms: raise adbc\.redis\.read_timeout, or set it to 0 for no timeout\)$`)
	if err == nil || !want.MatchString(err.Error()) {
		t.Fatalf("got %v", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("took %v", took)
	}
}

// fakeRedis is a server that speaks just enough RESP: it replies +PONG to
// PING, nil to HGET and an error to other commands (HELLO among them). On a
// command in hang it stops replying on that connection, as a server that
// hangs does, so the replies to a pipeline don't get out of step. It counts
// the commands it receives.
type fakeRedis struct {
	addr string
	mu   sync.Mutex
	seen map[string]int
}

func newFakeRedis(t *testing.T, hang ...string) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't listen on localhost: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeRedis{addr: ln.Addr().String(), seen: map[string]int{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				rd := bufio.NewReader(conn)
				hung := false
				for {
					args, err := readRESPArray(rd)
					if err != nil {
						return
					}
					name := strings.ToUpper(args[0])
					f.mu.Lock()
					f.seen[name]++
					f.mu.Unlock()
					switch {
					case hung || slices.Contains(hang, name):
						hung = true
					case name == "PING":
						_, _ = conn.Write([]byte("+PONG\r\n"))
					case name == "HGET":
						_, _ = conn.Write([]byte("$-1\r\n"))
					default:
						_, _ = conn.Write([]byte("-ERR unknown command\r\n"))
					}
				}
			}()
		}
	}()
	return f
}

func (f *fakeRedis) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[name]
}

func readRESPArray(rd *bufio.Reader) ([]string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil || n < 1 {
		return nil, fmt.Errorf("bad array header %q", line)
	}
	args := make([]string, n)
	for i := range args {
		if line, err = rd.ReadString('\n'); err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "$")))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(rd, buf); err != nil {
			return nil, err
		}
		args[i] = string(buf[:size])
	}
	return args, nil
}

// go-redis sends most commands again after a reply timed out. It must not
// for search commands (a repeated FT.CURSOR READ would skip a page of rows)
// and for SET … NX and SADD, whose replies the driver acts on (onceCmd), or
// for a pipeline with one of them.
func TestTimeoutNotRetried(t *testing.T) {
	f := newFakeRedis(t, "GET", "FT.CURSOR", "FT.AGGREGATE", "SET", "SADD")
	ctx := context.Background()
	opts := &goredis.Options{Addr: f.addr, Protocol: 2, DisableIdentity: true}
	client := newClient(newClientConfig(opts), false, timeouts{read: 200 * time.Millisecond, write: 200 * time.Millisecond})
	defer client.Close()
	st := &store{client: client}

	// An idempotent command is sent again (go-redis's default).
	if err := client.Get(ctx, "k").Err(); err == nil {
		t.Fatal("GET: no error")
	}
	if n := f.count("GET"); n < 2 {
		t.Errorf("GET sent %d times; expected go-redis to send it again", n)
	}
	// These are sent once.
	err := st.searchDo(ctx, "idx", "FT.CURSOR", "READ", "idx", 1, "COUNT", 10).Err()
	want := regexp.MustCompile(`^read tcp \S+: i/o timeout \(Redis did not reply within the read timeout, 200ms: ` +
		`raise adbc\.redis\.read_timeout, or set it to 0 for no timeout\)$`)
	if err == nil || !want.MatchString(err.Error()) {
		t.Errorf("FT.CURSOR READ: %v", err)
	}
	if _, err := st.aggregate(ctx, &aggRequest{index: "idx", query: "*"}); err == nil {
		t.Error("FT.AGGREGATE: no error")
	}
	// claimNames reserves a prefix and an index name with two SADDs in one
	// pipeline.
	if _, err := st.claimNames(ctx, "public", "t"); err == nil {
		t.Error("claimNames: no error")
	}
	if err := once(ctx, client, "SET", "k", "v", "NX").Err(); err == nil {
		t.Error("SET NX: no error")
	}
	for name, want := range map[string]int{"FT.CURSOR": 1, "FT.AGGREGATE": 1, "SADD": 2, "SET": 1} {
		if n := f.count(name); n != want {
			t.Errorf("%s sent %d times, want %d", name, n, want)
		}
	}
}
