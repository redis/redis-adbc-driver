// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/json"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
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
	if strings.Contains(string(encoded), "snapshot:17:rows:") || strings.Contains(string(encoded), "rediss://") {
		t.Fatal("foreground profile must not expose physical keys or endpoints")
	}
	t.Logf("snapshot_foreground_profile %s", encoded)
}

// Fixed foreground attribution keeps each target below the segment admission
// limit with workers disabled. It does not measure sustained maintenance (#337).
func TestSnapshotForegroundFixedWriteProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_FIXED_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_FIXED_PROFILE for owned Software attribution")
	}
	for _, unrelated := range []int{0, 32} {
		t.Run(fmt.Sprintf("unrelated_%d", unrelated), func(t *testing.T) {
			h, _, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE point_target(id BIGINT,v BIGINT,payload VARCHAR)")
			h.exec("INSERT INTO point_target SELECT i,9007199254740993,'row-'||i FROM generate_series(1,20000) AS g(i)")
			for _, name := range []string{"arrow_small", "arrow_batch"} {
				h.exec("CREATE TABLE " + name + "(id BIGINT,v BIGINT,payload VARCHAR)")
			}
			for i := 0; i < unrelated; i++ {
				h.exec(fmt.Sprintf("CREATE TABLE unrelated_%d(id BIGINT)", i))
			}
			profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
			ctx, _ := statisticsHookedTransport(t, h, profile)
			prior := h.ctx
			h.ctx = ctx
			defer func() { h.ctx = prior }()
			type observation struct {
				Case   string  `json:"case"`
				Sample int     `json:"sample"`
				Millis float64 `json:"milliseconds"`
			}
			var observations []observation
			value := int64(9007199254740993)
			for _, name := range []string{"point_update", "no_op_update", "arrow_small", "arrow_batch"} {
				for sample := 0; sample < 4; sample++ {
					var rec arrow.RecordBatch
					count := 1
					if name == "arrow_batch" {
						count = 512
					}
					if strings.HasPrefix(name, "arrow_") {
						builder := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "v", Type: arrow.PrimitiveTypes.Int64}, {Name: "payload", Type: arrow.BinaryTypes.String}}, nil))
						for i := 0; i < count; i++ {
							builder.Field(0).(*array.Int64Builder).Append(int64(sample*count + i + 1))
							builder.Field(1).(*array.Int64Builder).Append(9007199254740993)
							builder.Field(2).(*array.StringBuilder).Append("payload")
						}
						rec = builder.NewRecordBatch()
						builder.Release()
					}
					phase := fmt.Sprintf("%s_%d", name, sample)
					profile.mu.Lock()
					profile.phases[phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
					profile.phase = phase
					profile.mu.Unlock()
					started := time.Now()
					if name == "point_update" {
						value++
						h.expectRows("UPDATE point_target SET v=v+1 WHERE __rowid=1 RETURNING id,v", fmt.Sprintf("1|%d", value))
					} else if name == "no_op_update" {
						if n := h.exec("UPDATE point_target SET v=v+1 WHERE id=-1"); n != 0 {
							t.Fatal("no-op changed rows", n)
						}
					} else {
						st, err := h.conn.NewStatement(h.ctx)
						if err != nil {
							t.Fatal(err)
						}
						for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: name, adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeAppend} {
							if err = st.SetOption(h.ctx, k, v); err != nil {
								t.Fatal(err)
							}
						}
						if err = st.Bind(h.ctx, rec); err != nil {
							t.Fatal(err)
						}
						n, err := st.ExecuteUpdate(h.ctx)
						if err != nil || n != int64(count) {
							t.Fatal("Arrow append failed", n, err)
						}
						if err = st.Close(h.ctx); err != nil {
							t.Fatal(err)
						}
					}
					elapsed := float64(time.Since(started)) / float64(time.Millisecond)
					profile.mu.Lock()
					profile.phase = ""
					profile.phases[phase].Steps = 1
					profile.phases[phase].Millis = elapsed
					profile.mu.Unlock()
					if rec != nil {
						rec.Release()
					}
					observations = append(observations, observation{Case: name, Sample: sample, Millis: elapsed})
				}
			}
			h.ctx = prior
			rows, _ := h.query("SELECT id,v,payload FROM point_target ORDER BY id")
			if len(rows) != 20000 {
				t.Fatal("incomplete point target fetch", len(rows))
			}
			for i, row := range rows {
				want := int64(9007199254740993)
				if i == 0 {
					want = value
				}
				if row != fmt.Sprintf("%d|%d|row-%d", i+1, want, i+1) {
					t.Fatal("incorrect point target row", i+1)
				}
			}
			for _, name := range []string{"arrow_small", "arrow_batch"} {
				count := 4
				if name == "arrow_batch" {
					count *= 512
				}
				rows, _ := h.query("SELECT id,v,payload FROM " + name + " ORDER BY id")
				if len(rows) != count {
					t.Fatal("incomplete Arrow fetch", len(rows))
				}
				for i, row := range rows {
					if row != fmt.Sprintf("%d|9007199254740993|payload", i+1) {
						t.Fatal("incorrect Arrow row", i+1)
					}
				}
			}
			for i := 0; i < unrelated; i++ {
				h.expectRows(fmt.Sprintf("SELECT COUNT(*) FROM unrelated_%d", i), "0")
			}
			profile.mu.Lock()
			encoded, err := json.Marshal(map[string]any{"unrelated_tables": unrelated, "operations": observations, "foreground_calls": profile.phases, "caller_retries": 0, "maintenance": "disabled; target segments below admission limit"})
			profile.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("snapshot_fixed_profile %s", encoded)
		})
	}
}
