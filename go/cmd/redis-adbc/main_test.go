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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	redis "github.com/adbc-drivers/redis/go"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func TestCommandLineErrors(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"copy"}, "-prefix is required"},
		{[]string{"copy", "-prefix", "u:", "-mode", "upsert"}, "unknown -mode"},
		{[]string{"copy", "-prefix", "u:", "-on-error", "skip"}, "unknown -on-error"},
		{[]string{"copy", "-prefix", "u:", "-type", "age"}, "want key=value"},
		{[]string{"adopt"}, "-prefix is required"},
		{[]string{"adopt", "-prefix", "u:", "x"}, "unexpected arguments"},
		{[]string{"check", "u:"}, "unexpected arguments"},
		{[]string{"check", "-bogus"}, "flag provided but not defined"},
		{[]string{"scan"}, `unknown command "scan"`},
		{nil, "no command given"},
	} {
		err := run(ctx, c.args, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err = %v, want it to contain %q", c.args, err, c.want)
		}
	}
	var out bytes.Buffer
	if err := run(ctx, []string{"help"}, &out, io.Discard); err != nil || !strings.Contains(out.String(), "adopt    make an existing HASH collection") {
		t.Errorf("help: %v\n%s", err, out.String())
	}
}

// rawClient connects to Redis without the driver, to write an
// application's HASHes.
func rawClient(t *testing.T, uri string) goredis.UniversalClient {
	t.Helper()
	ctx := context.Background()
	opts, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal(err)
	}
	var c goredis.UniversalClient = goredis.NewClient(opts)
	if info, err := c.Info(ctx, "cluster").Result(); err == nil && strings.Contains(info, "cluster_enabled:1") {
		_ = c.Close()
		c = goredis.NewClusterClient(&goredis.ClusterOptions{Addrs: []string{opts.Addr}, Username: opts.Username,
			Password: opts.Password, TLSConfig: opts.TLSConfig})
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// deleteKeys deletes the keys under a prefix, on every primary.
func deleteKeys(t *testing.T, c goredis.UniversalClient, prefix string) {
	t.Helper()
	del := func(ctx context.Context, n *goredis.Client) error {
		keys, err := n.Keys(ctx, prefix+"*").Result()
		for _, k := range keys {
			if err == nil {
				err = n.Del(ctx, k).Err()
			}
		}
		return err
	}
	ctx := context.Background()
	var err error
	if cc, ok := c.(*goredis.ClusterClient); ok {
		err = cc.ForEachMaster(ctx, del)
	} else {
		err = del(ctx, c.(*goredis.Client))
	}
	if err != nil {
		t.Fatal(err)
	}
}

// sqlInt runs SQL through the driver and returns the first column of its
// first row, as an integer (0 for a statement without rows).
func sqlInt(t *testing.T, uri, sql string) int64 {
	t.Helper()
	ctx := context.Background()
	db, err := redis.NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{adbc.OptionKeyURI: uri})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	st, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(ctx)
	if err := st.SetSqlQuery(ctx, sql); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") {
		if _, err := st.ExecuteUpdate(ctx); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return 0
	}
	rdr, _, err := st.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	defer rdr.Release()
	if !rdr.Next() {
		t.Fatalf("%s: no rows", sql)
	}
	return rdr.RecordBatch().Column(0).(*array.Int64).Value(0)
}

func runCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// fixture writes an application's HASHes under prefix, and drops the
// given tables before and after the test.
func fixture(t *testing.T, prefix string, n int, tables ...string) (string, goredis.UniversalClient) {
	t.Helper()
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run the redis-adbc integration tests")
	}
	c := rawClient(t, uri)
	cleanup := func() {
		for _, table := range tables {
			sqlInt(t, uri, "DROP TABLE IF EXISTS "+table)
		}
		deleteKeys(t, c, prefix)
	}
	cleanup()
	t.Cleanup(cleanup)
	ctx := context.Background()
	for i := 1; i <= n; i++ {
		if err := c.HSet(ctx, fmt.Sprintf("%s%d", prefix, i), "name", fmt.Sprintf("u%d", i), "age", 20+i,
			"active", []string{"true", "false"}[i%2], "since", fmt.Sprintf("2024-02-%02d", i)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	return uri, c
}

func TestCheckCommand(t *testing.T) {
	uri, _ := fixture(t, "it:cli:user:", 20, "it_cli_user")
	out, _, err := runCmd(t, "check", "-uri", uri, "-prefix", "it:cli:user:", "-table", "it_cli_user")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Collection it:cli:user: on Redis",
		"20 HASH keys; the columns are guessed from 20 of them",
		"Guessed columns (as table public.it_cli_user):",
		"COLUMN  COPY TYPE",
		"SQL, copied into a driver table (redis-adbc copy): READY",
		"run: redis-adbc copy -prefix it:cli:user: -table it_cli_user",
		"SQL, adopted in place (redis-adbc adopt): READY",
		"run: redis-adbc adopt -prefix it:cli:user: -table it_cli_user",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Arrow") {
		t.Errorf("check mentions Arrow:\n%s", out)
	}
	out, _, err = runCmd(t, "check", "-uri", uri, "-prefix", "it:cli:user:", "-json")
	if err != nil {
		t.Fatal(err)
	}
	var r redis.HashReport
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Keys != 20 || len(r.Checks) != 2 || !r.Checks[0].Ready || !r.Checks[1].Ready {
		t.Errorf("check -json: %v, %+v", err, r)
	}
	// Without a prefix, the collections.
	out, _, err = runCmd(t, "check", "-uri", uri)
	if err != nil || !strings.Contains(out, "it:cli:user:") {
		t.Errorf("check without -prefix: %v\n%s", err, out)
	}
}

func TestCopyCommand(t *testing.T) {
	uri, _ := fixture(t, "it:cli:cp:", 20, "it_cli_cp")
	out, _, err := runCmd(t, "copy", "-uri", uri, "-prefix", "it:cli:cp:", "-table", "it_cli_cp")
	if err != nil || out != "20 rows copied into public.it_cli_cp\n" {
		t.Fatalf("copy: %v, %q", err, out)
	}
	// i = 2, 4, 6, 8: ages 22 + 24 + 26 + 28.
	if s := sqlInt(t, uri, "SELECT SUM(age) FROM it_cli_cp WHERE active AND since < DATE '2024-02-10'"); s != 100 {
		t.Errorf("SUM(age) = %d", s)
	}
	if _, _, err := runCmd(t, "copy", "-uri", uri, "-prefix", "it:cli:cp:", "-table", "it_cli_cp"); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Errorf("copy again: %v", err)
	}
	out, _, err = runCmd(t, "copy", "-uri", uri, "-prefix", "it:cli:cp:", "-table", "it_cli_cp", "-mode", "replace")
	if err != nil || out != "20 rows copied into public.it_cli_cp\n" {
		t.Errorf("copy -mode replace: %v, %q", err, out)
	}
	if n := sqlInt(t, uri, "SELECT COUNT(*) FROM it_cli_cp"); n != 20 {
		t.Errorf("%d rows after -mode replace", n)
	}
}

// adopt prints what it would do, then -apply does it.
func TestAdoptCommand(t *testing.T) {
	const prefix, table = "it:cli:ad:", "it_cli_ad"
	uri, c := fixture(t, prefix, 5, table)
	ctx := context.Background()
	out, _, err := runCmd(t, "adopt", "-uri", uri, "-prefix", prefix, "-table", table)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SQL, adopted in place (redis-adbc adopt): READY",
		"[missing] 5 HASHes have no __rowid field",
		"run: redis-adbc adopt -prefix it:cli:ad: -table it_cli_ad -apply",
		"Nothing was changed. With -apply, adopt would:",
		"FT.CREATE idx:public:it_cli_ad:adopted ON HASH PREFIX 1 it:cli:ad:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("adopt output lacks %q:\n%s", want, out)
		}
	}
	out, _, err = runCmd(t, "adopt", "-uri", uri, "-prefix", prefix, "-table", table, "-apply")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Adopted: public.it_cli_ad now reads the HASHes under it:cli:ad: (index idx:public:it_cli_ad:adopted; wrote __rowid into 5 HASHes)."; !strings.Contains(out, want) {
		t.Errorf("adopt -apply output lacks %q:\n%s", want, out)
	}
	if n := sqlInt(t, uri, "SELECT COUNT(*) FROM "+table); n != 5 {
		t.Errorf("%d rows", n)
	}
	// A HASH the application adds, then -refresh.
	if err := c.HSet(ctx, prefix+"6", "name", "u6", "age", 26, "active", "true", "since", "2024-02-06").Err(); err != nil {
		t.Fatal(err)
	}
	out, _, err = runCmd(t, "adopt", "-uri", uri, "-prefix", prefix, "-refresh", "-apply")
	if err != nil || !strings.Contains(out, "Wrote __rowid into 1 HASH, which SQL now reads.") {
		t.Errorf("adopt -refresh -apply: %v\n%s", err, out)
	}
	if n := sqlInt(t, uri, "SELECT COUNT(*) FROM "+table); n != 6 {
		t.Errorf("%d rows after -refresh", n)
	}
	sqlInt(t, uri, "DROP TABLE "+table)
	if n, err := c.Exists(ctx, prefix+"1").Result(); err != nil || n != 1 {
		t.Errorf("DROP TABLE of the adopted table deleted the HASHes: %d, %v", n, err)
	}
}
