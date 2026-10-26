// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"math"
	"reflect"
	"testing"
)

func TestPartitionPredicateBounds(t *testing.T) {
	for _, tc := range []struct {
		sql    string
		params []Value
		spans  []partitionSpan
		known  bool
	}{
		{"__rowid=42", nil, []partitionSpan{{42, 42}}, true},
		{"__rowid IN(9,2,2,3)", nil, []partitionSpan{{2, 3}, {9, 9}}, true},
		{"__rowid BETWEEN 10 AND 20", nil, []partitionSpan{{10, 20}}, true},
		{"__rowid > 95 AND __rowid < 100", nil, []partitionSpan{{96, 99}}, true},
		{"20 > __rowid", nil, []partitionSpan{{1, 19}}, true},
		{"__rowid <= 0", nil, []partitionSpan{}, true},
		{"__rowid=2 AND __rowid=3", nil, []partitionSpan{}, true},
		{"__rowid >= ?", []Value{intValue(typeInt64, 90)}, []partitionSpan{{90, 100}}, true},
		{"__rowid > ?", []Value{intValue(typeInt64, math.MaxInt64)}, []partitionSpan{}, true},
		{"__rowid < ?", []Value{intValue(typeInt64, math.MinInt64)}, []partitionSpan{}, true},
		{"__rowid = ?", []Value{floatValue(typeFloat64, 2)}, nil, false},
		{"__rowid = ?", []Value{nullValue(typeInt64)}, nil, false},
		{"__rowid='2'", nil, nil, false},
		{"id=2", nil, nil, false},
		{"__rowid=2 OR id=3", nil, nil, false},
		{"(__rowid=2 OR id=3) AND __rowid<10", nil, []partitionSpan{{1, 9}}, true},
		{"NOT (__rowid=2)", nil, nil, false},
		{"__rowid=2 AND id>5", nil, []partitionSpan{{2, 2}}, true},
		{"__rowid>=80 OR __rowid<3", nil, []partitionSpan{{1, 2}, {80, 100}}, true},
		{"__rowid=2.1", nil, nil, false},
		{"__rowid=CAST(2 AS BIGINT)", nil, nil, false},
		{"__rowid=2 OR __rowid=NULL", nil, nil, false},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			sel, err := partitionSelect("SELECT id FROM t WHERE " + tc.sql)
			if err != nil {
				t.Fatal(err)
			}
			spans, known := partitionPredicateSpans(sel.Where, 1, 100, tc.params)
			if known != tc.known || !reflect.DeepEqual(spans, tc.spans) {
				t.Fatalf("got %+v %v; expected %+v %v", spans, known, tc.spans, tc.known)
			}
		})
	}
}
func TestPartitionPredicateBudgets(t *testing.T) {
	var expr Expr
	for i := 1; i <= 1024; i++ {
		eq := &Binary{Op: "=", L: &ColumnRef{Name: rowIDField}, R: &Literal{V: intValue(typeInt64, int64(i*2))}}
		if expr == nil {
			expr = eq
		} else {
			expr = &Binary{Op: "OR", L: expr, R: eq}
		}
	}
	spans, known := partitionPredicateSpans(expr, 1, 10000, nil)
	ids, finite := partitionFiniteIDs(spans)
	if !known || !finite || len(ids) != 1024 {
		t.Fatal("bounded IN chain lost", known, finite, len(ids))
	}
	expr = &Binary{Op: "OR", L: expr, R: &Binary{Op: "=", L: &ColumnRef{Name: rowIDField}, R: &Literal{V: intValue(typeInt64, 9999)}}}
	if _, known := partitionPredicateSpans(expr, 1, 10000, nil); known {
		t.Fatal("oversized ID set should retain conservative path")
	}
	if _, finite := partitionFiniteIDs([]partitionSpan{{1, 1000000}}); finite {
		t.Fatal("enumerated an unbounded ID range")
	}
	if ids, ok := partitionFiniteIDs([]partitionSpan{{exactDoubleLimit - 2, exactDoubleLimit - 1}}); !ok || len(ids) != 2 || ids[1] != exactDoubleLimit-1 {
		t.Fatal(ids, ok)
	}
}
