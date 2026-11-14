// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotRootPrefetchSharesProofAndStaysPinLocal(t *testing.T) {
	s, ctx, pin, h := snapshotLeaseProofFixture(t, false)
	if err := s.validateSnapshotRead(ctx); err != nil {
		t.Fatal(err)
	}
	want := []any{"EVAL_RO", snapshotRootPrefetchLua, 1, snapshotMetadataMap(pin.root)}
	if h.batches != 1 || h.direct != 0 || len(h.rootWires) != 1 || !reflect.DeepEqual(h.rootWires[0], want) {
		t.Fatal("prefetch not bounded same-slot native pipeline", h.batches, h.direct, h.rootWires)
	}
	if snapshotOf(ctx).metadata.prefetchedRoots == nil {
		t.Fatal("successful empty root not retained")
	}
	got, err := resolveSnapshotMetadata(ctx, "absent", false)
	if err != nil || got != snapshotPrefix+"absent:"+pin.root+":absent" || h.nativeRootReads != 0 {
		t.Fatal("resolver did not consume existing pin map", got, err, h.nativeRootReads)
	}
	fresh := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: pin.root, store: s, pin: pin})
	if _, err := resolveSnapshotMetadata(fresh, "absent", false); err != nil {
		t.Fatal(err)
	}
	if h.nativeRootReads != 1 {
		t.Fatal("another scope reused prefetch", h.nativeRootReads)
	}
}

func TestSnapshotRootPrefetchDeclinePreservesNativeRead(t *testing.T) {
	for _, message := range []string{"ADBC_ROOT_PREFETCH_DECLINED", "ERR ADBC_ROOT_PREFETCH_DECLINED", "NOPERM this user has no permissions to run the 'eval_ro' command", "NOPERM User guest has no permissions to run the 'eval_ro' command"} {
		t.Run(message, func(t *testing.T) {
			s, ctx, _, h := snapshotLeaseProofFixture(t, false)
			h.rootErr = controlScriptNativeError(message)
			if err := s.validateSnapshotRead(ctx); err != nil {
				t.Fatal(err)
			}
			if snapshotOf(ctx).metadata.prefetchedRoots != nil || h.direct != 0 {
				t.Fatal("decline installed/replayed prefetch")
			}
			if _, err := resolveSnapshotMetadata(ctx, "absent", false); err != nil {
				t.Fatal(err)
			}
			if h.nativeRootReads != 1 {
				t.Fatal("decline bypassed original HGETALL", h.nativeRootReads)
			}
		})
	}
}

func TestSnapshotRootPrefetchRejectsUnknownBeforeLeaseRepair(t *testing.T) {
	decline := controlScriptNativeError("ERR ADBC_ROOT_PREFETCH_DECLINED")
	missing := controlScriptNativeError("NOSCRIPT missing lease digest")
	for _, kind := range []string{"root_loss", "decline_plus_pipeline_loss", "joined_decline", "wrapped_decline", "digest_then_root_loss", "digest_then_malformed", "expired", "manifest_before_root_loss", "unaccounted_refusal", "lease_unknown", "expired_malformed", "expired_native_root", "expired_pipeline_loss", "terminal_lease_malformed", "unknown_lease_decline_first", "composed_lease_decline_first"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, _, h := snapshotLeaseProofFixture(t, false)
			want := "unexpected EOF"
			switch kind {
			case "root_loss":
				h.rootErr = io.ErrUnexpectedEOF
			case "decline_plus_pipeline_loss":
				h.rootErr = decline
				h.pipelineErr = io.ErrUnexpectedEOF
			case "joined_decline":
				h.rootErr = errors.Join(decline, io.ErrUnexpectedEOF)
			case "wrapped_decline":
				h.rootErr = fmt.Errorf("wrapped: %w", decline)
				want = "wrapped:"
			case "digest_then_root_loss":
				h.validErr = missing
				h.rootErr = io.ErrUnexpectedEOF
			case "digest_then_malformed":
				h.validErr = missing
				h.rootReply = int64(42)
				want = "invalid snapshot metadata root reply"
			case "expired":
				h.valid = 0
				want = "statement snapshot expired"
			case "manifest_before_root_loss":
				h.manifest = "{"
				h.rootErr = io.ErrUnexpectedEOF
				want = "invalid snapshot metadata manifest"
			case "unaccounted_refusal":
				h.pipelineErr = decline
				want = "ADBC_ROOT_PREFETCH_DECLINED"
			case "lease_unknown":
				h.validErr = io.ErrUnexpectedEOF
				h.rootErr = decline
			case "expired_malformed":
				h.valid = 0
				h.rootReply = int64(42)
				want = "statement snapshot expired"
			case "expired_native_root":
				h.valid = 0
				h.rootErr = controlScriptNativeError("ERR root command unavailable")
				want = "statement snapshot expired"
			case "expired_pipeline_loss":
				h.valid = 0
				h.pipelineErr = io.ErrUnexpectedEOF
			case "terminal_lease_malformed":
				h.validErr = controlScriptNativeError("ERR lease unavailable")
				h.rootReply = int64(42)
				want = "ERR lease unavailable"
			case "unknown_lease_decline_first":
				h.validErr = io.ErrUnexpectedEOF
				h.rootErr = decline
				h.pipelineErr = decline
			case "composed_lease_decline_first":
				h.validErr = errors.Join(missing, io.ErrUnexpectedEOF)
				h.rootErr = decline
				h.pipelineErr = decline
			}
			err := s.validateSnapshotRead(ctx)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal("unknown/invalid acquisition accepted or wrong diagnostic", kind, want, err)
			}
			if h.direct != 0 || snapshotOf(ctx).metadata.prefetchedRoots != nil {
				t.Fatal("failed proof repaired or installed root map", h.direct)
			}
		})
	}
}

func TestSnapshotRootPrefetchAllowsOnlyExactNativeDecline(t *testing.T) {
	for _, message := range []string{"ERR ADBC_ROOT_PREFETCH_DECLINED suffix", "prefix ERR ADBC_ROOT_PREFETCH_DECLINED", "ERR ERR ADBC_ROOT_PREFETCH_DECLINED", "NOPERM HMGET denied", "NOSCRIPT missing digest"} {
		if snapshotRootPrefetchDeclined(controlScriptNativeError(message)) {
			t.Fatal("unrelated native error selected fallback", message)
		}
	}
	if snapshotRootPrefetchDeclined(errors.New("ERR ADBC_ROOT_PREFETCH_DECLINED")) {
		t.Fatal("untyped error selected fallback")
	}
}

func TestSnapshotRootPrefetchShapeAndOriginalProofPrecedence(t *testing.T) {
	for _, kind := range []string{"odd", "duplicate", "nonstring", "count", "missing_map", "too_many", "map_before_lease"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, _, h := snapshotLeaseProofFixture(t, false)
			want := "invalid snapshot metadata root reply"
			switch kind {
			case "odd":
				h.rootReply = []any{"aa"}
			case "duplicate":
				h.manifest = `{"state":"committed","metadata_keys":0,"metadata_buckets":2}`
				h.rootCount = 2
				h.rootReply = []any{"aa", "owner", "aa", "owner"}
			case "nonstring":
				h.manifest = `{"state":"committed","metadata_keys":0,"metadata_buckets":1}`
				h.rootCount = 1
				h.rootReply = []any{"aa", int64(1)}
			case "count":
				h.rootReply = []any{"aa", "owner"}
			case "missing_map":
				h.manifest = `{"state":"committed","metadata_keys":0,"metadata_buckets":1}`
				h.rootReply = []any{}
				want = "snapshot metadata map is incomplete"
			case "too_many":
				h.rootReply = make([]any, 514)
			case "map_before_lease":
				h.manifest = `{"state":"committed","metadata_keys":0,"metadata_buckets":1}`
				h.valid = 0
				want = "snapshot metadata map is incomplete"
			}
			err := s.validateSnapshotRead(ctx)
			if err == nil || !strings.Contains(err.Error(), want) || h.direct != 0 || snapshotOf(ctx).metadata.prefetchedRoots != nil {
				t.Fatal("invalid metadata was used", kind, err, h.direct)
			}
		})
	}
}

func TestSnapshotRootPrefetchSkipsSharedAndUnrelatedScopes(t *testing.T) {
	for _, kind := range []string{"shared", "no_pin", "no_scope", "other_root", "other_store", "writing", "reader_pin", "restricted_store"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, pin, h := snapshotLeaseProofFixture(t, false)
			scope := snapshotOf(ctx)
			switch kind {
			case "shared":
				pin.shared = &sharedReadView{}
			case "no_pin":
				pin = nil
			case "no_scope":
				ctx = context.Background()
			case "other_root":
				scope.id = strings.Repeat("c", 32)
			case "other_store":
				scope.store = &store{}
			case "writing":
				scope.write = true
			case "reader_pin":
				pin.reader = true
			case "restricted_store":
				s.restrictedReader = true
			}
			pipe := s.client.Pipeline()
			queued, err := s.queueSnapshotRootPrefetch(ctx, strings.Repeat("b", 32), pin, pipe)
			if err != nil || queued != nil || pipe.Len() != 0 || h.batches != 0 || h.direct != 0 {
				t.Fatal("prefetch entered unsupported scope", kind)
			}
		})
	}
}

func TestSnapshotRootPrefetchRetainsExactNonemptyMap(t *testing.T) {
	s, ctx, pin, h := snapshotLeaseProofFixture(t, false)
	ownerA, ownerB := strings.Repeat("a", 32), strings.Repeat("c", 32)
	h.manifest = `{"state":"committed","metadata_keys":0,"metadata_buckets":2}`
	h.rootCount = 2
	h.rootReply = []any{"ab", ownerA, "cd", ownerB}
	if err := s.validateSnapshotRead(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ab": ownerA, "cd": ownerB}
	if !reflect.DeepEqual(snapshotOf(ctx).metadata.prefetchedRoots, want) {
		t.Fatal("root reply changed")
	}
	// Choose an absent bucket: resolving absence must consume the exact map
	// without a second HGETALL, and retain it in the existing resolver state.
	suffix := "absent"
	for snapshotMetadataSlot(suffix) == "ab" || snapshotMetadataSlot(suffix) == "cd" {
		suffix += "x"
	}
	got, err := resolveSnapshotMetadata(ctx, suffix, false)
	if err != nil || got != snapshotPrefix+"absent:"+pin.root+":"+suffix || h.nativeRootReads != 0 || !reflect.DeepEqual(snapshotOf(ctx).metadata.roots, want) {
		t.Fatal("prefetched map not consumed exactly", got, err, h.nativeRootReads)
	}
}

// A real pipeline queues commands without sending them. Preserve a local queue
// failure even if it did not populate the Cmd's error, and discard earlier work.
type snapshotRootQueueFailureClient struct {
	goredis.UniversalClient
	pipe *snapshotRootQueueFailurePipeline
}

func (c *snapshotRootQueueFailureClient) Pipeline() goredis.Pipeliner { return c.pipe }

type snapshotRootQueueFailurePipeline struct {
	goredis.Pipeliner
	execs, discards int
}

func (p *snapshotRootQueueFailurePipeline) Process(ctx context.Context, cmd goredis.Cmder) error {
	if len(cmd.Args()) > 1 && cmd.Args()[1] == snapshotRootPrefetchLua {
		return io.ErrUnexpectedEOF
	}
	return p.Pipeliner.Process(ctx, cmd)
}
func (p *snapshotRootQueueFailurePipeline) Exec(ctx context.Context) ([]goredis.Cmder, error) {
	p.execs++
	return p.Pipeliner.Exec(ctx)
}
func (p *snapshotRootQueueFailurePipeline) Discard() { p.discards++; p.Pipeliner.Discard() }
func TestSnapshotRootPrefetchQueueFailureStopsBeforeExec(t *testing.T) {
	s, ctx, _, h := snapshotLeaseProofFixture(t, false)
	pipe := &snapshotRootQueueFailurePipeline{Pipeliner: s.client.Pipeline()}
	s.client = &snapshotRootQueueFailureClient{UniversalClient: s.client, pipe: pipe}
	err := s.validateSnapshotRead(ctx)
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") || pipe.execs != 0 || pipe.discards != 1 || pipe.Len() != 0 || h.batches != 0 || h.direct != 0 || snapshotOf(ctx).metadata.prefetchedRoots != nil {
		t.Fatal("queue failure executed/lost or installed data", err, pipe.execs, pipe.discards, h.batches, h.direct)
	}
}
