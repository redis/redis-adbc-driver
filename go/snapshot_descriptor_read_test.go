// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type descriptorReadReplies struct {
	expected                               string
	present                                int64
	root, owner, valueOwner, suffix, value string
	direct, proofs, gets                   int
	refs                                   string
	getErr, pipelineErr                    error
}

func (h *descriptorReadReplies) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *descriptorReadReplies) reply(cmd goredis.Cmder) error {
	switch c := cmd.(type) {
	case *goredis.MapStringStringCmd:
		c.SetVal(map[string]string{snapshotMetadataSlot(h.suffix): h.owner})
	case *goredis.SliceCmd:
		first := fmt.Sprint(c.Args()[2])
		if strings.HasPrefix(first, "n:") {
			c.SetVal([]any{"1", "1"})
		} else {
			c.SetVal([]any{h.expected, h.refs})
		}
	case *goredis.IntCmd:
		if c.Name() == "exists" {
			c.SetVal(h.present)
		} else {
			c.SetVal(1)
		}
	case *goredis.StringCmd:
		if c.Name() == "hget" {
			c.SetVal(h.valueOwner)
		} else {
			h.gets++
			if h.getErr != nil {
				c.SetErr(h.getErr)
				return h.getErr
			}
			c.SetVal(h.value)
		}
	default:
		return fmt.Errorf("unexpected descriptor proof command %T", cmd)
	}
	return nil
}
func (h *descriptorReadReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error { h.direct++; return h.reply(c) }
}
func (h *descriptorReadReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		h.proofs++
		var first error
		valueProof := false
		for _, c := range cs {
			if c.Name() == "get" {
				valueProof = true
			}
			if err := h.reply(c); first == nil {
				first = err
			}
		}
		if valueProof && h.pipelineErr != nil {
			return h.pipelineErr
		}
		return first
	}
}
func descriptorReadFixture(t *testing.T) (*store, context.Context, *descriptorReadReplies, string) {
	t.Helper()
	h := &descriptorReadReplies{root: strings.Repeat("a", 32), owner: strings.Repeat("b", 32), valueOwner: strings.Repeat("c", 32), suffix: strings.TrimPrefix(metaKey("public", "target"), metaPrefix), value: "exact descriptor payload", refs: "1", expected: "1", present: 1}
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	c.AddHook(h)
	t.Cleanup(func() { _ = c.Close() })
	s := &store{client: c, snapshots: true}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: h.root, store: s})
	return s, ctx, h, metaPrefix + h.suffix
}
func TestSnapshotDescriptorReadSharesNativePresenceProof(t *testing.T) {
	s, ctx, h, key := descriptorReadFixture(t)
	got, err := s.readSnapshotMetadataString(ctx, key)
	if err != nil || got != h.value {
		t.Fatal(got, err)
	}
	if h.direct != 1 || h.proofs != 2 || h.gets != 1 {
		t.Fatalf("direct=%d proofpipelines=%d GETs=%d; want 1,2,1", h.direct, h.proofs, h.gets)
	}
	scope := snapshotOf(ctx)
	if scope.metadata.reads[h.suffix] != h.valueOwner {
		t.Fatal("read dependency lost")
	}
	// Only proof routing is reused inside the pin. The native GET/ACL check is
	// still issued on another load, rather than caching its returned value.
	h.value = "new native reply"
	got, err = s.readSnapshotMetadataString(ctx, key)
	if err != nil || got != h.value || h.gets != 2 || h.direct != 2 {
		t.Fatal("native descriptor GET cached or repeated proof", got, err, h.gets, h.direct)
	}
	fresh := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{id: h.root, store: s})
	if _, err := s.readSnapshotMetadataString(fresh, key); err != nil {
		t.Fatal(err)
	}
	if h.proofs != 4 || h.gets != 3 {
		t.Fatal("new execution reused descriptor proof", h.proofs, h.gets)
	}
}
func TestSnapshotDescriptorReadRejectsFailureWithoutReplay(t *testing.T) {
	for _, kind := range []string{"permission", "pipeline_only", "proof"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, h, key := descriptorReadFixture(t)
			switch kind {
			case "permission":
				h.getErr = controlScriptNativeError("NOPERM GET refused")
			case "pipeline_only":
				h.pipelineErr = io.ErrUnexpectedEOF
			case "proof":
				h.refs = "0"
			}
			got, err := s.readSnapshotMetadataString(ctx, key)
			if err == nil || got != "" || h.gets != 1 {
				t.Fatal("failed descriptor exposed or GET replayed", got, err, h.gets)
			}
			if kind == "pipeline_only" && !strings.Contains(err.Error(), io.ErrUnexpectedEOF.Error()) {
				t.Fatal("pipeline-only failure lost", err)
			}
			physical := snapshotWorkspace(h.valueOwner) + h.suffix
			if snapshotOf(ctx).metadata.checked[physical] {
				t.Fatal("failed descriptor proof cached")
			}
		})
	}
}

func TestSnapshotDescriptorReadMissingAndNativeErrorPrecedence(t *testing.T) {
	for _, kind := range []string{"missing", "missing_referenced", "wrongtype", "badproof_and_denied", "badproof_and_pipeline_loss"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, h, key := descriptorReadFixture(t)
			want := ""
			switch kind {
			case "missing":
				h.expected = "0"
				h.present = 0
				h.getErr = goredis.Nil
			case "missing_referenced":
				h.present = 0
				h.getErr = goredis.Nil
				want = "missing or corrupt"
			case "wrongtype":
				h.getErr = controlScriptNativeError("WRONGTYPE GET refused")
				want = "WRONGTYPE"
			case "badproof_and_denied":
				h.refs = "0"
				h.getErr = controlScriptNativeError("NOPERM GET refused")
				want = "missing or corrupt"
			case "badproof_and_pipeline_loss":
				h.refs = "0"
				h.getErr = controlScriptNativeError("NOPERM GET refused")
				h.pipelineErr = io.ErrUnexpectedEOF
				want = "unexpected EOF"
			}
			got, err := s.readSnapshotMetadataString(ctx, key)
			if got != "" || h.gets != 1 {
				t.Fatal("unexpected descriptor value/replay", got, h.gets)
			}
			if kind == "missing" {
				if err != goredis.Nil {
					t.Fatal("legitimate absent metadata changed", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal("descriptor proof/native error precedence changed", want, err)
			}
		})
	}
}
