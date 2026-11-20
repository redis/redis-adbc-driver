// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

type unmanagedPartitionTestRow struct {
	Key     []byte
	N, V, S string
}

func unmanagedPartitionFixture(t *testing.T) (*sqlHarness, *goredis.Client, adbc.DatabaseWithContext, map[string]unmanagedPartitionTestRow) {
	t.Helper()
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	expected := map[string]unmanagedPartitionTestRow{}
	for n := 0; n < 83; n++ {
		key := fmt.Sprintf("app:%d", n)
		if n%3 == 1 {
			key = fmt.Sprintf("app:sub:%d", n)
		}
		fields := []any{"n", n, "v", int64(9007199254740993) + int64(n), "s", "value-" + strconv.Itoa(n), "active", 1, "unknown", "preserve"}
		if n%5 == 0 {
			fields[7] = 0
		}
		if err := raw.HSet(h.ctx, key, fields...).Err(); err != nil {
			t.Fatal(err)
		}
		if n >= 2 && n%2 == 0 && n%5 != 0 {
			expected[key] = unmanagedPartitionTestRow{[]byte(key), strconv.Itoa(n), strconv.FormatInt(9007199254740993+int64(n), 10), "value-" + strconv.Itoa(n)}
		}
	}
	binaryKey := "app:" + string([]byte{'a', 255, 'b'})
	nulKey := "app:" + string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(nulKey, 0) {
		t.Fatal("no zero byte in fixture")
	}
	if err := raw.HSet(h.ctx, binaryKey, "n", 1000, "v", "9007199254741993", "s", "binary-key", "active", 1, "unknown", "preserve").Err(); err != nil {
		t.Fatal(err)
	}
	expected[binaryKey] = unmanagedPartitionTestRow{[]byte(binaryKey), "1000", "9007199254741993", "binary-key"}
	if err := raw.HSet(h.ctx, nulKey, "n", 1002, "v", "9007199254741995", "s", "nul-key", "active", 1, "unknown", "preserve").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, "app:null", "n", 1004, "s", "null-value", "active", 1).Err(); err != nil {
		t.Fatal(err)
	}
	expected["app:null"] = unmanagedPartitionTestRow{[]byte("app:null"), "1004", "NULL", "null-value"}
	if err := raw.PExpire(h.ctx, nulKey, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "source_idx", "ON", "HASH", "PREFIX", 2, "app:", "app:sub:", "FILTER", "@active==1", "SCHEMA", "n", "NUMERIC", "v", "NUMERIC", "s", "TAG", "active", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "source_idx"); err != nil {
		t.Fatal(err)
	}
	// Some Software Search builds omit NUL-bearing keys from this filtered
	// index. Follow native membership exactly; never manufacture an indexed row
	// from its matching HASH fields. The non-UTF8 key above must remain present.
	reply, err := raw.Do(h.ctx, "FT.SEARCH", "source_idx", "*", "INKEYS", 1, nulKey, "LIMIT", 0, 0, "DIALECT", 2).Result()
	if err != nil || snapshotReplyWarning(reply) {
		t.Fatal("native NUL membership oracle", err)
	}
	members, err := parseSearchCount(reply)
	if err != nil || members < 0 || members > 1 {
		t.Fatal("native NUL membership oracle", members, err)
	}
	if members == 1 {
		expected[nulKey] = unmanagedPartitionTestRow{[]byte(nulKey), "1002", "9007199254741995", "nul-key"}
	} else {
		t.Log("native filtered index omits the NUL-bearing key; partition must omit it too")
	}
	setter := h.conn.(adbc.GetSetOptionsWithContext)
	if err := setter.SetOption(h.ctx, OptionStringForeignIndexesMode, "unmanaged"); err != nil {
		t.Fatal(err)
	}
	if err := setter.SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	return h, raw, db, expected
}
func unmanagedPartitionStatement(t *testing.T, h *sqlHarness, count int) (adbc.StatementWithContext, *arrow.Schema, adbc.Partitions) {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close(h.ctx) })
	for key, value := range map[string]string{OptionStringPartitionSourceMode: "unmanaged", OptionIntPartitionCount: strconv.Itoa(count)} {
		if err := st.SetOption(h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT CAST(__key AS BYTEA),n,v,s FROM redis_foreign.source_idx WHERE n>=2 AND n%2=0"); err != nil {
		t.Fatal(err)
	}
	schema, parts, _, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return st, schema, parts
}
func unmanagedPartitionRows(conn adbc.ConnectionWithContext, ctx context.Context, data []byte) ([]unmanagedPartitionTestRow, error) {
	r, err := conn.ReadPartition(ctx, data)
	if err != nil {
		return nil, err
	}
	defer r.Release()
	var rows []unmanagedPartitionTestRow
	for r.Next() {
		record := r.RecordBatch()
		keys := record.Column(0).(*array.Binary)
		for i := 0; i < int(record.NumRows()); i++ {
			rows = append(rows, unmanagedPartitionTestRow{append([]byte(nil), keys.Value(i)...), unmanagedPartitionValue(record.Column(1), i), unmanagedPartitionValue(record.Column(2), i), unmanagedPartitionValue(record.Column(3), i)})
		}
	}
	return rows, r.Err()
}
func unmanagedPartitionValue(a arrow.Array, i int) string {
	if a.IsNull(i) {
		return "NULL"
	}
	return a.ValueStr(i)
}

func assertUnmanagedPartitionRows(t *testing.T, rows []unmanagedPartitionTestRow, expected map[string]unmanagedPartitionTestRow) {
	t.Helper()
	seen := map[string]bool{}
	for _, row := range rows {
		want, ok := expected[string(row.Key)]
		if !ok || seen[string(row.Key)] || row.N != want.N || row.V != want.V || row.S != want.S {
			t.Fatalf("unexpected, duplicated or incorrect row: %#v", row)
		}
		seen[string(row.Key)] = true
	}
	if len(seen) != len(expected) {
		for key := range expected {
			if !seen[key] {
				t.Logf("missing key bytes: %x", []byte(key))
			}
		}
		t.Fatalf("got %d exact rows; want %d", len(seen), len(expected))
	}
}

func TestUnmanagedPartitionsExactParallelCoverage(t *testing.T) {
	h, raw, db, expected := unmanagedPartitionFixture(t)
	nulKey := "app:" + string([]byte{'a', 0, 'b'})
	expiry, err := raw.Do(h.ctx, "PEXPIRETIME", nulKey).Int64()
	if err != nil {
		t.Fatal(err)
	}
	sourceDump := map[string]string{}
	for key := range expected {
		sourceDump[key] = raw.Dump(h.ctx, key).Val()
	}
	sourceDump["app:"+string([]byte{'a', 0, 'b'})] = raw.Dump(h.ctx, "app:"+string([]byte{'a', 0, 'b'})).Val()
	for _, count := range []int{1, 4, 11} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			st, _, parts := unmanagedPartitionStatement(t, h, count)
			if parts.NumPartitions != uint64(count) {
				t.Fatal(parts.NumPartitions)
			}
			all := make(chan []unmanagedPartitionTestRow, count)
			errs := make(chan error, count)
			var wg sync.WaitGroup
			for _, part := range parts.PartitionIDs {
				conn, err := db.Open(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				wg.Go(func() {
					defer conn.Close(h.ctx)
					rows, err := unmanagedPartitionRows(conn, h.ctx, part)
					all <- rows
					errs <- err
				})
			}
			wg.Wait()
			close(all)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var rows []unmanagedPartitionTestRow
			for part := range all {
				rows = append(rows, part...)
			}
			assertUnmanagedPartitionRows(t, rows, expected)
			id, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringPartitionSnapshotID)
			if err != nil {
				t.Fatal(err)
			}
			h.exec("DROP PARTITION SNAPSHOT '" + id + "'")
			if _, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
				t.Fatal("released descriptor was admitted")
			}
		})
	}
	for key, dump := range sourceDump {
		if got := raw.Dump(h.ctx, key).Val(); got != dump {
			t.Fatal("partition reads mutated application HASH")
		}
	}
	afterExpiry, err := raw.Do(h.ctx, "PEXPIRETIME", nulKey).Int64()
	if err != nil || afterExpiry != expiry {
		t.Fatal("partition reads changed application TTL", afterExpiry, expiry, err)
	}
	if raw.Do(h.ctx, "FT.INFO", "source_idx").Err() != nil {
		t.Fatal("source index was removed")
	}
}

func TestUnmanagedPartitionsLiveSourceAndPinnedRegistration(t *testing.T) {
	h, raw, db, expected := unmanagedPartitionFixture(t)
	_, _, parts := unmanagedPartitionStatement(t, h, 4)
	// Rows are read when workers run, after planning. Keep exact native values.
	key := "app:2"
	if err := raw.HSet(h.ctx, key, "v", "9223372036854775806").Err(); err != nil {
		t.Fatal(err)
	}
	row := expected[key]
	row.V = "9223372036854775806"
	expected[key] = row
	if err := raw.Del(h.ctx, "app:8").Err(); err != nil {
		t.Fatal(err)
	}
	delete(expected, "app:8")
	if err := raw.PExpire(h.ctx, "app:6", time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	delete(expected, "app:6")
	// Dropping registration changes future planning, not this bounded group's
	// pinned catalog metadata. It never transfers ownership of source rows.
	h.exec("DROP TABLE redis_foreign.source_idx")
	conn, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(h.ctx)
	var rows []unmanagedPartitionTestRow
	for _, part := range parts.PartitionIDs {
		got, err := unmanagedPartitionRows(conn, h.ctx, part)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, got...)
	}
	assertUnmanagedPartitionRows(t, rows, expected)
}

func TestUnmanagedPartitionsIndexIdentityAndCancellation(t *testing.T) {
	h, raw, _, _ := unmanagedPartitionFixture(t)
	_, _, parts := unmanagedPartitionStatement(t, h, 1)
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, err := h.conn.ReadPartition(ctx, parts.PartitionIDs[0]); err == nil {
		t.Fatal("canceled reader admitted")
	}
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "source_idx").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "source_idx", "ON", "HASH", "PREFIX", 2, "app:", "app:sub:", "FILTER", "@active==1", "SCHEMA", "n", "TAG", "v", "NUMERIC", "s", "TAG", "active", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "source_idx"); err != nil {
		t.Fatal(err)
	}
	if r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		r.Release()
		t.Fatal("changed application index admitted")
	}
}

func TestUnmanagedPartitionProcessHelper(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_PARTITION_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	flex := os.Getenv("REDIS_SNAPSHOT_FLEX")
	if flex == "" {
		flex = "false"
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: flex}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	data, err := os.ReadFile(os.Getenv("REDIS_UNMANAGED_PARTITION_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := unmanagedPartitionRows(conn, ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("REDIS_UNMANAGED_PARTITION_OUTPUT"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestUnmanagedPartitionsIndependentProcesses(t *testing.T) {
	h, _, _, expected := unmanagedPartitionFixture(t)
	_, _, parts := unmanagedPartitionStatement(t, h, 4)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
	defer cancel()
	done := make(chan error, len(parts.PartitionIDs))
	for i, part := range parts.PartitionIDs {
		input := filepath.Join(dir, fmt.Sprintf("input%d", i))
		output := filepath.Join(dir, fmt.Sprintf("output%d", i))
		if err := os.WriteFile(input, part, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestUnmanagedPartitionProcessHelper$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), "REDIS_UNMANAGED_PARTITION_CHILD=1", "REDIS_UNMANAGED_PARTITION_INPUT="+input, "REDIS_UNMANAGED_PARTITION_OUTPUT="+output)
		go func() {
			out, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, out)
			}
			done <- err
		}()
	}
	for range parts.PartitionIDs {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var rows []unmanagedPartitionTestRow
	for i := range parts.PartitionIDs {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("output%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		var part []unmanagedPartitionTestRow
		if err := json.Unmarshal(data, &part); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, part...)
	}
	assertUnmanagedPartitionRows(t, rows, maps.Clone(expected))
}

func TestUnmanagedPartitionsNativeReadFailures(t *testing.T) {
	for _, mode := range []string{"scan_permission", "permission", "transport", "warning", "duplicate", "malformed", "hash_transport", "hash_wrongtype_transport"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _, _ := unmanagedPartitionFixture(t)
			st, _, parts := unmanagedPartitionStatement(t, h, 1)
			hook := &flexUnmanagedReadFault{mode: mode}
			ctx, transport := st.(*statementImpl).conn.executionContext(h.ctx)
			defer transport.finish()
			transport.client.AddHook(hook)
			rows, err := unmanagedPartitionRows(h.conn, ctx, parts.PartitionIDs[0])
			if err == nil || len(rows) != 0 {
				t.Fatal("failed native read escaped as complete output", len(rows), err)
			}
			expectedCalls := 1
			if strings.HasPrefix(mode, "hash_") {
				expectedCalls = 2
			} // one membership, then one failed HASH pipeline
			if hook.calls != expectedCalls {
				t.Fatal("failure was absent or automatically replayed", hook.calls)
			}
		})
	}
}

func TestUnmanagedPartitionsExpiryAndActiveReader(t *testing.T) {
	h, raw, db, expected := unmanagedPartitionFixture(t)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	for key, value := range map[string]string{OptionStringPartitionSourceMode: "unmanaged", OptionIntPartitionCount: "1", OptionIntPartitionResultTTL: "1"} {
		if err := st.SetOption(h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT CAST(__key AS BYTEA),n,v,s FROM redis_foreign.source_idx WHERE n>=2 AND n%2=0"); err != nil {
		t.Fatal(err)
	}
	_, parts, _, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(h.ctx)
	reader, err := worker.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	// Expire authoritative server admission without sleeping or altering source
	// HASHes. An already-admitted child retains its independent catalog lease.
	d, err := decodeUnmanagedPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	doc, err := raw.HGet(h.ctx, snapshotCatalogKey, "p:"+d.Snapshot.Token).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var pin map[string]any
	if err := json.Unmarshal(doc, &pin); err != nil {
		t.Fatal(err)
	}
	pin["expires"] = 0
	doc, _ = json.Marshal(pin)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+d.Snapshot.Token, doc).Err(); err != nil {
		t.Fatal(err)
	}
	d.Snapshot.Expires = time.Now().Add(24 * time.Hour).UnixMilli()
	tampered, _ := json.Marshal(d)
	if _, err := h.conn.ReadPartition(h.ctx, tampered); err == nil {
		t.Fatal("descriptor bytes extended server expiry")
	}
	var rows []unmanagedPartitionTestRow
	for reader.Next() {
		rec := reader.RecordBatch()
		keys := rec.Column(0).(*array.Binary)
		for i := 0; i < int(rec.NumRows()); i++ {
			rows = append(rows, unmanagedPartitionTestRow{append([]byte(nil), keys.Value(i)...), unmanagedPartitionValue(rec.Column(1), i), unmanagedPartitionValue(rec.Column(2), i), unmanagedPartitionValue(rec.Column(3), i)})
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
	assertUnmanagedPartitionRows(t, rows, expected)
}

func TestUnmanagedPartitionsOptInAndBoundParameters(t *testing.T) {
	h, _, _, expected := unmanagedPartitionFixture(t)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, "SELECT CAST(__key AS BYTEA),n,v,s FROM redis_foreign.source_idx WHERE n>=2 AND n%2=0"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.ExecutePartitions(h.ctx); err == nil {
		t.Fatal("default managed partitions accepted external data")
	}
	if err := st.SetOption(h.ctx, OptionStringPartitionSourceMode, "unmanaged"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, OptionStringPartitionResultMode, "retained"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.ExecutePartitions(h.ctx); err == nil {
		t.Fatal("source mode silently changed retained execution")
	}
	if err := st.SetOption(h.ctx, OptionStringPartitionResultMode, "live"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT CAST(__key AS BYTEA),n,v,s FROM redis_foreign.source_idx WHERE n>=? AND n%2=0"); err != nil {
		t.Fatal(err)
	}
	b := array.NewInt64Builder(memory.DefaultAllocator)
	b.Append(2)
	a := b.NewArray()
	b.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "min", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{a}, 1)
	a.Release()
	if err := st.Bind(h.ctx, rec); err != nil {
		rec.Release()
		t.Fatal(err)
	}
	rec.Release()
	_, parts, _, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows []unmanagedPartitionTestRow
	for _, part := range parts.PartitionIDs {
		got, err := unmanagedPartitionRows(h.conn, h.ctx, part)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, got...)
	}
	assertUnmanagedPartitionRows(t, rows, expected)
}
