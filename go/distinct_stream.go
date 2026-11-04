// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type streamDistinctPlanKey struct{}

const maxStreamDistinctEntries = 65536
const maxStreamDistinctBytes = 16 << 20
const maxStreamDistinctRows = 2_000_000

type streamDistinct struct {
	seen        map[string]bool
	bytes, rows int
}

func (p *selectPlan) distinctStreamable() bool {
	if !p.distinct || len(p.distinctOn) > 0 || len(p.order) > 0 || !managedRowIDTable(p.meta) {
		return false
	}
	inner := *p
	inner.distinct = false
	// Reuse the strict row-local UNION leaf/type contract; subqueries and
	// unresolved/nested records cannot retain unbounded auxiliary state.
	return inner.unionStreamable()
}
func (d *streamDistinct) keep(row []Value) (bool, error) {
	d.rows++
	if d.rows > maxStreamDistinctRows {
		return false, codeStateErrorf(adbc.StatusInvalidState, "53200", "stream DISTINCT input work budget exhausted")
	}
	// Bound serialization before rowKey allocates, then charge its exact size.
	if batchRowBytes(row) > maxStreamDistinctBytes/2 {
		return false, codeStateErrorf(adbc.StatusInvalidState, "53200", "stream DISTINCT row exceeds key budget")
	}
	key := rowKey(row)
	if d.seen[key] {
		return false, nil
	}
	if len(d.seen) >= maxStreamDistinctEntries || len(key)+96 > maxStreamDistinctBytes-d.bytes {
		return false, codeStateErrorf(adbc.StatusInvalidState, "53200", "stream DISTINCT retained-state budget exhausted")
	}
	d.seen[key] = true
	d.bytes += len(key) + 96
	return true, nil
}

func (e *executor) admitDistinctScan(ctx context.Context, p *selectPlan, sc selectScan) error {
	if sc.req.where.none {
		return nil
	}
	if sc.req.where.keys != nil {
		if len(sc.req.where.keys) <= maxStreamDistinctRows {
			return nil
		}
	} else {
		last, err := e.store.redis(ctx).Get(ctx, seqKey(p.meta.Schema, p.meta.Name)).Int64()
		if errors.Is(err, goredis.Nil) {
			return nil
		}
		if err != nil {
			return wrapRedis(err, "stream DISTINCT source admission failed")
		}
		if last >= 0 && last <= maxStreamDistinctRows {
			return nil
		}
	}
	return codeStateErrorf(adbc.StatusInvalidState, "53200", "stream DISTINCT allocated source span exceeds work budget")
}
