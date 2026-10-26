// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// These are query-shape profiles on a disposable local fixture, not retained
// full-scale ClickBench measurements. Q29 uses a smaller HAVING threshold.
// Independent validation checks allowed boundary ties against all SQL groups.
func TestClickBenchExecutionProfile(t *testing.T) {
	dir := os.Getenv("REDIS_EXECUTION_PROFILE_DIR")
	if dir == "" {
		t.Skip("set REDIS_EXECUTION_PROFILE_DIR to a fresh output directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	n, cardinality := 20000, 257
	for _, setting := range []struct {
		name string
		dst  *int
	}{{"REDIS_EXECUTION_PROFILE_ROWS", &n}, {"REDIS_EXECUTION_PROFILE_GROUPS", &cardinality}} {
		if value := os.Getenv(setting.name); value != "" {
			v, err := strconv.Atoi(value)
			if err != nil || v < 1 {
				t.Fatal("invalid profile size")
			}
			*setting.dst = v
		}
	}
	fixture, err := os.OpenFile(filepath.Join(dir, "fixture.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	encodeFixture := json.NewEncoder(fixture)
	h := newSQLHarness(t)
	h.dropTables("it_execution_profile")
	cols := []string{"WatchID BIGINT", "ClientIP BIGINT", "IsRefresh SMALLINT", "ResolutionWidth INTEGER", "SearchPhrase VARCHAR", "Referer VARCHAR"}
	for i := len(cols); i < 105; i++ {
		cols = append(cols, fmt.Sprintf("padding%d VARCHAR", i))
	}
	h.exec("CREATE TABLE it_execution_profile (" + strings.Join(cols, ",") + ")")
	client := h.rawClient()
	defer client.Close()
	st := &store{client: client}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_execution_profile")
	if err != nil {
		t.Fatal(err)
	}
	last := int64(0)
	for start := 0; start < n; start += pipelineChunk {
		pipe := client.Pipeline()
		for i := start; i < min(start+pipelineChunk, n); i++ {
			id := int64(i%4)*aggregatePartitionRows + int64(i/4) + 1
			last = max(last, id)
			// IDs have deliberate gaps to exercise four nonempty partitions.
			row := map[string]any{"watchid": int64(9007199254740992) + int64(i%cardinality), "clientip": int64(i % cardinality),
				"isrefresh": i % 2, "resolutionwidth": 800 + i%1024, "searchphrase": fmt.Sprintf("phrase-%03d", i%101),
				"referer": fmt.Sprintf("https://www.host-%02d.example/path-%d", i%31, i)}
			if i%13 == 0 {
				row["searchphrase"] = ""
			}
			if i%17 == 0 {
				row["resolutionwidth"] = nil
			}
			if i%19 == 0 {
				row["referer"] = nil
			}
			if i%23 == 0 {
				row["searchphrase"] = "a\x00tail"
			}
			fields := []any{rowIDField, id}
			for name, value := range row {
				if value != nil {
					fields = append(fields, name, fmt.Sprint(value))
				}
			}
			for j := 6; j < 105; j++ {
				fields = append(fields, fmt.Sprintf("padding%d", j), "unrelated-wide-payload-0123456789-abcdefghijklmn")
			}
			pipe.HSet(h.ctx, meta.prefix()+strconv.FormatInt(id, 10), fields...)
			if err := encodeFixture.Encode(row); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Set(h.ctx, seqKey(meta.Schema, meta.Name), last, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := st.updateTable(h.ctx, meta.Schema, meta.Name, func(m *tableMeta) error {
		for i := range m.Columns {
			if m.Columns[i].Name == "searchphrase" {
				m.Columns[i].TagValues = tagsTruncated
				m.Columns[i].TagsChecked = true
			}
		}
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "execution profile indexed", func() bool {
		count, err := st.countMatches(h.ctx, meta.index(), "*")
		return err == nil && count == int64(n)
	})
	var sums []string
	for i := 0; i < 90; i++ {
		arg := "ResolutionWidth"
		if i > 0 {
			arg += fmt.Sprintf("+%d", i)
		}
		sums = append(sums, "SUM("+arg+")")
	}
	queries := []struct {
		number int
		sql    string
	}{
		{26, "SELECT SearchPhrase FROM it_execution_profile WHERE SearchPhrase<>'' ORDER BY SearchPhrase LIMIT 10"},
		{29, `SELECT REGEXP_REPLACE(Referer, '^https?://(?:www\.)?([^/]+)/.*$', '\1') AS k,AVG(length(Referer)) AS l,COUNT(*) AS c,MIN(Referer) FROM it_execution_profile WHERE Referer<>'' GROUP BY k HAVING COUNT(*)>10 ORDER BY l DESC LIMIT 25`},
		{30, "SELECT " + strings.Join(sums, ",") + " FROM it_execution_profile"},
		{32, "SELECT WatchID,ClientIP,COUNT(*) AS c,SUM(IsRefresh),AVG(ResolutionWidth) FROM it_execution_profile WHERE SearchPhrase<>'' GROUP BY WatchID,ClientIP ORDER BY c DESC LIMIT 10"},
		{33, "SELECT WatchID,ClientIP,COUNT(*) AS c,SUM(IsRefresh),AVG(ResolutionWidth) FROM it_execution_profile GROUP BY WatchID,ClientIP ORDER BY c DESC LIMIT 10"},
		{36, "SELECT ClientIP,ClientIP-1,ClientIP-2,ClientIP-3,COUNT(*) AS c FROM it_execution_profile GROUP BY ClientIP,ClientIP-1,ClientIP-2,ClientIP-3 ORDER BY c DESC LIMIT 10"},
	}
	cpu := func() float64 {
		var r syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
			t.Fatal(err)
		}
		return float64(r.Utime.Nano()+r.Stime.Nano()) / 1e9
	}
	for _, q := range queries {
		for _, strategy := range []string{"baseline", "candidate"} {
			for trial := 1; trial <= 3; trial++ {
				e, plan, wp := distinctTestPlan(t, h, q.sql)
				opts, err := goredis.ParseURL(os.Getenv("REDIS_URI"))
				if err != nil {
					t.Fatal(err)
				}
				cfg := newClientConfig(opts)
				pc := newClient(cfg, false, timeouts{})
				if err := pc.Ping(h.ctx).Err(); err != nil {
					t.Fatal(err)
				}
				e.store = &store{client: pc}
				e.queryWorkers = 4
				var grant atomic.Int64
				e.workerGrant = &grant
				e.aggregateMetrics = &partitionMetrics{}
				finish := e.startDiagnostics(filepath.Join(dir, fmt.Sprintf("q%d-%s-%d.execution.json", q.number, strategy, trial)), cfg)
				ctx := context.WithValue(h.ctx, diagnosticsContextKey{}, e.diagnostics)
				var groups []aggGroup
				var values [][]Value
				var before, after runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&before)
				cpuBefore, started := cpu(), time.Now()
				mergeSeconds, mergeCPU, mergeAllocated := 0.0, 0.0, uint64(0)
				if q.number == 26 {
					var sc selectScan
					sc, err = e.prepareScan(ctx, plan, nil)
					if err == nil {
						if strategy == "baseline" {
							values, err = e.selectScanned(ctx, plan, sc, nil)
						} else {
							var handled bool
							values, handled, err = e.selectTopK(ctx, plan, sc, nil)
							if !handled {
								t.Fatal("top-K profile not selected")
							}
						}
					}
				} else if q.number == 32 || q.number == 33 {
					// Hold concurrency at one to isolate narrow scans/partial
					// state from scheduling. Production uses the shared budget.
					grant.Store(1)
					p, ok := tupleAggregateShape(plan, plan.aggregateCalls())
					if !ok {
						t.Fatal("shape")
					}
					// Match production's retention of residual-only source
					// columns before asking Redis for source tuples.
					need := map[string]bool{}
					columnRefs(wp.residual, need)
					for _, c := range plan.meta.Columns {
						found := false
						for _, old := range p.cols {
							found = found || old.Name == c.Name
						}
						if need[c.Name] && !found {
							p.cols = append(p.cols, c)
						}
					}
					if strategy == "baseline" {
						p.server = true
						p.partialGroups = false
					}
					spill := tupleSpill{dir: t.TempDir(), prefix: "profile", metrics: e.aggregateMetrics, combine: p.combine}
					e.aggregateMetrics.used.Store(true)
					e.aggregateMetrics.workers.Store(1)
					e.diagnostics.setPlan(map[string]any{"kind": "partitioned_exact", "server_source_tuples": p.server, "partial_groups": p.partialGroups, "workers": 1})
					e.diagnostics.setPhase("partition_fetch")
					for lo := int64(1); lo <= last && err == nil; lo += aggregatePartitionRows {
						err = e.aggregateTuplePartition(ctx, plan, p, wp, nil, lo, min(last, lo+aggregatePartitionRows-1), false, func(row spillTuple) error { e.aggregateMetrics.rows.Add(row.count); return spill.add(ctx, row) })
					}
					if err == nil {
						var m0, m1 runtime.MemStats
						runtime.ReadMemStats(&m0)
						mergeStart, c0 := time.Now(), cpu()
						e.diagnostics.setPhase("aggregate_merge")
						groups, err = e.reduceTupleRuns(ctx, plan, p, plan.aggregateCalls(), nil, &spill)
						mergeSeconds, mergeCPU = time.Since(mergeStart).Seconds(), cpu()-c0
						runtime.ReadMemStats(&m1)
						mergeAllocated = m1.TotalAlloc - m0.TotalAlloc
					}
				} else if strategy == "baseline" {
					groups, err = e.serialDriverAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
				} else if q.number == 30 {
					var handled bool
					groups, handled, err = e.parallelAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
					if !handled {
						t.Fatal("parallel profile not selected")
					}
				} else {
					var handled bool
					groups, handled, err = e.partitionedAggregate(ctx, plan, wp, plan.aggregateCalls(), nil, false)
					if !handled {
						t.Fatal("partition profile not selected")
					}
				}
				seconds, usedCPU := time.Since(started).Seconds(), cpu()-cpuBefore
				runtime.ReadMemStats(&after)
				finish(err)
				pc.Close()
				if err != nil {
					t.Fatal(err)
				}
				if q.number != 26 {
					values = profileGroupOutput(t, e, plan, groups)
				}
				output := make([][]any, len(values))
				for i, row := range values {
					output[i] = make([]any, len(row))
					for j, v := range row {
						if !v.Null {
							output[i][j] = encodeStored(v)
						}
					}
				}
				record := map[string]any{"query": q.number, "sql": q.sql, "strategy": strategy, "trial": trial, "rows": n, "group_cardinality": cardinality, "columns": 105,
					"seconds": seconds, "process_cpu_seconds": usedCPU, "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs,
					"merge_seconds": mergeSeconds, "merge_cpu_seconds": mergeCPU, "merge_allocated_bytes": mergeAllocated, "worker_grant": grant.Load(),
					"aggregate_metrics": json.RawMessage(e.aggregateMetrics.json()), "connection_read_bytes": e.diagnostics.readBytes.Load(), "values": output}
				b, _ := json.Marshal(record)
				file, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("q%d-%s-%d.json", q.number, strategy, trial)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.Write(append(b, '\n')); err != nil {
					t.Fatal(err)
				}
				file.Close()
				t.Logf("PROFILE Q%d %s trial=%d seconds=%.4f allocated=%d wire=%d", q.number, strategy, trial, seconds, after.TotalAlloc-before.TotalAlloc, e.diagnostics.readBytes.Load())
			}
		}
	}
}

func profileGroupOutput(t *testing.T, e *executor, plan *selectPlan, groups []aggGroup) [][]Value {
	t.Helper()
	env := e.newEnv(context.Background(), plan.meta.types(), nil)
	var rows []aggregateOutput
	for _, g := range groups {
		env.row, env.aggs = g.rep, g.results
		if plan.having != nil {
			v, err := env.eval(plan.having)
			if err != nil {
				t.Fatal(err)
			}
			if b, ok := truthy(v); !ok || !b {
				continue
			}
		}
		row := aggregateOutput{}
		var err error
		row.vals, err = evalItems(env, plan.items)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range plan.order {
			v, err := env.eval(o.expr)
			if err != nil {
				t.Fatal(err)
			}
			row.keys = append(row.keys, v)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return lessKeys(rows[i].keys, rows[j].keys, plan.order) })
	rows = applyLimit(rows, plan.sel.Offset, plan.sel.Limit)
	result := make([][]Value, len(rows))
	for i, row := range rows {
		result[i] = row.vals
	}
	return result
}
