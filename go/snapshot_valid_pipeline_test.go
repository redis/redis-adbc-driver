// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotLeaseProofReplies struct {
	batches, direct       int
	wires                 [][]any
	valid                 int64
	validErr, pipelineErr error
	manifest              string
}

func (h *snapshotLeaseProofReplies) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *snapshotLeaseProofReplies) reply(cmd goredis.Cmder) error {
	switch c := cmd.(type) {
	case *goredis.StringCmd:
		c.SetVal(h.manifest)
	case *goredis.IntCmd:
		c.SetVal(0)
	case onceCmd:
		if !c.NoRetry() {
			panic("lease can retry")
		}
		h.wires = append(h.wires, append([]any(nil), c.Args()...))
		if c.Args()[0] == "EVALSHA" && h.validErr != nil {
			c.SetErr(h.validErr)
			return h.validErr
		}
		c.SetVal(h.valid)
	default:
		panic("unexpected lease proof reply type")
	}
	return nil
}
func (h *snapshotLeaseProofReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error { h.direct++; return h.reply(c) }
}
func (h *snapshotLeaseProofReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		h.batches++
		var first error
		for _, c := range cs {
			if err := h.reply(c); first == nil {
				first = err
			}
		}
		if h.pipelineErr != nil {
			return h.pipelineErr
		}
		return first
	}
}
func snapshotLeaseProofFixture(t *testing.T, reader bool) (*store, context.Context, *snapshotPin, *snapshotLeaseProofReplies) {
	t.Helper()
	h := &snapshotLeaseProofReplies{valid: 1, manifest: `{"state":"committed","metadata_keys":0,"metadata_buckets":0}`}
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	c.AddHook(h)
	t.Cleanup(func() { _ = c.Close() })
	s := &store{client: c, snapshots: true, restrictedReader: reader, readerNamespace: snapshotReaderNamespace("unit")}
	pin := &snapshotPin{store: s, token: strings.Repeat("a", 32), root: strings.Repeat("b", 32)}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: pin.root, store: s, pin: pin})
	return s, ctx, pin, h
}
func TestSnapshotAcquireProofQueuesUnchangedLeaseCommand(t *testing.T) {
	for _, reader := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "reader"}[reader], func(t *testing.T) {
			s, ctx, pin, h := snapshotLeaseProofFixture(t, reader)
			if err := pin.control(ctx, "valid", "").Err(); err != nil {
				t.Fatal(err)
			}
			baseline := h.wires[0]
			h.wires = nil
			h.direct = 0
			if err := s.validateSnapshotRead(ctx); err != nil {
				t.Fatal(err)
			}
			if h.batches != 1 || h.direct != 0 || len(h.wires) != 1 {
				t.Fatal("proof did not share one pipeline", h.batches, h.direct, len(h.wires))
			}
			if !reflect.DeepEqual(baseline, h.wires[0]) {
				t.Fatal("queued validation changed keys/arguments")
			}
		})
	}
}
func TestSnapshotAcquireProofRejectsUncertainOrExpiredLease(t *testing.T) {
	for _, kind := range []string{"expired", "reply_loss", "digest_plus_loss", "local_loss", "manifest_before_digest"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, pin, h := snapshotLeaseProofFixture(t, false)
			want := "statement snapshot expired"
			switch kind {
			case "expired":
				h.valid = 0
			case "reply_loss":
				h.pipelineErr = io.ErrUnexpectedEOF
				want = "unexpected EOF"
			case "digest_plus_loss":
				h.validErr = controlScriptNativeError("NOSCRIPT missing digest")
				h.pipelineErr = io.ErrUnexpectedEOF
				want = "unexpected EOF"
			case "local_loss":
				pin.err = io.ErrUnexpectedEOF
				want = "unexpected EOF"
			case "manifest_before_digest":
				h.manifest = "{"
				h.validErr = controlScriptNativeError("NOSCRIPT missing digest")
				want = "invalid snapshot metadata manifest"
			}
			err := s.validateSnapshotRead(ctx)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal("invalid lease proof accepted or original error hidden", err)
			}
			if h.direct != 0 {
				t.Fatal("uncertain/refused proof replayed", h.direct)
			}
			if kind == "local_loss" && h.batches != 0 {
				t.Fatal("lost local lease reached Redis")
			}
		})
	}
}
func TestSnapshotAcquireProofFallsBackOnlyForDefiniteDigestRefusal(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "denied"}[denied], func(t *testing.T) {
			s, ctx, _, h := snapshotLeaseProofFixture(t, false)
			h.validErr = controlScriptNativeError("NOSCRIPT missing digest")
			if denied {
				h.validErr = controlScriptNativeError("NOPERM this user has no permissions to run the 'evalsha' command")
			}
			if err := s.validateSnapshotRead(ctx); err != nil {
				t.Fatal(err)
			}
			if h.batches != 1 || h.direct != 1 || len(h.wires) != 2 || h.wires[0][0] != "EVALSHA" || h.wires[1][0] != "EVAL" {
				t.Fatal("wrong definite-refusal fallback", h.batches, h.direct, len(h.wires))
			}
			before, after := h.wires[0], h.wires[1]
			if !reflect.DeepEqual(before[2:], after[2:]) || after[1] != snapshotControl {
				t.Fatal("fallback changed native lease proof")
			}
			if denied && !s.controlScript.evalOnly.Load() {
				t.Fatal("native command denial not retained")
			}
		})
	}
}
