// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestStarChunksBoundedExactRanges(t *testing.T) {
	for _, v := range []struct {
		last    int64
		workers int
	}{{0, 4}, {1, 4}, {2048, 0}, {2048, 33}} {
		if starFactChunkCount(v.last, v.workers) != 0 {
			t.Fatal("invalid configuration admitted", v)
		}
	}
	for _, last := range []int64{2048, 4097, 2880404, 9007199254740993, math.MaxInt64} {
		count := starFactChunkCount(last, 4)
		if count < 4 || count > int(maxStarChunks) {
			t.Fatal(count)
		}
		var previous int64
		for i := 0; i < count; i++ {
			r := starFactRange(last, count, i)
			if r.lo != previous+1 || r.hi < r.lo {
				t.Fatalf("gap/overflow last%d chunk%d: %+v after%d", last, i, r, previous)
			}
			previous = r.hi
		}
		if previous != math.MaxInt64 {
			t.Fatal("missing exact tail")
		}
	}
	if got := starFactChunkCount(2880404, 4); got != 11252 {
		t.Fatal(got)
	}
}

func TestStarChunksOrderedRoundRobinEmptyAndParallelProgress(t *testing.T) {
	const workers = 4
	const chunks = 20
	var started [chunks]atomic.Bool
	barrier := make(chan struct{})
	refilled := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var first atomic.Int64
	scan := func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
		started[chunk].Store(true)
		if chunk == workers {
			close(refilled)
		}
		if chunk < workers {
			if first.Add(1) == workers {
				close(barrier)
			}
			select {
			case <-barrier:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if chunk%5 == 0 {
			return nil
		}
		return consume([]map[string]Value{parallelFact(int64(chunk))})
	}
	var got []int64
	src, consumer := parallelStarJoinChunks(ctx, parallelStarShape(), workers, chunks, scan, func(rows []map[string]Value) error {
		if len(rows) > 0 && rows[0]["f.v"].I == 1 {
			select {
			case <-refilled:
			case <-ctx.Done():
				t.Error("worker did not advance to next small chunk")
				return ctx.Err()
			}
		}
		for _, row := range rows {
			got = append(got, row["f.v"].I)
		}
		return nil
	})
	var want []int64
	for i := 0; i < chunks; i++ {
		if !started[i].Load() {
			t.Fatal("chunk skipped", i)
		}
		if i%5 != 0 {
			want = append(want, int64(i), int64(i))
		}
	}
	if src != nil || consumer != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v %v %v", src, consumer, got)
	}
}

func TestStarChunksSourceErrorOrderAfterConsumer(t *testing.T) {
	earlier, later, reducer := errors.New("earlier chunk"), errors.New("later chunk"), errors.New("consumer")
	var done atomic.Int64
	scan := func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
		defer done.Add(1)
		if err := consume([]map[string]Value{parallelFact(int64(chunk))}); err != nil {
			return err
		}
		if chunk == 3 {
			return earlier
		}
		if chunk == 6 {
			return later
		}
		return nil
	}
	src, consumer := parallelStarJoinChunks(context.Background(), parallelStarShape(), 3, 12, scan, func([]map[string]Value) error { return reducer })
	if src != earlier || consumer != reducer || done.Load() != 12 {
		t.Fatalf("%v %v %d", src, consumer, done.Load())
	}
}

func TestStarChunksPanicAndCancellationJoinWorkers(t *testing.T) {
	for _, workerPanic := range []bool{false, true} {
		t.Run(map[bool]string{false: "consumer", true: "worker"}[workerPanic], func(t *testing.T) {
			var active atomic.Int64
			scan := func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
				active.Add(1)
				defer active.Add(-1)
				if workerPanic && chunk == 0 {
					panic("sentinel")
				}
				for i := 0; i < 10; i++ {
					if err := consume([]map[string]Value{parallelFact(int64(chunk))}); err != nil {
						return err
					}
				}
				return nil
			}
			func() {
				defer func() {
					if recover() != "sentinel" {
						t.Error("panic lost")
					}
				}()
				parallelStarJoinChunks(context.Background(), parallelStarShape(), 3, 12, scan, func([]map[string]Value) error { panic("sentinel") })
			}()
			if active.Load() != 0 {
				t.Fatal("worker retained after panic")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	var active atomic.Int64
	scan := func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
		active.Add(1)
		defer active.Add(-1)
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	src, _ := parallelStarJoinChunks(ctx, parallelStarShape(), 3, 12, scan, func([]map[string]Value) error { t.Fatal("callback after cancel"); return nil })
	if src == nil || active.Load() != 0 {
		t.Fatalf("%v active%d", src, active.Load())
	}
}

// Isolates CPU decode/probe/reducer scheduling. It deliberately excludes Redis,
// per-chunk iterator/readiness/control setup, so it cannot select a native policy.
func BenchmarkStarChunkCPU(b *testing.B) {
	const rows = 32768
	const workers = 4
	for _, chunkRows := range []int{256, 4096, rows / workers} {
		b.Run(fmt.Sprint(chunkRows), func(b *testing.B) {
			chunks := (rows + chunkRows - 1) / chunkRows
			shape := parallelStarShape()
			var starts atomic.Int64
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				count := 0
				src, consumer := parallelStarJoinChunks(context.Background(), shape, workers, chunks, func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
					starts.Add(1)
					hi := min((chunk+1)*chunkRows, rows)
					for lo := chunk * chunkRows; lo < hi; lo += 256 {
						page := make([]map[string]Value, 0, min(256, hi-lo))
						for i := lo; i < min(lo+256, hi); i++ {
							var decoded int64
							for j := 0; j < 32; j++ {
								v, err := strconv.ParseInt("123456789123", 10, 64)
								if err != nil {
									return err
								}
								decoded += v
							}
							row := parallelFact(int64(i))
							row["f.decode"] = intValue(typeInt64, decoded)
							page = append(page, row)
						}
						if err := consume(page); err != nil {
							return err
						}
					}
					return nil
				}, func(page []map[string]Value) error { count += len(page); return nil })
				if src != nil || consumer != nil || count != rows*2 {
					b.Fatalf("%v %v %d", src, consumer, count)
				}
			}
			b.ReportMetric(float64(starts.Load())/float64(b.N), "scan-starts/op")
		})
	}
}

// The historical GODEBUG setting makes recover return nil for panic(nil).
// A subprocess verifies the completion marker retains a separate panic bit.
func TestStarChunksNilPanic(t *testing.T) {
	if os.Getenv("ADBC_STAR_NIL_PANIC_CHILD") == "1" {
		var active atomic.Int64
		returned := false
		defer func() {
			_ = recover()
			if returned {
				t.Error("worker panic(nil) was suppressed")
			}
			if active.Load() != 0 {
				t.Error("worker retained after nil panic")
			}
		}()
		parallelStarJoinChunks(context.Background(), parallelStarShape(), 2, 4, func(ctx context.Context, chunk int, consume func([]map[string]Value) error) error {
			active.Add(1)
			defer active.Add(-1)
			if chunk == 0 {
				panic(nil)
			}
			return consume([]map[string]Value{parallelFact(int64(chunk))})
		}, func([]map[string]Value) error { return nil })
		returned = true
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestStarChunksNilPanic$", "-test.count=1")
	cmd.Env = append(os.Environ(), "ADBC_STAR_NIL_PANIC_CHILD=1", "GODEBUG=panicnil=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nil panic subprocess: %v\n%s", err, out)
	}
}

func TestStarChunkExactDirectoryPageHint(t *testing.T) {
	e, ctx, req, _ := parallelNativeStarFixture(t)
	m := *req.meta.join.items[1].view()
	m.SnapshotOverlay = true
	it, err := e.openScan(ctx, scanRequest{meta: &m, where: wherePlan{query: "*"}, ranges: &rowIDRangeScan{lo: 1, hi: 256, cap: 256}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	if it.delta == nil || it.delta.pageRows != 256 || it.delta.directoryPageRows != 256 {
		t.Fatal("exact directory hint not selected")
	}
}

func TestStarChunkBalancedPrefixBounds(t *testing.T) {
	for _, last := range []int64{2048, 4097, 2880404, 9007199254740993, math.MaxInt64} {
		chunks := starFactChunkCount(last, 4)
		var previous int64
		q := last / int64(chunks)
		for chunk := 0; chunk < chunks; chunk++ {
			bounds := starFactChunkRange(last, chunks, chunk)
			if bounds.lo != previous+1 || bounds.hi < bounds.lo {
				t.Fatalf("gap/overflow last%d chunk%d %+v", last, chunk, bounds)
			}
			if chunk < chunks-1 {
				n := bounds.hi - bounds.lo + 1
				if n != q && n != q+1 {
					t.Fatalf("unbalanced finite chunk last%d chunk%d rows%d quotient%d", last, chunk, n, q)
				}
			} else {
				if bounds.hi != math.MaxInt64 {
					t.Fatal("tail lost")
				}
				n := last - bounds.lo + 1
				if n != q && n != q+1 {
					t.Fatal("allocated tail imbalance", n, q)
				}
			}
			previous = bounds.hi
		}
	}
}
