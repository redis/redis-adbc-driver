// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// Controlled Go attribution on an owned Software fixture, not native throughput.
// The manual worker has no pacing; setup and complete validation are untimed.
func TestSnapshotCompactionCombinedDirectoryProfile(t *testing.T) {
	if os.Getenv("REDIS_COMPACTION_DIRECTORY_PROFILE") != "true" {
		t.Skip("set REDIS_COMPACTION_DIRECTORY_PROFILE=true on owned verified Software")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT,payload VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,'row-'||i FROM generate_series(1,20000) AS g(i)")
	for range 3 {
		if n := h.exec("UPDATE t SET v=v+1"); n != 20000 {
			t.Fatal("incomplete setup rewrite", n)
		}
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("compaction admission", started, err)
	}
	defer s.releaseSnapshotCompaction(h.ctx, token)
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.SourceStart != 0 {
		t.Fatal("expected full-source compaction", err)
	}
	observer := &compactionReadPipelineObserver{sources: map[string]bool{}}
	for _, segment := range job.Source.snapshotSegments() {
		observer.sources[snapshotMembers(snapshotDataID(segment.Prefix))] = true
	}
	s.client.AddHook(observer)
	ctx, cancel := context.WithTimeout(context.WithValue(h.ctx, compactionReadObservation{}, true), 2*time.Minute)
	defer cancel()
	steps := 0
	copySteps := 0
	started := time.Now()
	for {
		job, err := s.readSnapshotCompaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if job.Phase == "copy" {
			copySteps++
		}
		if _, err := s.snapshotCompactionStep(ctx, token); err != nil {
			t.Fatal(err)
		}
		steps++
	}
	elapsed := time.Since(started)
	rows, _ := h.query("SELECT id,v,payload FROM t ORDER BY id")
	if len(rows) != 20000 {
		t.Fatal("incomplete final fetch", len(rows))
	}
	for i, row := range rows {
		if row != fmt.Sprintf("%d|9007199254740996|row-%d", i+1, i+1) {
			t.Fatal("incorrect final value", i+1)
		}
	}
	encoded, err := json.Marshal(map[string]any{
		"rows": len(rows), "columns": 3, "sources": len(observer.sources),
		"steps": steps, "copy_steps": copySteps, "milliseconds": float64(elapsed) / float64(time.Millisecond),
		"directory_pipeline_calls": observer.pipelines, "directory_commands": observer.commands,
		"directory_client_milliseconds": float64(observer.elapsed) / float64(time.Millisecond),
		"pacing":                        false, "validated": true, "caller_retries": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("compaction_combined_directory_profile %s", encoded)
}
