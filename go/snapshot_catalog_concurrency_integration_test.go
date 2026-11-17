// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Exercise the public native ADBC statement path, with connections opened
// serially and only distinct catalog statements synchronized, as in dbt.
func TestSnapshotConcurrentCatalogStatements(t *testing.T) {
	h, _, db := newSnapshotHarnessMode(t, true, false)
	statements := make([]adbc.StatementWithContext, 4)
	for i := range statements {
		conn, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close(h.ctx) })
		statements[i], err = conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		stmt := statements[i]
		t.Cleanup(func() { _ = stmt.Close(h.ctx) })
	}
	run := func(format string) {
		t.Helper()
		var wg sync.WaitGroup
		start := make(chan struct{})
		failures := make(chan error, len(statements))
		for i, stmt := range statements {
			if err := stmt.SetSqlQuery(h.ctx, fmt.Sprintf(format, i)); err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func() { defer wg.Done(); <-start; _, err := stmt.ExecuteUpdate(h.ctx); failures <- err }()
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Errorf("%s: %v", format, err)
			}
		}
		if t.Failed() {
			t.FailNow()
		}
	}
	run("CREATE SCHEMA catalog_%d")
	// Same-schema membership and global OID allocation must both coordinate.
	for trial := 0; trial < 3; trial++ {
		run(fmt.Sprintf("CREATE TABLE public.model_%d_%%d AS SELECT %%[1]d AS id", trial))
		run(fmt.Sprintf("CREATE VIEW public.view_%d_%%d AS SELECT id FROM public.model_%d_%%[1]d", trial, trial))
		for i := range statements {
			h.expectRows(fmt.Sprintf("SELECT id FROM public.view_%d_%d", trial, i), fmt.Sprint(i))
		}
		run(fmt.Sprintf("ALTER TABLE public.model_%d_%%d RENAME TO renamed_%d_%%[1]d", trial, trial))
		run(fmt.Sprintf("DROP VIEW public.view_%d_%%d", trial))
		run(fmt.Sprintf("DROP TABLE public.renamed_%d_%%d", trial))
	}
	run("DROP SCHEMA catalog_%d")
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name LIKE 'model_%' OR table_name LIKE 'renamed_%'", "0")
}

// A waiting Arrow creator must neither reserve a workspace nor consume input.
func TestSnapshotCatalogAdmissionWaitsBeforeArrowInput(t *testing.T) {
	for _, mode := range []string{adbc.OptionValueIngestModeCreate, adbc.OptionValueIngestModeAppend} {
		for _, cancelWaiting := range []bool{false, true} {
			t.Run(fmt.Sprint(mode, "/cancel=", cancelWaiting), func(t *testing.T) {
				h, raw, db := newSnapshotHarnessMode(t, false, false)
				if mode == adbc.OptionValueIngestModeAppend {
					h.exec("CREATE TABLE created(id BIGINT)")
				}
				s := snapshotTestStore(t, raw)
				owner, pin, err := s.acquireWritableSnapshot(h.ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer pin.release()
				// Stage an actual catalog change after taking admission. The waiter must
				// resolve a fresh root once this change publishes, including its OID counter.
				wctx, id, err := s.beginSnapshotWrite(owner, pin)
				if err != nil {
					t.Fatal(err)
				}
				defer s.abortSnapshot(pin, id)
				snapshotExecuteTest(t, s, wctx, "CREATE TABLE owner(id BIGINT)")
				observer := &ingestAdmissionObserver{busy: make(chan struct{}), operation: "admit_catalog"}
				if mode == adbc.OptionValueIngestModeAppend {
					observer.operation = "admit_ingest"
				}
				conn := admissionConnection(t, db)
				stmt, input := admissionStatement(t, conn, "created", mode, 42, observer)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := stmt.ExecuteUpdate(ctx); done <- err }()
				select {
				case <-observer.busy:
				case <-time.After(5 * time.Second):
					t.Fatal("catalog waiter did not reach admission")
				}
				if input.calls.Load() != 0 {
					t.Fatal("waiting creator consumed input")
				}
				if cancelWaiting {
					cancel()
				} else {
					if err := s.publishSnapshot(wctx, pin, id); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-done:
					if cancelWaiting {
						if !isStatus(err, adbc.StatusCancelled) {
							t.Fatal("lost cancellation status", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("catalog waiter did not finish")
				}
				if cancelWaiting {
					if input.calls.Load() != 0 {
						t.Fatal("cancelled creator consumed input")
					}
				} else {
					if input.batches.Load() != 1 {
						t.Fatal("creator replayed input", input.batches.Load())
					}
					h.expectRows("SELECT id FROM created", "42")
					h.expectRows("SELECT COUNT(*) FROM owner", "0")
					h.expectRows("SELECT COUNT(DISTINCT oid) FROM pg_class WHERE relname IN ('owner','created')", "2")
				}
				pin.release()
				noAdmissionPins(t, h, raw)
			})
		}
	}

}

func TestSnapshotCatalogAdmissionLostReplyDoesNotReplay(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	observer := &ingestAdmissionObserver{loseReply: true, operation: "admit_catalog"}
	conn := admissionConnection(t, db)
	stmt, input := admissionStatement(t, conn, "created", adbc.OptionValueIngestModeCreate, 42, observer)
	if _, err := stmt.ExecuteUpdate(h.ctx); !isStatus(err, adbc.StatusIO) {
		t.Fatal("lost catalog admission reply was hidden", err)
	}
	if observer.claims.Load() != 1 || input.calls.Load() != 0 {
		t.Fatal("uncertain admission or input replayed", observer.claims.Load(), input.calls.Load())
	}
	noAdmissionPins(t, h, raw)
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='created'", "0")
}

func TestSnapshotCatalogAdmissionLease(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE live(id BIGINT)")
	s := snapshotTestStore(t, raw)
	read, reader, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.release()
	if n, err := s.snapshotControl(read, "admit_catalog", reader.token, "", reader.root).Int(); err != nil || n != -1 {
		t.Fatal("read-only pin admitted", n, err)
	}
	reader.release()
	owner, pin, err := s.acquireWritableSnapshot(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	if n, err := s.snapshotControl(owner, "renew", pin.token, "", pin.root).Int(); err != nil || n != 1 {
		t.Fatal("renew failed", n, err)
	}
	var value map[string]any
	doc, err := raw.HGet(h.ctx, snapshotCatalogKey, "p:"+pin.token).Bytes()
	if err != nil || json.Unmarshal(doc, &value) != nil || value["catalog"] != true {
		t.Fatal("renewal lost catalog ownership", err)
	}
	// Reads remain available while catalog admission is held.
	h.expectRows("SELECT COUNT(*) FROM live", "0")
	stale, stalePin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer stalePin.release()
	pin.release()
	h.exec("INSERT INTO live VALUES(7)")
	if n, err := s.snapshotControl(stale, "admit_catalog", stalePin.token, "", stalePin.root).Int(); err != nil || n != 0 {
		t.Fatal("stale root admitted", n, err)
	}
	stalePin.release()
	owner, pin, err = s.acquireWritableSnapshot(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	doc, err = raw.HGet(h.ctx, snapshotCatalogKey, "p:"+pin.token).Bytes()
	if err != nil || json.Unmarshal(doc, &value) != nil {
		t.Fatal(err)
	}
	value["expires"] = 0
	doc, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+pin.token, doc).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.snapshotControl(owner, "admit_catalog", pin.token, "", pin.root).Int(); err != nil || n != -1 {
		t.Fatal("expired owner admitted", n, err)
	}
	_, next, err := s.acquireWritableSnapshot(h.ctx, nil)
	if err != nil {
		t.Fatal("expired catalog owner stranded waiter", err)
	}
	next.release()
	pin.release()
	noAdmissionPins(t, h, raw)
}

// DML may depend on catalog membership even when it writes a different table.
// Execute it once, then ensure unrelated DDL cannot invalidate that dependency
// between execution and publication (the remaining dbt snapshot failure).
func TestSnapshotCatalogDMLMetadataDependencies(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE counts(v BIGINT)")
	s := snapshotTestStore(t, raw)
	owner, pin := holdIngestAdmission(t, h, s, "counts")
	wctx, id, err := s.beginSnapshotWrite(owner, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.prepareSnapshotTable(wctx, "public", "counts"); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, wctx, "INSERT INTO counts SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public'")
	conn := admissionConnection(t, db)
	stmt, err := conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	target := stmt.(*statementImpl).conn.store
	observer := &ingestAdmissionObserver{UniversalClient: target.client, busy: make(chan struct{}), operation: "admit_catalog"}
	target.client = observer
	if err := stmt.SetSqlQuery(h.ctx, "CREATE SCHEMA independent"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := stmt.ExecuteUpdate(ctx); done <- err }()
	select {
	case <-observer.busy:
	case err := <-done:
		t.Fatal("catalog writer did not protect admitted DML dependencies", err)
	case <-time.After(5 * time.Second):
		t.Fatal("catalog writer never reached admission")
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal("DML metadata dependency changed", err)
	}
	pin.release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("catalog writer stranded")
	}
	h.expectRows("SELECT v FROM counts", "1")
	h.expectRows("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='independent'", "1")
}
