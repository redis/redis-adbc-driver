// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSnapshotWriterHandoffIndependentDatabases(t *testing.T) {
	for _, closeQueued := range []bool{false, true} {
		t.Run(map[bool]string{false: "handoff", true: "close_queued"}[closeQueued], func(t *testing.T) {
			h, raw, db := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringWriterAdmissionQueue: "true"})
			h.exec("CREATE SEQUENCE handoff_q CACHE 1")
			h.exec("CREATE TABLE handoff_t(id BIGINT)")
			foreign := snapshotTestStore(t, raw) // Represents an owner outside the local registry.
			_, owner := holdIngestAdmission(t, h, foreign, "handoff_t")
			defer owner.release()
			first := admissionConnection(t, db)
			o1 := &ingestAdmissionObserver{busy: make(chan struct{})}
			s1 := sqlAdmissionStatement(t, first, "INSERT INTO handoff_t VALUES(NEXTVAL('handoff_q'))", o1)
			impl2, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringWriterAdmissionQueue: "true"}))
			if err != nil {
				t.Fatal(err)
			}
			impl2.snapshotCompactionDisabled = true
			secondDB := driverbase.NewDatabase(impl2)
			defer secondDB.Close(h.ctx)
			second := admissionConnection(t, secondDB)
			o2 := &ingestAdmissionObserver{busy: make(chan struct{})}
			s2 := sqlAdmissionStatement(t, second, "INSERT INTO handoff_t VALUES(NEXTVAL('handoff_q'))", o2)
			a, b := s1.(*statementImpl).conn.store, s2.(*statementImpl).conn.store
			if a.writerDomain == ([32]byte{}) || a.writerDomain != b.writerDomain {
				t.Fatal("separate database handles do not share representable identity")
			}
			ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
			defer cancel()
			d1, d2 := make(chan error, 1), make(chan error, 1)
			go func() { _, err := s1.ExecuteUpdate(ctx); d1 <- err }()
			select {
			case <-o1.busy:
			case err := <-d1:
				t.Fatal("native external owner did not block local head", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go func() { _, err := s2.ExecuteUpdate(ctx); d2 <- err }()
			waitWriterRegistryRequests(t, 2)
			if o2.claims.Load() != 0 {
				t.Fatal("queued writer polled native admission")
			}
			if closeQueued {
				if err := second.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-d2:
					if !isStatus(err, adbc.StatusInvalidState) {
						t.Fatal("queued Close status changed", err)
					}
				case <-ctx.Done():
					t.Fatal("queued Close did not join")
				}
				waitWriterRegistryRequests(t, 1)
			}
			owner.release()
			select {
			case err := <-d1:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !closeQueued {
				select {
				case err := <-d2:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if closeQueued {
				h.expectRows("SELECT id FROM handoff_t ORDER BY id", "1")
				h.expectRows("SELECT NEXTVAL('handoff_q')", "2")
			} else {
				h.expectRows("SELECT id FROM handoff_t ORDER BY id", "1", "2")
				h.expectRows("SELECT NEXTVAL('handoff_q')", "3")
				if o2.claims.Load() != 1 {
					t.Fatal("queued writer did not hand off in one native admission", o2.claims.Load())
				}
			}
			waitWriterRegistryRequests(t, 0)
			noAdmissionPins(t, h, raw)
		})
	}
}

// A separate process has no access to this registry. Model that exact native
// admission path with an independently opened connection whose hint is absent.
func TestSnapshotWriterHandoffUncoordinatedNativeGate(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "unknown_admission"}[unknown], func(t *testing.T) {
			h, raw, db := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringWriterAdmissionQueue: "true"})
			h.exec("CREATE SEQUENCE outside_q CACHE 1")
			h.exec("CREATE TABLE outside_t(id BIGINT)")
			holder := admissionConnection(t, db)
			probe, err := holder.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			ownerStore := probe.(*statementImpl).conn.store
			if err := probe.Close(h.ctx); err != nil {
				t.Fatal(err)
			}
			_, owner := holdIngestAdmission(t, h, ownerStore, "outside_t")
			defer owner.release()
			if owner.writerTurn == nil {
				t.Fatal("coordinated owner has no local turn")
			}
			outside := admissionConnection(t, db)
			observer := &ingestAdmissionObserver{busy: make(chan struct{}), loseReply: unknown}
			st := sqlAdmissionStatement(t, outside, "INSERT INTO outside_t VALUES(NEXTVAL('outside_q'))", observer)
			st.(*statementImpl).conn.store.writerDomain = [32]byte{}
			ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := st.ExecuteUpdate(ctx); done <- err }()
			select {
			case <-observer.busy:
			case err := <-done:
				t.Fatal("outside writer bypassed native holder", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !unknown {
				cancel()
			}
			select {
			case err := <-done:
				want := adbc.StatusCancelled
				if unknown {
					want = adbc.StatusIO
				}
				if !isStatus(err, want) {
					t.Fatal("outside native outcome was hidden", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("outside writer did not join")
			}
			if unknown && observer.claims.Load() != 1 {
				t.Fatal("unknown native admission was replayed")
			}
			h.expectRows("SELECT COUNT(*) FROM outside_t", "0")
			h.expectRows("SELECT NEXTVAL('outside_q')", "1")
			owner.release()
			waitWriterRegistryRequests(t, 0)
			if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 1 {
				t.Fatal("fresh explicit outside statement failed", n, err)
			}
			h.expectRows("SELECT id FROM outside_t", "2")
			noAdmissionPins(t, h, raw)
		})
	}
}

func TestSnapshotWriterHandoffOptionConnectionLifetime(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	impl, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringWriterAdmissionQueue: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	defer impl.Close(h.ctx)
	impl.snapshotCompactionDisabled = true
	enabled, err := impl.openConnection(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer enabled.Close(h.ctx)
	prior := enabled.store.writerDomain
	if prior == ([32]byte{}) {
		t.Fatal("enabled connection did not capture identity")
	}
	if err := impl.SetOption(h.ctx, OptionStringWriterAdmissionQueue, "false"); err != nil {
		t.Fatal(err)
	}
	disabled, err := impl.openConnection(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close(h.ctx)
	if disabled.store.writerDomain != ([32]byte{}) || enabled.store.writerDomain != prior {
		t.Fatal("database option mutated an existing connection or failed to preserve default")
	}
	if err := impl.SetOption(h.ctx, OptionStringWriterAdmissionQueue, "true"); err != nil {
		t.Fatal(err)
	}
	again, err := impl.openConnection(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close(h.ctx)
	if again.store.writerDomain != prior {
		t.Fatal("enabled subsequent connection failed to share identity")
	}
}
