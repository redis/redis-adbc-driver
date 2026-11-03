// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Lexicographic continuation is stable across server restarts and slot moves.
// Directories are immutable once published; reclamation cannot race a live pin.
func (s *store) walkSnapshotDirectory(ctx context.Context, directory string, visit func([]string) error) error {
	min := "-"
	for {
		keys, err := s.client.ZRangeByLex(ctx, directory, &goredis.ZRangeBy{Min: min, Max: "+", Count: 256}).Result()
		if err != nil {
			return wrapRedis(err, "failed to read snapshot directory")
		}
		if len(keys) == 0 {
			return nil
		}
		if err := visit(keys); err != nil {
			return err
		}
		min = "(" + keys[len(keys)-1]
	}
}

func (s *store) validateSnapshotMetadata(ctx context.Context, root string) error {
	raw, err := s.client.HGet(ctx, snapshotCatalogKey, "w:"+root).Bytes()
	if err != nil {
		return wrapRedis(err, "snapshot metadata manifest unavailable")
	}
	var work struct {
		State   string `json:"state"`
		Count   *int64 `json:"metadata_keys"`
		Buckets *int64 `json:"metadata_buckets"`
	}
	if err := json.Unmarshal(raw, &work); err != nil || work.State != "committed" || work.Count == nil || *work.Count < 0 {
		return errorf(adbc.StatusIO, "invalid snapshot metadata manifest")
	}
	count, err := s.client.ZCard(ctx, snapshotMetadataKeys(root)).Result()
	if err != nil {
		return err
	}
	if count != *work.Count {
		return errorf(adbc.StatusIO, "snapshot metadata directory is incomplete")
	}
	if work.Buckets == nil || *work.Buckets < 0 || *work.Buckets > 256 {
		return errorf(adbc.StatusIO, "invalid snapshot metadata bucket manifest")
	}
	buckets, err := s.client.HLen(ctx, snapshotMetadataMap(root)).Result()
	if err != nil {
		return err
	}
	if buckets != *work.Buckets {
		return errorf(adbc.StatusIO, "snapshot metadata map is incomplete")
	}
	return nil
}
