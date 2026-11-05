// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Attribute foreground client calls under the sustained comparison's SQL/data
// shape. The background worker has its own unhooked pool. This instrumented Go
// workload is not a native throughput comparison or an exclusive CPU profile.
func TestSnapshotForegroundWritePhaseProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_FOREGROUND_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_FOREGROUND_PROFILE for owned-fixture attribution")
	}
	h, _, _ := newSnapshotHarnessMode(t, true, false)
	names := []string{"sustained_a", "sustained_b"}
	initial := int64(9007199254740993)
	increments := []int64{0, 0}
	for _, name := range names {
		h.exec(fmt.Sprintf("CREATE TABLE %s(id BIGINT,v BIGINT,payload VARCHAR)", name))
		h.exec(fmt.Sprintf("INSERT INTO %s SELECT i,%d,'row-'||i FROM generate_series(1,20000) AS g(i)", name, initial))
	}
	for i, name := range names {
		h.expectRows(fmt.Sprintf("UPDATE %s SET v=v+1 WHERE id=1 RETURNING id,v", name), fmt.Sprintf("1|%d", initial+1))
		increments[i]++
	}
	profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
	ctx, _ := statisticsHookedTransport(t, h, profile)
	prior := h.ctx
	h.ctx = ctx
	defer func() { h.ctx = prior }()
	type observation struct {
		Operation int     `json:"operation"`
		Millis    float64 `json:"elapsed_milliseconds"`
	}
	var observations []observation
	for operation := range 64 {
		phase := fmt.Sprintf("write_%02d", operation)
		profile.mu.Lock()
		profile.phases[phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
		profile.phase = phase
		profile.mu.Unlock()
		table := operation % len(names)
		started := time.Now()
		h.expectRows(fmt.Sprintf("UPDATE %s SET v=v+1 WHERE id=1 RETURNING id,v", names[table]), fmt.Sprintf("1|%d", initial+increments[table]+1))
		elapsed := float64(time.Since(started)) / float64(time.Millisecond)
		increments[table]++
		profile.mu.Lock()
		profile.phase = ""
		profile.phases[phase].Steps = 1
		profile.phases[phase].Millis = elapsed
		profile.mu.Unlock()
		observations = append(observations, observation{Operation: operation, Millis: elapsed})
	}
	h.ctx = prior
	for table, name := range names {
		rows, _ := h.query(fmt.Sprintf("SELECT id,v,payload FROM %s ORDER BY id", name))
		if len(rows) != 20000 {
			t.Fatal("incomplete foreground-profile final fetch", len(rows))
		}
		for i, row := range rows {
			v := initial
			if i == 0 {
				v += increments[table]
			}
			if row != fmt.Sprintf("%d|%d|row-%d", i+1, v, i+1) {
				t.Fatalf("incorrect final row %d in synthetic table %d", i+1, table)
			}
		}
	}
	profile.mu.Lock()
	encoded, err := json.Marshal(map[string]any{"operations": observations, "foreground_calls": profile.phases, "rows_per_table": 20000, "tables": 2, "caller_retries": 0})
	profile.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "snapshot:rows:") || strings.Contains(string(encoded), "rediss://") {
		t.Fatal("foreground profile must not expose physical keys or endpoints")
	}
	t.Logf("snapshot_foreground_profile %s", encoded)
}
