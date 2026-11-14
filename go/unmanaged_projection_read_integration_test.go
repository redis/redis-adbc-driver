// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
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
	s := snapshotTestStore(t, raw)
	r := &unmanagedProjectionRead{store: s, fields: []string{"n", "missing", "label"}, bytes: 128}
	got, err := r.read(h.ctx, []string{"app:edge", "app:all-null", "app:wrong", "app:gone"})
	want := []aggRow{{keyField: "app:edge", "n": "9007199254740993", "label": nul}, {keyField: "app:all-null", "unselected": "still a HASH"}, nil, nil}
	if err != nil || !reflect.DeepEqual(got, want) || r.fallback {
		t.Fatal("atomic projected source read differs", got, err, r.fallback)
	}
	if raw.HGet(h.ctx, "app:edge", "__key").Val() != "application owned" {
		t.Fatal("source read mutated application field")
	}
	// Source reads remain live on the next invocation; metadata caching
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
	// Native HMGET has no Lua materialization ceiling: selected values above
	// the retired script limit are read without truncation or a Lua copy.
	large := strings.Repeat("x", unmanagedProjectionReadBytes+1)
	part := strings.Repeat("p", unmanagedProjectionReadBytes/2)
	boundary := strings.Repeat("b", unmanagedProjectionReadBytes-16)
	if err := raw.HSet(h.ctx, "app:bounded", "large", large, "left", part, "right", part, "boundary", boundary, "small", "ok").Err(); err != nil {
		t.Fatal(err)
	}
	s := snapshotTestStore(t, raw)
	for _, fields := range [][]string{{"large"}, {"left", "right"}, {"boundary"}, {"small"}, {"absent"}} {
		r := &unmanagedProjectionRead{store: s, fields: fields, bytes: 128, prepared: true}
		got, err := r.read(h.ctx, []string{"app:bounded"})
		if err != nil || len(got) != 1 || r.fallback {
			t.Fatal("native value read failed", err)
		}
		for _, field := range fields {
			value, err := raw.HGet(h.ctx, "app:bounded", field).Result()
			if field == "absent" {
				if _, ok := got[0][field]; ok {
					t.Fatal("absent field became non-NULL")
				}
				continue
			}
			if err != nil || got[0][field] != value {
				t.Fatal("native large value truncated", field, err)
			}
		}
	}
}

type unmanagedProjectionAfterNativeHook struct {
	after func()
	done  bool
}

func (*unmanagedProjectionAfterNativeHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}
func (*unmanagedProjectionAfterNativeHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *unmanagedProjectionAfterNativeHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		if !h.done && len(cmds) > 0 && cmds[0].Name() == "hmget" {
			h.done = true
			h.after()
		}
		return err
	}
}
func TestUnmanagedProjectionReadLiveChangeBeforeAllNullFallbackSoftware(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	for _, mode := range []string{"updated", "deleted", "wrongtype", "still_null"} {
		t.Run(mode, func(t *testing.T) {
			key := "app:temporal_" + mode
			if err := raw.HSet(h.ctx, key, "other", "keeps HASH alive").Err(); err != nil {
				t.Fatal(err)
			}
			hook := &unmanagedProjectionAfterNativeHook{after: func() {
				switch mode {
				case "updated":
					if err := raw.HSet(h.ctx, key, "n", "9007199254740993", "text", string([]byte{'n', 0, 'w'})).Err(); err != nil {
						t.Fatal(err)
					}
				case "deleted":
					if err := raw.Del(h.ctx, key).Err(); err != nil {
						t.Fatal(err)
					}
				case "wrongtype":
					if err := raw.Del(h.ctx, key).Err(); err != nil {
						t.Fatal(err)
					}
					if err := raw.Set(h.ctx, key, "now string", 0).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}}
			readerRaw := h.rawClient(hook)
			r := &unmanagedProjectionRead{store: &store{client: readerRaw, snapshots: true}, fields: []string{"n", "text"}, bytes: 128}
			got, err := r.read(h.ctx, []string{key})
			if err != nil || len(got) != 1 || !hook.done {
				t.Fatal(got, err, hook.done)
			}
			switch mode {
			case "updated":
				if got[0]["n"] != "9007199254740993" || got[0]["text"] != string([]byte{'n', 0, 'w'}) || got[0]["other"] != "keeps HASH alive" {
					t.Fatal("fallback mixed row images", got)
				}
			case "deleted", "wrongtype":
				if got[0] != nil {
					t.Fatal(got)
				}
			case "still_null":
				if got[0][keyField] != key || got[0]["other"] != "keeps HASH alive" {
					t.Fatal(got)
				}
			}
		})
	}
}
