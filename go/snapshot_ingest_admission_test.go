// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

type ingestAdmissionReader struct {
	array.RecordReader
	calls, batches atomic.Int64
}

func (r *ingestAdmissionReader) Next() bool {
	r.calls.Add(1)
	ok := r.RecordReader.Next()
	if ok {
		r.batches.Add(1)
	}
	return ok
}

// Observe only the maintenance-pool control request. The fixture has no worker
// and installs this delegate before sharing the connection with an execution.
type ingestAdmissionObserver struct {
	goredis.UniversalClient
	busy      chan struct{}
	once      sync.Once
	claims    atomic.Int64
	loseReply bool
	lost      atomic.Bool
}

func (o *ingestAdmissionObserver) Process(ctx context.Context, cmd goredis.Cmder) error {
	err := o.UniversalClient.Process(ctx, cmd)
	a := snapshotObservedControlArgs(cmd)
	if len(a) > 3 && strings.EqualFold(snapshotObservedControlName(cmd), "eval") {
		n, _ := strconv.Atoi(fmt.Sprint(a[2]))
		if len(a) > 3+n && a[3+n] == "admit_ingest" {
			o.claims.Add(1)
			if err == nil {
				if val, ok := cmd.(interface{ Val() any }); ok && val.Val() == int64(0) && o.busy != nil {
					o.once.Do(func() { close(o.busy) })
				}
				if o.loseReply && o.lost.CompareAndSwap(false, true) {
					cmd.SetErr(io.ErrUnexpectedEOF)
					return io.ErrUnexpectedEOF
				}
			}
		}
	}
	return err
}

func admissionStatement(t *testing.T, conn adbc.ConnectionWithContext, table, mode string, id int64, observer *ingestAdmissionObserver) (adbc.StatementWithContext, *ingestAdmissionReader) {
	t.Helper()
	st, err := conn.NewStatement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })
	if observer != nil {
		store := st.(*statementImpl).conn.store
		if store.compactor != nil || len(store.activeExecutions()) != 0 {
			t.Fatal("admission observer requires an idle no-worker fixture")
		}
		observer.UniversalClient = store.client
		store.client = observer
	}
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
		if err := st.SetOption(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	b := array.NewInt64Builder(memory.DefaultAllocator)
	b.Append(id)
	a := b.NewArray()
	b.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	record := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	a.Release()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
	record.Release()
	if err != nil {
		t.Fatal(err)
	}
	counted := &ingestAdmissionReader{RecordReader: reader}
	t.Cleanup(counted.Release)
	if err := st.BindStream(context.Background(), counted); err != nil {
		t.Fatal(err)
	}
	return st, counted
}
func admissionConnection(t *testing.T, db adbc.DatabaseWithContext) adbc.ConnectionWithContext {
	t.Helper()
	c, err := db.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}
func holdIngestAdmission(t *testing.T, h *sqlHarness, s *store, table string) (context.Context, *snapshotPin) {
	t.Helper()
	ctx, pin, err := s.acquireIngestSnapshot(h.ctx, func(ctx context.Context) (*tableMeta, error) { return s.getTable(ctx, defaultSchema, table) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pin.release)
	return ctx, pin
}
func noAdmissionPins(t *testing.T, h *sqlHarness, raw *goredis.Client) {
	t.Helper()
	fields, err := raw.HKeys(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if strings.HasPrefix(f, "p:") {
			t.Fatal("completed admission leaked a pin")
		}
	}
}

func TestSnapshotIngestAdmissionWaitsBeforeInput(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	h.exec("INSERT INTO t VALUES(1)")
	h.exec("CREATE TABLE u(id BIGINT NOT NULL)")
	s := snapshotTestStore(t, raw)
	old, pin := holdIngestAdmission(t, h, s, "t")
	observer := &ingestAdmissionObserver{busy: make(chan struct{})}
	st, input := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 2, observer)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()
	go func() { _, err := st.ExecuteUpdate(ctx); done <- err }()
	select {
	case <-observer.busy:
	case err := <-done:
		t.Fatal("writer did not wait", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if input.calls.Load() != 0 {
		t.Fatal("waiting writer consumed Arrow input")
	}
	other, otherInput := admissionStatement(t, admissionConnection(t, db), "u", adbc.OptionValueIngestModeAppend, 9, nil)
	if n, err := other.ExecuteUpdate(ctx); err != nil || n != 1 {
		t.Fatal("independent target was blocked", n, err)
	}
	if otherInput.calls.Load() != 2 || otherInput.batches.Load() != 1 {
		t.Fatal("independent input replay")
	}
	h.exec("INSERT INTO t VALUES(3)")
	oldRows := snapshotExecuteTest(t, s, old, "SELECT id FROM t")
	if len(oldRows.rows) != 1 || oldRows.rows[0][0].I != 1 {
		t.Fatal("admission pin lost its root", oldRows.rows)
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
	if input.calls.Load() != 2 || input.batches.Load() != 1 {
		t.Fatal("admission replayed Arrow input", input.calls.Load(), input.batches.Load())
	}
	h.expectRows("SELECT id FROM t ORDER BY id", "1", "2", "3")
	h.expectRows("SELECT COUNT(*),COUNT(DISTINCT __rowid),MAX(__rowid) FROM t", "3|3|3")
	h.expectRows("SELECT id FROM u", "9")
	noAdmissionPins(t, h, raw)
}

func TestSnapshotIngestAdmissionCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "close"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
			s := snapshotTestStore(t, raw)
			_, pin := holdIngestAdmission(t, h, s, "t")
			conn := admissionConnection(t, db)
			observer := &ingestAdmissionObserver{busy: make(chan struct{})}
			st, input := admissionStatement(t, conn, "t", adbc.OptionValueIngestModeAppend, 2, observer)
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
				t.Fatal("no busy admission", err)
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
				t.Fatal("waiting ingest did not stop")
			}
			want := adbc.StatusCancelled
			if mode == "deadline" {
				want = adbc.StatusTimeout
			}
			if mode == "close" {
				want = adbc.StatusInvalidState
			}
			if !isStatus(err, want) || input.calls.Load() != 0 {
				t.Fatal("cancelled admission consumed input or lost its status", err, input.calls.Load(), want)
			}
			pin.release()
			h.expectRows("SELECT COUNT(*) FROM t", "0")
			noAdmissionPins(t, h, raw)
		})
	}
}

func TestSnapshotIngestAdmissionLostReplyDoesNotReplay(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	observer := &ingestAdmissionObserver{loseReply: true}
	conn := admissionConnection(t, db)
	st, input := admissionStatement(t, conn, "t", adbc.OptionValueIngestModeAppend, 1, observer)
	if _, err := st.ExecuteUpdate(h.ctx); err == nil || !isStatus(err, adbc.StatusIO) {
		t.Fatal("lost admission reply was hidden", err)
	}
	if observer.claims.Load() != 1 || input.calls.Load() != 0 {
		t.Fatal("uncertain admission/input replayed", observer.claims.Load(), input.calls.Load())
	}
	noAdmissionPins(t, h, raw)
	h.expectRows("SELECT COUNT(*) FROM t", "0")
	fresh, freshInput := admissionStatement(t, conn, "t", adbc.OptionValueIngestModeAppend, 2, nil)
	if n, err := fresh.ExecuteUpdate(h.ctx); err != nil || n != 1 || freshInput.batches.Load() != 1 {
		t.Fatal("fresh explicit append failed", n, err)
	}
	h.expectRows("SELECT id FROM t", "2")
}

func TestSnapshotIngestAdmissionExpiredOwner(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT)")
	s := snapshotTestStore(t, raw)
	_, pin := holdIngestAdmission(t, h, s, "t")
	field := "p:" + pin.token
	var value map[string]any
	doc, err := raw.HGet(h.ctx, snapshotCatalogKey, field).Bytes()
	if err != nil || json.Unmarshal(doc, &value) != nil {
		t.Fatal("missing owner lease", err)
	}
	target := value["ingest_target"]
	if _, err := s.snapshotControl(h.ctx, "renew", pin.token, "", pin.root).Int(); err != nil {
		t.Fatal(err)
	}
	value = nil
	doc, err = raw.HGet(h.ctx, snapshotCatalogKey, field).Bytes()
	if err != nil || json.Unmarshal(doc, &value) != nil || value["ingest_target"] != target {
		t.Fatal("renewal lost admission hint", err)
	}
	value["expires"] = 0
	doc, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, snapshotCatalogKey, field, doc).Err(); err != nil {
		t.Fatal(err)
	}
	_, next := holdIngestAdmission(t, h, s, "t")
	if n, err := s.snapshotControl(h.ctx, "admit_ingest", pin.token, "", pin.root, target.(string)).Int(); err != nil || n != -1 {
		t.Fatal("expired owner was admitted", n, err)
	}
	next.release()
	pin.release()
	noAdmissionPins(t, h, raw)
}

func TestSnapshotIngestAdmissionConcurrentCreateAppend(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	var inputs []*ingestAdmissionReader
	var statements []adbc.StatementWithContext
	for i := range 4 {
		st, input := admissionStatement(t, admissionConnection(t, db), "fresh", adbc.OptionValueIngestModeCreateAppend, int64(i+1), nil)
		statements = append(statements, st)
		inputs = append(inputs, input)
	}
	start := make(chan struct{})
	done := make(chan error, 4)
	for _, st := range statements {
		go func() { <-start; _, err := st.ExecuteUpdate(h.ctx); done <- err }()
	}
	close(start)
	for range statements {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range inputs {
		if input.calls.Load() != 2 || input.batches.Load() != 1 {
			t.Fatal("create_append replayed input")
		}
	}
	h.expectRows("SELECT id FROM fresh ORDER BY id", "1", "2", "3", "4")
	h.expectRows("SELECT COUNT(*),COUNT(DISTINCT __rowid),MIN(__rowid),MAX(__rowid) FROM fresh", "4|4|1|4")
	noAdmissionPins(t, h, raw)
}
