// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotPublicationWorkObserver struct {
	descriptorReads, directoryPages, aborts atomic.Int64
}

func (*snapshotPublicationWorkObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *snapshotPublicationWorkObserver) record(cmd goredis.Cmder) {
	args := cmd.Args()
	if len(args) > 1 {
		key := fmt.Sprint(args[1])
		if cmd.Name() == "get" && strings.HasSuffix(key, ":table:public:t") {
			h.descriptorReads.Add(1)
		}
		if cmd.Name() == "zrangebylex" && strings.HasPrefix(key, snapshotPrefix+"members:") {
			h.directoryPages.Add(1)
		}
	}
	if compactionProfileCategory(cmd) == "control_abort" {
		h.aborts.Add(1)
	}
}
func (h *snapshotPublicationWorkObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { h.record(cmd); return next(ctx, cmd) }
}
func (h *snapshotPublicationWorkObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			h.record(cmd)
		}
		return next(ctx, cmds)
	}
}

func TestSnapshotPublicationReusesAuthorizedDescriptors(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t VALUES(1,9007199254740993),(2,9007199254740994)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	work, writer, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=v+1 WHERE __rowid=1")
	observer := &snapshotPublicationWorkObserver{}
	s.client.AddHook(observer)
	if err := s.publishSnapshot(work, writer, id); err != nil {
		t.Fatal(err)
	}
	if got := observer.descriptorReads.Load(); got != 1 {
		t.Fatalf("publication read private descriptor %d times; want one authorized read", got)
	}
	if got := observer.directoryPages.Load(); got != 1 {
		t.Fatalf("point publication read %d directory pages; want one", got)
	}
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|9007199254740994", "2|9007199254740994")
	prior := snapshotExecuteTest(t, s, old, "SELECT v FROM t WHERE __rowid=1")
	if len(prior.rows) != 1 || prior.rows[0][0].I != 9007199254740993 {
		t.Fatal("old reader changed", prior.rows)
	}
}

func TestSnapshotConfirmedSQLPublicationSkipsAbort(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER NOT NULL)")
	h.exec("INSERT INTO t VALUES(1,10)")
	s := snapshotTestStore(t, raw)
	observer := &snapshotPublicationWorkObserver{}
	s.client.AddHook(observer)
	for _, sql := range []string{"INSERT INTO t VALUES(2,20)", "UPDATE t SET v=11 WHERE __rowid=1", "DELETE FROM t WHERE __rowid=2"} {
		snapshotExecuteTest(t, s, h.ctx, sql)
		if got := observer.aborts.Load(); got != 0 {
			t.Fatalf("confirmed commit sent %d redundant aborts", got)
		}
	}
	// No-op DML still retires its private workspace rather than publishing it.
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t SET v=99 WHERE __rowid=999")
	if got := observer.aborts.Load(); got != 1 {
		t.Fatal("no-op private workspace was not aborted", got)
	}
	parsed, err := ParseScript("INSERT INTO t VALUES(3,NULL)")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	if _, err := e.execute(h.ctx, parsed[0], nil, nil, nil); err == nil {
		t.Fatal("NOT NULL violation accepted")
	}
	if got := observer.aborts.Load(); got != 2 {
		t.Fatal("failed private workspace was not aborted", got)
	}
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|11")
}

// The atomic clone checks the same base completeness proof before changing any
// references. Corrupt the base after acquisition so an acquisition check cannot
// accidentally be the only protection after removing the duplicate pipeline.
func TestSnapshotCloneAtomicallyRejectsInvalidBase(t *testing.T) {
	for _, kind := range []string{"missing_manifest", "invalid_manifest", "not_committed", "missing_keys", "negative_keys", "wrong_keys", "missing_buckets", "negative_buckets", "oversized_buckets", "wrong_buckets", "directory_type", "map_type", "actual_oversized_map", "actual_map_mismatch", "missing_reference"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			if kind == "missing_reference" {
				h.exec("CREATE TABLE t(v INTEGER)")
			}
			s := snapshotTestStore(t, raw)
			base, pin, err := s.acquireSnapshot(h.ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			root, id := pin.root, snapshotID()
			original, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+root).Result()
			if err != nil {
				t.Fatal(err)
			}
			if n, err := s.snapshotControl(base, "reserve", pin.token, id, root).Int(); err != nil || n != 1 {
				t.Fatal("reserve", n, err)
			}
			var work map[string]any
			if err := json.Unmarshal([]byte(original), &work); err != nil {
				t.Fatal(err)
			}
			var reference, referenceValue string
			defer func() {
				if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+root, original).Err(); err != nil {
					t.Error("restore manifest", err)
				}
				if kind == "directory_type" {
					_ = raw.Del(h.ctx, snapshotMetadataKeys(root)).Err()
				}
				if kind == "map_type" || kind == "actual_oversized_map" || kind == "actual_map_mismatch" {
					_ = raw.Del(h.ctx, snapshotMetadataMap(root)).Err()
				}
				if reference != "" {
					_ = raw.HSet(h.ctx, snapshotMetadataGraph, reference, referenceValue).Err()
				}
				s.abortSnapshot(pin, id)
				pin.release()
			}()
			switch kind {
			case "missing_manifest":
				err = raw.HDel(h.ctx, snapshotCatalogKey, "w:"+root).Err()
			case "invalid_manifest":
				err = raw.HSet(h.ctx, snapshotCatalogKey, "w:"+root, "{").Err()
			case "not_committed":
				work["state"] = "staging"
			case "missing_keys":
				delete(work, "metadata_keys")
			case "negative_keys":
				work["metadata_keys"] = -1
			case "wrong_keys":
				work["metadata_keys"] = 1
			case "missing_buckets":
				delete(work, "metadata_buckets")
			case "negative_buckets":
				work["metadata_buckets"] = -1
			case "oversized_buckets":
				work["metadata_buckets"] = 257
			case "wrong_buckets":
				work["metadata_buckets"] = 1
			case "directory_type":
				err = raw.Set(h.ctx, snapshotMetadataKeys(root), "wrong-type", 0).Err()
			case "map_type":
				err = raw.Set(h.ctx, snapshotMetadataMap(root), "wrong-type", 0).Err()
			case "actual_oversized_map", "actual_map_mismatch":
				// Seed the physical map, not only the declared count. Both a
				// matching oversized manifest and a false small manifest must
				// fail before the clone enumerates or retains any references.
				entries := map[string]any{}
				for i := range 257 {
					entries[fmt.Sprintf("corrupt_%d", i)] = snapshotID()
				}
				err = raw.HSet(h.ctx, snapshotMetadataMap(root), entries).Err()
				if kind == "actual_oversized_map" {
					work["metadata_buckets"] = 257
				}
			case "missing_reference":
				entries, e := raw.HGetAll(h.ctx, snapshotMetadataMap(root)).Result()
				if e != nil {
					t.Fatal(e)
				}
				for slot, owner := range entries {
					reference = "b:" + owner + ":" + slot
					break
				}
				if reference == "" {
					t.Fatal("fixture has no bucket")
				}
				referenceValue, err = raw.HGet(h.ctx, snapshotMetadataGraph, reference).Result()
				if err == nil {
					err = raw.HDel(h.ctx, snapshotMetadataGraph, reference).Err()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind != "missing_manifest" && kind != "invalid_manifest" {
				encoded, e := json.Marshal(work)
				if e != nil {
					t.Fatal(e)
				}
				if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+root, encoded).Err(); err != nil {
					t.Fatal(err)
				}
			}
			before, err := raw.HGetAll(h.ctx, snapshotMetadataGraph).Result()
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(base, snapshotContextKey{}, &snapshotScope{id: id, pin: pin, store: s, write: true})
			if err := s.cloneSnapshotMetadata(base, ctx, root, id); err == nil || !isStatus(err, adbc.StatusIO) {
				t.Fatal("invalid base clone not rejected with IO", err)
			}
			after, err := raw.HGetAll(h.ctx, snapshotMetadataGraph).Result()
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(before, after) {
				t.Fatal("invalid clone changed references")
			}
			if n, err := raw.HLen(h.ctx, snapshotMetadataMap(id)).Result(); err != nil || n != 0 {
				t.Fatal("invalid clone wrote target map", n, err)
			}
			if current, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || current != root {
				t.Fatal("invalid clone changed current root", err)
			}
		})
	}
}
