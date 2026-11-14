// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// loadTable must use the index attributes already checked by getTable, while
// every execution (even the same prepared SQL) still checks a live definition.
func TestUnmanagedStatementChecksIndexOncePerFreshPin(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	if err := raw.HSet(h.ctx, "live_once:1", "n", "9007199254740993").Err(); err != nil {
		t.Fatal(err)
	}
	create := func(prefix string) {
		t.Helper()
		if err := raw.Do(h.ctx, "FT.CREATE", "live_once_idx", "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "n", "NUMERIC").Err(); err != nil {
			t.Fatal(err)
		}
		if err := (&store{client: raw}).awaitIndexReady(h.ctx, "live_once_idx"); err != nil {
			t.Fatal(err)
		}
	}
	create("live_once:")
	if _, err := AdoptHashes(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, HashAdoptOptions{Mode: "unmanaged", Prefix: "live_once:", Table: "live_once", Apply: true}); err != nil {
		t.Fatal(err)
	}
	profile := &compactionPhaseProfile{phase: "read", phases: map[string]*compactionProfilePhase{"read": {RoundTrips: map[string]*compactionProfileRoundTrips{}}}}
	ctx, _ := statisticsHookedTransport(t, h, profile)
	stmt, err := h.conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	if err := stmt.SetSqlQuery(ctx, "SELECT n FROM live_once WHERE __key='live_once:1'"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	read := func(want int64) {
		t.Helper()
		reader, _, err := stmt.ExecuteQuery(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Release()
		n := 0
		for reader.Next() {
			rec := reader.RecordBatch()
			for i := 0; i < int(rec.NumRows()); i++ {
				n++
				v, ok := rec.Column(0).(*array.Int64)
				if !ok || v.IsNull(i) || v.Value(i) != want {
					t.Fatal("wrong exact current source value")
				}
			}
		}
		if err := reader.Err(); err != nil || n != 1 {
			t.Fatal("incomplete read", n, err)
		}
	}
	for sample, want := range []int64{9007199254740993, 9007199254740995} {
		if err := raw.HSet(h.ctx, "live_once:1", "n", want).Err(); err != nil {
			t.Fatal(err)
		}
		read(want)
		profile.mu.Lock()
		stats := profile.phases["read"].RoundTrips["ft.info"]
		calls := 0
		if stats != nil {
			calls = stats.Calls
		}
		profile.mu.Unlock()
		if calls != sample+1 {
			t.Fatalf("FT.INFO calls=%d after %d statements, want once per fresh pin", calls, sample+1)
		}
	}
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "live_once_idx").Err(); err != nil {
		t.Fatal(err)
	}
	create("other_once:")
	reader, _, err := stmt.ExecuteQuery(ctx)
	if reader != nil {
		reader.Release()
	}
	if err == nil || !strings.Contains(err.Error(), "definition changed") {
		t.Fatal("prepared SQL reused stale application index definition", err)
	}
	if got := raw.HGet(h.ctx, "live_once:1", "n").Val(); got != "9007199254740995" {
		t.Fatal("failed read mutated application source")
	}
}
