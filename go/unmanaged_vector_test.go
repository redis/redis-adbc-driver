// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"reflect"
	"testing"
)

func TestUnmanagedNativeVectorMatchesCompleteImage(t *testing.T) {
	p := &scanProjection{keyRows: true, width: 4, key: []int{0}, columns: []projectionColumn{
		{column: columnMeta{Name: "n", Type: typeInt64}, field: "n", predicate: -1, outputs: []int{1}, decode: columnMeta{Type: typeInt64}.decode},
		{column: columnMeta{Name: "s", Type: typeString}, field: "s", predicate: -1, outputs: []int{2, 3}, decode: columnMeta{Type: typeString}.decode},
	}}
	if !p.bindUnmanagedInputs([]string{"n", "s"}) || p.bindUnmanagedInputs([]string{"s", "n"}) {
		t.Fatal("incorrect native field binding")
	}
	for _, values := range [][]any{{"9007199254740993", "a" + string([]byte{0}) + "b"}, {nil, ""}, {"9223372036854775807", nil}, {"bad", "valid"}} {
		raw := aggRow{keyField: "app:key"}
		for i, name := range []string{"n", "s"} {
			if values[i] != nil {
				raw[name] = values[i].(string)
			}
		}
		got := make([]Value, p.width)
		want := make([]Value, p.width)
		gotErr := p.decodeUnmanagedInto("app:key", values, got)
		wantErr := p.decodeInto(raw, want)
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || !reflect.DeepEqual(got, want) {
			t.Fatal(values, got, want, gotErr, wantErr)
		}
	}
	p.predicate = &scanPredicate{}
	if p.bindUnmanagedInputs([]string{"n", "s"}) {
		t.Fatal("residual predicate bypassed")
	}
}
