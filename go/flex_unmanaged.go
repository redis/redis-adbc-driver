// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"io"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Flex has no driver-owned row-ID directory for unmanaged data. Enumerate HASH
// candidates with SCAN, but let the application index decide membership using
// singleton INKEYS counts. This preserves FILTERs without cursors, offsets,
// sortable attributes or a server result cap. Deduplication retains one entry
// per observed key; source values remain live and do not acquire snapshots.
type flexUnmanagedScan struct {
	store          *store
	meta           *tableMeta
	scan           *keyScan
	prefix         int
	pending        []string
	seen           map[string]bool
	direct, closed bool
}

func (d *flexUnmanagedScan) close() {
	d.closed = true
	d.scan = nil
	d.pending = nil
	d.seen = nil
}

func (d *flexUnmanagedScan) next(ctx context.Context) (keys []string, rows []aggRow, err error) {
	defer func() {
		if err != nil {
			d.close()
		}
	}()
	if d.closed {
		return nil, nil, io.EOF
	}
	if err := cancelled(ctx); err != nil {
		return nil, nil, err
	}
	if err := d.store.checkKeys(ctx, d.meta, false); err != nil {
		return nil, nil, err
	}
	if err := d.store.checkUnmanagedIndex(ctx, d.meta); err != nil {
		return nil, nil, err
	}
	for len(d.pending) == 0 {
		if d.direct {
			return nil, nil, io.EOF
		}
		if d.scan == nil {
			if d.prefix >= len(d.meta.UnmanagedPrefixes) {
				return nil, nil, io.EOF
			}
			prefix := d.meta.UnmanagedPrefixes[d.prefix]
			d.scan, err = (&hashConn{client: d.store.client}).scanKeys(ctx, prefix, "hash")
			if err != nil {
				return nil, nil, err
			}
			d.scan.match = retainedByteGlob(prefix)
		}
		page, done, err := d.scan.next(ctx)
		if err != nil {
			return nil, nil, err
		}
		prefix := d.meta.UnmanagedPrefixes[d.prefix]
		for _, key := range page {
			if !strings.HasPrefix(key, prefix) || !unmanagedSourceKey(key) {
				return nil, nil, errorf(adbc.StatusInvalidData, "unmanaged source SCAN returned an out-of-scope key")
			}
			if d.seen[key] {
				continue
			}
			d.seen[key] = true
			if !d.meta.UnmanagedForeign && !isRowKey(d.meta.prefix(), key) {
				continue
			}
			d.pending = append(d.pending, key)
		}
		if done {
			d.scan = nil
			d.prefix++
		}
	}
	// Bound both command count and request bytes. Redis SCAN COUNT is a hint;
	// consume a potentially larger scan reply in small membership/fetch batches.
	end, bytes := 0, 0
	for end < len(d.pending) && end < 128 {
		size := len(d.pending[end]) + len(d.meta.index()) + 128
		if size > 64<<20 {
			return nil, nil, errorf(adbc.StatusInvalidData, "unmanaged source key exceeds 64 MiB request limit")
		}
		if end > 0 && bytes+size > 1<<20 {
			break
		}
		bytes += size
		end++
	}
	keys, err = d.store.flexUnmanagedMembers(ctx, d.meta, d.pending[:end])
	if err != nil {
		return nil, nil, err
	}
	d.pending = d.pending[end:]
	rows, err = d.store.fetchKeyRows(ctx, keys, nil)
	if err != nil {
		return nil, nil, err
	}
	// No page may escape after the source definition or catalog lease failed.
	if err := d.store.checkUnmanagedIndex(ctx, d.meta); err != nil {
		return nil, nil, err
	}
	if err := d.store.checkKeys(ctx, d.meta, false); err != nil {
		return nil, nil, err
	}
	return keys, rows, nil
}

func (s *store) flexUnmanagedMembers(ctx context.Context, meta *tableMeta, keys []string) ([]string, error) {
	node, err := s.search(ctx, meta.index())
	if err != nil {
		return nil, err
	}
	client, ok := node.c.(interface{ Pipeline() goredis.Pipeliner })
	if !ok {
		return nil, errorf(adbc.StatusIO, "unmanaged membership requires a pipeline-capable Search client")
	}
	pipe := client.Pipeline()
	commands := make([]*goredis.Cmd, len(keys))
	for i, key := range keys {
		if !unmanagedSourceKey(key) {
			return nil, errorf(adbc.StatusInvalidArgument, "unmanaged row overlaps driver storage")
		}
		commands[i] = goredis.NewCmd(ctx, "FT.SEARCH", meta.index(), "*", "INKEYS", 1, key, "LIMIT", 0, 0, "TIMEOUT", 0, "DIALECT", 2)
		_ = pipe.Process(ctx, onceCmd{commands[i]})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, wrapRedis(err, "Flex unmanaged membership check failed")
	}
	var members []string
	for i, command := range commands {
		reply, err := command.Result()
		if err != nil {
			return nil, wrapRedis(err, "Flex unmanaged membership check failed")
		}
		if snapshotReplyWarning(reply) {
			return nil, errorf(adbc.StatusIO, "Flex unmanaged membership returned a partial-result warning")
		}
		n, err := parseSearchCount(reply)
		if err != nil {
			return nil, err
		}
		switch n {
		case 0:
		case 1:
			members = append(members, keys[i])
		default:
			return nil, errorf(adbc.StatusIO, "invalid Flex unmanaged singleton membership count")
		}
	}
	return members, nil
}
