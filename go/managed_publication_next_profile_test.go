// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Attribute foreground and maintenance command costs separately. Command times
// overlap across pools; their sum is not wall time, server CPU or admission wait.
// Every public ingest call remains unchanged and is never replayed.
func TestManagedPublicationNextProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_PUBLICATION_NEXT_PROFILE") != "true" {
		t.Skip("set REDIS_MANAGED_PUBLICATION_NEXT_PROFILE=true on owned verified Software")
	}
	for _, batch := range []int64{1000, 10000} {
		for trial := range 3 {
			t.Run(fmt.Sprintf("batch_%d_trial_%d", batch, trial), func(t *testing.T) {
				h, _, _ := newSnapshotHarnessMode(t, false, false)
				rec := followupRecord(10000)
				defer rec.Release()
				empty := rec.NewSlice(0, 0)
				err := followupIngest(h.ctx, h.conn, "publication_next", adbc.OptionValueIngestModeCreate, empty)
				empty.Release()
				if err != nil {
					t.Fatal(err)
				}
				fresh := func(phase string) *compactionPhaseProfile {
					return &compactionPhaseProfile{phase: phase, phases: map[string]*compactionProfilePhase{phase: {RoundTrips: map[string]*compactionProfileRoundTrips{}}}}
				}
				foreground, background := fresh("append"), fresh("maintenance")
				ctx, impl := statisticsHookedTransport(t, h, foreground)
				// Install the maintenance hook before its goroutine starts.
				impl.store.startSnapshotCompactor(impl.clientCfg, impl.timeouts, background)
				var calls []float64
				started := time.Now()
				for lo := int64(0); lo < rec.NumRows(); lo += batch {
					part := rec.NewSlice(lo, lo+batch)
					callAt := time.Now()
					err := followupIngest(ctx, h.conn, "publication_next", adbc.OptionValueIngestModeAppend, part)
					calls = append(calls, float64(time.Since(callAt))/float64(time.Millisecond))
					part.Release()
					if err != nil {
						t.Fatal(err)
					}
				}
				elapsed := time.Since(started)
				foreground.mu.Lock()
				foreground.phase = ""
				foreground.mu.Unlock()
				background.mu.Lock()
				background.phase = ""
				background.mu.Unlock()
				// Join outside timing so observations begun within the measured window
				// settle before encoding; release/cleanup commands are excluded by phase.
				impl.store.stopSnapshotCompactor()
				actual, err := followupFetch(ctx, h.conn, "SELECT * FROM publication_next")
				if err != nil {
					t.Fatal(err)
				}
				followupValidate(t, actual, 1, 10000)
				foreground.mu.Lock()
				background.mu.Lock()
				encoded, err := json.Marshal(map[string]any{"rows": 10000, "columns": 105, "call_rows": batch, "trial": trial, "milliseconds": float64(elapsed) / float64(time.Millisecond), "call_milliseconds": calls, "foreground": foreground.phases["append"], "background": background.phases["maintenance"], "validated": true, "caller_retries": 0})
				background.mu.Unlock()
				foreground.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("managed_publication_next_profile %s", encoded)
			})
		}
	}
}
