// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Controlled synthetic append comparison, not ClickBench. Background workers
// are disabled for attribution; the production-worker pressure test is separate.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSnapshotParallelAppendProfile(t *testing.T) {
	if os.Getenv("REDIS_PARALLEL_APPEND_PROFILE") != "true" {
		t.Skip("opt-in synthetic append profile")
	}
	if os.Getenv("REDIS_FLEX_BENCH_CONTAINER") == "" {
		t.Fatal("owned Software container required for server process counters")
	}
	for trial := 0; trial < 3; trial++ {
		order := []int{1, 4}
		if trial%2 == 1 {
			order = []int{4, 1}
		}
		for _, concurrency := range order {
			t.Run(fmt.Sprintf("trial=%d/concurrency=%d", trial, concurrency), func(t *testing.T) {
				h, raw, db := newSnapshotHarnessMode(t, false, false)
				h.exec("CREATE TABLE t(id BIGINT NOT NULL, v BIGINT, s TEXT)")
				const streams = 4
				const rowsPerStream = 4096
				const total = streams * rowsPerStream
				schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "v", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "s", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
				statements := make([]adbc.StatementWithContext, streams)
				samples := make([]*managedTailSample, streams)
				epoch := time.Now()
				for i := range statements {
					st, err := admissionConnection(t, db).NewStatement(h.ctx)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = st.Close(h.ctx) })
					if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, "t"); err != nil {
						t.Fatal(err)
					}
					if err := st.SetOption(h.ctx, adbc.OptionKeyIngestMode, adbc.OptionValueIngestModeAppend); err != nil {
						t.Fatal(err)
					}
					builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
					for n := range rowsPerStream {
						id := int64(i*rowsPerStream + n)
						builder.Field(0).(*array.Int64Builder).Append(id)
						builder.Field(1).(*array.Int64Builder).Append(9007199254740993 + id*7)
						builder.Field(2).(*array.StringBuilder).Append(fmt.Sprintf("row-%d", id))
					}
					rec := builder.NewRecordBatch()
					builder.Release()
					reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
					rec.Release()
					if err != nil {
						t.Fatal(err)
					}
					if err := st.BindStream(h.ctx, reader); err != nil {
						reader.Release()
						t.Fatal(err)
					}
					reader.Release()
					st.(*statementImpl).conn.store.client.AddHook(&managedTailHook{epoch: epoch})
					statements[i] = st
					samples[i] = &managedTailSample{Writer: i, Kind: "arrow_append"}
				}
				serverBefore := flexBenchServerStats(t)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				cpu, _ := flexBenchCPU()
				start := time.Now()
				execute := func(i int) error {
					sample := samples[i]
					sample.StartNS = time.Since(epoch).Nanoseconds()
					ctx := context.WithValue(h.ctx, managedTailOperationKey{}, sample)
					n, err := statements[i].ExecuteUpdate(ctx)
					sample.ElapsedNS = time.Since(epoch).Nanoseconds() - sample.StartNS
					if err == nil && n != rowsPerStream {
						return fmt.Errorf("stream %d affected %d rows", i, n)
					}
					return err
				}
				if concurrency == 1 {
					for i := range statements {
						if err := execute(i); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					var wg sync.WaitGroup
					errs := make(chan error, streams)
					for i := range statements {
						wg.Go(func() { errs <- execute(i) })
					}
					wg.Wait()
					close(errs)
					for err := range errs {
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				elapsed := time.Since(start)
				cpuAfter, rss := flexBenchCPU()
				runtime.ReadMemStats(&after)
				serverAfter := flexBenchServerStats(t)
				h.expectRows("SELECT COUNT(*),COUNT(DISTINCT id),COUNT(DISTINCT __rowid),SUM(id) FROM t", fmt.Sprintf("%d|%d|%d|%d", total, total, total, int64(total)*(total-1)/2))
				h.expectRows("SELECT COUNT(*) FROM t WHERE v <> 9007199254740993+id*7 OR s <> 'row-'||id", "0")
				s := snapshotTestStore(t, raw)
				ctx, pin, err := s.acquireSnapshot(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				meta, err := s.getTable(ctx, defaultSchema, "t")
				if err != nil {
					t.Fatal(err)
				}
				var physical int64
				for _, seg := range meta.snapshotSegments() {
					physical += seg.Rows
				}
				if physical != total {
					t.Fatal("directory/index cardinality", physical)
				}
				pin.release()
				maintenanceServerBefore := flexBenchServerStats(t)
				maintenanceCPU, _ := flexBenchCPU()
				maintenance := time.Now()
				parallelAppendProfileDrain(t, s)
				compaction := time.Since(maintenance)
				maintenanceCPUAfter, _ := flexBenchCPU()
				maintenanceServerAfter := flexBenchServerStats(t)
				h.expectRows("SELECT COUNT(*),COUNT(DISTINCT __rowid),SUM(id) FROM t", fmt.Sprintf("%d|%d|%d", total, total, int64(total)*(total-1)/2))
				var admissionNS, publishNS, publicationPhaseNS int64
				for _, sample := range samples {
					// Hooks contain Arrow admission and metadata CAS calls. Summed wall
					// durations can overlap across streams; they are not server CPU time.
					for _, call := range sample.Calls {
						if call.Category == "control_admit_append" && call.AdmissionGranted {
							sample.AdmissionNS = call.StartNS + call.ElapsedNS - sample.StartNS
							break
						}
					}
					admissionNS += sample.AdmissionNS
					var firstPublish, lastPublish int64
					for _, call := range sample.Calls {
						if call.Category == "control_publish" {
							publishNS += call.ElapsedNS
							if firstPublish == 0 || call.StartNS < firstPublish {
								firstPublish = call.StartNS
							}
							lastPublish = max(lastPublish, call.StartNS+call.ElapsedNS)
						}
					}
					if firstPublish > 0 {
						publicationPhaseNS += lastPublish - firstPublish
					}
				}
				payload := map[string]any{
					"trial":                                 trial,
					"concurrency":                           concurrency,
					"rows":                                  total,
					"seconds":                               elapsed.Seconds(),
					"client_cpu_seconds":                    cpuAfter - cpu,
					"client_allocated_bytes":                after.TotalAlloc - before.TotalAlloc,
					"client_heap_after_bytes":               after.HeapAlloc,
					"client_lifetime_peak_rss_bytes":        rss,
					"server_cpu_seconds":                    serverAfter.CPUSeconds - serverBefore.CPUSeconds,
					"server_rss_before_bytes":               serverBefore.RSSBytes,
					"server_rss_after_bytes":                serverAfter.RSSBytes,
					"summed_admission_seconds":              float64(admissionNS) / 1e9,
					"summed_publish_command_seconds":        float64(publishNS) / 1e9,
					"summed_publication_cas_rebase_seconds": float64(publicationPhaseNS) / 1e9,
					"compaction_seconds":                    compaction.Seconds(),
					"compaction_client_cpu_seconds":         maintenanceCPUAfter - maintenanceCPU,
					"compaction_server_cpu_seconds":         maintenanceServerAfter.CPUSeconds - maintenanceServerBefore.CPUSeconds,
					"compaction_server_rss_after_bytes":     maintenanceServerAfter.RSSBytes,
					"background_workers":                    false,
					"flex":                                  os.Getenv("REDIS_SNAPSHOT_FLEX") == "true",
					"client_os":                             runtime.GOOS,
					"client_arch":                           runtime.GOARCH,
					"route":                                 "host Docker published TLS port",
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("APPEND_PROFILE %s", data)
			})
		}
	}
}

func parallelAppendProfileDrain(t *testing.T, s *store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	token := snapshotID()
	for {
		progress, err := s.snapshotCompactionStep(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		if !progress {
			job, err := s.readSnapshotCompaction(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if job == nil {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
	}
}
