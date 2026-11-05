// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type compactionProfileRoundTrips struct {
	Calls    int     `json:"calls"`
	Commands int     `json:"commands"`
	Millis   float64 `json:"milliseconds"`
}
type compactionProfilePhase struct {
	Steps      int                                     `json:"steps"`
	Millis     float64                                 `json:"step_milliseconds"`
	RoundTrips map[string]*compactionProfileRoundTrips `json:"round_trips"`
}
type compactionPhaseProfile struct {
	mu     sync.Mutex
	phase  string
	phases map[string]*compactionProfilePhase
}

// Record categories and elapsed client round trips only. Arguments, keys, row
// values and endpoint/credential data are deliberately absent from the report.
func compactionProfileCategory(cmd goredis.Cmder) string {
	a := cmd.Args()
	if cmd.Name() == "eval" && len(a) > 3 {
		if a[1] == snapshotCompactionCopyLua {
			return "copy_commit"
		}
		if a[1] == snapshotControl {
			n, err := strconv.Atoi(fmt.Sprint(a[2]))
			if err == nil && n >= 1 && 3+n < len(a) {
				return "control_" + fmt.Sprint(a[3+n])
			}
		}
	}
	return cmd.Name()
}
func (p *compactionPhaseProfile) current() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase
}
func (p *compactionPhaseProfile) record(phase, category string, commands int, elapsed time.Duration) {
	if phase == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := p.phases[phase]
	if stats.RoundTrips[category] == nil {
		stats.RoundTrips[category] = &compactionProfileRoundTrips{}
	}
	r := stats.RoundTrips[category]
	r.Calls++
	r.Commands += commands
	r.Millis += float64(elapsed) / float64(time.Millisecond)
}
func (*compactionPhaseProfile) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (p *compactionPhaseProfile) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		phase := p.current()
		started := time.Now()
		err := next(ctx, cmd)
		p.record(phase, compactionProfileCategory(cmd), 1, time.Since(started))
		return err
	}
}
func (p *compactionPhaseProfile) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		phase := p.current()
		categories := make([]string, len(cmds))
		for i, cmd := range cmds {
			categories[i] = compactionProfileCategory(cmd)
		}
		category := "pipeline_mixed"
		if len(categories) > 0 {
			category = "pipeline_" + categories[0]
			for _, c := range categories[1:] {
				if c != categories[0] {
					category = "pipeline_mixed"
					break
				}
			}
		}
		started := time.Now()
		err := next(ctx, cmds)
		p.record(phase, category, len(cmds), time.Since(started))
		return err
	}
}

// This is a controlled attribution workload, not an end-to-end throughput
// benchmark. It drives real bounded steps with and without the worker's pacing;
// the extra phase observation is outside each step timer. Run exclusively on the
// owned fixture, after all ordinary/snapshot/benchmark processes have exited.
func TestSnapshotCompactionPhaseProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_COMPACTION_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_COMPACTION_PROFILE for owned-fixture profiling")
	}
	for _, paced := range []bool{true, false} {
		t.Run(fmt.Sprintf("paced_%t", paced), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT,v BIGINT,payload VARCHAR)")
			h.exec("INSERT INTO t SELECT i,9007199254740993,'row-'||i FROM generate_series(1,20000) AS g(i)")
			h.exec("CREATE TABLE unrelated(id BIGINT)")
			h.exec("INSERT INTO unrelated SELECT i FROM generate_series(1,20000) AS g(i)")
			for i := 0; i < 3; i++ {
				h.expectRows("UPDATE t SET v=v+1 WHERE id=1 RETURNING v", strconv.FormatInt(9007199254740994+int64(i), 10))
			}
			s := snapshotTestStore(t, raw)
			token := snapshotID()
			if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
				t.Fatal("compaction did not start", started, err)
			}
			defer s.releaseSnapshotCompaction(h.ctx, token)
			profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
			s.client.AddHook(profile)
			ctx, cancel := context.WithTimeout(h.ctx, time.Minute)
			defer cancel()
			var pacing time.Duration
			active, complete := true, false
			started := time.Now()
			for range 10000 {
				job, err := s.readSnapshotCompaction(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if job == nil {
					complete = true
					break
				}
				if paced || !active {
					delay := 5 * time.Millisecond
					if !active {
						delay = time.Second
					}
					wait := time.Now()
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(delay):
					}
					pacing += time.Since(wait)
				}
				profile.mu.Lock()
				profile.phase = job.Phase
				if profile.phases[job.Phase] == nil {
					profile.phases[job.Phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
				}
				profile.mu.Unlock()
				step := time.Now()
				progress, err := s.snapshotCompactionStep(ctx, token)
				elapsed := time.Since(step)
				profile.mu.Lock()
				profile.phase = ""
				profile.phases[job.Phase].Steps++
				profile.phases[job.Phase].Millis += float64(elapsed) / float64(time.Millisecond)
				profile.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				active = progress
			}
			elapsed := time.Since(started)
			if !complete {
				t.Fatal("bounded compaction profile did not complete")
			}
			rows, _ := h.query("SELECT id,v,payload FROM t ORDER BY id")
			if len(rows) != 20000 {
				t.Fatal("incomplete final fetch", len(rows))
			}
			for i, row := range rows {
				want := int64(9007199254740993)
				if i == 0 {
					want += 3
				}
				if row != fmt.Sprintf("%d|%d|row-%d", i+1, want, i+1) {
					t.Fatalf("incorrect final row %d", i+1)
				}
			}
			h.expectRows("SELECT COUNT(*),MIN(id),MAX(id) FROM unrelated", "20000|1|20000")
			profile.mu.Lock()
			encoded, err := json.Marshal(map[string]any{"paced": paced, "rows": len(rows), "unrelated_rows": 20000, "elapsed_milliseconds": float64(elapsed) / float64(time.Millisecond), "pacing_milliseconds": float64(pacing) / float64(time.Millisecond), "phases": profile.phases})
			profile.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "snapshot:rows:") {
				t.Fatal("profile must not expose physical keys")
			}
			t.Logf("compaction_phase_profile %s", encoded)
		})
	}
}
