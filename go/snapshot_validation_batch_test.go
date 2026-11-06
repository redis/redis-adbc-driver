// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Reply at the transport boundary, without a server. The assertion concerns
// round trips and concurrent proof publication, not Redis Search behavior.
type validationBatchReplies struct {
	snapshotHook
	segments                            map[string]snapshotSegment
	mutate                              func(goredis.Cmder) error
	beforeBatch                         func()
	direct, batches, infos, directories atomic.Int64
}

func (h *validationBatchReplies) reply(cmd goredis.Cmder) error {
	switch cmd.Name() {
	case "ft.info":
		h.infos.Add(1)
		seg := h.segments[fmt.Sprint(cmd.Args()[1])]
		once, ok := cmd.(onceCmd)
		if !ok {
			return fmt.Errorf("FT.INFO can be retried")
		}
		once.SetVal(map[string]any{"index_definition": map[string]any{"prefixes": []any{seg.Prefix}},
			"num_docs": seg.Rows, "indexing": 0, "percent_indexed": 1, "hash_indexing_failures": 0})
	case "zcard":
		h.directories.Add(1)
		cmd.(*goredis.IntCmd).SetVal(1)
	default:
		return fmt.Errorf("unexpected validation command %s", cmd.Name())
	}
	if h.mutate != nil {
		return h.mutate(cmd)
	}
	return nil
}

func (h *validationBatchReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		h.direct.Add(1)
		return h.reply(cmd)
	}
}

func (h *validationBatchReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		h.batches.Add(1)
		if h.beforeBatch != nil {
			h.beforeBatch()
		}
		for _, cmd := range cmds {
			if err := h.reply(cmd); err != nil {
				return err
			}
		}
		return nil
	}
}

func validationBatchFixture(t *testing.T) (*store, context.Context, *tableMeta, *validationBatchReplies) {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = client.Close() })
	hook := &validationBatchReplies{segments: map[string]snapshotSegment{}}
	client.AddHook(hook)
	var segments []snapshotSegment
	access := snapshotID()
	for range 3 {
		id := snapshotID()
		seg := snapshotSegment{Prefix: snapshotRows(id, defaultSchema, access), Index: snapshotIndexPrefix(id) + "working", Rows: 1}
		hook.segments[seg.Index] = seg
		segments = append(segments, seg)
	}
	n := int64(1)
	last := segments[len(segments)-1]
	meta := &tableMeta{SnapshotAccessID: access, Schema: defaultSchema, KeyPrefix: last.Prefix, IndexName: last.Index, SnapshotRows: &n,
		SnapshotOverlay: true, SnapshotSegments: segments[:len(segments)-1]}
	s := &store{client: client, snapshots: true}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s})
	return s, ctx, meta, hook
}

func TestSnapshotValidationBatchesConcurrentSources(t *testing.T) {
	s, ctx, meta, hook := validationBatchFixture(t)
	if err := s.validateSnapshotTable(ctx, meta); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := s.validateSnapshotTable(ctx, meta); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if hook.direct.Load() != 0 || hook.batches.Load() != 1 || hook.infos.Load() != 3 || hook.directories.Load() != 3 {
		t.Fatalf("validation transport: direct=%d batches=%d info=%d directories=%d; want 0,1,3,3",
			hook.direct.Load(), hook.batches.Load(), hook.infos.Load(), hook.directories.Load())
	}
	// Cold concurrent preparations may split reservations into several batches,
	// but each immutable descriptor must still be checked exactly once.
	cold := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s})
	for range 32 {
		wg.Go(func() {
			if err := s.validateSnapshotTable(cold, meta); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if hook.direct.Load() != 0 || hook.infos.Load() != 6 || hook.directories.Load() != 6 || hook.batches.Load() > 4 {
		t.Fatalf("cold concurrent validation duplicated or omitted proofs: direct=%d batches=%d info=%d directories=%d",
			hook.direct.Load(), hook.batches.Load(), hook.infos.Load(), hook.directories.Load())
	}
}

func TestSnapshotValidationBatchRefusesIncompleteProofs(t *testing.T) {
	for _, fault := range []string{"metadata denied", "directory denied", "short directory", "building", "partial docs", "failed images", "wrong prefix", "transport"} {
		t.Run(fault, func(t *testing.T) {
			s, ctx, meta, hook := validationBatchFixture(t)
			target := meta.snapshotSegments()[1]
			hook.mutate = func(cmd goredis.Cmder) error {
				key := fmt.Sprint(cmd.Args()[1])
				if cmd.Name() == "zcard" && key == snapshotMembers(snapshotDataID(target.Prefix)) {
					if fault == "short directory" {
						cmd.(*goredis.IntCmd).SetVal(0)
					}
					if fault == "directory denied" {
						err := errors.New("NOPERM directory")
						cmd.SetErr(err)
						return err
					}
				}
				if cmd.Name() == "ft.info" && key == target.Index {
					if fault == "metadata denied" {
						err := errors.New("NOPERM metadata")
						cmd.SetErr(err)
						return err
					}
					fields := cmd.(onceCmd).Val().(map[string]any)
					switch fault {
					case "building":
						fields["indexing"] = 1
					case "partial docs":
						fields["num_docs"] = 0
					case "failed images":
						fields["hash_indexing_failures"] = 1
					case "wrong prefix":
						fields["index_definition"] = map[string]any{"prefixes": []any{"alien:"}}
					case "transport":
						return errors.New("lost batch reply")
					}
				}
				return nil
			}
			if err := s.validateSnapshotTable(ctx, meta); err == nil {
				t.Fatal("accepted incomplete validation")
			}
			before := hook.batches.Load()
			if err := s.validateSnapshotTable(ctx, meta); err == nil {
				t.Fatal("cached incomplete validation as success")
			}
			if hook.batches.Load() != before {
				t.Fatal("replayed failed validation in the same statement")
			}
			hook.mutate = nil
			fresh := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s})
			if err := s.validateSnapshotTable(fresh, meta); err != nil {
				t.Fatal("fresh statement reused a prior failed proof", err)
			}
		})
	}
}

func TestSnapshotValidationBatchWaiterCancellation(t *testing.T) {
	s, ctx, meta, hook := validationBatchFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	hook.beforeBatch = func() { close(entered); <-release }
	owner := make(chan error, 1)
	go func() { owner <- s.validateSnapshotTable(ctx, meta) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not start validation")
	}
	waitCtx, cancel := context.WithCancel(ctx)
	waiter := make(chan error, 1)
	go func() { waiter <- s.validateSnapshotTable(waitCtx, meta) }()
	cancel()
	select {
	case err := <-waiter:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("waiter error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled waiter remained blocked on shared validation")
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-owner:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not finish")
	}
	if err := s.validateSnapshotTable(ctx, meta); err != nil {
		t.Fatal("waiter poisoned completed proof", err)
	}
	if hook.batches.Load() != 1 {
		t.Fatal("waiter duplicated owner validation")
	}
}

func TestSnapshotValidationBatchOwnerCancellation(t *testing.T) {
	s, ctx, meta, hook := validationBatchFixture(t)
	ownerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Simulate complete replies racing with cancellation. A transport hook can
	// return nil after the context is canceled; those replies cannot admit SQL.
	hook.beforeBatch = cancel
	if err := s.validateSnapshotTable(ownerCtx, meta); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("owner cancellation: %v", err)
	}
	if err := s.validateSnapshotTable(ctx, meta); err == nil {
		t.Fatal("canceled owner published a successful shared proof")
	}
	hook.beforeBatch = nil
	fresh := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s})
	if err := s.validateSnapshotTable(fresh, meta); err != nil {
		t.Fatal("fresh statement could not validate after cancellation", err)
	}
}
