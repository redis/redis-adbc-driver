// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
)

// The HASH payload varies at every block and row; a repeated padding string
// would make a nominally large fixture compress into the RAM tier.
func flexColdPayload(id int) string {
	var b strings.Builder
	b.Grow(4096)
	for block := 0; block < 64; block++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("flex-cold:%d:%d", id, block)))
		b.WriteString(hex.EncodeToString(digest[:]))
	}
	return b.String()
}

type flexColdResult struct {
	flexBenchResult
	Workers       int
	Phase         string
	WorkerSeconds []float64
}

// Only numeric storage counters are publishable. INFO can also contain endpoint
// addresses, replication IDs and configuration paths, which are omitted.
func flexColdStorage(t *testing.T, c *connectionImpl) map[string]float64 {
	t.Helper()
	raw, err := c.store.client.Info(context.Background(), "all").Result()
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]float64{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !(strings.Contains(key, "bigstore") || strings.Contains(key, "flash") || strings.Contains(key, "rocksdb") || strings.HasPrefix(key, "used_memory")) {
			continue
		}
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			result[key] = n
		}
	}
	return result
}

func TestFlexColdResourceProfile(t *testing.T) {
	dir := os.Getenv("REDIS_FLEX_COLD_PROFILE_DIR")
	if dir == "" {
		t.Skip("set REDIS_FLEX_COLD_PROFILE_DIR for an isolated tiered-storage profile")
	}
	const rows = 150000
	batchBytes := int64(0)
	if text := os.Getenv("REDIS_FLEX_COLD_BATCH_BYTES"); text != "" {
		var err error
		batchBytes, err = strconv.ParseInt(text, 10, 64)
		if err != nil || batchBytes < 1 {
			t.Fatal("invalid cold-profile Arrow batch byte budget")
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	h, c := flexBenchConn(t, true)
	name := "it_flex_cold_profile"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT, p VARCHAR NOINDEX)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	// Fixture loading is outside query timings. Each batch is bounded; the only
	// raw writes target this new, exclusively owned table and its sequence.
	for first := 1; first <= rows; first += 128 {
		pipe := c.store.client.Pipeline()
		for id := first; id < min(first+128, rows+1); id++ {
			pipe.HSet(h.ctx, meta.prefix()+strconv.Itoa(id), rowIDField, id, "n", id, "p", flexColdPayload(id))
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.store.client.Set(h.ctx, seqKey(defaultSchema, name), rows, 0).Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tiered fixture fully indexed", func() bool { n, err := c.store.countMatches(h.ctx, meta.index(), "*"); return err == nil && n == rows })
	clients := []*connectionImpl{c}
	for i := 1; i < 4; i++ {
		_, next := flexBenchConn(t, true)
		clients = append(clients, next)
	}
	storageBefore := flexColdStorage(t, c)
	file, err := os.OpenFile(filepath.Join(dir, "cold-concurrent-samples.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	type outcome struct {
		seconds float64
		rows    int
		err     error
	}
	for _, workers := range []int{1, 2, 4} {
		for trial := 0; trial < 3; trial++ {
			latencies := make([]float64, workers)
			run := func(ctx context.Context) int {
				d, _ := ctx.Value(diagnosticsContextKey{}).(*executionDiagnostics)
				for _, client := range clients[:workers] {
					client.clientCfg.diagnostics.Store(d)
				}
				defer func() {
					for _, client := range clients[:workers] {
						client.clientCfg.diagnostics.Store(nil)
					}
				}()
				start := make(chan struct{})
				done := make(chan outcome, workers)
				for _, client := range clients[:workers] {
					go func(c *connectionImpl) {
						<-start
						began := time.Now()
						n, err := flexColdFetch(ctx, c, name, rows)
						done <- outcome{time.Since(began).Seconds(), n, err}
					}(client)
				}
				close(start)
				count := 0
				var firstError error
				for i := 0; i < workers; i++ {
					r := <-done
					if r.err != nil && firstError == nil {
						firstError = r.err
					}
					latencies[i] = r.seconds
					count += r.rows
				}
				if firstError != nil {
					t.Fatal(firstError)
				}
				return count
			}
			result := flexBenchMeasure(t, c, flexBenchResult{Kind: "sql", Target: "flex", Schema: "plain", Reader: "bounded", Workload: "flash-dominant-wide", Trial: trial}, run)
			phase := "tiered-repeat"
			if workers == 1 && trial == 0 {
				phase = "first-scan-after-load"
			}
			if err := enc.Encode(flexColdResult{result, workers, phase, latencies}); err != nil {
				t.Fatal(err)
			}
			t.Logf("validated %d independent readers, trial %d: %d rows", workers, trial, result.Rows)
		}
	}
	metadata := map[string]any{"rows": rows, "flex_max_page_rows": flexRangeMaxRows, "payload_bytes_per_row": 4096, "stream_batch_bytes": batchBytes, "payload_bytes": int64(rows) * 4096, "validation": "every field of every row on every worker; exact ascending ID and SHA256-derived payload oracle", "trials_per_concurrency": 3, "workers": []int{1, 2, 4}, "cache_control": "no host-wide cache eviction; first scan after loading and subsequent tiered scans, not guaranteed cold OS page cache", "storage_before": storageBefore, "storage_after": flexColdStorage(t, c), "source": "Software Flex only; fixture loading excluded from query measurements"}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cold-concurrent-metadata.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func flexColdFetch(ctx context.Context, c *connectionImpl, name string, want int) (int, error) {
	st, err := c.NewStatement(ctx)
	if err != nil {
		return 0, err
	}
	defer st.Close(ctx)
	if budget := os.Getenv("REDIS_FLEX_COLD_BATCH_BYTES"); budget != "" {
		if err := st.SetOption(ctx, OptionIntStreamBatchBytes, budget); err != nil {
			return 0, err
		}
	}
	if err := st.SetSqlQuery(ctx, "SELECT n,p FROM "+name); err != nil {
		return 0, err
	}
	r, _, err := st.ExecuteQuery(ctx)
	if err != nil {
		return 0, err
	}
	defer r.Release()
	count := 0
	for r.Next() {
		rec := r.RecordBatch()
		ids, ok := rec.Column(0).(*array.Int64)
		if !ok {
			return count, fmt.Errorf("unexpected tiered ID type")
		}
		values, ok := rec.Column(1).(*array.String)
		if !ok {
			return count, fmt.Errorf("unexpected tiered payload type")
		}
		for i := 0; i < int(rec.NumRows()); i++ {
			count++
			if ids.IsNull(i) || ids.Value(i) != int64(count) || values.IsNull(i) || values.Value(i) != flexColdPayload(count) {
				return count, fmt.Errorf("tiered oracle mismatch at row %d", count)
			}
		}
	}
	if err := r.Err(); err != nil {
		return count, err
	}
	if count != want {
		return count, fmt.Errorf("tiered row count %d, want %d", count, want)
	}
	return count, nil
}
