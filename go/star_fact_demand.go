// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"sort"
)

// Each prepared dimension supplies necessary independent key sets, not a join
// result. Cross-combinations may pass these filters; the original compound
// bucket probes still enforce correlation and duplicate multiplicity.
//
// limit caps each key set. A dimension proven unfiltered keeps maxUnionTerms:
// its keys exclude only fact rows the inner join drops anyway, yet with Flex
// each key costs a Search term (SSB Q2.x would push all 2,556 dates).
func starFactDimensionDemand(ctx context.Context, fact *tableMeta, dims []starDimension, limit int) (Expr, error) {
	var combined Expr
	for _, dim := range dims {
		if dim.buckets == nil {
			continue
		} // no proof of emptiness before preparation
		if len(dim.buckets) == 0 {
			return &Literal{V: boolValue(false)}, nil
		}
		dimLimit := limit
		if dim.unfiltered {
			dimLimit = min(limit, maxUnionTerms)
		}
		for pair, factName := range dim.factCols {
			if pair >= len(dim.dimCols) {
				continue
			}
			cm, ok := fact.column(factName)
			if !ok || !(cm.Type.Kind.isInteger() || cm.Type.Kind == KindDate) {
				continue
			}
			values := map[string]Value{}
			safe := true
		collect:
			for _, rows := range dim.buckets {
				for _, row := range rows {
					if err := cancelled(ctx); err != nil {
						return nil, err
					}
					v, present := row[dim.dimCols[pair]]
					if !present || v.T != cm.Type {
						safe = false
						break collect
					}
					if v.Null {
						continue
					}
					key, ok := joinKey(v)
					if !ok {
						safe = false
						break collect
					}
					values[key] = v
					if len(values) > dimLimit {
						safe = false
						break collect
					}
				}
			}
			if !safe {
				continue
			}
			if len(values) == 0 {
				return &Literal{V: boolValue(false)}, nil
			}
			// Stable native query construction despite randomized bucket-map order.
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			var demand Expr
			// literalInList identifies the shared left operand by pointer.
			// Keep one node so residual evaluation can build/reuse its set.
			ref := &ColumnRef{Name: factName}
			for _, key := range keys {
				term := &Binary{Op: "=", L: ref, R: &Literal{V: values[key]}}
				if demand == nil {
					demand = term
				} else {
					demand = &Binary{Op: "OR", L: demand, R: term}
				}
			}
			if or, ok := demand.(*Binary); ok && or.Op == "OR" {
				or.keyDemand = true
			}
			combined = andAll([]Expr{combined, demand})
		}
	}
	return combined, nil
}
