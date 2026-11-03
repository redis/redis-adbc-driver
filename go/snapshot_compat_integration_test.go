// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"os"
	"slices"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestSnapshotAlterCompatibility(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER, v INTEGER)")
	h.exec("INSERT INTO t VALUES (1,10),(2,20)")
	h.exec("UPDATE t SET v=30 WHERE id=2")
	s := snapshotTestStore(t, raw)
	oldCtx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(oldCtx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE t ADD COLUMN n INTEGER DEFAULT 7 NOT NULL")
	h.expectRows("SELECT * FROM t ORDER BY id", "1|10|7", "2|30|7")
	h.exec("ALTER TABLE t ALTER COLUMN v TYPE VARCHAR USING 'v=' || v")
	h.expectRows("SELECT * FROM t ORDER BY id", "1|v=10|7", "2|v=30|7")
	h.exec("ALTER TABLE t DROP COLUMN n")
	h.exec("ALTER TABLE t RENAME COLUMN v TO value")
	h.exec("ALTER TABLE t RENAME TO renamed")
	h.expectRows("SELECT * FROM renamed ORDER BY id", "1|v=10", "2|v=30")
	h.expectError("ALTER TABLE renamed ALTER COLUMN value TYPE INTEGER USING value::integer", "invalid")
	rows, err := s.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1", old.prefix() + "2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["v"] != "10" || rows[1]["v"] != "30" || len(old.Columns) != 2 {
		t.Fatalf("old snapshot changed: %#v", rows)
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	h.expectError("ALTER TABLE renamed ADD CONSTRAINT bad CHECK (id > 1)", "violated")
	if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("failed ALTER published")
	}
	h.expectRows("SELECT * FROM renamed ORDER BY id", "1|v=10", "2|v=30")
}

func TestSnapshotExternalCopyCompatibility(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	for key, v := range map[string]string{"app:1": "10", "app:2": "20"} {
		if err := raw.HSet(h.ctx, key, "v", v).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InspectHashes(h.ctx, options, HashInspectOptions{Prefix: "app:"}); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "app:", Table: "copied", Apply: true, OwnIndex: true}); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM copied ORDER BY v", "10", "20")
	if err := raw.HSet(h.ctx, "app:1", "v", "99").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM copied ORDER BY v", "10", "20")
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "app:", Table: "copied", Apply: true, OwnIndex: true}); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM copied ORDER BY v", "20", "99")
	h.exec("DROP TABLE copied")
	if raw.HGet(h.ctx, "app:1", "v").Val() != "99" {
		t.Fatal("DROP mutated application HASH")
	}
	if _, err := CopyHashes(h.ctx, options, HashCopyOptions{Prefix: "app:", Table: "copied2"}); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM copied2 ORDER BY v", "20", "99")
	if _, err := SweepReleased(h.ctx, options, SweepOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM copied2 ORDER BY v", "20", "99")
	if err := raw.Do(h.ctx, "FT.CREATE", "app_index", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "v", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM redis_foreign.app_index ORDER BY v", "20", "99")
	if err := raw.HSet(h.ctx, "app:2", "v", "77").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM redis_foreign.app_index ORDER BY v", "20", "99")
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringForeignIndexesRefresh, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM redis_foreign.app_index ORDER BY v", "77", "99")
	if err := raw.Do(h.ctx, "FT.INFO", "app_index").Err(); err != nil {
		t.Fatal("refresh removed source index", err)
	}
	h.expectError("UPDATE redis_foreign.app_index SET v=0", "read-only")
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "app_index").Err(); err != nil {
		t.Fatal(err)
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringForeignIndexesRefresh, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectError("SELECT * FROM redis_foreign.app_index", "does not exist")
	if raw.HGet(h.ctx, "app:2", "v").Val() != "77" {
		t.Fatal("refresh removed source HASH")
	}
}

func TestSnapshotLivePartitionCompatibility(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,100) g(i)")
	h.exec("UPDATE t SET v=-v WHERE id>=50")
	h.exec("DELETE FROM t WHERE id=10")
	_, parts := planPartitions(t, h, "SELECT id,v FROM t WHERE v<0", 4)
	h.exec("UPDATE t SET v=-999 WHERE id=100")
	var ids []int64
	for _, part := range parts.PartitionIDs {
		r, err := h.conn.ReadPartition(h.ctx, part)
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			record := r.RecordBatch()
			col := record.Column(0).(*array.Int32)
			vals := record.Column(1).(*array.Int32)
			for i := 0; i < col.Len(); i++ {
				id := int64(col.Value(i))
				if vals.Value(i) != -col.Value(i) && !(id == 100 && vals.Value(i) == -999) {
					t.Fatal("wrong visible value")
				}
				ids = append(ids, id)
			}
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		r.Release()
	}
	slices.Sort(ids)
	if len(ids) != 51 || ids[0] != 50 || ids[50] != 100 {
		t.Fatalf("partition membership: %v", ids)
	}
	h.exec("TRUNCATE t")
	if r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		r.Release()
		t.Fatal("TRUNCATE did not invalidate live descriptor")
	}
}

func TestSnapshotRetainedPartitionCompatibility(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id BIGINT,v INTEGER)")
	h.exec("INSERT INTO t VALUES (1,10),(2,20),(3,30)")
	h.exec("UPDATE t SET v=25 WHERE id=2")
	h.exec("DELETE FROM t WHERE id=1")
	storage := retainedFixture(t, raw, defaultRetainedLimits())
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	impl := st.(*statementImpl)
	impl.conn.retainedNamespace = storage.namespace
	impl.conn.partitionControlPersistence = "guaranteed" // Software setup verified noeviction before this test.
	_ = st.Close(h.ctx)
	schema, parts, rows := retainedAPIPlan(t, h.conn, "SELECT id FROM t WHERE v>=25 ORDER BY id")
	if rows != 2 {
		t.Fatalf("retained row count %d", rows)
	}
	h.exec("TRUNCATE t")
	if got := retainedAPIReadIDs(t, h.conn, schema, parts); !slices.Equal(got, []int64{2, 3}) {
		t.Fatalf("retained rows changed: %v", got)
	}
	descriptor, err := decodeRetainedPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	h.exec("DROP PARTITION RESULT '" + descriptor.Reference.JobID + "'")
	if r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		r.Release()
		t.Fatal("revoked retained result was readable")
	}
}

func TestSnapshotLivePartitionPinsStream(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,10 FROM generate_series(1,1201) g(i)")
	h.exec("UPDATE t SET v=11 WHERE id=1")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	st.(*statementImpl).conn.stream.batchRows = 7
	_ = st.Close(h.ctx)
	_, parts := planPartitions(t, h, "SELECT id,v FROM t", 1)
	r, err := h.conn.ReadPartition(h.ctx, parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if !r.Next() {
		t.Fatal("first partition batch", r.Err())
	}
	count := 0
	check := func() {
		record := r.RecordBatch()
		ids := record.Column(0).(*array.Int32)
		values := record.Column(1).(*array.Int32)
		for i := 0; i < ids.Len(); i++ {
			count++
			want := int32(10)
			if count == 1 {
				want = 11
			}
			if ids.Value(i) != int32(count) || values.Value(i) != want {
				t.Fatalf("mixed partition snapshot at %d", count)
			}
		}
	}
	check()
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	h2.exec("DROP TABLE t")
	for r.Next() {
		check()
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 1201 {
		t.Fatalf("partition returned %d rows", count)
	}
	for field := range raw.HGetAll(h.ctx, snapshotCatalogKey).Val() {
		if len(field) > 2 && field[:2] == "p:" {
			t.Fatal("partition leaked reader pin")
		}
	}
}

func TestSnapshotTemporaryCompatibility(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES (1)")
	h.exec("CREATE TEMP TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES (2)")
	h.exec("CREATE TEMP VIEW tv AS SELECT v FROM t")
	h.expectRows("SELECT * FROM t", "2")
	h.expectRows("SELECT * FROM pg_temp.tv", "2")
	h.expectRows("SELECT * FROM public.t", "1")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	h2.expectRows("SELECT * FROM t", "1")
	h2.expectError("SELECT * FROM pg_temp.t", "does not exist")
	h.exec("ALTER TABLE pg_temp.t ADD COLUMN x INTEGER DEFAULT 3")
	h.expectRows("SELECT * FROM t", "2|3")
	h.exec("DROP VIEW pg_temp.tv")
	h.exec("DROP TABLE pg_temp.t")
	h.expectRows("SELECT * FROM t", "1")
	h.exec("CREATE TEMP TABLE leftover(v INTEGER)")
	h.exec("INSERT INTO leftover VALUES (9)")
	s := snapshotTestStore(t, raw)
	pctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	tempSchema := st.(*statementImpl).conn.store.tempSchema()
	_ = st.Close(h.ctx)
	old, err := s.getTable(pctx, tempSchema, "leftover")
	if err != nil {
		t.Fatal(err)
	}
	h2.exec("INSERT INTO public.t VALUES (4)")
	if err := h.conn.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	h2.expectRows("SELECT * FROM t ORDER BY v", "1", "4")
	rows, err := s.fetchSnapshotVisible(pctx, old, []string{old.prefix() + "1"}, nil)
	if err != nil || len(rows) != 1 || rows[0]["v"] != "9" {
		t.Fatalf("temporary pinned storage lost: %v %v", rows, err)
	}
	current, p2, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.release()
	if exists, err := s.tableExists(current, tempSchema, "leftover"); err != nil || exists {
		t.Fatalf("temporary descriptor not removed: %v %v", exists, err)
	}
}

func TestSnapshotImportStartsBesideExternalData(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	if err := h.conn.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := raw.FlushDB(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, "source:1", "v", "5").Err(); err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "source:", Table: "imported", Apply: true, OwnIndex: true}); err != nil {
		t.Fatal(err)
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	c, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: c}
	h2.expectRows("SELECT v FROM imported", "5")
	h2.exec("CREATE TABLE protected(v INTEGER)")
	h2.exec("INSERT INTO protected VALUES (42)")
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "source:", Table: "protected", Apply: true, OwnIndex: true}); err == nil {
		t.Fatal("import replaced unrelated table")
	}
	h2.expectRows("SELECT * FROM protected", "42")
}

func TestSnapshotCompatibilityRejectsPriorFormat(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "format", "4").Err(); err != nil {
		t.Fatal(err)
	}
	if c, err := db.Open(h.ctx); err == nil {
		_ = c.Close(h.ctx)
		t.Fatal("prior format was opened")
	}
	if format := raw.HGet(h.ctx, snapshotCatalogKey, "format").Val(); format != "4" {
		t.Fatal("rejected open modified prior catalog", format)
	}
}

func TestSnapshotTemporarySweepProtectsRevivedOwner(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TEMP TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES (1)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := st.(*statementImpl).conn.store
	id := s.stopHeartbeat()
	_ = st.Close(h.ctx)
	if err := raw.Del(h.ctx, tempAliveKey(id)).Err(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	stillDead := func() bool {
		calls++
		if calls == 2 {
			if err := raw.Set(h.ctx, tempAliveKey(id), "1", tempTTL).Err(); err != nil {
				t.Fatal(err)
			}
		}
		return true
	}
	if err := s.dropSnapshotTempAttempt(h.ctx, id, stillDead); err != errOwnerAlive {
		t.Fatalf("revived owner cleanup: %v", err)
	}
	pctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	if exists, err := s.tableExists(pctx, tempSchemaBase+id, "t"); err != nil || !exists {
		t.Fatalf("revived temporary descriptor lost: %v %v", exists, err)
	}
}
