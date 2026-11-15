// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

// Width and pending-proof-count coverage for the response grouping experiment.
// Eight probes repeat three real immutable indexes; they are not an eight-
// segment SQL fixture. Production compaction remains enabled throughout.
func TestSnapshotValidationWidthProfile(t *testing.T) {
	if os.Getenv("REDIS_VALIDATION_WIDTH_PROFILE") != "true" {
		t.Skip("opt-in exclusive verified Software proof-width diagnostic")
	}
	for _, columns := range []int{3, 24} {
		t.Run(fmt.Sprint(columns), func(t *testing.T) {
			h, _, _ := newSnapshotHarnessMode(t, true, false)
			defs := []string{"id BIGINT", "amount BIGINT", "label VARCHAR"}
			var extras []string
			for i := 3; i < columns; i++ {
				defs = append(defs, fmt.Sprintf("extra%d BIGINT", i))
				value := fmt.Sprint(i)
				if i%3 == 0 {
					value = "NULL"
				}
				extras = append(extras, value)
			}
			h.exec("CREATE TABLE validation_width(" + strings.Join(defs, ",") + ")")
			tail, wantTail := "", ""
			if len(extras) > 0 {
				tail, wantTail = ","+strings.Join(extras, ","), "|"+strings.Join(extras, "|")
			}
			h.exec("INSERT INTO validation_width VALUES(1,9007199254740993,'first'" + tail + "),(2,-9223372036854775808,NULL" + tail + "),(3,9223372036854775807,''" + tail + ")")
			oracle := func(expected []string) {
				t.Helper()
				rows, schema := h.query("SELECT * FROM validation_width ORDER BY id")
				if schema.NumFields() != columns || !reflect.DeepEqual(rows, expected) {
					t.Fatal("complete SQL row/width oracle changed")
				}
				for i, field := range schema.Fields() {
					want := arrow.INT64
					if i == 2 {
						want = arrow.STRING
					}
					if field.Type.ID() != want {
						t.Fatal("physical Arrow type changed", i, field.Type)
					}
				}
			}
			oracle([]string{"1|9007199254740993|first" + wantTail, "2|-9223372036854775808|NULL" + wantTail, "3|9223372036854775807|" + wantTail})
			h.exec("DELETE FROM validation_width WHERE __rowid=2")
			h.exec("UPDATE validation_width SET amount=9223372036854775807,label='' WHERE __rowid=1")
			expected := []string{"1|9223372036854775807|" + wantTail, "3|9223372036854775807|" + wantTail}
			oracle(expected)
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
			maintenance.AddHook(pointValidHook{"maintenance"})
			execution.AddHook(pointValidHook{"execution"})
			pinned, pin, err := impl.store.acquireSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			meta, err := impl.store.getTableMetadata(pinned, "public", "validation_width")
			if err != nil {
				t.Fatal(err)
			}
			if err := validateSnapshotDescriptor(pinned, meta); err != nil {
				t.Fatal(err)
			}
			segments := meta.snapshotSegments()
			if len(segments) != 3 {
				t.Fatal("expected three real published sources", len(segments))
			}
			type observation struct {
				Round      int                 `json:"round"`
				Probes     int                 `json:"pending_probes"`
				Millis     float64             `json:"proof_plus_valid_milliseconds"`
				ReadMillis float64             `json:"connection_socket_read_milliseconds"`
				ReadBytes  int64               `json:"connection_resp_read_bytes"`
				WriteBytes int64               `json:"connection_resp_written_bytes"`
				Requests   []pointValidRequest `json:"requests"`
			}
			var samples []observation
			counts := []int{1, 2, 3, 8}
			for round := 0; round < 8; round++ {
				for position := range counts {
					index := position
					if round%2 == 1 {
						index = len(counts) - 1 - position
					}
					n := counts[index]
					if err := pin.valid(pinned); err != nil {
						t.Fatal(err)
					}
					var probes []snapshotSegmentProbe
					for i := 0; i < n; i++ {
						probes = append(probes, snapshotSegmentProbe{segment: segments[i%len(segments)], proof: &snapshotSegmentValidation{done: make(chan struct{})}})
					}
					recorder := &pointValidSample{}
					work := context.WithValue(pinned, pointValidSampleKey{}, recorder)
					diagnostic := &executionDiagnostics{}
					if !impl.clientCfg.diagnostics.CompareAndSwap(nil, diagnostic) {
						t.Fatal("another diagnostic is active")
					}
					started := time.Now()
					impl.store.validateSnapshotSegments(work, probes)
					for _, probe := range probes {
						if probe.proof.err != nil {
							t.Fatal("native proof failed", probe.proof.err)
						}
						select {
						case <-probe.proof.done:
						default:
							t.Fatal("proof owner not released")
						}
					}
					validErr := pin.valid(work)
					ms := float64(time.Since(started)) / float64(time.Millisecond)
					if !impl.clientCfg.diagnostics.CompareAndSwap(diagnostic, nil) {
						t.Fatal("diagnostic ownership changed")
					}
					if validErr != nil {
						t.Fatal("fresh native validation failed", validErr)
					}
					infos, cards, valids := 0, 0, 0
					for _, request := range recorder.Requests {
						if request.Failed {
							t.Fatal("recorded request failed")
						}
						infos += strings.Count(request.Shape, "ft.info")
						cards += strings.Count(request.Shape, "zcard")
						if request.Shape == "control_valid" {
							valids++
						}
					}
					if infos != n || cards != n || valids != 1 {
						t.Fatal("native command/type proof was omitted", infos, cards, valids, n)
					}
					samples = append(samples, observation{round, n, ms, diagnostic.socketRead.seconds() * 1000, diagnostic.readBytes.Load(), diagnostic.writtenBytes.Load(), recorder.Requests})
				}
			}
			pin.release()
			transport.finish()
			oracle(expected)
			payload, err := json.Marshal(map[string]any{"scope": "native proof width/count diagnostic; real three-segment table, eight pending probes repeat real immutable sources; pre-acquired transport; not SQL latency/server CPU", "columns": columns, "real_segments": 3, "samples": samples, "rounds": 8, "caller_retries": 0, "background_compaction": "production workers enabled, unchanged four-segment trigger", "socket_scope": "connection-scoped RESP/socket counters may include concurrent maintenance I/O"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), snapshotPrefix) {
				t.Fatal("private runtime identity in profile")
			}
			t.Logf("snapshot_validation_width %s", payload)
		})
	}
}
