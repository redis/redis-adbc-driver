// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

type pointValidSampleKey struct{}
type pointValidRequest struct {
	Route  string  `json:"route"`
	Shape  string  `json:"shape"`
	Millis float64 `json:"milliseconds"`
	Failed bool    `json:"failed"`
}
type pointValidSample struct {
	mu              sync.Mutex
	Round           int                            `json:"round"`
	Case            string                         `json:"case"`
	Requests        []pointValidRequest            `json:"requests"`
	Before          []pointValidCall               `json:"before_proof"`
	After           []pointValidCall               `json:"after_proof"`
	ProofPoolDeltas map[string]pointValidPoolDelta `json:"proof_pool_deltas,omitempty"`
}
type pointValidPoolDelta struct {
	WaitCount  uint32  `json:"wait_count"`
	WaitMillis float64 `json:"wait_milliseconds"`
	Misses     uint32  `json:"misses"`
	Timeouts   uint32  `json:"timeouts"`
}
type pointValidCall struct {
	Millis           float64             `json:"wall_milliseconds"`
	MainPool         pointValidPoolDelta `json:"maintenance_main_pool_delta"`
	SocketReadMillis float64             `json:"connection_socket_read_milliseconds"`
	ReadBytes        int64               `json:"connection_resp_read_bytes"`
	WrittenBytes     int64               `json:"connection_resp_written_bytes"`
}
type pointValidHook struct{ route string }

func (pointValidHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h pointValidHook) record(ctx context.Context, shape string, started time.Time, err error) {
	if sample, _ := ctx.Value(pointValidSampleKey{}).(*pointValidSample); sample != nil {
		sample.mu.Lock()
		defer sample.mu.Unlock()
		sample.Requests = append(sample.Requests, pointValidRequest{h.route, shape, float64(time.Since(started)) / float64(time.Millisecond), err != nil})
	}
}
func (h pointValidHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		shape := compactionProfileCategory(c)
		started := time.Now()
		err := next(ctx, c)
		h.record(ctx, shape, started, err)
		return err
	}
}
func (h pointValidHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		shapes := make([]string, len(cs))
		for i, c := range cs {
			shapes[i] = compactionProfileCategory(c)
		}
		shape := compactionProfilePipelineShape(shapes)
		started := time.Now()
		err := next(ctx, cs)
		h.record(ctx, shape, started, err)
		return err
	}
}
func pointValidPoolDifference(before, after *goredis.PoolStats) pointValidPoolDelta {
	return pointValidPoolDelta{after.WaitCount - before.WaitCount, float64(after.WaitDurationNs-before.WaitDurationNs) / float64(time.Millisecond), after.Misses - before.Misses, after.Timeouts - before.Timeouts}
}

// Isolate the first standalone lease request after native segment proofs. This
// measures helper sequences on one pre-acquired execution transport and one pin,
// not SQL latency. Ordinary SQL oracles use the unchanged public execution path.
func TestSnapshotPointValidSequenceProfile(t *testing.T) {
	if os.Getenv("REDIS_POINT_VALID_PROFILE") != "true" {
		t.Skip("opt-in exclusive verified Software first-valid diagnostic")
	}
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	h.exec("CREATE TABLE valid_sequence(id BIGINT,amount BIGINT,label VARCHAR)")
	h.exec("INSERT INTO valid_sequence VALUES(1,9007199254740993,'first'),(2,-9223372036854775808,NULL),(3,9223372036854775807,'')")
	oracle := func(expected []string) {
		t.Helper()
		rows, schema := h.query("SELECT id,amount,label FROM valid_sequence ORDER BY id")
		if schema.NumFields() != 3 || schema.Field(0).Type.ID() != arrow.INT64 || schema.Field(1).Type.ID() != arrow.INT64 || schema.Field(2).Type.ID() != arrow.STRING || !reflect.DeepEqual(rows, expected) {
			t.Fatal("ordinary SQL oracle changed")
		}
	}
	oracle([]string{"1|9007199254740993|first", "2|-9223372036854775808|NULL", "3|9223372036854775807|"})
	h.exec("DELETE FROM valid_sequence WHERE __rowid=2")
	h.exec("UPDATE valid_sequence SET amount=9223372036854775807,label='' WHERE __rowid=1")
	expected := []string{"1|9223372036854775807|", "3|9223372036854775807|"}
	oracle(expected)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	impl := st.(*statementImpl).conn
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	maintenance, ok := impl.store.client.(*goredis.Client)
	if !ok {
		t.Fatal("diagnostic requires a single-endpoint Software client")
	}
	ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
	defer cancel()
	ctx, transport := impl.executionContext(ctx, &impl.streams)
	defer transport.finish()
	execution, ok := transport.client.(*goredis.Client)
	if !ok {
		t.Fatal("diagnostic requires a single-endpoint execution client")
	}
	maintenance.AddHook(pointValidHook{"maintenance"})
	execution.AddHook(pointValidHook{"execution"})
	pinned, pin, err := impl.store.acquireSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := impl.store.getTableMetadata(pinned, "public", "valid_sequence")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSnapshotDescriptor(pinned, meta); err != nil {
		t.Fatal(err)
	}
	segments := meta.snapshotSegments()
	if len(segments) != 3 {
		t.Fatalf("expected exactly three immutable segments, got %d", len(segments))
	}
	// No SQL row read uses this metadata-only shortcut. The diagnostic issues
	// native pin/proof reads, and SQL oracles retain all their ordinary proofs.
	var slowStart int64 = -1
	slowAvailable := false
	if entries, e := raw.SlowLogGet(ctx, 1).Result(); e == nil {
		slowAvailable = true
		if len(entries) > 0 {
			slowStart = entries[0].ID
		}
	}
	cases := []struct {
		name        string
		count       int
		maintenance bool
	}{{"no_segment_proof", 0, false}, {"one_execution_proof", 1, false}, {"three_execution_proofs", 3, false}, {"three_maintenance_proofs", 3, true}}
	var samples []*pointValidSample
	for round := 0; round < 8; round++ {
		for position := range cases {
			index := position
			if round%2 == 1 {
				index = len(cases) - 1 - position
			}
			c := cases[index]
			sample := &pointValidSample{Round: round, Case: c.name}
			work := context.WithValue(pinned, pointValidSampleKey{}, sample)
			valid := func() pointValidCall {
				before := maintenance.PoolStats()
				diagnostic := &executionDiagnostics{}
				active := &impl.clientCfg.diagnostics
				if !active.CompareAndSwap(nil, diagnostic) {
					t.Fatal("another execution diagnostic is active")
				}
				defer active.CompareAndSwap(diagnostic, nil)
				started := time.Now()
				validErr := pin.valid(work)
				ms := float64(time.Since(started)) / float64(time.Millisecond)
				if !active.CompareAndSwap(diagnostic, nil) {
					t.Fatal("execution diagnostic ownership changed")
				}
				if validErr != nil {
					t.Fatal("fresh native pin validation failed", validErr)
				}
				return pointValidCall{Millis: ms, MainPool: pointValidPoolDifference(before, maintenance.PoolStats()), SocketReadMillis: diagnostic.socketRead.seconds() * 1000, ReadBytes: diagnostic.readBytes.Load(), WrittenBytes: diagnostic.writtenBytes.Load()}
			}
			for i := 0; i < 3; i++ {
				sample.Before = append(sample.Before, valid())
			}
			if c.count > 0 {
				proofCtx := work
				proofClient := execution
				if c.maintenance {
					proofClient = maintenance
					proofCtx = context.WithValue(work, transportContextKey{}, (*executionTransport)(nil))
				}
				probes := make([]snapshotSegmentProbe, c.count)
				for i := range probes {
					probes[i] = snapshotSegmentProbe{segment: segments[i], proof: &snapshotSegmentValidation{done: make(chan struct{})}}
				}
				beforePools := proofClient.PoolStats()
				impl.store.validateSnapshotSegments(proofCtx, probes)
				afterPools := proofClient.PoolStats()
				sample.ProofPoolDeltas = map[string]pointValidPoolDelta{"main": pointValidPoolDifference(beforePools, afterPools)}
				if beforePools.PipelineStats != nil && afterPools.PipelineStats != nil {
					b, a := beforePools.PipelineStats, afterPools.PipelineStats
					sample.ProofPoolDeltas["pipeline"] = pointValidPoolDelta{a.WaitCount - b.WaitCount, float64(a.WaitDurationNs-b.WaitDurationNs) / float64(time.Millisecond), a.Misses - b.Misses, a.Timeouts - b.Timeouts}
				}
				for _, probe := range probes {
					if probe.proof.err != nil {
						t.Fatal("ordinary native segment proof failed", probe.proof.err)
					}
				}
			}
			for i := 0; i < 3; i++ {
				sample.After = append(sample.After, valid())
			}
			// Each sequence has six exact standalone lease calls, plus at most one
			// native proof pipeline. No script body, physical key or argument is logged.
			want := 6
			if c.count > 0 {
				want++
			}
			if len(sample.Requests) != want {
				t.Fatalf("unexpected sequence request count: got %d want %d", len(sample.Requests), want)
			}
			for _, request := range sample.Requests {
				if request.Failed {
					t.Fatal("diagnostic request failed")
				}
			}
			samples = append(samples, sample)
		}
	}
	// This read-only threshold-selected slowlog is supplemental native command
	// elapsed time, not CPU or a complete per-sample server trace. Missing entries
	// remain unavailable; the fixture's slowlog configuration is never changed.
	slow := map[string]any{"available": slowAvailable, "threshold_selected": true, "complete": false, "threshold_available": false, "omission": "SLOWLOG unavailable or refused; no server duration evidence"}
	if config, e := raw.ConfigGet(ctx, "slowlog-log-slower-than").Result(); e == nil {
		if n, e := strconv.ParseInt(config["slowlog-log-slower-than"], 10, 64); e == nil {
			slow["threshold_available"] = true
			slow["threshold_microseconds"] = n
		}
	}
	if slowAvailable {
		if entries, e := raw.SlowLogGet(ctx, 256).Result(); e == nil {
			type slowSample struct {
				Category string `json:"category"`
				Micros   int64  `json:"microseconds"`
			}
			var selected []slowSample
			for _, entry := range entries {
				if entry.ID <= slowStart || len(entry.Args) == 0 {
					continue
				}
				args := make([]any, len(entry.Args))
				for i, a := range entry.Args {
					args[i] = a
				}
				category := compactionProfileCategory(goredis.NewCmd(ctx, args...))
				if len(category) > 96 || strings.ContainsAny(category, "\r\n") {
					category = "other"
				}
				selected = append(selected, slowSample{category, entry.Duration.Microseconds()})
			}
			slow["samples"] = selected
			if len(selected) == 0 {
				slow["omission"] = "no threshold-selected sample observed; no server duration inferred"
			} else {
				slow["omission"] = "threshold-selected only; no exact per-sample alignment"
			}
		} else {
			slow["available"] = false
		}
	}
	pin.release()
	transport.finish()
	oracle(expected)
	payload, err := json.Marshal(map[string]any{"scope": "native helper sequence attribution on one real retained pin/descriptor and pre-acquired transport; not SQL latency or server CPU", "rounds": 8, "sequence_order": "four arms forward/reverse alternation; each has three fresh valid calls before/after zero, one or three native segment proofs", "segments": 3, "samples": samples, "server_slowlog": slow, "pool_stats_scope": "valid calls: maintenance main pool; proofs: relevant client main and dedicated pipeline pool when available; counters do not infer server CPU/transport time", "socket_scope": "existing diagnosticConn connection read elapsed/RESP byte counters during valid; may include concurrent maintenance I/O; not TLS framing, server CPU or an additive wall-time decomposition", "pool_topology": map[string]bool{"maintenance_dedicated_pipeline": maintenance.PoolStats().PipelineStats != nil, "execution_dedicated_pipeline": execution.PoolStats().PipelineStats != nil}, "ordinary_sql_oracles": "complete typed results before setup overlays, before diagnostic, and after pin/transport release", "caller_retries": 0, "maintenance": "production workers enabled on their separate unhooked pools"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), snapshotPrefix) {
		t.Fatal("private runtime identity in profile")
	}
	t.Logf("snapshot_point_valid_sequence %s", payload)
}
