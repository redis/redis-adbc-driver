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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func planPartitions(t *testing.T, h *sqlHarness, q string, count int) (*arrow.Schema, adbc.Partitions) {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if cap := os.Getenv("REDIS_PARTITION_SCAN_CAP"); cap != "" {
		if err := st.SetOption(h.ctx, OptionIntPartitionScanCap, cap); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSqlQuery(h.ctx, q); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, OptionIntPartitionCount, strconv.Itoa(count)); err != nil {
		t.Fatal(err)
	}
	schema, parts, n, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != -1 || parts.NumPartitions != uint64(len(parts.PartitionIDs)) {
		t.Fatalf("count %d, partitions %+v", n, parts)
	}
	return schema, parts
}

func TestPartitionReads(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_partition_api")
	h.exec("CREATE TABLE it_partition_api (id BIGINT, label TEXT, qty INTEGER)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_partition_api") })
	h.exec("INSERT INTO it_partition_api VALUES (1,'a',NULL),(2,'b',2),(3,'c',3),(4,'d',4),(5,'e',5),(6,'f',6)")
	h.exec("DELETE FROM it_partition_api WHERE id=3")
	worker := newSQLHarness(t)
	worker.exec("CREATE TEMP TABLE it_partition_api (other INTEGER)")
	for _, q := range []string{
		"SELECT id FROM it_partition_api", "SELECT id FROM it_partition_api WHERE qty IS NULL OR qty >= 4",
		"SELECT id FROM it_partition_api WHERE __rowid=2", "SELECT id FROM it_partition_api WHERE __rowid IN (1,4)",
		"SELECT id FROM it_partition_api WHERE false", "SELECT id FROM it_partition_api WHERE id=100",
		"SELECT a.id AS key FROM it_partition_api a WHERE a.qty IS NULL",
	} {
		t.Run(q, func(t *testing.T) {
			schema, parts := planPartitions(t, h, q, 4)
			var actual []int64
			for _, id := range parts.PartitionIDs {
				r, err := worker.conn.ReadPartition(h.ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if !r.Schema().Equal(schema) {
					t.Fatalf("schema %s != %s", r.Schema(), schema)
				}
				vals, _, err := readInts(r)
				r.Release()
				if err != nil {
					t.Fatal(err)
				}
				actual = append(actual, vals...)
			}
			r, _ := h.openStream(h.ctx, h.conn, q, nil, nil)
			expected, _, err := readInts(r)
			r.Release()
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(actual)
			slices.Sort(expected)
			if !slices.Equal(actual, expected) {
				t.Fatalf("got %v want %v", actual, expected)
			}
		})
	}
	_, parts := planPartitions(t, h, "SELECT id FROM it_partition_api", 2)
	// Inserts allocated after planning are outside the captured high watermark.
	h.exec("INSERT INTO it_partition_api VALUES (7,'g',7)")
	var vals []int64
	for _, id := range parts.PartitionIDs {
		r, err := worker.conn.ReadPartition(h.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		v, _, err := readInts(r)
		r.Release()
		if err != nil {
			t.Fatal(err)
		}
		vals = append(vals, v...)
	}
	slices.Sort(vals)
	if !slices.Equal(vals, []int64{1, 2, 4, 5, 6}) {
		t.Fatal(vals)
	}
	// Re-reading a descriptor is allowed, but returns current data.
	r, err := worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	var d partitionID
	json.Unmarshal(parts.PartitionIDs[0], &d)
	d.Target = "0123456789012345678901234567890123456789012345678901234567890123"
	bad, _ := json.Marshal(d)
	if _, err := worker.conn.ReadPartition(h.ctx, bad); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal(err)
	}
	h.exec("TRUNCATE it_partition_api")
	if _, err := worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal(err)
	}
	schema, empty := planPartitions(t, h, "SELECT id FROM it_partition_api", 4)
	r, err = worker.conn.ReadPartition(h.ctx, empty.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if !r.Schema().Equal(schema) || r.Next() || r.Err() != nil {
		t.Fatal("empty partition lost schema or returned rows")
	}
}

func TestPartitionStreamingLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_stream"
	h.setupStreamTable(table, streamRows)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	_, parts := planPartitions(t, h, "SELECT id FROM "+table, 1)
	worker := newSQLHarness(t)
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionIntStreamBatchRows: "1000",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.conn.Close(h.ctx)
	worker.conn, err = db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	r, err := worker.conn.ReadPartition(ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next() || r.RecordBatch().NumRows() > 1000 {
		t.Fatal("expected bounded Arrow batch")
	}
	id := cursorOf(r)
	if id == 0 {
		t.Fatal("expected live cursor")
	}
	cancel()
	if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
		t.Fatalf("cancel: %v", r.Err())
	}
	r.Release()
	if h.delCursor(h.rawClient(), table, id) {
		t.Fatal("cancel leaked cursor")
	}
	r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	id = cursorOf(r)
	r.Release()
	if h.delCursor(h.rawClient(), table, id) {
		t.Fatal("release leaked cursor")
	}
	r, err = worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	worker.conn.Close(h.ctx)
	if r.Next() || !isStatus(r.Err(), adbc.StatusInvalidState) {
		t.Fatalf("closed: %v", r.Err())
	}
	r.Release()
}

func TestPartitionReaderPermissions(t *testing.T) {
	a := newACLHarness(t)
	a.exec("DROP TABLE IF EXISTS it_partition_acl")
	a.exec("CREATE TABLE it_partition_acl (id BIGINT)")
	a.exec("INSERT INTO it_partition_acl VALUES (1),(2)")
	t.Cleanup(func() { a.exec("DROP TABLE IF EXISTS it_partition_acl") })
	_, parts := planPartitions(t, a.sqlHarness, "SELECT id FROM it_partition_acl", 1)
	d, _ := decodePartition(parts.PartitionIDs[0])
	d.ScanCap = 10 // named ACL readers deliberately do not probe CONFIG
	parts.PartitionIDs[0], _ = json.Marshal(d)
	const user = "it_acl_partitions"
	a.user(user, append(a.readOnlyCommands(), "+ft.info", "%R~adbc:*", "%R~public:*")...)
	worker := a.connect(user, nil)
	hidden := d
	hidden.ScanCap = 0
	unknown, _ := json.Marshal(hidden)
	if r, err := worker.conn.ReadPartition(a.ctx, unknown); !isStatus(err, adbc.StatusNotImplemented) {
		if r != nil {
			r.Release()
		}
		t.Fatalf("hidden cap must require explicit contract: %v", err)
	}
	r, err := worker.conn.ReadPartition(a.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err := readInts(r)
	r.Release()
	slices.Sort(vals)
	if err != nil || !slices.Equal(vals, []int64{1, 2}) {
		t.Fatalf("read-only reader: %v %v", vals, err)
	}
	if err := a.eachNode(func(n *goredis.Client) error { return n.Do(a.ctx, "ACL", "SETUSER", user, "-ft.info").Err() }); err != nil {
		t.Fatal(err)
	}
	if r, err := worker.conn.ReadPartition(a.ctx, parts.PartitionIDs[0]); !isStatus(err, adbc.StatusUnauthorized) {
		if r != nil {
			r.Release()
		}
		t.Fatalf("missing readiness permission must fail explicitly: %v", err)
	}
	if err := a.eachNode(func(n *goredis.Client) error { return n.Do(a.ctx, "ACL", "SETUSER", user, "+ft.info").Err() }); err != nil {
		t.Fatal(err)
	}
	if err := a.eachNode(func(n *goredis.Client) error {
		return n.Do(a.ctx, "ACL", "SETUSER", user, "-ft.aggregate").Err()
	}); err != nil {
		t.Fatal(err)
	}
	if r, err := worker.conn.ReadPartition(a.ctx, parts.PartitionIDs[0]); err == nil {
		r.Release()
		t.Fatal("descriptor bypassed the reading connection's denied FT.AGGREGATE permission")
	}
}

func TestPartitionLimitsAndScope(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_partition_limits")
	h.exec("CREATE TABLE it_partition_limits (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_partition_limits") })
	schema, empty := planPartitions(t, h, "SELECT id FROM it_partition_limits", 1024)
	if empty.NumPartitions != 1 || schema.NumFields() != 1 {
		t.Fatal("new empty table contract")
	}
	h.exec("INSERT INTO it_partition_limits VALUES (1)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if cap := os.Getenv("REDIS_PARTITION_SCAN_CAP"); cap != "" {
		if err := st.SetOption(h.ctx, OptionIntPartitionScanCap, cap); err != nil {
			t.Fatal(err)
		}
	}
	raw := h.rawClient()
	if err := raw.Set(h.ctx, seqKey(defaultSchema, "it_partition_limits"), exactDoubleLimit-1, 0).Err(); err != nil {
		t.Fatal(err)
	}
	_, parts := planPartitions(t, h, "SELECT id FROM it_partition_limits", 1024)
	var previous int64
	for _, part := range parts.PartitionIDs {
		d, err := decodePartition(part)
		if err != nil {
			t.Fatal(err)
		}
		if d.Lo != previous+1 || d.Hi < d.Lo {
			t.Fatalf("broken range: %+v", d)
		}
		previous = d.Hi
	}
	if previous != exactDoubleLimit-1 {
		t.Fatal(previous)
	}
	if err := raw.Set(h.ctx, seqKey(defaultSchema, "it_partition_limits"), exactDoubleLimit, 0).Err(); err != nil {
		t.Fatal(err)
	}
	st.SetSqlQuery(h.ctx, "SELECT id FROM it_partition_limits")
	if _, _, _, err := st.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	h.exec("CREATE TEMP TABLE it_partition_temp (id BIGINT)")
	st.SetSqlQuery(h.ctx, "SELECT * FROM it_partition_temp")
	if _, _, _, err := st.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}

}

func TestPartitionTimeZone(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_partition_zone")
	h.exec("CREATE TABLE it_partition_zone (id BIGINT, ts TIMESTAMP WITH TIME ZONE)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_partition_zone") })
	h.exec("INSERT INTO it_partition_zone VALUES (1,'2024-01-01 04:00:00+00'),(2,'2024-01-01 10:00:00+00')")
	h.exec("SET TimeZone='America/Los_Angeles'")
	q := "SELECT id FROM it_partition_zone WHERE ts >= '2024-01-01 00:00:00'"
	_, parts := planPartitions(t, h, q, 1)
	worker := newSQLHarness(t)
	worker.exec("SET TimeZone='UTC'")
	r, err := worker.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	vals, _, err := readInts(r)
	r.Release()
	if err != nil || !slices.Equal(vals, []int64{2}) {
		t.Fatalf("captured time zone: %v %v", vals, err)
	}
}

func TestPartitionCapRanges(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_cap_ranges"
	h.setupStreamTable(table, 2500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	// Include gaps, residual filtering, direct keys and empty ranges. Setting the
	// descriptor's lower bound simulates a hidden cap without CONFIG privileges.
	h.exec("DELETE FROM " + table + " WHERE id >= 100 AND id < 200")
	for _, q := range []string{
		"SELECT id FROM " + table,
		"SELECT id FROM " + table + " WHERE __rowid IN (1,100,201,2500)",
		"SELECT id FROM " + table + " WHERE id % 3 = 0",
		"SELECT id FROM " + table + " WHERE id > 99999",
	} {
		schema, parts := planPartitions(t, h, q, 1)
		d, _ := decodePartition(parts.PartitionIDs[0])
		d.ScanCap = 137
		data, _ := json.Marshal(d)
		r, err := h.conn.ReadPartition(h.ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		if !r.Schema().Equal(schema) {
			t.Fatal("schema changed")
		}
		actual, _, err := readInts(r)
		r.Release()
		if err != nil {
			t.Fatal(err)
		}
		r, _ = h.openStream(h.ctx, h.conn, q, nil, nil)
		expected, _, err := readInts(r)
		r.Release()
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(actual)
		slices.Sort(expected)
		if !slices.Equal(actual, expected) {
			t.Fatalf("%s: got %d rows want %d", q, len(actual), len(expected))
		}
	}
}

func TestPartitionIncrementalLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_incremental"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if cap := os.Getenv("REDIS_PARTITION_SCAN_CAP"); cap != "" {
		if err := st.SetOption(h.ctx, OptionIntPartitionScanCap, cap); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetOption(h.ctx, OptionStringIncremental, "true"); err != nil {
		t.Fatal(err)
	}
	for _, insert := range []bool{false, true} {
		if insert {
			h.exec("INSERT INTO " + table + " VALUES (1),(2),(3)")
		}
		st.SetSqlQuery(h.ctx, "SELECT id FROM "+table)
		schema, parts, _, err := st.ExecutePartitions(h.ctx)
		if err != nil || parts.NumPartitions == 0 {
			t.Fatalf("initial: %v", err)
		}
		for range 3 {
			terminal, empty, _, err := st.ExecutePartitions(h.ctx)
			if err != nil || empty.NumPartitions != 0 || !terminal.Equal(schema) {
				t.Fatalf("terminal: %+v %v", empty, err)
			}
		}
		for _, part := range parts.PartitionIDs {
			r, err := h.conn.ReadPartition(h.ctx, part)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = readInts(r)
			r.Release()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	st.SetSqlQuery(h.ctx, "SELECT id FROM "+table+" ORDER BY id")
	for range 2 {
		if _, _, _, err := st.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusNotImplemented) {
			t.Fatal(err)
		}
	}
	st.SetSqlQuery(h.ctx, "SELECT id FROM "+table)
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, _, _, err := st.ExecutePartitions(ctx); !isStatus(err, adbc.StatusCancelled) {
		t.Fatal(err)
	}
	if _, _, _, err := st.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusCancelled) {
		t.Fatal(err)
	}
	st.SetOption(h.ctx, OptionStringIncremental, "false")
	for range 2 {
		if _, p, _, err := st.ExecutePartitions(h.ctx); err != nil || p.NumPartitions == 0 {
			t.Fatalf("disabled: %v", err)
		}
	}
}

func TestPartitionRuntimeCapDiscovery(t *testing.T) {
	if os.Getenv("REDIS_PARTITION_CAP_TEST") != "1" {
		t.Skip("requires dedicated Redis with permission to change Search configuration")
	}
	h := newSQLHarness(t)
	raw := h.rawClient()
	const table = "it_partition_runtime_cap"
	h.setupStreamTable(table, 500)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	// CONFIG has no key: ClusterClient may route each call to a different node.
	// Save/restore every primary so this opt-in test cannot poison later tests.
	eachPrimary := func(fn func(goredis.UniversalClient) error) error {
		if cc, ok := raw.(*goredis.ClusterClient); ok {
			return cc.ForEachMaster(h.ctx, func(_ context.Context, node *goredis.Client) error { return fn(node) })
		}
		return fn(raw)
	}
	if err := eachPrimary(func(node goredis.UniversalClient) error {
		original, err := node.ConfigGet(h.ctx, "search-max-aggregate-results").Result()
		if err != nil {
			return err
		}
		value := original["search-max-aggregate-results"]
		if value == "" {
			return fmt.Errorf("Search aggregate cap is unavailable")
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := node.ConfigSet(ctx, "search-max-aggregate-results", value).Err(); err != nil {
				t.Errorf("restore Search aggregate cap: %v", err)
			}
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, parts := planPartitions(t, h, "SELECT id FROM "+table, 1)
	for _, cap := range []string{"137", "11", "3", "137"} {
		if err := eachPrimary(func(node goredis.UniversalClient) error {
			return node.ConfigSet(h.ctx, "search-max-aggregate-results", cap).Err()
		}); err != nil {
			t.Fatal(err)
		}
		r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
		if err != nil {
			t.Fatal(err)
		}
		vals, _, err := readInts(r)
		r.Release()
		if err != nil || len(vals) != 500 {
			t.Fatalf("cap %s: %d rows %v", cap, len(vals), err)
		}
		slices.Sort(vals)
		for i, v := range vals {
			if v != int64(i) {
				t.Fatalf("cap %s: row %d = %d", cap, i, v)
			}
		}
	}
}

func TestPartitionLiveReadConsistency(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_partition_live_consistency"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (1),(2),(3)")
	_, parts := planPartitions(t, h, "SELECT id FROM "+table, 1)
	read := func() []int64 {
		t.Helper()
		r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
		if err != nil {
			t.Fatal(err)
		}
		values, _, err := readInts(r)
		r.Release()
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(values)
		return values
	}
	if values := read(); !slices.Equal(values, []int64{1, 2, 3}) {
		t.Fatal(values)
	}
	h.exec("UPDATE " + table + " SET id=20 WHERE id=2")
	h.exec("DELETE FROM " + table + " WHERE id=3")
	h.exec("INSERT INTO " + table + " VALUES (4)")
	if values := read(); !slices.Equal(values, []int64{1, 20}) {
		t.Fatalf("live retry/high watermark: %v", values)
	}
}
