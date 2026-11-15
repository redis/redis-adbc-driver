// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

type pointValidIsolationCase struct {
	Name        string `json:"name"`
	Shape       string `json:"shape"`
	Count       int    `json:"count"`
	Chunk       int    `json:"indexes_per_pipeline_chunk"`
	Serial      bool   `json:"serial"`
	SameIndex   bool   `json:"same_index"`
	Maintenance bool   `json:"maintenance_client"`
}

type pointValidIsolationSample struct {
	*pointValidSample
	Response            string  `json:"response"`
	SequenceMillis      float64 `json:"complete_sequence_milliseconds"`
	ProofMillis         float64 `json:"proof_sequence_milliseconds"`
	ProofAndFirstMillis float64 `json:"proof_and_first_response_milliseconds"`
	ProofReadBytes      int64   `json:"proof_connection_resp_read_bytes"`
	ProofWrittenBytes   int64   `json:"proof_connection_resp_written_bytes"`
	ProofSocketMillis   float64 `json:"proof_connection_socket_read_milliseconds"`
}

// This diagnostic varies only helper requests around one real retained pin.
// It does not remove any proof from SQL or measure normal statement latency.
func TestSnapshotPointValidIsolationProfile(t *testing.T) {
	if os.Getenv("REDIS_POINT_VALID_ISOLATION_PROFILE") != "true" {
		t.Skip("opt-in exclusive verified Software proof/response isolation")
	}
	h, _, _ := newSnapshotHarnessMode(t, true, false)
	h.exec("CREATE TABLE valid_isolation(id BIGINT,amount BIGINT,label VARCHAR)")
	h.exec("INSERT INTO valid_isolation VALUES(1,9007199254740993,'first'),(2,-9223372036854775808,NULL),(3,9223372036854775807,'')")
	oracle := func(expected []string) {
		t.Helper()
		rows, schema := h.query("SELECT id,amount,label FROM valid_isolation ORDER BY id")
		if schema.NumFields() != 3 || schema.Field(0).Type.ID() != arrow.INT64 || schema.Field(1).Type.ID() != arrow.INT64 || schema.Field(2).Type.ID() != arrow.STRING || !reflect.DeepEqual(rows, expected) {
			t.Fatal("ordinary SQL typed/full-row oracle changed")
		}
	}
	oracle([]string{"1|9007199254740993|first", "2|-9223372036854775808|NULL", "3|9223372036854775807|"})
	h.exec("DELETE FROM valid_isolation WHERE __rowid=2")
	h.exec("UPDATE valid_isolation SET amount=9223372036854775807,label='' WHERE __rowid=1")
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
		t.Fatal("single-endpoint Software maintenance client required")
	}
	ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
	defer cancel()
	ctx, transport := impl.executionContext(ctx, &impl.streams)
	defer transport.finish()
	execution, ok := transport.client.(*goredis.Client)
	if !ok {
		t.Fatal("single-endpoint Software execution client required")
	}
	maintenance.AddHook(pointValidHook{"maintenance"})
	execution.AddHook(pointValidHook{"execution"})
	pinned, pin, err := impl.store.acquireSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := impl.store.getTableMetadata(pinned, "public", "valid_isolation")
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
	cases := []pointValidIsolationCase{
		{Name: "none"},
		{Name: "three_pairs_pipeline", Shape: "pairs", Count: 3},
		{Name: "three_info_pipeline", Shape: "info", Count: 3},
		{Name: "one_info_pipeline", Shape: "info", Count: 1},
		{Name: "two_info_pipeline", Shape: "info", Count: 2},
		{Name: "three_info_chunks_one", Shape: "info", Count: 3, Chunk: 1},
		{Name: "three_info_chunks_two", Shape: "info", Count: 3, Chunk: 2},
		{Name: "three_pairs_chunks_one", Shape: "pairs", Count: 3, Chunk: 1},
		{Name: "three_pairs_chunks_two", Shape: "pairs", Count: 3, Chunk: 2},
		{Name: "three_zcard_pipeline", Shape: "zcard", Count: 3},
		{Name: "six_zcard_pipeline", Shape: "zcard", Count: 6},
		{Name: "three_info_serial", Shape: "info", Count: 3, Serial: true},
		{Name: "three_pairs_serial", Shape: "pairs", Count: 3, Serial: true},
		{Name: "six_zcard_serial", Shape: "zcard", Count: 6, Serial: true},
		{Name: "three_same_info_pipeline", Shape: "info", Count: 3, SameIndex: true},
		{Name: "three_pairs_maintenance_pipeline", Shape: "pairs", Count: 3, Maintenance: true},
	}
	type proofReply struct {
		cmd     *goredis.Cmd
		segment snapshotSegment
		info    bool
	}
	var samples []*pointValidIsolationSample
	for round := 0; round < 8; round++ {
		for position := 0; position < len(cases)*2; position++ {
			index := position
			if round%2 == 1 {
				index = len(cases)*2 - 1 - position
			}
			c, response := cases[index/2], "valid"
			if index%2 == 1 {
				response = "ping"
			}
			sequenceStarted := time.Now()
			base := &pointValidSample{Round: round, Case: c.Name}
			sample := &pointValidIsolationSample{pointValidSample: base, Response: response}
			work := context.WithValue(pinned, pointValidSampleKey{}, base)
			request := func() pointValidCall {
				before := maintenance.PoolStats()
				diagnostic := &executionDiagnostics{}
				active := &impl.clientCfg.diagnostics
				if !active.CompareAndSwap(nil, diagnostic) {
					t.Fatal("another execution diagnostic is active")
				}
				defer active.CompareAndSwap(diagnostic, nil)
				started := time.Now()
				var requestErr error
				if response == "valid" {
					requestErr = pin.valid(work)
				} else {
					cmd := goredis.NewCmd(work, "PING")
					requestErr = maintenance.Process(work, onceCmd{cmd})
					if requestErr == nil && (cmd.Err() != nil || cmd.Val() != "PONG") {
						t.Fatal("native PING reply changed")
					}
				}
				ms := float64(time.Since(started)) / float64(time.Millisecond)
				if !active.CompareAndSwap(diagnostic, nil) {
					t.Fatal("diagnostic ownership changed")
				}
				if requestErr != nil {
					t.Fatal("native response failed", requestErr)
				}
				return pointValidCall{Millis: ms, MainPool: pointValidPoolDifference(before, maintenance.PoolStats()), SocketReadMillis: diagnostic.socketRead.seconds() * 1000, ReadBytes: diagnostic.readBytes.Load(), WrittenBytes: diagnostic.writtenBytes.Load()}
			}
			for i := 0; i < 3; i++ {
				base.Before = append(base.Before, request())
			}
			proofClient := execution
			if c.Maintenance {
				proofClient = maintenance
			}
			proofStarted := time.Now()
			proofDiagnostic := &executionDiagnostics{}
			if !impl.clientCfg.diagnostics.CompareAndSwap(nil, proofDiagnostic) {
				t.Fatal("another proof diagnostic is active")
			}
			beforePools := proofClient.PoolStats()
			var replies []proofReply
			for i := 0; i < c.Count; i++ {
				seg := segments[i%len(segments)]
				if c.SameIndex {
					seg = segments[0]
				}
				if c.Shape == "info" || c.Shape == "pairs" {
					replies = append(replies, proofReply{goredis.NewCmd(work, "FT.INFO", seg.Index), seg, true})
				}
				if c.Shape == "zcard" || c.Shape == "pairs" {
					replies = append(replies, proofReply{goredis.NewCmd(work, "ZCARD", snapshotMembers(snapshotDataID(seg.Prefix))), seg, false})
				}
			}
			if c.Serial {
				for _, reply := range replies {
					if err := proofClient.Process(work, onceCmd{reply.cmd}); err != nil {
						t.Fatal("serial native probe failed", err)
					}
				}
			} else if len(replies) > 0 {
				chunkCommands := len(replies)
				if c.Chunk > 0 {
					chunkCommands = c.Chunk
					if c.Shape == "pairs" {
						chunkCommands *= 2
					}
				}
				for offset := 0; offset < len(replies); offset += chunkCommands {
					pipe := proofClient.Pipeline()
					for _, reply := range replies[offset:min(offset+chunkCommands, len(replies))] {
						if err := pipe.Process(work, onceCmd{reply.cmd}); err != nil {
							pipe.Discard()
							t.Fatal("queue native probe failed", err)
						}
					}
					if _, err := pipe.Exec(work); err != nil {
						t.Fatal("native probe pipeline failed", err)
					}
				}
			}
			afterPools := proofClient.PoolStats()
			base.ProofPoolDeltas = map[string]pointValidPoolDelta{"main": pointValidPoolDifference(beforePools, afterPools)}
			if b, a := beforePools.PipelineStats, afterPools.PipelineStats; b != nil && a != nil {
				base.ProofPoolDeltas["pipeline"] = pointValidPoolDelta{a.WaitCount - b.WaitCount, float64(a.WaitDurationNs-b.WaitDurationNs) / float64(time.Millisecond), a.Misses - b.Misses, a.Timeouts - b.Timeouts}
			}
			if err := work.Err(); err != nil {
				t.Fatal("diagnostic deadline failed", err)
			}
			for _, reply := range replies {
				if reply.cmd.Err() != nil {
					t.Fatal("native probe reply failed", reply.cmd.Err())
				}
				seg := reply.segment
				if reply.info {
					info := indexInfoOf(reply.cmd.Val())
					if info.indexing || info.percent < 1 || info.failures != 0 || info.numDocs != seg.Rows || len(info.prefixes) != 1 || info.prefixes[0] != seg.Prefix {
						t.Fatal("native index probe failed independent readiness/identity oracle")
					}
				} else if n, ok := reply.cmd.Val().(int64); !ok || n != seg.Rows {
					t.Fatal("native row-directory probe failed exact cardinality oracle")
				}
			}
			if !impl.clientCfg.diagnostics.CompareAndSwap(proofDiagnostic, nil) {
				t.Fatal("proof diagnostic ownership changed")
			}
			sample.ProofMillis = float64(time.Since(proofStarted)) / float64(time.Millisecond)
			sample.ProofReadBytes, sample.ProofWrittenBytes = proofDiagnostic.readBytes.Load(), proofDiagnostic.writtenBytes.Load()
			sample.ProofSocketMillis = proofDiagnostic.socketRead.seconds() * 1000
			for i := 0; i < 3; i++ {
				base.After = append(base.After, request())
				if i == 0 {
					sample.ProofAndFirstMillis = float64(time.Since(proofStarted)) / float64(time.Millisecond)
				}
			}
			sample.SequenceMillis = float64(time.Since(sequenceStarted)) / float64(time.Millisecond)
			want := 6
			if c.Serial {
				want += len(replies)
			} else if len(replies) > 0 {
				if c.Chunk > 0 {
					want += (c.Count + c.Chunk - 1) / c.Chunk
				} else {
					want++
				}
			}
			if len(base.Requests) != want {
				t.Fatalf("unexpected helper request count: got %d want %d", len(base.Requests), want)
			}
			for _, observed := range base.Requests {
				if observed.Failed {
					t.Fatal("recorded helper request failed")
				}
			}
			samples = append(samples, sample)
		}
	}
	pin.release()
	transport.finish()
	oracle(expected)
	payload, err := json.Marshal(map[string]any{
		"scope":   "native proof/response sequences on one retained real pin and pre-acquired transport; not SQL latency or server CPU",
		"version": 2, "rounds": 8, "cases": cases, "samples": samples, "caller_retries": 0,
		"sequence_order":       "all case/response pairs forward then reverse on alternate rounds; three response calls before and after probes",
		"pool_topology":        map[string]bool{"maintenance_dedicated_pipeline": maintenance.PoolStats().PipelineStats != nil, "execution_dedicated_pipeline": execution.PoolStats().PipelineStats != nil},
		"socket_scope":         "connection-scoped diagnosticConn socket-read elapsed and RESP bytes; concurrent maintenance I/O can contribute; not additive wall-time decomposition or server CPU",
		"ordinary_sql_oracles": "normal complete typed SQL before overlays, before diagnostic, and after release; int64 extrema/>2^53/NULL/empty/deletion visibility",
		"server_duration":      "not measured; no server configuration or SLOWLOG threshold changed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), snapshotPrefix) {
		t.Fatal("private runtime identity in diagnostic")
	}
	t.Logf("snapshot_point_valid_isolation %s", payload)
}
