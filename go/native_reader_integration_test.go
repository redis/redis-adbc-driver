// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"context"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func nativeTestReader(t *testing.T) *NativeReader {
	t.Helper()
	r, err := OpenNativeReader(context.Background(), testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestNativeReaderCloseCancelsRead(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (v TEXT)")
	h.exec("INSERT INTO t VALUES ('old')")
	r := nativeTestReader(t)
	if err := r.AcquireSnapshot(h.ctx, 0); err != nil {
		t.Fatal(err)
	}
	token := r.conn.store.sharedView.pin.token
	started := make(chan struct{})
	var signal sync.Once
	r.conn.clientCfg.single.Dialer = func(ctx context.Context, _, _ string) (net.Conn, error) {
		signal.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	// Disable dial retries in this fault fixture so one dial signals admission.
	r.conn.clientCfg.single.DialerRetries = 0
	done := make(chan error, 1)
	go func() { _, err := r.HGetAll(h.ctx, "public", "t", 1); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not begin")
	}
	closed := make(chan error, 1)
	go func() { closed <- r.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not interrupt read")
	}
	if err := <-done; err == nil {
		t.Fatal("interrupted read succeeded")
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
		t.Fatal("Close leaked shared pin")
	}
}

func TestNativeReaderPermissionRefusal(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (v TEXT)")
	h.exec("INSERT INTO t VALUES ('secret')")
	r := nativeTestReader(t)
	// Inject a typed backend refusal on row reads, including existence-only
	// reads. No adapter path may turn a refusal into an absent field or row.
	r.conn.clientCfg.single.OnConnect = func(ctx context.Context, c *goredis.Conn) error {
		return nativePermissionError("NOPERM native reader test refusal")
	}
	if _, err := r.Exists(h.ctx, "public", "t", 1); err == nil {
		t.Fatal("permission refusal returned success")
	}
}

type nativePermissionError string

func (e nativePermissionError) Error() string { return string(e) }
func (nativePermissionError) RedisError()     {}

func TestNativeReaderHashVisibility(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE customers (id BIGINT, name TEXT, country TEXT)")
	h.exec("INSERT INTO customers VALUES (101,'Alice','USA'),(NULL,NULL,NULL),(102,'',NULL)")
	r := nativeTestReader(t)
	ctx := h.ctx
	row, err := r.HGetAll(ctx, "public", "customers", 1)
	want := map[string][]byte{"id": []byte("101"), "name": []byte("Alice"), "country": []byte("USA")}
	if err != nil || !reflect.DeepEqual(row, want) {
		t.Fatalf("read: %#v, %v", row, err)
	}
	if found, err := r.Exists(ctx, "public", "customers", 101); err != nil || found {
		t.Fatalf("application ID treated as row ID: %v, %v", found, err)
	}
	if found, err := r.Exists(ctx, "public", "customers", 2); err != nil || !found {
		t.Fatalf("all NULL row: %v, %v", found, err)
	}
	if n, err := r.HLen(ctx, "public", "customers", 2); err != nil || n != 0 {
		t.Fatalf("NULL fields: %d, %v", n, err)
	}
	if yes, err := r.HExists(ctx, "public", "customers", 3, "name"); err != nil || !yes {
		t.Fatalf("empty string missing: %v, %v", yes, err)
	}
	values, err := r.HMGet(ctx, "public", "customers", 1, "name", "missing", "name", "__rowid")
	if err != nil || !reflect.DeepEqual(values, [][]byte{[]byte("Alice"), nil, []byte("Alice"), nil}) {
		t.Fatalf("projection: %#v, %v", values, err)
	}
	if err := r.AcquireSnapshot(ctx, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE customers SET name='Bob' WHERE id=101")
	h.exec("DELETE FROM customers WHERE id=102")
	if v, err := r.HGet(ctx, "public", "customers", 1, "name"); err != nil || string(v) != "Alice" {
		t.Fatalf("pinned: %q, %v", v, err)
	}
	if err := r.ReleaseSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if v, err := r.HGet(ctx, "public", "customers", 1, "name"); err != nil || string(v) != "Bob" {
		t.Fatalf("updated: %q, %v", v, err)
	}
	if found, err := r.Exists(ctx, "public", "customers", 3); err != nil || found {
		t.Fatalf("tombstone: %v, %v", found, err)
	}
	h.exec("ALTER TABLE customers RENAME COLUMN name TO label")
	h.exec("ALTER TABLE customers ADD COLUMN extra TEXT DEFAULT 'added'")
	if v, err := r.HGet(ctx, "public", "customers", 1, "label"); err != nil || string(v) != "Bob" {
		t.Fatalf("rename: %q, %v", v, err)
	}
	if v, err := r.HGet(ctx, "public", "customers", 1, "extra"); err != nil || string(v) != "added" {
		t.Fatalf("default: %q, %v", v, err)
	}
	h.exec("UPDATE customers SET extra=NULL WHERE id=101")
	if v, err := r.HGet(ctx, "public", "customers", 1, "extra"); err != nil || v != nil {
		t.Fatalf("explicit NULL: %q, %v", v, err)
	}
	if err := r.AcquireSnapshot(ctx, 0); err != nil {
		t.Fatal(err)
	}
	h.exec("TRUNCATE customers")
	h.exec("DROP TABLE customers")
	if yes, err := r.Exists(ctx, "public", "customers", 1); err != nil || !yes {
		t.Fatalf("drop invalidated pin: %v, %v", yes, err)
	}
	if err := r.ReleaseSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HGetAll(ctx, "public", "customers", 1); err == nil {
		t.Fatal("missing table accepted")
	}
}

func TestNativeReaderFailureAndLifetime(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (v BIGINT)")
	h.exec("INSERT INTO t VALUES (9007199254740993)")
	r := nativeTestReader(t)
	if _, err := r.HGetAll(h.ctx, "public", "t", 0); err == nil {
		t.Fatal("invalid row ID accepted")
	}
	if _, err := r.HMGet(h.ctx, "public", "t", 1); err == nil {
		t.Fatal("empty HMGET accepted")
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, err := r.HGetAll(ctx, "public", "t", 1); err == nil {
		t.Fatal("cancellation ignored")
	}
	if err := r.AcquireSnapshot(h.ctx, time.Second); err == nil {
		t.Fatal("invalid retention accepted")
	}
	if err := r.AcquireSnapshot(h.ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := r.AcquireSnapshot(h.ctx, 0); err == nil {
		t.Fatal("nested acquisition accepted")
	}
	_, _, err := r.ReadHash(h.ctx, "public", "t", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.conn.store.sharedView.cancel()
	if _, err := r.HGetAll(h.ctx, "public", "t", 1); err == nil {
		t.Fatal("expired view silently replaced")
	}
	if err := r.ReleaseSnapshot(h.ctx); err != nil {
		t.Fatal(err)
	}
	if v, err := r.HGet(h.ctx, "public", "t", 1, "v"); err != nil || string(v) != "9007199254740993" {
		t.Fatalf("fresh exact value: %q, %v", v, err)
	}
	// Removing an authoritative image is corruption, never a missing logical row.
	sctx, pin, err := r.conn.store.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := r.conn.store.getTable(sctx, "public", "t")
	pin.release()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Del(h.ctx, meta.prefix()+"1").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HGetAll(h.ctx, "public", "t", 1); err == nil {
		t.Fatal("missing image returned success")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HGetAll(h.ctx, "public", "t", 1); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed client: %v", err)
	}
}
