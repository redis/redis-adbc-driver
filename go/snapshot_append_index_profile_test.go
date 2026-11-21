// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// Kept independent of the early-index implementation so this file can be copied
// unchanged onto the parent revision for matched before/after measurements.
type appendIndexProfileHook struct {
	snapshotHook
	creates, notifications, rows           atomic.Int64
	createNS, notifyNS, proofNS, publishNS atomic.Int64
	firstRowNS, lastRowNS                  atomic.Int64
}

func (o *appendIndexProfileHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		rows := snapshotInsertBatchRowKeys(cmd)
		notify := len(a) > 1 && a[1] == snapshotIndexNotifyLua
		create := cmd.Name() == "ft.create"
		proof := cmd.Name() == "ft.info" || cmd.Name() == "ft.search" && len(a) > 2 && a[2] == "@"+rowIDField+":[1 +inf]"
		publish := false
		if snapshotObservedControlName(cmd) == "eval" && len(a) > 1 && a[1] == snapshotControl {
			for _, arg := range a[3:] {
				if arg == "publish" || arg == "publish_append" || arg == "seal" {
					publish = true
				}
			}
		}
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start).Nanoseconds()
		// Count completed mutations only; script-cache/permission refusals did
		// not write rows. Unknown replies are failures of the whole profile.
		if err != nil {
			return err
		}
		if create {
			o.creates.Add(1)
			o.createNS.Add(duration)
		}
		if notify {
			o.notifications.Add(1)
			o.notifyNS.Add(duration)
		}
		if proof {
			o.proofNS.Add(duration)
		}
		if publish {
			o.publishNS.Add(duration)
		}
		if len(rows) > 0 {
			o.firstRowNS.CompareAndSwap(0, start.UnixNano())
			o.lastRowNS.Store(time.Now().UnixNano())
			o.rows.Add(int64(len(rows)))
		}
		return nil
	}
}

type appendIndexProfileReader struct {
	array.RecordReader
	first, eof time.Time
}

func (r *appendIndexProfileReader) Next() bool {
	if r.first.IsZero() {
		r.first = time.Now()
	}
	ok := r.RecordReader.Next()
	if !ok {
		r.eof = time.Now()
	}
	return ok
}

// This measures acknowledged publication, then fully validates every value,
// NULL, unique logical ID and total row count outside timing. Source columns
// are synthetic int64 (including >2^53), not the canonical ClickBench dataset.
// Run the Linux binary in the server namespace for transport-controlled timing;
// collect server/client CPU, RSS and I/O externally. Command durations overlap
// across workers and are diagnostic, not additive wall-time phases.
func TestSnapshotAppendIndexProfile(t *testing.T) {
	if os.Getenv("REDIS_APPEND_INDEX_PROFILE") != "true" {
		t.Skip("explicit owned Software/QPF4 profile only")
	}
	rows, writers := 100000, 1
	for key, target := range map[string]*int{"REDIS_APPEND_INDEX_PROFILE_ROWS": &rows, "REDIS_APPEND_INDEX_PROFILE_WRITERS": &writers} {
		if value := os.Getenv(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				t.Fatal(key, err)
			}
			*target = n
		}
	}
	if rows < 1 || rows > 100000 || (writers != 1 && writers != 4) || rows%writers != 0 {
		t.Fatal("use 1..100000 total rows divisible by 1 or 4 writers")
	}
	h, _, db := newSnapshotHarnessMode(t, false, false)
	declarations := make([]string, 105)
	for col := range declarations {
		name := fmt.Sprintf("c%d", col)
		if col == 0 {
			name = "id"
		}
		declarations[col] = name + " BIGINT NOINDEX"
	}
	h.exec("CREATE TABLE profile(" + strings.Join(declarations, ",") + ")")
	rec := followupRecord(rows)
	defer rec.Release()
	statements := make([]adbc.StatementWithContext, writers)
	contexts := make([]context.Context, writers)
	readers := make([]*appendIndexProfileReader, writers)
	hooks := make([]*appendIndexProfileHook, writers)
	for writer := range writers {
		conn := admissionConnection(t, db)
		stmt, err := conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = stmt.Close(context.Background()) })
		for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: "profile", adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeAppend} {
			if err := stmt.SetOption(h.ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
		part := rec.NewSlice(int64(writer*rows/writers), int64((writer+1)*rows/writers))
		r, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{part})
		part.Release()
		if err != nil {
			t.Fatal(err)
		}
		reader := &appendIndexProfileReader{RecordReader: r}
		if err := stmt.BindStream(h.ctx, reader); err != nil {
			reader.Release()
			t.Fatal(err)
		}
		reader.Release()
		hook := &appendIndexProfileHook{}
		// Both execution and maintenance pools must be observed.
		view := &sqlHarness{t: t, ctx: h.ctx, conn: conn}
		ctx, _ := statisticsHookedTransport(t, view, hook)
		statements[writer], contexts[writer], readers[writer], hooks[writer] = stmt, ctx, reader, hook
	}
	type result struct {
		writer  int
		n       int64
		err     error
		elapsed time.Duration
	}
	done := make(chan result, writers)
	start := time.Now()
	for writer, stmt := range statements {
		go func() {
			begin := time.Now()
			n, err := stmt.ExecuteUpdate(contexts[writer])
			done <- result{writer, n, err, time.Since(begin)}
		}()
	}
	results := make([]result, writers)
	for range writers {
		r := <-done
		results[r.writer] = r
	}
	elapsed := time.Since(start)
	for _, r := range results {
		if r.err != nil || r.n != int64(rows/writers) {
			t.Fatalf("writer %d acknowledged %d: %v", r.writer, r.n, r.err)
		}
	}
	actual, err := followupFetch(h.ctx, h.conn, "SELECT * FROM profile ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	followupValidate(t, actual, 1, int64(rows))
	t.Logf("APPEND_PROFILE rows=%d columns=105 writers=%d route=%q acknowledged_ms=%.3f validated=true", rows, writers, os.Getenv("REDIS_APPEND_INDEX_PROFILE_ROUTE"), float64(elapsed.Microseconds())/1000)
	for writer, hook := range hooks {
		reader := readers[writer]
		if hook.rows.Load() != int64(rows/writers) || reader.eof.IsZero() {
			t.Fatalf("incomplete observation writer %d rows=%d", writer, hook.rows.Load())
		}
		t.Logf("APPEND_PROFILE_WORKER writer=%d input_ms=%.3f row_write_wall_ms=%.3f creates=%d create_ms=%.3f notify_calls=%d notify_ms=%.3f native_info_proof_ms=%.3f seal_publish_ms=%.3f acknowledged_ms=%.3f", writer,
			float64(reader.eof.Sub(reader.first).Microseconds())/1000, float64(hook.lastRowNS.Load()-hook.firstRowNS.Load())/1e6, hook.creates.Load(), float64(hook.createNS.Load())/1e6, hook.notifications.Load(), float64(hook.notifyNS.Load())/1e6, float64(hook.proofNS.Load())/1e6, float64(hook.publishNS.Load())/1e6, float64(results[writer].elapsed.Microseconds())/1000)
	}
}
