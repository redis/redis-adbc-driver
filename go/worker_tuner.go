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
	"slices"
	"strconv"
	"sync/atomic"
	"time"
)

// Compare a candidate with the same retained allocation before AND after the
// probe. Six-sample robust windows reject bursts and baseline drift; a single
// slow observation can never ratchet a retained allocation down.
const tuningWindow = 6
const tuningHold = 48
const tuningIncomparableLimit = 8

type workerTuner struct {
	current, cpu, best, samples, stable, direction int
	bestScore, bestTail                            float64
	beforeScore, beforeTail, probeScore, probeTail float64
	bestNoise, beforeNoise, probeNoise             float64
	incomparable                                   int
	candidate, cooldown, pressure                  int
	rates, tails                                   [tuningWindow]float64
	phase                                          string
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
		*t = workerTuner{current: min(32, cpu, upper), cpu: cpu, phase: "baseline", cooldown: tuningHold, reason: "runtime capacity initialization"}
	}
	t.current = max(1, min(t.current, upper))
	return t.current
}
func median(values []float64) float64 {
	copy := append([]float64(nil), values...)
	slices.Sort(copy)
	n := len(copy)
	if n%2 == 0 {
		return (copy[n/2-1] + copy[n/2]) / 2
	}
	return copy[n/2]
}
func stableWindow(values []float64) (float64, float64, bool) {
	center := median(values)
	deviations := make([]float64, len(values))
	for i, v := range values {
		deviations[i] = math.Abs(v - center)
	}
	spread := median(deviations) / math.Max(center, 1e-12)
	return center, spread, center > 0 && spread <= .12
}
func (t *workerTuner) hold(reason string) {
	t.current = t.best
	t.phase = "holding"
	t.stable = 0
	t.samples = 0
	t.incomparable = 0
	t.reason = reason
}
func (t *workerTuner) probe(upper int) {
	if t.direction == 0 {
		t.direction = 1
	}
	if t.pressure >= 3 {
		t.direction = -1
	}
	candidate := t.best + t.direction
	if candidate < 1 || candidate > upper {
		t.direction = -t.direction
		candidate = t.best + t.direction
	}
	if candidate == t.best || candidate < 1 || candidate > upper || (t.pressure >= 3 && candidate > t.best) {
		t.hold("holding retained allocation: CPU pressure or ceiling")
		return
	}
	t.beforeScore, t.beforeTail = t.bestScore, t.bestTail
	t.beforeNoise = t.bestNoise
	t.candidate = candidate
	t.current = candidate
	t.phase = "probe"
	t.samples = 0
	t.incomparable = 0
	t.reason = "bounded candidate probe"
}

// A temporary candidate must not persist indefinitely when observations cannot
// be compared. Allow transition grace (old leases normally drain in one or two
// windows), then abandon the comparison. This bounds discarded completion
// windows, not elapsed time: grants remain non-preemptive.
func (t *workerTuner) discardWindow() {
	if t.phase == "probe" || t.phase == "verify" {
		t.incomparable++
		if t.incomparable >= tuningIncomparableLimit {
			t.cooldown = min(192, max(tuningHold, t.cooldown*2))
			t.hold("abandoned incomparable probe: retained allocation")
			return
		}
	}
	t.reason = "discarded failed, mixed, or allocation-transition window"
}

func (t *workerTuner) observe(o tuneObservation, upper int) {
	t.capacity(upper)
	t.last = o
	t.observations++
	if o.Seconds <= 0 || o.Rows <= 0 {
		return
	}
	if o.MemoryPressure >= .85 {
		observations := t.observations
		*t = workerTuner{current: max(1, t.current-1), cpu: t.cpu, phase: "baseline", cooldown: tuningHold, reason: "runtime memory pressure", observations: observations, last: o}
		return
	}
	if o.CPUUtilization >= .90 {
		t.pressure++
	} else {
		t.pressure = 0
	}
	t.rates[t.samples] = float64(o.Rows) / o.Seconds
	t.tails[t.samples] = o.TailSeconds
	t.samples++
	if t.samples < tuningWindow {
		return
	}
	t.samples = 0
	score, noise, reliable := stableWindow(t.rates[:])
	tail, _, tailReliable := stableWindow(t.tails[:])
	if !reliable || !tailReliable {
		if t.best == 0 {
			t.reason = "insufficient confidence: noisy baseline"
			return
		}
		t.cooldown = min(192, max(tuningHold, t.cooldown*2))
		t.hold("insufficient confidence: retain allocation after noise")
		return
	}
	switch t.phase {
	case "probe":
		t.probeScore, t.probeTail = score, tail
		t.probeNoise = noise
		t.current = t.best
		t.phase = "verify"
		t.reason = "rechecking retained allocation after probe"
		return
	case "verify":
		// Reject comparisons made during an environmental trend, rather than
		// assigning a server-wide slowdown to the number of workers.
		drift := score < t.beforeScore*.90 || score > t.beforeScore*1.10 || tail < t.beforeTail*.85 || tail > t.beforeTail*1.15
		reference := math.Max(score, t.beforeScore)
		tailReference := math.Min(tail, t.beforeTail)
		margin := math.Max(.03, 2*math.Max(noise, math.Max(t.beforeNoise, t.probeNoise))/math.Sqrt(tuningWindow))
		gain := t.probeScore > reference*(1+margin)
		efficient := t.candidate < t.best && t.probeScore >= reference*.98
		if !drift && (gain || efficient) && t.probeTail <= tailReference*1.10 {
			t.best, t.bestScore, t.bestTail = t.candidate, t.probeScore, t.probeTail
			t.bestNoise = t.probeNoise
			t.current = t.best
			t.cooldown = tuningHold
			t.reason = "accepted paired throughput improvement"
			if efficient && !gain {
				t.reason = "accepted equivalent throughput with fewer workers"
			}
			// Continue a successful direction; at an endpoint hold, rather than
			// immediately paying for a probe in the opposite direction.
			if t.best+t.direction >= 1 && t.best+t.direction <= upper {
				t.probe(upper)
			} else {
				t.hold(t.reason)
			}
		} else {
			t.bestScore, t.bestTail = score, tail
			t.bestNoise = noise
			t.direction = -t.direction
			t.cooldown = min(192, max(tuningHold, t.cooldown*2))
			reason := "candidate rejected: throughput or tail bound"
			if drift {
				reason = "candidate rejected: baseline drift"
			}
			t.hold(reason)
		}
		return
	case "holding":
		changed := score < t.bestScore*.85 || tail > t.bestTail*1.20
		t.bestScore, t.bestTail = score, tail
		t.bestNoise = noise
		t.stable += tuningWindow
		if changed {
			t.direction = -1
			t.probe(upper)
			return
		}
		if t.stable < t.cooldown {
			t.reason = "holding measured allocation"
			return
		}
		t.probe(upper)
		return
	default:
		t.best = t.current
		t.bestScore, t.bestTail = score, tail
		t.bestNoise = noise
		t.probe(upper)
	}
}

// Cohorts include admission wait and finish at quiescence or eight completed
// queries, allowing adaptation under continuous traffic. Mixed workloads are
// discarded rather than comparing different SQL costs. No Redis polling or sampler goroutine.
type workerCohort struct {
	started                  time.Time
	cpu                      float64
	pending, peak, completed int
	generation               uint64
	key                      string
	mixed, failed            bool
	rows                     int64
	tail                     float64
	fetch                    int64
}
type workerLease struct {
	started    time.Time
	generation uint64
	key        string
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

// All coordinator/model/lease bookkeeping is protected by the admission lock.
func (b *workerBudget) syncEpoch() {
	allocation := b.tuner.capacity(b.upper())
	if allocation != b.epochAllocation || b.tuner.cpu != b.epochCPU {
		b.generation++
		b.epochAllocation, b.epochCPU = allocation, b.tuner.cpu
	}
}
func (b *workerBudget) pendingMixed(key string) bool {
	for k, n := range b.pendingKeys {
		if n > 0 && k != key {
			return true
		}
	}
	for g, n := range b.pendingGenerations {
		if n > 0 && g != b.generation {
			return true
		}
	}
	return false
}
func (b *workerBudget) begin(key string) workerLease {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.syncEpoch()
	now := time.Now()
	if b.cohort.pending == 0 {
		b.cohort = workerCohort{started: now, cpu: cpuSnapshot(), key: key, generation: b.generation}
	}
	if b.pendingKeys == nil {
		b.pendingKeys = map[string]int{}
		b.pendingGenerations = map[uint64]int{}
	}
	b.pendingKeys[key]++
	b.pendingGenerations[b.generation]++
	c := &b.cohort
	c.pending++
	c.peak = max(c.peak, c.pending)
	c.mixed = c.mixed || b.pendingMixed(c.key) || c.generation != b.generation
	return workerLease{now, b.generation, key}
}
func (b *workerBudget) cacheModel(regime string, t workerTuner) {
	if regime == "" {
		return
	}
	if b.models == nil {
		b.models = map[string]workerTuner{}
	}
	if _, exists := b.models[regime]; !exists {
		if len(b.modelOrder) == 8 {
			delete(b.models, b.modelOrder[0])
			b.modelOrder = b.modelOrder[1:]
		}
		b.modelOrder = append(b.modelOrder, regime)
	}
	// Keep the retained allocation, but never resume a partially measured probe.
	// Its before-window predates the intervening workload and is not comparable.
	if t.best > 0 {
		t.current = t.best
	}
	t.phase, t.samples, t.candidate, t.incomparable = "baseline", 0, 0, 0
	b.models[regime] = t
}
func (b *workerBudget) finish(lease workerLease, stats *scanTelemetry, failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.syncEpoch()
	if b.pendingKeys[lease.key]--; b.pendingKeys[lease.key] == 0 {
		delete(b.pendingKeys, lease.key)
	}
	if b.pendingGenerations[lease.generation]--; b.pendingGenerations[lease.generation] == 0 {
		delete(b.pendingGenerations, lease.generation)
	}
	c := &b.cohort
	c.pending--
	c.completed++
	c.rows += stats.rows.Load()
	c.fetch += stats.fetch.Load()
	c.tail = max(c.tail, time.Since(lease.started).Seconds())
	c.failed = c.failed || failed
	c.mixed = c.mixed || lease.generation != c.generation || lease.key != c.key
	if c.pending != 0 && c.completed < 8 {
		return
	}
	defer func() {
		b.syncEpoch()
		key := c.key
		if len(b.pendingKeys) == 1 {
			for k := range b.pendingKeys {
				key = k
			}
		}
		b.cohort = workerCohort{started: time.Now(), cpu: cpuSnapshot(), pending: c.pending, peak: c.pending, key: key, generation: b.generation, mixed: b.pendingMixed(key)}
	}()
	if c.mixed || c.failed || c.generation != b.generation || b.pendingMixed(c.key) {
		b.tuner.discardWindow()
		b.dispatch()
		return
	}
	regime := c.key + ":" + strconv.Itoa(c.peak)
	if b.regime != regime {
		b.cacheModel(b.regime, b.tuner)
		b.regime = regime
		if model, exists := b.models[regime]; exists {
			previousCPU := model.cpu
			b.tuner = model
			b.tuner.capacity(b.upper())
			switch {
			case previousCPU != b.tuner.cpu:
				b.tuner.reason = "runtime capacity invalidated cached workload"
			case model.best > 0:
				b.tuner.reason = "resumed retained workload allocation"
			default:
				b.tuner.reason = "resumed unlearned workload baseline"
			}
		} else {
			b.tuner = workerTuner{}
			b.tuner.capacity(b.upper())
			b.tuner.reason = "new workload or concurrency baseline"
		}
		b.dispatch()
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
		Retained     int             `json:"retained_workers"`
		Candidate    int             `json:"candidate_workers"`
		Phase        string          `json:"phase"`
		CachedModels int             `json:"cached_models"`
		Signals      tuneObservation `json:"signals"`
	}{b.limit(), b.tuner.current, b.upper(), b.tuner.reason, b.tuner.observations, b.tuner.best, b.tuner.candidate, b.tuner.phase, len(b.models), b.tuner.last})
	return string(v)
}

func cpuSnapshot() float64 {
	cpu, _ := runtimeSignals()
	return cpu
}
