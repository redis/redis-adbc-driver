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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Redis OSS does not expose the Cloud MAX_AGGREGATE_GROUPS setting. This
// proxy hook executes real pipelines, but returns Cloud's resource error
// whenever their actual tuple cardinality exceeds a small configured cap.
// A late-page variant verifies that abandoned attempts are never counted.
type aggregateCapHook struct {
	cap      int
	failures atomic.Int64
	late     bool
	cursor   atomic.Int64
	cleanup  goredis.UniversalClient
}

func (h *aggregateCapHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *aggregateCapHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *aggregateCapHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "ft.cursor" && len(args) > 3 && strings.EqualFold(fmt.Sprint(args[1]), "READ") && args[3] == h.cursor.Load() && h.cursor.Load() != 0 {
			h.failures.Add(1)
			return fmt.Errorf("SEARCH_LIMIT_OVER Aggregate GROUPBY exceeded MAX_AGGREGATE_GROUPS limit of %d groups", h.cap)
		}
		err := next(ctx, cmd)
		if err != nil || cmd.Name() != "ft.aggregate" {
			return err
		}
		grouped := false
		for _, arg := range args {
			grouped = grouped || arg == "GROUPBY"
		}
		if !grouped {
			return nil
		}
		rows, cursor, err := parseCursorReply(cmd.(interface {
			Val() any
			SetVal(any)
		}).Val())
		if err != nil {
			return err
		}
		if len(rows) <= h.cap {
			return nil
		}
		if h.late {
			// The first page is accepted; inject the resource failure on READ.
			// The synthetic ID is deleted by the normal cursor close path.
			if cursor == 0 {
				cursor = 987654321
			}
			h.cursor.Store(cursor)
			cmd.(interface {
				Val() any
				SetVal(any)
			}).SetVal([]any{cmd.(interface {
				Val() any
				SetVal(any)
			}).Val().([]any)[0], cursor})
			return nil
		}
		if cursor != 0 {
			_ = h.cleanup.Do(ctx, "FT.CURSOR", "DEL", args[1], cursor).Err()
		}
		h.failures.Add(1)
		return fmt.Errorf("SEARCH_LIMIT_OVER Aggregate GROUPBY exceeded MAX_AGGREGATE_GROUPS limit of %d groups", h.cap)
	}
}

func aggregateGroupsText(groups []aggGroup, plan *selectPlan) []string {
	var out []string
	for _, g := range groups {
		var fields []string
		for _, x := range plan.sel.GroupBy {
			fields = append(fields, partitionValueString(g.rep[x.(*ColumnRef).Name]))
		}
		for _, f := range plan.aggregateCalls() {
			fields = append(fields, partitionValueString(g.results[f]))
		}
		out = append(out, strings.Join(fields, "|"))
	}
	sort.Strings(out)
	return out
}

func TestSQLPartitionedAggregateExact(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_partitioned")
	h.exec("CREATE TABLE it_partitioned (region INTEGER, user_id BIGINT, phone INTEGER, model VARCHAR, phrase VARCHAR, adv INTEGER, width INTEGER)")
	// Repeated tuple/user across row-ID ranges, NULL-only groups, normalized
	// tags, NUL tails, and integers that collide when converted to double.
	h.exec(`INSERT INTO it_partitioned VALUES
 (1,9007199254740992,1,' X ','a',2,100),
 (1,9007199254740993,1,'X','a',3,200),
 (1,9007199254740993,1,'X','a',3,200),
 (1,NULL,1,'','',0,NULL),
 (2,9223372036854775807,2,'nul\x00a','b',4,300),
 (2,-9223372036854775808,2,'nul\x00b','b',5,NULL),
 (NULL,NULL,NULL,NULL,NULL,NULL,NULL),
 (3,NULL,3,'null-only','null-only',7,NULL)`)
	queries := []string{
		"SELECT COUNT(DISTINCT user_id) FROM it_partitioned",
		"SELECT adv,COUNT(*) FROM it_partitioned WHERE adv<>0 GROUP BY adv",
		"SELECT region,COUNT(DISTINCT user_id) FROM it_partitioned GROUP BY region",
		"SELECT region,SUM(adv),COUNT(*),AVG(width),COUNT(DISTINCT user_id) FROM it_partitioned GROUP BY region",
		"SELECT model,COUNT(DISTINCT user_id) FROM it_partitioned WHERE model<>'' GROUP BY model",
		"SELECT phone,model,COUNT(DISTINCT user_id) FROM it_partitioned WHERE model<>'' GROUP BY phone,model",
		"SELECT phrase,COUNT(*) FROM it_partitioned WHERE phrase<>'' GROUP BY phrase",
		"SELECT phrase,COUNT(DISTINCT user_id) FROM it_partitioned WHERE phrase<>'' GROUP BY phrase",
		"SELECT adv,phrase,COUNT(*) FROM it_partitioned WHERE phrase<>'' GROUP BY adv,phrase",
		"SELECT user_id,COUNT(*) FROM it_partitioned GROUP BY user_id",
		"SELECT user_id,phrase,COUNT(*) FROM it_partitioned GROUP BY user_id,phrase",
		"SELECT model,COUNT(DISTINCT user_id,phrase) FROM it_partitioned GROUP BY model",
		"SELECT region,COUNT(user_id),MIN(user_id),MAX(user_id) FROM it_partitioned GROUP BY region",
	}
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			e, plan, wp := distinctTestPlan(t, h, sql)
			p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
			if !ok {
				t.Fatal("unsupported plan")
			}
			// Force two ranges so equality must merge across partition boundaries.
			spill := tupleSpill{dir: t.TempDir(), prefix: "fixture"}
			for _, r := range [][2]int64{{1, 2}, {3, 8}} {
				if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, r[0], r[1], false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
					t.Fatal(err)
				}
			}
			got, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill)
			if err != nil {
				t.Fatal(err)
			}
			want, err := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if a, b := aggregateGroupsText(got, plan), aggregateGroupsText(want, plan); !reflect.DeepEqual(a, b) {
				t.Fatalf("got %v want %v", a, b)
			}
		})
	}
}

func TestSQLPartitionedGroupCapRecovery(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_partition_cap")
	h.exec("CREATE TABLE it_partition_cap (g INTEGER, v BIGINT)")
	var values []string
	for i := 0; i < 64; i++ {
		values = append(values, fmt.Sprintf("(%d,%d)", i%3, 9007199254740992+int64(i%31)))
	}
	h.exec("INSERT INTO it_partition_cap VALUES " + strings.Join(values, ","))
	for _, late := range []bool{false, true} {
		for _, sql := range []string{
			"SELECT COUNT(DISTINCT v) FROM it_partition_cap",
			"SELECT g,COUNT(DISTINCT v) FROM it_partition_cap GROUP BY g",
			"SELECT g,COUNT(*) FROM it_partition_cap GROUP BY g",
		} {
			e, plan, wp := distinctTestPlan(t, h, sql)
			client := h.rawClient()
			hook := &aggregateCapHook{cap: 2, late: late, cleanup: h.rawClient()}
			if cc, ok := client.(*goredis.ClusterClient); ok {
				node, err := cc.MasterForKey(h.ctx, plan.meta.index())
				if err != nil {
					t.Fatal(err)
				}
				node.AddHook(hook)
			} else {
				client.AddHook(hook)
			}
			e.store = &store{client: client}
			// End-to-end retry from the failed global aggregate, below the normal
			// large-table threshold, must not return the server resource error.
			got, err := e.selectAggregate(h.ctx, plan, nil)
			if err != nil {
				t.Fatalf("late=%v %s: %v", late, sql, err)
			}
			if len(got) == 0 || hook.failures.Load() == 0 {
				t.Fatalf("recovery not exercised: %v failures=%d", got, hook.failures.Load())
			}
			clean, _, _ := distinctTestPlan(t, h, sql)
			want, err := clean.runSelect(h.ctx, plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			stringify := func(rows [][]Value) []string {
				var out []string
				for _, row := range rows {
					var v []string
					for _, x := range row {
						v = append(v, partitionValueString(x))
					}
					out = append(out, strings.Join(v, "|"))
				}
				sort.Strings(out)
				return out
			}
			if a, b := stringify(got), stringify(want); !reflect.DeepEqual(a, b) {
				t.Fatalf("got %v want %v", a, b)
			}
			// A finite range must split repeatedly rather than immediately
			// falling back to scanning the whole input.
			shape, supported := tupleAggregateShape(plan, plan.aggregateCalls())
			if !supported {
				t.Fatal("shape rejected")
			}
			spill := tupleSpill{dir: t.TempDir(), prefix: "cap"}
			beforeFailures := hook.failures.Load()
			if err := e.aggregateTuplePartition(h.ctx, plan, shape, wp, nil, 1, 64, false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
				t.Fatal(err)
			}
			reduced, err := e.reduceTupleRuns(h.ctx, plan, shape, plan.aggregateCalls(), nil, &spill)
			if err != nil {
				t.Fatal(err)
			}
			serial, err := clean.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if a, b := aggregateGroupsText(reduced, plan), aggregateGroupsText(serial, plan); !reflect.DeepEqual(a, b) {
				t.Fatalf("split got %v want %v", a, b)
			}
			if hook.failures.Load() <= beforeFailures+1 {
				t.Fatal("finite range did not split")
			}
		}
	}
}

func partitionValueString(v Value) string {
	if v.Null {
		return "NULL"
	}
	return encodeStored(v)
}

func TestSQLPartitionedWorkersTopKAndCleanup(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_partition_workers")
	h.exec("CREATE TABLE it_partition_workers (g INTEGER, v BIGINT)")
	h.exec("INSERT INTO it_partition_workers VALUES (1,9007199254740993),(1,9007199254740993),(1,NULL),(2,7),(2,8),(3,NULL)")
	e, plan, wp := distinctTestPlan(t, h, "SELECT g,COUNT(DISTINCT v) AS n,COUNT(*) FROM it_partition_workers GROUP BY g ORDER BY n DESC,g LIMIT 1 OFFSET 1")
	// Move duplicates into another partition and leave a sparse sequence.
	for _, id := range []int{2, 5} {
		if err := e.store.client.HSet(h.ctx, plan.meta.prefix()+fmt.Sprint(id), rowIDField, aggregatePartitionRows+int64(id)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), aggregatePartitionRows*2, 0).Err(); err != nil {
		t.Fatal(err)
	}
	e.queryWorkers = 2
	e.workerBudget = &workerBudget{configured: 2}
	var grant atomic.Int64
	e.workerGrant = &grant
	e.aggregateMetrics = &partitionMetrics{}
	got, ok, err := e.partitionedAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil, false)
	if err != nil || !ok {
		t.Fatalf("handled=%v err=%v", ok, err)
	}
	if len(got) != 2 {
		t.Fatalf("top offset+limit retained %d groups", len(got))
	}
	if grant.Load() != 2 || e.workerBudget.active != 0 {
		t.Fatalf("grant=%d active=%d", grant.Load(), e.workerBudget.active)
	}
	if e.aggregateMetrics.rows.Load() != 6 || e.aggregateMetrics.spillBytes.Load() == 0 {
		t.Fatalf("metrics=%s", e.aggregateMetrics.json())
	}
	rows, err := e.selectAggregate(h.ctx, plan, nil)
	if err != nil || len(rows) != 1 || rows[0][0].I != 1 || rows[0][1].I != 1 || rows[0][2].I != 3 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	// Cancellation after opening a real cursor must release the shared grant,
	// close that cursor, and remove the query-local spill directory.
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "redis-adbc-aggregate-*"))
	ctx, cancel := context.WithCancel(h.ctx)
	client := h.rawClient()
	hook := &partitionCancelHook{cancel: cancel}
	if cc, ok := client.(*goredis.ClusterClient); ok {
		node, err := cc.MasterForKey(h.ctx, plan.meta.index())
		if err != nil {
			t.Fatal(err)
		}
		node.AddHook(hook)
	} else {
		client.AddHook(hook)
	}
	e.store = &store{client: client}
	if _, _, err := e.partitionedAggregate(ctx, plan, wp, plan.aggregateCalls(), nil, false); err == nil {
		t.Fatal("cancellation succeeded")
	}
	if e.workerBudget.active != 0 {
		t.Fatal("worker grant leaked")
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "redis-adbc-aggregate-*"))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("spill directory leak: before=%v after=%v", before, after)
	}
}

// Unlike the serial distinct fixture hook, this hook can be called by two
// partition workers at once.
type partitionCancelHook struct {
	once   sync.Once
	cancel context.CancelFunc
}

func (h *partitionCancelHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *partitionCancelHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *partitionCancelHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && cmd.Name() == "ft.aggregate" {
			h.once.Do(h.cancel)
		}
		return err
	}
}

func TestSQLClickBenchFollowupShapes(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_cb_followup")
	h.exec("CREATE TABLE it_cb_followup (uid BIGINT, phrase VARCHAR, url VARCHAR, title VARCHAR, counter INTEGER, ts TIMESTAMP)")
	h.exec(`INSERT INTO it_cb_followup VALUES
 (9007199254740993,'z','google-a','Google',1,'2013-07-14 00:01:01'),
 (9007199254740993,'z','google-b','Google',1,'2013-07-14 00:01:59'),
 (9007199254740992,'a','x.google.y','Google',2,'2013-07-14 00:02:01'),
 (9007199254740993,'z','google-a','Google',1,'2013-07-14 00:01:01'),
 (7,'','google','Google',2,NULL),(8,NULL,NULL,NULL,3,NULL),
 (9,'a','GOOGLE','google',3,'1969-12-31 23:59:59'),
 (10,'a','googlé','Google',4,'2013-07-14 00:01:01')`)
	queries := []string{
		"SELECT uid,phrase,COUNT(*) FROM it_cb_followup GROUP BY uid,phrase LIMIT 2",
		"SELECT uid,extract(minute FROM ts) AS m,phrase,COUNT(*) FROM it_cb_followup GROUP BY uid,m,phrase ORDER BY COUNT(*) DESC LIMIT 2",
		"SELECT phrase,MIN(url),COUNT(*) AS c FROM it_cb_followup WHERE url LIKE '%google%' AND phrase<>'' GROUP BY phrase ORDER BY c DESC LIMIT 2",
		"SELECT phrase,MIN(url),MIN(title),COUNT(*) AS c,COUNT(DISTINCT uid) FROM it_cb_followup WHERE title LIKE '%Google%' AND url NOT LIKE '%.google.%' AND phrase<>'' GROUP BY phrase ORDER BY c DESC LIMIT 2",
		"SELECT counter,AVG(length(url)) AS l,COUNT(*) AS c FROM it_cb_followup WHERE url<>'' GROUP BY counter HAVING COUNT(*)>1 ORDER BY l DESC LIMIT 1",
	}
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			e, plan, wp := distinctTestPlan(t, h, sql)
			p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
			if !ok {
				t.Fatal("shape rejected")
			}
			spill := tupleSpill{dir: t.TempDir(), prefix: "followup"}
			for _, r := range [][2]int64{{1, 2}, {3, 8}} {
				if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, r[0], r[1], false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
					t.Fatal(err)
				}
				if err := spill.flush(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			groups, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill)
			if err != nil {
				t.Fatal(err)
			}
			if len(groups) > 2 {
				t.Fatalf("unbounded groups: %d", len(groups))
			}
			// Use public projection to compare exact expressions and HAVING; sorted
			// ties may differ, so use a query with complete ordering below as well.
			if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), aggregatePartitionRows*2, 0).Err(); err != nil {
				t.Fatal(err)
			}
			e.queryWorkers = 2
			e.pushdown = PushdownNone
			actual, err := e.selectAggregate(h.ctx, plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), 8, 0).Err(); err != nil {
				t.Fatal(err)
			}
			full := *plan
			sel := *plan.sel
			sel.Limit = nil
			full.sel = &sel
			all, err := e.selectAggregate(h.ctx, &full, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range actual {
				found := false
				for _, other := range all {
					if reflect.DeepEqual(row, other) {
						found = true
					}
				}
				if !found {
					t.Fatalf("inexact group: %v; expected one of %v", row, all)
				}
			}
			if len(actual) == 0 {
				t.Fatal("no groups")
			}
			if strings.Contains(sql, "HAVING") && (len(actual) != 1 || actual[0][0].I != 1 || actual[0][2].I != 3) {
				t.Fatalf("HAVING/top-K: %v", actual)
			}
		})
	}
	e, plan, wp := distinctTestPlan(t, h, "SELECT COUNT(*) FROM it_cb_followup WHERE url LIKE '%google%'")
	if !parallelPredicate(wp.residual) {
		t.Fatal("LIKE excluded")
	}
	if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), 4096, 0).Err(); err != nil {
		t.Fatal(err)
	}
	e.queryWorkers = 2
	got, ok, err := e.parallelAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
	if err != nil || !ok || got[0].results[plan.aggregateCalls()[0]].I != 5 {
		t.Fatalf("groups=%v handled=%v err=%v", got, ok, err)
	}
}

func TestSQLStreamingStringTopK(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_string_topk")
	h.exec("CREATE TABLE it_string_topk (phrase VARCHAR, unused INTEGER)")
	h.exec(`INSERT INTO it_string_topk VALUES ('z',1),('',2),('a',3),('A',4),('a',5),('a\x00b',6),(NULL,7),(' b',8)`)
	e, plan, _ := distinctTestPlan(t, h, "SELECT phrase FROM it_string_topk WHERE phrase<>'' ORDER BY phrase LIMIT 3 OFFSET 1")
	sc, err := e.prepareScan(h.ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc.indexSort = false
	sc.pushLimit = false
	sc.req.sortBy = nil
	sc.req.limit = nil
	sc.req.late = nil
	sc.req.ties = nil
	got, handled, err := e.selectTopK(h.ctx, plan, sc, nil)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	want, err := e.selectScanned(h.ctx, plan, sc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
