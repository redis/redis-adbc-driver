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
	"encoding/json"
	"math"
	"runtime"
	"runtime/metrics"
	"strconv"
	"sync/atomic"
	"time"
)

// A trial uses three comparable, naturally executed cohorts. An improvement
// must exceed 8%; downward probes prefer fewer workers within 3% throughput.
// Throughput regressions >10% or tail regressions >15% roll back. Exploration costs at most one extra worker and never replays SQL.
type workerTuner struct {
	current, cpu, best, samples, stable, direction int
	score, tail, bestScore, bestTail               float64
	probing                                        bool
	reason                                         string
	observations                                   uint64
	last                                           tuneObservation
}
type tuneObservation struct {
	Rows           int64   `json:"rows"`
	Seconds        float64 `json:"seconds"`
	TailSeconds    float64 `json:"tail_seconds"`
	CPUUtilization float64 `json:"runtime_cpu_utilization"`
	MemoryPressure float64 `json:"runtime_memory_pressure"`
	FetchSeconds   float64 `json:"fetch_seconds"`
	Concurrency    int     `json:"concurrency"`
}

func (t *workerTuner) capacity(upper int) int {
	cpu := max(1, runtime.GOMAXPROCS(0))
	if t.current == 0 || t.cpu != cpu {
		*t = workerTuner{current: min(4, cpu, upper), cpu: cpu, reason: "runtime capacity initialization"}
	}
	t.current = max(1, min(t.current, upper))
	return t.current
}
func (t *workerTuner) observe(o tuneObservation, upper int) {
	t.capacity(upper)
	t.last = o
	t.observations++
	if o.Seconds <= 0 || o.Rows <= 0 {
		return
	}
	if o.MemoryPressure >= .85 || o.CPUUtilization >= .90 {
		t.current = max(1, t.current-1)
		t.best, t.samples, t.stable = 0, 0, 0
		t.probing = false
		t.reason = "runtime CPU or memory pressure"
		return
	}
	rate := float64(o.Rows) / o.Seconds
	if t.samples == 0 {
		t.score, t.tail = rate, o.TailSeconds
	} else {
		t.score = .7*t.score + .3*rate
		t.tail = .7*t.tail + .3*o.TailSeconds
	}
	t.samples++
	if t.samples < 3 {
		return
	}
	t.samples = 0
	if t.probing {
		t.probing = false
		efficient := t.current < t.best && t.score >= t.bestScore*.97
		if (t.score > t.bestScore*1.08 || efficient) && t.tail <= t.bestTail*1.15 {
			t.best, t.bestScore, t.bestTail = t.current, t.score, t.tail
			t.reason = "measured throughput improvement"
		} else {
			t.current = t.best
			t.direction = -t.direction
			t.stable = 0
			t.reason = "probe rolled back: gain or tail bound not met"
			return
		}
	} else if t.best == 0 {
		t.best, t.bestScore, t.bestTail = t.current, t.score, t.tail
	} else if t.score < t.bestScore*.90 || t.tail > t.bestTail*1.15 {
		t.current = max(1, t.current-1)
		t.best = 0
		t.direction = -1
		t.reason = "measured throughput or tail regression"
		return
	} else {
		t.bestScore = .8*t.bestScore + .2*t.score
		t.bestTail = .8*t.bestTail + .2*t.tail
		t.stable++
		if t.stable < 4 {
			t.reason = "holding measured allocation"
			return
		}
		t.stable = 0
	}
	if t.direction == 0 {
		t.direction = 1
	}
	candidate := t.current + t.direction
	if candidate < 1 || candidate > upper {
		t.direction = -t.direction
		candidate = t.current + t.direction
	}
	if candidate >= 1 && candidate <= upper {
		t.current = candidate
		t.probing = true
		t.reason = "bounded one-worker exploration"
	} else {
		t.reason = "operator or runtime exploration ceiling"
	}
}

// Cohorts include admission wait and finish at quiescence or eight completed
// queries, allowing adaptation under continuous traffic. Mixed workloads are
// discarded rather than comparing different SQL costs. No Redis polling or sampler goroutine.
type workerCohort struct {
	started       time.Time
	cpu           float64
	pending, peak int
	completed     int
	allocation    int
	key           string
	mixed, failed bool
	rows          int64
	tail          float64
	fetch         int64
}
type scanTelemetry struct{ rows, fetch atomic.Int64 }

func runtimeSignals() (float64, float64) {
	s := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}, {Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/memory/classes/total:bytes"}, {Name: "/memory/classes/heap/released:bytes"}, {Name: "/gc/gomemlimit:bytes"}}
	metrics.Read(s)
	cpu := s[0].Value.Float64() + s[1].Value.Float64()
	limit := s[4].Value.Uint64()
	pressure := 0.0
	if limit < math.MaxInt64 {
		pressure = float64(s[2].Value.Uint64()-s[3].Value.Uint64()) / float64(max(1, limit))
	}
	return cpu, pressure
}
func (b *workerBudget) begin(key string) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if b.cohort.pending == 0 {
		cpu, _ := runtimeSignals()
		b.cohort = workerCohort{started: now, cpu: cpu, key: key, allocation: b.tuner.capacity(b.upper())}
	}
	c := &b.cohort
	c.pending++
	c.peak = max(c.peak, c.pending)
	c.mixed = c.mixed || c.key != key || c.allocation != b.tuner.capacity(b.upper())
	return now
}
func (b *workerBudget) finish(start time.Time, stats *scanTelemetry, failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := &b.cohort
	c.pending--
	c.completed++
	c.rows += stats.rows.Load()
	c.fetch += stats.fetch.Load()
	c.tail = max(c.tail, time.Since(start).Seconds())
	c.failed = c.failed || failed
	if c.pending != 0 && c.completed < 8 {
		return
	}
	defer func() {
		b.cohort = workerCohort{started: time.Now(), cpu: cpuSnapshot(), pending: c.pending, peak: c.pending, key: c.key, mixed: c.mixed && c.pending > 0, allocation: b.tuner.capacity(b.upper())}
	}()
	if c.mixed || c.failed || c.allocation != b.tuner.capacity(b.upper()) {
		b.tuner.reason = "discarded mixed or failed cohort"
		return
	}
	// Changing workload size or concurrency invalidates the comparison baseline.
	regime := c.key + ":" + strconv.Itoa(c.peak)
	if b.regime != regime {
		b.regime = regime
		b.tuner = workerTuner{current: min(4, runtime.GOMAXPROCS(0), b.upper()), cpu: runtime.GOMAXPROCS(0), reason: "workload or concurrency changed"}
		// The completed cohort used the previous allocation: don't train on it.
		return
	}
	cpu, pressure := runtimeSignals()
	elapsed := time.Since(c.started).Seconds()
	b.tuner.observe(tuneObservation{Rows: c.rows, Seconds: elapsed, TailSeconds: c.tail, CPUUtilization: max(0, cpu-c.cpu) / elapsed / float64(max(1, runtime.GOMAXPROCS(0))), MemoryPressure: pressure, FetchSeconds: float64(c.fetch) / 1e9, Concurrency: c.peak}, b.upper())
	b.dispatch()
}
func (b *workerBudget) tuningJSON() string {
	// Caller holds mu.
	b.tuner.capacity(b.upper())
	v, _ := json.Marshal(struct {
		Budget       int             `json:"budget"`
		Target       int             `json:"target_workers"`
		Ceiling      int             `json:"exploration_ceiling"`
		Reason       string          `json:"reason"`
		Observations uint64          `json:"observations"`
		Signals      tuneObservation `json:"signals"`
	}{b.limit(), b.tuner.current, b.upper(), b.tuner.reason, b.tuner.observations, b.tuner.last})
	return string(v)
}

func cpuSnapshot() float64 {
	cpu, _ := runtimeSignals()
	return cpu
}
