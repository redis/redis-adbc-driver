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
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotPublicationRefusalReplies struct {
	snapshotHook
	meta                    *tableMeta
	proofErr                error
	laterInfoErr            error
	initialInfoErr          error
	infos, creates, queries int
}

func (h *snapshotPublicationRefusalReplies) reply(cmd goredis.Cmder) error {
	switch cmd.Name() {
	case "zcard":
		cmd.(*goredis.IntCmd).SetVal(1)
	case "ft.info":
		h.infos++
		if h.initialInfoErr != nil {
			cmd.SetErr(h.initialInfoErr)
		} else if h.laterInfoErr != nil {
			if h.infos == 1 {
				cmd.SetErr(readinessServerError("Inconsistent index state"))
			} else {
				cmd.SetErr(h.laterInfoErr)
			}
		} else {
			cmd.(interface{ SetVal(any) }).SetVal(map[string]any{"index_definition": map[string]any{"prefixes": []any{h.meta.prefix()}}, "num_docs": int64(1), "indexing": 0, "percent_indexed": 1, "hash_indexing_failures": 0})
		}
	case "ft.search":
		h.queries++
		cmd.SetErr(h.proofErr)
	case "ft.create":
		h.creates++
		cmd.SetErr(errors.New("stopped after replacement CREATE"))
	default:
		cmd.SetErr(fmt.Errorf("unexpected publication command %s", cmd.Name()))
	}
	return cmd.Err()
}
func (h *snapshotPublicationRefusalReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error { return h.reply(cmd) }
}
func (h *snapshotPublicationRefusalReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		var first error
		for _, cmd := range cmds {
			if err := h.reply(cmd); first == nil {
				first = err
			}
		}
		return first
	}
}
func publicationRefusalFixture(t *testing.T) (*store, context.Context, *tableMeta, *snapshotPublicationRefusalReplies) {
	t.Helper()
	id := strings.Repeat("a", 32)
	m := &tableMeta{Schema: defaultSchema, Name: "publication_refusal", KeyPrefix: snapshotRows(id, defaultSchema, strings.Repeat("b", 32)), IndexName: snapshotIndexPrefix(id) + "final"}
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = c.Close() })
	h := &snapshotPublicationRefusalReplies{meta: m}
	c.AddHook(h)
	s := &store{client: c, snapshots: true}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s, write: true, owned: map[string]bool{id: true}})
	return s, ctx, m, h
}
func TestSnapshotPublicationProofUnknownNeverRebuildsIndex(t *testing.T) {
	native := readinessServerError("NOPERM injected index-proof denial")
	for _, kind := range []string{"joined", "wrapped", "untyped", "native"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, m, h := publicationRefusalFixture(t)
			want := "unexpected EOF"
			wantCreates := 0
			switch kind {
			case "joined":
				h.proofErr = errors.Join(native, io.ErrUnexpectedEOF)
			case "wrapped":
				h.proofErr = fmt.Errorf("wrapped unknown: %w", native)
				want = "wrapped unknown"
			case "untyped":
				h.proofErr = errors.New("NOPERM unknown proof reply")
				want = "unknown proof reply"
			case "native":
				h.proofErr = native
				want = "stopped after replacement CREATE"
				wantCreates = 1
			}
			err := s.validateSnapshotTables(ctx, []*tableMeta{m})
			if err == nil || !strings.Contains(err.Error(), want) || h.creates != wantCreates || h.queries != 1 {
				t.Fatal("unsafe query-proof compatibility/replay or lost diagnostic", err, "creates", h.creates, "queries", h.queries)
			}
		})
	}
}
func TestSnapshotPublicationLaterInfoUnknownKeepsIOStatus(t *testing.T) {
	native := readinessServerError("NOPERM injected FT.INFO denial")
	for _, kind := range []string{"joined", "wrapped", "native"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, m, h := publicationRefusalFixture(t)
			want := "unexpected EOF"
			status := adbc.StatusIO
			switch kind {
			case "joined":
				h.laterInfoErr = errors.Join(native, io.ErrUnexpectedEOF)
			case "wrapped":
				h.laterInfoErr = fmt.Errorf("wrapped unknown: %w", native)
				want = "wrapped unknown"
			case "native":
				h.laterInfoErr = native
				want = "injected FT.INFO denial"
				status = adbc.StatusUnauthorized
			}
			err := s.validateSnapshotTables(ctx, []*tableMeta{m})
			if err == nil || !strings.Contains(err.Error(), want) || !isStatus(err, status) || h.infos != 2 || h.creates != 0 || h.queries != 0 {
				t.Fatal("unknown readiness reclassified/replayed", err, "info", h.infos, "creates", h.creates, "queries", h.queries)
			}
		})
	}
}

func TestSnapshotPublicationTransientRequiresDirectNative(t *testing.T) {
	for _, message := range []string{"Inconsistent index state", "SEARCH_VALUE_BAD Inconsistent index state"} {
		native := readinessServerError(message)
		if !snapshotPublicationTransient(native) {
			t.Fatal("direct native transient lost", message)
		}
		for _, err := range []error{errors.New(message), fmt.Errorf("wrapped: %w", native), errors.Join(native, io.ErrUnexpectedEOF), readinessServerError(message + ": another failure")} {
			if snapshotPublicationTransient(err) {
				t.Fatal("unknown/altered failure classified transient", err)
			}
		}
	}
}
func TestSnapshotPublicationUnknownTransientNeverPolls(t *testing.T) {
	for _, kind := range []string{"joined", "wrapped", "untyped"} {
		t.Run(kind, func(t *testing.T) {
			s, parent, m, h := publicationRefusalFixture(t)
			ctx, cancel := context.WithTimeout(parent, 100*time.Millisecond)
			defer cancel()
			native := readinessServerError("Inconsistent index state")
			want := "unexpected EOF"
			switch kind {
			case "joined":
				h.initialInfoErr = errors.Join(native, io.ErrUnexpectedEOF)
			case "wrapped":
				h.initialInfoErr = fmt.Errorf("wrapped unknown: %w", native)
				want = "wrapped unknown"
			case "untyped":
				h.initialInfoErr = errors.New("Inconsistent index state")
				want = "Inconsistent index state"
			}
			err := s.validateSnapshotTables(ctx, []*tableMeta{m})
			if err == nil || !strings.Contains(err.Error(), want) || !isStatus(err, adbc.StatusIO) || h.infos != 1 || h.creates != 0 || h.queries != 0 {
				t.Fatal("unknown readiness polled/reclassified or diagnostic lost", err, "infos", h.infos, "creates", h.creates, "queries", h.queries)
			}
		})
	}
}
