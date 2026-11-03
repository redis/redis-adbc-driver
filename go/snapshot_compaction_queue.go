// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const snapshotCompactionQueue = snapshotPrefix + "compaction:queue"
const snapshotCompactionCandidates = snapshotPrefix + "compaction:candidates"

type snapshotCompactionCandidate struct {
	References []string `json:"references"`
	Key        string   `json:"key"`
	Schema     string   `json:"schema"`
	Name       string   `json:"name"`
	OID        string   `json:"oid"` // Preserve identity without Lua numeric re-encoding.
	Lineage    string   `json:"lineage"`
	Version    string   `json:"version"`
}
type snapshotCompactionQueueUpdate struct {
	Key       string                       `json:"key"`
	Candidate *snapshotCompactionCandidate `json:"candidate,omitempty"`
}

// Reuse descriptors already checked by final validation. No additional catalog
// enumeration or metadata reads are needed to publish maintenance eligibility.
func prepareSnapshotCompactionQueue(ctx context.Context, tables []*tableMeta) error {
	scope := snapshotOf(ctx)
	scope.metadataMu.Lock()
	keys := make([]string, 0, len(scope.metadata.dirty))
	for key := range scope.metadata.dirty {
		keys = append(keys, key)
	}
	scope.metadataMu.Unlock()
	sort.Strings(keys)
	eligible := map[string]*tableMeta{}
	for _, m := range tables {
		if len(m.snapshotSegments()) >= snapshotCompactionTrigger {
			eligible[strings.TrimPrefix(metaKey(m.Schema, m.Name), metaPrefix)] = m
		}
	}
	updates := make([]snapshotCompactionQueueUpdate, 0, len(keys))
	for _, key := range keys {
		update := snapshotCompactionQueueUpdate{Key: key}
		if m := eligible[key]; m != nil {
			if m.OID <= 0 || !snapshotMetadataID(m.SnapshotLineage) {
				return errorf(adbc.StatusIO, "invalid compaction candidate identity")
			}
			update.Candidate = &snapshotCompactionCandidate{Key: key, Schema: m.Schema, Name: m.Name, OID: strconv.FormatInt(m.OID, 10), Lineage: m.SnapshotLineage, Version: scope.id}
			for _, seg := range m.snapshotSegments() {
				update.Candidate.References = append(update.Candidate.References, snapshotDataID(seg.Prefix))
			}
		}
		updates = append(updates, update)
	}
	raw, err := json.Marshal(updates)
	if err == nil {
		scope.compactionUpdates = string(raw)
	}
	return err
}

func (s *store) startSnapshotCompaction(ctx context.Context, token string) (bool, error) {
	candidates, err := s.snapshotControl(ctx, "compact_candidates", "", "", "").StringSlice()
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		return false, nil
	}
	if len(candidates)%2 != 0 {
		return false, errorf(adbc.StatusIO, "invalid compaction queue reply")
	}
	cctx, pin, err := s.acquireCompactionCatalog(ctx)
	if err != nil {
		return false, err
	}
	defer pin.release()
	var firstError error
	for i := 0; i < len(candidates); i += 2 {
		key, raw := candidates[i], candidates[i+1]
		var candidate snapshotCompactionCandidate
		if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
			return false, err
		}
		if len(candidate.References) < snapshotCompactionTrigger || len(candidate.References) > snapshotMaxSegments {
			return false, errorf(adbc.StatusIO, "invalid compaction candidate sources")
		}
		for _, data := range candidate.References {
			if !snapshotMetadataID(data) {
				return false, errorf(adbc.StatusIO, "invalid compaction candidate data identity")
			}
		}
		oid, parseErr := strconv.ParseInt(candidate.OID, 10, 64)
		if parseErr != nil || oid <= 0 || candidate.Key != key || key != strings.TrimPrefix(metaKey(candidate.Schema, candidate.Name), metaPrefix) || !snapshotMetadataID(candidate.Version) || !snapshotMetadataID(candidate.Lineage) {
			return false, errorf(adbc.StatusIO, "invalid compaction queue identity")
		}
		meta, err := s.getTable(cctx, candidate.Schema, candidate.Name)
		var ae adbc.Error
		missing := errors.As(err, &ae) && ae.Code == adbc.StatusNotFound
		if err != nil && !missing {
			// Preserve the work item but move a failed source behind other due work.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
			e := s.snapshotControl(cleanup, "compact_defer", "", key, "", raw).Err()
			cancel()
			if e != nil {
				return false, e
			}
			if ctx.Err() != nil {
				return false, err
			}
			if firstError == nil {
				firstError = err
			}
			continue
		}
		if missing || meta.OID != oid || meta.SnapshotLineage != candidate.Lineage || len(meta.snapshotSegments()) < snapshotCompactionTrigger {
			if err := s.snapshotControl(ctx, "compact_discard", "", key, "", raw).Err(); err != nil {
				return false, err
			}
			continue
		}
		scope := snapshotOf(cctx)
		scope.metadataMu.Lock()
		version := scope.metadata.reads[key]
		scope.metadataMu.Unlock()
		if !snapshotMetadataID(version) {
			return false, errorf(adbc.StatusIO, "invalid compaction source version")
		}
		job := &snapshotCompactionJob{ID: snapshotID(), Token: token, Data: snapshotID(), Source: meta, Phase: "copy", References: []string{}, QueueKey: key, QueueVersion: version, QueueOID: strconv.FormatInt(meta.OID, 10)}
		job.Prefix = snapshotRows(job.Data, meta.Schema)
		for _, seg := range meta.snapshotSegments() {
			job.References = append(job.References, snapshotDataID(seg.Prefix))
		}
		encoded, err := json.Marshal(job)
		if err != nil {
			return false, err
		}
		n, err := s.snapshotControl(cctx, "compact_start", pin.token, job.ID, pin.root, string(encoded)).Int()
		return n == 1, err
	}
	return false, firstError
}
