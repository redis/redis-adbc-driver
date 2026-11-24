// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// A join's independently necessary source unions never replace its real
// equality probes/residuals. Ignore unsafe or over-cap pairs independently.
func (e *executor) nativeJoinLookups(ctx context.Context, env *evalEnv, pairs []equiPair, left []map[string]Value, view *tableMeta, wp wherePlan) (wherePlan, error) {
	for _, pair := range pairs {
		cm, ok := lookupColumn(view, pair.right)
		if !ok || !cm.Indexed || !simpleName(cm.field()) {
			continue
		}
		seen := map[string]bool{}
		var values []Value
		safe := true
		for _, row := range left {
			if err := cancelled(ctx); err != nil {
				return wp, err
			}
			env.row = row
			v, err := env.eval(pair.left)
			if err != nil {
				return wp, invalidArg(err)
			}
			if key, ok := joinKey(v); ok && !seen[key] {
				// Do not introduce a new proof for integer Search equality outside
				// exact-double range. The complete hash join remains the fallback.
				if cm.Type.Kind.isInteger() {
					if cv, err := coerceIn(v, cm.Type, e.zone()); err == nil && !cv.Null && (cv.I > exactDoubleLimit || cv.I < -exactDoubleLimit) {
						safe = false
						break
					}
				}
				seen[key] = true
				values = append(values, v)
				if len(values) > maxUnionTerms {
					safe = false
					break
				}
			}
		}
		if !safe {
			continue
		}
		q, ok := unionQuery(cm, values, e.zone())
		if !ok {
			continue
		}
		switch {
		case q == noMatchQuery:
			wp.none = true
			return wp, nil
		case wp.query == "*" || wp.query == "":
			wp.query = q
		default:
			wp.query += " " + q
		}
	}
	return wp, nil
}

func (e *executor) publishedJoinSource(ctx context.Context, m *tableMeta) bool {
	if ctx == nil {
		return false
	}
	scope := snapshotOf(ctx)
	if e.retained != nil || e.partitionDirectTable != nil || e.partitionForeignTable != "" || ctx.Value(retainedAllocationKey{}) != nil || ctx.Value(subBudgetKey{}) != nil || ctx.Value(snapshotDirectReadKey{}) != nil {
		return false
	}
	if m == nil || m.isMem || m.view != nil || m.lazy != nil || m.group != nil || m.join != nil || m.Live || m.Unmanaged || e.store == nil || !e.store.snapshots || scope == nil || scope.pin == nil || scope.store != e.store || scope.operation || scope.write || scope.maintenance || scope.pin.shared != nil || m.retainedSource || m.MaterializedSQL != "" || m.matviewRead != nil || !managedRowIDTable(m) || m.SnapshotRows == nil || len(m.PendingCleanup) != 0 || validateSnapshotDescriptor(ctx, m) != nil {
		return false
	}
	for _, seg := range m.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			return false
		}
	}
	return true
}
