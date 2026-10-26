// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"errors"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// rowIDRangeScan describes an internal input source, not an ADBC descriptor.
// Ordinary execution applies its global operators/limits outside this source.
type rowIDRangeScan struct{ lo, hi, cap, configured int64 }

// Cursor COUNT bounds reply pages, not the rows Search sorts before its first
// reply. Bound each row-ID query independently even on servers with an
// unrestricted result cap, so a streamed scan cannot sort the whole table.
const maxScanRangeRows int64 = 65536

// Restrict replacement of the HASH fallback to row-local plans whose required
// output order is ascending internal row ID. Other sorts and layouts retain the
// complete ordinary path; never apply a global sort or LIMIT per range.
func (e *executor) boundedOrdinaryScan(ctx context.Context, plan *selectPlan, cachedCap int64) (*rowIDRangeScan, error) {
	m := plan.meta
	if cachedCap < 1 || !plan.streamable() || !managedRowIDTable(m) {
		return nil, nil
	}
	if len(plan.order) > 0 {
		if len(plan.order) != 1 {
			return nil, nil
		}
		col, ok := plan.order[0].expr.(*ColumnRef)
		if !ok || col.Name != rowIDField || plan.order[0].desc {
			return nil, nil
		}
	}
	cap, err := e.store.partitionScanCap(ctx, m.index(), 0)
	if err != nil {
		var ae adbc.Error
		if errors.As(err, &ae) && ae.Code == adbc.StatusNotImplemented {
			return nil, nil
		}
		return nil, err
	}
	if err := e.store.checkRangeReadiness(ctx, m.index()); err != nil {
		var ae adbc.Error
		if errors.As(err, &ae) && (ae.Code == adbc.StatusInvalidState || ae.Code == adbc.StatusNotImplemented || ae.Code == adbc.StatusUnauthorized) {
			return nil, nil // retain the complete ordinary HASH fallback
		}
		return nil, err
	}
	cap = min(cap, cachedCap, maxScanRangeRows)
	last, err := e.store.redis(ctx).Get(ctx, seqKey(m.Schema, m.Name)).Int64()
	if errors.Is(err, goredis.Nil) {
		last, err = 0, nil
	}
	if err != nil {
		return nil, wrapRedis(err, "reading scan row sequence failed")
	}
	if last < 0 || last >= exactDoubleLimit || last > 0 && (last-1)/cap+1 > maxPartitionSubranges {
		return nil, nil
	}
	return &rowIDRangeScan{lo: 1, hi: last, cap: cap}, nil
}

func managedRowIDTable(m *tableMeta) bool {
	return m != nil && !m.isMem && !isTempSchema(m.Schema) && !isTempAlias(m.Schema) && m.Schema != foreignSchema &&
		!m.Adopted && !m.KeyRows && !m.Foreign && !m.AppIndex && m.RekeyTo == "" && !m.Rewriting && m.KeyPrefix != ""
}

// newScanReader owns the cursor/context and Arrow setup shared by both public
// APIs. Attach stream owners only once ordinary execution's first fill proves
// that a live reader must be returned; partition reads attach immediately.
func (e *executor) newScanReader(ctx context.Context, plan *selectPlan, req scanRequest, params []Value, so *streamOpts) (*streamReader, error) {
	cols := so.cols
	if cols == nil {
		cols = plan.columns()
	}
	sctx, cancel := context.WithCancelCause(ctx)
	it, err := e.openScan(sctx, req, params)
	if err != nil {
		cancel(nil)
		return nil, err
	}
	it.projection = newScanProjection(it, plan.items)
	r := &streamReader{ctx: sctx, cancel: cancel, exec: e, cache: e.cache, items: plan.items, it: it,
		env: e.newEnv(sctx, plan.meta.types(), params), zone: e.zone(), cols: cols, schema: resultSchema(cols), mem: so.mem, batchRows: so.batchRows, batchBytes: so.batchBytes, left: -1}
	r.refs.Store(1)
	if it.projection != nil {
		r.arrow = &scanArrow{scratch: make([]Value, len(cols))}
	}
	return r, nil
}

func (r *streamReader) attachOwners(owners []*streams) {
	r.owners = owners
	for _, owner := range owners {
		owner.add(r)
	}
}
