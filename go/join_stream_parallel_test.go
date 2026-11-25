// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func parallelStarShape() *starShape {
	dim := starDimension{factCols: []string{"f.id"}, dimCols: []string{"d.id"}, buckets: map[string][]map[string]Value{}}
	one := map[string]Value{"d.id": intValue(typeInt64, 1), "d.v": stringValue("first")}
	two := map[string]Value{"d.id": intValue(typeInt64, 1), "d.v": stringValue("again")}
	key, _ := starRowKey(one, dim.dimCols)
	dim.buckets[key] = []map[string]Value{one, two}
	return &starShape{dims: []starDimension{dim}}
}
func parallelFact(v int64) map[string]Value {
	return map[string]Value{"f.id": intValue(typeInt64, 1), "f.v": intValue(typeInt64, v), "f.__key": stringValue(fmt.Sprint("original:", v))}
}

func TestParallelStarJoinOrderedBoundedBagsAndProvenance(t *testing.T) {
	shape := parallelStarShape()
	secondStarted := make(chan struct{})
	var done atomic.Int64
	scan := func(ctx context.Context, w int, consume func([]map[string]Value) error) error {
		defer done.Add(1)
		if w == 0 {
			select {
			case <-secondStarted:
			case <-ctx.Done():
				return ctx.Err()
			}
			return consume([]map[string]Value{parallelFact(0)})
		}
		close(secondStarted)
		rows := make([]map[string]Value, 351)
		for i := 0; i < 350; i++ {
			rows[i] = parallelFact(int64(i + 1))
		}
		rows[350] = map[string]Value{"f.id": nullValue(typeInt64)}
		return consume(rows)
	}
	count, maxPage := 0, 0
	last := int64(-1)
	src, consumer := parallelStarJoinPages(context.Background(), shape, 2, scan, func(rows []map[string]Value) error {
		maxPage = max(maxPage, len(rows))
		for _, r := range rows {
			v := r["f.v"].I
			if v < last {
				t.Fatal("partition completion reordered facts")
			}
			last = v
			if r["f.__key"].S != fmt.Sprint("original:", v) {
				t.Fatal("provenance lost")
			}
			count++
		}
		return nil
	})
	if src != nil || consumer != nil || count != 702 || maxPage > starOutputPage || done.Load() != 2 {
		t.Fatalf("source=%v consumer=%v count=%d max=%d done=%d", src, consumer, count, maxPage, done.Load())
	}
	if len(shape.dims[0].buckets) != 1 {
		t.Fatal("shared buckets mutated")
	}
}

func TestParallelStarJoinLaterSourceErrorPrecedesReducer(t *testing.T) {
	sourceErr, consumerErr := errors.New("later source reply lost"), errors.New("earlier reducer failed")
	var done atomic.Int64
	scan := func(_ context.Context, w int, consume func([]map[string]Value) error) error {
		defer done.Add(1)
		if err := consume([]map[string]Value{parallelFact(int64(w))}); err != nil {
			return err
		}
		if w == 1 {
			return sourceErr
		}
		return nil
	}
	calls := 0
	src, consumer := parallelStarJoinPages(context.Background(), parallelStarShape(), 2, scan, func([]map[string]Value) error { calls++; return consumerErr })
	if !errors.Is(src, sourceErr) || !errors.Is(consumer, consumerErr) || calls != 1 || done.Load() != 2 {
		t.Fatalf("source=%v consumer=%v calls=%d done=%d", src, consumer, calls, done.Load())
	}
}

func TestParallelStarJoinCancellationJoinsBlockedWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var done atomic.Int64
	scan := func(ctx context.Context, w int, consume func([]map[string]Value) error) error {
		defer done.Add(1)
		rows := make([]map[string]Value, 1000)
		for i := range rows {
			rows[i] = parallelFact(int64(w))
		}
		if err := consume(rows); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
	calls := 0
	src, _ := parallelStarJoinPages(ctx, parallelStarShape(), 2, scan, func([]map[string]Value) error { calls++; cancel(); return nil })
	if src == nil || done.Load() != 2 || calls != 1 {
		t.Fatalf("source=%v done=%d calls=%d", src, done.Load(), calls)
	}
}

func TestParallelStarFactRangesRemainExact(t *testing.T) {
	for _, last := range []int64{4097, 9007199254740993, math.MaxInt64} {
		var previous int64
		for w := 0; w < 3; w++ {
			r := starFactRange(last, 3, w)
			if r.lo != previous+1 || r.hi < r.lo {
				t.Fatalf("range gap/overlap: %+v after%d", r, previous)
			}
			previous = r.hi
		}
		if previous != math.MaxInt64 {
			t.Fatal("unbounded exact tail lost")
		}
	}
}

type parallelNativeReplies struct {
	*streamNativeReplies
	mu            sync.Mutex
	n             int64
	sourceError   error
	sequenceError error
	rangeReads    int
}

func (h *parallelNativeReplies) replyParallel(c goredis.Cmder) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch c.Name() {
	case "get":
		if h.sequenceError != nil {
			return h.sequenceError
		}
		c.(*goredis.StringCmd).SetVal(strconv.FormatInt(h.n, 10))
		return nil
	case "zlexcount":
		hi := strings.TrimPrefix(fmt.Sprint(c.Args()[3]), "(")
		lo, err := strconv.ParseInt(hi, 10, 64)
		if err != nil {
			return err
		}
		c.(*goredis.IntCmd).SetVal(min(h.n, lo-1))
		return nil
	case "zrange":
		h.rangeReads++
		if h.sourceError != nil {
			return h.sourceError
		}
		var lo, hi int64
		if from, count, ok := mockDirectorySeek(c.Args()); ok {
			id, err := strconv.ParseInt(from, 10, 64)
			if err != nil {
				return err
			}
			lo = max(id, 1) - 1
			hi = lo + count - 1
		} else {
			lo, hi = c.Args()[2].(int64), c.Args()[3].(int64)
		}
		var ids []string
		for i := lo; i <= hi && i < h.n; i++ {
			ids = append(ids, fmt.Sprintf("%020d", i+1))
		}
		c.(*goredis.StringSliceCmd).SetVal(ids)
		return nil
	}
	return h.reply(c)
}
func (h *parallelNativeReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error {
		err := h.replyParallel(c)
		if err != nil {
			c.SetErr(err)
		}
		return err
	}
}
func (h *parallelNativeReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		var es []error
		for _, c := range cs {
			if err := h.replyParallel(c); err != nil {
				c.SetErr(err)
				es = append(es, err)
			}
		}
		if len(es) == 1 {
			return es[0]
		}
		return errors.Join(es...)
	}
}

func parallelNativeStarFixture(t *testing.T) (*executor, context.Context, scanRequest, *parallelNativeReplies) {
	t.Helper()
	e, req := starTestFixture(23)
	d, ctx, base := streamNativeFixture(t)
	n := int64(4097)
	d.meta.SnapshotRows = &n
	d.meta.Columns = []columnMeta{{Name: "id", Type: typeInt64}, {Name: "v", Type: typeInt64}}
	seg := d.meta.snapshotSegments()[0]
	base.segments[seg.Index] = seg
	for i := int64(1); i <= n; i++ {
		base.rows[seg.Prefix+fmt.Sprint(i)] = aggRow{rowIDField: fmt.Sprint(i), "id": "1", "v": fmt.Sprint(i)}
	}
	hook := &parallelNativeReplies{streamNativeReplies: base, n: n}
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = c.Close() })
	c.AddHook(hook)
	d.store.client = c
	req.meta.join.items[1].base = d.meta
	e.store = d.store
	e.workerBudget = &workerBudget{configured: 2}
	e.queryWorkers = 2
	e.workerGrant = &atomic.Int64{}
	e.diagnostics = &executionDiagnostics{}
	return e, ctx, req, hook
}
func TestParallelStarNativeRangesGrantAndFailureCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			e, ctx, req, h := parallelNativeStarFixture(t)
			if fail {
				h.sourceError = errors.New("synthetic native source failure")
			}
			count := 0
			handled, err := e.scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error {
				count += len(rows)
				if len(rows) > starOutputPage {
					t.Fatal("unbounded callback")
				}
				return nil
			})
			if !handled || e.workerGrant.Load() != 2 || e.workerBudget.active != 0 || len(e.workerBudget.queue) != 0 {
				t.Fatalf("grant leak: handled%v err%v grant%d active%d", handled, err, e.workerGrant.Load(), e.workerBudget.active)
			}
			if fail {
				if err == nil || !strings.Contains(err.Error(), h.sourceError.Error()) {
					t.Fatalf("source failure lost: %v", err)
				}
			} else if err != nil || count != 8194 {
				t.Fatalf("range gaps/duplicates: count%d err%v", count, err)
			}
		})
	}
}

func TestParallelStarJoinBackpressureBoundsLaterWorker(t *testing.T) {
	var advanced atomic.Int64
	secondFull := make(chan struct{})
	scan := func(ctx context.Context, w int, consume func([]map[string]Value) error) error {
		if w == 0 {
			select {
			case <-secondFull:
			case <-ctx.Done():
				return ctx.Err()
			}
			if n := advanced.Load(); n > 256 {
				t.Errorf("later worker advanced past one queued+one pending output page: %d", n)
			}
			return consume([]map[string]Value{parallelFact(0)})
		}
		for i := 0; i < 1000; i++ {
			advanced.Add(1)
			if i == 255 {
				close(secondFull)
			}
			if err := consume([]map[string]Value{parallelFact(1)}); err != nil {
				return err
			}
		}
		return nil
	}
	count := 0
	src, consumer := parallelStarJoinPages(context.Background(), parallelStarShape(), 2, scan, func(rows []map[string]Value) error { count += len(rows); return nil })
	if src != nil || consumer != nil || count != 2002 {
		t.Fatalf("%v %v count%d", src, consumer, count)
	}
}

func TestParallelStarQueuedGrantCancellationKeepsOtherOwner(t *testing.T) {
	e, ctx, req, _ := parallelNativeStarFixture(t)
	held, err := e.workerBudget.acquire(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer e.workerBudget.release(held)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := e.scanStarJoinPages(ctx, req, nil, func([]map[string]Value) error { t.Error("queued canceled source emitted rows"); return nil })
		done <- err
	}()
	waitQueued(t, e.workerBudget, 1)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("queued cancellation lost")
	}
	if got := e.workerBudget.option(OptionIntQueryWorkersActive); got != "2" {
		t.Fatalf("canceled waiter stole another owner's tokens: %s", got)
	}
	if got := e.workerBudget.option(OptionIntQueryWorkersQueued); got != "0" {
		t.Fatalf("queued request leaked: %s", got)
	}
}

func TestParallelStarOneWorkerAndConsumerErrorReleaseGrant(t *testing.T) {
	e, ctx, req, _ := parallelNativeStarFixture(t)
	e.queryWorkers = 1
	sentinel := errors.New("consumer rejected page")
	handled, err := e.scanStarJoinPages(ctx, req, nil, func([]map[string]Value) error { return sentinel })
	if !handled || !errors.Is(err, sentinel) || e.workerGrant.Load() != 1 || e.workerBudget.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatalf("single grant cleanup: %v %v grant%d active%s", handled, err, e.workerGrant.Load(), e.workerBudget.option(OptionIntQueryWorkersActive))
	}
}

func TestParallelStarFactResidualRemainsComplete(t *testing.T) {
	e, ctx, req, _ := parallelNativeStarFixture(t)
	pred := &Binary{Op: ">=", L: &ColumnRef{Name: "f.v"}, R: &Literal{V: intValue(typeInt64, 4090)}}
	req.meta.join.items[1].pushed = []Expr{pred}
	req.where.residual = &Binary{Op: "AND", L: req.where.residual, R: pred}
	count := 0
	handled, err := e.scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error {
		for _, row := range rows {
			if row["f.v"].I < 4090 {
				t.Error("fact residual lost")
			}
			count++
		}
		return nil
	})
	if !handled || err != nil || count != 16 || e.workerGrant.Load() != 2 {
		t.Fatalf("residual range scan: %v %v count%d grant%d", handled, err, count, e.workerGrant.Load())
	}
}

func TestParallelStarUnknownSequenceReplyNeverFallback(t *testing.T) {
	e, ctx, req, h := parallelNativeStarFixture(t)
	h.sequenceError = errors.Join(goredis.Nil, errors.New("row sequence reply lost"))
	calls := 0
	handled, err := e.scanStarJoinPages(ctx, req, nil, func([]map[string]Value) error { calls++; return nil })
	if !handled || err == nil || !strings.Contains(err.Error(), "reply lost") || calls != 0 || e.workerBudget.option(OptionIntQueryWorkersActive) != "0" {
		t.Fatalf("ambiguous sequence reply retried: %v %v calls%d", handled, err, calls)
	}
}

func TestParallelStarSelectiveFactKeepsExistingSource(t *testing.T) {
	e, ctx, req, _ := parallelNativeStarFixture(t)
	req.meta.join.items[1].base.Columns[0].Indexed = true
	pred := &Binary{Op: "=", L: &ColumnRef{Name: "f.id"}, R: &Literal{V: intValue(typeInt64, 1)}}
	req.meta.join.items[1].pushed = []Expr{pred}
	shape, ok := e.planStarShape(ctx, req.meta.join, req.need, req.where.residual)
	if !ok {
		t.Fatal("filtered star preflight failed")
	}
	handled, err := e.scanParallelStarFacts(ctx, shape, req.meta.join, nil, func([]map[string]Value) error {
		t.Error("selective Search path consumed by broad parallel path")
		return nil
	})
	if handled || err != nil || e.workerGrant.Load() != 0 {
		t.Fatalf("selective path did not retain original scan: %v %v", handled, err)
	}
}

func TestParallelStarParsedExactAggregateMatchesEstablishedSerial(t *testing.T) {
	e, p := deferredTestPlan(t, `WITH d(id,g) AS (VALUES (1,'a'),(1,'a'),(2,'b')), f(id,v) AS (VALUES (1,CAST(10.05 AS DECIMAL(7,2))),(2,CAST(NULL AS DECIMAL(7,2))),(3,CAST(0.10 AS DECIMAL(7,2))),(4,CAST(1.00 AS DECIMAL(7,2)))) SELECT d.g,SUM(f.v),COUNT(*),AVG(f.v),SUM(CASE WHEN d.g='a' THEN (f.v-1.00)/2 ELSE NULL END) FROM d,f WHERE d.id=f.id GROUP BY d.g ORDER BY d.g`)
	native, ctx, req, h := parallelNativeStarFixture(t)
	fact := req.meta.join.items[1].base
	fact.Columns = []columnMeta{{Name: "id", Type: typeInt32}, {Name: "v", Type: decimalType(7, 2)}}
	for _, row := range h.rows {
		row["v"] = "10.05"
	}
	replaced := false
	for _, it := range p.meta.join.items {
		if _, ok := it.base.column("v"); ok {
			it.base = fact
			replaced = true
		}
	}
	if !replaced || !streamingStarAggregate(p, p.aggregateCalls()) {
		t.Fatal("real parsed decimal fixture not eligible")
	}
	e.store = native.store
	e.workerBudget = native.workerBudget
	e.queryWorkers = 2
	e.workerGrant = native.workerGrant
	e.diagnostics = native.diagnostics
	got, err := e.runSelect(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.workerGrant.Load() != 2 {
		t.Fatal("parsed native query silently used serial fallback")
	}
	e.workerBudget = nil
	want, err := e.runSelect(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0][1].Text() != "82349.70" || got[0][2].I != 8194 {
		t.Fatalf("parallel decimal/CASE results differ from established serial: got%v want%v", got, want)
	}
}
