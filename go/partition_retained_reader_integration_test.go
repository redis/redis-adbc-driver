// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

func retainedReaderFixture(t *testing.T) (*retainedStore, retainedReference) {
	t.Helper()
	h := newSQLHarness(t)
	store := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	payload := retainedIPCFixture(t)
	frame, err := preflightRetainedIPC(payload)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := store.begin(h.ctx, 8192)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := upload.put(h.ctx, payload, 3); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := upload.commit(h.ctx, frame.schema, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return store, ref
}

func TestRetainedPartitionReaderBatchingAndLocalRelease(t *testing.T) {
	store, ref := retainedReaderFixture(t)
	ctx := context.Background()
	owner := &streams{}
	r, err := newRetainedReader(ctx, store, ref, 0, 2, 2, owner)
	if err != nil {
		t.Fatal(err)
	}
	var values []int64
	var batches []int64
	for r.Next() {
		rec := r.RecordBatch()
		values = append(values, rec.Column(0).(*array.Int64).Int64Values()...)
		batches = append(batches, rec.NumRows())
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	r.Release()
	if len(values) != 6 || values[0] != 1<<53+1 || values[4] != 1<<63-1 || len(batches) != 4 || batches[0] != 2 || batches[1] != 1 {
		t.Fatal(values, batches)
	}
	if owner.closeAll(errConnClosed) != 0 {
		t.Fatal("finished reader retained owner")
	}
	r, err = newRetainedReader(ctx, store, ref, 0, 2, 1, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next() {
		t.Fatal(r.Err())
	}
	// The caller may retain a batch after releasing its reader.
	batch := r.RecordBatch()
	batch.Retain()
	r.Release()
	if batch.Column(0).(*array.Int64).Value(0) != 1<<53+1 {
		t.Fatal("released live Arrow memory")
	}
	batch.Release()
	replay, err := newRetainedReader(ctx, store, ref, 0, 2, 3)
	if err != nil {
		t.Fatal("release revoked shared job", err)
	}
	defer replay.Release()
	var count int64
	for replay.Next() {
		count += replay.RecordBatch().NumRows()
	}
	if count != 6 || replay.Err() != nil {
		t.Fatal(count, replay.Err())
	}
}

func TestRetainedPartitionReaderCancellationAndOwnerClose(t *testing.T) {
	store, ref := retainedReaderFixture(t)
	owner := &streams{}
	ctx, cancel := context.WithCancel(context.Background())
	r, err := newRetainedReader(ctx, store, ref, 0, 2, 1, owner)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if r.Next() || !isStatus(r.Err(), adbc.StatusCancelled) {
		t.Fatal(r.Err())
	}
	r.Release()
	r, err = newRetainedReader(context.Background(), store, ref, 0, 2, 1, owner)
	if err != nil {
		t.Fatal(err)
	}
	if owner.closeAll(errConnClosed) != 1 {
		t.Fatal("reader not registered")
	}
	if r.Next() || !isStatus(r.Err(), adbc.StatusInvalidState) {
		t.Fatal(r.Err())
	}
	r.Release()
	if _, err = store.manifest(context.Background(), ref); err != nil {
		t.Fatal("owner close revoked shared job", err)
	}
}

func TestRetainedPartitionReaderMidReadFailure(t *testing.T) {
	for _, failure := range []string{"revoke", "evict-manifest", "corrupt-chunk", "wrong-rows", "wrong-schema"} {
		t.Run(failure, func(t *testing.T) {
			store, ref := retainedReaderFixture(t)
			ctx := context.Background()
			r, err := newRetainedReader(ctx, store, ref, 0, 2, 3)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			if !r.Next() || r.RecordBatch().NumRows() != 3 {
				t.Fatal(r.Err())
			}
			switch failure {
			case "revoke":
				err = store.revoke(ctx, ref.JobID, "")
			case "evict-manifest":
				err = store.client.Del(ctx, store.jobKey(ref.JobID, "manifest")).Err()
			case "corrupt-chunk":
				err = store.client.Set(ctx, store.jobKey(ref.JobID, "chunk:1"), "broken", time.Minute).Err()
			default:
				// Integrity-valid but semantically invalid producer output must fail the
				// Arrow schema/row checks, not be returned or evaluated from source SQL.
				payload := retainedIPCFixture(t)
				if failure == "wrong-rows" {
					// Change row count without changing the schema. The manifest is kept
					// unchanged; update its chunk digest to model an invalid producer.
					offset := 8 + retainedU32(payload, 4)
					meta := payload[offset+8 : offset+8+retainedU32(payload, offset+4)]
					record := retainedTable(meta, retainedU32(meta, 0)).table(2)
					at := record.field(0, 8)
					meta[at] = 2
				} else {
					payload = bytes.Replace(payload, []byte("payload"), []byte("changed"), 1)
				}
				err = store.client.Set(ctx, store.jobKey(ref.JobID, "chunk:1"), payload, time.Minute).Err()
				// Model a producer that publishes this invalid chunk coherently, including
				// the manifest digest; the reader still must validate its Arrow contract.
				m := r.manifest
				m.Chunks[1].SHA256 = retainedSHA(payload)
				data, jsonErr := json.Marshal(m)
				if jsonErr != nil {
					t.Fatal(jsonErr)
				}
				err = store.client.Set(ctx, store.jobKey(ref.JobID, "manifest"), data, time.Minute).Err()
				r.ref.Manifest = retainedSHA(data)
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("continued after invalid shared result")
			}
		})
	}
}

func TestRetainedPartitionReaderCloseRace(t *testing.T) {
	store, ref := retainedReaderFixture(t)
	for range 16 {
		owner := &streams{}
		r, err := newRetainedReader(context.Background(), store, ref, 0, 2, 1, owner)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for r.Next() {
			}
		}()
		go func() { defer wg.Done(); owner.closeAll(errConnClosed) }()
		wg.Wait()
		r.Release()
	}
	if _, err := store.manifest(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedPartitionReaderExpiryBetweenChunks(t *testing.T) {
	h := newSQLHarness(t)
	s := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	payload := retainedIPCFixture(t)
	frame, err := preflightRetainedIPC(payload)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := s.begin(h.ctx, 8192)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := upload.put(h.ctx, payload, 3); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := upload.commit(h.ctx, frame.schema, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRetainedReader(h.ctx, s, ref, 0, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if !r.Next() {
		t.Fatal(r.Err())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		now, err := s.serverTime(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if now.UnixMilli() >= ref.Expires {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture expiry deadline exceeded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Next() || !isStatus(r.Err(), adbc.StatusNotFound) {
		t.Fatal("expired job continued", r.Err())
	}
}

func TestRetainedPartitionReaderProcessHelper(t *testing.T) {
	raw := os.Getenv("ADBC_RETAINED_TEST_DESCRIPTOR")
	if raw == "" {
		t.Skip("internal subprocess helper")
	}
	d, err := decodeRetainedPartition([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	options, _, _, err := parseURI(os.Getenv("REDIS_URI"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configureTLSRoots(options, os.Getenv("REDIS_RETAINED_TLS_CA_FILE")); err != nil {
		t.Fatal(err)
	}
	options.Protocol = 3
	options.DisableIdentity = true
	var client goredis.UniversalClient
	if os.Getenv("ADBC_RETAINED_TEST_CLUSTER") == "true" {
		client = goredis.NewClusterClient(clusterOptions(options))
	} else {
		client = goredis.NewClient(options)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := retainedReaderStore(ctx, client, d.Reference.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := newRetainedReader(ctx, store, d.Reference, d.Start, d.End, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	var values []int64
	for reader.Next() {
		values = append(values, reader.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
	}
	if reader.Err() != nil {
		t.Fatal(reader.Err())
	}
	expected := []int64{1<<53 + 1, 1<<63 - 1, -1, 1<<53 + 1, 1<<63 - 1, -1}
	if !slices.Equal(values, expected) {
		t.Fatal("exact independent-process rows differ")
	}
	fmt.Println("RETAINED_PROCESS_REPLAY_OK")
}

func TestRetainedPartitionReaderIndependentProcess(t *testing.T) {
	store, ref := retainedReaderFixture(t)
	_, cluster := store.client.(*goredis.ClusterClient)
	d := retainedPartitionID{Version: 2, Reference: ref, Start: 0, End: 2}
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.client.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		command := exec.CommandContext(ctx, executable, "-test.run=^TestRetainedPartitionReaderProcessHelper$", "-test.v")
		command.Env = append(os.Environ(), "ADBC_RETAINED_TEST_DESCRIPTOR="+string(encoded), fmt.Sprintf("ADBC_RETAINED_TEST_CLUSTER=%t", cluster))
		output, err := command.CombinedOutput()
		cancel()
		if err != nil || !strings.Contains(string(output), "RETAINED_PROCESS_REPLAY_OK") {
			t.Fatalf("independent process replay failed: %v: %s", err, output)
		}
	}
}
