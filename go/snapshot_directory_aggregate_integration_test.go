// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func directoryAggregatePlan(t *testing.T, h *sqlHarness, s *store, ctx context.Context, sql string, workers int, budget *workerBudget) (*executor, *selectPlan, wherePlan) {
	t.Helper()
	e := &executor{store: s, schema: defaultSchema, pushdown: PushdownNone, cache: newExecCache(), now: time.Now().UTC(), queryWorkers: workers, workerBudget: budget, workerGrant: &atomic.Int64{}}
	parsed, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	wp, err := e.planWhere(ctx, plan.sel.Where, plan.meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, plan, wp
}

func TestSnapshotDirectoryAggregateExactAndPinned(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: strconv.FormatBool(os.Getenv("REDIS_SNAPSHOT_FLEX") == "true"), OptionStringQueryWorkers: "4", OptionStringQueryWorkerBudget: "4", OptionStringAggregatePushdown: PushdownNone})
	h.exec("CREATE TABLE t(id BIGINT, v BIGINT, d DECIMAL(30,4), f DOUBLE PRECISION)")
	h.exec("INSERT INTO t SELECT i,CASE WHEN i%5=0 THEN NULL WHEN i%2=0 THEN 9007199254740993 ELSE -9007199254740993 END,CAST(i AS DECIMAL(30,4)),CAST(i AS DOUBLE PRECISION)/7 FROM generate_series(1,4096) AS g(i)")
	h.exec("UPDATE t SET v=7,d=NULL WHERE id IN (1024,2048,4096)")
	h.exec("DELETE FROM t WHERE id IN (1,1025,2049,3073)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	queries := []string{
		"SELECT COUNT(*),COUNT(v),SUM(v),AVG(v),MIN(v),MAX(v),SUM(d),AVG(d),SUM(f),AVG(f) FROM t",
		"SELECT COUNT(*),SUM(v),SUM(d) FROM t WHERE id>=1024 AND id<=3073",
		"SELECT COUNT(*),SUM(v) FROM t WHERE v<0",
		"SELECT COUNT(*),SUM(v),MIN(d),MAX(f) FROM t WHERE id<0",
	}
	for _, sql := range queries {
		e, plan, wp := directoryAggregatePlan(t, h, s, ctx, sql, 4, &workerBudget{configured: 4})
		want, err := e.serialDriverAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil {
			t.Fatal(err)
		}
		got, handled, err := e.parallelAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil || !handled || !directoryAggregateResultsEqual(got, want) {
			t.Fatalf("%s: handled=%v err=%v got=%v want=%v", sql, handled, err, got, want)
		}
		if grant := e.workerGrant.Load(); grant != 4 {
			t.Fatalf("expected four directory workers, got %d", grant)
		}
		if e.workerBudget.active != 0 || len(e.workerBudget.queue) != 0 {
			t.Fatal("worker grant leaked")
		}
	}
	h.expectRows("SELECT COUNT(*),COUNT(DISTINCT id),SUM(id) FROM t", "4092|4092|8384508")
	// Later writes and compaction cannot alter the original generation held by workers.
	h.exec("UPDATE t SET v=99 WHERE id<=2000")
	snapshotDrainCompactionTest(t, s)
	e, plan, wp := directoryAggregatePlan(t, h, s, ctx, queries[0], 4, &workerBudget{configured: 4})
	original, err := e.serialDriverAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, handled, err := e.parallelAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
	if err != nil || !handled || !directoryAggregateResultsEqual(got, original) {
		t.Fatal("publication changed pinned aggregate", handled, err)
	}
}

func TestSnapshotDirectoryAggregateHighRowIDs(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_FLEX") == "true" {
		t.Skip("Flex allocator retains its 2^53 ceiling")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	s := snapshotTestStore(t, raw)
	work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "INSERT INTO t VALUES(0,0)")
	seed := int64(9007199254740993)
	if err := s.redis(work).Set(work, seqKey(defaultSchema, "t"), seed, 0).Err(); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, work, "INSERT INTO t VALUES(1,17),(2,23)")
	if err := s.publishSnapshot(work, pin, id); err != nil {
		t.Fatal(err)
	}
	pin.release()
	ctx, reader, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.release()
	sql := fmt.Sprintf("SELECT COUNT(*),SUM(v),MIN(__rowid),MAX(__rowid) FROM t WHERE __rowid>%d", seed)
	e, plan, wp := directoryAggregatePlan(t, h, s, ctx, sql, 4, &workerBudget{configured: 4})
	groups, handled, err := e.parallelAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
	if err != nil || !handled {
		t.Fatal(handled, err)
	}
	result := groups[0].results
	aggs := plan.aggregateCalls()
	if result[aggs[0]].I != 2 || result[aggs[1]].Text() != "40" || result[aggs[2]].I != seed+1 || result[aggs[3]].I != seed+2 {
		t.Fatal("high row IDs rounded or lost", result)
	}
	for _, sql := range []string{
		"SELECT COUNT(*),SUM(v) FROM t WHERE id=2",
		fmt.Sprintf("SELECT COUNT(*),SUM(v) FROM t WHERE id=2 AND __rowid>%d", seed),
	} {
		e, p, w := directoryAggregatePlan(t, h, s, ctx, sql, 4, &workerBudget{configured: 4})
		if p.meta.SnapshotOverlay || w.query == "*" || w.original == nil {
			t.Fatal("fixture must cover pushed baseline predicate", p.meta.SnapshotOverlay, w.query, w.original)
		}
		got, handled, err := e.parallelAggregate(ctx, p, w, p.aggregateCalls(), nil)
		if err != nil || !handled {
			t.Fatal(sql, handled, err)
		}
		aggs := p.aggregateCalls()
		if got[0].results[aggs[0]].Text() != "1" || got[0].results[aggs[1]].Text() != "23" {
			t.Fatal("pushed predicate was broadened", sql, got)
		}
		w.original = nil
		if _, handled, err := e.parallelAggregate(ctx, p, w, p.aggregateCalls(), nil); !handled || !isStatus(err, adbc.StatusInvalidState) {
			t.Fatal("missing full predicate did not fail closed", handled, err)
		}
	}

}

func TestSnapshotDirectoryAggregateUnsupportedShapes(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,4096) AS g(i)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	for _, sql := range []string{"SELECT COUNT(DISTINCT v) FROM t", "SELECT id,COUNT(*) FROM t GROUP BY id", "SELECT COUNT(*) FILTER(WHERE v>0) FROM t"} {
		e, plan, wp := directoryAggregatePlan(t, h, s, ctx, sql, 4, &workerBudget{configured: 4})
		if _, handled, err := e.parallelAggregate(ctx, plan, wp, plan.aggregateCalls(), nil); handled || err != nil {
			t.Fatal("unsupported shape admitted", sql, handled, err)
		}
	}
}

type directoryAggregateObservation struct{}
type directoryAggregateBarrier struct {
	snapshotHook
	ready     chan struct{}
	release   chan struct{}
	entered   atomic.Int64
	cancelled atomic.Int64
	failure   string
}

func (h *directoryAggregateBarrier) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if ctx.Value(directoryAggregateObservation{}) == true && cmd.Name() == "zlexcount" {
			ordinal := h.entered.Add(1)
			if ordinal == 4 {
				close(h.ready)
			}
			select {
			case <-h.ready:
			case <-ctx.Done():
				// Cancellation can win this select even after ready closes.
				// Count peers that leave here as well as at the later wait.
				h.cancelled.Add(1)
				cmd.SetErr(ctx.Err())
				return ctx.Err()
			}
			if h.failure != "" && ordinal == 1 {
				var err error = controlScriptNativeError("NOPERM directory aggregate permission refusal")
				if h.failure == "transport" {
					err = io.ErrUnexpectedEOF
				}
				cmd.SetErr(err)
				return err
			}
			if h.failure != "" {
				<-ctx.Done()
				h.cancelled.Add(1)
				cmd.SetErr(ctx.Err())
				return ctx.Err()
			}
			select {
			case <-h.release:
			case <-ctx.Done():
				h.cancelled.Add(1)
				cmd.SetErr(ctx.Err())
				return ctx.Err()
			}
		}
		return next(ctx, cmd)
	}
}

func TestDirectoryAggregateBarrierEarlyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), directoryAggregateObservation{}, true))
	cancel()
	hook := &directoryAggregateBarrier{ready: make(chan struct{}), release: make(chan struct{})}
	cmd := goredis.NewIntCmd(ctx, "zlexcount", "unused", "-", "+")
	err := hook.ProcessHook(func(context.Context, goredis.Cmder) error {
		t.Fatal("canceled barrier reached Redis")
		return nil
	})(ctx, cmd)
	if err != context.Canceled || hook.cancelled.Load() != 1 {
		t.Fatalf("early cancellation: %v, observed %d", err, hook.cancelled.Load())
	}
}

func TestSnapshotDirectoryAggregateFailureJoinsWorkers(t *testing.T) {
	for _, mode := range []string{"permission", "transport", "cancel", "expired_pin"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
			h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,4096) AS g(i)")
			s := snapshotTestStore(t, raw)
			work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=v")
			hook := &directoryAggregateBarrier{ready: make(chan struct{}), release: make(chan struct{})}
			if mode == "permission" || mode == "transport" {
				hook.failure = mode
			}
			s.client.AddHook(hook)
			budget := &workerBudget{configured: 4}
			workers := 4
			if mode == "expired_pin" {
				previousCPU := runtime.GOMAXPROCS(4)
				defer runtime.GOMAXPROCS(previousCPU)
				budget = &workerBudget{}
				workers = 0
			}
			e, plan, wp := directoryAggregatePlan(t, h, s, work, "SELECT SUM(v),COUNT(*) FROM t WHERE id>0", workers, budget)
			run, cancel := context.WithTimeout(context.WithValue(work, directoryAggregateObservation{}, true), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, handled, err := e.parallelAggregate(run, plan, wp, plan.aggregateCalls(), nil)
				if !handled && err == nil {
					err = fmt.Errorf("not handled")
				}
				done <- err
			}()
			select {
			case <-hook.ready:
			case err := <-done:
				t.Fatal("workers did not overlap", err)
			case <-run.Done():
				t.Fatal(run.Err())
			}
			switch mode {
			case "cancel":
				cancel()
			case "expired_pin":
				doc, err := raw.HGet(h.ctx, snapshotCatalogKey, "p:"+pin.token).Bytes()
				if err != nil {
					t.Fatal(err)
				}
				var p map[string]any
				if err := json.Unmarshal(doc, &p); err != nil {
					t.Fatal(err)
				}
				p["expires"] = 0
				doc, _ = json.Marshal(p)
				if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+pin.token, doc).Err(); err != nil {
					t.Fatal(err)
				}
				close(hook.release)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("failed read returned success")
				}
				if mode == "permission" && !strings.Contains(err.Error(), "permission refusal") {
					t.Fatal("peer cancellation hid the source failure", err)
				}
				if mode == "cancel" && !isStatus(err, adbc.StatusCancelled) {
					t.Fatal("cancellation status", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("worker failure did not cancel/join peers")
			}
			if budget.active != 0 || len(budget.queue) != 0 {
				t.Fatal("failed query retained grant")
			}
			if mode == "expired_pin" && (budget.tuner.observations != 0 || budget.cohort.pending != 0) {
				t.Fatal("expired aggregate trained the automatic tuner")
			}
			if (mode == "permission" || mode == "transport") && hook.cancelled.Load() != 3 {
				t.Fatal("not all sibling workers observed cancellation", hook.cancelled.Load())
			}
			s.abortSnapshot(pin, id)
			pin.release()
		})
	}
}

func directoryAggregateResultsEqual(a, b []aggGroup) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i].results) != len(b[i].results) || !reflect.DeepEqual(a[i].rep, b[i].rep) {
			return false
		}
		for f, x := range a[i].results {
			y, ok := b[i].results[f]
			if !ok || !reflect.DeepEqual(x.T, y.T) || x.Null != y.Null {
				return false
			}
			if x.Null {
				continue
			}
			if x.T.Kind.isFloat() {
				if math.IsNaN(x.F) && math.IsNaN(y.F) {
					continue
				}
				if math.Float64bits(x.F) != math.Float64bits(y.F) {
					return false
				}
			} else if x.Text() != y.Text() {
				return false
			}
		}
	}
	return true
}

func TestSnapshotDirectoryAggregateSpecialValues(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT,d DECIMAL(30,4),f DOUBLE PRECISION)")
	h.exec("INSERT INTO t SELECT i,CASE WHEN i%2=0 THEN 9007199254740993 ELSE -9007199254740993 END,CAST(i AS DECIMAL(30,4)),CASE WHEN i%5=0 THEN NULL ELSE CAST(i AS DOUBLE PRECISION) END FROM generate_series(1,4096) AS g(i)")
	h.exec("UPDATE t SET f='NaN'::DOUBLE PRECISION WHERE id=2")
	h.exec("UPDATE t SET f='Infinity'::DOUBLE PRECISION WHERE id=1025")
	h.exec("UPDATE t SET f='-Infinity'::DOUBLE PRECISION WHERE id=2049")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	for _, sql := range []string{
		"SELECT COUNT(*),COUNT(f),SUM(v),AVG(v),SUM(d),AVG(d),SUM(f),AVG(f),MIN(f),MAX(f) FROM t",
		"SELECT COUNT(*),SUM(f),AVG(f),MIN(f),MAX(f) FROM t WHERE id>2",
	} {
		e, plan, wp := directoryAggregatePlan(t, h, s, ctx, sql, 4, &workerBudget{configured: 4})
		want, err := e.serialDriverAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil {
			t.Fatal(err)
		}
		got, handled, err := e.parallelAggregate(ctx, plan, wp, plan.aggregateCalls(), nil)
		if err != nil || !handled || !directoryAggregateResultsEqual(got, want) {
			t.Fatal("special aggregate values differ", sql, handled, err)
		}
	}
}

func TestSnapshotDirectoryAggregateBudgetAndAutotuning(t *testing.T) {
	h, _, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: strconv.FormatBool(os.Getenv("REDIS_SNAPSHOT_FLEX") == "true"), OptionStringQueryWorkers: "auto", OptionStringQueryWorkerBudget: "4", OptionStringAggregatePushdown: PushdownNone})
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,4096) AS g(i)")
	h.exec("UPDATE t SET v=7 WHERE id=1024")
	for range 10 {
		h.expectRows("SELECT COUNT(*),SUM(v) FROM t", "4096|8389639")
	}
	opts := h.conn.(adbc.GetSetOptionsWithContext)
	raw, err := opts.GetOption(h.ctx, OptionStringQueryWorkerTuning)
	if err != nil {
		t.Fatal(err)
	}
	var tuning struct {
		Observations uint64
		Signals      tuneObservation
	}
	if err := json.Unmarshal([]byte(raw), &tuning); err != nil || tuning.Observations == 0 || tuning.Signals.Rows == 0 {
		t.Fatal("automatic directory workers did not observe rows", raw, err)
	}
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	impl := st.(*statementImpl)
	budget := impl.conn.workerBudget
	held, err := budget.acquire(h.ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	if err := st.SetSqlQuery(h.ctx, "SELECT SUM(v) FROM t"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		reader, _, err := st.ExecuteQuery(ctx)
		if reader != nil {
			reader.Release()
		}
		done <- err
	}()
	waitQueued(t, budget, 1)
	cancel()
	select {
	case err := <-done:
		if !isStatus(err, adbc.StatusCancelled) {
			t.Fatal("queued cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued reader did not cancel")
	}
	budget.release(held)
	if budget.option(OptionIntQueryWorkersActive) != "0" || budget.option(OptionIntQueryWorkersQueued) != "0" {
		t.Fatal("queued cancellation leaked worker resources")
	}
}

func TestSnapshotDirectoryAggregateConcurrentPublication(t *testing.T) {
	// The supported plain-index option also runs on RAM, exercising the same
	// authoritative directory path there and on an actual Flex test backend.
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,4096) AS g(i)")
	s := snapshotTestStore(t, raw)
	s.flex = true
	hook := &directoryAggregateBarrier{ready: make(chan struct{}), release: make(chan struct{})}
	s.client.AddHook(hook)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	budget := &workerBudget{configured: 4}
	e, plan, wp := directoryAggregatePlan(t, h, s, ctx, "SELECT COUNT(*),SUM(v) FROM t", 4, budget)
	run, cancel := context.WithTimeout(context.WithValue(ctx, directoryAggregateObservation{}, true), 15*time.Second)
	defer cancel()
	type outcome struct {
		groups  []aggGroup
		handled bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		g, ok, err := e.parallelAggregate(run, plan, wp, plan.aggregateCalls(), nil)
		done <- outcome{g, ok, err}
	}()
	select {
	case <-hook.ready:
	case v := <-done:
		t.Fatal("workers did not overlap", v.err)
	case <-run.Done():
		t.Fatal(run.Err())
	}
	for _, sql := range []string{"UPDATE t SET v=9 WHERE id<=1024", "DELETE FROM t WHERE id IN (1025,2049)", "UPDATE t SET v=11 WHERE id>3072"} {
		h.exec(sql)
	}
	snapshotDrainCompactionTest(t, s)
	close(hook.release)
	select {
	case v := <-done:
		if v.err != nil || !v.handled {
			t.Fatal("pinned workers lost source", v.handled, v.err)
		}
		aggs := plan.aggregateCalls()
		if v.groups[0].results[aggs[0]].Text() != "4096" || v.groups[0].results[aggs[1]].Text() != "8390656" {
			t.Fatal("active workers observed later publication", v.groups)
		}
	case <-run.Done():
		t.Fatal(run.Err())
	}
	if budget.active != 0 || len(budget.queue) != 0 {
		t.Fatal("publication overlap leaked worker grant")
	}
	h.expectRows("SELECT COUNT(*) FROM t", "4094")
}
