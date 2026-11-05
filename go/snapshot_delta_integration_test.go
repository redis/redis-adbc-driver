// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotDeltaWrites struct {
	snapshotHook
	rows     int
	prefixes map[string]bool
	indexes  map[string]bool
}

func (h *snapshotDeltaWrites) check(cmd goredis.Cmder) {
	for _, key := range snapshotInsertBatchRowKeys(cmd) {
		h.rows++
		h.prefixes[snapshotDataID(fmt.Sprint(key))] = true
	}
	if wrapped, ok := cmd.(snapshotFencedCommand); ok {
		cmd = wrapped.Cmder
	}
	a := cmd.Args()
	if len(a) < 2 {
		return
	}
	name, key := strings.ToLower(fmt.Sprint(a[0])), fmt.Sprint(a[1])
	if name == "hset" && snapshotRowPrefix(key) != "" {
		h.rows++
		h.prefixes[snapshotDataID(key)] = true
	}
	if name == "ft.create" {
		h.indexes[key] = true
	}
}
func (h *snapshotDeltaWrites) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error { h.check(c); return next(ctx, c) }
}
func (h *snapshotDeltaWrites) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		for _, c := range cs {
			h.check(c)
		}
		return next(ctx, cs)
	}
}
func TestSnapshotDeltaOneRowWriteAndVisibility(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT,payload VARCHAR,nullable INTEGER)")
	h.exec("INSERT INTO t SELECT i,i,'unchanged',NULL FROM generate_series(1,1001) AS g(i)")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	base := snapshotTableTest(t, s, old, "t")
	before := raw.Dump(h.ctx, base.prefix()+"1").Val()
	counter := &snapshotDeltaWrites{prefixes: map[string]bool{}, indexes: map[string]bool{}}
	s.client.AddHook(counter)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t SET v=9007199254740993 WHERE __rowid=1")
	if counter.rows != 1 || len(counter.prefixes) != 1 {
		t.Fatalf("one-row UPDATE wrote %d row images in %d prefixes", counter.rows, len(counter.prefixes))
	}
	if counter.indexes[base.index()] {
		t.Fatal("UPDATE rebuilt immutable base index")
	}
	if raw.Dump(h.ctx, base.prefix()+"1").Val() != before {
		t.Fatal("UPDATE changed old row image")
	}
	now, np, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := snapshotTableTest(t, s, now, "t")
	np.release()
	if !next.SnapshotOverlay || len(next.SnapshotSegments) != 1 || next.SnapshotSegments[0].Prefix != base.prefix() || next.SnapshotSegments[0].Index != base.index() || next.SnapshotRows == nil || *next.SnapshotRows != 1 {
		t.Fatalf("unexpected delta manifest: %+v", next)
	}
	if raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(next.prefix()))).Val() != 1 {
		t.Fatal("delta directory is not change-sized")
	}
	h.expectRows("SELECT id,v,payload,nullable FROM t WHERE __rowid=1", "1|9007199254740993|unchanged|NULL")
	h.expectRows("SELECT COUNT(*),SUM(v) FROM t", "1001|9007199255242493")
	h.expectRows("SELECT id FROM t WHERE v<=3 ORDER BY id LIMIT 10", "2", "3")
	h.expectRows("SELECT id FROM t ORDER BY v LIMIT 2", "2", "3")
	h.expectRows("SELECT id FROM t LIMIT 1 OFFSET 1", "2")
	if res := snapshotExecuteTest(t, s, old, "SELECT v FROM t WHERE __rowid=1"); len(res.rows) != 1 || res.rows[0][0].I != 1 {
		t.Fatalf("old snapshot changed: %v", res.rows)
	}
	counter.rows = 0
	snapshotExecuteTest(t, s, h.ctx, "DELETE FROM t WHERE __rowid=2 RETURNING id")
	if counter.rows != 1 {
		t.Fatalf("one-row DELETE wrote %d row images", counter.rows)
	}
	h.expectRows("SELECT COUNT(*),SUM(v) FROM t", "1000|9007199255242491")
	h.expectRows("SELECT id FROM t WHERE v<=3 ORDER BY id", "3")
	h.expectRows("SELECT COUNT(*) FROM t WHERE __rowid=2", "0")
	counter.rows = 0
	snapshotExecuteTest(t, s, h.ctx, "INSERT INTO t VALUES(1002,5,'new',42)")
	if counter.rows != 1 {
		t.Fatalf("INSERT copied rows: %d", counter.rows)
	}
	h.expectRows("SELECT id,v,payload,nullable FROM t WHERE __rowid=1002", "1002|5|new|42")
	h.expectRows("SELECT COUNT(*) FROM t", "1001")
}

func TestSnapshotDeltaPredicatesAggregatesAndRepeatedChanges(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,g VARCHAR,v INTEGER,payload VARCHAR)")
	h.exec("INSERT INTO t VALUES(1,'a',10,'first'),(2,'a',20,'second'),(3,'b',30,'third'),(4,'c',40,'fourth')")
	h.exec("UPDATE t SET g='b',v=100,payload=NULL WHERE id=1")
	h.exec("DELETE FROM t WHERE id=3")
	h.exec("UPDATE t SET v=200 WHERE id=1 RETURNING id,v,payload")
	h.expectRows("SELECT id,g,v,payload FROM t ORDER BY id", "1|b|200|NULL", "2|a|20|second", "4|c|40|fourth")
	h.expectRows("SELECT g,COUNT(*),SUM(v) FROM t GROUP BY g ORDER BY g", "a|1|20", "b|1|200", "c|1|40")
	h.expectRows("SELECT COUNT(DISTINCT g),SUM(v),MIN(v),MAX(v) FROM t", "3|260|20|200")
	h.expectRows("SELECT id FROM t WHERE g='a' ORDER BY id LIMIT 1", "2")
	h.expectRows("SELECT id FROM t WHERE v<100 ORDER BY v DESC LIMIT 1", "4")
	h.expectRows("SELECT id FROM t WHERE EXISTS(SELECT 1 FROM t AS x WHERE x.id=t.id AND x.v>100)", "1")
	h.expectRows("SELECT a.id,b.v FROM t AS a JOIN t AS b ON a.id=b.id ORDER BY a.id", "1|200", "2|20", "4|40")
	h.exec("UPDATE t SET v=v+1 WHERE v>=100")
	h.expectRows("SELECT v FROM t WHERE id=1", "201")
	h.exec("DELETE FROM t WHERE g='b'")
	h.expectRows("SELECT id FROM t ORDER BY id", "2", "4")
	h.exec("TRUNCATE t RESTART IDENTITY")
	h.exec("INSERT INTO t VALUES(9,'z',9,'reset')")
	h.expectRows("SELECT __rowid,id,g,v,payload FROM t", "1|9|z|9|reset")
}

func TestSnapshotDeltaCompactionRetainsStreamingReader(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,10 FROM generate_series(1,1001) AS g(i)")
	h.exec("UPDATE t SET v=11 WHERE __rowid=1")
	s := snapshotTestStore(t, raw)
	old, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	before := snapshotTableTest(t, s, old, "t")
	oldSegments := before.snapshotSegments()
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	if err := stmt.SetOption(h.ctx, OptionIntStreamBatchRows, "7"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
		t.Fatal(err)
	}
	reader, n, err := stmt.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if n != -1 || !reader.Next() {
		t.Fatalf("overlay did not stream: n=%d err=%v", n, reader.Err())
	}
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	writer := &sqlHarness{t: t, ctx: h.ctx, conn: other, afterExec: h.afterExec}
	sawCompaction := false
	previous := len(oldSegments)
	// Include a tombstone before compaction so copied output must exclude it.
	writer.exec("DELETE FROM t WHERE __rowid=2")
	for i := 0; i < snapshotMaxSegments+2; i++ {
		writer.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
		ctx, p, err := s.acquireSnapshot(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		m := snapshotTableTest(t, s, ctx, "t")
		p.release()
		count := len(m.snapshotSegments())
		if count < previous {
			sawCompaction = true
		}
		previous = count
		if count > snapshotMaxSegments {
			t.Fatal("segment bound exceeded")
		}
	}
	if !sawCompaction {
		t.Fatal("segment threshold did not compact")
	}
	snapshotReapTest(s, h.ctx)
	for _, seg := range oldSegments {
		if err := raw.Do(h.ctx, "FT.INFO", seg.Index).Err(); err != nil {
			t.Fatalf("compaction reclaimed pinned index: %v", err)
		}
	}
	count := 0
	for {
		rec := reader.RecordBatch()
		ids := rec.Column(0).(*array.Int32)
		values := rec.Column(1).(*array.Int32)
		for i := 0; i < int(rec.NumRows()); i++ {
			want := int32(10)
			if ids.Value(i) == 1 {
				want = 11
			}
			if values.Value(i) != want {
				t.Fatalf("mixed snapshot at id=%d: %d", ids.Value(i), values.Value(i))
			}
			count++
		}
		if !reader.Next() {
			break
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 1001 {
		t.Fatalf("old stream lost rows: %d", count)
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "1000|10|109")
	pin.release()
	snapshotReapTest(s, h.ctx)
	for _, seg := range oldSegments {
		if raw.Exists(h.ctx, seg.Prefix+"1", snapshotMembers(snapshotDataID(seg.Prefix))).Val() != 0 {
			t.Fatal("compacted unreferenced data/directory leaked")
		}
		if err := raw.Do(h.ctx, "FT.INFO", seg.Index).Err(); err == nil {
			t.Fatal("compacted unreferenced index leaked")
		}
	}
}

func TestSnapshotDeltaDirectoryDamageFailsClosed(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1),(2)")
	h.exec("UPDATE t SET v=10 WHERE __rowid=1")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta := snapshotTableTest(t, s, ctx, "t")
	if err := raw.Del(h.ctx, meta.prefix()+"1").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.fetchSnapshotVisible(ctx, meta, []string{meta.prefix() + "1"}, []string{"v"}); err == nil {
		t.Fatal("missing replacement fell back to an old row")
	}
	if err := raw.HSet(h.ctx, meta.prefix()+"1", rowIDField, "2", "v", "10").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.fetchSnapshotVisible(ctx, meta, []string{meta.prefix() + "1"}, []string{"v"}); err == nil {
		t.Fatal("direct lookup accepted mismatched row identity")
	}
	if err := raw.Del(h.ctx, meta.prefix()+"1").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Del(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.fetchSnapshotVisible(ctx, meta, []string{meta.prefix() + "1"}, []string{"v"}); err == nil {
		t.Fatal("missing row and directory exposed an older version")
	}
	// Removing the entire inherited directory must fail a completed scan rather
	// than silently turn it into an empty or partial result.
	if err := raw.Del(h.ctx, snapshotMembers(snapshotDataID(meta.SnapshotSegments[0].Prefix))).Err(); err != nil {
		t.Fatal(err)
	}
	scan := newSnapshotDeltaScan(s, meta, []string{})
	if _, _, err := scan.next(ctx); err == nil {
		t.Fatal("missing inherited directory produced a successful partial page")
	}
}

type snapshotFailCompaction struct {
	snapshotHook
	prefix string
}

func (h *snapshotFailCompaction) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	partial := &compactionPartialCopyHook{}
	inject := partial.ProcessHook(next)
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if compactionPhysicalCopyRows(cmd) > 0 && h.prefix == "" {
			h.prefix = snapshotRowPrefix(fmt.Sprint(cmd.Args()[6]))
		}
		return inject(ctx, cmd)
	}
}
func TestSnapshotDeltaFailedCompactionDoesNotPublish(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t VALUES(1,10),(2,20)")
	for i := 0; i < snapshotCompactionTrigger-1; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if progress, err := s.snapshotCompactionStep(h.ctx, token); err != nil || !progress {
		t.Fatalf("start: %v %v", progress, err)
	}
	before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	hook := &snapshotFailCompaction{}
	s.client.AddHook(hook)
	if _, err := s.snapshotCompactionStep(h.ctx, token); err == nil {
		t.Fatal("injected compaction error ignored")
	}
	if hook.prefix == "" {
		t.Fatal("failure did not reach compaction row writes")
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
		t.Fatal("failed compaction published")
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.After != "" || job.LastError == "" {
		t.Fatalf("missing durable error/checkpoint: %+v %v", job, err)
	}
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|102", "2|20")
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	replacement := snapshotTestStore(t, raw)
	snapshotDrainCompactionTest(t, replacement)
	ctx, pin, err := replacement.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	current := snapshotTableTest(t, replacement, ctx, "t")
	if current.prefix() != hook.prefix {
		t.Fatal("recovery discarded durable copied output")
	}
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|102", "2|20")
}

func TestSnapshotDeltaExactRowIDsBeyondDoublePrecision(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	if err := s.redis(wctx).Set(wctx, seqKey("public", "t"), "9007199254740991", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatal(err)
	}
	h.exec("INSERT INTO t VALUES(1)")
	h.exec("INSERT INTO t VALUES(2)")
	h.expectRows("SELECT __rowid,v FROM t", "9007199254740992|1", "9007199254740993|2")
	h.exec("UPDATE t SET v=22 WHERE __rowid=9007199254740993")
	h.exec("DELETE FROM t WHERE __rowid=9007199254740992")
	h.expectRows("SELECT __rowid,v FROM t", "9007199254740993|22")
	h.expectRows("SELECT COUNT(*) FROM t WHERE __rowid=9007199254740992", "0")
}
