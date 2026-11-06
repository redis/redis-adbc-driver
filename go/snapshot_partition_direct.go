// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotDirectReadKey struct{}

// Native ownership/directory evidence suffices for finite HASH reads. Do not
// insert this narrower proof into the full Search/index validation cache.
func (s *store) validateSnapshotDirectTable(ctx context.Context, meta *tableMeta) error {
	if err := validateSnapshotDescriptor(ctx, meta); err != nil {
		return err
	}
	if snapshotOf(ctx) == nil || snapshotOf(ctx).pin == nil {
		return errorf(adbc.StatusInvalidState, "direct snapshot partition requires a pin")
	}
	segments := meta.snapshotSegments()
	type probe struct {
		record, refs *goredis.StringCmd
		count        *goredis.IntCmd
	}
	checks := make([]probe, len(segments))
	pipe := s.redis(ctx).Pipeline()
	for i, seg := range segments {
		if snapshotOwns(ctx, seg.Prefix) {
			return errorf(adbc.StatusInvalidState, "direct snapshot partition requires published data")
		}
		id := snapshotDataID(seg.Prefix)
		checks[i] = probe{pipe.HGet(ctx, snapshotMetadataGraph, "d:"+id), pipe.HGet(ctx, snapshotMetadataGraph, "r:"+id), pipe.ZCard(ctx, snapshotMembers(id))}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return wrapRedis(err, "direct snapshot ownership or directory proof is unavailable")
	}
	for i, check := range checks {
		var record struct {
			Prefix string `json:"prefix"`
			State  string `json:"state"`
		}
		refs, err := check.refs.Int64()
		if err != nil || refs <= 0 || strconv.FormatInt(refs, 10) != check.refs.Val() || json.Unmarshal([]byte(check.record.Val()), &record) != nil || record.State != "live" || record.Prefix != segments[i].Prefix || check.count.Val() != segments[i].Rows {
			return errorf(adbc.StatusIO, "direct snapshot ownership or row directory is invalid")
		}
	}
	return snapshotOf(ctx).pin.valid(ctx)
}

// Batch membership checks include present HASHes: cardinality alone cannot
// distinguish an owned row from an unregistered image with a valid row ID.
func (s *store) snapshotDirectMembers(ctx context.Context, seg snapshotSegment, keys []string) ([]bool, error) {
	directory := snapshotMembers(snapshotDataID(seg.Prefix))
	args := []any{"ZMSCORE", directory}
	for _, key := range keys {
		args = append(args, snapshotDirectoryMember(key))
	}
	pipe := s.redis(ctx).Pipeline()
	count := pipe.ZCard(ctx, directory)
	command := pipe.Do(ctx, args...)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, wrapRedis(err, "direct snapshot row membership is unavailable")
	}
	scores, err := command.Slice()
	if err != nil || len(scores) != len(keys) || count.Val() != seg.Rows {
		return nil, errorf(adbc.StatusIO, "direct snapshot row directory changed or returned invalid membership")
	}
	present := make([]bool, len(keys))
	for i, score := range scores {
		if score == nil {
			continue
		}
		zero := false
		switch value := score.(type) {
		case float64:
			zero = value == 0
		case string:
			zero = value == "0"
		}
		if !zero {
			return nil, errorf(adbc.StatusIO, "direct snapshot row directory has a nonzero or invalid score")
		}
		present[i] = true
	}
	return present, nil
}
