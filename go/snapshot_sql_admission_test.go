// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func sqlAdmissionStatement(t *testing.T, conn adbc.ConnectionWithContext, sql string, observer *ingestAdmissionObserver) adbc.StatementWithContext {
	t.Helper()
	st, err := conn.NewStatement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })
	if observer != nil {
		s := st.(*statementImpl).conn.store
		observer.UniversalClient = s.client
		s.client = observer
	}
	if err := st.SetSqlQuery(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSnapshotSQLAdmissionConcurrentDML(t *testing.T) {
	for _, kind := range []string{"insert", "update", "delete", "merge", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE SEQUENCE q CACHE 1")
			h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
			if kind == "update" || kind == "merge" {
				h.exec("INSERT INTO t VALUES(0)")
			}
			if kind == "delete" {
				h.exec("INSERT INTO t VALUES(1),(2),(3),(4)")
			}
			var statements []adbc.StatementWithContext
			var inputs []*ingestAdmissionReader
			for i := range 4 {
				conn := admissionConnection(t, db)
				if kind == "mixed" && i >= 2 {
					st, input := admissionStatement(t, conn, "t", adbc.OptionValueIngestModeAppend, int64(99+i), nil)
					statements = append(statements, st)
					inputs = append(inputs, input)
					continue
				}
				sql := "INSERT INTO t VALUES(NEXTVAL('q'))"
				switch kind {
				case "update":
					sql = "UPDATE t SET id=id+1"
				case "delete":
					sql = fmt.Sprintf("DELETE FROM t WHERE id=%d", i+1)
				case "merge":
					sql = "MERGE INTO t USING (SELECT 1 AS v) s ON true WHEN MATCHED THEN UPDATE SET id=t.id+s.v"
				}
				statements = append(statements, sqlAdmissionStatement(t, conn, sql, nil))
			}
			start := make(chan struct{})
			done := make(chan error, 4)
			ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
			defer cancel()
			for _, st := range statements {
				go func() {
					<-start
					n, err := st.ExecuteUpdate(ctx)
					if err == nil && n != 1 {
						err = fmt.Errorf("affected rows %d, want 1", n)
					}
					done <- err
				}()
			}
			close(start)
			for range statements {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "insert":
				h.expectRows("SELECT id FROM t ORDER BY id", "1", "2", "3", "4")
				h.expectRows("SELECT NEXTVAL('q')", "5")
			case "mixed":
				h.expectRows("SELECT id FROM t ORDER BY id", "1", "2", "101", "102")
				h.expectRows("SELECT NEXTVAL('q')", "3")
			case "update", "merge":
				h.expectRows("SELECT id FROM t", "4")
			case "delete":
				h.expectRows("SELECT COUNT(*) FROM t", "0")
			}
			for _, input := range inputs {
				if input.calls.Load() != 2 || input.batches.Load() != 1 {
					t.Fatal("mixed writer replayed Arrow input")
				}
			}
			noAdmissionPins(t, h, raw)
		})
	}
}

func TestSnapshotSQLAdmissionCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "close"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE SEQUENCE q CACHE 1")
			h.exec("CREATE TABLE t(id BIGINT)")
			s := snapshotTestStore(t, raw)
			_, pin := holdIngestAdmission(t, h, s, "t")
			conn := admissionConnection(t, db)
			observer := &ingestAdmissionObserver{busy: make(chan struct{})}
			st := sqlAdmissionStatement(t, conn, "INSERT INTO t VALUES(NEXTVAL('q'))", observer)
			duration := 10 * time.Second
			if mode == "deadline" {
				duration = 2 * time.Second
			}
			ctx, cancel := context.WithTimeout(h.ctx, duration)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := st.ExecuteUpdate(ctx); done <- err }()
			select {
			case <-observer.busy:
			case err := <-done:
				t.Fatal("SQL did not wait", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode == "cancel" {
				cancel()
			}
			if mode == "close" {
				if err := conn.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("waiting SQL did not stop")
			}
			want := adbc.StatusCancelled
			if mode == "deadline" {
				want = adbc.StatusTimeout
			}
			if mode == "close" {
				want = adbc.StatusInvalidState
			}
			if !isStatus(err, want) {
				t.Fatal("waiting SQL lost cancellation status", err, want)
			}
			pin.release()
			h.expectRows("SELECT COUNT(*) FROM t", "0")
			h.expectRows("SELECT NEXTVAL('q')", "1")
			noAdmissionPins(t, h, raw)
		})
	}
}

func TestSnapshotSQLAdmissionLostReplyDoesNotExecuteOrReplay(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE SEQUENCE q CACHE 1")
	h.exec("CREATE TABLE t(id BIGINT)")
	observer := &ingestAdmissionObserver{loseReply: true}
	conn := admissionConnection(t, db)
	st := sqlAdmissionStatement(t, conn, "INSERT INTO t VALUES(NEXTVAL('q'))", observer)
	if _, err := st.ExecuteUpdate(h.ctx); !isStatus(err, adbc.StatusIO) {
		t.Fatal("lost SQL admission reply was hidden", err)
	}
	if observer.claims.Load() != 1 {
		t.Fatal("uncertain admission replayed", observer.claims.Load())
	}
	noAdmissionPins(t, h, raw)
	h.expectRows("SELECT COUNT(*) FROM t", "0")
	h.expectRows("SELECT NEXTVAL('q')", "1")
	// Explicit subsequent execution can acquire the released gate.
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 1 {
		t.Fatal("fresh explicit SQL failed", n, err)
	}
	h.expectRows("SELECT id FROM t", "2")
}

func TestSnapshotSQLAdmissionWaitsBeforeExecution(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	h.exec("INSERT INTO t VALUES(1)")
	h.exec("CREATE TABLE u(id BIGINT)")
	s := snapshotTestStore(t, raw)
	old, pin := holdIngestAdmission(t, h, s, "t")
	observer := &ingestAdmissionObserver{busy: make(chan struct{})}
	st := sqlAdmissionStatement(t, admissionConnection(t, db), "UPDATE t SET id=id+10", observer)
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := st.ExecuteUpdate(ctx); done <- err }()
	select {
	case <-observer.busy:
	case err := <-done:
		t.Fatal("SQL executed without waiting for the target writer", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Other targets remain writable. The waiting SQL must reacquire after both
	// this independent publication and the admitted owner's same-table write.
	h.exec("INSERT INTO u VALUES(9)")
	wctx, id, err := s.beginSnapshotWrite(old, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.prepareSnapshotTable(wctx, defaultSchema, "t"); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, wctx, "INSERT INTO t VALUES(3)")
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	pin.release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	h.expectRows("SELECT id FROM t ORDER BY id", "11", "13")
	h.expectRows("SELECT id FROM u", "9")
	noAdmissionPins(t, h, raw)
}
