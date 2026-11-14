// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestUnmanagedProjectionReadSoftware(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	nul := string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(nul, 0) {
		t.Fatal("fixture lacks NUL")
	}
	for key, values := range map[string]map[string]any{
		"app:edge":     {"n": "9007199254740993", "label": nul, "__key": "application owned"},
		"app:all-null": {"unselected": "still a HASH"},
	} {
		if err := raw.HSet(h.ctx, key, values).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Set(h.ctx, "app:wrong", "string", 0).Err(); err != nil {
		t.Fatal(err)
	}
	// Exercise full-body read-only execution explicitly, even with a warm digest.
	if _, err := raw.Do(h.ctx, "EVAL_RO", unmanagedProjectionReadLua, 1, "app:edge", "n", "missing", "label").Result(); err != nil {
		t.Fatal("read-only script unsupported", err)
	}
	s := snapshotTestStore(t, raw)
	r := &unmanagedProjectionRead{store: s, fields: []string{"n", "missing", "label"}, bytes: len(unmanagedProjectionReadLua) + 128}
	got, err := r.read(h.ctx, []string{"app:edge", "app:all-null", "app:wrong", "app:gone"})
	want := []aggRow{{keyField: "app:edge", "n": "9007199254740993", "label": nul}, {keyField: "app:all-null"}, nil, nil}
	if err != nil || !reflect.DeepEqual(got, want) || r.fallback {
		t.Fatal("atomic projected source read differs", got, err, r.fallback)
	}
	if raw.HGet(h.ctx, "app:edge", "__key").Val() != "application owned" {
		t.Fatal("source read mutated application field")
	}
	// Source reads remain live on the next invocation; metadata/script caching
	// cannot retain values, existence or the application's old type.
	if err := raw.HSet(h.ctx, "app:edge", "n", "-9223372036854775808").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Del(h.ctx, "app:all-null").Err(); err != nil {
		t.Fatal(err)
	}
	got, err = r.read(h.ctx, []string{"app:edge", "app:all-null"})
	if err != nil || got[0]["n"] != "-9223372036854775808" || got[1] != nil {
		t.Fatal("source read retained old values", got, err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, err := r.read(ctx, []string{"app:edge"}); err == nil {
		t.Fatal("canceled projection succeeded")
	}
}
