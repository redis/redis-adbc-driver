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
	"testing"
	"time"
)

// A bounded attribution control, not canonical ClickBench or a scaling claim.
// Recompile with each candidate burst constant and use the same owned fixture.
// Setup, final complete fetch and independent value checks are outside timers.
func TestSnapshotCompactionWideProfile(t *testing.T) {
	if os.Getenv("REDIS_COMPACTION_WIDE_PROFILE") != "true" {
		t.Skip("explicit owned Software QPF4 profile only")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	columns := []string{"id BIGINT NOINDEX"}
	expressions := []string{"i"}
	for c := 1; c < 105; c++ {
		if c%2 == 0 {
			columns = append(columns, fmt.Sprintf("c%d BIGINT NOINDEX", c))
			expressions = append(expressions, fmt.Sprintf("i+%d", c))
		} else {
			columns = append(columns, fmt.Sprintf("c%d VARCHAR NOINDEX", c))
			expressions = append(expressions, fmt.Sprintf("CASE WHEN i%%7=0 THEN NULL ELSE 'field%d-'||i END", c))
		}
	}
	h.exec("CREATE TABLE t(" + strings.Join(columns, ",") + ")")
	for wave := 0; wave < 4; wave++ {
		h.exec(fmt.Sprintf("INSERT INTO t SELECT %s FROM generate_series(%d,%d) AS g(i)", strings.Join(expressions, ","), wave*1024+1, (wave+1)*1024))
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("admission", started, err)
	}
	defer s.releaseSnapshotCompaction(h.ctx, token)
	profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
	s.client.AddHook(profile)
	ctx, cancel := context.WithTimeout(h.ctx, 3*time.Minute)
	defer cancel()
	start := time.Now()
	for {
		job, err := s.readSnapshotCompaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		profile.mu.Lock()
		profile.phase = job.Phase
		if profile.phases[job.Phase] == nil {
			profile.phases[job.Phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
		}
		profile.mu.Unlock()
		step := time.Now()
		_, err = s.snapshotCompactionStep(ctx, token)
		elapsed := time.Since(step)
		profile.mu.Lock()
		profile.phase = ""
		profile.phases[job.Phase].Steps++
		profile.phases[job.Phase].Millis += float64(elapsed) / float64(time.Millisecond)
		profile.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	rows, _ := h.query("SELECT * FROM t ORDER BY id")
	if len(rows) != 4096 {
		t.Fatal("incomplete fetch", len(rows))
	}
	for i, row := range rows {
		values := strings.Split(row, "|")
		if len(values) != 105 {
			t.Fatal("column count", len(values))
		}
		if values[0] != strconv.Itoa(i+1) {
			t.Fatal("id", i+1)
		}
		for c := 1; c < 105; c++ {
			want := fmt.Sprintf("field%d-%d", c, i+1)
			if c%2 == 0 {
				want = strconv.Itoa(i + 1 + c)
			} else if (i+1)%7 == 0 {
				want = "NULL"
			}
			if values[c] != want {
				t.Fatalf("row %d column %d got %q want %q", i+1, c, values[c], want)
			}
		}
	}
	encoded, err := json.Marshal(map[string]any{"batch_rows": snapshotCompactionBatchRows, "batches_per_step": snapshotCompactionCopyBatchesPerStep, "columns": 105, "rows": len(rows), "sources": 4, "milliseconds": float64(elapsed) / float64(time.Millisecond), "phases": profile.phases, "validated": true, "paced": false, "concurrent_writers": 0, "canonical_clickbench": false})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("compaction_wide_profile %s", encoded)
}
