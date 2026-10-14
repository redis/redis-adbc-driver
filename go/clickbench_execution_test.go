// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func projectedGroupKeys(t *testing.T, e *executor, plan *selectPlan, groups []aggGroup) []string {
	t.Helper()
	env := e.newEnv(context.Background(), plan.meta.types(), nil)
	var out []string
	for _, g := range groups {
		env.row, env.aggs = g.rep, g.results
		row, err := evalItems(env, plan.items)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rowKey(row))
	}
	sort.Strings(out)
	return out
}

func TestSQLCheckedParallelInputs(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_checked_parallel")
	h.exec("CREATE TABLE it_checked_parallel (v BIGINT,n BIGINT)")
	h.exec("INSERT INTO it_checked_parallel(v) VALUES (9007199254740993),(-9007199254740993),(7),(NULL)")
	sql := "SELECT SUM(v+1),AVG(v*2),COUNT(v-1),MIN(v+1),MAX(v+1),SUM(v+1) FROM it_checked_parallel"
	e, plan, wp := distinctTestPlan(t, h, sql)
	if err := e.store.client.HSet(h.ctx, plan.meta.prefix()+"2", rowIDField, 3000).Err(); err != nil {
		t.Fatal(err)
	}
	if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), 4096, 0).Err(); err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 2, 4} {
		e.queryWorkers = workers
		got, handled, err := e.parallelAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil || !handled {
			t.Fatalf("workers=%d handled=%v err=%v", workers, handled, err)
		}
		want, err := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(projectedGroupKeys(t, e, plan, got), projectedGroupKeys(t, e, plan, want)) {
			t.Fatal("parallel expression results differ")
		}
	}
	h.expectRows(sql, "10|4.666666666666667|3|-9007199254740992|9007199254740994|10")
	// Overflow before final cancellation and NULL on the other operand must
	// both survive compiled evaluation; division remains in the serial path.
	h.exec("DELETE FROM it_checked_parallel")
	h.exec("INSERT INTO it_checked_parallel(v) VALUES (9223372036854775807),(-9223372036854775808),(NULL)")
	for _, query := range []string{
		"SELECT SUM(v),SUM(v+1) FROM it_checked_parallel",
		"SELECT SUM(n+(v+1)) FROM it_checked_parallel",
	} {
		e, plan, wp = distinctTestPlan(t, h, query)
		e.queryWorkers = 2
		if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), 8192, 0).Err(); err != nil {
			t.Fatal(err)
		}
		_, handled, got := e.parallelAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
		_, want := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
		if !handled || got == nil || want == nil || got.Error() != want.Error() {
			t.Fatalf("%s handled=%v errors: %v / %v", query, handled, got, want)
		}
	}
	e, plan, _ = distinctTestPlan(t, h, "SELECT SUM(v/0),SUM(v+1) FROM it_checked_parallel")
	if parallelAggregates(plan.aggregateCalls(), plan.meta.types()) {
		t.Fatal("division admitted")
	}
}

func TestSQLPartitionedExpressionGroups(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_expression_groups")
	h.exec("CREATE TABLE it_expression_groups (ip BIGINT, referer VARCHAR)")
	h.exec(`INSERT INTO it_expression_groups VALUES
 (9007199254740993,'https://www.example.com/a'),(9007199254740993,'http://example.com/b'),
 (9007199254740992,'https://other.com/c'),(9007199254740992,''),(NULL,NULL),(NULL,'http://example.com/d')`)
	queries := []string{
		`SELECT REGEXP_REPLACE(referer, '^https?://(?:www\.)?([^/]+)/.*$', '\1') AS k,AVG(length(referer)) AS l,COUNT(*) AS c,MIN(referer) FROM it_expression_groups WHERE referer<>'' GROUP BY k HAVING COUNT(*)>0 ORDER BY l DESC LIMIT 25`,
		"SELECT ip,ip-1,ip-2,ip-3,COUNT(*) AS c FROM it_expression_groups GROUP BY ip,ip-1,ip-2,ip-3 ORDER BY c DESC LIMIT 10",
	}
	for _, sql := range queries {
		e, plan, wp := distinctTestPlan(t, h, sql)
		shape, ok := tupleAggregateShape(plan, plan.aggregateCalls())
		if !ok {
			t.Fatal("shape rejected")
		}
		if strings.Contains(sql, "ip-1") && !reflect.DeepEqual(shape.groupKeyMask, []bool{true, false, false, false}) {
			t.Fatalf("dependent key mask: %v", shape.groupKeyMask)
		}
		if strings.Contains(sql, "REGEXP_REPLACE") && !shape.partialGroups {
			t.Fatal("regex grouping did not use partial final groups")
		}
		if err := e.store.client.HSet(h.ctx, plan.meta.prefix()+"2", rowIDField, aggregatePartitionRows+2).Err(); err != nil {
			t.Fatal(err)
		}
		if err := e.store.client.Set(h.ctx, seqKey(plan.meta.Schema, plan.meta.Name), aggregatePartitionRows*2, 0).Err(); err != nil {
			t.Fatal(err)
		}
		e.queryWorkers = 2
		got, handled, err := e.partitionedAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil, false)
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", sql, handled, err)
		}
		want, err := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(projectedGroupKeys(t, e, plan, got), projectedGroupKeys(t, e, plan, want)) {
			t.Fatal("expression groups differ")
		}
	}
	_, independent, _ := distinctTestPlan(t, h, "SELECT ip-1,COUNT(*) FROM it_expression_groups GROUP BY ip-1")
	shape, _ := tupleAggregateShape(independent, independent.aggregateCalls())
	if !reflect.DeepEqual(shape.groupKeyMask, []bool{true}) {
		t.Fatal("removed a nonredundant checked key")
	}
	h.exec("INSERT INTO it_expression_groups VALUES (-9223372036854775808,'x')")
	e, plan, wp := distinctTestPlan(t, h, queries[1])
	_, handled, got := e.partitionedAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil, false)
	_, want := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
	if !handled || got == nil || want == nil || got.Error() != want.Error() {
		t.Fatalf("checked group error: %v / %v", got, want)
	}
}

func TestSQLPartialGroupsMultipleSpillPasses(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_partial_states")
	h.exec("CREATE TABLE it_partial_states (g BIGINT,v BIGINT,w INTEGER)")
	for i := 0; i < spillFanIn+1; i++ {
		h.exec("INSERT INTO it_partial_states VALUES (9007199254740993,9223372036854775807,7),(9007199254740993,-9223372036854775807,NULL),(9007199254740993,1,11),(9007199254740992,NULL,NULL)")
	}
	e, plan, wp := distinctTestPlan(t, h, "SELECT g,COUNT(*) AS c,SUM(v),AVG(w),SUM(v) FROM it_partial_states GROUP BY g ORDER BY c DESC LIMIT 10")
	p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
	if !ok || !p.partialGroups || p.server {
		t.Fatal("mixed aggregates did not select narrow partial groups")
	}
	m := &partitionMetrics{}
	s := tupleSpill{dir: t.TempDir(), prefix: "partial", combine: p.combine, metrics: m}
	for i := int64(0); i < spillFanIn+1; i++ {
		if err := e.aggregateTuplePartition(h.ctx, plan, p, wp, nil, i*4+1, i*4+4, false, func(row spillTuple) error { return s.add(h.ctx, row) }); err != nil {
			t.Fatal(err)
		}
		if err := s.flush(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.reduceTupleRuns(h.ctx, plan, p, plan.aggregateCalls(), nil, &s)
	if err != nil {
		t.Fatal(err)
	}
	want, err := e.serialDriverAggregate(h.ctx, plan, wp, plan.aggregateCalls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projectedGroupKeys(t, e, plan, got), projectedGroupKeys(t, e, plan, want)) {
		t.Fatal("partial state merge differs")
	}
	if m.spillBytes.Load() == 0 || m.spillReadBytes.Load() == 0 || m.spillRead.seconds() <= 0 || m.spillWrite.seconds() <= 0 {
		t.Fatalf("missing I/O metrics: %s", m.json())
	}
	h.expectRows("SELECT g,COUNT(*),SUM(v),AVG(w) FROM it_partial_states GROUP BY g ORDER BY g", "9007199254740992|33|NULL|NULL", "9007199254740993|99|33|9")
}

func TestExecutionDiagnosticsLive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.json")
	var grant atomic.Int64
	e := &executor{aggregateMetrics: &partitionMetrics{}, workerGrant: &grant}
	cfg := &clientConfig{}
	finish := e.startDiagnostics(path, cfg)
	d := e.diagnostics
	d.setPhase("partition_fetch")
	d.setPlan(map[string]any{"kind": "partitioned_exact"})
	grant.Store(4)
	d.processed.Add(100)
	d.metrics.used.Store(true)
	d.metrics.workers.Store(4)
	d.metrics.rows.Add(75)
	endWait := d.redisWait.start()
	var sample map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(b, &sample) == nil && sample["worker_grant"] == float64(4) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	endWait()
	finish(fmt.Errorf("cancelled"))
	if sample["status"] != "running" || sample["processed_rows"] != float64(100) || sample["redis_round_trip_worker_seconds"].(float64) <= 0 {
		t.Fatalf("no live progress: %v", sample)
	}
	if cfg.diagnostics.Load() != nil {
		t.Fatal("diagnostic collector leaked")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &sample); err != nil || sample["status"] != "execute_failed" {
		t.Fatalf("final checkpoint: %s %v", b, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("sidecar permissions")
	}
	// Optional I/O failure must never replace the query's error.
	e.startDiagnostics(filepath.Join(t.TempDir(), "missing", "out.json"), cfg)(fmt.Errorf("original"))
}

func TestSQLExecutionDiagnosticsAndStreamTopK(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_diag_topk")
	h.exec("CREATE TABLE it_diag_topk (phrase VARCHAR,payload INTEGER)")
	h.exec("INSERT INTO it_diag_topk VALUES ('z',1),('a\x00z',2),('a\x00b',3),('',4),('b',5),(NULL,6)")
	path := filepath.Join(t.TempDir(), "q26.execution.json")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err = st.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringExecutionDiagnosticsFile, path); err != nil {
		t.Fatal(err)
	}
	if err = st.SetSqlQuery(h.ctx, "SELECT phrase FROM it_diag_topk WHERE phrase<>'' ORDER BY phrase LIMIT 2"); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err = json.Unmarshal(b, &sample); err != nil {
		t.Fatal(err)
	}
	if sample["phase"] != "select_topk" || sample["status"] != "execute_returned" || sample["processed_rows"].(float64) == 0 || sample["connection_read_bytes"].(float64) == 0 {
		t.Fatalf("snapshot: %s", b)
	}
	if strings.Contains(string(b), os.Getenv("REDIS_URI")) {
		t.Fatal("URI in diagnostic file")
	}
	// Exercise the cap fallback, including projection fields pruned by the
	// earlier late-projection plan, without changing shared server settings.
	e, plan, _ := distinctTestPlan(t, h, "SELECT payload,phrase FROM it_diag_topk WHERE phrase<>'' ORDER BY payload LIMIT 2 OFFSET 1")
	e.store.searchCapsOnce.Do(func() { e.store.searchCaps = [2]int64{1, 1} })
	sc, err := e.prepareScan(h.ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.req.hashScan || sc.indexSort || sc.req.late != nil || !sc.req.need["payload"] || !sc.req.need["phrase"] {
		t.Fatal("cap fallback dropped projection inputs")
	}
	got, handled, err := e.selectTopK(h.ctx, plan, sc, nil)
	if err != nil || !handled {
		t.Fatalf("hash topk %v %v", handled, err)
	}
	want, err := e.selectScanned(h.ctx, plan, sc, nil)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("hash topk got=%v want=%v err=%v", got, want, err)
	}
}
