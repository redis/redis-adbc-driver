// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

// Opt-in synthetic comparisons for #322. This is not ClickBench. Every result
// is fully consumed and checked against a generated-data oracle.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type flexBenchResult struct {
	Kind, Target, Schema, Reader, Workload                            string
	Trial, Rows                                                       int
	Seconds, ClientCPUSeconds                                         float64
	Commands, ReadBytes, WrittenBytes                                 int64
	AllocatedBytes, SampledPeakHeapBytes, ProcessLifetimePeakRSSBytes uint64
	ServerBefore, ServerAfter                                         *flexBenchServer
	AggregateMetrics                                                  json.RawMessage `json:",omitempty"`
}
type flexBenchServer struct {
	CPUSeconds                              float64
	RSSBytes, DiskReadBytes, DiskWriteBytes uint64
}

const flexBenchProc = `import os,json
cpu=rss=rd=wr=0
for pid in os.listdir('/proc'):
 if not pid.isdigit(): continue
 try:
  comm=open('/proc/'+pid+'/comm').read().strip()
  if not (comm.startswith('redis-server') or comm=='dmcproxy'): continue
  s=open('/proc/'+pid+'/stat').read().rsplit(')',1)[1].split()
  cpu+=(int(s[11])+int(s[12]))/os.sysconf('SC_CLK_TCK')
  rss+=int(s[21])*os.sysconf('SC_PAGE_SIZE')
  for line in open('/proc/'+pid+'/io'):
   k,v=line.split(':')
   if k=='read_bytes': rd+=int(v)
   if k=='write_bytes': wr+=int(v)
 except (OSError,ValueError): pass
print(json.dumps(dict(CPUSeconds=cpu,RSSBytes=rss,DiskReadBytes=rd,DiskWriteBytes=wr)))`

func flexBenchServerStats(t *testing.T) *flexBenchServer {
	t.Helper()
	name := os.Getenv("REDIS_FLEX_BENCH_CONTAINER")
	if name == "" {
		return nil
	}
	out, err := exec.Command("docker", "exec", name, "python3", "-c", flexBenchProc).Output()
	if err != nil {
		t.Fatal("server process counters:", err)
	}
	var s flexBenchServer
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	if s.RSSBytes == 0 {
		t.Fatal("no Redis/proxy processes measured")
	}
	return &s
}
func flexBenchCPU() (float64, uint64) {
	var r syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	rss := uint64(r.Maxrss)
	if runtime.GOOS == "linux" {
		rss *= 1024
	}
	return float64(r.Utime.Nano()+r.Stime.Nano()) / 1e9, rss
}
func flexBenchConn(t *testing.T, flex bool) (*sqlHarness, *connectionImpl) {
	t.Helper()
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringFlex: strconv.FormatBool(flex), OptionStringTelemetryEnabled: "false", OptionStringQueryWorkers: "1", OptionStringQueryWorkerBudget: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx); _ = db.Close(ctx) })
	st, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	impl := st.(*statementImpl).conn
	_ = st.Close(ctx)
	return &sqlHarness{t: t, ctx: ctx, conn: conn}, impl
}
func flexBenchMeasure(t *testing.T, c *connectionImpl, r flexBenchResult, fn func(context.Context) int) flexBenchResult {
	t.Helper()
	r.ServerBefore = flexBenchServerStats(t)
	c.aggregateMetrics.Store(nil)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapAlloc)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for old := peak.Load(); m.HeapAlloc > old; old = peak.Load() {
					if peak.CompareAndSwap(old, m.HeapAlloc) {
						break
					}
				}
			case <-stop:
				return
			}
		}
	}()
	d := &executionDiagnostics{}
	c.clientCfg.diagnostics.Store(d)
	ctx := context.WithValue(context.Background(), diagnosticsContextKey{}, d)
	cpu, _ := flexBenchCPU()
	started := time.Now()
	r.Rows = fn(ctx)
	r.Seconds = time.Since(started).Seconds()
	cpuAfter, rss := flexBenchCPU()
	r.ClientCPUSeconds = cpuAfter - cpu
	c.clientCfg.diagnostics.Store(nil)
	close(stop)
	<-done
	runtime.ReadMemStats(&after)
	r.Commands = d.commands.Load()
	r.ReadBytes = d.readBytes.Load()
	r.WrittenBytes = d.writtenBytes.Load()
	r.AllocatedBytes = after.TotalAlloc - before.TotalAlloc
	r.SampledPeakHeapBytes = max(peak.Load(), after.HeapAlloc)
	r.ProcessLifetimePeakRSSBytes = rss
	r.ServerAfter = flexBenchServerStats(t)
	if m := c.aggregateMetrics.Load(); m != nil {
		r.AggregateMetrics = json.RawMessage(m.json())
	}
	return r
}
func flexBenchPayload(id int) string { return strconv.Itoa(id) + "-" + strings.Repeat("x", 120) }
func flexBenchScore(id int) int64    { return int64(id * 7919 % 1000003) }
func TestFlexComparativeProfile(t *testing.T) {
	dir := os.Getenv("REDIS_FLEX_BENCH_DIR")
	if dir == "" {
		t.Skip("set REDIS_FLEX_BENCH_DIR for synthetic comparative profiles")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	n, trials := 50000, 5
	for _, s := range []struct {
		name string
		dst  *int
	}{{"REDIS_FLEX_BENCH_ROWS", &n}, {"REDIS_FLEX_BENCH_TRIALS", &trials}} {
		if v := os.Getenv(s.name); v != "" {
			i, err := strconv.Atoi(v)
			if err != nil || i < 100 {
				if s.name == "REDIS_FLEX_BENCH_TRIALS" && err == nil && i >= 1 {
					*s.dst = i
					continue
				}
				t.Fatal("invalid profile size")
			}
			*s.dst = i
		}
	}
	target := os.Getenv("REDIS_FLEX_BENCH_TARGET")
	if target != "ram" && target != "flex" {
		t.Fatal("REDIS_FLEX_BENCH_TARGET must be ram or flex")
	}
	file, err := os.OpenFile(filepath.Join(dir, target+"-samples.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	type table struct {
		schema, name string
		h            *sqlHarness
		c            *connectionImpl
		meta         *tableMeta
	}
	var tables []table
	for _, plain := range []bool{false, true} {
		if target == "flex" && !plain {
			continue
		}
		schema := "sortable"
		if plain {
			schema = "plain"
		}
		h, c := flexBenchConn(t, plain)
		name := "it_flex_bench_" + schema
		h.exec("DROP TABLE IF EXISTS " + name)
		cols := []string{"id BIGINT", "g INTEGER", "n BIGINT"}
		var pads, values []string
		for i := 0; i < 12; i++ {
			p := fmt.Sprintf("p%d", i)
			pads = append(pads, p)
			cols = append(cols, p+" VARCHAR NOINDEX")
			values = append(values, "cast(i as varchar)||'-'||repeat('x',120)")
		}
		h.exec("CREATE TABLE " + name + " (" + strings.Join(cols, ",") + ")")
		h.exec("INSERT INTO " + name + " SELECT i,i%257,CASE WHEN i%17=0 THEN NULL ELSE (i*7919)%1000003 END," + strings.Join(values, ",") + " FROM generate_series(1," + strconv.Itoa(n) + ") AS t(i)")
		meta, err := c.store.getTable(h.ctx, defaultSchema, name)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "benchmark rows indexed", func() bool {
			v, err := c.store.countMatches(h.ctx, meta.index(), "*")
			return err == nil && v == int64(n)
		})
		tables = append(tables, table{schema, name, h, c, meta})
		t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + name) })
		info, err := c.store.searchDo(h.ctx, meta.index(), "FT.INFO", meta.index()).Result()
		if err != nil {
			t.Fatal(err)
		}
		fields, ok := redisReplyMap(info)
		if !ok {
			t.Fatal("malformed FT.INFO")
		}
		safe := map[string]any{"target": target, "schema": schema, "rows": n, "columns": 15}
		for _, k := range []string{"num_docs", "inverted_sz_mb", "total_inverted_index_blocks", "doc_table_size_mb", "sortable_values_size_mb", "key_table_size_mb", "total_index_memory_sz_mb"} {
			safe[k] = fields[k]
		}
		b, _ := json.MarshalIndent(safe, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, target+"-"+schema+"-index.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	workloads := []string{"selective", "broad", "wide", "topk", "sort", "scalar", "grouped"}
	var ordered []int
	for id := 1; id <= n; id++ {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a%17 == 0 || b%17 == 0 {
			if a%17 == 0 && b%17 == 0 {
				return a < b
			}
			return b%17 == 0
		}
		x, y := flexBenchScore(a), flexBenchScore(b)
		if x == y {
			return a < b
		}
		return x < y
	})
	// Fixed formulas independently define every expected SQL row.
	expected := func(work string) [][]string {
		var rows [][]string
		switch work {
		case "scalar":
			lo, hi := int64(1000003), int64(0)
			for id := 1; id <= n; id++ {
				if id%17 != 0 {
					lo = min(lo, flexBenchScore(id))
					hi = max(hi, flexBenchScore(id))
				}
			}
			return [][]string{{strconv.Itoa(n), strconv.FormatInt(int64(n)*(int64(n)+1)/2, 10), fmt.Sprint(lo), fmt.Sprint(hi)}}
		case "grouped":
			counts, sums := make([]int64, 257), make([]int64, 257)
			for id := 1; id <= n; id++ {
				g := id % 257
				counts[g]++
				sums[g] += int64(id)
			}
			for g := range counts {
				if counts[g] > 0 {
					rows = append(rows, []string{fmt.Sprint(g), fmt.Sprint(counts[g]), fmt.Sprint(sums[g])})
				}
			}
			return rows
		}
		ids := ordered
		if work != "sort" && work != "topk" {
			ids = make([]int, n)
			for i := range ids {
				ids[i] = i + 1
			}
		}
		if work == "selective" {
			ids = ids[max(0, n-100):]
		}
		if work == "topk" {
			ids = ids[:min(100, n)]
		}
		for _, id := range ids {
			row := []string{strconv.Itoa(id)}
			if work == "wide" {
				for range 12 {
					row = append(row, flexBenchPayload(id))
				}
			} else {
				v := fmt.Sprint(flexBenchScore(id))
				if id%17 == 0 {
					v = "NULL"
				}
				row = append(row, v)
			}
			rows = append(rows, row)
		}
		return rows
	}
	for _, work := range workloads {
		want := expected(work)
		for trial := 0; trial <= trials; trial++ {
			for offset := range tables {
				tab := tables[(offset+trial)%len(tables)]
				sql := "SELECT id,n FROM " + tab.name
				switch work {
				case "selective":
					sql += " WHERE id>" + strconv.Itoa(n-100) + " ORDER BY id"
				case "wide":
					var f []string
					for i := 0; i < 12; i++ {
						f = append(f, fmt.Sprintf("p%d", i))
					}
					sql = "SELECT id," + strings.Join(f, ",") + " FROM " + tab.name
				case "topk":
					sql += " ORDER BY n NULLS LAST,id LIMIT 100"
				case "sort":
					sql += " ORDER BY n NULLS LAST,id"
				case "scalar":
					sql = "SELECT count(*),sum(id),min(n),max(n) FROM " + tab.name
				case "grouped":
					sql = "SELECT g,count(*),sum(id) FROM " + tab.name + " GROUP BY g ORDER BY g"
				}
				run := func(ctx context.Context) int {
					st, err := tab.h.conn.NewStatement(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer st.Close(ctx)
					if err := st.SetSqlQuery(ctx, sql); err != nil {
						t.Fatal(err)
					}
					rdr, _, err := st.ExecuteQuery(ctx)
					if err != nil {
						t.Fatal(work, tab.schema, err)
					}
					defer rdr.Release()
					count := 0
					for rdr.Next() {
						rec := rdr.RecordBatch()
						for i := 0; i < int(rec.NumRows()); i++ {
							if count >= len(want) {
								t.Fatal("extra SQL row")
							}
							for col, w := range want[count] {
								a := rec.Column(col)
								got := "NULL"
								if !a.IsNull(i) {
									got = a.ValueStr(i)
								}
								if got != w {
									t.Fatalf("%s %s row %d column %d: %q != %q", tab.schema, work, count, col, got, w)
								}
							}
							count++
						}
					}
					if err := rdr.Err(); err != nil {
						t.Fatal(err)
					}
					if count != len(want) {
						t.Fatalf("%s: %d rows, want %d", work, count, len(want))
					}
					return count
				}
				r := flexBenchMeasure(t, tab.c, flexBenchResult{Kind: "sql", Target: target, Schema: tab.schema, Reader: map[bool]string{true: "bounded", false: "default"}[tab.c.store.flex], Workload: work, Trial: trial}, run)
				if trial > 0 {
					if err := enc.Encode(r); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		t.Logf("completed SQL %s (%d rows validated per run)", work, len(want))
	}
	// Factorial scan comparison: both readers fetch exact HASH fields through
	// the same store.fetchRows helper. No server sorts/reducers are included.
	for _, work := range []string{"selective", "broad", "wide"} {
		for trial := 0; trial <= trials; trial++ {
			type mode struct {
				tab     table
				bounded bool
			}
			var modes []mode
			for _, tab := range tables {
				if target == "ram" {
					modes = append(modes, mode{tab, false})
				}
				modes = append(modes, mode{tab, true})
			}
			for offset := range modes {
				mode := modes[(offset+trial)%len(modes)]
				tab := mode.tab
				reader := "cursor"
				if mode.bounded {
					reader = "bounded"
				}
				fields := []string{"id", "g", "n"}
				if work == "wide" {
					for i := 0; i < 12; i++ {
						fields = append(fields, fmt.Sprintf("p%d", i))
					}
				}
				query := "*"
				lo := 1
				if work == "selective" {
					lo = n - 99
					query = fmt.Sprintf("@id:[%d %d]", lo, n)
				}
				run := func(ctx context.Context) int {
					st := &store{client: tab.c.store.client, flex: mode.bounded}
					req := &aggRequest{meta: tab.meta, index: tab.meta.index(), query: query, width: len(fields) + 2, load: []string{"__key", rowIDField}}
					if mode.bounded {
						req.load = append(req.load, fields...)
					}
					cur, err := st.openAggregate(ctx, req)
					if err != nil {
						t.Fatal(err)
					}
					defer cur.close(ctx)
					seen := make([]bool, n+1)
					count := 0
					for {
						rows, err := cur.next(ctx)
						if errors.Is(err, io.EOF) {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						if !mode.bounded {
							keys := make([]string, len(rows))
							for i, row := range rows {
								keys[i] = row["__key"]
							}
							rows, err = st.fetchRows(ctx, keys, fields)
							if err != nil {
								t.Fatal(err)
							}
						}
						for _, row := range rows {
							id, err := strconv.Atoi(row["id"])
							if err != nil || id < lo || id > n || seen[id] {
								t.Fatal("invalid/duplicate raw row")
							}
							seen[id] = true
							count++
							if row["g"] != strconv.Itoa(id%257) {
								t.Fatal("bad group")
							}
							w := ""
							if id%17 != 0 {
								w = fmt.Sprint(flexBenchScore(id))
							}
							if row["n"] != w {
								t.Fatal("bad exact number")
							}
							if work == "wide" {
								for i := 0; i < 12; i++ {
									if row[fmt.Sprintf("p%d", i)] != flexBenchPayload(id) {
										t.Fatal("bad payload")
									}
								}
							}
						}
					}
					if count != n-lo+1 {
						t.Fatalf("incomplete raw read %d", count)
					}
					return count
				}
				r := flexBenchMeasure(t, tab.c, flexBenchResult{Kind: "raw", Target: target, Schema: tab.schema, Reader: reader, Workload: work, Trial: trial}, run)
				if trial > 0 {
					if err := enc.Encode(r); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		t.Logf("completed factorial raw %s", work)
	}
	meta := map[string]any{"rows": n, "trials": trials, "warmup": 1, "go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "workers": 1, "payload_columns": 12, "payload_suffix_bytes": 120, "validation": "every field of every fully fetched row against independent formulas", "client_memory": "Go heap sampled at 1ms; process RSS is lifetime HWM including fixture setup", "server_counters": "sum of redis-server and dmcproxy process CPU, RSS and /proc/io endpoints; includes background work, not per-document flash reads"}
	b, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, target+"-metadata.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
