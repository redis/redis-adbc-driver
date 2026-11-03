// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"testing"
)

func TestSnapshotSourceKeyIsNotPhysicalRowLookup(t *testing.T) {
	m := &tableMeta{Schema: "public", Name: "copied", KeyPrefix: snapshotRows(snapshotID()), SnapshotSourceKey: true}
	where := &Binary{Op: "=", L: &ColumnRef{Name: keyField}, R: &Literal{V: intValue(typeInt64, 123)}}
	e := &executor{schema: "public"}
	wp, err := e.planWhere(context.Background(), where, m, nil)
	if err != nil || wp.keys != nil || wp.residual == nil {
		t.Fatalf("source key became a physical row-ID lookup: %+v %v", wp, err)
	}
}

func TestSnapshotSourceKeyPositionalProjection(t *testing.T) {
	it, items := projectionFixture()
	it.meta.SnapshotSourceKey = true
	it.need[keyField] = true
	it.hashFetch = true
	it.fetch = []string{"id", "old_label", "qty", nullMarker("qty"), "stamp", snapshotSourceKeyField}
	items = append(items, planItem{expr: &ColumnRef{Name: keyField}}, planItem{expr: &ColumnRef{Name: keyField}})
	p := newScanProjection(it, items)
	if p == nil {
		t.Fatal("source-key positional projection was not bound")
	}
	values := []any{"7", "9007199254740993", "label", nil, nil, nil, "app:key\x00雪"}
	row := make([]Value, len(items))
	if err := p.decodeHashInto(values, row); err != nil {
		t.Fatal(err)
	}
	if row[1].I != 9007199254740993 || row[5].I != 7 || row[6].S != "app:key\x00雪" || row[7].S != row[6].S {
		t.Fatal("physical row identity and original key were confused", row)
	}
	values[len(values)-1] = nil
	if err := p.decodeHashInto(values, row); err != nil || !row[6].Null || !row[7].Null || row[5].I != 7 {
		t.Fatal("private row without provenance lost its independent row ID", row, err)
	}
}
