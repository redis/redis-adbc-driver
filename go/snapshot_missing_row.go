// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Preserve native typed decoders and commands while preventing transparent
// transport replay, including helper calls outside an execution transport.
type snapshotMissingCountCmd struct{ *goredis.IntCmd }

func (snapshotMissingCountCmd) NoRetry() bool { return true }

type snapshotMissingScoreCmd struct{ *goredis.FloatCmd }

func (snapshotMissingScoreCmd) NoRetry() bool { return true }

// A published missing image needs both directory cardinality and membership
// evidence. Queue the same native reads in one request, preserving count-before-
// score error precedence. This is not an atomic read or a cached proof. Private
// images keep their existing score-only path; their directory is still growing.
func (s *store) verifySnapshotMissingRow(ctx context.Context, seg snapshotSegment, key string) error {
	client := s.redis(ctx)
	directory := snapshotMembers(snapshotDataID(seg.Prefix))
	score := goredis.NewFloatCmd(ctx, "zscore", directory, snapshotDirectoryMember(key))
	var commandErr error
	if snapshotOwns(ctx, seg.Prefix) {
		commandErr = client.Process(ctx, snapshotMissingScoreCmd{score})
	} else {
		pipe := client.Pipeline()
		count := goredis.NewIntCmd(ctx, "zcard", directory)
		if err := pipe.Process(ctx, snapshotMissingCountCmd{count}); err != nil {
			pipe.Discard()
			return wrapRedis(err, "failed to verify snapshot row directory")
		}
		if err := pipe.Process(ctx, snapshotMissingScoreCmd{score}); err != nil {
			pipe.Discard()
			return wrapRedis(err, "failed to verify snapshot row directory")
		}
		_, commandErr = pipe.Exec(ctx)
		n, err := count.Result()
		if err != nil {
			return wrapRedis(err, "failed to verify snapshot row directory")
		}
		if n != seg.Rows {
			return errorf(adbc.StatusIO, "published snapshot row directory changed")
		}
	}
	// Only the exact native Nil reply proves absence. A populated score or count
	// cannot explain an outer transport/hook error, nor can wrapped/joined Nil.
	if commandErr != nil && !(commandErr == goredis.Nil && score.Err() == goredis.Nil) {
		return wrapRedis(commandErr, "failed to verify snapshot row directory")
	}
	if err := score.Err(); err != goredis.Nil {
		if err != nil {
			return wrapRedis(err, "failed to verify snapshot row directory")
		}
		return errorf(adbc.StatusIO, "snapshot directory references a missing row")
	}
	return nil
}
