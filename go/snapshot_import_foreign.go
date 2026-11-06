// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Capture into the private control-namespace journal before producing any row
// HASHes. A broad application index therefore cannot ingest its own new copy.
func (s *store) snapshotForeignImportReader(ctx context.Context, source *store, meta *tableMeta) (func(context.Context) ([]map[string]Value, bool, error), error) {
	capture := &retainedCapture{store: source}
	topology, err := source.retainedTopology(ctx)
	if err != nil {
		return nil, err
	}
	info, err := capture.index(ctx, meta)
	if err != nil {
		return nil, err
	}
	count, err := source.countMatches(ctx, meta.index(), "*")
	if err != nil {
		return nil, err
	}
	expected := retainedCapturedSource{meta: meta, identity: retainedSourceIdentity(meta, info), topology: topology, rows: count, checkCount: true}
	prefixes := info.prefixes
	if len(prefixes) == 0 {
		prefixes = []string{""}
	}
	fields := []string{rowIDField}
	for _, col := range meta.Columns {
		fields = append(fields, col.field(), nullMarker(col.field()))
	}
	var scan *keyScan
	var pending []string
	prefix := 0
	var captured int64
	return func(ctx context.Context) ([]map[string]Value, bool, error) {
		for len(pending) == 0 {
			if scan == nil {
				if prefix >= len(prefixes) {
					if captured != count {
						return nil, false, errorf(adbc.StatusInvalidState, "foreign source membership changed or capture was incomplete")
					}
					if err := capture.check(ctx, expected); err != nil {
						return nil, false, err
					}
					return nil, true, nil
				}
				var err error
				scan, err = (&hashConn{client: source.client}).scanKeys(ctx, prefixes[prefix], "hash")
				if err != nil {
					return nil, false, err
				}
				scan.match = retainedByteGlob(prefixes[prefix])
			}
			keys, done, err := scan.next(ctx)
			if err != nil {
				return nil, false, err
			}
			for _, key := range keys {
				if !strings.HasPrefix(key, prefixes[prefix]) {
					return nil, false, errorf(adbc.StatusInvalidData, "foreign source SCAN returned an out-of-scope key")
				}
			}
			pending = keys
			if done {
				scan = nil
				prefix++
			}
		}
		keys := pending[:min(snapshotImportPage, len(pending))]
		pending = pending[len(keys):]
		keys, err := snapshotImportUnseen(ctx, keys)
		if err != nil {
			return nil, false, err
		}
		keys, err = capture.members(ctx, meta, keys)
		if err != nil {
			return nil, false, err
		}
		// Retained-query quotas remain unchanged. Managed import applies those
		// byte/row bounds independently to each source page, not the whole copy.
		capture.budget, capture.maxRows, capture.rows = newRetainedBudget(snapshotMaxBytes), snapshotImportPage, 0
		rows, err := capture.readBatch(ctx, meta, fields, keys)
		if err != nil {
			return nil, false, err
		}
		captured += int64(len(rows))
		return rows, false, nil
	}, nil
}
