// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

// Keep pipeline duration by exact privacy-safe shape. The shared compaction
// observer combines all mixed-pipeline duration; counts alone cannot attribute
// the dependent root, bucket and descriptor proof stages.
type pointProtocolProfile struct {
	*compactionPhaseProfile
	PipelineDurations map[string]map[string]*compactionProfileRoundTrips
	StandaloneCalls   map[string][]pointProtocolCall
}

type pointProtocolCall struct {
	Category string  `json:"category"`
	Millis   float64 `json:"milliseconds"`
}

func (p *pointProtocolProfile) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return p.compactionPhaseProfile.ProcessHook(func(ctx context.Context, cmd goredis.Cmder) error {
		phase := p.current()
		category := compactionProfileCategory(cmd)
		started := time.Now()
		err := next(ctx, cmd)
		elapsed := time.Since(started)
		if phase != "" {
			p.mu.Lock()
			p.StandaloneCalls[phase] = append(p.StandaloneCalls[phase], pointProtocolCall{category, float64(elapsed) / float64(time.Millisecond)})
			p.mu.Unlock()
		}
		return err
	})
}

func (p *pointProtocolProfile) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return p.compactionPhaseProfile.ProcessPipelineHook(func(ctx context.Context, cmds []goredis.Cmder) error {
		phase := p.current()
		categories := make([]string, len(cmds))
		for i, c := range cmds {
			categories[i] = compactionProfileCategory(c)
		}
		shape := compactionProfilePipelineShape(categories)
		started := time.Now()
		err := next(ctx, cmds)
		elapsed := time.Since(started)
		if phase != "" {
			p.mu.Lock()
			if p.PipelineDurations[phase] == nil {
				p.PipelineDurations[phase] = map[string]*compactionProfileRoundTrips{}
			}
			if p.PipelineDurations[phase][shape] == nil {
				p.PipelineDurations[phase][shape] = &compactionProfileRoundTrips{}
			}
			v := p.PipelineDurations[phase][shape]
			v.Calls++
			v.Commands += len(cmds)
			v.Millis += float64(elapsed) / float64(time.Millisecond)
			p.mu.Unlock()
		}
		return err
	})
}

// Attribute SQL point reads on one pre-acquired execution transport. Queries
// fully exhaust/release results and close their statements, but per-statement
// pool acquisition, owner registration and transport finish/return are excluded.
// This is partial Go attribution, not public Python/ADBC latency or a server API.
func TestSnapshotPointProtocolProfile(t *testing.T) {
	if os.Getenv("REDIS_POINT_PROTOCOL_PROFILE") == "" {
		t.Skip("set REDIS_POINT_PROTOCOL_PROFILE on an exclusively owned, verified Software fixture")
	}
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	h.exec("CREATE TABLE point_protocol(id BIGINT, amount BIGINT, label VARCHAR)")
	h.exec("INSERT INTO point_protocol VALUES(1,9007199254740993,'first'),(2,-9223372036854775808,NULL),(3,9223372036854775807,'')")
	// An application-owned HASH supplies a transport control without relying on
	// managed physical key discovery or bypassing ownership in the measured SQL.
	control := "point-protocol:control"
	if err := raw.HSet(h.ctx, control, "id", "3", "amount", "9223372036854775807", "label", "").Err(); err != nil {
		t.Fatal(err)
	}
	profile := &pointProtocolProfile{compactionPhaseProfile: &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}, PipelineDurations: map[string]map[string]*compactionProfileRoundTrips{}, StandaloneCalls: map[string][]pointProtocolCall{}}
	ctx, _ := statisticsHookedTransport(t, h, profile)
	prior := h.ctx
	h.ctx = ctx
	defer func() { h.ctx = prior }()
	type observation struct {
		Case          string  `json:"case"`
		Sample        int     `json:"sample"`
		Millis        float64 `json:"milliseconds"`
		ControlMillis float64 `json:"control_milliseconds"`
	}
	var samples []observation
	run := func(name, predicate string, expected []string) {
		t.Helper()
		query := "SELECT id,amount,label FROM point_protocol WHERE " + predicate
		// Warm the execution pool and script policy before measuring. Each statement
		// still obtains a new pin and performs current native ACL/ownership proofs.
		for i := 0; i < 4; i++ {
			rows, schema := h.query(query)
			if schema.NumFields() != 3 || schema.Field(0).Type.ID() != arrow.INT64 || schema.Field(1).Type.ID() != arrow.INT64 || schema.Field(2).Type.ID() != arrow.STRING || !reflect.DeepEqual(rows, expected) {
				t.Fatal("point warmup result mismatch", name)
			}
		}
		for i := 0; i < 16; i++ {
			phase := fmt.Sprintf("%s_%02d", name, i)
			profile.mu.Lock()
			profile.phases[phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
			profile.phase = phase
			profile.mu.Unlock()
			started := time.Now()
			rows, schema := h.query(query)
			elapsed := float64(time.Since(started)) / float64(time.Millisecond)
			profile.mu.Lock()
			profile.phase = ""
			profile.phases[phase].Steps = 1
			profile.phases[phase].Millis = elapsed
			profile.mu.Unlock()
			if schema.NumFields() != 3 || schema.Field(0).Type.ID() != arrow.INT64 || schema.Field(1).Type.ID() != arrow.INT64 || schema.Field(2).Type.ID() != arrow.STRING || !reflect.DeepEqual(rows, expected) {
				t.Fatal("point result mismatch", name, i)
			}
			// Control values are validated independently, including empty string.
			// This fixed-order diagnostic is not a causal A/B timing comparison.
			controlRead := func() float64 {
				began := time.Now()
				values, err := raw.HGetAll(prior, control).Result()
				ms := float64(time.Since(began)) / float64(time.Millisecond)
				if err != nil || len(values) != 3 || values["id"] != "3" || values["amount"] != "9223372036854775807" || values["label"] != "" {
					t.Fatal("invalid transport control")
				}
				return ms
			}
			// The control is deliberately outside the foreground hook and SQL timer.
			samples = append(samples, observation{name, i, elapsed, controlRead()})
		}
	}
	run("single_segment_hit", "__rowid=1", []string{"1|9007199254740993|first"})
	run("single_segment_null", "__rowid=2", []string{"2|-9223372036854775808|NULL"})
	run("single_segment_absent", "__rowid=999", nil)
	// Two overlays remain below the four-segment background compaction trigger.
	// Verify the actual layout rather than assuming setup preserved that shape.
	h.exec("DELETE FROM point_protocol WHERE __rowid=2")
	h.exec("UPDATE point_protocol SET amount=9223372036854775807,label='' WHERE __rowid=1")
	s := snapshotTestStore(t, raw)
	check, pin, err := s.acquireSnapshot(prior)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(check, "public", "point_protocol")
	if err != nil {
		pin.release()
		t.Fatal(err)
	}
	segments := len(meta.snapshotSegments())
	pin.release()
	if segments != 3 {
		t.Fatalf("overlay profile requires exactly three segments, got %d", segments)
	}
	run("overlay_newest", "__rowid=1", []string{"1|9223372036854775807|"})
	run("overlay_tombstone", "__rowid=2", nil)
	run("overlay_oldest", "__rowid=3", []string{"3|9223372036854775807|"})
	run("overlay_absent", "__rowid=999", nil)
	profile.mu.Lock()
	payload, err := json.Marshal(map[string]any{
		"source_scope": "partial Go SQL attribution: full fetch/conversion/pin release/statement close on one pre-acquired execution transport; per-statement pool acquisition/owner registration/finish excluded; not public Python/native timing",
		"clients":      1, "samples_per_case": 16, "warmups_per_case": 4, "caller_retries": 0,
		"overlay_segments": segments, "samples": samples, "foreground_calls": profile.phases,
		"pipeline_duration_by_shape": profile.PipelineDurations,
		"standalone_calls_in_order":  profile.StandaloneCalls,
		"maintenance":                "production worker enabled on its separate unhooked pool",
		"control":                    "known physical-key HGETALL on an independent application HASH; same endpoint, separate pool; no managed logical visibility/pin/ownership/tombstone proofs; not redis-py or semantically equivalent",
	})
	profile.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), snapshotPrefix) {
		t.Fatal("private runtime identity in profile")
	}
	t.Logf("snapshot_point_protocol_profile %s", payload)
}

// The profile is publishable evidence: failures still propagate and sensitive
// keys/fields/values must not survive in either the command or shape summaries.
func TestSnapshotPointProfileKeepsPrivacyAndTransportFailure(t *testing.T) {
	p := &pointProtocolProfile{
		compactionPhaseProfile: &compactionPhaseProfile{phase: "proof", phases: map[string]*compactionProfilePhase{"proof": {RoundTrips: map[string]*compactionProfileRoundTrips{}}}},
		PipelineDurations:      map[string]map[string]*compactionProfileRoundTrips{},
		StandaloneCalls:        map[string][]pointProtocolCall{},
	}
	ctx := context.Background()
	cmds := []goredis.Cmder{
		goredis.NewCmd(ctx, "HMGET", "private-key", "private-field"),
		goredis.NewCmd(ctx, "GET", "private-descriptor"),
	}
	called := 0
	run := p.ProcessPipelineHook(func(context.Context, []goredis.Cmder) error { called++; return io.ErrUnexpectedEOF })
	if err := run(ctx, cmds); err != io.ErrUnexpectedEOF || called != 1 {
		t.Fatal("profile changed transport failure or replayed request", err, called)
	}
	record := p.PipelineDurations["proof"]["hmget,get"]
	if record == nil || record.Calls != 1 || record.Commands != 2 {
		t.Fatal("profile did not retain failed-request attribution")
	}
	direct := p.ProcessHook(func(context.Context, goredis.Cmder) error { called++; return io.ErrUnexpectedEOF })
	if err := direct(ctx, cmds[1]); err != io.ErrUnexpectedEOF || called != 2 {
		t.Fatal("profile changed direct transport failure or replayed request", err, called)
	}
	payload, err := json.Marshal(map[string]any{"commands": p.phases, "pipeline_durations": p.PipelineDurations, "standalone": p.StandaloneCalls})
	if err != nil || strings.Contains(string(payload), "private-") {
		t.Fatal("profile retained sensitive command arguments")
	}
}
