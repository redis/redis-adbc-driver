// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// A reservation can race with other completed writers after the initial sweep.
// Fill the real ledger without reaping, then distinguish reclaimable work from
// genuinely active owners. No catalog records or limits are forged here.
func fillSnapshotGenerations(t *testing.T, h *sqlHarness, s *store, retire bool) {
	t.Helper()
	for range 15 {
		base, pin, err := s.acquireSnapshotMode(h.ctx, false, true)
		if err != nil {
			t.Fatal(err)
		}
		_, id, err := s.beginSnapshotWrite(base, pin)
		if err != nil {
			pin.release()
			t.Fatal(err)
		}
		t.Cleanup(func() { s.abortSnapshot(pin, id); pin.release() })
		if retire {
			s.abortSnapshot(pin, id)
			pin.release()
		}
	}
}

func snapshotWorkCount(t *testing.T, s *store, ctx context.Context) int {
	t.Helper()
	fields, err := s.client.HKeys(ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, field := range fields {
		if strings.HasPrefix(field, "w:") {
			n++
		}
	}
	return n
}

type generationReservationObserver struct {
	snapshotHook
	mu     sync.Mutex
	ids    []string
	clones int
	dataGC int
	lose   bool
}

func (h *generationReservationObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		reservation := false
		if cmd.Name() == "eval" && len(args) > 3 {
			if args[1] == snapshotMetadataCloneRoot {
				h.mu.Lock()
				h.clones++
				h.mu.Unlock()
			}
			if args[1] == snapshotControl {
				n, ok := args[2].(int)
				if ok && len(args) > 3+n && fmt.Sprint(args[3+n]) == "claim_data" {
					h.mu.Lock()
					h.dataGC++
					h.mu.Unlock()
				}
				if ok && len(args) > 3+n+2 && fmt.Sprint(args[3+n]) == "reserve" {
					reservation = true
					if once, ok := cmd.(interface{ NoRetry() bool }); !ok || !once.NoRetry() {
						return errors.New("reservation lost its no-retry wrapper")
					}
					h.mu.Lock()
					h.ids = append(h.ids, fmt.Sprint(args[3+n+2]))
					h.mu.Unlock()
				}
			}
		}
		err := next(ctx, cmd)
		if err == nil && reservation && h.lose {
			// A transport error containing the refusal text is not a Redis refusal.
			err = errors.New("lost reservation reply: snapshot generation limit (16) reached; release old readers or retry cleanup")
			cmd.SetErr(err)
		}
		return err
	}
}

func TestSnapshotGenerationAdmissionReclaimsRetiredWork(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	fillSnapshotGenerations(t, h, s, true)
	if n := snapshotWorkCount(t, s, h.ctx); n != 16 {
		t.Fatalf("fixture has %d workspaces, want 16", n)
	}
	observer := &generationReservationObserver{}
	s.client.AddHook(observer)
	work, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal("reclaimable work refused before any SQL/input", err)
	}
	defer s.abortSnapshot(pin, id)
	if snapshotOf(work) == nil || !snapshotOf(work).write || snapshotOf(work).id != id {
		t.Fatal("missing admitted write scope")
	}
	if n := snapshotWorkCount(t, s, h.ctx); n > 16 {
		t.Fatal("admission raised the generation limit", n)
	}
	if root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); root != pin.root {
		t.Fatal("admission changed the committed root")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.ids) != 2 || observer.ids[0] != observer.ids[1] || observer.clones != 1 || observer.dataGC != 0 {
		t.Fatalf("unexpected admission attempts/copies: %v %d", observer.ids, observer.clones)
	}
}

func TestSnapshotGenerationAdmissionProtectsActiveWork(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	fillSnapshotGenerations(t, h, s, false)
	_, id, err := s.beginSnapshotWrite(base, pin)
	if err == nil || id != "" || !strings.Contains(err.Error(), "generation limit (16)") {
		t.Fatal("active work was evicted", id, err)
	}
	if n := snapshotWorkCount(t, s, h.ctx); n != 16 {
		t.Fatal("active ledger changed", n)
	}
}

func TestSnapshotGenerationAdmissionNeverReplaysUnknownReservation(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	observer := &generationReservationObserver{lose: true}
	s.client.AddHook(observer)
	_, id, err := s.beginSnapshotWrite(base, pin)
	if err == nil || id != "" {
		t.Fatal("unknown reservation admitted SQL", id, err)
	}
	observer.mu.Lock()
	ids := append([]string(nil), observer.ids...)
	clones := observer.clones
	observer.mu.Unlock()
	if len(ids) != 1 || clones != 0 {
		t.Fatal("unknown reservation was replayed or copied", len(ids), clones)
	}
	defer s.abortSnapshot(pin, ids[0])
	if exists := raw.HExists(h.ctx, snapshotCatalogKey, "w:"+ids[0]).Val(); !exists {
		t.Fatal("lost-reply fixture did not reserve on the server")
	}
}
