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
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

func snapshotDrainCompactionTest(t *testing.T, s *store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token := snapshotID()
	for i := 0; i < 10000; i++ {
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		progress, err := s.snapshotCompactionStep(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		if !progress {
			job, err := s.readSnapshotCompaction(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if job == nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("compaction failed to finish bounded steps")
}
func snapshotExecRetryTest(t *testing.T, h *sqlHarness, sql string, debug ...*store) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = st.SetSqlQuery(h.ctx, sql); err != nil {
			t.Fatal(err)
		}
		_, err = st.ExecuteUpdate(h.ctx)
		_ = st.Close(h.ctx)
		if err == nil {
			return
		}
		var ae adbc.Error
		if !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" || time.Now().After(deadline) {
			if len(debug) > 0 {
				if j, _ := debug[0].readSnapshotCompaction(h.ctx); j != nil {
					t.Logf("job phase=%s after=%s error=%s", j.Phase, j.After, j.LastError)
				}
			}
			t.Fatalf("%s: %v", sql, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSnapshotBackgroundCompactionSustainsWritesWithPinnedStream(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	snapshotExecRetryTest(t, h, "CREATE TABLE t(id INTEGER,v INTEGER)")
	snapshotExecRetryTest(t, h, "INSERT INTO t SELECT i,10 FROM generate_series(1,2001) AS g(i)")
	snapshotExecRetryTest(t, h, "UPDATE t SET v=11 WHERE __rowid=1")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionIntStreamBatchRows, "7"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
		t.Fatal(err)
	}
	rdr, n, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Release()
	if n != -1 || !rdr.Next() {
		t.Fatalf("stream: %d %v", n, rdr.Err())
	}
	debug := snapshotTestStore(t, raw)
	for i := 0; i < 30; i++ {
		snapshotExecRetryTest(t, h, fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i), debug)
	}
	count := 0
	for {
		rec := rdr.RecordBatch()
		ids := rec.Column(0).(*array.Int32)
		vs := rec.Column(1).(*array.Int32)
		for i := 0; i < int(rec.NumRows()); i++ {
			want := int32(10)
			if ids.Value(i) == 1 {
				want = 11
			}
			if vs.Value(i) != want {
				t.Fatal("old stream changed during background compaction")
			}
			count++
		}
		if !rdr.Next() {
			break
		}
	}
	if err := rdr.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2001 {
		t.Fatalf("old stream lost rows: %d", count)
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "2001|10|129")
	// The worker continues after statements stop; the latest root eventually
	// falls below the soft threshold without executing SQL to do compaction.
	maintenance := snapshotTestStore(t, raw)
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, pin, err := maintenance.acquireSnapshot(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		m := snapshotTableTest(t, maintenance, ctx, "t")
		pin.release()
		if len(m.snapshotSegments()) < snapshotCompactionTrigger {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle background worker did not compact")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSnapshotCompactionBackpressureDoesNoRowWork(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	for i := 0; i < snapshotMaxSegments-1; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", i))
	}
	s := snapshotTestStore(t, raw)
	counter := &snapshotDeltaWrites{prefixes: map[string]bool{}, indexes: map[string]bool{}}
	s.client.AddHook(counter)
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	parsed, err := ParseScript("UPDATE t SET v=99 WHERE __rowid=1")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	_, err = e.execute(h.ctx, parsed[0], nil, nil, nil)
	var ae adbc.Error
	if !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" || !strings.Contains(err.Error(), "background compaction") {
		t.Fatalf("backpressure: %v", err)
	}
	if counter.rows != 0 || len(counter.indexes) != 0 || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("ordinary write performed compaction or published at hard limit")
	}
	snapshotDrainCompactionTest(t, s)
	h.exec("UPDATE t SET v=99 WHERE __rowid=1")
	h.expectRows("SELECT v FROM t", "99")
}

// Drive one existing job without starting another, so tests can inspect the
// first installation and its newer tail independently of later compactions.
func snapshotCompleteJobTest(t *testing.T, s *store, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		j, err := s.readSnapshotCompaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			return
		}
		if _, err := s.snapshotCompactionStep(ctx, token); err != nil {
			t.Fatal(err)
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("compaction stalled: %v phase=%s error=%s", err, j.Phase, j.LastError)
		}
	}
}
func TestSnapshotCompactionResumeFencesOldOwnerAndGraftsTail(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,10 FROM generate_series(1,1001) AS g(i)")
	for i := 0; i < 3; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
	}
	s := snapshotTestStore(t, raw)
	oldToken := snapshotID()
	if progress, err := s.snapshotCompactionStep(h.ctx, oldToken); err != nil || !progress {
		t.Fatalf("start: %v %v", progress, err)
	}
	if _, err := s.snapshotCompactionStep(h.ctx, oldToken); err != nil {
		t.Fatal(err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if job.After == "" || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != snapshotCompactionBatchRows {
		t.Fatal("first batch was not checkpointed/bounded")
	}
	oldJob := *job
	oldCtx := s.compactionWriteContext(h.ctx, &oldJob)
	// A process crash leaves the checkpoint/output protected, including after
	// its lease expires and unrelated reclamation runs.
	var owner map[string]any
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+job.ID).Val()), &owner); err != nil {
		t.Fatal(err)
	}
	owner["expires"] = 0
	record, _ := json.Marshal(owner)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+job.ID, record).Err(); err != nil {
		t.Fatal(err)
	}
	snapshotReapTest(s, h.ctx)
	if raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != snapshotCompactionBatchRows {
		t.Fatal("expired durable output reclaimed")
	}
	resumed := snapshotTestStore(t, raw)
	token := snapshotID()
	if _, err := resumed.snapshotCompactionStep(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	progress, err := resumed.readSnapshotCompaction(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Data != job.Data || progress.After <= job.After || progress.Token == oldToken {
		t.Fatal("new owner did not resume the existing checkpoint")
	}
	if err := s.client.HSet(oldCtx, snapshotRows(job.Data)+"1002", rowIDField, "1002", "v", "999").Err(); err == nil {
		t.Fatal("expired owner wrote after takeover")
	}
	if raw.Exists(h.ctx, snapshotRows(job.Data)+"1002").Val() != 0 {
		t.Fatal("stale writer reached Redis")
	}
	// These commits happen while source copying is incomplete. Installation must
	// replace only the source prefix, preserving all subsequent deltas/sequence.
	h.exec("UPDATE t SET v=500 WHERE __rowid=1")
	h.exec("DELETE FROM t WHERE __rowid=2")
	h.exec("INSERT INTO t VALUES(1002,77)")
	for {
		job, err := resumed.readSnapshotCompaction(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job.Phase != "copy" {
			sealed := resumed.compactionWriteContext(h.ctx, job)
			if err := resumed.client.HSet(sealed, snapshotRows(job.Data)+"2000", rowIDField, "2000", "v", "999").Err(); err == nil {
				t.Fatal("copy mutation reached sealed output")
			}
			if raw.Exists(h.ctx, snapshotRows(job.Data)+"2000").Val() != 0 {
				t.Fatal("sealed output changed")
			}
			break
		}
		if _, err := resumed.snapshotCompactionStep(h.ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	snapshotCompleteJobTest(t, resumed, token)
	ctx, pin, err := resumed.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	current := snapshotTableTest(t, resumed, ctx, "t")
	if len(current.snapshotSegments()) != 4 || snapshotDataID(current.SnapshotSegments[0].Prefix) != job.Data {
		t.Fatal("compaction replaced newer tail segments")
	}
	h.expectRows("SELECT COUNT(*),SUM(v) FROM t", "1001|10567")
	h.expectRows("SELECT v FROM t WHERE __rowid=1", "500")
	h.expectRows("SELECT COUNT(*) FROM t WHERE __rowid=2", "0")
	h.expectRows("SELECT id,v FROM t WHERE __rowid=1002", "1002|77")
}

func TestSnapshotCompactionDropAndTruncateDiscardObsoleteJob(t *testing.T) {
	for _, sql := range []string{"DROP TABLE t", "TRUNCATE t"} {
		t.Run(sql, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(v INTEGER)")
			h.exec("INSERT INTO t VALUES(1),(2),(3)")
			for i := 0; i < 3; i++ {
				h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
			}
			s := snapshotTestStore(t, raw)
			token := snapshotID()
			if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			job, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || job == nil {
				t.Fatal(err)
			}
			h.exec(sql)
			if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			if j, _ := s.readSnapshotCompaction(h.ctx); j != nil {
				t.Fatal("obsolete job remained active")
			}
			snapshotReapTest(s, h.ctx)
			if raw.Exists(h.ctx, snapshotRows(job.Data)+"1", snapshotMembers(job.Data)).Val() != 0 {
				t.Fatal("obsolete output/directory leaked")
			}
			if sql == "TRUNCATE t" {
				h.expectRows("SELECT COUNT(*) FROM t", "0")
			} else {
				h.expectError("SELECT * FROM t", "does not exist")
			}
		})
	}
}

type snapshotCompactionBatchTracker struct {
	snapshotHook
	maxRows, totalRows, maxBytes int
}

func (h *snapshotCompactionBatchTracker) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if len(a) > 2 && cmd.Name() == "eval" && strings.Contains(fmt.Sprint(a[1]), "snapshot atomic compaction copy batch") {
			var keys int
			fmt.Sscan(fmt.Sprint(a[2]), &keys)
			rows, bytes := keys-3, 0
			for _, key := range a[6 : 3+keys] {
				bytes += len(fmt.Sprint(key))
			}
			for _, arg := range a[3+keys+7:] {
				bytes += len(fmt.Sprint(arg))
			}
			h.totalRows += rows
			h.maxRows = max(h.maxRows, rows)
			h.maxBytes = max(h.maxBytes, bytes)
		}
		return next(ctx, cmd)
	}
}
func (h *snapshotCompactionBatchTracker) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		rows, bytes := 0, 0
		for _, cmd := range cmds {
			if wrapped, ok := cmd.(snapshotFencedCommand); ok {
				cmd = wrapped.Cmder
			}
			a := cmd.Args()
			if len(a) > 1 && strings.EqualFold(fmt.Sprint(a[0]), "HSET") && snapshotRowPrefix(fmt.Sprint(a[1])) != "" {
				rows++
				for _, arg := range a[1:] {
					bytes += len(fmt.Sprint(arg))
				}
			}
		}
		h.totalRows += rows
		h.maxRows = max(h.maxRows, rows)
		h.maxBytes = max(h.maxBytes, bytes)
		return next(ctx, cmds)
	}
}
func TestSnapshotCompactionBatchesBoundBytes(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,payload VARCHAR)")
	h.exec("INSERT INTO t SELECT i,'" + strings.Repeat("x", 65536) + "' FROM generate_series(1,100) AS g(i)")
	for i := 0; i < 3; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET id=%d WHERE __rowid=1", 100+i))
	}
	s := snapshotTestStore(t, raw)
	tracker := &snapshotCompactionBatchTracker{}
	s.client.AddHook(tracker)
	snapshotDrainCompactionTest(t, s)
	if tracker.totalRows != 100 || tracker.maxRows > snapshotCompactionBatchRows || tracker.maxBytes > snapshotCompactionBatchBytes {
		t.Fatalf("unbounded copy batches: %+v", tracker)
	}
	h.expectRows("SELECT COUNT(*),MIN(length(payload)),MAX(length(payload)) FROM t", "100|65536|65536")
}

func TestSnapshotCompactionExceedsStatementRowBudgetInBatches(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,10 FROM generate_series(1,100000) AS g(i)")
	h.exec("INSERT INTO t VALUES(100001,10)")
	h.exec("UPDATE t SET v=11 WHERE __rowid=1")
	h.exec("UPDATE t SET v=12 WHERE __rowid=1")
	s := snapshotTestStore(t, raw)
	tracker := &snapshotCompactionBatchTracker{}
	s.client.AddHook(tracker)
	token := snapshotID()
	ctx, cancel := context.WithTimeout(h.ctx, 2*time.Minute)
	defer cancel()
	if _, err := s.snapshotCompactionStep(ctx, token); err != nil {
		t.Fatal(err)
	}
	for {
		job, err := s.readSnapshotCompaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if _, err := s.snapshotCompactionStep(ctx, token); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	if tracker.totalRows != 100001 || tracker.totalRows <= snapshotMaxWrites || tracker.maxRows > snapshotCompactionBatchRows {
		t.Fatalf("whole-job row budget or oversized batch: %+v", tracker)
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "100001|10|12")
	h.exec("UPDATE t SET v=13 WHERE __rowid=1")
	h.expectRows("SELECT v FROM t WHERE __rowid=1", "13")
}

type snapshotPauseCompaction struct {
	snapshotHook
	reached chan struct{}
	once    sync.Once
}

func (h *snapshotPauseCompaction) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if compactionPhysicalCopyRows(cmd) >= 0 {
			h.once.Do(func() { close(h.reached) })
			<-ctx.Done()
			cmd.SetErr(ctx.Err())
			return ctx.Err()
		}
		return next(ctx, cmd)
	}
}
func (h *snapshotPauseCompaction) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if wrapped, ok := cmd.(snapshotFencedCommand); ok {
				cmd = wrapped.Cmder
			}
			a := cmd.Args()
			if len(a) > 1 && strings.EqualFold(fmt.Sprint(a[0]), "HSET") && snapshotRowPrefix(fmt.Sprint(a[1])) != "" {
				h.once.Do(func() { close(h.reached) })
				<-ctx.Done()
				return ctx.Err()
			}
		}
		return next(ctx, cmds)
	}
}
func TestSnapshotCompactionCloseReleasesRecoverableJob(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,501) AS g(i)")
	for i := 0; i < 3; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	before, err := s.readSnapshotCompaction(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A real background controller waits behind the test's lease until its
	// cancellation-aware hook is installed, then takes over the durable job.
	owner := snapshotTestStore(t, raw)
	opts := *raw.Options()
	opts.ContextTimeoutEnabled = true
	barrier := &snapshotPauseCompaction{reached: make(chan struct{})}
	owner.startSnapshotCompactor(newClientConfig(&opts), timeouts{}, barrier)
	owner.mu.Lock()
	worker := owner.compactor
	owner.mu.Unlock()

	defer owner.stopSnapshotCompactor()
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	owner.wakeSnapshotCompactor()
	select {
	case <-barrier.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach copy barrier")
	}
	owner.stopSnapshotCompactor()
	select {
	case <-worker.done:
	default:
		t.Fatal("compactor survived connection close")
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.After != before.After || job.Data != before.Data {
		t.Fatalf("close discarded checkpoint: %+v %v", job, err)
	}
	var record struct {
		Expires int64 `json:"expires"`
	}
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+job.ID).Val()), &record); err != nil {
		t.Fatal(err)
	}
	if record.Expires != 0 {
		t.Fatal("cooperative close did not release job lease")
	}
	snapshotCompleteJobTest(t, s, snapshotID())
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "501|2|501")
}

func TestSnapshotCompactionPublicationYieldsToWriteIntent(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1),(2)")
	for i := 0; i < 3; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	_, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()

	for {
		if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
			t.Fatal(err)
		}
		j, err := s.readSnapshotCompaction(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if j != nil && j.Phase == "install" {
			break
		}
	}
	// The existing writer predates the ready gate; new writers yield while
	// maintenance waits for that writer to finish.
	waiting, cancel := context.WithTimeout(h.ctx, 30*time.Millisecond)
	_, newPin, admissionErr := s.acquireSnapshot(waiting, true)
	cancel()
	if newPin != nil {
		newPin.release()
		t.Fatal("ready compactor admitted a writer before its gate reopened")
	}
	var ae adbc.Error
	if !errors.As(admissionErr, &ae) || ae.Code != adbc.StatusTimeout {
		t.Fatalf("new writer did not wait at the publication gate: %v", admissionErr)
	}
	before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if progress, err := s.snapshotCompactionStep(h.ctx, token); err != nil || progress {
		t.Fatalf("maintenance did not yield: %v %v", progress, err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
		t.Fatal("background publication invalidated a writer's acquired root")
	}
	pin.release()
	snapshotCompleteJobTest(t, s, token)
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() == before {
		t.Fatal("publication did not resume after writer release")
	}
	h.expectRows("SELECT v FROM t ORDER BY __rowid", "102", "2")
}
