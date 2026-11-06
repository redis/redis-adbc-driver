// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Use the production capture path: a fabricated HASH would miss changes to
// metadata ownership, physical placement, or the native payload encoding.
func checkSnapshotImportPayloadACL(t *testing.T, readerURI string) {
	t.Helper()
	client := func(uri string) *goredis.Client {
		opts, err := goredis.ParseURL(uri)
		if err != nil {
			t.Fatal(err)
		}
		if err := configureTLSRoots(opts, os.Getenv("SSL_CERT_FILE")); err != nil {
			t.Fatal(err)
		}
		opts.DisableIdentity = true
		c := goredis.NewClient(opts)
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	ctx := context.Background()
	raw := client(os.Getenv("REDIS_URI"))
	s := snapshotTestStore(t, raw)
	sctx, pin, err := s.acquireSnapshot(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(sctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	meta := &tableMeta{Schema: "public", Name: "rbac_private_capture", Columns: []columnMeta{{Name: "secret", Type: typeString}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	spool := newSnapshotImportSpool(wctx, s, meta)
	const source = "app:private:capture"
	if err := spool.append(wctx, []map[string]Value{{keyField: stringValue(source), "secret": stringValue("captured-private-value")}}); err != nil {
		t.Fatal(err)
	}
	physical, err := resolveSnapshotMetadata(wctx, strings.TrimPrefix(spool.rows, metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := raw.HGet(ctx, physical, source).Result()
	if err != nil {
		t.Fatal(err)
	}
	row, err := decodeSnapshotImportRow(meta, payload)
	if err != nil || len(row) != 1 || row[0].S != "captured-private-value" {
		t.Fatal("production capture was not readable by its owner", err)
	}
	reader := client(readerURI)
	deadline := time.Now().Add(20 * time.Second)
	for {
		err := reader.Ping(ctx).Err()
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "WRONGPASS") || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, command := range [][]any{{"HGET", physical, source}, {"HMGET", physical, source}, {"HGETALL", physical}} {
		if err := reader.Do(ctx, command...).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Errorf("restricted reader could access captured payload using %s: %v", command[0], err)
		}
	}
}
