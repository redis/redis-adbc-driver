// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotSweepPassLimit = 256

func (c *hashConn) sweepSnapshots(ctx context.Context, opts SweepOptions, res *Sweep, progress func(string)) (*Sweep, error) {
	res.ManagedSnapshots = true
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	if info, err := c.client.Info(ctx, "server").Result(); err == nil {
		res.ServerVersion = infoField(info, "redis_version")
	}
	defer func() {
		sort.Slice(res.Prefixes, func(i, j int) bool { return res.Prefixes[i].Prefix < res.Prefixes[j].Prefix })
	}()
	if !opts.Apply {
		prefixes, more, err := c.snapshotSweepCandidates(ctx)
		res.More = more
		if err != nil {
			return res, err
		}
		for _, prefix := range prefixes {
			found, err := c.countSnapshotSweepRows(ctx, prefix)
			res.Prefixes = append(res.Prefixes, SweptPrefix{Prefix: prefix, Found: found})
			res.Released++
			res.Found += found
			if err != nil {
				res.More = true
				return res, err
			}
		}
		metadata, _, err := c.snapshotSweepPending(ctx)
		res.MetadataPending = metadata
		res.More = res.More || metadata
		return res, err
	}
	byPrefix := map[string]int{}
	progress("Reclaiming registered snapshot garbage; active reader and writer references remain protected")
	for range snapshotSweepPassLimit {
		step, err := c.st.reapSnapshotStep(ctx)
		if step.manifest {
			res.ManifestsRetired++
		}
		if step.prefix != "" {
			index, ok := byPrefix[step.prefix]
			if !ok {
				index = len(res.Prefixes)
				byPrefix[step.prefix] = index
				res.Prefixes = append(res.Prefixes, SweptPrefix{Prefix: step.prefix})
				res.Released++
			}
			// Count only confirmed row deletion replies, including partial progress.
			res.Prefixes[index].Found += step.deleted
			res.Prefixes[index].Deleted += step.deleted
			res.Found += step.deleted
			res.Deleted += step.deleted
		}
		if err != nil {
			res.More = true
			return res, wrapRedis(transportContextError(ctx, err), "snapshot sweep stopped")
		}
		metadata, data, err := c.snapshotSweepPending(ctx)
		if err != nil {
			res.More = true
			return res, err
		}
		res.MetadataPending = metadata
		res.More = metadata || data
		if !res.More {
			return res, nil
		}
	}
	return res, nil
}

func (c *hashConn) snapshotSweepPending(ctx context.Context) (metadata, data bool, err error) {
	root, err := c.st.snapshotControl(ctx, "peek", "", "", "").Text()
	if err != nil {
		return false, false, err
	}
	pipe := c.client.Pipeline()
	buckets := pipe.ZCard(ctx, snapshotMetadataBucketQueue)
	values := pipe.ZCard(ctx, snapshotMetadataValueQueue)
	rows := pipe.ZCard(ctx, snapshotDataQueue)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, false, err
	}
	return root != "" || buckets.Val() > 0 || values.Val() > 0, rows.Val() > 0, nil
}

// Preview only zero-reference ownership records already eligible for row GC.
// MetadataPending explicitly reports additional retirement work; preview never
// detaches references, drops indexes, or deletes row/metadata objects.
func (c *hashConn) snapshotSweepCandidates(ctx context.Context) ([]string, bool, error) {
	ids, err := c.client.ZRange(ctx, snapshotDataQueue, 0, snapshotSweepPassLimit).Result()
	if err != nil {
		return nil, false, err
	}
	more := len(ids) > snapshotSweepPassLimit
	if more {
		ids = ids[:snapshotSweepPassLimit]
	}
	var prefixes []string
	for _, id := range ids {
		if !snapshotMetadataID(id) {
			return nil, more, errorf(adbc.StatusIO, "invalid snapshot garbage identity")
		}
		proof, err := c.client.HMGet(ctx, snapshotMetadataGraph, "r:"+id, "d:"+id).Result()
		if err != nil {
			return nil, more, err
		}
		if proof[0] == nil && proof[1] == nil {
			if err := c.client.ZScore(ctx, snapshotDataQueue, id).Err(); err != nil {
				if err == goredis.Nil {
					continue
				}
				return nil, more, err
			}
			return nil, more, errorf(adbc.StatusIO, "snapshot garbage ownership missing")
		}
		refs, err := strconv.ParseInt(fmt.Sprint(proof[0]), 10, 64)
		var owner struct {
			Prefix string `json:"prefix"`
			State  string `json:"state"`
		}
		if err != nil || refs < 0 || json.Unmarshal([]byte(fmt.Sprint(proof[1])), &owner) != nil || snapshotDataID(owner.Prefix) != id || snapshotRowPrefix(owner.Prefix) != owner.Prefix || (owner.State != "live" && owner.State != "deleting") {
			return nil, more, errorf(adbc.StatusIO, "invalid snapshot garbage ownership")
		}
		if refs == 0 {
			prefixes = append(prefixes, owner.Prefix)
		}
	}
	return prefixes, more, nil
}

func (c *hashConn) countSnapshotSweepRows(ctx context.Context, prefix string) (int64, error) {
	var found int64
	err := c.st.walkSnapshotDirectory(ctx, snapshotMembers(snapshotDataID(prefix)), func(members []string) error {
		pipe := c.client.Pipeline()
		commands := make([]*goredis.StatusCmd, 0, len(members))
		for _, member := range members {
			n, err := strconv.ParseInt(member, 10, 64)
			if err != nil || n < 1 || len(member) != 20 {
				return errorf(adbc.StatusIO, "invalid snapshot row directory member")
			}
			commands = append(commands, pipe.Type(ctx, prefix+strconv.FormatInt(n, 10)))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		for _, cmd := range commands {
			switch cmd.Val() {
			case "hash":
				found++
			case "none":
			default:
				return errorf(adbc.StatusIO, "registered snapshot row has unexpected type")
			}
		}
		return nil
	})
	return found, err
}
