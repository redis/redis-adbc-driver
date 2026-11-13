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

// Command attribution only: persistent Go connection, synthetic wide rows and
// automatic background maintenance. Full result validation is outside timing.
// The foreground hook excludes the background worker's independent pool.
func TestManagedWritePublicationProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_WRITE_PROFILE") != "true" {
		t.Skip("set REDIS_MANAGED_WRITE_PROFILE=true on owned verified Software")
	}
	for _, batch := range []int64{1000, 10000} {
		for trial := range 3 {
			t.Run(fmt.Sprintf("batch_%d_trial_%d", batch, trial), func(t *testing.T) {
				h, _, _ := newSnapshotHarnessMode(t, true, false)
				rec := followupRecord(10000)
				defer rec.Release()
				empty := rec.NewSlice(0, 0)
				err := followupIngest(h.ctx, h.conn, "write_profile", adbc.OptionValueIngestModeCreate, empty)
				empty.Release()
				if err != nil {
					t.Fatal(err)
				}
				profile := &compactionPhaseProfile{phase: "append", phases: map[string]*compactionProfilePhase{"append": {RoundTrips: map[string]*compactionProfileRoundTrips{}}}}
				ctx, _ := statisticsHookedTransport(t, h, profile)
				started := time.Now()
				for lo := int64(0); lo < rec.NumRows(); lo += batch {
					part := rec.NewSlice(lo, lo+batch)
					err := followupIngest(ctx, h.conn, "write_profile", adbc.OptionValueIngestModeAppend, part)
					part.Release()
					if err != nil {
						t.Fatal(err)
					}
				}
				elapsed := time.Since(started)
				profile.mu.Lock()
				profile.phase = ""
				profile.mu.Unlock()
				actual, err := followupFetch(ctx, h.conn, "SELECT * FROM write_profile")
				if err != nil {
					t.Fatal(err)
				}
				followupValidate(t, actual, 1, 10000)
				profile.mu.Lock()
				encoded, err := json.Marshal(map[string]any{"rows": 10000, "columns": 105, "batch": batch, "trial": trial, "milliseconds": float64(elapsed) / float64(time.Millisecond), "foreground": profile.phases["append"], "validated": true, "caller_retries": 0})
				profile.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("managed_write_publication_profile %s", encoded)
			})
		}
	}
}
