// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"strings"
	"testing"
)

func TestLiveEvolutionPlan(t *testing.T) {
	old := &tableMeta{Columns: []columnMeta{{Name: "n", Type: typeInt64, Nullable: true}}}
	inferred := &tableMeta{Columns: []columnMeta{{Name: "n", Type: typeInt64, Nullable: true}, {Name: "label", Type: typeString, Nullable: true}}}
	next, err := planLiveEvolution(old, inferred, []string{"label", "n"}, []string{"n"})
	if err != nil {
		t.Fatal(err)
	}
	if !next.Live || next.LiveVersion == "" || next.Columns[0].Name != "label" || next.Columns[0].Indexed || !next.Columns[1].Indexed {
		t.Fatalf("bad plan: %+v", next)
	}
	for _, names := range [][]string{nil, {"n", "n"}, {"__key"}, {"missing"}} {
		if _, err := planLiveEvolution(old, inferred, names, nil); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
	if _, err := planLiveEvolution(old, inferred, []string{"n"}, []string{"label"}); err == nil {
		t.Fatal("indexed removed column")
	}
	changed := *inferred
	changed.Columns = append([]columnMeta(nil), inferred.Columns...)
	changed.Columns[0].Type = typeString
	if _, err := planLiveEvolution(old, &changed, []string{"n"}, nil); err == nil || !strings.Contains(err.Error(), "cannot change") {
		t.Fatalf("accepted type rewrite: %v", err)
	}
}
