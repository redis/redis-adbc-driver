// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type metadataHintReplies struct {
	native *descriptorReadReplies
	reply  []any
	err    error
	calls  int
	wires  [][]any
}

func (h *metadataHintReplies) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *metadataHintReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error {
		if c.Name() == "evalsha" || c.Name() == "eval" {
			h.calls++
			h.wires = append(h.wires, append([]any(nil), c.Args()...))
			if h.err != nil {
				c.SetErr(h.err)
				return h.err
			}
			c.(onceCmd).SetVal(h.reply)
			return nil
		}
		h.native.direct++
		return h.native.reply(c)
	}
}
func (h *metadataHintReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return h.native.ProcessPipelineHook(nil)
}
func metadataHintFixture(t *testing.T) (*store, context.Context, *metadataHintReplies, string) {
	t.Helper()
	n := &descriptorReadReplies{root: strings.Repeat("a", 32), owner: strings.Repeat("b", 32), valueOwner: strings.Repeat("c", 32), suffix: strings.TrimPrefix(metaKey("public", "target"), metaPrefix), value: "native payload", refs: "1", expected: "1", present: 1}
	h := &metadataHintReplies{native: n}
	h.reply = []any{"ready", []any{snapshotMetadataSlot(n.suffix), n.owner}, []any{"1", "1"}, int64(1), int64(1), []any{"1", "1"}, int64(1), "value", "exact descriptor"}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	client.AddHook(h)
	t.Cleanup(func() { _ = client.Close() })
	s := &store{client: client, snapshots: true}
	pin := &snapshotPin{ctx: context.Background(), store: s, root: n.root, token: strings.Repeat("d", 32), descriptorHint: true}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: n.root, pin: pin, store: s})
	s.metadataReadHints.put(n.suffix, snapshotMetadataReadHint{n.owner, n.valueOwner})
	return s, ctx, h, metaPrefix + n.suffix
}
func TestSnapshotMetadataHintCombinesFreshDeclaredKeyProofs(t *testing.T) {
	s, ctx, h, key := metadataHintFixture(t)
	value, err := s.readSnapshotMetadataString(ctx, key)
	if err != nil || value != "exact descriptor" {
		t.Fatal(value, err)
	}
	if h.calls != 1 || h.native.direct != 0 || h.native.proofs != 0 {
		t.Fatal("did not combine dependency chain", h.calls, h.native.direct, h.native.proofs)
	}
	args := h.wires[0]
	if args[2] != 5 {
		t.Fatal("keys undeclared", args[2])
	}
	want := []string{snapshotMetadataMap(h.native.root), snapshotMetadataGraph, snapshotMetadataBucket(h.native.owner, snapshotMetadataSlot(h.native.suffix)), snapshotMetadataDirectory(h.native.owner, snapshotMetadataSlot(h.native.suffix)), snapshotWorkspace(h.native.valueOwner) + h.native.suffix}
	for i, key := range want {
		if args[i+3] != key || !strings.Contains(key, "{meta}") {
			t.Fatal("wrong declared key", i, args[i+3])
		}
	}
	scope := snapshotOf(ctx)
	if scope.metadata.reads[h.native.suffix] != h.native.valueOwner || !scope.metadata.checked[want[4]] {
		t.Fatal("routing/dependency proof lost")
	}
	// A second same-pin load keeps the original single native GET and ACL check.
	value, err = s.readSnapshotMetadataString(ctx, key)
	if err != nil || value != h.native.value || h.native.gets != 1 || h.calls != 1 {
		t.Fatal("value cached or proof repeated", value, err, h.calls, h.native.gets)
	}
	fresh := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: scope.id, pin: scope.pin, store: s})
	_, err = s.readSnapshotMetadataString(fresh, key)
	if err != nil || h.calls != 2 {
		t.Fatal("new pin reused proof", err, h.calls)
	}
}
func TestSnapshotMetadataHintColdLearnsOnlyNativeOwners(t *testing.T) {
	s, ctx, h, key := metadataHintFixture(t)
	s.metadataReadHints = snapshotMetadataReadHints{}
	value, err := s.readSnapshotMetadataString(ctx, key)
	if err != nil || value != h.native.value || h.calls != 0 {
		t.Fatal(value, err, h.calls)
	}
	hint, ok := s.metadataReadHints.get(h.native.suffix)
	if !ok || hint != (snapshotMetadataReadHint{h.native.owner, h.native.valueOwner}) {
		t.Fatal("native successful owner hint missing")
	}
}
func TestSnapshotMetadataHintRefusalsAndUnknowns(t *testing.T) {
	for _, kind := range []string{"miss", "decline", "envelope_permission", "lost_reply", "composed_permission", "inner_permission", "badproof_permission", "oversized", "badproof_oversized"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, h, key := metadataHintFixture(t)
			fallback := false
			switch kind {
			case "miss", "decline":
				h.reply = []any{kind}
				fallback = true
			case "envelope_permission":
				h.err = controlScriptNativeError("NOPERM No permissions to access a key")
				fallback = true
			case "lost_reply":
				h.err = io.ErrUnexpectedEOF
			case "composed_permission":
				h.err = errors.Join(controlScriptNativeError("NOPERM No permissions to access a key"), io.ErrUnexpectedEOF)
			case "inner_permission":
				h.reply[7] = "error"
				h.reply[8] = "NOPERM GET refused"
			case "badproof_permission":
				h.reply[5] = []any{"1", "0"}
				h.reply[7] = "error"
				h.reply[8] = "NOPERM GET refused"
			case "oversized":
				h.reply[7] = "decline"
				h.reply[8] = nil
				fallback = true
			case "badproof_oversized":
				h.reply[5] = []any{"1", "0"}
				h.reply[7] = "decline"
				h.reply[8] = nil
			}
			value, err := s.readSnapshotMetadataString(ctx, key)
			if h.calls != 1 {
				t.Fatal("uncertain script replayed", h.calls)
			}
			if fallback {
				if err != nil || value != h.native.value || h.native.gets != 1 {
					t.Fatal("definite pre-GET refusal did not retain native path", value, err, h.native.gets)
				}
			} else {
				if err == nil || value != "" || h.native.gets != 0 {
					t.Fatal("failure exposed/replayed value", value, err, h.native.gets)
				}
				if strings.HasPrefix(kind, "badproof") && !strings.Contains(err.Error(), "missing or corrupt") {
					t.Fatal("GET error hid original proof failure", err)
				}
				if (kind == "lost_reply" || kind == "composed_permission") && !strings.Contains(err.Error(), "unexpected EOF") {
					t.Fatal("unknown outcome hidden", err)
				}
				if snapshotOf(ctx).metadata.checked[snapshotWorkspace(h.native.valueOwner)+h.native.suffix] {
					t.Fatal("failed proof cached")
				}
			}
		})
	}
}
func TestSnapshotMetadataHintRejectsMalformedReply(t *testing.T) {
	for _, kind := range []string{"truncated", "wrong_status", "odd_map", "duplicate_map", "bad_root", "bad_bucket", "bad_count", "bad_fields", "bad_value", "bad_reference", "wrong_exists", "large_value", "nil_value", "unknown_kind", "wrong_error"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, h, key := metadataHintFixture(t)
			switch kind {
			case "truncated":
				h.reply = h.reply[:8]
			case "wrong_status":
				h.reply[0] = int64(1)
			case "odd_map":
				h.reply[1] = []any{"01"}
			case "duplicate_map":
				h.reply[1] = []any{snapshotMetadataSlot(h.native.suffix), h.native.owner, snapshotMetadataSlot(h.native.suffix), h.native.owner}
			case "bad_root":
				h.reply[1] = []any{snapshotMetadataSlot(h.native.suffix), strings.Repeat("e", 32)}
			case "bad_bucket":
				h.reply[2] = []any{"1e0", "1"}
			case "bad_count":
				h.reply[3] = int64(2)
			case "bad_fields":
				h.reply[4] = int64(0)
			case "bad_value":
				h.reply[5] = []any{"0", "1"}
			case "bad_reference":
				h.reply[5] = []any{"1", "9223372036854775808"}
			case "wrong_exists":
				h.reply[6] = int64(0)
			case "large_value":
				h.reply[8] = strings.Repeat("x", snapshotMetadataHintValueBytes+1)
			case "nil_value":
				h.reply[8] = nil
			case "unknown_kind":
				h.reply[7] = "unknown"
			case "wrong_error":
				h.reply = []any{"native", int64(3)}
			}
			value, err := s.readSnapshotMetadataString(ctx, key)
			if err == nil || value != "" || h.calls != 1 || h.native.gets != 0 {
				t.Fatal("bad reply accepted/replayed", value, err, h.calls, h.native.gets)
			}
		})
	}
}
func TestSnapshotMetadataHintOnlyOrdinaryReadAcquisitions(t *testing.T) {
	for _, kind := range []string{"writer", "private", "shared", "restricted", "reader", "partition", "materialized", "retained", "maintenance", "other_store", "other_root", "long_suffix", "sequence"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, h, key := metadataHintFixture(t)
			scope := snapshotOf(ctx)
			switch kind {
			case "writer", "partition":
				scope.pin.descriptorHint = false
			case "private":
				scope.write = true
			case "shared":
				scope.pin.shared = &sharedReadView{}
			case "restricted":
				s.restrictedReader = true
			case "reader":
				scope.pin.reader = true
			case "materialized":
				scope.pin.materialized = true
			case "retained":
				scope.operation = true
			case "maintenance":
				scope.maintenance = true
			case "other_store":
				scope.pin.store = &store{}
			case "other_root":
				scope.pin.root = strings.Repeat("f", 32)
			case "long_suffix":
				key = metaPrefix + "table:" + strings.Repeat("x", 1024)
			case "sequence":
				key = seqKey("public", "target")
			}
			_, handled, err := s.readSnapshotMetadataHint(ctx, key)
			if handled || err != nil || h.calls != 0 {
				t.Fatal("unsafe context used hint", handled, err, h.calls)
			}
		})
	}
}
func TestSnapshotMetadataHintCacheBoundedAndThreadSafe(t *testing.T) {
	var cache snapshotMetadataReadHints
	hint := snapshotMetadataReadHint{strings.Repeat("a", 32), strings.Repeat("b", 32)}
	for i := range 256 {
		cache.put(fmt.Sprintf("table:%0100d", i), hint)
	}
	if len(cache.entries) > 64 || len(cache.order) > 64 || cache.bytes > snapshotMetadataHintBytes {
		t.Fatal("hint cache unbounded")
	}
	if _, ok := cache.get(fmt.Sprintf("table:%0100d", 0)); ok {
		t.Fatal("old routing hint retained")
	}
	before := cache.bytes
	cache.put(strings.Repeat("x", 1025), hint)
	cache.put("bad", snapshotMetadataReadHint{"invalid", hint.value})
	if cache.bytes != before {
		t.Fatal("unbounded/invalid address retained")
	}
	for i := range 8 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for j := range 128 {
				key := fmt.Sprintf("table:%d:%d", i, j)
				cache.put(key, hint)
				cache.get(key)
			}
		})
	}
}

func TestSnapshotMetadataHintTypedPermissionStatus(t *testing.T) {
	for _, kind := range []string{"exists", "hgetall", "get", "object_proof_before_get", "bad_outer_tag", "bad_get_tag", "untagged_native_acl"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, h, _ := metadataHintFixture(t)
			permission := true
			switch kind {
			case "exists", "hgetall":
				h.reply = []any{"permission", kind}
			case "get":
				h.reply[7] = "permission"
				h.reply[8] = "get"
			case "object_proof_before_get":
				h.reply[5] = []any{"1", "0"}
				h.reply[7] = "permission"
				h.reply[8] = "get"
				permission = false
			case "bad_outer_tag":
				h.reply = []any{"permission", "memory"}
				permission = false
			case "bad_get_tag":
				h.reply[7] = "permission"
				h.reply[8] = "exists"
				permission = false
			case "untagged_native_acl":
				h.reply = []any{"native", "ERR ACL failure in script: backend-specific text"}
				permission = false
			}
			value, err := s.getTableMetadata(ctx, "public", "target")
			if err == nil || value != nil || h.calls != 1 || h.native.gets != 0 {
				t.Fatal("permission reply accepted/replayed", value, err, h.calls, h.native.gets)
			}
			// Cold native backend errors pass through the existing public wrapRedis IO
			// classification. Cache warmness must not introduce a different SQLSTATE.
			if !isStatus(err, adbc.StatusIO) || sqlState(err) != "" {
				t.Fatal("public native compatibility changed", err, sqlState(err))
			}
			if permission && !strings.Contains(err.Error(), "NOPERM") {
				t.Fatal("typed native permission tag lost", err)
			}
			if kind == "object_proof_before_get" && !strings.Contains(err.Error(), "object is missing or corrupt") {
				t.Fatal("GET permission hid object corruption", err)
			}
		})
	}
}
