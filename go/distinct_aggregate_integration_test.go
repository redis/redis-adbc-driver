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
	"reflect"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func distinctTestPlan(t *testing.T, h *sqlHarness, sql string) (*executor, *selectPlan, wherePlan) {
	t.Helper()
	e := &executor{store: &store{client: h.rawClient()}, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache(), now: time.Now().UTC()}
	parsed, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(h.ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	wp, err := e.planWhere(h.ctx, plan.sel.Where, plan.meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, plan, wp
}

func TestSQLExactCountDistinct(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_distinct_exact")
	h.exec("CREATE TABLE it_distinct_exact (g VARCHAR, v BIGINT, s VARCHAR, enabled INTEGER)")
	h.exec(`INSERT INTO it_distinct_exact VALUES
 ('a',9007199254740992,'x',1), ('a',9007199254740993,'x',1),
 ('a',9007199254740993,'x',0), ('a',NULL,'y',1),
 ('b',9223372036854775807,'',0), ('b',-9223372036854775808,NULL,0),
 (NULL,9007199254740993,'x',1), (NULL,9007199254740993,'x',1),
 ('empty',NULL,NULL,0)`)
	queries := []struct {
		sql  string
		rows []string
	}{
		{"SELECT COUNT(DISTINCT v), COUNT(DISTINCT s), COUNT(DISTINCT v,s) FROM it_distinct_exact", []string{"4|3|3"}},
		{"SELECT g,COUNT(DISTINCT v),COUNT(DISTINCT s) FROM it_distinct_exact GROUP BY g ORDER BY g", []string{"a|2|2", "b|2|1", "empty|0|0", "NULL|1|1"}},
		{"SELECT g,COUNT(DISTINCT v) FILTER (WHERE enabled=1) FROM it_distinct_exact GROUP BY g ORDER BY g", []string{"a|2", "b|0", "empty|0", "NULL|1"}},
		{"SELECT g,COUNT(DISTINCT v) FILTER (WHERE enabled=1),COUNT(DISTINCT s) FILTER (WHERE enabled=0) FROM it_distinct_exact GROUP BY g ORDER BY g", []string{"a|2|1", "b|0|1", "empty|0|0", "NULL|1|0"}},
		{"SELECT g,COUNT(DISTINCT v) FILTER (WHERE FALSE) FROM it_distinct_exact GROUP BY g ORDER BY g", []string{"a|0", "b|0", "empty|0", "NULL|0"}},
		{"SELECT COUNT(DISTINCT v) FROM it_distinct_exact WHERE enabled=99", []string{"0"}},
		{"SELECT g,COUNT(DISTINCT v) AS n FROM it_distinct_exact GROUP BY g HAVING COUNT(DISTINCT v)>1 ORDER BY n DESC,g LIMIT 1 OFFSET 1", []string{"b|2"}},
		{"SELECT COUNT(DISTINCT s) FROM it_distinct_exact WHERE enabled=1", []string{"2"}},
	}
	for _, q := range queries {
		h.expectRows(q.sql, q.rows...)
		e, plan, wp := distinctTestPlan(t, h, q.sql)
		if _, ok, err := e.indexCountDistinct(h.ctx, plan, wp, plan.aggregateCalls(), nil); err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", q.sql, ok, err)
		}
	}
	// Normalization, empty strings, and NUL bytes must never collapse values.
	h.exec("INSERT INTO it_distinct_exact VALUES ('nul\x00a',1,'ab\x00x',1),('nul\x00b',1,'ab\x00y',1),('nul\x00a',1,'ab',1),('a',1,' X ',1),('a',1,'X',1),('nul\x00a',1,'\x00lead',1),('nul\x00a',1,'ab\x00',1)")
	h.expectRows("SELECT COUNT(DISTINCT g),COUNT(DISTINCT s) FROM it_distinct_exact", "5|10")
	sql := "SELECT g,COUNT(DISTINCT s) FROM it_distinct_exact GROUP BY g"
	e, plan, wp := distinctTestPlan(t, h, sql)
	groups, ok, err := e.indexCountDistinct(h.ctx, plan, wp, plan.aggregateCalls(), nil)
	if err != nil || !ok {
		t.Fatalf("NUL grouping: %v %v", ok, err)
	}
	found := map[string]int64{}
	for _, g := range groups {
		found[g.rep["g"].S] = g.results[plan.aggregateCalls()[0]].I
	}
	if found["nul\x00a"] != 4 || found["nul\x00b"] != 1 {
		t.Fatalf("NUL groups: %v", found)
	}
	for _, version := range []int64{0, 80400} {
		e, p, w := distinctTestPlan(t, h, "SELECT COUNT(DISTINCT s) FROM it_distinct_exact")
		client := h.rawClient()
		if cc, ok := client.(*goredis.ClusterClient); ok {
			if err := cc.ForEachMaster(h.ctx, func(_ context.Context, node *goredis.Client) error {
				node.AddHook(&distinctModuleHook{version: version})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		} else {
			client.AddHook(&distinctModuleHook{version: version})
		}
		e.store = &store{client: client}
		if _, ok, err := e.indexCountDistinct(h.ctx, p, w, p.aggregateCalls(), nil); err != nil || ok {
			t.Fatalf("unverified binary equality: %v %v", ok, err)
		}
		rows, err := e.runSelect(h.ctx, p, nil)
		if err != nil || len(rows) != 1 || rows[0][0].I != 10 {
			t.Fatalf("binary fallback: %v %v", rows, err)
		}
	}
	h.exec("ALTER TABLE it_distinct_exact RENAME COLUMN v TO renamed")
	h.expectRows("SELECT COUNT(DISTINCT renamed) FROM it_distinct_exact", "5")
	// Unsafe SQL predicates / expression arguments and mixed aggregates fall back.
	for _, sql := range []string{
		"SELECT COUNT(DISTINCT renamed+1) FROM it_distinct_exact",
		"SELECT COUNT(DISTINCT s) FILTER (WHERE LENGTH(s)>0) FROM it_distinct_exact",
		"SELECT COUNT(*),COUNT(DISTINCT renamed) FROM it_distinct_exact",
	} {
		e, p, w := distinctTestPlan(t, h, sql)
		if _, ok, err := e.indexCountDistinct(h.ctx, p, w, p.aggregateCalls(), nil); err != nil || ok {
			t.Fatalf("fallback %s: %v %v", sql, ok, err)
		}
	}
	h.exec("ALTER TABLE it_distinct_exact ADD COLUMN later BIGINT DEFAULT 9007199254740993")
	h.expectRows("SELECT COUNT(DISTINCT later) FROM it_distinct_exact", "1")
	e, p, w := distinctTestPlan(t, h, "SELECT COUNT(DISTINCT later) FROM it_distinct_exact")
	if _, ok, err := e.indexCountDistinct(h.ctx, p, w, p.aggregateCalls(), nil); err != nil || ok {
		t.Fatalf("virtual default fallback: %v %v", ok, err)
	}
}

func TestSQLDistinctPagesAndShards(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_distinct_pages")
	h.exec("CREATE TABLE it_distinct_pages (g INTEGER,v BIGINT)")
	st := &store{client: h.rawClient()}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_distinct_pages")
	if err != nil {
		t.Fatal(err)
	}
	const distinct = 12003
	// Each tuple has two rows with different keys/slots, exercising global
	// deduplication of overlapping values across shards and cursor pages.
	for start := 0; start < 2*distinct; start += pipelineChunk {
		pipe := st.client.Pipeline()
		for i := start; i < min(start+pipelineChunk, 2*distinct); i++ {
			v := int64(9007199254740992) + int64(i%distinct)
			pipe.HSet(h.ctx, fmt.Sprintf("%s%d", meta.prefix(), i+1), rowIDField, i+1, "g", i%3, "v", fmt.Sprint(v))
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	sql := "SELECT g,COUNT(DISTINCT v) FROM it_distinct_pages GROUP BY g ORDER BY g"
	h.expectRows(sql, "0|4001", "1|4001", "2|4001")
	h.expectRows("SELECT COUNT(DISTINCT v) FROM it_distinct_pages", "12003")
	e, p, w := distinctTestPlan(t, h, sql)
	if _, ok, err := e.indexCountDistinct(h.ctx, p, w, p.aggregateCalls(), nil); err != nil || !ok {
		t.Fatalf("page strategy: %v %v", ok, err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, _, err := e.indexCountDistinct(ctx, p, w, p.aggregateCalls(), nil); err == nil {
		t.Fatal("cancelled query succeeded")
	}
	ctx, cancel = context.WithCancel(h.ctx)
	client := h.rawClient()
	hook := &distinctCancelHook{cancel: cancel}
	if cc, ok := client.(*goredis.ClusterClient); ok {
		node, err := cc.MasterForKey(h.ctx, meta.index())
		if err != nil {
			t.Fatal(err)
		}
		node.AddHook(hook)
	} else {
		client.AddHook(hook)
	}
	e.store = &store{client: client}
	if _, _, err := e.indexCountDistinct(ctx, p, w, p.aggregateCalls(), nil); err == nil || !hook.opened {
		t.Fatalf("cancel after cursor open: %v opened=%v", err, hook.opened)
	}
	// Use an unhooked client for subsequent reads and assert cursor cleanup.
	e.store = st
	info, err := st.searchDo(h.ctx, meta.index(), "FT.INFO", meta.index()).Result()
	if err != nil {
		t.Fatal(err)
	}
	fields := info.([]any)
	found := false
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] != "cursor_stats" {
			continue
		}
		stats := fields[i+1].([]any)
		for j := 0; j+1 < len(stats); j += 2 {
			if stats[j] == "index_total" {
				found = true
				if stats[j+1] != int64(0) {
					t.Fatalf("leaked cursor: %v", stats)
				}
			}
		}
	}
	if !found {
		t.Fatal("FT.INFO omitted cursor stats")
	}
	// Alternate integer spelling is equal in SQL, but not as raw strings.
	// Validate canonical spelling and fall back rather than overcounting.
	if err := st.client.HSet(h.ctx, meta.prefix()+"1", "v", "+9007199254740992").Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := e.indexCountDistinct(h.ctx, p, w, p.aggregateCalls(), nil); err != nil || ok {
		t.Fatalf("canonical fallback: %v %v", ok, err)
	}
	h.expectRows("SELECT COUNT(DISTINCT v) FROM it_distinct_pages", "12003")
}

func TestSQLDistinctTupleResidual(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_distinct_residual")
	h.exec("CREATE TABLE it_distinct_residual (s VARCHAR, v BIGINT)")
	h.exec("INSERT INTO it_distinct_residual VALUES ('',1),(' a ',9007199254740992),('a',9007199254740993),('a',9007199254740993),('a\x00b',9007199254740993),('a\x00c',NULL),(NULL,1)")
	for _, sql := range []string{
		"SELECT s,COUNT(DISTINCT v) FROM it_distinct_residual WHERE s<>'' GROUP BY s",
		"SELECT COUNT(DISTINCT s) FILTER (WHERE s<>'') FROM it_distinct_residual",
		"SELECT COUNT(DISTINCT v) FROM it_distinct_residual WHERE v>9007199254740992",
		"SELECT s,COUNT(DISTINCT v) FILTER (WHERE v=9007199254740993) FROM it_distinct_residual GROUP BY s",
	} {
		e, p, w := distinctTestPlan(t, h, sql)
		aggs := p.aggregateCalls()
		got, ok, err := e.indexCountDistinct(h.ctx, p, w, aggs, nil)
		if !ok || err != nil {
			t.Fatalf("%s: %v %v", sql, ok, err)
		}
		want, err := e.driverAggregate(h.ctx, p, w, aggs, nil)
		if err != nil {
			t.Fatal(err)
		}
		byGroup := func(groups []aggGroup) map[string]string {
			out := map[string]string{}
			for _, g := range groups {
				vals := []Value{}
				for _, expr := range p.sel.GroupBy {
					vals = append(vals, g.rep[expr.(*ColumnRef).Name])
				}
				out[rowKey(vals)] = g.results[aggs[0]].Text()
			}
			return out
		}
		if !reflect.DeepEqual(byGroup(got), byGroup(want)) {
			t.Fatalf("%s: got %v want %v", sql, byGroup(got), byGroup(want))
		}
	}
	h.dropTables("it_distinct_noindex")
	h.exec("CREATE TABLE it_distinct_noindex (v BIGINT NOINDEX)")
	h.exec("INSERT INTO it_distinct_noindex VALUES (9007199254740992),(9007199254740993),(9007199254740993),(NULL)")
	h.expectRows("SELECT COUNT(DISTINCT v) FROM it_distinct_noindex", "2")
}

type distinctCancelHook struct {
	cancel context.CancelFunc
	opened bool
}

func (h *distinctCancelHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *distinctCancelHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *distinctCancelHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && strings.EqualFold(fmt.Sprint(cmd.Args()[0]), "FT.AGGREGATE") {
			h.opened = true
			h.cancel()
		}
		return err
	}
}

// Force unlike tuples into one group instead of relying on finding a
// particular 64-bit hash collision. The collision checks must reject the
// merged group, even when every representative value looks canonical.
func TestSQLDistinctCollisionVerification(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_distinct_collision")
	h.exec("CREATE TABLE it_distinct_collision (v BIGINT, s VARCHAR)")
	for _, values := range []string{
		"(9007199254740992,'x'),(9007199254740993,'x')",
		"(1,'a\x00b'),(1,'a\x00c')",
		"(1,'x'),(NULL,'x')",
	} {
		h.exec("TRUNCATE it_distinct_collision")
		h.exec("INSERT INTO it_distinct_collision VALUES " + values)
		e, p, w := distinctTestPlan(t, h, "SELECT COUNT(DISTINCT v,s) FROM it_distinct_collision")
		v, _ := p.meta.column("v")
		s, _ := p.meta.column("s")
		req := distinctRequest(p.meta, w, nil, []columnMeta{v, s})
		// Replace grouping on the actual tuple by GROUPBY 0, preserving all
		// verification reducers, and explicitly return representative columns.
		for i, arg := range req.groupBy {
			if arg == "GROUPBY" {
				n := req.groupBy[i+1].(int)
				req.groupBy = append(append(req.groupBy[:i+1:i+1], 0), req.groupBy[i+2+n:]...)
				break
			}
		}
		req.groupBy = append(req.groupBy, "REDUCE", "FIRST_VALUE", 1, "@v", "AS", "v", "REDUCE", "FIRST_VALUE", 1, "@s", "AS", "s")
		cur, err := e.store.openAggregate(h.ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		page, err := cur.next(h.ctx)
		cur.close(h.ctx)
		if err != nil || len(page) != 1 {
			t.Fatalf("forced group: %v %v", page, err)
		}
		if req.distinctTuple(page[0]) {
			t.Fatalf("accepted unlike tuples: %v", page[0])
		}
	}
}

// Version zero denies inspection; an older version lacks verified binary
// equality. Both must leave NUL strings on the row-based exact path.
type distinctModuleHook struct{ version int64 }

func (h *distinctModuleHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *distinctModuleHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *distinctModuleHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "module" {
			if h.version == 0 {
				return fmt.Errorf("NOPERM MODULE LIST")
			}
			cmd.(*goredis.Cmd).SetVal([]any{[]any{"name", "search", "ver", h.version}})
			return nil
		}
		return next(ctx, cmd)
	}
}
