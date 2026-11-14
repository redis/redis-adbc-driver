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

func TestUnmanagedProjectionReadValueBoundSoftware(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	// Keep the regression modest in size: exceeding the fast-path limit proves
	// fallback without constructing the hundreds-of-MiB external value that
	// motivated the bound. None of these data are rejected or truncated.
	large := strings.Repeat("x", unmanagedProjectionReadBytes+1)
	part := strings.Repeat("p", unmanagedProjectionReadBytes/2)
	boundary := strings.Repeat("b", unmanagedProjectionReadBytes-16)
	if err := raw.HSet(h.ctx, "app:bounded", "large", large, "left", part, "right", part, "boundary", boundary, "small", "ok").Err(); err != nil {
		t.Fatal(err)
	}
	s := snapshotTestStore(t, raw)
	for _, fields := range [][]string{{"large"}, {"left", "right"}} {
		args := []any{"EVAL_RO", unmanagedProjectionReadLua, 1, "app:bounded"}
		for _, field := range fields {
			args = append(args, field)
		}
		if _, err := raw.Do(h.ctx, args...).Result(); !unmanagedProjectionTooLarge(err) {
			t.Fatal("native size guard did not decline before HMGET", fields, err)
		}
		r := &unmanagedProjectionRead{store: s, fields: fields, bytes: len(unmanagedProjectionReadLua) + 128, prepared: true}
		got, err := r.read(h.ctx, []string{"app:bounded"})
		if err != nil || len(got) != 1 || r.fallback {
			t.Fatal("large row was rejected or disabled later projection", err)
		}
		for _, field := range fields {
			if got[0][field] != raw.HGet(h.ctx, "app:bounded", field).Val() {
				t.Fatal("large field truncated", field)
			}
		}
	}
	for _, tc := range []struct{ field, value string }{{"boundary", boundary}, {"small", "ok"}, {"absent", ""}} {
		value, err := raw.Do(h.ctx, "EVAL_RO", unmanagedProjectionReadLua, 1, "app:bounded", tc.field).Result()
		if err != nil {
			t.Fatal("bounded selected value could not use script despite unselected large fields", tc.field, err)
		}
		values, ok := value.([]any)
		if !ok || len(values) != 1 {
			t.Fatal(value)
		}
		if tc.field == "absent" {
			if values[0] != nil {
				t.Fatal("missing field became nonNULL")
			}
		} else if values[0] != tc.value {
			t.Fatal("bounded field changed", tc.field)
		}
	}
}
