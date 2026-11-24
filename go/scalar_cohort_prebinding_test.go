// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func prebindingReviewFixture() (*scalarCohort, *scalarCohortLane, *executor, Expr) {
	e := &executor{cache: newExecCache(), sess: newSession(defaultSchema)}
	meta := &tableMeta{Columns: []columnMeta{{Name: "a", Type: typeInt64}, {Name: "b", Type: typeInt32}, {Name: "metric", Field: "native_metric", Type: decimalType(18, 2), Missing: "7.50", MissingThrough: 10}}}
	predicate := &Binary{Op: "AND", L: filteredComparison(">=", "b", 10), R: filteredComparison(">=", "a", 1)}
	fn := &Func{Name: "AVG", Args: []Expr{&ColumnRef{Name: "metric"}}}
	typ, _ := inferType(fn, meta.types(), nil)
	lane := &scalarCohortLane{fn: fn, typ: typ, need: map[string]bool{"metric": true, "b": true, "a": true}, predicate: bindScanPredicate(predicate, meta.types(), nil, e.zone()), input: "metric"}
	c := &scalarCohort{meta: meta, columns: meta.Columns, lanes: []*scalarCohortLane{lane}}
	return c, lane, e, predicate
}

func TestScalarCohortPrebindingMetadataAndPredicateOrder(t *testing.T) {
	c, lane, _, _ := prebindingReviewFixture()
	if !c.bindLanes() {
		t.Fatal("valid lane bindings rejected")
	}
	if !reflect.DeepEqual(lane.columns, []int{0, 1, 2}) || !reflect.DeepEqual(lane.predicateSlots, []int{1, 0}) || lane.inputSlot != 2 {
		t.Fatalf("wrong metadata/predicate/input order: %v/%v/%d", lane.columns, lane.predicateSlots, lane.inputSlot)
	}
	// Registration-only labels/maps are not consulted again in the hot loop.
	// A bound lane can consume its frozen positional configuration independently.
	lane.need = nil
	lane.input = "not-a-runtime-lookup"
	lane.acc = newAccumulator(lane.fn)
	if err := c.input(aggRow{rowIDField: "1", "a": "2", "b": "11", "native_metric": "4.25"}, lane, make([]scalarCohortDecoded, len(c.columns))); err != nil || lane.acc.count != 1 {
		t.Fatalf("runtime fell back to string-map binding: count=%d error=%v", lane.acc.count, err)
	}
	count := &scalarCohortLane{fn: &Func{Name: "COUNT", Star: true}, need: map[string]bool{"a": true}}
	c.lanes = []*scalarCohortLane{count}
	if !c.bindLanes() || count.inputSlot != -1 {
		t.Fatal("COUNT acquired an AVG input")
	}
	count.acc = newAccumulator(count.fn)
	if err := c.input(aggRow{rowIDField: "1", "a": "2"}, count, make([]scalarCohortDecoded, len(c.columns))); err != nil || count.acc.count != 1 {
		t.Fatal("COUNT used invalid input slot", err)
	}
}

func TestScalarCohortPrebindingMatchesGenericDecodeAndFilter(t *testing.T) {
	for _, raw := range []aggRow{
		{rowIDField: "1", "a": "2", "b": "11", "native_metric": "4.25"},
		{rowIDField: "1", "a": "-1", "b": "11", "native_metric": "bad-rejected-metric"},
		{rowIDField: "1", "a": "bad-first", "b": "bad-second", "native_metric": "bad-third"},
		{rowIDField: "1", "a": "2", "native_metric": "4.25"},
		{rowIDField: "1", "a": "2", "b": "11"},
		{rowIDField: "1", "a": "2", "b": "11", nullMarker("native_metric"): "1"},
		{rowIDField: "1", "a": "9007199254740993", "b": "11", "native_metric": "9007199254740.93"},
	} {
		c, lane, e, predicate := prebindingReviewFixture()
		if !c.bindLanes() {
			t.Fatal("valid lane binding failed")
		}
		lane.acc = newAccumulator(lane.fn)
		gotErr := c.input(raw, lane, make([]scalarCohortDecoded, len(c.columns)))
		reference := newAccumulator(lane.fn)
		values, wantErr := decodeRow(c.meta, raw, lane.need)
		if wantErr == nil {
			env := e.newEnv(context.Background(), c.meta.types(), nil)
			env.row = values
			v, err := env.eval(predicate)
			wantErr = err
			if b, valid := truthy(v); err == nil && b && valid {
				reference.add(values["metric"])
			}
		}
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
			t.Fatalf("raw=%v error/order differs: %v want%v", raw, gotErr, wantErr)
		}
		if gotErr != nil {
			continue
		}
		got, err := lane.acc.result(e.newEnv(context.Background(), c.meta.types(), nil), lane.typ)
		if err != nil {
			t.Fatal(err)
		}
		want, err := reference.result(e.newEnv(context.Background(), c.meta.types(), nil), lane.typ)
		if err != nil {
			t.Fatal(err)
		}
		if got.T != want.T || got.Null != want.Null || !got.Null && got.Text() != want.Text() {
			t.Fatalf("bound value/type differs: %v/%s want%v/%s", got.T, cohortText(got), want.T, cohortText(want))
		}
	}
}

func TestScalarCohortPrebindingFailureIsAtomicBeforeExecution(t *testing.T) {
	for _, kind := range []string{"missing_needed", "missing_predicate", "missing_input", "duplicate_column", "unknown_aggregate"} {
		t.Run(kind, func(t *testing.T) {
			c, lane, _, _ := prebindingReviewFixture()
			untouched := &scalarCohortLane{fn: &Func{Name: "COUNT", Star: true}, need: map[string]bool{"a": true}, columns: []int{99}, predicateSlots: []int{98}, inputSlot: 97}
			c.lanes = append([]*scalarCohortLane{untouched}, c.lanes...)
			switch kind {
			case "missing_needed":
				lane.need["absent"] = true
			case "missing_predicate":
				lane.predicate.fields[0] = "absent"
			case "missing_input":
				lane.input = "absent"
			case "duplicate_column":
				c.columns = append(c.columns, c.columns[0])
			case "unknown_aggregate":
				lane.fn = &Func{Name: "SUM"}
			}
			if c.bindLanes() {
				t.Fatal("incomplete binding admitted")
			}
			if !reflect.DeepEqual(untouched.columns, []int{99}) || !reflect.DeepEqual(untouched.predicateSlots, []int{98}) || untouched.inputSlot != 97 {
				t.Fatal("failed cohort published partial lane bindings")
			}
			if c.started || c.scans != 0 {
				t.Fatal("binding failure started a source scan")
			}
		})
	}
}
