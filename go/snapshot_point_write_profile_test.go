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

	goredis "github.com/redis/go-redis/v9"
)

// Match the native scenario fixture/addressing while observing Go client call
// time. This is diagnostic attribution, not Python/ADBC end-to-end latency or
// Redis server CPU. The production worker remains enabled on its separate pool.
func TestManagedKnownRowWriteProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_POINT_PROFILE") == "" {
		t.Skip("set REDIS_MANAGED_POINT_PROFILE on an owned Software fixture")
	}
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	for _, name := range []string{"point_insert", "point_update", "point_delete"} {
		h.exec("CREATE TABLE " + name + "(id BIGINT,grp BIGINT,amount BIGINT,label VARCHAR,ordinal BIGINT)")
		h.exec("INSERT INTO " + name + " SELECT i,i%16,i*3,'row-'||i,i FROM generate_series(1,1000) AS g(i)")
	}
	profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
	ctx, _ := statisticsHookedTransport(t, h, profile)
	prior := h.ctx
	h.ctx = ctx
	defer func() { h.ctx = prior }()
	type sample struct {
		Operation string  `json:"operation"`
		Index     int     `json:"index"`
		Millis    float64 `json:"milliseconds"`
	}
	var samples []sample
	for _, kind := range []string{"insert", "update", "delete"} {
		for i := 1; i <= 32; i++ {
			phase := fmt.Sprintf("%s_%02d", kind, i)
			profile.mu.Lock()
			profile.phases[phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
			profile.phase = phase
			profile.mu.Unlock()
			began := time.Now()
			sql := ""
			switch kind {
			case "insert":
				id := i + 1000
				sql = fmt.Sprintf("INSERT INTO point_insert VALUES(%d,%d,%d,'row-%d',%d)", id, id%16, id*3, id, id)
			case "update":
				sql = fmt.Sprintf("UPDATE point_update SET amount=%d WHERE __rowid=%d", -i, i)
			case "delete":
				sql = fmt.Sprintf("DELETE FROM point_delete WHERE __rowid=%d", i)
			}
			n := h.exec(sql)
			elapsed := float64(time.Since(began)) / float64(time.Millisecond)
			profile.mu.Lock()
			profile.phase = ""
			profile.phases[phase].Steps = 1
			profile.phases[phase].Millis = elapsed
			profile.mu.Unlock()
			if n != 1 {
				t.Fatal("incorrect affected count", kind, i, n)
			}
			samples = append(samples, sample{kind, i, elapsed})
		}
	}
	h.ctx = prior
	for _, kind := range []string{"insert", "update", "delete"} {
		rows, schema := h.query("SELECT id,grp,amount,label FROM point_" + kind + " ORDER BY id")
		if schema.NumFields() != 4 {
			t.Fatal("incorrect result width", schema)
		}
		first, last := 1, 1000
		if kind == "insert" {
			last = 1032
		}
		if kind == "delete" {
			first = 33
		}
		if len(rows) != last-first+1 {
			t.Fatal("incomplete final state", kind, len(rows))
		}
		for i, row := range rows {
			id := i + first
			amount := id * 3
			if kind == "update" && id <= 32 {
				amount = -id
			}
			if want := fmt.Sprintf("%d|%d|%d|row-%d", id, id%16, amount, id); row != want {
				t.Fatal("wrong final state", kind, id, row)
			}
		}
	}
	// Slowlog availability and elapsed command durations are separate evidence.
	// A missing/filtered command is unavailable, never a zero-cost server result.
	// Discard command arguments, endpoint/client identities and physical keys.
	slow := map[string]any{"available": false}
	if entries, err := raw.SlowLogGet(h.ctx, 128).Result(); err == nil {
		type slowSample struct {
			Category string `json:"category"`
			Micros   int64  `json:"microseconds"`
		}
		var values []slowSample
		for _, entry := range entries {
			if len(entry.Args) == 0 {
				continue
			}
			args := make([]any, len(entry.Args))
			for i, v := range entry.Args {
				args[i] = v
			}
			cmd := goredis.NewCmd(h.ctx, args...)
			category := compactionProfileCategory(cmd)
			if len(category) > 96 || strings.ContainsAny(category, "\r\n") {
				category = strings.ToLower(entry.Args[0])
			}
			values = append(values, slowSample{category, entry.Duration.Microseconds()})
		}
		slow = map[string]any{"available": true, "samples": values, "metric": "native slowlog command elapsed microseconds; threshold-selected, not server CPU"}
	}
	profile.mu.Lock()
	payload, err := json.Marshal(map[string]any{"rows_per_fixture": 1000, "public_columns": 4, "control_columns": 1, "samples": samples, "foreground_calls": profile.phases, "maintenance": "production worker enabled; its separate client is not hooked", "slowlog": slow, "caller_retries": 0})
	profile.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), "snapshot:17:rows:") {
		t.Fatal("private runtime identity in profile")
	}
	t.Logf("managed_known_row_write_profile %s", payload)
}
