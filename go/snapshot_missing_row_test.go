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

	goredis "github.com/redis/go-redis/v9"
)

type snapshotMissingReplies struct {
	newer, older                                         string
	count                                                int64
	countErr, scoreErr, outerErr                         error
	olderReads, proofBatches, scalarScores, scalarCounts int
	retryableProofs                                      int
}

func (*snapshotMissingReplies) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *snapshotMissingReplies) reply(c goredis.Cmder) error {
	if c.Name() == "zscore" || c.Name() == "zcard" {
		if marker, ok := c.(interface{ NoRetry() bool }); !ok || !marker.NoRetry() {
			h.retryableProofs++
		}
	}
	args := c.Args()
	switch c := c.(type) {
	case *goredis.MapStringStringCmd:
		key := fmt.Sprint(c.Args()[1])
		if strings.HasPrefix(key, h.older) {
			h.olderReads++
			if strings.HasSuffix(key, ":1") {
				c.SetVal(map[string]string{rowIDField: "1", "v": "9007199254740993"})
				return nil
			}
		}
		c.SetVal(map[string]string{})
	case interface {
		SetVal(int64)
		SetErr(error)
	}:
		c.SetVal(h.count)
		c.SetErr(h.countErr)
		return h.countErr
	case interface {
		SetVal(float64)
		SetErr(error)
	}:
		var err error = goredis.Nil
		if fmt.Sprint(args[1]) == snapshotMembers(snapshotDataID(h.newer)) {
			err = h.scoreErr
		}
		c.SetErr(err)
		return err
	default:
		return fmt.Errorf("unexpected missing-row command %T", c)
	}
	return nil
}
func (h *snapshotMissingReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error {
		if c.Name() == "zscore" {
			h.scalarScores++
		}
		if c.Name() == "zcard" {
			h.scalarCounts++
		}
		return h.reply(c)
	}
}
func (h *snapshotMissingReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		proof := len(cs) > 0 && cs[0].Name() == "zcard"
		if proof {
			h.proofBatches++
		}
		var first error
		for _, c := range cs {
			if err := h.reply(c); first == nil {
				first = err
			}
		}
		if proof && h.outerErr != nil {
			return h.outerErr
		}
		return first
	}
}
func snapshotMissingFixture(t *testing.T, private bool) (*store, context.Context, *tableMeta, *snapshotMissingReplies) {
	t.Helper()
	newer := snapshotRows(strings.Repeat("a", 32), "public", strings.Repeat("c", 32))
	older := snapshotRows(strings.Repeat("b", 32), "public", strings.Repeat("c", 32))
	h := &snapshotMissingReplies{newer: newer, older: older, count: 1, scoreErr: goredis.Nil}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
	client.AddHook(h)
	t.Cleanup(func() { _ = client.Close() })
	s := &store{client: client, snapshots: true}
	scope := &snapshotScope{store: s, write: private, owned: map[string]bool{snapshotDataID(newer): true}}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, scope)
	n := int64(1)
	meta := &tableMeta{KeyPrefix: newer, SnapshotRows: &n, SnapshotSegments: []snapshotSegment{{Prefix: older, Rows: 1}}}
	return s, ctx, meta, h
}

func TestSnapshotMissingRowUncertainAbsenceNeverReadsOlder(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, kind := range []string{"wrapped_nil", "joined_nil_eof"} {
			t.Run(fmt.Sprintf("private_%t/%s", private, kind), func(t *testing.T) {
				s, ctx, m, h := snapshotMissingFixture(t, private)
				h.scoreErr = fmt.Errorf("incomplete native reply: %w", goredis.Nil)
				if kind == "joined_nil_eof" {
					h.scoreErr = errors.Join(goredis.Nil, io.ErrUnexpectedEOF)
				}
				_, err := s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "1", m.prefix() + "2"}, nil)
				if err == nil || h.olderReads != 0 {
					t.Fatalf("uncertain absence exposed older image: err=%v older_reads=%d", err, h.olderReads)
				}
			})
		}
	}
}

func TestSnapshotMissingRowNativeProofRetainsExactOlderImage(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprintf("private_%t", private), func(t *testing.T) {
			s, ctx, m, h := snapshotMissingFixture(t, private)
			rows, err := s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "1", m.prefix() + "2"}, nil)
			if err != nil || len(rows) != 2 || rows[0] == nil || rows[0]["v"] != "9007199254740993" || rows[1] != nil {
				t.Fatal("exact absence/older-image behavior changed", rows, err)
			}
			if h.retryableProofs != 0 {
				t.Fatal("absence proof may retry after a lost reply", h.retryableProofs)
			}
			if h.scalarCounts != 0 {
				t.Fatal("published directory count was not batched", h.scalarCounts)
			}
			if private {
				if h.scalarScores != 2 || h.proofBatches != 1 {
					t.Fatal("private images gained a count proof or lost score-only path", h.scalarScores, h.proofBatches)
				}
			} else if h.scalarScores != 0 || h.proofBatches != 3 {
				t.Fatal("published missing rows did not use one proof pipeline each", h.scalarScores, h.proofBatches)
			}
		})
	}
}

func TestSnapshotMissingRowNativeProofFailurePrecedence(t *testing.T) {
	for _, kind := range []string{"count_error", "count_mismatch", "score_present", "score_denied", "outer_loss", "outer_joined_nil", "outer_nil_score_present"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, m, h := snapshotMissingFixture(t, false)
			want := ""
			switch kind {
			case "count_error":
				h.countErr = controlScriptNativeError("NOPERM count denied")
				h.scoreErr = io.ErrUnexpectedEOF
				want = "count denied"
			case "count_mismatch":
				h.count = 2
				h.scoreErr = io.ErrUnexpectedEOF
				want = "published snapshot row directory changed"
			case "score_present":
				h.scoreErr = nil
				want = "snapshot directory references a missing row"
			case "score_denied":
				h.scoreErr = controlScriptNativeError("NOPERM score denied")
				want = "score denied"
			case "outer_loss":
				h.outerErr = io.ErrUnexpectedEOF
				want = "unexpected EOF"
			case "outer_joined_nil":
				h.outerErr = errors.Join(goredis.Nil, io.ErrUnexpectedEOF)
				want = "unexpected EOF"
			case "outer_nil_score_present":
				h.outerErr = goredis.Nil
				h.scoreErr = nil
				want = "redis: nil"
			}
			_, err := s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "1", m.prefix() + "2"}, nil)
			if err == nil || !strings.Contains(err.Error(), want) || h.olderReads != 0 {
				t.Fatal("invalid proof accepted or precedence changed", kind, err, h.olderReads)
			}
			if h.proofBatches != 1 || h.scalarScores != 0 || h.scalarCounts != 0 {
				t.Fatal("failed proof replayed or split into multiple requests", h.proofBatches, h.scalarScores, h.scalarCounts)
			}
		})
	}
}
