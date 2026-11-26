// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// Deterministic discovery proves that first-batch delivery does not depend on
// enumerating the entire source. Real identity/index/HASH reads still run.
type liveStreamHookKey struct{}

type liveStreamHook struct {
	snapshotHook
	pages          [][]string
	scans, fetched int
	failScan       int
	denyIndex      bool
	failFetch      bool
}

func (h *liveStreamHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if ctx.Value(liveStreamHookKey{}) == h && h.denyIndex && cmd.Name() == "ft.info" {
			err := controlScriptNativeError("NOPERM this user has no permissions to run the 'ft.info' command")
			cmd.SetErr(err)
			return err
		}
		if ctx.Value(liveStreamHookKey{}) != h || cmd.Name() != "scan" {
			return next(ctx, cmd)
		}
		marker, ok := cmd.(interface{ NoRetry() bool })
		if !ok || !marker.NoRetry() {
			return errors.New("live SCAN permits replay")
		}
		h.scans++
		if h.failScan == h.scans {
			cmd.SetErr(io.ErrUnexpectedEOF)
			return io.ErrUnexpectedEOF
		}
		scan := cmd.(unmanagedProjectionCmd).Cmder.(*goredis.ScanCmd)
		cursor := uint64(h.scans)
		if h.scans >= len(h.pages) {
			cursor = 0
		}
		scan.SetVal(h.pages[h.scans-1], cursor)
		return nil
	}
}
func (h *liveStreamHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, commands []goredis.Cmder) error {
		if ctx.Value(liveStreamHookKey{}) != h {
			return next(ctx, commands)
		}
		for _, cmd := range commands {
			if cmd.Name() != "hgetall" {
				continue
			}
			marker, ok := cmd.(interface{ NoRetry() bool })
			if !ok || !marker.NoRetry() {
				return errors.New("live HASH fetch permits replay")
			}
			h.fetched++
		}
		if h.failFetch && h.fetched > 0 {
			for _, cmd := range commands {
				cmd.SetErr(io.ErrUnexpectedEOF)
			}
			return io.ErrUnexpectedEOF
		}
		return next(ctx, commands)
	}
}

func liveStreamFixture(t *testing.T) (*hashFixture, [][]string) {
	f := newHashFixture(t, "it:live:stream:")
	binary := f.prefix + string([]byte{'b', 0, 'c'})
	if !strings.ContainsRune(binary, 0) {
		t.Fatal("binary fixture lacks zero byte")
	}
	keys := []string{f.prefix + "a", f.prefix + "b", binary, f.prefix + "d", f.prefix + "null"}
	for i, key := range keys[:4] {
		f.hset(key, "n", fmt.Sprint((i+1)*10), "text", strings.Repeat("x", 100))
	}
	f.hset(keys[4], "text", "null numeric")
	f.hset(f.prefix+"nested:child", "n", "999")
	if _, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_stream", Apply: true, Types: map[string]string{"n": "BIGINT", "text": "VARCHAR"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(f.h.ctx, f.opts, "it_live_stream"); err != nil {
			t.Error(err)
		}
	})
	if err := f.h.conn.(adbc.GetSetOptionsWithContext).SetOption(f.h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	// Includes empty pages, duplicate keys, nested collection, and vanished key.
	return f, [][]string{{keys[0], keys[1], keys[0], f.prefix + "nested:child"}, {}, {keys[1], keys[2], f.prefix + "gone"}, {keys[3], keys[4]}}
}
func liveTestStream(t *testing.T, f *hashFixture, hook *liveStreamHook, query string, opts map[string]string) (adbc.StatementWithContext, array.RecordReader, error) {
	t.Helper()
	st, err := f.h.conn.NewStatement(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close(f.h.ctx) })
	if err = st.SetSqlQuery(f.h.ctx, query); err != nil {
		t.Fatal(err)
	}
	for key, value := range opts {
		if err = st.SetOption(f.h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(liveArrowExecution(t, f, st, hook), liveStreamHookKey{}, hook)
	rdr, _, err := st.ExecuteQuery(ctx)
	return st, rdr, err
}
func TestLiveCollectionIncrementalStream(t *testing.T) {
	f, pages := liveStreamFixture(t)
	cases := []struct {
		name, query string
		want        []int64
		nulls       int
		opts        map[string]string
	}{
		{"full", "SELECT n FROM redis_live.it_live_stream", []int64{10, 20, 30, 40}, 1, nil},
		{"residual", "SELECT n+1 FROM redis_live.it_live_stream WHERE n%20=0", []int64{21, 41}, 0, nil},
		{"offset_limit", "SELECT n FROM redis_live.it_live_stream LIMIT 2 OFFSET 1", []int64{20, 30}, 0, nil},
		{"sparse", "SELECT n FROM redis_live.it_live_stream WHERE n=40", []int64{40}, 0, nil},
		{"byte_budget", "SELECT n FROM redis_live.it_live_stream", []int64{10, 20, 30, 40}, 1, map[string]string{OptionIntStreamBatchBytes: "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := &liveStreamHook{pages: pages}
			opts := map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"}
			for k, v := range tc.opts {
				opts[k] = v
			}
			_, rdr, err := liveTestStream(t, f, hook, tc.query, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer rdr.Release()
			if tc.name == "full" && (hook.scans != 1 || hook.fetched != 2) {
				t.Fatalf("eager source: scans=%d fetched=%d", hook.scans, hook.fetched)
			}
			var got []int64
			nulls := 0
			for rdr.Next() {
				rec := rdr.RecordBatch()
				if rec.NumRows() != 1 {
					t.Fatalf("batch has %d rows", rec.NumRows())
				}
				values, ok := rec.Column(0).(*array.Int64)
				if !ok {
					t.Fatalf("unexpected Arrow type %s", rec.Column(0).DataType())
				}
				for i := 0; i < values.Len(); i++ {
					if values.IsNull(i) {
						nulls++
					} else {
						got = append(got, values.Value(i))
					}
				}
			}
			if err := rdr.Err(); err != nil {
				t.Fatal(err)
			}
			sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
			if !reflect.DeepEqual(got, tc.want) || nulls != tc.nulls {
				t.Fatalf("got %v + %d NULL, want %v + %d NULL", got, nulls, tc.want, tc.nulls)
			}
		})
	}
}
func TestLiveCollectionStreamLifecycle(t *testing.T) {
	f, pages := liveStreamFixture(t)
	opts := map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"}
	t.Run("uncertain_scan", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages, failScan: 2}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		for r.Next() {
		}
		if r.Err() == nil || hook.scans != 2 {
			t.Fatalf("lost error/replayed discovery: %v scans=%d", r.Err(), hook.scans)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		st, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if !r.Next() {
			t.Fatal(r.Err())
		}
		if err := canceler(t, st).Cancel(f.h.ctx); err != nil {
			t.Fatal(err)
		}
		if r.Next() || r.Err() == nil {
			t.Fatal("canceled stream emitted buffered rows")
		}
	})
	t.Run("release", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if err != nil {
			t.Fatal(err)
		}
		underlying := inspectionReader(r).(*streamReader)
		r.Release()
		if !underlying.closed || underlying.it.liveScan.seen != nil || underlying.it.liveScan.pending != nil {
			t.Fatal("released scan retained resources")
		}
	})
	t.Run("blocking_rejected_before_scan", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream ORDER BY n", opts)
		if r != nil {
			r.Release()
		}
		if !isStatus(err, adbc.StatusNotImplemented) || hook.scans != 0 || hook.fetched != 0 {
			t.Fatalf("err=%v scans=%d fetched=%d", err, hook.scans, hook.fetched)
		}
	})
	t.Run("live_union_rejected_before_scan", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream UNION ALL SELECT n FROM redis_live.it_live_stream", opts)
		if r != nil {
			r.Release()
		}
		if !isStatus(err, adbc.StatusNotImplemented) || hook.scans != 0 || hook.fetched != 0 {
			t.Fatalf("err=%v scans=%d fetched=%d", err, hook.scans, hook.fetched)
		}
	})
	t.Run("permission_revoked_buffered", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if !r.Next() {
			t.Fatal(r.Err())
		}
		hook.denyIndex = true
		if r.Next() || r.Err() == nil {
			t.Fatal("permission refusal accepted buffered result")
		}
	})
	t.Run("uncertain_fetch", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages, failFetch: true}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if r != nil {
			r.Release()
		}
		if err == nil || hook.fetched != 2 || hook.scans != 1 {
			t.Fatalf("err=%v fetched=%d scans=%d", err, hook.fetched, hook.scans)
		}
	})
	t.Run("initially_exhausted_keeps_checks", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages[:1]}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "100"})
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if _, ok := inspectionReader(r).(*streamReader); !ok {
			t.Fatalf("detached live result: %T", inspectionReader(r))
		}
		hook.denyIndex = true
		if r.Next() || r.Err() == nil {
			t.Fatal("initially exhausted result lost definition checks")
		}
	})
	t.Run("definition_changed_buffered", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if !r.Next() {
			t.Fatal(r.Err())
		}
		raw, record, err := (&store{client: f.raw}).readLiveRecord(f.h.ctx, liveRef{schema: liveSchema}, "it_live_stream")
		if err != nil {
			t.Fatal(err)
		}
		defer f.raw.HSet(f.h.ctx, liveRegistry, "it_live_stream", raw)
		record.Meta.LiveVersion = snapshotID()
		updated, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.raw.HSet(f.h.ctx, liveRegistry, "it_live_stream", string(updated)).Err(); err != nil {
			t.Fatal(err)
		}
		if r.Next() || r.Err() == nil {
			t.Fatal("changed definition accepted buffered result")
		}
	})
	t.Run("index_missing_buffered", func(t *testing.T) {
		hook := &liveStreamHook{pages: pages}
		_, r, err := liveTestStream(t, f, hook, "SELECT n FROM redis_live.it_live_stream", opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if !r.Next() {
			t.Fatal(r.Err())
		}
		_, record, err := (&store{client: f.raw}).readLiveRecord(f.h.ctx, liveRef{schema: liveSchema}, "it_live_stream")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.raw.Do(f.h.ctx, "FT.DROPINDEX", record.Meta.index()).Err(); err != nil {
			t.Fatal(err)
		}
		if r.Next() || r.Err() == nil {
			t.Fatal("missing index accepted buffered result")
		}
	})
}
