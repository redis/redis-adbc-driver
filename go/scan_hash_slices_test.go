// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func hashProjectionFixture() (*scanIter, *scanProjection) {
	it, items := projectionFixture()
	it.hashFetch = true
	it.fetch = []string{"id", "old_label", "qty", nullMarker("qty"), "stamp"}
	return it, newScanProjection(it, items)
}

func TestHashSliceProjectionCompatibility(t *testing.T) {
	_, p := hashProjectionFixture()
	if p == nil {
		t.Fatal("valid HASH projection was not bound")
	}
	for _, values := range [][]any{
		{"1", "9007199254740993", "a\x00b", nil, nil, "1234567890123456"},
		{"2", "-9223372036854775808", nil, nil, "1", nil},
		{"10", "9223372036854775807", "", nil, "", nil},
		{"11", "1", nil, nil, nil, nil},
		{"12", "1", "x", "-4", nil, nil},
		{nil, nil, nil, nil, nil, nil},
		{"3", "bad", "x", nil, nil, nil},
		{"bad", "1", nil, nil, nil, nil},
		{"4", "1", nil, nil, nil, "bad"},
		{"4", int64(1), []byte("ignored"), nil, false, nil},
	} {
		raw := aggRow{}
		if id, ok := values[0].(string); ok {
			raw[rowIDField] = id
		}
		for i, field := range p.hashFields {
			if value, ok := values[i+1].(string); ok {
				raw[field] = value
			}
		}
		got, want := make([]Value, p.width), make([]Value, p.width)
		gotErr, wantErr := p.decodeHashInto(values, got), p.decodeInto(raw, want)
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || !reflect.DeepEqual(got, want) {
			t.Fatalf("values=%v\ngot=%v %v\nwant=%v %v", values, got, gotErr, want, wantErr)
		}
	}
	for _, invalid := range [][]any{nil, {"1"}, make([]any, 7)} {
		if err := p.decodeHashInto(invalid, make([]Value, p.width)); !isStatus(err, adbc.StatusInternal) {
			t.Fatalf("invalid width returned %v", err)
		}
	}
	// Required columns still decode even when no output slot selects them.
	it, items := projectionFixture()
	it.hashFetch, it.fetch = true, []string{"id", "old_label", "qty", nullMarker("qty"), "stamp"}
	p = newScanProjection(it, items[:1])
	if err := p.decodeHashInto([]any{"1", "bad", "x", nil, nil, nil}, make([]Value, 1)); err == nil {
		t.Fatal("ignored malformed unselected required column")
	}
	// Incomplete bindings must fall back, never read the internal ID as data.
	it.fetch = it.fetch[1:]
	if newScanProjection(it, items) != nil {
		t.Fatal("incomplete HASH bindings were accepted")
	}
}

type hashSliceHook struct {
	t      *testing.T
	rows   map[string][]any
	calls  int
	fields []string
}

func (h *hashSliceHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *hashSliceHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *hashSliceHook) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		h.calls++
		for _, cmd := range cmds {
			args := cmd.Args()
			if args[0] != "hmget" || args[2] != rowIDField || len(args) != len(h.fields)+3 {
				h.t.Fatalf("unexpected command %v", args)
			}
			for i, field := range h.fields {
				if args[i+3] != field {
					h.t.Fatalf("field order differs: %v", args)
				}
			}
			cmd.(*goredis.SliceCmd).SetVal(h.rows[args[1].(string)])
		}
		return nil
	}
}

func TestHashSliceExistenceDiagnosticsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := goredis.NewClient(&goredis.Options{})
	defer client.Close()
	it, p := hashProjectionFixture()
	live := []any{"1", "9007199254740993", "a\x00b", nil, nil, nil}
	hook := &hashSliceHook{t: t, fields: it.fetch, rows: map[string][]any{"gone": make([]any, len(live)), "live": live}}
	client.AddHook(hook)
	it.store, it.env, it.projection = &store{client: client}, &evalEnv{ctx: ctx}, p
	it.diagnostics = &executionDiagnostics{}
	var decoded [][]Value
	p.consumeHash = func(values []any) error {
		row := make([]Value, p.width)
		if err := p.decodeHashInto(values, row); err != nil {
			return err
		}
		decoded = append(decoded, row)
		return nil
	}
	keys, rows, err := it.nextHashValues(ctx, []string{"gone", "", "live"})
	if err != nil || !reflect.DeepEqual(keys, []string{"live"}) || rows != nil || len(decoded) != 1 ||
		it.diagnostics.processed.Load() != 3 || it.diagnostics.matched.Load() != 1 {
		t.Fatalf("keys=%v rows=%v decoded=%d error=%v diagnostics=%+v", keys, rows, len(decoded), err, it.diagnostics)
	}
	// Changing the command reply's slots must not mutate decoded values.
	live[2] = "replacement"
	if decoded[0][0].S != "a\x00b" {
		t.Fatal("decoded string aliases a mutable reply slot")
	}
	hook.rows["malformed"] = []any{"1"}
	if _, err := it.store.fetchRowSlices(ctx, []string{"malformed"}, it.fetch, wideFetchChunk); !isStatus(err, adbc.StatusInternal) {
		t.Fatalf("malformed reply returned %v", err)
	}
	consumed := 0
	p.consumeHash = func([]any) error { consumed++; cancel(); return nil }
	if _, _, err := it.nextHashValues(ctx, []string{"live", "live"}); !isStatus(err, adbc.StatusCancelled) || consumed != 1 {
		t.Fatalf("cancel returned %v after %d rows", err, consumed)
	}
	calls := hook.calls
	if _, err := it.store.fetchRowSlices(ctx, []string{"live"}, it.fetch, wideFetchChunk); !isStatus(err, adbc.StatusCancelled) || hook.calls != calls {
		t.Fatalf("cancelled read queued commands: %v, calls=%d", err, hook.calls)
	}
}
