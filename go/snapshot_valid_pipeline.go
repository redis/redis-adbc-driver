// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
)

// The queued FCALL preserves the pin's declared keys, namespace, TTL and
// no-retry marker. Refused or uncertain replies fail without a second command.
type queuedSnapshotValidation struct {
	pin     *snapshotPin
	command *goredis.Cmd
}

func (p *snapshotPin) queueValid(ctx context.Context, pipe goredis.Pipeliner) *queuedSnapshotValidation {
	var args []any
	if p.reader || p.store.restrictedReader {
		keys := []any{snapshotCatalogKey, snapshotReaderDirectory, snapshotReaderPrefix + p.store.readerNamespace, snapshotCatalogKey, snapshotReaderDirectory}
		args = []any{"FCALL", redisFunctionName(snapshotReaderControl), len(keys)}
		args = append(args, keys...)
		args = append(args, "valid", p.store.readerNamespace, p.token, "", p.root, "120000", 0)
	} else {
		keys := []any{snapshotCatalogKey, snapshotMetadataGraph, snapshotDataQueue, snapshotCompactionQueue, snapshotCompactionCandidates}
		args = []any{"FCALL", redisFunctionName(snapshotControl), len(keys)}
		args = append(args, keys...)
		args = append(args, "valid", p.token, "", p.root, snapshotLease.Milliseconds(), "", "[]", len(keys), 0)
	}
	queued := &queuedSnapshotValidation{pin: p}
	queued.command = once(ctx, pipe, args...)
	return queued
}
func (q *queuedSnapshotValidation) resolve(ctx context.Context, pipelineErr error) error {
	if pipelineErr != nil {
		return wrapRedis(pipelineErr, "failed to validate statement snapshot")
	}
	if err := q.pin.validLocal(ctx); err != nil {
		return err
	}
	return q.pin.validReply(q.command)
}
