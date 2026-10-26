// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"math"
	"slices"
	"strconv"
)

const maxPartitionDirectIDs = 1024
const maxPartitionPruneNodes = 4096
const maxPartitionPruneDepth = 128

// Only exact typed integers are extracted. Do not reproduce the ordinary
// executor's coercions to infer bounds for floats, decimals, text or casts.
func partitionInteger(x Expr, params []Value, depth int) (int64, bool) {
	if depth > maxPartitionPruneDepth {
		return 0, false
	}
	var v Value
	switch x := x.(type) {
	case *Literal:
		if x.Unknown {
			return 0, false
		}
		v = x.V
	case *Param:
		if x.Index < 0 || x.Index >= len(params) {
			return 0, false
		}
		v = params[x.Index]
	case *Unary:
		n, ok := partitionInteger(x.X, params, depth+1)
		if !ok {
			return 0, false
		}
		if x.Op == "+" {
			return n, true
		}
		if x.Op == "-" && n != math.MinInt64 {
			return -n, true
		}
		return 0, false
	default:
		return 0, false
	}
	return v.I, !v.Null && v.T.Kind.isInteger()
}

// Unknown means no safe constraint. AND can retain its known conjunct; OR
// requires both sides. Sorted disjoint intervals bound scratch space. Even
// when budgets are exhausted, inference only widens the candidate set.
func partitionPredicateSpans(expr Expr, lo, hi int64, params []Value) ([]partitionSpan, bool) {
	if hi < lo {
		return []partitionSpan{}, true
	}
	budget := maxPartitionPruneNodes
	var visit func(Expr, int) ([]partitionSpan, bool)
	visit = func(expr Expr, depth int) ([]partitionSpan, bool) {
		if budget == 0 || depth > maxPartitionPruneDepth {
			return nil, false
		}
		budget--
		b, ok := expr.(*Binary)
		if !ok {
			return nil, false
		}
		if b.Op == "OR" {
			// IN is parsed as a left-deep OR chain. Flatten it iteratively so
			// supported bounded lists do not consume recursive stack per item.
			pending := []Expr{b.L, b.R}
			all := []partitionSpan{}
			for len(pending) > 0 {
				x := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				if branch, ok := x.(*Binary); ok && branch.Op == "OR" {
					if budget == 0 {
						return nil, false
					}
					budget--
					pending = append(pending, branch.L, branch.R)
					continue
				}
				spans, known := visit(x, depth+1)
				if !known {
					return nil, false
				}
				if len(all)+len(spans) > maxPartitionDirectIDs {
					return nil, false
				}
				all = append(all, spans...)
			}
			slices.SortFunc(all, func(a, b partitionSpan) int {
				if a.lo < b.lo {
					return -1
				}
				if a.lo > b.lo {
					return 1
				}
				return 0
			})
			out := []partitionSpan{}
			for _, span := range all {
				if len(out) > 0 && span.lo <= out[len(out)-1].hi+1 {
					out[len(out)-1].hi = max(out[len(out)-1].hi, span.hi)
				} else {
					out = append(out, span)
				}
			}
			return out, true
		}
		if b.Op == "AND" {
			a, ka := visit(b.L, depth+1)
			z, kz := visit(b.R, depth+1)
			if !ka {
				return z, kz
			}
			if !kz {
				return a, ka
			}
			out := []partitionSpan{}
			for i, j := 0, 0; i < len(a) && j < len(z); {
				from, to := max(a[i].lo, z[j].lo), min(a[i].hi, z[j].hi)
				if from <= to {
					out = append(out, partitionSpan{from, to})
					if len(out) > maxPartitionDirectIDs {
						return nil, false
					}
				}
				if a[i].hi < z[j].hi {
					i++
				} else {
					j++
				}
			}
			return out, true
		}
		op := b.Op
		if op != "=" && op != "<" && op != "<=" && op != ">" && op != ">=" {
			return nil, false
		}
		col, ok := b.L.(*ColumnRef)
		other := b.R
		if !ok || col.Name != rowIDField || col.Outer != 0 {
			col, ok = b.R.(*ColumnRef)
			other = b.L
			op = flipOp[op]
		}
		if !ok || col.Name != rowIDField || col.Outer != 0 {
			return nil, false
		}
		n, ok := partitionInteger(other, params, 0)
		if !ok {
			return nil, false
		}
		from, to := lo, hi
		switch op {
		case "=":
			from, to = max(lo, n), min(hi, n)
		case "<":
			if n == math.MinInt64 {
				return []partitionSpan{}, true
			}
			to = min(hi, n-1)
		case "<=":
			to = min(hi, n)
		case ">":
			if n == math.MaxInt64 {
				return []partitionSpan{}, true
			}
			from = max(lo, n+1)
		case ">=":
			from = max(lo, n)
		}
		if from > to {
			return []partitionSpan{}, true
		}
		return []partitionSpan{{from, to}}, true
	}
	return visit(expr, 0)
}

func partitionFiniteIDs(spans []partitionSpan) ([]int64, bool) {
	total := int64(0)
	for _, s := range spans {
		width := s.hi - s.lo + 1
		if width > maxPartitionDirectIDs-total {
			return nil, false
		}
		total += width
	}
	ids := make([]int64, 0, total)
	for _, s := range spans {
		for id := s.lo; id <= s.hi; id++ {
			ids = append(ids, id)
		}
	}
	return ids, true
}
func partitionPointSpans(ids []int64, n int) []partitionSpan {
	if len(ids) == 0 {
		return []partitionSpan{{1, 0}}
	}
	n = min(n, len(ids))
	out := make([]partitionSpan, n)
	for i := range out {
		out[i] = partitionSpan{ids[len(ids)*i/n], ids[len(ids)*(i+1)/n-1]}
	}
	return out
}
func partitionKeys(meta *tableMeta, ids []int64) []string {
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = meta.prefix() + strconv.FormatInt(id, 10)
	}
	return keys
}
