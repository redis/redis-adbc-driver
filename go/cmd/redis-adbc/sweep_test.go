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
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	redis "github.com/adbc-drivers/redis/go"
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

// A dropped table's rows that a writer left: sweep counts them, and with
// -apply deletes them.
func TestSweepCommand(t *testing.T) {
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run the redis-adbc integration tests")
	}
	c := rawClient(t, uri)
	ctx := context.Background()
	sqlInt(t, uri, "DROP TABLE IF EXISTS it_cli_sw")
	sqlInt(t, uri, "CREATE TABLE it_cli_sw (id BIGINT)")
	raw, err := c.Get(ctx, "adbc:{meta}:table:public:it_cli_sw").Result()
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		KeyPrefix string `json:"key_prefix"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil || meta.KeyPrefix == "" {
		t.Fatalf("the table's key prefix: %v, %s", err, raw)
	}
	prefix := meta.KeyPrefix
	sqlInt(t, uri, "DROP TABLE it_cli_sw")
	for i := 1; i <= 3; i++ {
		if err := c.HSet(ctx, fmt.Sprintf("%s%d", prefix, i), "__rowid", i).Err(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { deleteKeys(t, c, prefix) })

	out, _, err := runCmd(t, "sweep", "-uri", uri)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Released key prefixes on Redis") || !hasRow(out, "PREFIX", "HASHES") || !hasRow(out, prefix, "3") ||
		!strings.Contains(out, "\nNothing was deleted. With -apply, sweep deletes the ") {
		t.Errorf("sweep output:\n%s", out)
	}
	if n, err := c.Exists(ctx, prefix+"1").Result(); err != nil || n != 1 {
		t.Errorf("the dry run deleted a key: %d, %v", n, err)
	}
	out, _, err = runCmd(t, "sweep", "-uri", uri, "-apply")
	if err != nil {
		t.Fatal(err)
	}
	if !hasRow(out, "PREFIX", "HASHES", "DELETED") || !hasRow(out, prefix, "3", "3") || !strings.Contains(out, "\nDeleted ") {
		t.Errorf("sweep -apply output:\n%s", out)
	}
	out, _, err = runCmd(t, "sweep", "-uri", uri, "-json")
	if err != nil {
		t.Fatal(err)
	}
	var res redis.Sweep
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Applied || res.Released == 0 {
		t.Fatalf("sweep -json: %v, %+v", err, res)
	}
	for _, p := range res.Prefixes {
		if p.Prefix == prefix {
			t.Errorf("sweep -json after -apply: %+v", p)
		}
	}

	if _, _, err := runCmd(t, "sweep", "x"); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Errorf("sweep x: %v", err)
	}
	out, _, _ = runCmd(t, "help")
	if !strings.Contains(out, "sweep    delete the rows left under the key prefixes of dropped") {
		t.Errorf("help:\n%s", out)
	}
}
