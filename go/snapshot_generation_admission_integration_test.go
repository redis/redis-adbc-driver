// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
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

// Failure-only diagnostics report counts and lease ranges, never physical IDs,
// keys, SQL, values, credentials or endpoints. Keep the original failure intact.
func snapshotAdmissionDiagnostics(t *testing.T, s *store, ctx context.Context) {
	t.Helper()
	now, err := s.client.Time(ctx).Result()
	if err != nil {
		t.Log("snapshot admission diagnostics: server clock unavailable")
		return
	}
	entries, err := s.client.HGetAll(ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Log("snapshot admission diagnostics: control records unavailable")
		return
	}
	type record struct {
		Root          string `json:"root"`
		Base          string `json:"base"`
		Token         string `json:"token"`
		State         string `json:"state"`
		Expires       int64  `json:"expires"`
		Writing       bool   `json:"writing"`
		Partition     bool   `json:"partition"`
		MetadataReady bool   `json:"metadata_ready"`
	}
	stats := map[string]int64{}
	origins := map[string]int64{}
	owners, protected := map[string]string{}, map[string]bool{}
	for field, value := range entries {
		if !strings.HasPrefix(field, "p:") {
			continue
		}
		var pin record
		if json.Unmarshal([]byte(value), &pin) != nil {
			stats["invalid_pin_records"]++
			continue
		}
		if pin.Expires <= now.UnixMilli() {
			stats["expired_pins"]++
			continue
		}
		stats["live_pins"]++
		originLabel := "untracked"
		if origin, ok := snapshotPinTestOrigins.Load(strings.TrimPrefix(field, "p:")); ok {
			value := origin.(snapshotPinTestOrigin)
			originLabel = value.test + ":" + value.pool + ":" + value.operation
			if value.failedReply {
				originLabel += ":failed_reply"
			}
		}
		origins[originLabel]++
		if pin.Writing {
			stats["live_writing_pins"]++
		} else if pin.Partition {
			stats["live_partition_sets"]++
		} else {
			stats["live_reader_pins"]++
		}
		owners[strings.TrimPrefix(field, "p:")] = pin.Root
		protected[pin.Root] = true
	}
	stats["distinct_pinned_roots"] = int64(len(protected))
	for field, value := range entries {
		if !strings.HasPrefix(field, "w:") {
			continue
		}
		stats["workspaces"]++
		var work record
		if json.Unmarshal([]byte(value), &work) != nil {
			stats["invalid_work_records"]++
			continue
		}
		state := work.State
		switch state {
		case "staging", "sealed", "committed", "deleting", "compacting":
		default:
			state = "other"
		}
		stats["state_"+state]++
		if state == "committed" && strings.TrimPrefix(field, "w:") != entries["root"] && !protected[strings.TrimPrefix(field, "w:")] {
			stats["unpinned_old_committed"]++
		}
		if state != "staging" && state != "sealed" && state != "compacting" {
			continue
		}
		remaining := work.Expires - now.UnixMilli()
		if remaining <= 0 {
			stats["expired_workspaces"]++
		} else if stats["minimum_work_remaining_ms"] == 0 || remaining < stats["minimum_work_remaining_ms"] {
			stats["minimum_work_remaining_ms"] = remaining
		}
		if state == "staging" || state == "sealed" {
			if owners[work.Token] == work.Base && work.Token != "" {
				stats["live_owned_private"]++
			} else if work.MetadataReady {
				stats["unowned_ready_private"]++
			} else {
				stats["unowned_preclone_private"]++
			}
		}
	}
	encoded, _ := json.Marshal(stats)
	t.Logf("snapshot admission diagnostics (before failed caller pin release): %s", encoded)
	encodedOrigins, _ := json.Marshal(origins)
	t.Logf("snapshot live pin test origins: %s", encodedOrigins)
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

// Another pre-execution writer can take a slot between our confirmed manifest
// retirement and reservation. Capacity refusals are definite server outcomes;
// they must not be confused with an unknown successful reservation reply.
type generationSlotCompetitor struct {
	generationReservationObserver
	competing *store
	pin       *snapshotPin
	id        string
	attempts  int
}

func (h *generationSlotCompetitor) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	observed := h.generationReservationObserver.ProcessHook(next)
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if cmd.Name() == "eval" && len(a) > 3 && a[1] == snapshotControl {
			keys, ok := a[2].(int)
			if ok && 3+keys < len(a) && a[3+keys] == "reserve" {
				h.attempts++
				if h.attempts == 2 {
					n, err := h.competing.snapshotControl(ctx, "reserve", h.pin.token, h.id, h.pin.root).Int()
					if err != nil || n != 1 {
						return fmt.Errorf("competing reservation did not consume the free slot: admitted=%d error=%v", n, err)
					}
				}
			}
		}
		return observed(ctx, cmd)
	}
}

func TestSnapshotGenerationAdmissionCompetingReservation(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s, competitor := snapshotTestStore(t, raw), snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	_, otherPin, err := competitor.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer otherPin.release()
	fillSnapshotGenerations(t, h, s, true)
	if n := snapshotWorkCount(t, s, h.ctx); n != 16 {
		t.Fatal("generation fixture not full", n)
	}
	otherID := snapshotID()
	defer competitor.abortSnapshot(otherPin, otherID)
	observer := &generationSlotCompetitor{competing: competitor, pin: otherPin, id: otherID}
	s.client.AddHook(observer)
	work, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal("reclaimable capacity refused after a competing reservation", err)
	}
	defer s.abortSnapshot(pin, id)
	if snapshotOf(work) == nil || !snapshotOf(work).write {
		t.Fatal("reservation did not admit a private workspace")
	}
	observer.mu.Lock()
	ids := append([]string(nil), observer.ids...)
	clones, dataGC := observer.clones, observer.dataGC
	observer.mu.Unlock()
	if len(ids) != 3 || ids[0] != id || ids[1] != id || ids[2] != id || clones != 1 || dataGC != 0 {
		t.Fatal("unexpected reservation retries, metadata copies or row GC", len(ids), clones, dataGC)
	}
	if n := snapshotWorkCount(t, s, h.ctx); n > 16 {
		t.Fatal("admission raised the generation limit", n)
	}
	if !raw.HExists(h.ctx, snapshotCatalogKey, "w:"+otherID).Val() || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != pin.root {
		t.Fatal("admission evicted the competitor or changed the committed root")
	}
}

func TestSnapshotGenerationAdmissionNeverReplaysUnknownAfterReclaim(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	fillSnapshotGenerations(t, h, s, true)
	observer := &generationReservationObserver{lose: true}
	s.client.AddHook(observer)
	_, id, err := s.beginSnapshotWrite(base, pin)
	if err == nil || id != "" {
		t.Fatal("unknown successful reservation admitted SQL/input")
	}
	observer.mu.Lock()
	ids := append([]string(nil), observer.ids...)
	clones := observer.clones
	observer.mu.Unlock()
	if len(ids) != 2 || ids[0] != ids[1] || clones != 0 {
		t.Fatal("unknown post-reclamation reservation was replayed or consumed input", len(ids), clones)
	}
	defer s.abortSnapshot(pin, ids[0])
	if !raw.HExists(h.ctx, snapshotCatalogKey, "w:"+ids[0]).Val() {
		t.Fatal("lost-reply fixture did not reserve on the server")
	}
}

// A lost reservation reply leaves a fresh staging record whose caller cannot
// abort by ID. Once its owning pin is released it cannot publish, and should
// not occupy the bounded ledger for the rest of the two-minute work lease.
func TestSnapshotGenerationAdmissionReclaimsReleasedUnknownOwner(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s, orphan := snapshotTestStore(t, raw), snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	orphanBase, orphanPin, err := orphan.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer orphanPin.release()
	lost := &generationReservationObserver{lose: true}
	orphan.client.AddHook(lost)
	if _, id, err := orphan.beginSnapshotWrite(orphanBase, orphanPin); err == nil || id != "" {
		t.Fatal("fixture did not lose a successful reservation reply")
	}
	lost.mu.Lock()
	ids := append([]string(nil), lost.ids...)
	lost.mu.Unlock()
	if len(ids) != 1 {
		t.Fatal("unknown reservation was replayed")
	}
	defer orphan.abortSnapshot(orphanPin, ids[0])
	for range 14 {
		owner, p, err := s.acquireSnapshotMode(h.ctx, false, true)
		if err != nil {
			t.Fatal(err)
		}
		_, work, err := s.beginSnapshotWrite(owner, p)
		if err != nil {
			p.release()
			t.Fatal(err)
		}
		t.Cleanup(func() { s.abortSnapshot(p, work); p.release() })
	}
	var state struct {
		State   string `json:"state"`
		Expires int64  `json:"expires"`
	}
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+ids[0]).Val()), &state); err != nil {
		t.Fatal(err)
	}
	now, err := raw.Time(h.ctx).Result()
	if err != nil || state.State != "staging" || state.Expires <= now.UnixMilli() || snapshotWorkCount(t, s, h.ctx) != 16 {
		t.Fatal("fixture lacks a fresh unexpired unknown reservation", err)
	}
	if candidate, err := s.snapshotControl(h.ctx, "peek", "", "", "").Text(); err != nil || candidate != "" {
		t.Fatal("live unknown owner became reclaimable", candidate, err)
	}
	orphanPin.release()
	observed := &generationReservationObserver{}
	s.client.AddHook(observed)
	_, work, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal("released unknown owner stranded otherwise-admissible work", err)
	}
	defer s.abortSnapshot(pin, work)
	if raw.HExists(h.ctx, snapshotCatalogKey, "w:"+ids[0]).Val() || snapshotWorkCount(t, s, h.ctx) != 16 || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != pin.root {
		t.Fatal("orphan reclamation changed the root, limit or retained unknown owner")
	}
	check := snapshotTestStore(t, raw)
	for _, op := range []string{"renew", "seal"} {
		if n, err := check.snapshotControl(h.ctx, op, orphanPin.token, ids[0], orphanPin.root).Int(); err != nil || n != 0 {
			t.Fatal("released owner revived reclaimed work", op, n, err)
		}
	}
	observed.mu.Lock()
	defer observed.mu.Unlock()
	if len(observed.ids) != 2 || observed.ids[0] != observed.ids[1] || observed.clones != 1 || observed.dataGC != 0 {
		t.Fatal("unexpected reservation replay, copy or data collection")
	}
}

// A background collector can complete the same claimed retirement. Its deletion
// must not count as ours, but it did free capacity: another writer can take that
// slot before our next reservation, while other reclaimable manifests remain.
type generationConcurrentRetirement struct {
	generationReservationObserver
	collector *store
	pin       *snapshotPin
	workspace string
	collected bool
}

func (h *generationConcurrentRetirement) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	observed := h.generationReservationObserver.ProcessHook(next)
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if !h.collected && cmd.Name() == "eval" && len(a) > 7 && a[1] == snapshotMetadataRetireRoot {
			h.collected = true
			claimed := fmt.Sprint(a[7])
			if err := h.collector.retireSnapshotMetadata(ctx, claimed); err != nil {
				return err
			}
			if err := h.collector.snapshotControl(ctx, "reclaimed", "", claimed, "").Err(); err != nil {
				return err
			}
			if n, err := h.collector.snapshotControl(ctx, "reserve", h.pin.token, h.workspace, h.pin.root).Int(); err != nil || n != 1 {
				return fmt.Errorf("competitor did not take the concurrently freed slot: admitted=%d error=%v", n, err)
			}
		}
		return observed(ctx, cmd)
	}
}

func TestSnapshotGenerationAdmissionConcurrentRetirement(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s, collector := snapshotTestStore(t, raw), snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	_, otherPin, err := collector.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer otherPin.release()
	fillSnapshotGenerations(t, h, s, true)
	competitor := snapshotID()
	defer collector.abortSnapshot(otherPin, competitor)
	observer := &generationConcurrentRetirement{collector: collector, pin: otherPin, workspace: competitor}
	s.client.AddHook(observer)
	_, work, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal("concurrent retirement hid remaining reclaimable capacity", err)
	}
	defer s.abortSnapshot(pin, work)
	if !observer.collected || !raw.HExists(h.ctx, snapshotCatalogKey, "w:"+competitor).Val() || snapshotWorkCount(t, s, h.ctx) > 16 || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != pin.root {
		t.Fatal("admission changed the limit, root or competing owner's work")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.ids) != 3 || observer.ids[0] != observer.ids[1] || observer.ids[1] != observer.ids[2] || observer.clones != 1 || observer.dataGC != 0 {
		t.Fatal("unexpected attempts, replay, cloning or row GC", len(observer.ids), observer.clones, observer.dataGC)
	}
}

func TestSnapshotSweepConcurrentManifestAccounting(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s, collector := snapshotTestStore(t, raw), snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	_, work, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		t.Fatal(err)
	}
	s.abortSnapshot(pin, work)
	_, otherPin, err := collector.acquireSnapshotMode(h.ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer otherPin.release()
	competitor := snapshotID()
	defer collector.abortSnapshot(otherPin, competitor)
	observer := &generationConcurrentRetirement{collector: collector, pin: otherPin, workspace: competitor}
	s.client.AddHook(observer)
	if reclaimed, err := s.reapSnapshotManifest(h.ctx); err != nil || reclaimed {
		t.Fatal("sweep counted another collector's deletion", reclaimed, err)
	}
	if !observer.collected || raw.HExists(h.ctx, snapshotCatalogKey, "w:"+work).Val() || !raw.HExists(h.ctx, snapshotCatalogKey, "w:"+competitor).Val() {
		t.Fatal("fixture did not complete a real concurrent retirement")
	}
	assertSnapshotDataReferences(t, h, s)
}
