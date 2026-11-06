// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSnapshotClusterPublicationRejectsIncompleteMetadataBucket(t *testing.T) {
	for _, operation := range []string{"seal", "publish"} {
		for _, corruption := range []string{"missing_directory", "wrong_count", "missing_bucket", "missing_reference"} {
			t.Run(operation+"/"+corruption, func(t *testing.T) {
				h, raw, _ := snapshotClusterHarness(t)
				st, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close(h.ctx)
				s := st.(*statementImpl).conn.store
				ctx, pin, err := s.acquireSnapshot(h.ctx, true)
				if err != nil {
					t.Fatal(err)
				}
				defer pin.release()
				wctx, id, err := s.beginSnapshotWrite(ctx, pin)
				if err != nil {
					t.Fatal(err)
				}
				defer s.abortSnapshot(pin, id)
				const suffix = "publication_integrity"
				if err := s.client.Set(wctx, metaPrefix+suffix, "private", 0).Err(); err != nil {
					t.Fatal(err)
				}
				slot := snapshotMetadataSlot(suffix)
				bucket := snapshotMetadataBucket(id, slot)
				switch corruption {
				case "missing_directory":
					err = raw.Del(h.ctx, snapshotMetadataDirectory(id, slot)).Err()
				case "wrong_count":
					err = raw.HSet(h.ctx, snapshotMetadataGraph, "n:"+id+":"+slot, 2).Err()
				case "missing_bucket":
					err = raw.Del(h.ctx, bucket).Err()
				case "missing_reference":
					err = raw.HDel(h.ctx, snapshotMetadataGraph, "b:"+id+":"+slot).Err()
				}
				if err != nil {
					t.Fatal(err)
				}
				err = s.snapshotControl(wctx, operation, pin.token, id, pin.root).Err()
				if err == nil || !strings.Contains(err.Error(), "snapshot metadata bucket") {
					t.Fatal("incomplete metadata admitted by "+operation, err)
				}
				if root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); root != pin.root {
					t.Fatal("incomplete metadata became the committed root")
				}
			})
		}
	}
}

func TestSnapshotClusterPublicationAllowsValidEmptyBuckets(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			h, _, _ := snapshotClusterHarness(t)
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			s := st.(*statementImpl).conn.store
			ctx, pin, err := s.acquireSnapshot(h.ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			wctx, id, err := s.beginSnapshotWrite(ctx, pin)
			if err != nil {
				t.Fatal(err)
			}
			defer s.abortSnapshot(pin, id)
			const key = metaPrefix + "publication_integrity"
			if err := s.client.Set(wctx, key, "private", 0).Err(); err != nil {
				t.Fatal(err)
			}
			if empty {
				if err := s.client.Del(wctx, key).Err(); err != nil {
					t.Fatal(err)
				}
				if err := s.pruneSnapshotMetadata(wctx); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := s.snapshotControl(wctx, "seal", pin.token, id, pin.root).Int(); err != nil || n != 1 {
				t.Fatal("valid bucket did not seal", n, err)
			}
			if n, err := s.snapshotControl(wctx, "publish", pin.token, id, pin.root).Int(); err != nil || n != 1 {
				t.Fatal("valid bucket did not publish", n, err)
			}
		})
	}
}

func TestSnapshotClusterPublicationRejectsIncompleteMergedMetadata(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	s := st.(*statementImpl).conn.store
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	ctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.client.Set(ctx, metaPrefix+"original_integrity", "private", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.snapshotControl(ctx, "seal", pin.token, id, pin.root).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	latest, other, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.release()
	merged, mergeID, err := s.beginSnapshotWrite(latest, other)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(other, mergeID)
	const suffix = "merged_integrity"
	if err := s.client.Set(merged, metaPrefix+suffix, "private", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Del(h.ctx, snapshotMetadataDirectory(mergeID, snapshotMetadataSlot(suffix))).Err(); err != nil {
		t.Fatal(err)
	}
	proof, err := json.Marshal(snapshotMergeBase{ID: mergeID, Token: other.token, Base: other.root})
	if err != nil {
		t.Fatal(err)
	}
	err = s.snapshotControl(ctx, "publish", pin.token, id, pin.root, string(proof), "", mergeID).Err()
	if err == nil || !strings.Contains(err.Error(), "snapshot metadata bucket") {
		t.Fatal("incomplete merged map admitted", err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != pin.root || raw.HLen(h.ctx, snapshotMetadataMap(id)).Val() == 0 {
		t.Fatal("merge refusal changed root or original metadata ownership")
	}
}
