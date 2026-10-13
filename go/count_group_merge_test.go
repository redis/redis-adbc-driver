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
	"reflect"
	"sort"
	"testing"
)

func TestSQLCountExpressionSpillGroups(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_count_minute")
	h.exec("CREATE TABLE it_count_minute (uid BIGINT, ts TIMESTAMP, phrase VARCHAR)")
	h.exec(`INSERT INTO it_count_minute VALUES
 (9007199254740993,'2013-07-14 00:01:01','same'),
 (9007199254740993,'2013-07-14 00:01:59','same'),
 (9007199254740993,'2013-07-14 01:01:01','same'),
 (9007199254740993,'2013-07-14 01:01:01','same'),
 (9007199254740992,'1969-12-31 23:59:00','negative'),
 (9007199254740992,'1969-12-31 23:59:59.999999','negative'),
 (7,NULL,NULL),(7,NULL,NULL),(7,NULL,NULL),
 (8,'2013-07-14 00:01:01','Same'),(9,'2013-07-14 00:01:01',' same')`)
	for _, sql := range []string{
		"SELECT uid,extract(minute FROM ts) AS m,phrase,COUNT(*) AS c FROM it_count_minute GROUP BY uid,m,phrase ORDER BY c DESC LIMIT 3",
		"SELECT uid,extract(minute FROM ts) AS m,phrase,COUNT(*) AS c,COUNT(*) AS c2 FROM it_count_minute GROUP BY uid,m,phrase HAVING COUNT(*)>1 ORDER BY c DESC LIMIT 2 OFFSET 1",
		"SELECT uid,extract(minute FROM ts) AS m,phrase,COUNT(*) AS c FROM it_count_minute GROUP BY uid,m,phrase LIMIT 3",
		"SELECT uid,extract(minute FROM ts) AS m,phrase,COUNT(*) AS c FROM it_count_minute GROUP BY uid,m,phrase ORDER BY c DESC,uid LIMIT 3",
	} {
		t.Run(sql, func(t *testing.T) {
			e, plan, wp := distinctTestPlan(t, h, sql)
			p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
			if !ok || !p.countGroups || p.server {
				t.Fatal("minute COUNT did not select narrow count groups")
			}
			spill := tupleSpill{dir: t.TempDir(), prefix: "count"}
			for id := int64(1); id <= 11; id++ {
				if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, id, id, false, func(row spillTuple) error { return spill.add(h.ctx, row) }); err != nil {
					t.Fatal(err)
				}
				// Force timestamps from the same SQL minute into different sorted files.
				if err := spill.flush(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			groups, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &spill)
			if err != nil {
				t.Fatal(err)
			}
			env := e.newEnv(h.ctx, plan.meta.types(), nil)
			var actual []string
			for _, g := range groups {
				env.row, env.aggs = g.rep, g.results
				row, err := evalItems(env, plan.items)
				if err != nil {
					t.Fatal(err)
				}
				actual = append(actual, rowKey(row))
			}
			// Merge retains OFFSET+LIMIT groups; public projection applies OFFSET.
			expected, err := e.selectAggregate(h.ctx, plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			if plan.sel.Offset == nil {
				var want []string
				for _, row := range expected {
					want = append(want, rowKey(row))
				}
				if len(plan.order) > 0 {
					sort.Strings(actual)
					sort.Strings(want)
					if !reflect.DeepEqual(actual, want) {
						t.Fatalf("got %v want %v", actual, want)
					}
				}
			}
			full := *plan
			sel := *plan.sel
			sel.Limit = nil
			sel.Offset = nil
			full.sel = &sel
			all, err := e.selectAggregate(h.ctx, &full, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range actual {
				found := false
				for _, row := range all {
					found = found || rowKey(row) == key
				}
				if !found {
					t.Fatal("returned incomplete or wrong group")
				}
			}
		})
	}
	// Different timestamps within a minute must coalesce only for COUNT(*).
	e, plan, _ := distinctTestPlan(t, h, "SELECT extract(minute FROM ts),COUNT(*),COUNT(uid) FROM it_count_minute GROUP BY extract(minute FROM ts)")
	p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
	if !ok || p.countGroups {
		t.Fatal("mixed COUNT inputs incorrectly coalesced")
	}
	_ = e
}
