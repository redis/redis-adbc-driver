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
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	redis "github.com/adbc-drivers/redis/go"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// hasRow reports whether a line of out has exactly these fields.
func hasRow(out string, fields ...string) bool {
	for _, line := range strings.Split(out, "\n") {
		if slices.Equal(strings.Fields(line), fields) {
			return true
		}
	}
	return false
}

// Snapshot sweep reports registered ownership, preserves active reader pins,
// and drains eligible metadata/data without broad prefix deletion.
func TestSweepCommand(t *testing.T) {
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run the redis-adbc integration tests")
	}
	c := rawClient(t, uri)
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
	exec := func(sql string) {
		t.Helper()
		st, err := conn.NewStatement(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(ctx)
		if err := st.SetSqlQuery(ctx, sql); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ExecuteUpdate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	exec("DROP TABLE IF EXISTS it_cli_sw")
	exec("CREATE TABLE it_cli_sw (id BIGINT)")
	exec("INSERT INTO it_cli_sw VALUES(1),(2),(3)")
	query, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := query.SetSqlQuery(ctx, "SELECT key_prefix FROM information_schema.tables WHERE table_schema='public' AND table_name='it_cli_sw'"); err != nil {
		t.Fatal(err)
	}
	result, _, err := query.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Next() || result.RecordBatch().NumRows() != 1 {
		t.Fatal("missing public prefix metadata", result.Err())
	}
	prefix := result.RecordBatch().Column(0).(*array.String).Value(0)
	result.Release()
	query.Close(ctx)
	if prefix == "" || !strings.Contains(prefix, ":{meta}:snapshot:17:rows:") {
		t.Fatal("unexpected private prefix")
	}
	t.Cleanup(func() { c.Del(context.Background(), prefix+"x", prefix+"900") })
	readerStmt, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer readerStmt.Close(ctx)
	for key, value := range map[string]string{redis.OptionStringStreamResults: "true", redis.OptionIntStreamBatchRows: "1"} {
		if err := readerStmt.SetOption(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := readerStmt.SetSqlQuery(ctx, "SELECT id FROM it_cli_sw"); err != nil {
		t.Fatal(err)
	}
	pinned, _, err := readerStmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Release()
	exec("DROP TABLE it_cli_sw")
	out, _, err := runCmd(t, "sweep", "-uri", uri, "-json")
	if err != nil {
		t.Fatal(err)
	}
	var res redis.Sweep
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.ManagedSnapshots || res.Applied || res.Deleted != 0 {
		t.Fatalf("pinned preview: %+v %v", res, err)
	}
	if n := c.Exists(ctx, prefix+"1", prefix+"2", prefix+"3").Val(); n != 3 {
		t.Fatal("preview reclaimed pinned rows", n)
	}
	// Close stops the automatic worker before releasing its streams/pins. The
	// explicit CLI now owns subsequent maintenance, with no competing background GC.
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	pinned.Release()
	if err := c.HSet(ctx, prefix+"x", "app", "keep").Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, prefix+"900", "keep", 0).Err(); err != nil {
		t.Fatal(err)
	}
	out, _, err = runCmd(t, "sweep", "-uri", uri)
	if err != nil || !strings.HasPrefix(out, "Managed snapshot cleanup on Redis") || !strings.Contains(out, "pending retirement can expose more") {
		t.Fatalf("managed preview output: %v\n%s", err, out)
	}
	if n := c.Exists(ctx, prefix+"1", prefix+"2", prefix+"3").Val(); n != 3 {
		t.Fatal("preview deleted eligible rows before Apply", n)
	}
	out, _, err = runCmd(t, "sweep", "-uri", uri, "-apply")
	if err != nil {
		t.Fatal(err)
	}
	if !hasRow(out, "PREFIX", "HASHES", "DELETED") || !hasRow(out, prefix, "3", "3") || !strings.Contains(out, "\nDeleted ") {
		t.Fatalf("managed Apply output:\n%s", out)
	}
	if c.Exists(ctx, prefix+"1", prefix+"2", prefix+"3").Val() != 0 || c.HGet(ctx, prefix+"x", "app").Val() != "keep" || c.Get(ctx, prefix+"900").Val() != "keep" {
		t.Fatal("Apply failed ownership-based reclamation")
	}
	out, _, err = runCmd(t, "sweep", "-uri", uri, "-json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.ManagedSnapshots || res.Applied || res.Found != 0 || res.Deleted != 0 || res.More {
		t.Fatalf("post-Apply JSON: %+v %v", res, err)
	}
	if _, _, err := runCmd(t, "sweep", "x"); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatal("sweep accepted unexpected arguments", err)
	}
	out, _, _ = runCmd(t, "help")
	if !strings.Contains(out, "sweep    reclaim retired snapshot rows") {
		t.Fatal("missing sweep help")
	}
}
