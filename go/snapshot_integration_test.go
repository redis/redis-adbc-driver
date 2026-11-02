// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/adbc-drivers/redis/go/internal/tlstrust"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// REDIS_SNAPSHOT_URI must name a DISPOSABLE database. These tests initialize a
// different storage format and clear that database before/after each test.
func newSnapshotHarness(t *testing.T) (*sqlHarness, *goredis.Client, adbc.DatabaseWithContext) {
	return newSnapshotHarnessMode(t, false, true)
}

// Protocol tests drive the same durable worker in explicit bounded steps so
// races with deliberately corrupted indexes/leases remain deterministic. The
// background integration/native tests use automatic production workers.
func newSnapshotHarnessMode(t *testing.T, background, drain bool) (*sqlHarness, *goredis.Client, adbc.DatabaseWithContext) {
	t.Helper()
	uri := os.Getenv("REDIS_SNAPSHOT_URI")
	if uri == "" {
		t.Skip("set REDIS_SNAPSHOT_URI to a disposable Redis Software database")
	}
	opts, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal(err)
	}
	opts.DisableIdentity = true
	if opts.TLSConfig != nil {
		opts.TLSConfig.RootCAs, err = tlstrust.Roots()
		if err != nil {
			t.Fatal(err)
		}
	}
	raw := goredis.NewClient(opts)
	ctx := context.Background()
	if err := raw.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	// FLUSHDB does not drop Search definitions. This explicitly disposable
	// database must have neither old keys nor indexes before initialization.
	indexes, err := raw.Do(ctx, "FT._LIST").StringSlice()
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		_ = raw.Do(ctx, "FT.DROPINDEX", index).Err()
	}
	t.Cleanup(func() {
		indexes, _ := raw.Do(ctx, "FT._LIST").StringSlice()
		for _, index := range indexes {
			_ = raw.Do(ctx, "FT.DROPINDEX", index).Err()
		}
		_ = raw.FlushDB(ctx).Err()
		_ = raw.Close()
	})
	impl, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, map[string]string{adbc.OptionKeyURI: uri})
	if err != nil {
		t.Fatal(err)
	}
	impl.snapshotCompactionDisabled = !background
	db := driverbase.NewDatabase(impl)
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx); _ = db.Close(ctx) })
	h := &sqlHarness{t: t, ctx: ctx, conn: conn}
	if drain {
		maintenance := snapshotTestStore(t, raw)
		h.afterExec = func() { snapshotDrainCompactionTest(t, maintenance) }
	}
	return h, raw, db
}

func snapshotTestStore(t *testing.T, raw *goredis.Client) *store {
	t.Helper()
	opts := *raw.Options()
	opts.ContextTimeoutEnabled = true
	c := goredis.NewClient(&opts)
	c.AddHook(snapshotHook{})
	t.Cleanup(func() { _ = c.Close() })
	return &store{client: c, snapshots: true}
}

func TestSnapshotStreamRetainsGeneration(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (id INTEGER, v BIGINT, a VARCHAR, b VARCHAR, c VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,'a','b','c' FROM generate_series(1,12001) AS g(i)")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: other}
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
	if n != -1 {
		t.Fatalf("query did not stream: %d", n)
	}
	oldRoot := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if !rdr.Next() {
		t.Fatalf("first batch: %v", rdr.Err())
	}
	count := 0
	check := func() {
		r := rdr.RecordBatch()
		values := r.Column(1).(*array.Int64)
		for i := 0; i < int(r.NumRows()); i++ {
			if values.Value(i) != 9007199254740993 {
				t.Fatalf("mixed generation at row %d: %d", count, values.Value(i))
			}
			count++
		}
	}
	check()
	h2.exec("UPDATE t SET v=42")
	h2.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "12001|42|42")
	if n := raw.Exists(h.ctx, snapshotWorkspace(oldRoot)+strings.TrimPrefix(metaKey("public", "t"), metaPrefix)).Val(); n != 1 {
		t.Fatal("pinned metadata reclaimed")
	}
	for rdr.Next() {
		check()
	}
	if err := rdr.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 12001 {
		t.Fatalf("read %d rows", count)
	}
	h2.expectRows("SELECT COUNT(*) FROM t", "12001") // reclamation after exhaustion
	if n := raw.Exists(h.ctx, snapshotWorkspace(oldRoot)+strings.TrimPrefix(metaKey("public", "t"), metaPrefix)).Val(); n != 0 {
		t.Fatal("released generation not reclaimed")
	}
}

func TestSnapshotConflictsAndExpiredWriterFence(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (v INTEGER)")
	s := snapshotTestStore(t, raw)
	ctx1, p1, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer p1.release()
	ctx2, p2, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.release()
	w1, id1, err := s.beginSnapshotWrite(ctx1, p1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(p1, id1)
	w2, id2, err := s.beginSnapshotWrite(ctx2, p2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(p2, id2)
	for i, ctx := range []context.Context{w1, w2} {
		if err := s.prepareSnapshotTable(ctx, "public", "t"); err != nil {
			t.Fatal(err)
		}
		meta, err := s.getTable(ctx, "public", "t")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.insertRows(ctx, meta, [][]Value{{intValue(typeInt32, int64(i+1))}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.publishSnapshot(w1, p1, id1); err != nil {
		t.Fatal(err)
	}
	var ae adbc.Error
	if err := s.publishSnapshot(w2, p2, id2); !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" {
		t.Fatalf("expected serialization conflict, got %v", err)
	}
	h.expectRows("SELECT v FROM t", "1")
	// Expire a private writer and reclaim it while its Go context is still
	// usable. Both HASH mutations and index creation must be server-fenced.
	loser, err := s.getTable(w2, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	rawRecord, _ := json.Marshal(map[string]any{"state": "staging", "token": p2.token, "base": p2.root, "expires": 0})
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+id2, rawRecord).Err(); err != nil {
		t.Fatal(err)
	}
	s.reapSnapshot(h.ctx)
	key := loser.prefix() + "late:1"
	if err := s.client.HSet(w2, key, rowIDField, "1").Err(); err == nil {
		t.Fatal("expired writer resurrected a HASH")
	}
	if raw.Exists(h.ctx, key).Val() != 0 {
		t.Fatal("expired write reached Redis")
	}
	meta := &tableMeta{KeyPrefix: loser.prefix(), IndexName: snapshotIndexPrefix(snapshotDataID(loser.prefix())) + "late"}
	if err := s.searchDo(w2, meta.index(), indexCreateArgs(meta)...).Err(); err == nil {
		t.Fatal("expired writer created an index")
	}
}

type snapshotLostReply struct {
	snapshotHook
	lost atomic.Bool
}

func (h *snapshotLostReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if err == nil && len(args) > 4 && fmt.Sprint(args[0]) == "EVAL" && args[1] == snapshotControl && args[4] == "publish" && h.lost.CompareAndSwap(false, true) {
			cmd.SetErr(io.ErrUnexpectedEOF)
			return io.ErrUnexpectedEOF
		}
		return err
	}
}

func TestSnapshotLostPublicationReply(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	s := snapshotTestStore(t, raw)
	hook := &snapshotLostReply{}
	s.client.AddHook(hook)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.prepareSnapshotTable(wctx, "public", "t"); err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(wctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.insertRows(wctx, meta, [][]Value{{intValue(typeInt32, 7)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		t.Fatalf("durable outcome did not resolve lost reply: %v", err)
	}
	if !hook.lost.Load() {
		t.Fatal("lost-reply hook not exercised")
	}
	h.expectRows("SELECT v FROM t", "7")
}

type snapshotPendingIndex struct {
	snapshotHook
	reads atomic.Int32
}

func (h *snapshotPendingIndex) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if scope := snapshotOf(ctx); err == nil && scope != nil && scope.write && strings.EqualFold(fmt.Sprint(cmd.Args()[0]), "FT.INFO") {
			if h.reads.Add(1) == 1 {
				result := cmd.(interface {
					Val() any
					SetVal(any)
				})
				fields, _ := redisReplyFields(result.Val())
				fields["indexing"], fields["num_docs"] = int64(1), int64(0)
				result.SetVal(fields)
			}
		}
		return err
	}
}

func TestSnapshotPrivateIndexesReadyBeforeDML(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(7)")
	s := snapshotTestStore(t, raw)
	hook := &snapshotPendingIndex{}
	s.client.AddHook(hook)
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
	if err := s.prepareSnapshotTable(wctx, "public", "t"); err != nil {
		t.Fatal(err)
	}
	if hook.reads.Load() < 2 {
		t.Fatal("private workspace became executable before copied index readiness was checked")
	}
	if err := s.prepareSnapshotTable(wctx, "public", "t"); err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(wctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	if meta.SnapshotRows == nil || *meta.SnapshotRows != 0 || len(meta.SnapshotSegments) != 1 || meta.SnapshotSegments[0].Rows != 1 {
		t.Fatal("private empty delta and inherited base were not validated before execution")
	}
}

func TestSnapshotManagedStatements(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (id INTEGER, v BIGINT, pad VARCHAR)")
	h.exec("INSERT INTO t VALUES (1,9007199254740993,'a'),(2,9223372036854775807,'b')")
	h.expectRows("SELECT v FROM t ORDER BY id", "9007199254740993", "9223372036854775807")
	h.exec("UPDATE t SET v = 42 WHERE id = 1")
	h.expectRows("SELECT v FROM t ORDER BY id", "42", "9223372036854775807")
	h.exec("DELETE FROM t WHERE id = 2")
	h.expectRows("SELECT COUNT(*) FROM t", "1")
	h.exec("INSERT INTO t VALUES(3,3,'adbc:{meta}:table:user-value')")
	h.expectRows("SELECT pad FROM t WHERE id=3", "adbc:{meta}:table:user-value")
}

func TestSnapshotJoinsViewsDDLAndCatalogs(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE a(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO a VALUES(1,10),(2,20)")
	h.exec("CREATE TABLE b AS SELECT id,v+1 AS v FROM a")
	h.exec("CREATE VIEW av AS SELECT id,v FROM a")
	h.expectRows("WITH c AS (SELECT id,v FROM av) SELECT c.id,c.v,b.v FROM c JOIN b ON c.id=b.id ORDER BY c.id", "1|10|11", "2|20|21")
	h.expectRows("SELECT x.id,(SELECT max(y.v) FROM a y WHERE y.id<=x.id) FROM a x ORDER BY x.id", "1|10", "2|20")
	h.exec("UPDATE a SET v=b.v FROM b WHERE a.id=b.id")
	h.expectRows("SELECT v FROM av ORDER BY id", "11", "21")
	h.exec("UPDATE b SET v=99 WHERE id=1")
	h.exec("INSERT INTO b VALUES(3,30)")
	h.exec("MERGE INTO a USING b AS s ON a.id=s.id WHEN MATCHED THEN UPDATE SET v=s.v WHEN NOT MATCHED THEN INSERT(id,v) VALUES(s.id,s.v)")
	h.expectRows("SELECT id,v FROM a ORDER BY id", "1|99", "2|21", "3|30")
	h.exec("DELETE FROM a USING b WHERE a.id=b.id AND b.id=2")
	h.expectRows("SELECT id FROM a ORDER BY id", "1", "3")
	returned, _ := h.query("UPDATE a SET v=v+1 RETURNING id,v")
	sort.Strings(returned) // RETURNING has no ORDER BY contract.
	if strings.Join(returned, ",") != "1|100,3|31" {
		t.Fatalf("RETURNING: %v", returned)
	}
	h.expectRows("SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name", "a", "av", "b")
	h.expectRows("SELECT count(*) FROM pg_catalog.pg_class WHERE relname='a'", "1")
	schema := "public"
	if got, err := h.conn.GetTableSchema(h.ctx, nil, &schema, "a"); err != nil || got.NumFields() != 2 {
		t.Fatalf("table schema: %v %v", got, err)
	}
	h.exec("TRUNCATE a,b")
	h.expectRows("SELECT (SELECT COUNT(*) FROM a),(SELECT COUNT(*) FROM b)", "0|0")
	h.exec("DROP VIEW av")
	h.exec("DROP TABLE a")
	h.exec("DROP TABLE b")
	h.exec("CREATE SCHEMA other")
	h.exec("CREATE TABLE other.t(v INTEGER)")
	h.exec("DROP SCHEMA other CASCADE")
}

func TestSnapshotArrowIngestAtomicity(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER NOT NULL)")
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int32, Nullable: true}}, nil)
	batch := func(null bool) arrow.RecordBatch {
		b := array.NewRecordBuilder(mem, schema)
		defer b.Release()
		values := b.Field(0).(*array.Int32Builder)
		if null {
			values.AppendNull()
		} else {
			values.Append(7)
		}
		return b.NewRecordBatch()
	}
	for _, fail := range []bool{true, false} {
		first, second := batch(false), batch(fail)
		reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{first, second})
		first.Release()
		second.Release()
		if err != nil {
			t.Fatal(err)
		}
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, "t"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetOption(h.ctx, adbc.OptionKeyIngestMode, adbc.OptionValueIngestModeAppend); err != nil {
			t.Fatal(err)
		}
		if err := st.BindStream(h.ctx, reader); err != nil {
			t.Fatal(err)
		}
		before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
		n, err := st.ExecuteUpdate(h.ctx)
		_ = st.Close(h.ctx)
		if fail {
			if err == nil {
				t.Fatal("invalid later Arrow batch succeeded")
			}
			if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
				t.Fatal("failed ingest published a root")
			}
			h.expectRows("SELECT COUNT(*) FROM t", "0")
		} else {
			if err != nil || n != 2 {
				t.Fatalf("ingest: %d %v", n, err)
			}
			h.expectRows("SELECT v FROM t", "7", "7")
		}
	}
}

type snapshotFailingInput struct{ array.RecordReader }

func (snapshotFailingInput) Err() error { return io.ErrUnexpectedEOF }

func TestSnapshotArrowIngestModes(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int32, Nullable: true}}, nil)
	ingest := func(target, mode string, value int32, fail bool) {
		b := array.NewInt32Builder(mem)
		b.Append(value)
		a := b.NewArray()
		b.Release()
		rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
		a.Release()
		reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
		rec.Release()
		if err != nil {
			t.Fatal(err)
		}
		if fail {
			reader = snapshotFailingInput{reader}
		}
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, target); err != nil {
			t.Fatal(err)
		}
		if err := st.SetOption(h.ctx, adbc.OptionKeyIngestMode, mode); err != nil {
			t.Fatal(err)
		}
		if err := st.BindStream(h.ctx, reader); err != nil {
			t.Fatal(err)
		}
		before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
		n, err := st.ExecuteUpdate(h.ctx)
		if fail {
			if err == nil {
				t.Fatal("failed Arrow input published")
			}
			if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
				t.Fatal("failed input changed catalog root")
			}
		} else if err != nil || n != 1 {
			t.Fatalf("%s: %d %v", mode, n, err)
		}
	}
	ingest("t", adbc.OptionValueIngestModeCreate, 1, false)
	ingest("t", adbc.OptionValueIngestModeCreateAppend, 2, false)
	h.expectRows("SELECT v FROM t ORDER BY v", "1", "2")
	ingest("t", adbc.OptionValueIngestModeReplace, 3, false)
	ingest("t", adbc.OptionValueIngestModeAppend, 4, false)
	h.expectRows("SELECT v FROM t ORDER BY v", "3", "4")
	ingest("u", adbc.OptionValueIngestModeCreateAppend, 5, false)
	h.expectRows("SELECT v FROM u", "5")
	ingest("t", adbc.OptionValueIngestModeReplace, 6, true)
	h.expectRows("SELECT v FROM t ORDER BY v", "3", "4")
	ingest("failed", adbc.OptionValueIngestModeCreate, 7, true)
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='failed'", "0")
}

func TestSnapshotExpiredReaderAndCancellation(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,20) AS g(i)")
	for _, expire := range []bool{true, false} {
		ctx, cancel := context.WithCancel(h.ctx)
		st, err := h.conn.NewStatement(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetOption(ctx, OptionIntStreamBatchRows, "2"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSqlQuery(ctx, "SELECT v FROM t"); err != nil {
			t.Fatal(err)
		}
		r, _, err := st.ExecuteQuery(ctx)
		if err != nil {
			t.Fatal(err)
		}
		stream, ok := r.(*streamReader)
		if !ok {
			t.Fatalf("not streaming: %T", r)
		}
		if !r.Next() {
			t.Fatalf("initial batch: %v", r.Err())
		}
		if expire {
			record, _ := json.Marshal(map[string]any{"root": stream.snapshot.root, "expires": 0})
			if err := raw.HSet(ctx, snapshotCatalogKey, "p:"+stream.snapshot.token, record).Err(); err != nil {
				t.Fatal(err)
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("expired reader returned another batch")
			}
		} else {
			cancel()
			select {
			case <-stream.snapshot.done:
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled reader retained its pin")
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("cancelled reader returned another batch")
			}
		}
		r.Release()
		cancel()
		_ = st.Close(h.ctx)
		for field := range raw.HGetAll(h.ctx, snapshotCatalogKey).Val() {
			if strings.HasPrefix(field, "p:") {
				t.Fatalf("leaked reader pin %s", field)
			}
		}
	}
}

func TestSnapshotRejectsUnsupportedAndLegacyAccess(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	for _, query := range []string{"ALTER TABLE t ADD COLUMN x INTEGER", "CREATE TEMP TABLE tmp(v INTEGER)", "SELECT * FROM pg_temp.t", "SELECT * FROM redis_foreign.t"} {
		before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSqlQuery(h.ctx, query); err != nil {
			_ = st.Close(h.ctx)
			continue
		}
		_, err = st.ExecuteUpdate(h.ctx)
		_ = st.Close(h.ctx)
		if err == nil {
			t.Fatalf("unsupported operation succeeded: %s", query)
		}
		if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
			t.Fatalf("unsupported operation published: %s", query)
		}
	}
	uri := os.Getenv("REDIS_SNAPSHOT_URI")
	if db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: uri, OptionStringStatementSnapshots: "false"}); err == nil {
		_ = db.Close(h.ctx)
		t.Fatal("statement snapshots could be disabled")
	}
	if _, err := SweepReleased(h.ctx, map[string]string{adbc.OptionKeyURI: uri}, SweepOptions{}); err == nil {
		t.Fatal("legacy sweep accepted snapshot storage")
	}
	h.expectRows("SELECT COUNT(*) FROM t", "0")
}

func TestSnapshotRejectsExistingFormatWithoutUpgrade(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	if err := raw.Del(h.ctx, snapshotCatalogKey).Err(); err != nil {
		t.Fatal(err)
	}
	legacyKey := metaKey("public", "existing")
	legacy := `{"schema":"public","name":"existing","columns":[{"name":"v","type":{"Kind":2}}]}`
	if err := raw.Set(h.ctx, legacyKey, legacy, 0).Err(); err != nil {
		t.Fatal(err)
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	if conn, err := db.Open(h.ctx); err == nil {
		_ = conn.Close(h.ctx)
		t.Fatal("existing unversioned data was silently opened without snapshots")
	}
	if got := raw.Get(h.ctx, legacyKey).Val(); got != legacy {
		t.Fatal("rejected open modified existing data")
	}
	if raw.Exists(h.ctx, snapshotCatalogKey).Val() != 0 {
		t.Fatal("rejected open partially initialized new format")
	}
}

func TestSnapshotCapacityAndMissingIndex(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	s := snapshotTestStore(t, raw)
	var pins []*snapshotPin
	defer func() {
		for _, p := range pins {
			p.release()
		}
	}()
	for i := 0; i < 15; i++ {
		_, p, err := s.acquireSnapshot(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		pins = append(pins, p)
		h.exec(fmt.Sprintf("COMMENT ON TABLE t IS 'generation %d'", i))
	}
	before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	h.expectError("INSERT INTO t VALUES(2)", "generation limit")
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
		t.Fatal("capacity failure published")
	}
	for _, p := range pins {
		p.release()
	}
	pins = nil
	ctx, p, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(ctx, "public", "t")
	p.release()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.DROPINDEX", meta.index()).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError("SELECT COUNT(*) FROM t", "snapshot index is unavailable")
}

type snapshotReadBarrier struct {
	snapshotHook
	command        string
	ready, proceed chan struct{}
	hit            atomic.Bool
}

func (b *snapshotReadBarrier) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if err := b.checkpoint(ctx, cmd); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}
func (b *snapshotReadBarrier) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if err := b.checkpoint(ctx, cmd); err != nil {
				return err
			}
		}
		return next(ctx, cmds)
	}
}
func (b *snapshotReadBarrier) checkpoint(ctx context.Context, cmd goredis.Cmder) error {
	if scope := snapshotOf(ctx); scope != nil && !scope.write && strings.EqualFold(fmt.Sprint(cmd.Args()[0]), b.command) && b.hit.CompareAndSwap(false, true) {
		close(b.ready)
		select {
		case <-b.proceed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestSnapshotNestedReadsShareRoot(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE a(id INTEGER,v INTEGER)")
	h.exec("CREATE TABLE b(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO a VALUES(1,10)")
	h.exec("INSERT INTO b VALUES(1,20)")
	for _, test := range []struct{ query, command string }{
		{"SELECT a.v,b.v FROM a JOIN b ON a.id=b.id", "ZRANGE"},
		{"WITH x AS (SELECT v FROM a) SELECT x.v,b.v FROM x CROSS JOIN b", "ZRANGE"},
		{"SELECT a.v,(SELECT MAX(b.v) FROM b WHERE b.id=a.id) FROM a", "ZRANGE"},
		{"SELECT (SELECT SUM(v) FROM a),(SELECT SUM(v) FROM b)", "ZRANGE"},
		{"SELECT v,v*2 FROM a WHERE __rowid=1", "HMGET"},
	} {
		t.Run(test.query, func(t *testing.T) {
			h.exec("UPDATE a SET v=10")
			h.exec("UPDATE b SET v=20")
			s := snapshotTestStore(t, raw)
			barrier := &snapshotReadBarrier{command: test.command, ready: make(chan struct{}), proceed: make(chan struct{})}
			s.client.AddHook(barrier)
			parsed, err := ParseScript(test.query)
			if err != nil {
				t.Fatal(err)
			}
			e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
			type answer struct {
				res execResult
				err error
			}
			done := make(chan answer, 1)
			ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
			defer cancel()
			go func() { res, err := e.execute(ctx, parsed[0], nil, nil, nil); done <- answer{res, err} }()
			select {
			case <-barrier.ready:
			case <-ctx.Done():
				t.Fatal("query did not reach read barrier")
			}
			h.exec("UPDATE a SET v=110")
			h.exec("UPDATE b SET v=120")
			close(barrier.proceed)
			result := <-done
			if result.err != nil {
				t.Fatal(result.err)
			}
			if len(result.res.rows) != 1 || result.res.rows[0][0].I != 10 || result.res.rows[0][1].I != 20 {
				t.Fatalf("mixed root: %v", result.res.rows)
			}
		})
	}
}
