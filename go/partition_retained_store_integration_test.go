// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func retainedTestClient(t *testing.T, raw goredis.UniversalClient) goredis.UniversalClient {
	t.Helper()
	// Legacy inspection helpers use RESP2. The new retained path is tested
	// explicitly with the project's default RESP3 on both endpoint types.
	if cc, ok := raw.(*goredis.ClusterClient); ok {
		opts := *cc.Options()
		opts.Protocol = 3
		opts.PushNotificationProcessor = nil
		raw = goredis.NewClusterClient(&opts)
	} else {
		opts := *raw.(*goredis.Client).Options()
		opts.Protocol = 3
		opts.PushNotificationProcessor = nil
		raw = goredis.NewClient(&opts)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func retainedFixture(t *testing.T, cleanupClient goredis.UniversalClient, limits retainedLimits) *retainedStore {
	t.Helper()
	raw := retainedTestClient(t, cleanupClient)
	id, err := retainedRandomID()
	if err != nil {
		t.Fatal(err)
	}
	s, err := newRetainedStore(raw, "it_"+id, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		clear := func(ctx context.Context, client *goredis.Client) error {
			iter := client.Scan(ctx, 0, s.prefix+"*", 100).Iterator()
			for iter.Next(ctx) {
				if err := client.Del(ctx, iter.Val()).Err(); err != nil {
					return err
				}
			}
			return iter.Err()
		}
		if cc, ok := cleanupClient.(*goredis.ClusterClient); ok {
			if err := cc.ForEachMaster(ctx, clear); err != nil {
				t.Error(err)
			}
		} else if err := clear(ctx, cleanupClient.(*goredis.Client)); err != nil {
			t.Error(err)
		}
	})
	return s
}

func retainedIPCFixture(t *testing.T) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "payload", Type: arrow.BinaryTypes.Binary, Nullable: true}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.Int64Builder).AppendValues([]int64{1<<53 + 1, 1<<63 - 1, -1}, nil)
	b.Field(1).(*array.BinaryBuilder).AppendValues([][]byte{{0, 255}, nil, []byte("exact\x00")}, []bool{true, false, true})
	r := b.NewRecordBatch()
	defer r.Release()
	var output bytes.Buffer
	w := ipc.NewWriter(&output, ipc.WithSchema(schema))
	if err := w.Write(r); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func publishRetainedFixture(t *testing.T, s *retainedStore, payload []byte, ttl time.Duration) retainedReference {
	t.Helper()
	u, err := s.begin(context.Background(), s.limits.JobBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.put(context.Background(), payload, 3); err != nil {
		t.Fatal(err)
	}
	ref, err := u.commit(context.Background(), retainedSHA([]byte("fixture-schema")), ttl)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestRetainedPartitionPublicationAndReplay(t *testing.T) {
	h := newSQLHarness(t)
	admin := h.rawClient()
	s := retainedFixture(t, admin, defaultRetainedLimits())
	payload := retainedIPCFixture(t)
	u, err := s.begin(h.ctx, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.put(h.ctx, payload, 3); err != nil {
		t.Fatal(err)
	}
	if n, err := s.client.Exists(h.ctx, s.jobKey(u.id, "manifest")).Result(); err != nil || n != 0 {
		t.Fatal("uncommitted manifest became visible")
	}
	for _, key := range []string{s.jobKey(u.id, "meta"), s.jobKey(u.id, "chunk:0")} {
		if ttl, err := s.client.PTTL(h.ctx, key).Result(); err != nil || ttl <= 0 || ttl > s.lease {
			t.Fatalf("provisional key lacks bounded TTL: %v %v", ttl, err)
		}
	}
	ref, err := u.commit(h.ctx, retainedSHA([]byte("fixture-schema")), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// The committed job's lifetime is independent of its uploader object.
	u = nil
	if err := s.client.Close(); err != nil {
		t.Fatal(err)
	}
	consumer := *s
	consumer.client = retainedTestClient(t, admin)
	s = &consumer
	for range 2 {
		manifest, err := s.manifest(h.ctx, ref)
		if err != nil || manifest.Rows != 3 || manifest.Bytes != int64(len(payload)) {
			t.Fatalf("manifest: %+v %v", manifest, err)
		}
		data, err := s.chunk(h.ctx, ref, manifest, 0)
		if err != nil || !bytes.Equal(data, payload) {
			t.Fatalf("replay changed IPC bytes: %v", err)
		}
		reader, err := ipc.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if !reader.Next() {
			t.Fatal("no Arrow record")
		}
		ids := reader.RecordBatch().Column(0).(*array.Int64).Int64Values()
		if !slices.Equal(ids, []int64{1<<53 + 1, 1<<63 - 1, -1}) {
			t.Fatal("retained IPC rounded large IDs")
		}
		if reader.Next() || reader.Err() != nil {
			t.Fatal("unexpected Arrow tail", reader.Err())
		}
		reader.Release()
	}
	for _, key := range []string{s.jobKey(ref.JobID, "meta"), s.jobKey(ref.JobID, "manifest"), s.jobKey(ref.JobID, "chunk:0")} {
		deadline, err := s.client.Do(h.ctx, "PEXPIRETIME", key).Int64()
		if err != nil || deadline != ref.Expires {
			t.Fatal("published keys have inconsistent expiry", err)
		}
	}
	appKey := "it_retained_app:" + s.namespace
	if err := s.client.Set(h.ctx, appKey, "exact\x00\xff", 0).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.client.Del(h.ctx, appKey).Err() })
	if err := s.revoke(h.ctx, ref.JobID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.manifest(h.ctx, ref); !isStatus(err, adbc.StatusInvalidState) || !strings.Contains(err.Error(), "revoked") {
		t.Fatal("revoked result was readable", err)
	}
	if value, err := s.client.Get(h.ctx, appKey).Result(); err != nil || value != "exact\x00\xff" {
		t.Fatal("revocation changed application data")
	}
	if n, err := s.client.Exists(h.ctx, s.jobKey(ref.JobID, "manifest"), s.jobKey(ref.JobID, "chunk:0")).Result(); err != nil || n != 0 {
		t.Fatal("revocation left output bytes", err)
	}
}

func TestRetainedPartitionQuotaRaces(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), retainedLimits{JobBytes: 64, TotalBytes: 128, ChunkBytes: 64, Chunks: 2, Jobs: 16})
	var wg sync.WaitGroup
	uploads := make(chan *retainedUpload, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if u, err := s.begin(h.ctx, 64); err == nil {
				uploads <- u
			} else if !strings.Contains(err.Error(), "RETAINED_BYTE_QUOTA") {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(uploads)
	var accepted []*retainedUpload
	for u := range uploads {
		accepted = append(accepted, u)
	}
	if len(accepted) != 2 {
		t.Fatalf("quota admitted %d reservations, want 2", len(accepted))
	}
	if err := accepted[0].put(h.ctx, []byte("a"), 1); err != nil {
		t.Fatal(err)
	}
	ref, err := accepted[0].commit(h.ctx, retainedSHA([]byte("schema")), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Publication must release unused reservation bytes, not block another job
	// until the committed result expires: 1 committed + 64 reserved + 63 = 128.
	released, err := s.begin(h.ctx, 63)
	if err != nil {
		t.Fatal("commit did not return unused quota", err)
	}
	if err := released.abort(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.revoke(h.ctx, ref.JobID, ""); err != nil {
		t.Fatal(err)
	}
	for _, u := range accepted[1:] {
		if err := u.abort(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.begin(h.ctx, 64)
	if err != nil {
		t.Fatal("abort failed to release payload quota", err)
	}
	if err := u.put(h.ctx, []byte("a"), 1); err != nil {
		t.Fatal(err)
	}
	if err := u.put(h.ctx, []byte("b"), 1); err != nil {
		t.Fatal(err)
	}
	if err := u.put(h.ctx, []byte("c"), 1); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("chunk count limit not enforced", err)
	}
	changed := *s
	changed.limits.TotalBytes++
	if _, err := changed.begin(h.ctx, 1); err == nil || !strings.Contains(err.Error(), "RETAINED_CONFIG_MISMATCH") {
		t.Fatal("planner silently changed tenant quotas", err)
	}
}

func TestRetainedPartitionRevokeRacingUpload(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), retainedLimits{JobBytes: 256, TotalBytes: 512, ChunkBytes: 32, Chunks: 8, Jobs: 16})
	u, err := s.begin(h.ctx, 256)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 8 {
			if err := u.put(h.ctx, []byte("chunk"), 1); err != nil {
				if !strings.Contains(err.Error(), "RETAINED_UPLOAD_INACTIVE") {
					t.Error(err)
				}
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		if err := s.revoke(h.ctx, u.id, u.nonce); err != nil {
			t.Error(err)
		}
	}()
	close(start)
	wg.Wait()
	keys := []string{s.jobKey(u.id, "manifest")}
	for i := range s.limits.Chunks {
		keys = append(keys, s.jobKey(u.id, fmt.Sprint("chunk:", i)))
	}
	if n, err := s.client.Exists(h.ctx, keys...).Result(); err != nil || n != 0 {
		t.Fatal("upload/revoke race left unaccounted bytes", n, err)
	}
	if _, err := u.commit(h.ctx, retainedSHA([]byte("schema")), time.Hour); err == nil {
		t.Fatal("revoked upload published a result")
	}
}

func TestRetainedPartitionRevokeUsesCompleteJobKeySpace(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), retainedLimits{JobBytes: 32, TotalBytes: 64, ChunkBytes: 32, Chunks: 2, Jobs: 2})
	u, err := s.begin(h.ctx, 32)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"first", "second"} {
		if err := u.put(h.ctx, []byte(part), 1); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := u.commit(h.ctx, retainedSHA([]byte("schema")), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	revoker := *s
	revoker.limits.Chunks = 1 // A different process may have a smaller local limit.
	if err := revoker.revoke(h.ctx, ref.JobID, ""); err != nil {
		t.Fatal(err)
	}
	if n, err := s.client.Exists(h.ctx, s.jobKey(ref.JobID, "chunk:0"), s.jobKey(ref.JobID, "chunk:1")).Result(); err != nil || n != 0 {
		t.Fatal("revocation left unaccounted chunks with different local limits", n, err)
	}
}

func TestRetainedPartitionJobQuotaAndCancelledPublication(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), retainedLimits{JobBytes: 32, TotalBytes: 64, ChunkBytes: 32, Chunks: 2, Jobs: 2})
	for range 2 {
		u, err := s.begin(h.ctx, 32)
		if err != nil {
			t.Fatal(err)
		}
		if err := u.put(h.ctx, []byte("provisional"), 1); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(h.ctx)
		cancel()
		if _, err := u.commit(ctx, retainedSHA([]byte("schema")), time.Hour); err == nil {
			t.Fatal("cancelled coordinator committed a result")
		}
		if err := u.abort(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.begin(h.ctx, 1); err == nil || !strings.Contains(err.Error(), "RETAINED_JOB_QUOTA") {
		t.Fatal("revocation tombstones were allowed to grow without a bound", err)
	}
}

func TestRetainedPartitionCorruptLeaseCannotExtendOrphanLifetime(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	u, err := s.begin(h.ctx, 4096)
	if err != nil {
		t.Fatal(err)
	}
	key := s.jobKey(u.id, "meta")
	deadline, err := s.client.Do(h.ctx, "PEXPIRETIME", key).Int64()
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.client.Get(h.ctx, key).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	meta["deadline"] = fmt.Sprint(time.Now().Add(365 * 24 * time.Hour).UnixMilli())
	corrupt, _ := json.Marshal(meta)
	if err := s.client.SetArgs(h.ctx, key, corrupt, goredis.SetArgs{KeepTTL: true}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := u.put(h.ctx, []byte("must-not-survive"), 1); err == nil {
		t.Fatal("corrupt metadata extended a provisional chunk beyond its original lease")
	}
	if err := u.abort(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := s.client.Do(h.ctx, "PEXPIRETIME", key).Int64(); err != nil || got != deadline {
		t.Fatal("abort trusted the corrupt deadline instead of the quota lease", got, err)
	}
}

func TestRetainedPartitionMissingCorruptAndUnpublished(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	payload := retainedIPCFixture(t)
	ref := publishRetainedFixture(t, s, payload, time.Hour)
	m, err := s.manifest(h.ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	key := s.jobKey(ref.JobID, "chunk:0")
	if err := s.client.Set(h.ctx, key, "corrupt", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.chunk(h.ctx, ref, m, 0); !isStatus(err, adbc.StatusInvalidData) {
		t.Fatal("corrupt chunk replayed", err)
	}
	if err := s.client.Del(h.ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.chunk(h.ctx, ref, m, 0); !isStatus(err, adbc.StatusInvalidData) {
		t.Fatal("missing chunk replayed", err)
	}
	if err := s.client.Set(h.ctx, s.jobKey(ref.JobID, "manifest"), "{}", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.manifest(h.ctx, ref); !isStatus(err, adbc.StatusInvalidData) {
		t.Fatal("corrupt manifest accepted", err)
	}
	u, err := s.begin(h.ctx, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.put(h.ctx, payload, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.client.Del(h.ctx, s.jobKey(u.id, "chunk:0")).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := u.commit(h.ctx, retainedSHA([]byte("schema")), time.Hour); err == nil || !strings.Contains(err.Error(), "RETAINED_CHUNK_MISSING") {
		t.Fatal("incomplete upload committed", err)
	}
	if n, err := s.client.Exists(h.ctx, s.jobKey(u.id, "manifest")).Result(); err != nil || n != 0 {
		t.Fatal("failed publication exposed a manifest")
	}
	if err := u.abort(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedPartitionManifestEvictionStopsReplay(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	ref := publishRetainedFixture(t, s, retainedIPCFixture(t), time.Hour)
	m, err := s.manifest(h.ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.client.Del(h.ctx, s.jobKey(ref.JobID, "manifest")).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.chunk(h.ctx, ref, m, 0); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("reader replayed a chunk after its manifest was evicted", err)
	}
}

func TestRetainedPartitionControlStateDoesNotPolluteApplicationIndexes(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	index := "it_retained_global_" + s.namespace
	app := "it_retained_application:" + s.namespace
	search := &store{client: s.client}
	if err := s.client.HSet(h.ctx, app, "state", "source", "name", "application").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = search.searchDo(h.ctx, index, "FT.DROPINDEX", index).Err()
		_ = s.client.Del(h.ctx, app).Err()
	})
	if err := search.searchDo(h.ctx, index, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, "",
		"SCHEMA", "state", "TAG", "name", "TEXT", "bytes", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := search.countMatches(h.ctx, index, "@state:{uploading}"); err != nil || n != 0 {
		t.Fatal("application index fixture was not empty of job state", n, err)
	}
	if _, err := s.begin(h.ctx, 4096); err != nil {
		t.Fatal(err)
	}
	if n, err := search.countMatches(h.ctx, index, "@state:{uploading}"); err != nil || n != 0 {
		t.Fatal("retained metadata changed application HASH index membership", n, err)
	}
}

func TestRetainedPartitionExpiryAndAbandonedUpload(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), retainedLimits{JobBytes: 32, TotalBytes: 32, ChunkBytes: 32, Chunks: 2, Jobs: 2})
	s.lease = 500 * time.Millisecond
	u, err := s.begin(h.ctx, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.put(h.ctx, []byte("abandoned"), 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		n, err := s.client.Exists(h.ctx, s.jobKey(u.id, "meta"), s.jobKey(u.id, "chunk:0")).Result()
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("abandoned provisional output did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.lease = 10 * time.Minute
	v, err := s.begin(h.ctx, 32)
	if err != nil {
		t.Fatal("expired quota was not reclaimed", err)
	}
	if err := v.put(h.ctx, []byte("committed"), 1); err != nil {
		t.Fatal(err)
	}
	ref, err := v.commit(h.ctx, retainedSHA([]byte("schema")), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for {
		now, err := s.client.Time(h.ctx).Result()
		if err != nil {
			t.Fatal(err)
		}
		if now.UnixMilli() >= ref.Expires {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("committed result did not reach fixed expiry")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := s.manifest(h.ctx, ref); !isStatus(err, adbc.StatusNotFound) || !strings.Contains(err.Error(), "expired") {
		t.Fatal("expired result not distinguished", err)
	}
	if _, err := s.begin(h.ctx, 32); err != nil {
		t.Fatal("expired committed quota was not reclaimed", err)
	}
}

func TestRetainedPartitionIndependentACL(t *testing.T) {
	a := newACLHarness(t)
	s := retainedFixture(t, a.raw, defaultRetainedLimits())
	other := retainedFixture(t, a.raw, defaultRetainedLimits())
	payload := retainedIPCFixture(t)
	frame, err := preflightRetainedIPC(payload)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := s.begin(a.ctx, s.limits.JobBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := upload.put(a.ctx, payload, 3); err != nil {
		t.Fatal(err)
	}
	ref, err := upload.commit(a.ctx, frame.schema, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	foreign := publishRetainedFixture(t, other, payload, time.Hour)
	user := "it_retained_" + s.namespace[3:15]
	rules := []string{"+ping", "+time", "+getrange", "%R~" + s.prefix + "*"}
	if a.cluster {
		rules = append(rules, "+cluster|slots", "+command", "+asking")
	}
	a.user(user, rules...)
	var client goredis.UniversalClient
	if cc, ok := s.client.(*goredis.ClusterClient); ok {
		opts := *cc.Options()
		opts.Username, opts.Password = user, aclPassword
		opts.PushNotificationProcessor = nil
		client = goredis.NewClusterClient(&opts)
	} else {
		opts := *s.client.(*goredis.Client).Options()
		opts.Username, opts.Password = user, aclPassword
		opts.PushNotificationProcessor = nil
		client = goredis.NewClient(&opts)
	}
	defer client.Close()
	worker, err := retainedReaderStore(a.ctx, client, s.namespace)
	if err != nil {
		t.Fatal(err)
	}
	m, err := worker.manifest(a.ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := worker.chunk(a.ctx, ref, m, 0)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("independently authenticated read failed", err)
	}
	reader, err := newRetainedReader(a.ctx, worker, ref, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for reader.Next() {
		total += reader.RecordBatch().NumRows()
	}
	reader.Release()
	if total != 3 || reader.Err() != nil {
		t.Fatal("independent read-only Arrow replay failed", reader.Err())
	}
	if err := worker.revoke(a.ctx, ref.JobID, ""); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("readonly worker revoked a job", err)
	}
	wrong, _ := newRetainedStore(client, other.namespace, other.limits)
	if _, err := wrong.manifest(a.ctx, foreign); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("descriptor acted as a cross-tenant bearer token", err)
	}
	if _, err := worker.manifest(a.ctx, foreign); !isStatus(err, adbc.StatusInvalidArgument) {
		t.Fatal("configured namespace check missing", err)
	}
	if _, err := worker.chunk(a.ctx, ref, m, 0); err != nil {
		t.Fatal("denied mutation invalidated another reader", err)
	}
	// No endpoint, secret, nonce or arbitrary Redis key is in the reference.
	encoded, _ := json.Marshal(ref)
	for _, secret := range []string{aclPassword, "redis://", "rediss://", s.jobKey(ref.JobID, "meta")} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal(fmt.Sprintf("retained reference contains forbidden connection/key data: %q", secret))
		}
	}
}

func TestRetainedPartitionRevokeRepairsMissingOrCorruptMetadata(t *testing.T) {
	for _, failure := range []string{"missing", "corrupt"} {
		t.Run(failure, func(t *testing.T) {
			h := newSQLHarness(t)
			s := retainedFixture(t, h.rawClient(), retainedLimits{JobBytes: 32, TotalBytes: 64, ChunkBytes: 32, Chunks: 1, Jobs: 2})
			upload, err := s.begin(h.ctx, 32)
			if err != nil {
				t.Fatal(err)
			}
			if err := upload.put(h.ctx, []byte("payload"), 1); err != nil {
				t.Fatal(err)
			}
			ref, err := upload.commit(h.ctx, retainedSHA([]byte("schema")), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			meta := s.jobKey(ref.JobID, "meta")
			if failure == "missing" {
				err = s.client.Del(h.ctx, meta).Err()
			} else {
				err = s.client.Set(h.ctx, meta, "corrupt", time.Minute).Err()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.revoke(h.ctx, ref.JobID, ""); err != nil {
				t.Fatal("explicit publisher revoke could not repair job metadata", err)
			}
			if n, err := s.client.Exists(h.ctx, s.jobKey(ref.JobID, "manifest"), s.jobKey(ref.JobID, "chunk:0")).Result(); err != nil || n != 0 {
				t.Fatal("explicit revoke left output after metadata loss", n, err)
			}
			if bytes, err := s.client.ZScore(h.ctx, s.prefix+"quota-bytes", ref.JobID).Result(); err != nil || bytes != 0 {
				t.Fatal("revocation did not return quota", bytes, err)
			}
			if _, err := s.manifest(h.ctx, ref); !isStatus(err, adbc.StatusInvalidState) {
				t.Fatal("repaired job did not retain revoked state", err)
			}
		})
	}
}
