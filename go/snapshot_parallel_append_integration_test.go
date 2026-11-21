// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// The second Next is reached only after the first batch has been written to
// private Redis storage. Waiting here proves staging overlaps, not just binds.
type parallelAppendReader struct {
	array.RecordReader
	ctx     context.Context
	staged  chan struct{}
	resume  chan struct{}
	next    int
	failure error
}

func (r *parallelAppendReader) Next() bool {
	r.next++
	if r.next == 2 {
		close(r.staged)
		if r.ctx == nil {
			<-r.resume
		} else {
			select {
			case <-r.resume:
			case <-r.ctx.Done():
				return false
			}
		}
		if r.failure != nil {
			return false
		}
	}
	return r.RecordReader.Next()
}
func (r *parallelAppendReader) Err() error {
	if r.ctx != nil && r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	if r.failure != nil && r.next >= 2 {
		return r.failure
	}
	return r.RecordReader.Err()
}

func TestSnapshotParallelAppendQueued(t *testing.T) {
	t.Setenv("REDIS_SNAPSHOT_WRITER_QUEUE", "true")
	TestSnapshotParallelAppendStreams(t)
}

func TestSnapshotParallelAppendStagedCancellation(t *testing.T) {
	snapshotParallelAppendStagedCancellation(t, false)
}

func TestSnapshotAppendIndexedStagedCancellation(t *testing.T) {
	snapshotParallelAppendStagedCancellation(t, true)
}

func snapshotParallelAppendStagedCancellation(t *testing.T, earlyIndex bool) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	declaration := "CREATE TABLE t(id BIGINT NOT NULL)"
	if earlyIndex {
		declaration = "CREATE TABLE t(id BIGINT NOT NULL NOINDEX)"
	}
	h.exec(declaration)
	st, _ := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 1, nil)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	impl := st.(*statementImpl)
	r := &parallelAppendReader{RecordReader: impl.params, ctx: ctx, staged: make(chan struct{}), resume: make(chan struct{})}
	impl.params = r
	done := make(chan error, 1)
	go func() { _, err := st.ExecuteUpdate(ctx); done <- err }()
	select {
	case <-r.staged:
	case err := <-done:
		t.Fatal("failed before staging", err)
	case <-time.After(10 * time.Second):
		t.Fatal("no staged batch")
	}
	other, _ := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 2, nil)
	if n, err := other.ExecuteUpdate(h.ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled stream published")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	h.expectRows("SELECT id FROM t", "2")
	s := snapshotTestStore(t, raw)
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
	noAdmissionPins(t, h, raw)
}

func TestSnapshotParallelAppendStreams(t *testing.T) {
	snapshotParallelAppendStreams(t, false)
}

func TestSnapshotAppendIndexedParallelStreams(t *testing.T) {
	snapshotParallelAppendStreams(t, true)
}

func snapshotParallelAppendStreams(t *testing.T, earlyIndex bool) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("fail=", fail), func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			declaration := "CREATE TABLE t(id BIGINT NOT NULL)"
			if earlyIndex {
				declaration = "CREATE TABLE t(id BIGINT NOT NULL NOINDEX)"
			}
			h.exec(declaration)
			h.exec("INSERT INTO t VALUES(0)")
			s := snapshotTestStore(t, raw)
			old, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			type result struct {
				n   int64
				err error
			}
			readers := make([]*parallelAppendReader, 4)
			done := make([]chan result, 4)
			finished := make([]bool, 4)
			observers := make([]*snapshotAppendIndexObserver, 4)
			for i := range readers {
				st, _ := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, int64(i+1), nil)
				impl := st.(*statementImpl)
				ctx := h.ctx
				if earlyIndex {
					observers[i] = &snapshotAppendIndexObserver{}
					ctx = snapshotAppendIndexTransport(t, ctx, st, observers[i])
				}
				r := &parallelAppendReader{RecordReader: impl.params, staged: make(chan struct{}), resume: make(chan struct{})}
				if fail && i == 1 {
					r.failure = errors.New("deliberate stream failure")
				}
				impl.params = r
				readers[i] = r
				done[i] = make(chan result, 1)
				go func(i int) { n, err := st.ExecuteUpdate(ctx); done[i] <- result{n, err} }(i)
			}
			// Always unblock input, including a fatal overlap assertion.
			defer func() {
				for _, r := range readers {
					select {
					case <-r.resume:
					default:
						close(r.resume)
					}
				}
				for i, ch := range done {
					if finished[i] {
						continue
					}
					select {
					case <-ch:
					case <-time.After(5 * time.Second):
						t.Error("append did not finish during cleanup")
					}
				}
			}()
			for i, r := range readers {
				select {
				case <-r.staged:
				case v := <-done[i]:
					finished[i] = true
					t.Fatalf("stream %d exited before overlap: %+v", i, v)
				case <-time.After(15 * time.Second):
					t.Fatalf("stream %d did not stage concurrently", i)
				}
				if earlyIndex && (observers[i].creates.Load() != 1 || observers[i].rows.Load() != 1) {
					t.Fatalf("stream %d did not populate its initial index before staging: %+v", i, observers[i])
				}
			}
			h.expectRows("SELECT COUNT(*) FROM t", "1")
			type retained struct {
				ctx   context.Context
				pin   *snapshotPin
				count int
			}
			var retainedRoots []retained
			visible := 1
			// Reverse allocation order, and leave some peers running after every
			// publication. A failed peer must not erase any completed append.
			for i := 3; i >= 0; i-- {
				close(readers[i].resume)
				select {
				case v := <-done[i]:
					finished[i] = true
					if fail && i == 1 {
						if v.err == nil {
							t.Fatal("failed input published")
						}
					} else if v.err != nil || v.n != 1 {
						t.Fatalf("stream %d: %+v", i, v)
					} else {
						visible++
					}
				case <-time.After(15 * time.Second):
					t.Fatalf("stream %d failed to publish", i)
				}
				retainedCtx, retainedPin, err := s.acquireSnapshot(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer retainedPin.release()
				retainedRoots = append(retainedRoots, retained{retainedCtx, retainedPin, visible})
			}
			if fail {
				h.expectRows("SELECT id FROM t ORDER BY id", "0", "1", "3", "4")
			} else {
				h.expectRows("SELECT id FROM t ORDER BY id", "0", "1", "2", "3", "4")
			}
			want := 5
			if fail {
				want--
			}
			h.expectRows("SELECT COUNT(*),COUNT(DISTINCT __rowid) FROM t", fmt.Sprintf("%d|%d", want, want))
			rows := snapshotExecuteTest(t, s, old, "SELECT id FROM t")
			if len(rows.rows) != 1 || rows.rows[0][0].I != 0 {
				t.Fatal("pinned reader changed", rows.rows)
			}
			pin.release()
			assertSnapshotDataReferences(t, h, s)
			snapshotReapTest(s, h.ctx)
			for _, root := range retainedRoots {
				rows := snapshotExecuteTest(t, s, root.ctx, "SELECT id FROM t")
				if len(rows.rows) != root.count {
					t.Fatal("intermediate pinned root changed after GC", len(rows.rows), root.count)
				}
				root.pin.release()
			}
			snapshotReapTest(s, h.ctx)
			noAdmissionPins(t, h, raw)
			if earlyIndex {
				for i, observer := range observers {
					if observer.creates.Load() != 1 || observer.notifications.Load() != 0 {
						t.Fatalf("stream %d rebuilt/notified its early index: %+v", i, observer)
					}
				}
			}
		})
	}
}

func TestSnapshotParallelAppendFlex(t *testing.T) {
	t.Setenv("REDIS_SNAPSHOT_FLEX", "true")
	TestSnapshotParallelAppendStreams(t)
}

// A child has its own driver, pools, process-local queue and Go runtime. The
// parent owns the disposable database; helpers must never initialize/flush it.
func TestSnapshotParallelAppendProcessHelper(t *testing.T) {
	if os.Getenv("REDIS_APPEND_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: os.Getenv("REDIS_SNAPSHOT_FLEX")}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	conn := admissionConnection(t, db)
	value, err := strconv.ParseInt(os.Getenv("REDIS_APPEND_VALUE"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := admissionStatement(t, conn, "t", adbc.OptionValueIngestModeAppend, value, nil)
	impl := st.(*statementImpl)
	r := &parallelAppendReader{RecordReader: impl.params, staged: make(chan struct{}), resume: make(chan struct{})}
	impl.params = r
	go func() {
		select {
		case <-r.staged:
		case <-ctx.Done():
			return
		}
		_ = os.WriteFile(os.Getenv("REDIS_APPEND_STAGED"), []byte("staged"), 0600)
		for {
			if _, err := os.Stat(os.Getenv("REDIS_APPEND_RESUME")); err == nil {
				close(r.resume)
				return
			}
			select {
			case <-ctx.Done():
				close(r.resume)
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	if n, err := st.ExecuteUpdate(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestSnapshotParallelAppendProcesses(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
	defer cancel()
	done := make([]chan error, 2)
	for i := range done {
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestSnapshotParallelAppendProcessHelper$", "-test.timeout=40s")
		flex := os.Getenv("REDIS_SNAPSHOT_FLEX")
		if flex == "" {
			flex = "false"
		}
		cmd.Env = append(os.Environ(), "REDIS_APPEND_CHILD=1", "REDIS_APPEND_VALUE="+strconv.Itoa(i+1), "REDIS_SNAPSHOT_FLEX="+flex, "REDIS_APPEND_STAGED="+filepath.Join(dir, fmt.Sprintf("staged%d", i)), "REDIS_APPEND_RESUME="+filepath.Join(dir, fmt.Sprintf("resume%d", i)))
		done[i] = make(chan error, 1)
		go func(i int) {
			output, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, output)
			}
			done[i] <- err
		}(i)
	}
	for i := range done {
		for {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("staged%d", i))); err == nil {
				break
			}
			select {
			case err := <-done[i]:
				t.Fatalf("child %d exited before staging: %v", i, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	h.expectRows("SELECT COUNT(*) FROM t", "0")
	for i := range done {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("resume%d", i)), []byte("resume"), 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done[i]:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	h.expectRows("SELECT id FROM t ORDER BY id", "1", "2")
	h.expectRows("SELECT COUNT(*),COUNT(DISTINCT __rowid),MAX(__rowid) FROM t", "2|2|2")
	rows := snapshotExecuteTest(t, s, old, "SELECT id FROM t")
	if len(rows.rows) != 0 {
		t.Fatal("old reader changed", rows.rows)
	}
	pin.release()
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}

func TestSnapshotParallelAppendAllocationReplyLoss(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	observer := &ingestAdmissionObserver{operation: "append_allocate", loseReply: true}
	st, input := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 1, observer)
	if _, err := st.ExecuteUpdate(h.ctx); err == nil {
		t.Fatal("lost allocation reply succeeded")
	}
	if observer.claims.Load() != 1 || input.calls.Load() != 1 {
		t.Fatal("unknown allocation or input was replayed", observer.claims.Load(), input.calls.Load())
	}
	other, _ := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 2, nil)
	if n, err := other.ExecuteUpdate(h.ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	h.expectRows("SELECT id FROM t", "2")
	s := snapshotTestStore(t, raw)
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
	noAdmissionPins(t, h, raw)
}

func TestSnapshotParallelAppendBoundBeforeInput(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	s := snapshotTestStore(t, raw)
	var pins []*snapshotPin
	for range 4 {
		_, pin, err := s.acquireTableWriteSnapshotMode(h.ctx, func(ctx context.Context) (*tableMeta, error) { return s.getTable(ctx, defaultSchema, "t") }, true)
		if err != nil {
			t.Fatal(err)
		}
		pins = append(pins, pin)
		defer pin.release()
	}
	st, input := admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeAppend, 1, nil)
	ctx, cancel := context.WithTimeout(h.ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := st.ExecuteUpdate(ctx); err == nil {
		t.Fatal("fifth stream admitted")
	}
	if input.calls.Load() != 0 {
		t.Fatal("capacity waiter consumed input")
	}
	for _, pin := range pins {
		pin.release()
	}
	noAdmissionPins(t, h, raw)
}

func stageParallelAppend(t *testing.T, h *sqlHarness, s *store, sql string) (context.Context, *snapshotPin, string) {
	t.Helper()
	base, pin, err := s.acquireTableWriteSnapshotMode(h.ctx, func(ctx context.Context) (*tableMeta, error) { return s.getTable(ctx, defaultSchema, "t") }, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		pin.release()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.abortSnapshot(pin, id); pin.release() })
	if err := s.prepareSnapshotTable(ctx, defaultSchema, "t"); err != nil {
		t.Fatal(err)
	}
	snapshotExecuteTest(t, s, ctx, sql)
	return ctx, pin, id
}

func TestSnapshotParallelAppendPublicationRaces(t *testing.T) {
	for _, fault := range []string{"lost_reply", "catalog_cas", "expired_original", "expired_merge"} {
		t.Run(fault, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
			h.exec("CREATE TABLE u(id BIGINT)")
			s := snapshotTestStore(t, raw)
			a, ap, aid := stageParallelAppend(t, h, s, "INSERT INTO t VALUES(1)")
			b, bp, bid := stageParallelAppend(t, h, s, "INSERT INTO t VALUES(2)")
			if err := s.publishSnapshot(a, ap, aid); err != nil {
				t.Fatal(err)
			}
			ap.release()
			observer := &snapshotMergePublicationHook{lose: fault == "lost_reply"}
			if fault == "catalog_cas" {
				observer.before = func() { h.exec("INSERT INTO u VALUES(7)") }
			}
			if fault == "expired_original" || fault == "expired_merge" {
				observer.beforeMerge = func(m snapshotMergeBase) {
					token := bp.token
					if fault == "expired_merge" {
						token = m.Token
					}
					doc, err := raw.HGet(h.ctx, snapshotCatalogKey, "p:"+token).Bytes()
					if err != nil {
						t.Fatal(err)
					}
					var p map[string]any
					if err := json.Unmarshal(doc, &p); err != nil {
						t.Fatal(err)
					}
					p["expires"] = 0
					doc, _ = json.Marshal(p)
					if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+token, doc).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
			s.client.AddHook(observer)
			err := s.publishSnapshot(b, bp, bid)
			if fault == "expired_original" || fault == "expired_merge" {
				if err == nil {
					t.Fatal("expired merge published")
				}
				h.expectRows("SELECT id FROM t", "1")
			} else {
				if err != nil {
					t.Fatal(err)
				}
				h.expectRows("SELECT id FROM t ORDER BY id", "1", "2")
			}
			if fault == "lost_reply" && (!observer.lost.Load() || observer.merges.Load() != 1) {
				t.Fatal("uncertain publication was replayed", observer.merges.Load())
			}
			if fault == "catalog_cas" && observer.merges.Load() != 2 {
				t.Fatal("metadata CAS was not retried exactly once", observer.merges.Load())
			}
			bp.release()
			s.abortSnapshot(bp, bid)
			snapshotReapTest(s, h.ctx)
			assertSnapshotDataReferences(t, h, s)
		})
	}
}

func TestSnapshotParallelAppendCompactionPressure(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, true, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	const writers = 12
	statements := make([]adbc.StatementWithContext, writers)
	for i := range statements {
		statements[i], _ = admissionStatement(t, admissionConnection(t, db), "t", adbc.OptionValueIngestModeCreateAppend, int64(i), nil)
	}
	ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
	defer cancel()
	done := make(chan error, writers)
	for _, st := range statements {
		go func() {
			n, err := st.ExecuteUpdate(ctx)
			if err == nil && n != 1 {
				err = fmt.Errorf("affected=%d", n)
			}
			done <- err
		}()
	}
	for range writers {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	h.expectRows("SELECT COUNT(*),COUNT(DISTINCT id),COUNT(DISTINCT __rowid),SUM(id) FROM t", "12|12|12|66")
	s := snapshotTestStore(t, raw)
	pinned, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(pinned, defaultSchema, "t")
	if err != nil || len(meta.snapshotSegments()) > snapshotMaxSegments {
		t.Fatal("segment bound", meta, err)
	}
	pin.release()
}

func TestSnapshotParallelAppendCoordinatesExclusiveWriters(t *testing.T) {
	for _, sql := range []string{"INSERT INTO t VALUES(2)", "TRUNCATE t", "ALTER TABLE t ADD COLUMN v BIGINT"} {
		t.Run(sql, func(t *testing.T) {
			if os.Getenv("REDIS_SNAPSHOT_FLEX") == "true" && sql == "ALTER TABLE t ADD COLUMN v BIGINT" {
				t.Skip("general ALTER is outside the experimental Flex contract")
			}
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
			s := snapshotTestStore(t, raw)
			ctx, pin, id := stageParallelAppend(t, h, s, "INSERT INTO t VALUES(1)")
			st, err := admissionConnection(t, db).NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, sql); err != nil {
				t.Fatal(err)
			}
			impl := st.(*statementImpl)
			op := "admit_catalog"
			if sql == "INSERT INTO t VALUES(2)" {
				op = "admit_ingest"
			}
			observer := &ingestAdmissionObserver{UniversalClient: impl.conn.store.client, operation: op, busy: make(chan struct{})}
			impl.conn.store.client = observer
			done := make(chan error, 1)
			run, cancel := context.WithTimeout(h.ctx, 10*time.Second)
			defer cancel()
			go func() { _, err := st.ExecuteUpdate(run); done <- err }()
			select {
			case <-observer.busy:
			case err := <-done:
				t.Fatal("exclusive writer bypassed append", err)
			case <-run.Done():
				t.Fatal(run.Err())
			}
			if err := s.publishSnapshot(ctx, pin, id); err != nil {
				t.Fatal(err)
			}
			pin.release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-run.Done():
				t.Fatal(run.Err())
			}
			switch sql {
			case "TRUNCATE t":
				h.expectRows("SELECT COUNT(*) FROM t", "0")
			case "INSERT INTO t VALUES(2)":
				h.expectRows("SELECT id FROM t ORDER BY id", "1", "2")
			default:
				h.expectRows("SELECT id,v FROM t", "1|NULL")
			}
		})
	}
}

func TestSnapshotParallelAppendInterleavedRowRanges(t *testing.T) {
	for _, high := range []bool{false, true} {
		t.Run(fmt.Sprint("high=", high), func(t *testing.T) {
			if high && os.Getenv("REDIS_SNAPSHOT_FLEX") == "true" {
				t.Skip("Flex row IDs are limited to 2^53")
			}
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
			s := snapshotTestStore(t, raw)
			var seed int64
			if high {
				init, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "INSERT INTO t VALUES(0)")
				seed = 9007199254740993
				if err := s.redis(init).Set(init, seqKey(defaultSchema, "t"), seed, 0).Err(); err != nil {
					t.Fatal(err)
				}
				if err := s.publishSnapshot(init, pin, id); err != nil {
					t.Fatal(err)
				}
				pin.release()
			}
			a, ap, aid := stageParallelAppend(t, h, s, "INSERT INTO t VALUES(1)")
			b, bp, bid := stageParallelAppend(t, h, s, "INSERT INTO t VALUES(2)")
			snapshotExecuteTest(t, s, a, "INSERT INTO t VALUES(3)")
			snapshotExecuteTest(t, s, b, "INSERT INTO t VALUES(4)")
			// The later publication has a smaller private counter: merging must retain
			// the prior published maximum instead of overwriting it with that counter.
			if err := s.publishSnapshot(b, bp, bid); err != nil {
				t.Fatal(err)
			}
			bp.release()
			if err := s.publishSnapshot(a, ap, aid); err != nil {
				t.Fatal(err)
			}
			ap.release()
			h.exec("INSERT INTO t VALUES(5)")
			expected := make([]string, 5)
			for i := range expected {
				expected[i] = fmt.Sprintf("%d|%d", i+1, seed+int64(i+1))
			}
			h.expectRows("SELECT id,__rowid FROM t WHERE id>0 ORDER BY id", expected...)
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			counter, err := s.redis(ctx).Get(ctx, seqKey(defaultSchema, "t")).Int64()
			if err != nil || counter != seed+5 {
				t.Fatal("lost high water", counter, err)
			}
		})
	}
}

func TestSnapshotParallelAppendConservativeColumnFacts(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(s TEXT, f DOUBLE PRECISION)")
	s := snapshotTestStore(t, raw)
	binary := string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(binary, 0) {
		t.Fatal("fixture has no zero byte")
	}
	a, ap, aid := stageParallelAppend(t, h, s, "INSERT INTO t VALUES('"+binary+"',1)")
	b, bp, bid := stageParallelAppend(t, h, s, "INSERT INTO t VALUES('plain','NaN'::DOUBLE PRECISION)")
	if err := s.publishSnapshot(a, ap, aid); err != nil {
		t.Fatal(err)
	}
	ap.release()
	if err := s.publishSnapshot(b, bp, bid); err != nil {
		t.Fatal(err)
	}
	bp.release()
	h.expectRows("SELECT s FROM t WHERE s='"+binary+"'", binary)
	h.expectRows("SELECT COUNT(*) FROM t WHERE f='NaN'::DOUBLE PRECISION", "1")
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, defaultSchema, "t")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Columns[0].TagValues != tagsTruncated || !meta.Columns[1].NaNs {
		t.Fatal("lost conservative column facts", meta.Columns)
	}
}
