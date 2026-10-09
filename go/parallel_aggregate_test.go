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
	"math"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestMergeExactSumPartitions(t *testing.T) {
	cases := [][]float64{{1e300, 1, -1e300, 0.25, -0.125}, {math.SmallestNonzeroFloat64, 1, -1}, {math.Inf(1), 2}, {math.Inf(1), math.Inf(-1)}, {math.NaN(), 1}}
	for _, values := range cases {
		serial := &exactSum{}
		parts := make([]exactSum, 4)
		for i, v := range values {
			x := floatValue(typeFloat64, v)
			serial.add(x)
			parts[i%len(parts)].add(x)
		}
		combined := &exactSum{}
		for i := range parts {
			mergeExactSum(combined, &parts[i])
		}
		want, _ := serial.sumValue(typeFloat64)
		got, _ := combined.sumValue(typeFloat64)
		if !(math.IsNaN(want.F) && math.IsNaN(got.F)) && math.Float64bits(want.F) != math.Float64bits(got.F) {
			t.Fatalf("%v: got %v want %v", values, got, want)
		}
		if combined.n != serial.n {
			t.Fatal("lost input count")
		}
	}
}

func TestDefaultQueryWorkers(t *testing.T) {
	for _, n := range []int{1, 4, 16} {
		old := runtime.GOMAXPROCS(n)
		got := effectiveQueryWorkers(0)
		runtime.GOMAXPROCS(old)
		if got != min(32, n*2) {
			t.Fatalf("GOMAXPROCS %d: got %d", n, got)
		}
	}
	if got := effectiveQueryWorkers(4); got != 4 {
		t.Fatalf("explicit override: %d", got)
	}
}

func TestQueryWorkersOption(t *testing.T) {
	d := NewDriver(memory.DefaultAllocator)
	for _, opts := range []map[string]string{{}, {OptionStringQueryWorkers: "auto"}} {
		db, err := d.NewDatabaseWithContext(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		got, err := db.(adbc.GetSetOptionsWithContext).GetOption(context.Background(), OptionStringQueryWorkers)
		if err != nil || got != fmt.Sprint(effectiveQueryWorkers(0)) {
			t.Fatalf("automatic workers %q, %v", got, err)
		}
	}
	for _, v := range []string{"0", "33", "-1", "bad"} {
		if _, err := d.NewDatabaseWithContext(context.Background(), map[string]string{OptionStringQueryWorkers: v}); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	db, err := d.NewDatabaseWithContext(context.Background(), map[string]string{OptionStringQueryWorkers: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.(adbc.GetSetOptionsWithContext).GetOption(context.Background(), OptionStringQueryWorkers); err != nil || got != "4" {
		t.Fatalf("option %q, %v", got, err)
	}
}

func TestSQLParallelAggregates(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_parallel_aggregate")
	h.exec("CREATE TABLE it_parallel_aggregate (v BIGINT, f DOUBLE PRECISION, g INTEGER)")
	st := &store{client: h.rawClient()}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_parallel_aggregate")
	if err != nil {
		t.Fatal(err)
	}
	const n = 10003
	for start := 0; start < n; start += pipelineChunk {
		pipe := st.client.Pipeline()
		for i := start; i < min(start+pipelineChunk, n); i++ {
			fields := []any{rowIDField, i + 1, "g", i % 7}
			if i%5 != 0 {
				v := int64(9007199254740993)
				if i%2 == 1 {
					v = -v
				}
				fields = append(fields, "v", fmt.Sprint(v-int64(i)), "f", fmt.Sprint(float64(i)/7))
			}
			pipe.HSet(h.ctx, fmt.Sprint(meta.prefix(), i+1), fields...)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.client.Set(h.ctx, seqKey(meta.Schema, meta.Name), n, 0).Err(); err != nil {
		t.Fatal(err)
	}
	// Include a hole across a partition boundary and rows whose inputs are NULL.
	st.client.Del(h.ctx, fmt.Sprint(meta.prefix(), 2500), fmt.Sprint(meta.prefix(), 5001))
	newHarness := func(workers string) *sqlHarness {
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringQueryWorkers: workers, OptionStringAggregatePushdown: PushdownNone})
		if err != nil {
			t.Fatal(err)
		}
		c, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close(h.ctx); db.Close(h.ctx) })
		return &sqlHarness{t: t, ctx: h.ctx, conn: c}
	}
	serial, parallel, automatic := newHarness("1"), newHarness("4"), newHarness("auto")
	queries := []string{
		"SELECT COUNT(*), COUNT(v), SUM(v), AVG(v), MIN(v), MAX(v), SUM(f), AVG(f) FROM it_parallel_aggregate",
		"SELECT COUNT(*), SUM(v), AVG(f) FROM it_parallel_aggregate WHERE g <> 3",
		"SELECT COUNT(*), SUM(v), AVG(v), MIN(v), MAX(v) FROM it_parallel_aggregate WHERE g < 0",
		"SELECT g, COUNT(*), SUM(v) FROM it_parallel_aggregate GROUP BY g ORDER BY g",
		"SELECT COUNT(DISTINCT v), SUM(v+1) FROM it_parallel_aggregate",
	}
	for _, q := range queries {
		want, _ := serial.query(q)
		got, _ := parallel.query(q)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s\ngot %v\nwant %v", q, got, want)
		}
		got, _ = automatic.query(q)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("automatic %s: %v want %v", q, got, want)
		}
	}
	// Normal execution trains the automatic coordinator without replaying SQL.
	autoOptions := automatic.conn.(adbc.GetSetOptionsWithContext)
	expected, _ := serial.query(queries[0])
	for i := 0; i < 12; i++ {
		got, _ := automatic.query(queries[0])
		if !reflect.DeepEqual(got, expected) {
			t.Fatal("tuning changed SQL results")
		}
	}
	snapshot, _ := autoOptions.GetOption(h.ctx, OptionStringQueryWorkerTuning)
	var state struct {
		Observations uint64
		Signals      tuneObservation
	}
	if err := json.Unmarshal([]byte(snapshot), &state); err != nil || state.Observations == 0 || state.Signals.Rows == 0 {
		t.Fatalf("automatic scans did not record actual work: %s %v", snapshot, err)
	}
	// Four connections from one database share two tokens, including the
	// explicit ceiling (four) which must never bypass the shared limit.
	shared, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringQueryWorkers: "4",
		OptionStringQueryWorkerBudget: "2", OptionStringAggregatePushdown: PushdownNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shared.Close(h.ctx) })
	connections := make([]*sqlHarness, 4)
	for i := range connections {
		c, err := shared.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close(h.ctx) })
		connections[i] = &sqlHarness{t: t, ctx: h.ctx, conn: c}
	}
	want, _ := serial.query(queries[0])
	var wg sync.WaitGroup
	for _, c := range connections {
		wg.Add(1)
		go func(c *sqlHarness) {
			defer wg.Done()
			got, _ := c.query(queries[0])
			if !reflect.DeepEqual(got, want) {
				t.Errorf("concurrent aggregate: %v want %v", got, want)
			}
		}(c)
	}
	wg.Wait()
	if shared.workerBudget.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatal("success leaked workers")
	}
	for _, c := range connections {
		opts := c.conn.(adbc.GetSetOptionsWithContext)
		if got, err := opts.GetOption(h.ctx, OptionStringQueryWorkerBudget); err != nil || got != "2" {
			t.Fatalf("connection budget: %s %v", got, err)
		}
		value, err := opts.GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
		grant, _ := strconv.Atoi(value)
		if err != nil || grant < 1 || grant > 2 {
			t.Fatalf("connection grant bypassed budget: %s %v", value, err)
		}
	}
	// Cancel a real statement after it has reached the admission queue.
	held, _ := shared.workerBudget.acquire(h.ctx, 2)
	ctx, cancel := context.WithCancel(h.ctx)
	stmnt, err := connections[0].conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmnt.Close(h.ctx)
	if err := stmnt.SetSqlQuery(ctx, queries[0]); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 1)
	go func() {
		rdr, _, err := stmnt.ExecuteQuery(ctx)
		if rdr != nil {
			rdr.Release()
		}
		errs <- err
	}()
	waitQueued(t, shared.workerBudget, 1)
	cancel()
	select {
	case err := <-errs:
		if !isStatus(err, adbc.StatusCancelled) {
			t.Fatalf("cancelled query status: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled query did not leave queue")
	}
	shared.workerBudget.release(held)
	if shared.workerBudget.option(OptionIntQueryWorkersActive) != "0" || shared.workerBudget.option(OptionIntQueryWorkersQueued) != "0" {
		t.Fatal("cancellation leaked workers")
	}
	// A missing index makes the worker scan fail after admission.
	if err := st.client.Do(h.ctx, "FT.DROPINDEX", meta.index()).Err(); err != nil {
		t.Fatal(err)
	}
	e := &executor{store: st, queryWorkers: 4, workerBudget: shared.workerBudget}
	_, handled, scanErr := e.parallelAggregate(h.ctx, &selectPlan{meta: meta, sel: &SelectStmt{}}, wherePlan{query: "*"}, []*Func{{Name: "SUM", Args: []Expr{&ColumnRef{Name: "v"}}}}, nil)
	if !handled || scanErr == nil {
		t.Fatalf("expected admitted scan error: handled=%v err=%v", handled, scanErr)
	}

	if shared.workerBudget.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatal("error leaked workers")
	}

	e.queryWorkers = 0
	e.workerBudget = &workerBudget{}
	_, handled, scanErr = e.parallelAggregate(h.ctx, &selectPlan{meta: meta, sel: &SelectStmt{}}, wherePlan{query: "*"}, []*Func{{Name: "SUM", Args: []Expr{&ColumnRef{Name: "v"}}}}, nil)
	if !handled || scanErr == nil || e.workerBudget.cohort.pending != 0 || e.workerBudget.active != 0 || e.workerBudget.tuner.observations != 0 {
		t.Fatal("automatic error cleanup leaked or trained on failed query")
	}
}
