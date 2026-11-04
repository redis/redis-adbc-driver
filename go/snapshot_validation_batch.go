// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotSegmentProbe struct {
	segment snapshotSegment
	proof   *snapshotSegmentValidation
	info    *goredis.Cmd
	count   *goredis.IntCmd
}

// A descriptor bounds the batch to snapshotMaxSegments. Every registered index
// and row directory has {meta}; select that primary explicitly, as searchDo does,
// rather than let Cluster command-key inference route an FT.INFO elsewhere.
// This is a read pipeline, not a transaction or a retry of SQL.
func (s *store) validateSnapshotSegments(ctx context.Context, probes []snapshotSegmentProbe) {
	defer func() {
		for _, probe := range probes {
			close(probe.proof.done)
		}
	}()
	fail := func(err error) {
		for _, probe := range probes {
			probe.proof.err = err
		}
	}
	node, err := s.search(ctx, snapshotCatalogKey)
	if err != nil {
		fail(err)
		return
	}
	client, ok := node.c.(interface{ Pipeline() goredis.Pipeliner })
	if !ok {
		fail(errorf(adbc.StatusIO, "snapshot validation requires a pipeline-capable primary"))
		return
	}
	pipe := client.Pipeline()
	for i := range probes {
		probe := &probes[i]
		probe.info = goredis.NewCmd(ctx, "FT.INFO", probe.segment.Index)
		// Preserve Search no-retry behavior for the complete batch, including
		// lost replies or primary movement. The caller may issue a fresh statement.
		_ = pipe.Process(ctx, onceCmd{probe.info})
		probe.count = pipe.ZCard(ctx, snapshotMembers(snapshotDataID(probe.segment.Prefix)))
	}
	_, execErr := pipe.Exec(ctx)
	if err := ctx.Err(); err != nil {
		fail(wrapRedis(err, "snapshot validation batch canceled"))
		return
	}
	for _, probe := range probes {
		var err error
		switch {
		case probe.info.Err() != nil:
			err = wrapRedis(probe.info.Err(), "published snapshot index is unavailable")
		case probe.count.Err() != nil:
			err = wrapRedis(probe.count.Err(), "snapshot row directory is unavailable")
		case execErr != nil:
			// A transport/hook error must not turn default/partial replies into
			// a successful proof, even if individual command errors are absent.
			err = wrapRedis(execErr, "snapshot validation batch is unavailable")
		default:
			info, seg := indexInfoOf(probe.info.Val()), probe.segment
			if probe.count.Val() != seg.Rows || info.indexing || info.percent < 1 || info.failures != 0 || info.numDocs != seg.Rows || len(info.prefixes) != 1 || info.prefixes[0] != seg.Prefix {
				err = errorf(adbc.StatusIO, "published snapshot segment is incomplete or rebuilding")
			}
		}
		probe.proof.err = err
	}
}
