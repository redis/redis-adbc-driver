// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

// Vary client/pipeline-pool routing without moving any production lease proof.
// Pipeline PING is one fresh native command, not lease batching or cached proof.
func TestSnapshotReplyRoutingProfile(t *testing.T) {
	if os.Getenv("REDIS_REPLY_ROUTING_PROFILE") != "true" {
		t.Skip("opt-in exclusive verified Software response-routing diagnostic")
	}
	for _, policy := range []string{"default", "no_dedicated_pipeline_pool"} {
		t.Run(policy, func(t *testing.T) {
			u, err := url.Parse(os.Getenv("REDIS_SNAPSHOT_URI"))
			if err != nil {
				t.Fatal("invalid private fixture URI")
			}
			q := u.Query()
			q.Del("pipeline_pool_size")
			if policy != "default" {
				q.Set("pipeline_pool_size", "-1")
			}
			u.RawQuery = q.Encode()
			h, raw, _ := newSnapshotHarnessOptions(t, true, false, map[string]string{adbc.OptionKeyURI: u.String()})
			defs := []string{"id BIGINT", "amount BIGINT", "label VARCHAR"}
			var extras []string
			for i := 3; i < 24; i++ {
				defs = append(defs, fmt.Sprintf("extra%d BIGINT", i))
				extras = append(extras, fmt.Sprint(i))
			}
			h.exec("CREATE TABLE reply_routing(" + strings.Join(defs, ",") + ")")
			tail, expectedTail := ","+strings.Join(extras, ","), "|"+strings.Join(extras, "|")
			h.exec("INSERT INTO reply_routing VALUES(1,9007199254740993,'first'" + tail + "),(2,-9223372036854775808,NULL" + tail + "),(3,9223372036854775807,''" + tail + ")")
			oracle := func(expected []string) {
				t.Helper()
				rows, schema := h.query("SELECT * FROM reply_routing ORDER BY id")
				if schema.NumFields() != 24 || !reflect.DeepEqual(rows, expected) {
					t.Fatal("normal SQL complete-result oracle changed")
				}
				for i, f := range schema.Fields() {
					want := arrow.INT64
					if i == 2 {
						want = arrow.STRING
					}
					if f.Type.ID() != want {
						t.Fatal("normal SQL physical Arrow type changed")
					}
				}
			}
			oracle([]string{"1|9007199254740993|first" + expectedTail, "2|-9223372036854775808|NULL" + expectedTail, "3|9223372036854775807|" + expectedTail})
			h.exec("DELETE FROM reply_routing WHERE __rowid=2")
			h.exec("UPDATE reply_routing SET amount=9223372036854775807,label='' WHERE __rowid=1")
			expected := []string{"1|9223372036854775807|" + expectedTail, "3|9223372036854775807|" + expectedTail}
			oracle(expected)
			type sequenceCase struct {
				Name  string `json:"name"`
				Shape string `json:"shape"`
				Count int    `json:"count"`
				Bytes int    `json:"payload_bytes"`
			}
			cases := []sequenceCase{{Name: "one_info", Shape: "info", Count: 1}, {Name: "three_same_info", Shape: "info", Count: 3}, {Name: "three_zcard", Shape: "zcard", Count: 3}}
			payloads := map[int]string{}
			for _, n := range []int{4096, 8000, 8191, 8192, 8193, 16384, 32768} {
				payloads[n] = strings.Repeat("x", n)
				if err := raw.Set(h.ctx, fmt.Sprint("reply-routing:payload:", n), payloads[n], 0).Err(); err != nil {
					t.Fatal("failed to prepare application-owned bulk control")
				}
				cases = append(cases, sequenceCase{Name: fmt.Sprint("bulk_", n), Shape: "get", Count: 1, Bytes: n})
			}
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			impl := st.(*statementImpl).conn
			if err := st.Close(h.ctx); err != nil {
				t.Fatal(err)
			}
			maintenance := impl.store.client.(*goredis.Client)
			ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
			defer cancel()
			ctx, transport := impl.executionContext(ctx, &impl.streams)
			defer transport.finish()
			execution := transport.client.(*goredis.Client)
			wantDedicated := policy == "default"
			if (execution.PoolStats().PipelineStats != nil) != wantDedicated || (maintenance.PoolStats().PipelineStats != nil) != wantDedicated {
				t.Fatal("requested native pipeline pool policy was not effective")
			}
			maintenance.AddHook(pointValidHook{"maintenance"})
			execution.AddHook(pointValidHook{"execution"})
			pinned, pin, err := impl.store.acquireSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			meta, err := impl.store.getTableMetadata(pinned, "public", "reply_routing")
			if err != nil {
				t.Fatal(err)
			}
			if err := validateSnapshotDescriptor(pinned, meta); err != nil {
				t.Fatal(err)
			}
			segments := meta.snapshotSegments()
			if len(segments) != 3 {
				t.Fatal("three real immutable segments required", len(segments))
			}
			seg := segments[0]
			type call struct {
				Millis     float64                        `json:"wall_milliseconds"`
				Socket     float64                        `json:"connection_socket_read_milliseconds"`
				ReadBytes  int64                          `json:"connection_resp_read_bytes"`
				WriteBytes int64                          `json:"connection_resp_written_bytes"`
				Pool       map[string]pointValidPoolDelta `json:"client_pool_deltas"`
			}
			type sample struct {
				Round      int                 `json:"round"`
				Case       string              `json:"case"`
				Route      string              `json:"response_route"`
				Before     []call              `json:"before_proof"`
				After      []call              `json:"after_proof"`
				Sequence   float64             `json:"complete_sequence_milliseconds"`
				ProofFirst float64             `json:"proof_and_first_response_milliseconds"`
				ProofRead  int64               `json:"proof_connection_resp_read_bytes"`
				Requests   []pointValidRequest `json:"requests"`
			}
			difference := func(before, after *goredis.PoolStats) map[string]pointValidPoolDelta {
				out := map[string]pointValidPoolDelta{"main": pointValidPoolDifference(before, after)}
				if b, a := before.PipelineStats, after.PipelineStats; b != nil && a != nil {
					out["pipeline"] = pointValidPoolDelta{a.WaitCount - b.WaitCount, float64(a.WaitDurationNs-b.WaitDurationNs) / float64(time.Millisecond), a.Misses - b.Misses, a.Timeouts - b.Timeouts}
				}
				return out
			}
			routes := []string{"scalar_maintenance", "scalar_execution", "pipeline_maintenance", "pipeline_execution"}
			var samples []sample
			for round := 0; round < 8; round++ {
				for position := 0; position < len(cases)*len(routes); position++ {
					index := position
					if round%2 == 1 {
						index = len(cases)*len(routes) - 1 - position
					}
					c, route := cases[index/len(routes)], routes[index%len(routes)]
					if err := pin.valid(pinned); err != nil {
						t.Fatal(err)
					}
					s := sample{Round: round, Case: c.Name, Route: route}
					recorder := &pointValidSample{}
					work := context.WithValue(pinned, pointValidSampleKey{}, recorder)
					sequenceStarted := time.Now()
					ping := func() call {
						client := maintenance
						if strings.HasSuffix(route, "execution") {
							client = execution
						}
						before := client.PoolStats()
						diagnostic := &executionDiagnostics{}
						if !impl.clientCfg.diagnostics.CompareAndSwap(nil, diagnostic) {
							t.Fatal("another diagnostic is active")
						}
						started := time.Now()
						cmd := goredis.NewCmd(work, "PING")
						if strings.HasPrefix(route, "pipeline_") {
							pipe := client.Pipeline()
							if err := pipe.Process(work, onceCmd{cmd}); err != nil {
								pipe.Discard()
								t.Fatal("queue native PING failed", err)
							}
							if _, err := pipe.Exec(work); err != nil {
								t.Fatal("native pipeline PING failed", err)
							}
						} else if err := client.Process(work, onceCmd{cmd}); err != nil {
							t.Fatal("native scalar PING failed", err)
						}
						ms := float64(time.Since(started)) / float64(time.Millisecond)
						if !impl.clientCfg.diagnostics.CompareAndSwap(diagnostic, nil) {
							t.Fatal("diagnostic ownership changed")
						}
						if cmd.Err() != nil || cmd.Val() != "PONG" {
							t.Fatal("native PING result changed")
						}
						return call{ms, diagnostic.socketRead.seconds() * 1000, diagnostic.readBytes.Load(), diagnostic.writtenBytes.Load(), difference(before, client.PoolStats())}
					}
					for i := 0; i < 3; i++ {
						s.Before = append(s.Before, ping())
					}
					proofStarted := time.Now()
					proofDiagnostic := &executionDiagnostics{}
					if !impl.clientCfg.diagnostics.CompareAndSwap(nil, proofDiagnostic) {
						t.Fatal("another proof diagnostic is active")
					}
					pipe := execution.Pipeline()
					var replies []*goredis.Cmd
					for i := 0; i < c.Count; i++ {
						args := []any{"FT.INFO", seg.Index}
						if c.Shape == "zcard" {
							args = []any{"ZCARD", snapshotMembers(snapshotDataID(seg.Prefix))}
						} else if c.Shape == "get" {
							args = []any{"GET", fmt.Sprint("reply-routing:payload:", c.Bytes)}
						}
						cmd := goredis.NewCmd(work, args...)
						if err := pipe.Process(work, onceCmd{cmd}); err != nil {
							pipe.Discard()
							t.Fatal("queue reply control failed", err)
						}
						replies = append(replies, cmd)
					}
					if _, err := pipe.Exec(work); err != nil {
						t.Fatal("reply control pipeline failed", err)
					}
					if !impl.clientCfg.diagnostics.CompareAndSwap(proofDiagnostic, nil) {
						t.Fatal("proof diagnostic ownership changed")
					}
					s.ProofRead = proofDiagnostic.readBytes.Load()
					for _, cmd := range replies {
						if cmd.Err() != nil {
							t.Fatal("reply control native result failed", cmd.Err())
						}
						switch c.Shape {
						case "get":
							if cmd.Val() != payloads[c.Bytes] {
								t.Fatal("complete application bulk string changed")
							}
						case "zcard":
							if cmd.Val() != seg.Rows {
								t.Fatal("exact native directory count changed")
							}
						case "info":
							info := indexInfoOf(cmd.Val())
							if info.indexing || info.percent < 1 || info.failures != 0 || info.numDocs != seg.Rows || len(info.prefixes) != 1 || info.prefixes[0] != seg.Prefix {
								t.Fatal("exact native index identity/readiness changed")
							}
						}
					}
					for i := 0; i < 3; i++ {
						s.After = append(s.After, ping())
						if i == 0 {
							s.ProofFirst = float64(time.Since(proofStarted)) / float64(time.Millisecond)
						}
					}
					s.Sequence = float64(time.Since(sequenceStarted)) / float64(time.Millisecond)
					s.Requests = recorder.Requests
					if len(s.Requests) != 7 {
						t.Fatal("one fresh control pipeline and six PING requests required", len(s.Requests))
					}
					for _, request := range s.Requests {
						if request.Failed {
							t.Fatal("recorded native request failed")
						}
					}
					samples = append(samples, s)
				}
			}
			pin.release()
			transport.finish()
			oracle(expected)
			opts := execution.Options()
			payload, err := json.Marshal(map[string]any{"scope": "native response-routing helper sequences on a real three-segment pin; not SQL latency, lease batching or server CPU", "pool_policy": policy, "cases": cases, "routes": routes, "rounds": 8, "samples": samples, "caller_retries": 0, "ordinary_sql_oracles": "complete normal typed results before/after, unchanged production workers/proofs", "socket_scope": "connection-scoped diagnosticConn RESP/read counters may include concurrent maintenance I/O", "client_options": map[string]int{"read_buffer": opts.ReadBufferSize, "write_buffer": opts.WriteBufferSize, "pipeline_read_buffer_option": opts.PipelineReadBufferSize, "pipeline_write_buffer_option": opts.PipelineWriteBufferSize, "pipeline_pool_size": opts.PipelinePoolSize, "library_default_pipeline_buffer": goredis.DefaultPipelineBufferSize}, "pool_topology": map[string]bool{"maintenance_dedicated_pipeline": maintenance.PoolStats().PipelineStats != nil, "execution_dedicated_pipeline": execution.PoolStats().PipelineStats != nil}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), snapshotPrefix) {
				t.Fatal("private runtime identity in diagnostic")
			}
			t.Logf("snapshot_reply_routing %s", payload)
		})
	}
}
