// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func livePartitionFixture(t *testing.T) (*hashFixture, string) {
	t.Helper()
	f := newHashFixture(t, "it:live:partition550:")
	name := "it_live_partition550"
	for i := range 64 {
		f.hset(f.prefix+strconv.Itoa(i), "n", strconv.Itoa(i), "v", fmt.Sprintf("row-%d", i), "unknown", "keep")
	}
	key := f.prefix + string([]byte{'b', 0, 255})
	if !strings.ContainsRune(key, 0) {
		t.Fatal("binary fixture has no zero byte")
	}
	f.hset(key, "n", "9007199254740993", "v", string([]byte{'v', 0, 'x'}))
	f.hset(f.prefix+"unknown", "unknown", "untouched")
	f.hset(f.prefix+"nested:excluded", "n", "9999", "v", "nested")
	if _, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: name, Apply: true, Types: map[string]string{"n": "BIGINT", "v": "VARCHAR"}, IndexColumns: []string{"n", "v"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(context.Background(), f.opts, name); err != nil && !isStatus(err, adbc.StatusNotFound) {
			t.Error(err)
		}
	})
	if err := f.h.conn.(adbc.GetSetOptionsWithContext).SetOption(f.h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	return f, name
}
func planLiveHashPartitions(t *testing.T, f *hashFixture, q string, n int, ttl string) adbc.Partitions {
	return planLiveHashPartitionsLayout(t, f, q, n, ttl, "")
}
func planLiveHashPartitionsLayout(t *testing.T, f *hashFixture, q string, n int, ttl, layout string) adbc.Partitions {
	t.Helper()
	st, err := f.h.conn.NewStatement(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(f.h.ctx)
	if err := st.SetSqlQuery(f.h.ctx, q); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{OptionStringPartitionSourceMode: "live_hash", OptionIntPartitionCount: strconv.Itoa(n)} {
		if err := st.SetOption(f.h.ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if ttl != "" {
		if err := st.SetOption(f.h.ctx, OptionIntPartitionResultTTL, ttl); err != nil {
			t.Fatal(err)
		}
	}
	if layout != "" {
		if err := st.SetOption(f.h.ctx, OptionStringResultLayouts, layout); err != nil {
			t.Fatal(err)
		}
	}
	_, parts, rows, err := st.ExecutePartitions(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rows != -1 || int(parts.NumPartitions) != n {
		t.Fatal(rows, parts.NumPartitions)
	}
	d, err := decodeLiveHashPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	stStore := &store{client: f.raw, snapshots: true}
	t.Cleanup(func() { _ = stStore.releasePartitionSnapshot(context.Background(), d.Snapshot.Token) })
	return parts
}
func readLiveHashPart(ctx context.Context, c adbc.ConnectionWithContext, id []byte) ([]string, error) {
	r, err := c.ReadPartition(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.Release()
	var rows []string
	for r.Next() {
		b := r.RecordBatch()
		for i := 0; i < int(b.NumRows()); i++ {
			key := string(b.Column(0).(*array.Binary).Value(i))
			n := "NULL"
			if !b.Column(1).IsNull(i) {
				n = strconv.FormatInt(b.Column(1).(*array.Int64).Value(i), 10)
			}
			v := "NULL"
			if !b.Column(2).IsNull(i) {
				v = b.Column(2).(*array.String).Value(i)
			}
			rows = append(rows, key+"|"+n+"|"+v)
		}
	}
	return rows, r.Err()
}
func liveHashWorker(t *testing.T, f *hashFixture) adbc.ConnectionWithContext {
	return liveHashWorkerRows(t, f, "1")
}
func liveHashWorkerRows(t *testing.T, f *hashFixture, batchRows string) adbc.ConnectionWithContext {
	t.Helper()
	opts := map[string]string{}
	for k, v := range f.opts {
		opts[k] = v
	}
	opts[OptionStringLiveTables] = "true"
	opts[OptionIntStreamBatchRows] = batchRows
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(f.h.ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	c, err := db.Open(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}
func TestLiveHashPartitionCoverageAndOwnership(t *testing.T) {
	f, name := livePartitionFixture(t)
	binaryKey := f.prefix + string([]byte{'b', 0, 255})
	if err := f.raw.Expire(f.h.ctx, binaryKey, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	before, err := f.raw.Dump(f.h.ctx, binaryKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	parts := planLiveHashPartitions(t, f, "SELECT CAST(__key AS BYTEA),n,v FROM redis_live."+name+" WHERE n%3=0 OR n IS NULL OR n>9007199254740992", 7, "")
	if err := f.h.conn.Close(f.h.ctx); err != nil {
		t.Fatal("producer close", err)
	}
	type outcome struct {
		rows []string
		err  error
	}
	done := make(chan outcome, 7)
	for _, id := range parts.PartitionIDs {
		c := liveHashWorker(t, f)
		go func() { rows, err := readLiveHashPart(f.h.ctx, c, id); done <- outcome{rows, err} }()
	}
	var actual []string
	for range 7 {
		out := <-done
		if out.err != nil {
			t.Fatal(out.err)
		}
		actual = append(actual, out.rows...)
	}
	var want []string
	for i := range 64 {
		if i%3 == 0 {
			want = append(want, fmt.Sprintf("%s%d|%d|row-%d", f.prefix, i, i, i))
		}
	}
	want = append(want, binaryKey+"|9007199254740993|"+string([]byte{'v', 0, 'x'}), f.prefix+"unknown|NULL|NULL")
	sort.Strings(actual)
	sort.Strings(want)
	if strings.Join(actual, "\n") != strings.Join(want, "\n") {
		t.Fatalf("coverage mismatch got %q want %q", actual, want)
	}
	after, err := f.raw.Dump(f.h.ctx, binaryKey).Result()
	if err != nil || before != after {
		t.Fatal("partition read changed original HASH", err)
	}
	if ttl := f.raw.TTL(f.h.ctx, binaryKey).Val(); ttl < 59*time.Minute {
		t.Fatal("partition read changed TTL", ttl)
	}
	if f.raw.HExists(f.h.ctx, binaryKey, rowIDField).Val() {
		t.Fatal("partition read inserted row ID")
	}
	if f.raw.HGet(f.h.ctx, f.prefix+"1", "unknown").Val() != "keep" {
		t.Fatal("unknown fields changed")
	}
}
func TestLiveHashPartitionLiveValuesAndStaleRegistration(t *testing.T) {
	f, name := livePartitionFixture(t)
	q := "SELECT CAST(__key AS BYTEA),n,v FROM redis_live." + name
	parts := planLiveHashPartitions(t, f, q, 1, "")
	worker := liveHashWorker(t, f)
	f.hset(f.prefix+"0", "v", "changed")
	rows, err := readLiveHashPart(f.h.ctx, worker, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rows, "\n"), f.prefix+"0|0|changed") {
		t.Fatal("worker froze live source values")
	}
	if err := UnregisterLiveHashes(f.h.ctx, f.opts, name); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: name, Apply: true, Types: map[string]string{"n": "BIGINT", "v": "VARCHAR"}, IndexColumns: []string{"n", "v"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0]); err == nil {
		t.Fatal("stale registration descriptor succeeded")
	}
}
func TestLiveHashPartitionExpiryReleaseAndTampering(t *testing.T) {
	f, name := livePartitionFixture(t)
	q := "SELECT CAST(__key AS BYTEA),n,v FROM redis_live." + name
	worker := liveHashWorker(t, f)
	parts := planLiveHashPartitions(t, f, q, 1, "1")
	time.Sleep(1100 * time.Millisecond)
	if _, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0]); err == nil {
		t.Fatal("expired descriptor admitted")
	}
	parts = planLiveHashPartitions(t, f, q, 2, "")
	d, err := decodeLiveHashPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*liveHashPartitionID){func(d *liveHashPartitionID) { d.Bucket = d.Buckets }, func(d *liveHashPartitionID) { d.Schema = "public" }, func(d *liveHashPartitionID) { d.Lo = 1 }, func(d *liveHashPartitionID) { d.Target = "bad" }} {
		bad := d
		mutate(&bad)
		raw, _ := json.Marshal(bad)
		if _, err := worker.ReadPartition(f.h.ctx, raw); err == nil {
			t.Fatal("malformed descriptor admitted")
		}
	}
	if err := (&store{client: f.raw, snapshots: true}).releasePartitionSnapshot(f.h.ctx, d.Snapshot.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0]); err == nil {
		t.Fatal("released descriptor admitted")
	}
}

func TestLiveHashPartitionProcessHelper(t *testing.T) {
	if os.Getenv("REDIS_LIVE_PARTITION_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, retainedTestDatabaseOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringLiveTables: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	c, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	descriptor, err := base64.StdEncoding.DecodeString(os.Getenv("REDIS_LIVE_PARTITION_ID"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readLiveHashPart(ctx, c, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([][]byte, len(rows))
	for i, row := range rows {
		encoded[i] = []byte(row)
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("REDIS_LIVE_PARTITION_OUTPUT"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLiveHashPartitionIndependentProcesses(t *testing.T) {
	f, name := livePartitionFixture(t)
	parts := planLiveHashPartitions(t, f, "SELECT CAST(__key AS BYTEA),n,v FROM redis_live."+name, 3, "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(f.h.ctx, 40*time.Second)
	defer cancel()
	type result struct {
		index int
		err   error
	}
	done := make(chan result, 3)
	for i, id := range parts.PartitionIDs {
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestLiveHashPartitionProcessHelper$", "-test.timeout=35s")
		cmd.Env = append(os.Environ(), "REDIS_LIVE_PARTITION_CHILD=1", "REDIS_LIVE_PARTITION_ID="+base64.StdEncoding.EncodeToString(id), "REDIS_LIVE_PARTITION_OUTPUT="+filepath.Join(directory, strconv.Itoa(i)))
		go func() {
			out, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, out)
			}
			done <- result{i, err}
		}()
	}
	var actual []string
	for range 3 {
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		data, err := os.ReadFile(filepath.Join(directory, strconv.Itoa(r.index)))
		if err != nil {
			t.Fatal(err)
		}
		var rows [][]byte
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			actual = append(actual, string(row))
		}
	}
	var want []string
	for i := range 64 {
		want = append(want, fmt.Sprintf("%s%d|%d|row-%d", f.prefix, i, i, i))
	}
	want = append(want, f.prefix+string([]byte{'b', 0, 255})+"|9007199254740993|"+string([]byte{'v', 0, 'x'}), f.prefix+"unknown|NULL|NULL")
	sort.Strings(actual)
	sort.Strings(want)
	if strings.Join(actual, "\n") != strings.Join(want, "\n") {
		t.Fatal("independent process coverage mismatch", actual, want)
	}
}
func TestLiveHashPartitionBufferedIdentityAndCancellation(t *testing.T) {
	for _, fault := range []string{"registration", "cancel", "connection_close"} {
		t.Run(fault, func(t *testing.T) {
			f, name := livePartitionFixture(t)
			parts := planLiveHashPartitions(t, f, "SELECT CAST(__key AS BYTEA),n,v FROM redis_live."+name, 1, "")
			worker := liveHashWorker(t, f)
			ctx, cancel := context.WithCancel(f.h.ctx)
			defer cancel()
			r, err := worker.ReadPartition(ctx, parts.PartitionIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			if !r.Next() {
				t.Fatal("first batch missing", r.Err())
			}
			switch fault {
			case "registration":
				if err := UnregisterLiveHashes(f.h.ctx, f.opts, name); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "connection_close":
				if err := worker.Close(f.h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("buffered reader ignored source/connection invalidation", fault, r.Err())
			}
		})
	}
}
func TestLiveHashPartitionBudgetsAndModeBoundary(t *testing.T) {
	f, name := livePartitionFixture(t)
	st, err := f.h.conn.NewStatement(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(f.h.ctx)
	options := st.(adbc.GetSetOptionsWithContext)
	if v, err := options.GetOption(f.h.ctx, OptionStringPartitionSourceMode); err != nil || v != "managed" {
		t.Fatal(v, err)
	}
	if err := st.SetSqlQuery(f.h.ctx, "SELECT n FROM redis_live."+name); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.ExecutePartitions(f.h.ctx); err == nil {
		t.Fatal("default managed mode admitted live source")
	}
	if err := st.SetOption(f.h.ctx, OptionStringPartitionSourceMode, "live_hash"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(f.h.ctx, OptionStringPartitionResultMode, "retained"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.ExecutePartitions(f.h.ctx); err == nil {
		t.Fatal("retained live source unexpectedly enabled")
	}
	store := st.(*statementImpl).conn.store
	meta, err := store.liveTable(f.h.ctx, liveRef{schema: liveSchema}, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []string{"bytes", "work", "pages"} {
		scan := &livePartitionScan{store: store, meta: meta, buckets: 1, seen: map[string]bool{}}
		switch budget {
		case "bytes":
			scan.bytes = livePartitionMaxKeyBytes
		case "work":
			scan.work = livePartitionMaxWork
		case "pages":
			scan.pages = livePartitionMaxScanPages
		}
		if _, _, err := scan.next(f.h.ctx); err == nil || !strings.Contains(err.Error(), "53200") {
			t.Fatal("budget did not fail explicitly", budget, err)
		}
		if !scan.closed || scan.seen != nil {
			t.Fatal("budget failure retained source inventory")
		}
	}
}

type livePartitionReadFault struct {
	prefix, mode string
	calls        atomic.Int64
}

func (*livePartitionReadFault) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *livePartitionReadFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && h.mode == "duplicate_scan" {
			if scan, ok := cmd.(*goredis.ScanCmd); ok {
				keys, cursor := scan.Val()
				scan.SetVal(append(append([]string(nil), keys...), keys...), cursor)
			}
		}
		return err
	}
}
func (h *livePartitionReadFault) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		source := false
		for _, cmd := range cmds {
			a := cmd.Args()
			source = source || cmd.Name() == "hgetall" && len(a) > 1 && strings.HasPrefix(fmt.Sprint(a[1]), h.prefix)
		}
		if !source || h.mode == "duplicate_scan" {
			return next(ctx, cmds)
		}
		h.calls.Add(1)
		if h.mode == "unknown_reply" {
			if err := next(ctx, cmds); err != nil {
				return err
			}
			return io.ErrUnexpectedEOF
		}
		var err error = controlScriptNativeError("NOPERM live source read denied")
		if h.mode == "composed_wrong_type" {
			err = errors.Join(controlScriptNativeError("WRONGTYPE operation against a key holding the wrong kind of value"), io.ErrUnexpectedEOF)
		}
		for _, cmd := range cmds {
			cmd.SetErr(err)
		}
		return err
	}
}
func TestLiveHashPartitionReadFailuresAndDuplicates(t *testing.T) {
	for _, fault := range []string{"unknown_reply", "permission_refusal", "composed_wrong_type", "duplicate_scan"} {
		t.Run(fault, func(t *testing.T) {
			f, name := livePartitionFixture(t)
			parts := planLiveHashPartitions(t, f, "SELECT CAST(__key AS BYTEA),n,v FROM redis_live."+name, 1, "")
			worker := liveHashWorker(t, f)
			st, err := worker.NewStatement(f.h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			impl := st.(*statementImpl).conn
			ctx, execution := impl.executionContext(f.h.ctx)
			if execution == nil {
				t.Fatal("missing owned execution transport")
			}
			t.Cleanup(execution.finish)
			_ = st.Close(f.h.ctx)
			hook := &livePartitionReadFault{prefix: f.prefix, mode: fault}
			execution.client.AddHook(hook)
			rows, err := readLiveHashPart(ctx, worker, parts.PartitionIDs[0])
			if fault == "duplicate_scan" {
				if err != nil || len(rows) != 66 {
					t.Fatal("duplicate SCAN rows escaped", len(rows), err)
				}
			} else {
				if err == nil || hook.calls.Load() != 1 {
					t.Fatal("source failure succeeded or replayed", err, hook.calls.Load())
				}
			}
		})
	}
}

func TestLiveHashPartitionIndexDefinitionChanged(t *testing.T) {
	f, name := livePartitionFixture(t)
	parts := planLiveHashPartitions(t, f, "SELECT CAST(__key AS BYTEA),n,v FROM redis_live."+name, 1, "")
	worker := liveHashWorker(t, f)
	st, err := worker.NewStatement(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := st.(*statementImpl).conn.store
	_ = st.Close(f.h.ctx)
	meta, err := store.liveTable(f.h.ctx, liveRef{schema: liveSchema}, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.raw.Do(context.Background(), "FT.DROPINDEX", meta.index()).Err(); err != nil {
			t.Error(err)
			return
		}
		if err := f.raw.Do(context.Background(), liveIndexArgs(meta)...).Err(); err != nil {
			t.Error(err)
			return
		}
		// Recreating the index rescans the whole keyspace (#653).
		deadline := time.Now().Add(keyspaceWait(context.Background(), f.raw, 5*time.Second))
		for {
			if err := store.validateLiveIndex(context.Background(), meta); err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Error("restored fixture index did not become ready")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	r, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if !r.Next() {
		t.Fatal(r.Err())
	}
	if err := f.raw.Do(f.h.ctx, "FT.ALTER", meta.index(), "SCHEMA", "ADD", "unexpected", "TAG").Err(); err != nil {
		t.Fatal(err)
	}
	if r.Next() || r.Err() == nil {
		t.Fatal("changed index allowed buffered batch")
	}
}

func TestLiveHashPartitionEvolutionInvalidatesReaders(t *testing.T) {
	f, name := livePartitionFixture(t)
	query := "SELECT CAST(__key AS BYTEA),n,v FROM redis_live." + name
	parts := planLiveHashPartitions(t, f, query, 1, "")
	worker := liveHashWorker(t, f)
	st, err := worker.NewStatement(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := st.(*statementImpl).conn.store
	_ = st.Close(f.h.ctx)
	meta, err := store.liveTable(f.h.ctx, liveRef{schema: liveSchema}, name)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatal(reader.Err())
	}
	before, err := f.raw.Dump(f.h.ctx, f.prefix+"0").Result()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvolveLiveHashes(f.h.ctx, f.opts, LiveHashEvolutionOptions{HashAdoptOptions: HashAdoptOptions{Prefix: f.prefix, Table: name, Apply: true, IndexColumns: []string{"n"}}, Columns: []string{"n", "v"}, ExpectedIndex: meta.index(), ExclusiveQuiescent: true}); err != nil {
		t.Fatal(err)
	}
	if reader.Next() || reader.Err() == nil {
		t.Fatal("buffered reader survived live schema/index evolution")
	}
	if _, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0]); err == nil {
		t.Fatal("old descriptor survived live evolution")
	}
	next := planLiveHashPartitions(t, f, query, 1, "")
	rows, err := readLiveHashPart(f.h.ctx, worker, next.PartitionIDs[0])
	if err != nil || len(rows) != 66 {
		t.Fatal("fresh evolved partitions", len(rows), err)
	}
	after, err := f.raw.Dump(f.h.ctx, f.prefix+"0").Result()
	if err != nil || before != after {
		t.Fatal("evolution/partition changed source HASH", err)
	}
}

func TestLiveHashPartitionCachedLayoutFences(t *testing.T) {
	for _, fault := range []string{"evolution", "expired_child"} {
		t.Run(fault, func(t *testing.T) {
			f, name := livePartitionFixture(t)
			parts := planLiveHashPartitionsLayout(t, f, "SELECT CAST(NULL AS BYTEA) AS pad,n,v FROM redis_live."+name, 1, "", `{"pad":"fixed_size_binary:10485760"}`)
			worker := liveHashWorkerRows(t, f, "65536")
			r, err := worker.ReadPartition(f.h.ctx, parts.PartitionIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			if !r.Next() {
				t.Fatal("first layout slice missing", r.Err())
			}
			layout, ok := r.(*layoutReader)
			if !ok || layout.pending == nil || layout.offset == 0 || layout.offset >= layout.pending.NumRows() {
				t.Fatal("fixture did not retain multiple slices of one source batch")
			}
			source, ok := layout.source.(*streamReader)
			if !ok || source.snapshot == nil {
				t.Fatal("fixture lost native source pin")
			}
			if fault == "expired_child" {
				field := "p:" + source.snapshot.token
				raw, err := f.raw.HGet(f.h.ctx, snapshotCatalogKey, field).Bytes()
				if err != nil {
					t.Fatal(err)
				}
				var record map[string]any
				if err := json.Unmarshal(raw, &record); err != nil {
					t.Fatal(err)
				}
				record["expires"] = 0
				raw, _ = json.Marshal(record)
				if err := f.raw.HSet(f.h.ctx, snapshotCatalogKey, field, raw).Err(); err != nil {
					t.Fatal(err)
				}
			} else {
				meta, err := source.exec.store.liveTable(f.h.ctx, liveRef{schema: liveSchema}, name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := EvolveLiveHashes(f.h.ctx, f.opts, LiveHashEvolutionOptions{HashAdoptOptions: HashAdoptOptions{Prefix: f.prefix, Table: name, Apply: true, IndexColumns: []string{"n"}}, Columns: []string{"n", "v"}, ExpectedIndex: meta.index(), ExclusiveQuiescent: true}); err != nil {
					t.Fatal(err)
				}
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("cached layout slice escaped source identity/lease fence", fault, r.Err())
			}
		})
	}
}
