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
	"runtime"
	"testing"
	"time"
)

func TestWorkerTunerMeasuredWorkloads(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	for _, tc := range []struct {
		name                 string
		concurrency, optimum int
		cpu                  float64
	}{
		{"CPU bound", 1, 1, .95}, {"IO bound", 1, 6, .2}, {"diminishing returns", 1, 3, .2}, {"concurrent contention", 4, 2, .4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tuner := workerTuner{}
			for i := 0; i < 400; i++ {
				n := tuner.capacity(8)
				rate := float64(min(n, tc.optimum)) * 1000
				// Beyond the optimum, contention increases latency and reduces throughput.
				if n > tc.optimum {
					rate *= .8
				}
				tuner.observe(tuneObservation{Rows: 1000 * int64(tc.concurrency), Seconds: 1000 / rate, TailSeconds: 1000 / rate, CPUUtilization: tc.cpu, Concurrency: tc.concurrency}, 8)
			}
			if tuner.current < max(1, tc.optimum-1) || tuner.current > tc.optimum+1 || (tc.cpu < .9 && tuner.best != tc.optimum) {
				t.Fatalf("allocation %d want optimum %d or bounded probe", tuner.current, tc.optimum)
			}
		})
	}
}
func TestWorkerTunerNoiseCapacityAndPressure(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	tuner := workerTuner{}
	changes := 0
	for i := 0; i < 120; i++ {
		previous := tuner.capacity(8)
		noise := 1.0
		if i%2 == 0 {
			noise = 1.03
		}
		tuner.observe(tuneObservation{Rows: 1000, Seconds: noise, TailSeconds: noise}, 8)
		if tuner.current != previous {
			changes++
		}
		if tuner.current > 5 {
			t.Fatalf("noise caused unbounded growth: %d", tuner.current)
		}
	}
	if changes > 20 {
		t.Fatalf("oscillating under noise: %d changes", changes)
	}
	runtime.GOMAXPROCS(1)
	if n := tuner.capacity(2); n != 1 || tuner.best != 0 {
		t.Fatal("runtime change retained stale model")
	}
	runtime.GOMAXPROCS(4)
	tuner.capacity(8)
	tuner.current = 4
	tuner.observe(tuneObservation{Rows: 1000, Seconds: 1, TailSeconds: 1, MemoryPressure: .9}, 8)
	if tuner.current != 3 {
		t.Fatal("memory pressure did not reduce target")
	}
	if n := tuner.capacity(2); n != 2 {
		t.Fatal("operator ceiling ignored")
	}
}
func TestWorkerTunerCohortAndFailedCleanup(t *testing.T) {
	b := &workerBudget{}
	stats := &scanTelemetry{}
	stats.rows.Store(1000)
	start := b.begin("same workload")
	b.finish(start, stats, false) // reset comparison regime
	for i := 0; i < 3; i++ {
		start = b.begin("same workload")
		b.finish(start, stats, false)
	}
	if b.tuner.observations != 3 {
		t.Fatalf("observations %d", b.tuner.observations)
	}
	previous := b.tuner.observations
	a := b.begin("one")
	c := b.begin("two")
	b.finish(a, stats, false)
	b.finish(c, stats, false)
	if b.tuner.observations != previous {
		t.Fatal("trained on mixed work")
	}
	start = b.begin("same workload")
	b.finish(start, stats, true)
	if b.tuner.observations != previous || b.cohort.pending != 0 {
		t.Fatal("failed cohort trained or leaked")
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(b.option(OptionStringQueryWorkerTuning)), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["reason"] == nil || snapshot["signals"] == nil {
		t.Fatal("missing observability")
	}
}
func TestWorkerTunerContinuousTraffic(t *testing.T) {
	b := &workerBudget{}
	stats := &scanTelemetry{}
	stats.rows.Store(1000)
	anchor := b.begin("continuous")
	for i := 0; i < 40; i++ {
		start := b.begin("continuous")
		b.finish(start, stats, false)
	}
	if b.tuner.observations == 0 {
		t.Fatal("continuous traffic never updates")
	}
	b.finish(anchor, stats, false)
	if b.cohort.pending != 0 {
		t.Fatal("continuous window leaked queries")
	}
}
func TestWorkerTunerExplicitCeilings(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	b := &workerBudget{configured: 3}
	b.tuner = workerTuner{current: 8, cpu: 4}
	n, err := b.acquire(context.Background(), 0)
	if err != nil || n != 3 {
		t.Fatalf("explicit budget: %d %v", n, err)
	}
	b.release(n)
	b = &workerBudget{tuner: workerTuner{current: 6, cpu: 4}}
	n, err = b.acquire(context.Background(), 2)
	if err != nil || n != 2 {
		t.Fatalf("explicit query ceiling: %d %v", n, err)
	}
	b.release(n)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if n, err = b.acquire(ctx, 0); n != 0 || err != context.DeadlineExceeded {
		t.Fatal("cancelled admission")
	}
}

func BenchmarkWorkerTuningSignals(b *testing.B) {
	for b.Loop() {
		runtimeSignals()
	}
}
func BenchmarkWorkerTuningCohort(b *testing.B) {
	budget := &workerBudget{}
	stats := &scanTelemetry{}
	stats.rows.Store(1000)
	for b.Loop() {
		start := budget.begin("benchmark")
		budget.finish(start, stats, false)
	}
}

func TestWorkerTunerTailAndRedisChanges(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	tuner := workerTuner{current: 1, cpu: 4}
	for i := 0; i < tuningWindow; i++ {
		tuner.observe(tuneObservation{Rows: 1000, Seconds: 1, TailSeconds: 1}, 8)
	}
	if tuner.current != 2 {
		t.Fatal("missing bounded probe")
	}
	for i := 0; i < tuningWindow; i++ {
		tuner.observe(tuneObservation{Rows: 1000, Seconds: .5, TailSeconds: 1.3}, 8)
	}
	if tuner.current != 1 {
		t.Fatal("accepted throughput improvement with excessive tail cost")
	}
	for i := 0; i < tuningWindow; i++ {
		tuner.observe(tuneObservation{Rows: 1000, Seconds: 1, TailSeconds: 1}, 8)
	}
	if tuner.best != 1 || tuner.phase != "holding" {
		t.Fatalf("tail guard failed after reference verification: %+v", tuner)
	}
	// A deployment that previously sustained four workers now saturates at two.
	tuner = workerTuner{current: 4, cpu: 4, best: 4, bestScore: 4000, bestTail: .25}
	for i := 0; i < 300; i++ {
		n := tuner.capacity(8)
		rate := float64(min(n, 2)) * 1000
		if n > 2 {
			rate *= .7
		}
		tuner.observe(tuneObservation{Rows: 1000, Seconds: 1000 / rate, TailSeconds: 1000 / rate}, 8)
	}
	if tuner.current < 1 || tuner.current > 3 || tuner.best != 2 {
		t.Fatalf("did not reassess slower Redis: %d", tuner.current)
	}
}

func TestWorkerTunerPartitionBound(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	b := &workerBudget{tuner: workerTuner{current: 8, cpu: 4}}
	n, err := b.acquireBounded(context.Background(), 0, 2)
	if err != nil || n != 2 {
		t.Fatalf("small scan partition bound: %d %v", n, err)
	}
	b.release(n)
	if b.active != 0 {
		t.Fatal("bounded scan leaked capacity")
	}
}

func TestWorkerTunerRejectsDriftAndBursts(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	for _, drift := range []bool{false, true} {
		tuner := workerTuner{}
		for phase := 0; phase < 3; phase++ {
			for sample := 0; sample < tuningWindow; sample++ {
				seconds := 1.0
				if phase == 1 {
					seconds = .5
				}
				if drift && phase == 2 {
					seconds = .5
				}
				if !drift && phase == 1 && sample%2 == 0 {
					seconds = 2
				}
				tuner.observe(tuneObservation{Rows: 1000, Seconds: seconds, TailSeconds: seconds}, 8)
			}
		}
		if tuner.best != 4 || tuner.current != 4 || tuner.phase != "holding" {
			t.Fatalf("drift=%v trained on incomparable measurements: %+v", drift, tuner)
		}
	}
	// A slowdown starts a probe; it cannot repeatedly ratchet the retained model.
	tuner := workerTuner{current: 4, cpu: 4, best: 4, bestScore: 1000, bestTail: 1, phase: "holding", cooldown: tuningHold}
	for i := 0; i < tuningWindow; i++ {
		tuner.observe(tuneObservation{Rows: 1000, Seconds: 2, TailSeconds: 2}, 8)
	}
	if tuner.best != 4 || tuner.current != 3 || tuner.phase != "probe" {
		t.Fatalf("slowdown changed retained allocation without a comparison: %+v", tuner)
	}
}

func TestWorkerTunerWorkloadCache(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	b := &workerBudget{}
	stats := &scanTelemetry{}
	stats.rows.Store(1000)
	lease := b.begin("first")
	b.finish(lease, stats, false)
	b.tuner.best, b.tuner.current, b.tuner.phase, b.tuner.samples = 2, 3, "probe", 3
	lease = b.begin("second")
	b.finish(lease, stats, false)
	lease = b.begin("first")
	b.finish(lease, stats, false)
	if b.tuner.current != 2 || b.tuner.phase != "baseline" || b.tuner.samples != 0 {
		t.Fatalf("did not restore retained model with a fresh comparison: %+v", b.tuner)
	}
	for i := 0; i < 12; i++ {
		b.cacheModel(fmt.Sprint(i), workerTuner{current: 4, cpu: 4})
	}
	if len(b.models) != 8 || len(b.modelOrder) != 8 {
		t.Fatal("unbounded model cache")
	}
	if err := b.configure("auto"); err != nil {
		t.Fatal(err)
	}
	if len(b.models) != 0 || b.regime != "" {
		t.Fatal("operator reset preserved stale workload models")
	}
}

func TestWorkerTunerContinuousTransitionRecovery(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	b := &workerBudget{}
	stats := &scanTelemetry{}
	stats.rows.Store(1000)
	stale := b.begin("old workload")
	for i := 0; i < 24; i++ {
		lease := b.begin("new workload")
		b.finish(lease, stats, false)
	}
	if b.tuner.observations != 0 {
		t.Fatal("trained while mixed old work remained")
	}
	b.finish(stale, stats, false)
	anchor := b.begin("new workload")
	for i := 0; i < 24; i++ {
		lease := b.begin("new workload")
		b.finish(lease, stats, false)
	}
	if b.tuner.observations == 0 {
		t.Fatal("mixed marker remained sticky under continuous traffic")
	}
	// A live runtime change makes outstanding leases incomparable, then recovers.
	runtime.GOMAXPROCS(2)
	for i := 0; i < 16; i++ {
		lease := b.begin("new workload")
		b.finish(lease, stats, false)
	}
	if b.tuner.observations != 0 {
		t.Fatal("trained across an allocation generation")
	}
	b.finish(anchor, stats, false)
	anchor = b.begin("new workload")
	for i := 0; i < 24; i++ {
		lease := b.begin("new workload")
		b.finish(lease, stats, false)
	}
	if b.tuner.observations == 0 {
		t.Fatal("allocation transition did not recover")
	}
	b.finish(anchor, stats, false)
	if len(b.pendingKeys) != 0 || len(b.pendingGenerations) != 0 || b.cohort.pending != 0 {
		t.Fatal("lease accounting leaked")
	}
}

func TestWorkerTunerResetWithOutstandingLease(t *testing.T) {
	b := &workerBudget{}
	stats := &scanTelemetry{}
	stats.rows.Store(1000)
	old := b.begin("same")
	if err := b.configure("auto"); err != nil {
		t.Fatal(err)
	}
	current := b.begin("same")
	b.finish(old, stats, false)
	b.finish(current, stats, false)
	if b.tuner.observations != 0 {
		t.Fatal("reset accepted an old observation with the same allocation")
	}
	for i := 0; i < 3; i++ {
		lease := b.begin("same")
		b.finish(lease, stats, false)
	}
	if b.tuner.observations != 2 {
		t.Fatal("reset did not recover")
	}
}
