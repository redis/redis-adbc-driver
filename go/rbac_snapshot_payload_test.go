// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
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
	for _, command := range [][]any{
		{"EVAL", "return redis.call('HGET',KEYS[1],ARGV[1])", 1, physical, source},
		{"HSET", snapshotCatalogKey, "root", snapshotID()},
		{"EVAL", "return redis.call('HSET',KEYS[1],'root',ARGV[1])", 1, snapshotCatalogKey, snapshotID()},
		{"HSET", snapshotReaderPrefix + snapshotReaderNamespace("other-principal@example.com"), snapshotID(), "{}"},
		{"SREM", snapshotReaderDirectory, "other-principal"},
		{"HSET", meta.prefix() + "1", "secret", "forbidden"},
	} {
		if err := reader.Do(ctx, command...).Err(); err == nil || !(strings.Contains(err.Error(), "NOPERM") || strings.HasPrefix(err.Error(), "ERR ACL failure in script:")) {
			t.Errorf("restricted reader crossed a private command boundary using %s: %v", command[0], err)
		}
	}
}

func checkSnapshotRestrictedQuota(t *testing.T, admin, reader *sqlHarness) {
	t.Helper()
	storeFor := func(h *sqlHarness) *store {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		return st.(*statementImpl).conn.store
	}
	a, r := storeFor(admin), storeFor(reader)
	a.stopSnapshotCompactor()
	var tokens []string
	defer func() {
		for _, token := range tokens {
			_ = r.snapshotControl(reader.ctx, "release", token, "", "").Err()
		}
	}()
	root := ""
	for i := 0; i < 128; i++ {
		token := snapshotID()
		var err error
		root, err = r.snapshotControl(reader.ctx, "acquire", token, "", "").Text()
		if err != nil {
			t.Fatalf("reader %d: %v", i, err)
		}
		tokens = append(tokens, token)
	}
	if err := r.snapshotControl(reader.ctx, "acquire", snapshotID(), "", "").Err(); err == nil || !strings.Contains(err.Error(), "reader limit (128)") {
		t.Fatal("reader quota was not enforced", err)
	}
	key := snapshotReaderPrefix + r.readerNamespace
	expired, _ := json.Marshal(map[string]any{"root": root, "expires": 0})
	if err := a.client.HSet(admin.ctx, key, tokens[0], expired).Err(); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"valid", "renew"} {
		if n, err := r.snapshotControl(reader.ctx, op, tokens[0], "", root).Int(); err != nil || n != 0 {
			t.Fatal("expired lease revived", op, n, err)
		}
	}
	if err := r.snapshotControl(reader.ctx, "acquire", tokens[0], "", "").Err(); err == nil || !strings.Contains(err.Error(), "existing reader lease is no longer valid") {
		t.Fatal("expired token silently repinned", err)
	}
	if n, err := r.snapshotControl(reader.ctx, "renew", tokens[1], "", snapshotID()).Int(); err != nil || n != 0 {
		t.Fatal("wrong-root renewal succeeded", n, err)
	}
	newToken := snapshotID()
	if _, err := r.snapshotControl(reader.ctx, "acquire", newToken, "", "").Text(); err != nil {
		t.Fatal("expired lease did not free quota", err)
	}
	tokens = append(tokens, newToken)
	for _, token := range tokens {
		if err := r.snapshotControl(reader.ctx, "release", token, "", "").Err(); err != nil {
			t.Fatal(err)
		}
	}
	snapshotReapTest(a, admin.ctx)
	if a.client.SCard(admin.ctx, snapshotReaderDirectory).Val() != 0 {
		t.Fatal("inactive namespaces were not reclaimed")
	}
}

func checkSnapshotRestrictedRetention(t *testing.T, admin, reader *sqlHarness) {
	t.Helper()
	ac, rc := *admin, *reader
	ac.t, rc.t = t, t
	admin, reader = &ac, &rc
	admin.dropTables("it_rbac_lease_probe")
	getStore := func(h *sqlHarness) *store {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		return st.(*statementImpl).conn.store
	}
	a, r := getStore(admin), getStore(reader)
	if !r.restrictedReader {
		t.Fatal("reader unexpectedly has catalog write capability")
	}
	admin.exec("CREATE TABLE it_rbac_lease_probe(v INTEGER)")
	admin.exec("INSERT INTO it_rbac_lease_probe VALUES(77)")
	ctx, pin, err := r.acquireSnapshot(reader.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := a.getTable(ctx, "public", "it_rbac_lease_probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := pin.valid(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.client.HGetAll(ctx, meta.prefix()+"1").Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("lease expanded raw row access", err)
	}
	group, err := r.retainPartitionSnapshot(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if group.Namespace != r.readerNamespace {
		t.Fatal("partition lost principal namespace")
	}
	workerCtx, worker, err := a.acquirePartitionSnapshot(admin.ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.release()
	if err := r.releasePartitionSnapshot(reader.ctx, group.Token); err != nil {
		t.Fatal(err)
	}
	if _, late, err := a.acquirePartitionSnapshot(admin.ctx, group); err == nil {
		late.release()
		t.Fatal("released producer accepted a late worker")
	}
	admin.exec("DROP TABLE it_rbac_lease_probe")
	snapshotReapTest(a, admin.ctx)
	if a.client.Exists(admin.ctx, meta.prefix()+"1").Val() != 1 {
		t.Fatal("collector removed a restricted reader's live root")
	}
	if err := pin.valid(ctx); err != nil {
		t.Fatal(err)
	}
	pin.release()
	snapshotReapTest(a, admin.ctx)
	if err := worker.valid(workerCtx); err != nil {
		t.Fatal(err)
	}
	if a.client.Exists(admin.ctx, meta.prefix()+"1").Val() != 1 {
		t.Fatal("producer release revoked an active cross-principal worker")
	}
	worker.release()
	snapshotReapTest(a, admin.ctx)
	if a.client.Exists(admin.ctx, meta.prefix()+"1").Val() != 0 {
		t.Fatal("released restricted root was not reclaimed")
	}
}
