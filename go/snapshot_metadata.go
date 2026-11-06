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
	// A live pin protects this immutable committed root. Batch the same native
	// proof reads; this is not a transaction or a weaker cached validation.
	pipe := s.client.Pipeline()
	manifest := pipe.HGet(ctx, snapshotCatalogKey, "w:"+root)
	directory := pipe.ZCard(ctx, snapshotMetadataKeys(root))
	rootMap := pipe.HLen(ctx, snapshotMetadataMap(root))
	_, pipelineErr := pipe.Exec(ctx)
	// Preserve proof/error precedence instead of consuming a failed command's
	// zero value or letting a later pipeline error hide the manifest failure.
	raw, err := manifest.Bytes()
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
	count, err := directory.Result()
	if err != nil {
		return err
	}
	if count != *work.Count {
		return errorf(adbc.StatusIO, "snapshot metadata directory is incomplete")
	}
	if work.Buckets == nil || *work.Buckets < 0 || *work.Buckets > 256 {
		return errorf(adbc.StatusIO, "invalid snapshot metadata bucket manifest")
	}
	buckets, err := rootMap.Result()
	if err != nil {
		return err
	}
	if buckets != *work.Buckets {
		return errorf(adbc.StatusIO, "snapshot metadata map is incomplete")
	}
	// Hooks/transports can fail the pipeline even when individual replies are
	// populated. A proof never succeeds after an unaccounted-for pipeline error.
	return pipelineErr
}
