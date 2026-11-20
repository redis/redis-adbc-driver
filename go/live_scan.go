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

// liveCollectionScan owns one SCAN traversal, never a managed row directory.
// COUNT is a hint: pending retains one server reply, fetches are capped at 128
// keys/1 MiB requests, and seen grows with observed source keys. HASH reply sizes
// remain application controlled. No source command is automatically replayed.
type liveCollectionScan struct {
	store        *store
	meta         *tableMeta
	scan         *keyScan
	query        string
	pending      []string
	seen         map[string]bool
	done, closed bool
}

func (s *liveCollectionScan) close() {
	s.closed = true
	s.scan, s.pending, s.seen = nil, nil, nil
}

func (s *liveCollectionScan) validate(ctx context.Context) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := s.store.checkLiveIdentity(ctx, s.meta); err != nil {
		return err
	}
	return s.store.validateLiveIndex(ctx, s.meta)
}

func (s *liveCollectionScan) next(ctx context.Context) (keys []string, rows []aggRow, err error) {
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if s.closed {
		return nil, nil, io.EOF
	}
	if err := s.validate(ctx); err != nil {
		return nil, nil, err
	}
	if len(s.pending) == 0 {
		if s.done {
			return nil, nil, io.EOF
		}
		ks := s.scan
		if ks.node >= len(ks.nodes) {
			s.done = true
			return nil, nil, io.EOF
		}
		node, ok := ks.nodes[ks.node].(processor)
		if !ok {
			return nil, nil, errorf(adbc.StatusInternal, "live SCAN client cannot process commands")
		}
		cmd := goredis.NewScanCmd(ctx, nil, "SCAN", ks.cursor, "MATCH", ks.match, "COUNT", scanCount, "TYPE", "hash")
		if err := node.Process(ctx, unmanagedProjectionCmd{cmd}); err != nil {
			return nil, nil, wrapRedis(transportContextError(ctx, err), "live SCAN failed")
		}
		page, cursor, err := cmd.Result()
		if err != nil {
			return nil, nil, wrapRedis(err, "live SCAN failed")
		}
		ks.cursor = cursor
		if cursor == 0 {
			ks.node++
		}
		s.done = ks.node >= len(ks.nodes)
		for _, key := range page {
			if !strings.HasPrefix(key, s.meta.prefix()) {
				return nil, nil, errorf(adbc.StatusInvalidData, "live SCAN returned an out-of-scope key")
			}
			if !isRowKey(s.meta.prefix(), key) || s.seen[key] {
				continue
			}
			s.seen[key] = true
			s.pending = append(s.pending, key)
		}
	}
	end, bytes := 0, 0
	for end < len(s.pending) && end < 128 {
		size := len(s.pending[end]) + len(s.meta.index()) + len(s.query) + 128
		if size > 64<<20 {
			return nil, nil, errorf(adbc.StatusInvalidData, "live source key exceeds 64 MiB request limit")
		}
		if end > 0 && bytes+size > 1<<20 {
			break
		}
		bytes += size
		end++
	}
	keys, err = s.store.liveCandidates(ctx, s.meta, s.query, s.pending[:end])
	if err != nil {
		return nil, nil, err
	}
	s.pending = s.pending[end:]
	rows, err = (&unmanagedProjectionRead{store: s.store}).native(ctx, keys)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validate(ctx); err != nil {
		return nil, nil, err
	}
	return keys, rows, nil
}
