// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

// A failed native count read must not become a successful zero-count proof.
// Keep manifest/directory/map refusal precedence when reads share a pipeline.
func TestSnapshotManifestProofRejectsInvalidInputs(t *testing.T) {
	for _, kind := range []string{"missing_manifest", "invalid_manifest", "not_committed", "missing_keys", "negative_keys", "wrong_keys", "missing_buckets", "negative_buckets", "oversized_buckets", "wrong_buckets", "directory_type", "map_type", "manifest_before_directory"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			root := snapshotOf(ctx).id
			original, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+root).Result()
			if err != nil {
				t.Fatal(err)
			}
			var work map[string]any
			if err = json.Unmarshal([]byte(original), &work); err != nil {
				t.Fatal(err)
			}
			if work["metadata_keys"] != float64(0) || work["metadata_buckets"] != float64(0) {
				t.Fatal("test requires a valid empty committed manifest")
			}
			if err = s.validateSnapshotMetadata(ctx, root); err != nil {
				t.Fatal("healthy empty proof rejected", err)
			}
			defer func() {
				if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+root, original).Err(); err != nil {
					t.Error("restore manifest", err)
				}
				if kind == "directory_type" || kind == "manifest_before_directory" {
					if err := raw.Del(h.ctx, snapshotMetadataKeys(root)).Err(); err != nil {
						t.Error("restore directory", err)
					}
				}
				if kind == "map_type" {
					if err := raw.Del(h.ctx, snapshotMetadataMap(root)).Err(); err != nil {
						t.Error("restore map", err)
					}
				}
			}()
			want := "invalid snapshot metadata manifest"
			switch kind {
			case "missing_manifest", "manifest_before_directory":
				err = raw.HDel(h.ctx, snapshotCatalogKey, "w:"+root).Err()
				want = "snapshot metadata manifest unavailable"
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
				want = "snapshot metadata directory is incomplete"
			case "missing_buckets":
				delete(work, "metadata_buckets")
				want = "invalid snapshot metadata bucket manifest"
			case "negative_buckets":
				work["metadata_buckets"] = -1
				want = "invalid snapshot metadata bucket manifest"
			case "oversized_buckets":
				work["metadata_buckets"] = 257
				want = "invalid snapshot metadata bucket manifest"
			case "wrong_buckets":
				work["metadata_buckets"] = 1
				want = "snapshot metadata map is incomplete"
			case "directory_type":
				err = raw.Set(h.ctx, snapshotMetadataKeys(root), "wrong-type", 0).Err()
				want = "WRONGTYPE"
			case "map_type":
				err = raw.Set(h.ctx, snapshotMetadataMap(root), "wrong-type", 0).Err()
				want = "WRONGTYPE"
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "manifest_before_directory" {
				if err = raw.Set(h.ctx, snapshotMetadataKeys(root), "wrong-type", 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "missing_manifest" && kind != "invalid_manifest" && kind != "manifest_before_directory" {
				encoded, err := json.Marshal(work)
				if err != nil {
					t.Fatal(err)
				}
				if err = raw.HSet(h.ctx, snapshotCatalogKey, "w:"+root, encoded).Err(); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.validateSnapshotMetadata(ctx, root); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("invalid proof not rejected as %q: %v", want, err)
			}
		})
	}
}

type snapshotManifestPipelineReplyLoss struct{ hit atomic.Bool }

func (*snapshotManifestPipelineReplyLoss) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}
func (*snapshotManifestPipelineReplyLoss) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (f *snapshotManifestPipelineReplyLoss) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, commands []goredis.Cmder) error {
		err := next(ctx, commands)
		if err == nil && len(commands) == 3 && commands[0].Name() == "hget" && commands[1].Name() == "zcard" && commands[2].Name() == "hlen" && f.hit.CompareAndSwap(false, true) {
			return io.ErrUnexpectedEOF
		}
		return err
	}
}

func TestSnapshotManifestProofRejectsPipelineErrorWithHealthyValues(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	fault := &snapshotManifestPipelineReplyLoss{}
	s.client.AddHook(fault)
	err = s.validateSnapshotMetadata(ctx, snapshotOf(ctx).id)
	if !fault.hit.Load() {
		t.Fatal("test did not inject the pipeline-only error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("pipeline error was hidden by healthy command values", err)
	}
}
