// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
)

// The queued command is exactly the existing valid operation, including its
// declared protocol-17 keys, reader namespace, TTL and no-retry marker. Only
// definite top-level digest refusals can trigger one standalone full-body
// attempt; neither metadata reads nor an uncertain lease reply are replayed.
type queuedSnapshotValidation struct {
	pin      *snapshotPin
	command  *goredis.Cmd
	fallback []any
	digest   bool
}

func (p *snapshotPin) queueValid(ctx context.Context, pipe goredis.Pipeliner) *queuedSnapshotValidation {
	var args []any
	digest := false
	if p.reader || p.store.restrictedReader {
		keys := []any{snapshotCatalogKey, snapshotReaderDirectory, snapshotReaderPrefix + p.store.readerNamespace, snapshotCatalogKey, snapshotReaderDirectory}
		args = []any{"EVAL", snapshotReaderControl, len(keys)}
		args = append(args, keys...)
		args = append(args, "valid", p.store.readerNamespace, p.token, "", p.root, "120000", 0)
	} else {
		keys := []any{snapshotCatalogKey, snapshotMetadataGraph, snapshotDataQueue, snapshotCompactionQueue, snapshotCompactionCandidates}
		args = []any{"EVAL", snapshotControl, len(keys)}
		args = append(args, keys...)
		args = append(args, "valid", p.token, "", p.root, snapshotLease.Milliseconds(), "", "[]", len(keys), 0)
		digest = !p.store.controlScript.evalOnly.Load()
	}
	queued := &queuedSnapshotValidation{pin: p, fallback: args, digest: digest}
	wire := args
	if digest {
		wire = append([]any(nil), args...)
		wire[0], wire[1] = "EVALSHA", snapshotControlDigest
	}
	queued.command = once(ctx, pipe, wire...)
	return queued
}
func (q *queuedSnapshotValidation) resolve(ctx context.Context, pipelineErr error) error {
	missing, denied := snapshotControlScriptRefusal(q.command.Err())
	// A hook/transport failure cannot be hidden by a simultaneously populated
	// native NOSCRIPT reply. Accept only the exact direct native refusal as the
	// pipeline error (or nil for transports that expose errors per command).
	sameRefusal := pipelineErr == nil
	if m, d := snapshotControlScriptRefusal(pipelineErr); (m || d) && q.command.Err() != nil && pipelineErr.Error() == q.command.Err().Error() {
		sameRefusal = true
	}
	if q.digest && (missing || denied) && sameRefusal {
		if err := q.pin.validLocal(ctx); err != nil {
			return err
		}
		if denied {
			q.pin.store.controlScript.evalOnly.Store(true)
		}
		return q.pin.validReply(once(ctx, q.pin.store.client, q.fallback...))
	}
	if pipelineErr != nil {
		return wrapRedis(pipelineErr, "failed to validate statement snapshot")
	}
	if err := q.pin.validLocal(ctx); err != nil {
		return err
	}
	return q.pin.validReply(q.command)
}
