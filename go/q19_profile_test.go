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
	goredis "github.com/redis/go-redis/v9"
	"net"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in, disposable 105-column fixture. This compares identical SQL/results
// without touching the cloud runner or treating pilot times as full-scale proof.
func TestQ19Profile(t *testing.T) {
	if os.Getenv("REDIS_Q19_PROFILE") == "" {
		t.Skip("set REDIS_Q19_PROFILE=1")
	}
	n := 20000
	if value := os.Getenv("REDIS_Q19_PROFILE_ROWS"); value != "" {
		var err error
		n, err = strconv.Atoi(value)
		if err != nil || n < 1 {
			t.Fatal("invalid profile rows")
		}
	}
	h := newSQLHarness(t)
	h.dropTables("it_q19_profile")
	cols := []string{"uid BIGINT", "ts TIMESTAMP", "phrase VARCHAR"}
	for i := 3; i < 105; i++ {
		cols = append(cols, fmt.Sprintf("c%d VARCHAR", i))
	}
	h.exec("CREATE TABLE it_q19_profile (" + strings.Join(cols, ",") + ")")
	client := h.rawClient()
	defer client.Close()
	meta, err := (&store{client: client}).getTable(h.ctx, defaultSchema, "it_q19_profile")
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < n; start += pipelineChunk {
		pipe := client.Pipeline()
		for i := start; i < min(start+pipelineChunk, n); i++ {
			fields := []any{rowIDField, i + 1, "uid", strconv.FormatInt(9007199254740992+int64(i%1000), 10), "ts", encodeStored(Value{T: meta.Columns[1].Type, I: 1373760000000000 + int64(i%3600)*1000000}), "phrase", fmt.Sprintf("phrase-%d", i%17)}
			for j := 3; j < 105; j++ {
				fields = append(fields, fmt.Sprintf("c%d", j), "unrelated-payload-0123456789-abcdefghijklmn")
			}
			pipe.HSet(h.ctx, meta.prefix()+strconv.Itoa(i+1), fields...)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "Q19 fixture indexed", func() bool {
		count, err := (&store{client: client}).countMatches(h.ctx, meta.index(), "*")
		return err == nil && count == int64(n)
	})
	e, plan, wp := distinctTestPlan(t, h, "SELECT uid,extract(minute FROM ts) AS m,phrase,COUNT(*) AS c FROM it_q19_profile GROUP BY uid,m,phrase ORDER BY c DESC LIMIT 10")
	opts, err := goredis.ParseURL(os.Getenv("REDIS_URI"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Protocol = 3
	var wire atomic.Int64
	opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return scanProfileConn{Conn: conn, bytes: &wire}, nil
	}
	profileClient := goredis.NewClient(opts)
	defer profileClient.Close()
	if err := profileClient.Ping(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	e.store = &store{client: profileClient}
	var expected []string
	for _, strategy := range []string{"server_source_tuples", "narrow_rows"} {
		for trial := 1; trial <= 3; trial++ {
			p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
			if !ok {
				t.Fatal("shape rejected")
			}
			p.server = strategy == "server_source_tuples"
			p.countGroups = false // baseline key; candidate uses final SQL group key
			if strategy == "narrow_rows" {
				p.countGroups = true
			}
			e.aggregateMetrics = &partitionMetrics{}
			spill := tupleSpill{dir: t.TempDir(), prefix: "q19", metrics: e.aggregateMetrics}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			wire.Store(0)
			started := time.Now()
			for lo := int64(1); lo <= int64(n); lo += aggregatePartitionRows {
				if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, lo, min(int64(n), lo+aggregatePartitionRows-1), false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
					t.Fatal(err)
				}
			}
			partitionSeconds := time.Since(started).Seconds()
			merged := time.Now()
			groups, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill)
			if err != nil {
				t.Fatal(err)
			}
			mergeSeconds := time.Since(merged).Seconds()
			runtime.ReadMemStats(&after)
			var got []string
			env := e.newEnv(h.ctx, plan.meta.types(), nil)
			for _, g := range groups {
				env.row, env.aggs = g.rep, g.results
				values, err := evalItems(env, plan.items)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, rowKey(values))
			}
			// Both merges break COUNT ties by the same complete SQL group-key order.
			sort.Strings(got)
			if expected == nil {
				expected = got
			} else if !reflect.DeepEqual(got, expected) {
				t.Fatalf("strategy changed results")
			}
			result, _ := json.Marshal(map[string]any{"strategy": strategy, "trial": trial, "rows": n, "partition_seconds": partitionSeconds, "merge_seconds": mergeSeconds, "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "spill_bytes": e.aggregateMetrics.spillBytes.Load(), "tuples": e.aggregateMetrics.tuples.Load(), "logical_value_bytes": e.aggregateMetrics.valueBytes.Load(), "redis_read_bytes": wire.Load()})
			t.Logf("PROFILE %s", result)
		}
	}
}
