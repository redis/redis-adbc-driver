// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
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
		{"SADD", snapshotReaderDomains, "forbidden-domain"},
		{"HSET", meta.prefix() + "1", "secret", "forbidden"},
	} {
		if err := reader.Do(ctx, command...).Err(); err == nil || !(strings.Contains(err.Error(), "NOPERM") || strings.HasPrefix(err.Error(), "ERR ACL failure in script:")) {
			t.Errorf("restricted reader crossed a private command boundary using %s: %v", command[0], err)
		}
	}
}

func checkSnapshotRestrictedPeers(t *testing.T, first, second *sqlHarness) {
	t.Helper()
	f, s := *first, *second
	f.t, s.t = t, t
	first, second = &f, &s
	storeFor := func(h *sqlHarness) *store {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		return st.(*statementImpl).conn.store
	}
	a, b := storeFor(first), storeFor(second)
	if !a.restrictedReader || !b.restrictedReader || a.readerNamespace == b.readerNamespace {
		t.Fatal("fixture does not have two distinct restricted principals")
	}
	second.expectRows("SELECT id FROM it_rbac_renamed", "84")
	second.expectError("SELECT id FROM it_rbac_denied", "NOPERM")
	for _, pair := range [][2]*store{{a, b}, {b, a}} {
		key := snapshotReaderPrefix + pair[1].readerNamespace
		modelDenied(t, pair[0].client.HSet(first.ctx, key, "forbidden", "{}").Err())
		modelDenied(t, once(first.ctx, pair[0].client, "EVAL", `return redis.call('HSET',KEYS[1],'forbidden','{}')`, 1, key).Err())
	}
	ctx, pin, err := a.acquireSnapshot(first.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	group, err := a.retainPartitionSnapshot(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer a.releasePartitionSnapshot(first.ctx, group.Token)
	workerCtx, worker, err := b.acquirePartitionSnapshot(second.ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.release()
	if worker.leaseKey() != snapshotReaderPrefix+b.readerNamespace {
		t.Fatal("worker did not own its independent lease")
	}
	if err := a.releasePartitionSnapshot(first.ctx, group.Token); err != nil {
		t.Fatal(err)
	}
	pin.release()
	if err := worker.valid(workerCtx); err != nil {
		t.Fatal("producer release invalidated a peer worker", err)
	}
	if _, late, err := b.acquirePartitionSnapshot(second.ctx, group); err == nil {
		late.release()
		t.Fatal("released producer admitted a late peer worker")
	}
}

func checkSnapshotSharedRole(t *testing.T, first, second *sqlHarness) {
	t.Helper()
	f, s := *first, *second
	f.t, s.t = t, t
	first, second = &f, &s
	storeFor := func(h *sqlHarness) *store {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		return st.(*statementImpl).conn.store
	}
	a, b := storeFor(first), storeFor(second)
	if !a.restrictedReader || !b.restrictedReader || a.readerNamespace != b.readerNamespace {
		t.Fatal("shared role did not resolve to one backend authorization domain")
	}
	first.expectRows("SELECT id FROM it_rbac_renamed", "84")
	second.expectRows("SELECT id FROM it_rbac_renamed", "84")
	second.expectError("SELECT id FROM it_rbac_denied", "NOPERM")
	modelDenied(t, b.client.HSet(second.ctx, snapshotCatalogKey, "root", snapshotID()).Err())
	// Raw rights are shared inside the role, just as its row-read ACL is. This
	// intentionally makes no per-user isolation promise within that domain.
	key := snapshotReaderPrefix + b.readerNamespace
	if err := b.client.HSet(second.ctx, key, "domain_canary", "{}").Err(); err != nil {
		t.Fatal(err)
	}
	defer b.client.HDel(second.ctx, key, "domain_canary")
	if a.client.HGet(first.ctx, key, "domain_canary").Val() != "{}" {
		t.Fatal("fixture did not use the shared backend role rights")
	}
}

func checkSnapshotRestrictedPoison(t *testing.T, admin, reader *sqlHarness) {
	t.Helper()
	ac, rc := *admin, *reader
	ac.t, rc.t = t, t
	admin, reader = &ac, &rc
	storeFor := func(h *sqlHarness) *store {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		return st.(*statementImpl).conn.store
	}
	a, r := storeFor(admin), storeFor(reader)
	admin.dropTables("it_rbac_poison_probe")
	admin.exec("CREATE TABLE it_rbac_poison_probe(v INTEGER)")
	admin.exec("INSERT INTO it_rbac_poison_probe VALUES(55)")
	ctx, pin, err := r.acquireSnapshot(reader.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := a.getTable(ctx, "public", "it_rbac_poison_probe")
	if err != nil {
		t.Fatal(err)
	}
	admin.exec("DROP TABLE it_rbac_poison_probe")
	own := snapshotReaderPrefix + r.readerNamespace
	for _, mode := range []string{"malformed_namespace", "namespace_count", "namespace_bytes", "hash_count", "hash_bytes"} {
		t.Run(mode, func(t *testing.T) {
			var cleanup func()
			switch mode {
			case "malformed_namespace", "namespace_count", "namespace_bytes":
				var members []any
				if mode == "malformed_namespace" {
					members = []any{"invalid-principal"}
				}
				if mode == "namespace_bytes" {
					members = []any{strings.Repeat("x", 300000)}
				}
				if mode == "namespace_count" {
					for i := 0; i < 129; i++ {
						members = append(members, snapshotReaderNamespace(fmt.Sprintf("poison_%d", i)))
					}
				}
				if err := r.client.SAdd(reader.ctx, snapshotReaderDirectory, members...).Err(); err != nil {
					t.Fatal(err)
				}
				cleanup = func() {
					if err := a.client.SRem(admin.ctx, snapshotReaderDirectory, members...).Err(); err != nil {
						t.Error(err)
					}
				}
			case "hash_count", "hash_bytes":
				fields := []string{"poison"}
				args := []any{"poison", strings.Repeat("x", 300000)}
				if mode == "hash_count" {
					fields = nil
					args = nil
					for i := 0; i < 129; i++ {
						f := fmt.Sprintf("poison_%d", i)
						fields = append(fields, f)
						args = append(args, f, "{}")
					}
				}
				if err := r.client.HSet(reader.ctx, own, args...).Err(); err != nil {
					t.Fatal(err)
				}
				cleanup = func() {
					if err := a.client.HDel(admin.ctx, own, fields...).Err(); err != nil {
						t.Error(err)
					}
				}
			}
			defer cleanup()
			if err := a.snapshotControl(admin.ctx, "claim", "", "", "").Err(); err == nil {
				t.Fatal("collector accepted poisoned reader state")
			}
			a.reapSnapshot(admin.ctx)
			if a.client.Exists(admin.ctx, meta.prefix()+"1").Val() != 1 {
				t.Fatal("poisoned reader state freed a valid root")
			}
			if err := pin.valid(ctx); err != nil {
				t.Fatal("poisoning invalidated an unrelated valid lease", err)
			}
		})
	}
	snapshotReapTest(a, admin.ctx)
	if a.client.Exists(admin.ctx, meta.prefix()+"1").Val() != 1 {
		t.Fatal("repair lost a valid reader root")
	}
	pin.release()
	snapshotReapTest(a, admin.ctx)
	if a.client.Exists(admin.ctx, meta.prefix()+"1").Val() != 0 {
		t.Fatal("repaired reader state stranded reclamation")
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
	hashBytes, err := a.client.Do(admin.ctx, "MEMORY", "USAGE", key, "SAMPLES", 0).Int64()
	if err != nil || hashBytes > 262144 {
		t.Fatal("128 legitimate leases exceeded their byte budget", hashBytes, err)
	}
	directoryBytes, err := a.client.Do(admin.ctx, "MEMORY", "USAGE", snapshotReaderDirectory, "SAMPLES", 0).Int64()
	if err != nil || directoryBytes > 262144 {
		t.Fatal("legitimate directory exceeded its byte budget", directoryBytes, err)
	}
	t.Logf("reader resource sample: pins=128 hash_bytes=%d directory_bytes=%d control_script_bytes=%d", hashBytes, directoryBytes, len(snapshotReaderControl))
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
	if err := a.releasePartitionSnapshot(admin.ctx, group.Token); err != nil {
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

func checkSnapshotGrantedGenerations(t *testing.T, admin, reader *sqlHarness) {
	t.Helper()
	ac, rc := *admin, *reader
	ac.t, rc.t = t, t
	admin, reader = &ac, &rc
	waitRows := func(query, want string) {
		rows, _ := reader.query(query)
		if strings.Join(rows, "\n") != want {
			t.Fatalf("%s: got %q want %q", query, rows, want)
		}
	}
	prefix := func() string {
		rows, _ := admin.query("SELECT key_prefix FROM information_schema.tables WHERE table_schema='public' AND table_name='it_rbac_allowed'")
		if len(rows) != 1 {
			t.Fatal("missing granted table prefix")
		}
		return rows[0]
	}
	initial := prefix()
	admin.exec("UPDATE it_rbac_allowed SET id=43")
	waitRows("SELECT id FROM it_rbac_allowed", "43")
	admin.exec("INSERT INTO it_rbac_allowed VALUES(44)")
	waitRows("SELECT id FROM it_rbac_allowed ORDER BY id", "43\n44")
	admin.exec("DELETE FROM it_rbac_allowed WHERE id=44")
	waitRows("SELECT id FROM it_rbac_allowed", "43")
	admin.exec("ALTER TABLE it_rbac_allowed ADD COLUMN extra INTEGER DEFAULT 7")
	reader.expectRows("SELECT id,extra FROM it_rbac_allowed", "43|7")
	admin.exec("ALTER TABLE it_rbac_allowed ALTER COLUMN id TYPE BIGINT")
	waitRows("SELECT id FROM it_rbac_allowed", "43")
	admin.exec("ALTER TABLE it_rbac_allowed DROP COLUMN extra")
	admin.exec("UPDATE it_rbac_allowed SET id=42")
	st, err := admin.conn.NewStatement(admin.ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := st.(*statementImpl).conn.store
	_ = st.Close(admin.ctx)
	snapshotDrainCompactionTest(t, s)
	waitRows("SELECT id FROM it_rbac_allowed", "42")
	final := prefix()
	if final == initial || snapshotAccessID(initial) == "" || snapshotAccessID(initial) != snapshotAccessID(final) {
		t.Fatal("physical replacement changed logical table access identity")
	}
	reader.expectError("SELECT id FROM it_rbac_denied", "NOPERM")
}
